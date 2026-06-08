//go:build !windows && !darwin

// Stub audio capture for platforms without a native loopback implementation.
// Generates a 440Hz sine wave for testing the pipeline.
package capture

import (
	"fmt"
	"math"
	"sync/atomic"
)

// StubCapture generates synthetic audio for testing on unsupported platforms.
type StubCapture struct {
	format  AudioFormat
	running atomic.Bool
	phase   float64
}

// NewStubCapture creates a stub capture that produces a 440Hz sine wave.
func NewStubCapture() *StubCapture {
	return &StubCapture{
		format: AudioFormat{
			SampleRate: 48000,
			Channels:   2,
			BitDepth:   32,
		},
	}
}

func (s *StubCapture) Start() error {
	s.running.Store(true)
	return nil
}

func (s *StubCapture) Stop() error {
	s.running.Store(false)
	return nil
}

func (s *StubCapture) Format() AudioFormat {
	return s.format
}

func (s *StubCapture) Read(buf []float32) (int, error) {
	if !s.running.Load() {
		return 0, fmt.Errorf("capture not running")
	}

	freq := 440.0
	amplitude := 0.3
	phaseInc := 2.0 * math.Pi * freq / float64(s.format.SampleRate)

	for i := 0; i < len(buf); i += s.format.Channels {
		sample := float32(amplitude * math.Sin(s.phase))
		for ch := 0; ch < s.format.Channels && i+ch < len(buf); ch++ {
			buf[i+ch] = sample
		}
		s.phase += phaseInc
		if s.phase >= 2.0*math.Pi {
			s.phase -= 2.0 * math.Pi
		}
	}

	return len(buf), nil
}

func (s *StubCapture) LoopbackLatencyMs() float64 {
	return 0.0 // no real loopback
}

var _ AudioCapture = (*StubCapture)(nil)

func platformNewCapture() AudioCapture {
	return NewStubCapture()
}
