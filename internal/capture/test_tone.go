package capture

import (
	"math"
	"sync"
	"time"
)

type TestToneCapture struct {
	format   AudioFormat
	running  bool
	mu       sync.Mutex
	phase    float64
}

func NewTestToneCapture(sampleRate, channels int) AudioCapture {
	return &TestToneCapture{
		format: AudioFormat{
			SampleRate: sampleRate,
			Channels:   channels,
			BitDepth:   32,
		},
	}
}

func (t *TestToneCapture) Start() error {
	t.mu.Lock()
	t.running = true
	t.phase = 0
	t.mu.Unlock()
	return nil
}

func (t *TestToneCapture) Stop() error {
	t.mu.Lock()
	t.running = false
	t.mu.Unlock()
	return nil
}

func (t *TestToneCapture) Format() AudioFormat {
	return t.format
}

func (t *TestToneCapture) Read(buf []float32) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.running {
		return 0, nil
	}

	// Wait 5ms to simulate real audio pacing (assuming buf holds 5ms of data)
	time.Sleep(5 * time.Millisecond)

	n := len(buf)
	samplesPerChannel := n / t.format.Channels

	// 440Hz sine wave
	freq := 440.0
	phaseInc := 2.0 * math.Pi * freq / float64(t.format.SampleRate)

	for i := 0; i < samplesPerChannel; i++ {
		sample := float32(math.Sin(t.phase) * 0.5) // 50% volume
		t.phase += phaseInc
		if t.phase >= 2.0*math.Pi {
			t.phase -= 2.0 * math.Pi
		}

		for ch := 0; ch < t.format.Channels; ch++ {
			buf[i*t.format.Channels+ch] = sample
		}
	}

	return n, nil
}

func (t *TestToneCapture) LoopbackLatencyMs() float64 {
	return 5.0
}
