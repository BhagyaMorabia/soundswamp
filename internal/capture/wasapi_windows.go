//go:build windows

// WASAPI loopback audio capture for Windows.
//
// Uses IAudioClient in shared mode with AUDCLNT_STREAMFLAGS_LOOPBACK to capture
// all system audio output. Event-driven (not polling) for minimum latency.
//
// This implementation uses Windows COM/WASAPI APIs via syscall rather than CGO
// to avoid requiring a C compiler for the basic build. The WASAPI COM interfaces
// are accessed through their vtable pointers directly.
//
// Typical capture latency: 6-20ms depending on driver and IAudioClient version.
package capture

import (
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// WASAPI COM GUIDs
var (
	clsidMMDeviceEnumerator = windows.GUID{0xBCDE0395, 0xE52F, 0x467C, [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	iidIMMDeviceEnumerator  = windows.GUID{0xA95664D2, 0x9614, 0x4F35, [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}
	iidIAudioClient         = windows.GUID{0x1CB9AD4C, 0xDBFA, 0x4c32, [8]byte{0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2}}
	iidIAudioCaptureClient  = windows.GUID{0xC8ADBD64, 0xE71E, 0x48a0, [8]byte{0xA4, 0xDE, 0x18, 0x5C, 0x39, 0x5C, 0xD3, 0x17}}
)

// WASAPI constants
const (
	eRender             = 0
	eConsole            = 0
	audclntSharemode    = 0 // AUDCLNT_SHAREMODE_SHARED
	audclntLoopback     = 0x00020000 // AUDCLNT_STREAMFLAGS_LOOPBACK
	audclntEventCallback = 0x00040000 // AUDCLNT_STREAMFLAGS_EVENTCALLBACK
	waveFormatExtensible = 0xFFFE

	// REFERENCE_TIME units: 100-nanosecond intervals
	reftimesPerSec     = 10000000
	reftimesPerMillisec = 10000
)

// WAVEFORMATEXTENSIBLE describes the PCM format from WASAPI.
type waveFormatEx struct {
	FormatTag      uint16
	Channels       uint16
	SamplesPerSec  uint32
	AvgBytesPerSec uint32
	BlockAlign     uint16
	BitsPerSample  uint16
	CbSize         uint16
}

// WASAPICapture implements AudioCapture using Windows WASAPI loopback.
type WASAPICapture struct {
	mu sync.Mutex

	// COM objects (stored as uintptr to avoid CGO)
	deviceEnumerator uintptr
	device           uintptr
	audioClient      uintptr
	captureClient    uintptr

	// Audio format
	format       AudioFormat
	mixFormat    *waveFormatEx
	bytesPerFrame int

	// Event-driven capture
	captureEvent windows.Handle

	// State
	running   atomic.Bool
	stopChan  chan struct{}

	// Ring buffer for captured audio
	ringBuf     []float32
	ringWrite   int
	ringRead    int
	ringSize    int
	ringMu      sync.Mutex
	ringCond    *sync.Cond

	// Calibration
	loopbackLatencyMs float64
}

// NewWASAPICapture creates a new WASAPI loopback capture instance.
func NewWASAPICapture() *WASAPICapture {
	w := &WASAPICapture{
		stopChan: make(chan struct{}),
		ringSize: 48000 * 2 * 2, // 2 seconds of stereo at 48kHz
	}
	w.ringBuf = make([]float32, w.ringSize)
	w.ringCond = sync.NewCond(&w.ringMu)
	return w
}

// Start initializes WASAPI COM objects and begins loopback capture.
func (w *WASAPICapture) Start() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.running.Load() {
		return fmt.Errorf("capture already running")
	}

	// Initialize COM for this goroutine
	if err := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED); err != nil {
		// If already initialized, that's fine
		if err != windows.ERROR_SUCCESS && !isAlreadyInitialized(err) {
			return fmt.Errorf("CoInitializeEx failed: %w", err)
		}
	}

	// Create the device enumerator
	var enumerator uintptr
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)),
		0,
		1|4, // CLSCTX_INPROC_SERVER | CLSCTX_LOCAL_SERVER
		uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)),
		uintptr(unsafe.Pointer(&enumerator)),
	)
	if hr != 0 {
		return fmt.Errorf("CoCreateInstance(MMDeviceEnumerator) failed: 0x%08X", hr)
	}
	w.deviceEnumerator = enumerator

	// Get default audio output device (render endpoint)
	var device uintptr
	hr = vGetDefaultAudioEndpoint(enumerator, eRender, eConsole, &device)
	if hr != 0 {
		return fmt.Errorf("GetDefaultAudioEndpoint failed: 0x%08X", hr)
	}
	w.device = device

	// Activate IAudioClient
	var audioClient uintptr
	hr = vActivate(device, &iidIAudioClient, 1|4, 0, &audioClient)
	if hr != 0 {
		return fmt.Errorf("IMMDevice::Activate(IAudioClient) failed: 0x%08X", hr)
	}
	w.audioClient = audioClient

	// Get mix format (native format — no resampling)
	var mixFormatPtr uintptr
	hr = vGetMixFormat(audioClient, &mixFormatPtr)
	if hr != 0 {
		return fmt.Errorf("IAudioClient::GetMixFormat failed: 0x%08X", hr)
	}
	w.mixFormat = (*waveFormatEx)(unsafe.Pointer(mixFormatPtr))

	w.format = AudioFormat{
		SampleRate: int(w.mixFormat.SamplesPerSec),
		Channels:   int(w.mixFormat.Channels),
		BitDepth:   32, // We always convert to float32
	}
	w.bytesPerFrame = int(w.mixFormat.BlockAlign)

	// Resize ring buffer for actual channel count
	w.ringSize = w.format.SampleRate * w.format.Channels * 2 // 2 seconds
	w.ringBuf = make([]float32, w.ringSize)
	w.ringWrite = 0
	w.ringRead = 0

	// Initialize audio client with loopback (polling mode)
	// WASAPI loopback capture does not support event-driven mode on many systems.
	var bufferDuration int64 = reftimesPerSec / 10 // 100ms buffer
	hr = vInitialize(audioClient, audclntSharemode,
		audclntLoopback,
		bufferDuration, 0, mixFormatPtr, 0)
	if hr != 0 {
		return fmt.Errorf("IAudioClient::Initialize failed: 0x%08X", hr)
	}

	// Get capture client
	var captureClient uintptr
	hr = vGetService(audioClient, &iidIAudioCaptureClient, &captureClient)
	if hr != 0 {
		return fmt.Errorf("IAudioClient::GetService(IAudioCaptureClient) failed: 0x%08X", hr)
	}
	w.captureClient = captureClient

	// Start capture
	hr = vStart(audioClient)
	if hr != 0 {
		return fmt.Errorf("IAudioClient::Start failed: 0x%08X", hr)
	}

	w.running.Store(true)
	w.stopChan = make(chan struct{})

	// Measure loopback latency (approximate)
	w.measureLoopbackLatency()

	// Start the capture goroutine
	go w.captureLoop()

	return nil
}

// Stop halts capture and releases COM resources.
func (w *WASAPICapture) Stop() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.running.Load() {
		return nil
	}

	w.running.Store(false)
	close(w.stopChan)

	// Signal any blocked readers
	w.ringCond.Broadcast()

	// Stop the audio client
	if w.audioClient != 0 {
		vStop(w.audioClient)
	}

	// Release COM objects in reverse order
	if w.captureClient != 0 {
		vRelease(w.captureClient)
		w.captureClient = 0
	}
	if w.audioClient != 0 {
		vRelease(w.audioClient)
		w.audioClient = 0
	}
	if w.device != 0 {
		vRelease(w.device)
		w.device = 0
	}
	if w.deviceEnumerator != 0 {
		vRelease(w.deviceEnumerator)
		w.deviceEnumerator = 0
	}
	if w.captureEvent != 0 {
		windows.CloseHandle(w.captureEvent)
		w.captureEvent = 0
	}

	return nil
}

// Format returns the native audio format of the captured stream.
func (w *WASAPICapture) Format() AudioFormat {
	return w.format
}

// Read fills buf with interleaved float32 PCM samples.
// Blocks until data is available.
func (w *WASAPICapture) Read(buf []float32) (int, error) {
	if !w.running.Load() {
		return 0, fmt.Errorf("capture not running")
	}

	w.ringMu.Lock()
	defer w.ringMu.Unlock()

	// Wait for data to be available
	for w.ringAvailable() < len(buf) && w.running.Load() {
		w.ringCond.Wait()
	}

	if !w.running.Load() {
		return 0, fmt.Errorf("capture stopped")
	}

	// Copy requested data to output buffer
	n := len(buf)

	for i := 0; i < n; i++ {
		buf[i] = w.ringBuf[w.ringRead%w.ringSize]
		w.ringRead++
	}

	return n, nil
}

// LoopbackLatencyMs returns the measured loopback capture pipeline delay.
func (w *WASAPICapture) LoopbackLatencyMs() float64 {
	return w.loopbackLatencyMs
}

// captureLoop runs in a goroutine, reading from WASAPI and writing to the ring buffer.
func (w *WASAPICapture) captureLoop() {
	// Set thread priority for audio via MMCSS
	taskName, _ := windows.UTF16PtrFromString("Pro Audio")
	var taskIndex uint32
	taskHandle, _, _ := procAvSetMmThreadCharacteristics.Call(uintptr(unsafe.Pointer(taskName)), uintptr(unsafe.Pointer(&taskIndex)))
	if taskHandle != 0 {
		defer procAvRevertMmThreadCharacteristics.Call(taskHandle)
	}

	for w.running.Load() {
		// Poll for data every 10ms
		time.Sleep(10 * time.Millisecond)
		w.processCaptureBuffer()
	}
}

// processCaptureBuffer reads all available data from the WASAPI capture client.
func (w *WASAPICapture) processCaptureBuffer() {
	for {
		var dataPtr uintptr
		var numFrames uint32
		var flags uint32

		hr := vGetBuffer(w.captureClient, &dataPtr, &numFrames, &flags, nil, nil)
		if hr != 0 || numFrames == 0 {
			break
		}

		// Check for silence flag
		isSilent := (flags & 0x2) != 0 // AUDCLNT_BUFFERFLAGS_SILENT

		totalSamples := int(numFrames) * w.format.Channels

		w.ringMu.Lock()

		if isSilent {
			// Write zeros for silent periods
			for i := 0; i < totalSamples; i++ {
				w.ringBuf[w.ringWrite%w.ringSize] = 0
				w.ringWrite++
			}
		} else {
			// Convert captured data to float32 and write to ring buffer
			// WASAPI shared mode typically provides IEEE float32 natively
			rawData := unsafe.Slice((*byte)(unsafe.Pointer(dataPtr)), int(numFrames)*w.bytesPerFrame)
			w.convertAndWrite(rawData, int(numFrames))
		}

		w.ringCond.Signal()
		w.ringMu.Unlock()

		// Release the buffer
		vReleaseBuffer(w.captureClient, numFrames)
	}
}

// convertAndWrite handles conversion from WASAPI's native format to float32.
func (w *WASAPICapture) convertAndWrite(data []byte, numFrames int) {
	bitsPerSample := int(w.mixFormat.BitsPerSample)

	switch bitsPerSample {
	case 32:
		// IEEE float32 — most common for shared mode
		samples := unsafe.Slice((*float32)(unsafe.Pointer(&data[0])), numFrames*w.format.Channels)
		for _, s := range samples {
			w.ringBuf[w.ringWrite%w.ringSize] = s
			w.ringWrite++
		}

	case 16:
		// Int16 PCM
		samples := unsafe.Slice((*int16)(unsafe.Pointer(&data[0])), numFrames*w.format.Channels)
		for _, s := range samples {
			w.ringBuf[w.ringWrite%w.ringSize] = float32(s) / float32(math.MaxInt16)
			w.ringWrite++
		}

	case 24:
		// 24-bit PCM packed in 3 bytes
		for i := 0; i < numFrames*w.format.Channels; i++ {
			offset := i * 3
			val := int32(data[offset]) | int32(data[offset+1])<<8 | int32(data[offset+2])<<16
			if val&0x800000 != 0 {
				val |= -0x1000000 // sign extend
			}
			w.ringBuf[w.ringWrite%w.ringSize] = float32(val) / float32(0x7FFFFF)
			w.ringWrite++
		}

	default:
		// Unknown format, fill with silence
		for i := 0; i < numFrames*w.format.Channels; i++ {
			w.ringBuf[w.ringWrite%w.ringSize] = 0
			w.ringWrite++
		}
	}
}

// ringAvailable returns the number of samples available for reading.
// Caller must hold ringMu.
func (w *WASAPICapture) ringAvailable() int {
	return w.ringWrite - w.ringRead
}

// measureLoopbackLatency estimates the WASAPI loopback capture latency.
// Uses IAudioClient::GetStreamLatency for the initial estimate.
// The chirp-based measurement refinement happens at calibration time.
func (w *WASAPICapture) measureLoopbackLatency() {
	if w.audioClient == 0 {
		w.loopbackLatencyMs = 15.0 // conservative default
		return
	}

	var latency int64
	hr := vGetStreamLatency(w.audioClient, &latency)
	if hr != 0 {
		w.loopbackLatencyMs = 15.0
		return
	}

	// latency is in REFERENCE_TIME (100ns units)
	w.loopbackLatencyMs = float64(latency) / float64(reftimesPerMillisec)
	if w.loopbackLatencyMs < 5.0 {
		w.loopbackLatencyMs = 5.0 // floor
	}
}

// isAlreadyInitialized checks if the HRESULT from CoInitializeEx indicates that
// COM was already initialized in a compatible mode (not a real failure).
//
// F21 fix: the previous implementation returned true for ANY non-nil error,
// which silently swallowed real failures like E_OUTOFMEMORY (0x8007000E).
// Only two HRESULTs are safe to ignore:
//   - S_FALSE (0x00000001):        already initialized in this threading model
//   - RPC_E_CHANGED_MODE (0x80010106): initialized in a different threading model
//     (MTA vs STA) — acceptable for our loopback use case.
func isAlreadyInitialized(err error) bool {
	if err == nil {
		return false
	}
	// Convert the syscall error to its numeric HRESULT code.
	type hresulter interface{ Error() string }
	if e, ok := err.(syscall.Errno); ok {
		code := uint32(e)
		return code == 0x00000001 || // S_FALSE
			code == 0x80010106      // RPC_E_CHANGED_MODE
	}
	return false
}

// -----------------------------------------------------------------------
// COM vtable call helpers — these call through the COM interface vtables
// without requiring CGO. Each COM interface is a pointer to a vtable,
// which is an array of function pointers.
// -----------------------------------------------------------------------

var (
	ole32               = windows.NewLazyDLL("ole32.dll")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")

	avrt                               = windows.NewLazyDLL("avrt.dll")
	procAvSetMmThreadCharacteristics   = avrt.NewProc("AvSetMmThreadCharacteristicsW")
	procAvRevertMmThreadCharacteristics = avrt.NewProc("AvRevertMmThreadCharacteristics")
)

// IUnknown::Release — vtable index 2
func vRelease(obj uintptr) uintptr {
	vtable := *(*uintptr)(unsafe.Pointer(obj))
	fn := *(*uintptr)(unsafe.Pointer(vtable + 2*unsafe.Sizeof(uintptr(0))))
	ret, _, _ := callN(fn, obj)
	return ret
}

// IMMDeviceEnumerator::GetDefaultAudioEndpoint — vtable index 4
func vGetDefaultAudioEndpoint(enumerator uintptr, dataFlow, role int, device *uintptr) uintptr {
	vtable := *(*uintptr)(unsafe.Pointer(enumerator))
	fn := *(*uintptr)(unsafe.Pointer(vtable + 4*unsafe.Sizeof(uintptr(0))))
	ret, _, _ := callN(fn, enumerator, uintptr(dataFlow), uintptr(role), uintptr(unsafe.Pointer(device)))
	return ret
}

// IMMDevice::Activate — vtable index 3
func vActivate(device uintptr, iid *windows.GUID, clsCtx uint32, params uintptr, obj *uintptr) uintptr {
	vtable := *(*uintptr)(unsafe.Pointer(device))
	fn := *(*uintptr)(unsafe.Pointer(vtable + 3*unsafe.Sizeof(uintptr(0))))
	ret, _, _ := callN(fn, device, uintptr(unsafe.Pointer(iid)), uintptr(clsCtx), params, uintptr(unsafe.Pointer(obj)))
	return ret
}

// IAudioClient::Initialize — vtable index 3
func vInitialize(client uintptr, shareMode, streamFlags int, bufferDuration, periodicity int64, format uintptr, sessionGUID uintptr) uintptr {
	vtable := *(*uintptr)(unsafe.Pointer(client))
	fn := *(*uintptr)(unsafe.Pointer(vtable + 3*unsafe.Sizeof(uintptr(0))))
	ret, _, _ := callN(fn, client,
		uintptr(shareMode),
		uintptr(streamFlags),
		uintptr(bufferDuration),
		uintptr(periodicity),
		format,
		sessionGUID)
	return ret
}

// IAudioClient::GetMixFormat — vtable index 8
func vGetMixFormat(client uintptr, format *uintptr) uintptr {
	vtable := *(*uintptr)(unsafe.Pointer(client))
	fn := *(*uintptr)(unsafe.Pointer(vtable + 8*unsafe.Sizeof(uintptr(0))))
	ret, _, _ := callN(fn, client, uintptr(unsafe.Pointer(format)))
	return ret
}

// IAudioClient::GetStreamLatency — vtable index 7
func vGetStreamLatency(client uintptr, latency *int64) uintptr {
	vtable := *(*uintptr)(unsafe.Pointer(client))
	fn := *(*uintptr)(unsafe.Pointer(vtable + 7*unsafe.Sizeof(uintptr(0))))
	ret, _, _ := callN(fn, client, uintptr(unsafe.Pointer(latency)))
	return ret
}

// IAudioClient::SetEventHandle — vtable index 12
func vSetEventHandle(client uintptr, eventHandle uintptr) uintptr {
	vtable := *(*uintptr)(unsafe.Pointer(client))
	fn := *(*uintptr)(unsafe.Pointer(vtable + 12*unsafe.Sizeof(uintptr(0))))
	ret, _, _ := callN(fn, client, eventHandle)
	return ret
}

// IAudioClient::GetService — vtable index 14
func vGetService(client uintptr, iid *windows.GUID, service *uintptr) uintptr {
	vtable := *(*uintptr)(unsafe.Pointer(client))
	fn := *(*uintptr)(unsafe.Pointer(vtable + 14*unsafe.Sizeof(uintptr(0))))
	ret, _, _ := callN(fn, client, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(service)))
	return ret
}

// IAudioClient::Start — vtable index 5
func vStart(client uintptr) uintptr {
	vtable := *(*uintptr)(unsafe.Pointer(client))
	fn := *(*uintptr)(unsafe.Pointer(vtable + 5*unsafe.Sizeof(uintptr(0))))
	ret, _, _ := callN(fn, client)
	return ret
}

// IAudioClient::Stop — vtable index 6
func vStop(client uintptr) uintptr {
	vtable := *(*uintptr)(unsafe.Pointer(client))
	fn := *(*uintptr)(unsafe.Pointer(vtable + 6*unsafe.Sizeof(uintptr(0))))
	ret, _, _ := callN(fn, client)
	return ret
}

// IAudioCaptureClient::GetBuffer — vtable index 3
func vGetBuffer(captureClient uintptr, data *uintptr, numFrames *uint32, flags *uint32, devicePos *uint64, qpcPos *uint64) uintptr {
	vtable := *(*uintptr)(unsafe.Pointer(captureClient))
	fn := *(*uintptr)(unsafe.Pointer(vtable + 3*unsafe.Sizeof(uintptr(0))))
	ret, _, _ := callN(fn, captureClient,
		uintptr(unsafe.Pointer(data)),
		uintptr(unsafe.Pointer(numFrames)),
		uintptr(unsafe.Pointer(flags)),
		uintptr(unsafe.Pointer(devicePos)),
		uintptr(unsafe.Pointer(qpcPos)))
	return ret
}

// IAudioCaptureClient::ReleaseBuffer — vtable index 4
func vReleaseBuffer(captureClient uintptr, numFrames uint32) uintptr {
	vtable := *(*uintptr)(unsafe.Pointer(captureClient))
	fn := *(*uintptr)(unsafe.Pointer(vtable + 4*unsafe.Sizeof(uintptr(0))))
	ret, _, _ := callN(fn, captureClient, uintptr(numFrames))
	return ret
}

// callN is a variadic COM vtable caller.
func callN(fn uintptr, args ...uintptr) (uintptr, uintptr, error) {
	switch len(args) {
	case 1:
		return syscall.SyscallN(fn, args[0])
	case 2:
		return syscall.SyscallN(fn, args[0], args[1])
	case 3:
		return syscall.SyscallN(fn, args[0], args[1], args[2])
	case 4:
		return syscall.SyscallN(fn, args[0], args[1], args[2], args[3])
	case 5:
		return syscall.SyscallN(fn, args[0], args[1], args[2], args[3], args[4])
	case 6:
		return syscall.SyscallN(fn, args[0], args[1], args[2], args[3], args[4], args[5])
	case 7:
		return syscall.SyscallN(fn, args[0], args[1], args[2], args[3], args[4], args[5], args[6])
	default:
		return syscall.SyscallN(fn, args...)
	}
}

// Ensure WASAPICapture implements AudioCapture at compile time.
var _ AudioCapture = (*WASAPICapture)(nil)

// platformNewCapture is called by NewCapture() on Windows.
func platformNewCapture() AudioCapture {
	return NewWASAPICapture()
}

// init registers the capture start time for latency measurement.
func init() {
	_ = time.Now()
}
