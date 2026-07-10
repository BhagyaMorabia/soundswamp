package stream

import (
	"fmt"
	"log/slog"
	"net"
	"syscall"
	"unsafe"
)

var (
	qwave = syscall.NewLazyDLL("qwave.dll")

	procQOSCreateHandle    = qwave.NewProc("QOSCreateHandle")
	procQOSCloseHandle     = qwave.NewProc("QOSCloseHandle")
	procQOSAddSocketToFlow = qwave.NewProc("QOSAddSocketToFlow")
)

type qosVersion struct {
	MajorVersion uint16
	MinorVersion uint16
}

const (
	// Traffic types
	qosTrafficTypeBestEffort    = 0
	qosTrafficTypeBackground    = 1
	qosTrafficTypeExcellentEffort = 2
	qosTrafficTypeAudioVideo    = 3
	qosTrafficTypeVoice         = 4
	qosTrafficTypeControl       = 5

	// Flags
	qosNonAdaptiveFlow = 0x00000002
)

// QOSManager handles the qWAVE (Quality Windows Audio/Video Experience) API.
type QOSManager struct {
	handle uintptr
	logger *slog.Logger
}

// NewQOSManager initializes the qWAVE API.
// It returns nil if the qWAVE API is unavailable on this system.
func NewQOSManager(logger *slog.Logger) *QOSManager {
	if err := qwave.Load(); err != nil {
		logger.Warn("qWAVE API (qwave.dll) not found on this system. QoS disabled.", "error", err)
		return nil
	}

	version := qosVersion{MajorVersion: 1, MinorVersion: 0}
	var handle uintptr

	ret, _, err := procQOSCreateHandle.Call(
		uintptr(unsafe.Pointer(&version)),
		uintptr(unsafe.Pointer(&handle)),
	)

	if ret == 0 {
		logger.Warn("QOSCreateHandle failed (is the 'qWave' Windows service running?). QoS disabled.", "error", err)
		return nil
	}

	logger.Info("qWAVE QoS API successfully initialized")
	return &QOSManager{
		handle: handle,
		logger: logger,
	}
}

// AddUDPSocketToVoiceFlow registers a UDP socket with the qWAVE subsystem to receive
// highest-priority DSCP markings (Voice/AudioVideo) from the OS and Wi-Fi adapter.
func (qm *QOSManager) AddUDPSocketToVoiceFlow(conn *net.UDPConn) error {
	if qm == nil || qm.handle == 0 {
		return nil // QoS is disabled, ignore
	}

	sysConn, err := conn.SyscallConn()
	if err != nil {
		return fmt.Errorf("failed to get syscall connection: %w", err)
	}

	var qosErr error
	var flowID uint32

	err = sysConn.Control(func(fd uintptr) {
		ret, _, e := procQOSAddSocketToFlow.Call(
			qm.handle,
			fd,
			0, // DestAddr (optional, 0 means all traffic on this socket if not connected)
			uintptr(qosTrafficTypeVoice),
			uintptr(qosNonAdaptiveFlow), // Flags (Must be NON_ADAPTIVE if DestAddr is NULL)
			uintptr(unsafe.Pointer(&flowID)),
		)
		if ret == 0 {
			qosErr = fmt.Errorf("QOSAddSocketToFlow failed: %w", e)
		}
	})

	if err != nil {
		return fmt.Errorf("Control failed: %w", err)
	}

	if qosErr != nil {
		return qosErr
	}

	qm.logger.Info("Socket successfully added to qWAVE Voice flow", "flow_id", flowID)
	return nil
}

// Close releases the qWAVE handle.
func (qm *QOSManager) Close() {
	if qm != nil && qm.handle != 0 {
		procQOSCloseHandle.Call(qm.handle)
		qm.handle = 0
	}
}
