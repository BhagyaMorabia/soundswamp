// Package capture defines the audio capture interface and common types
// for platform-specific loopback implementations.
package capture

// AudioFormat describes the PCM format of captured audio.
type AudioFormat struct {
	SampleRate int // e.g. 48000
	Channels   int // e.g. 2 (stereo), 6 (5.1), 8 (7.1)
	BitDepth   int // 32 for float32
}

// AudioCapture is the interface that platform-specific loopback implementations
// must satisfy. Implementations exist for:
//   - Windows: WASAPI loopback (wasapi_windows.go)
//   - macOS:   CoreAudio + BlackHole (coreaudio_darwin.go)
//   - Stub:    Silent PCM generator for testing (stub.go)
type AudioCapture interface {
	// Start begins capturing audio from the system's default output device.
	// It must be called before Read.
	Start() error

	// Stop halts capture and releases OS audio resources.
	Stop() error

	// Format returns the audio format of the captured stream.
	// This is determined by the OS audio mixer's native format — no resampling occurs.
	Format() AudioFormat

	// Read fills buf with interleaved float32 PCM samples and returns the number
	// of samples written. It blocks until at least one frame of audio is available.
	// The buffer layout is [L0, R0, L1, R1, ...] for stereo, or
	// [FL0, FR0, C0, LFE0, SL0, SR0, FL1, FR1, ...] for 5.1.
	Read(buf []float32) (int, error)

	// LoopbackLatencyMs returns the measured one-way capture pipeline latency in
	// milliseconds. This is used by Fix B (laptop loopback offset) to subtract
	// the capture delay from the laptop client's playback target.
	// The value is measured during the calibration chirp at startup.
	LoopbackLatencyMs() float64
}
