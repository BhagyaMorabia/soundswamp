// Package codec provides Opus encoding and decoding for the SoundSwarm audio pipeline.
//
// In production, this wraps libopus via CGO. This file provides the Go-side interface
// and a pure-Go PCM passthrough fallback for development/testing without libopus installed.
//
// Build with -tags opus_cgo to use the real libopus bindings (requires libopus-dev).
// Without that tag, the encoder passes raw PCM (no compression), which is fine for
// testing the pipeline on localhost.
package codec

import (
	"encoding/binary"
	"fmt"
	"math"
)

// SampleRate is the fixed sample rate for the entire SoundSwarm pipeline.
// Opus and all capture/playback systems operate at 48kHz.
const SampleRate = 48000

// FrameSamples returns the number of samples per channel for a given frame duration.
func FrameSamples(frameMs int) int {
	return SampleRate * frameMs / 1000 // 48000 * 10 / 1000 = 480
}

// EncoderConfig holds Opus encoder parameters.
type EncoderConfig struct {
	SampleRate int // Must be 48000
	Channels   int // 1 (mono) or 2 (stereo)
	Bitrate    int // bits per second, e.g. 128000
	FrameMs    int // 10, 20, 40, or 60
	FEC        bool // Enable in-band forward error correction
	VBR        bool // Variable bitrate
}

// DefaultEncoderConfig returns the standard SoundSwarm encoder configuration.
func DefaultEncoderConfig() EncoderConfig {
	return EncoderConfig{
		SampleRate: SampleRate,
		Channels:   2,
		Bitrate:    128000,
		FrameMs:    5,
		FEC:        true,
		VBR:        true,
	}
}

// Encoder compresses PCM audio into Opus frames.
type Encoder struct {
	config    EncoderConfig
	frameSamples int
}

// NewEncoder creates a new Opus encoder with the given configuration.
// This is the pure-Go fallback that passes PCM as-is. Build with -tags opus_cgo
// for real Opus compression.
func NewEncoder(cfg EncoderConfig) (*Encoder, error) {
	if cfg.SampleRate != SampleRate {
		return nil, fmt.Errorf("sample rate must be %d, got %d", SampleRate, cfg.SampleRate)
	}
	if cfg.Channels != 1 && cfg.Channels != 2 {
		return nil, fmt.Errorf("channels must be 1 or 2, got %d", cfg.Channels)
	}
	switch cfg.FrameMs {
	case 5, 10, 20, 40, 60:
	default:
		return nil, fmt.Errorf("frame duration must be 5/10/20/40/60 ms, got %d", cfg.FrameMs)
	}

	return &Encoder{
		config:       cfg,
		frameSamples: FrameSamples(cfg.FrameMs),
	}, nil
}

// Encode compresses a PCM float32 frame into an Opus packet.
// pcm must contain exactly FrameSamples() * Channels samples.
//
// Fallback implementation: converts float32 PCM to int16 LE bytes.
// This is uncompressed but exercises the entire packet pipeline correctly.
func (e *Encoder) Encode(pcm []float32) ([]byte, error) {
	expected := e.frameSamples * e.config.Channels
	if len(pcm) != expected {
		return nil, fmt.Errorf("expected %d samples, got %d", expected, len(pcm))
	}

	// Fallback: encode as int16 little-endian PCM
	buf := make([]byte, len(pcm)*2)
	for i, sample := range pcm {
		// Clamp to [-1.0, 1.0] then scale to int16
		if sample > 1.0 {
			sample = 1.0
		} else if sample < -1.0 {
			sample = -1.0
		}
		s16 := int16(sample * math.MaxInt16)
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(s16))
	}

	return buf, nil
}

// FrameSamples returns the number of samples per channel for the current frame size.
func (e *Encoder) FrameSamples() int {
	return e.frameSamples
}

// FrameMs returns the current frame duration in milliseconds.
func (e *Encoder) FrameMs() int {
	return e.config.FrameMs
}

// SetFrameMs changes the encoder's frame duration. Used for iOS background fallback.
func (e *Encoder) SetFrameMs(ms int) error {
	switch ms {
	case 5, 10, 20, 40, 60:
	default:
		return fmt.Errorf("frame duration must be 5/10/20/40/60 ms, got %d", ms)
	}
	e.config.FrameMs = ms
	e.frameSamples = FrameSamples(ms)
	return nil
}

// Config returns the current encoder configuration.
func (e *Encoder) Config() EncoderConfig {
	return e.config
}

// Decoder decompresses Opus frames back into PCM audio.
type Decoder struct {
	sampleRate int
	channels   int
}

// NewDecoder creates a new Opus decoder.
// Fallback implementation: decodes int16 LE back to float32.
func NewDecoder(sampleRate, channels int) (*Decoder, error) {
	if sampleRate != SampleRate {
		return nil, fmt.Errorf("sample rate must be %d, got %d", SampleRate, sampleRate)
	}
	if channels != 1 && channels != 2 {
		return nil, fmt.Errorf("channels must be 1 or 2, got %d", channels)
	}
	return &Decoder{
		sampleRate: sampleRate,
		channels:   channels,
	}, nil
}

// Decode decompresses an Opus packet into PCM float32 samples.
// pcm must be large enough to hold the decoded frame.
// Returns the number of samples per channel decoded.
func (d *Decoder) Decode(data []byte, pcm []float32) (int, error) {
	if len(data)%2 != 0 {
		return 0, fmt.Errorf("data length must be even for int16 decoding, got %d", len(data))
	}
	samples := len(data) / 2
	if len(pcm) < samples {
		return 0, fmt.Errorf("pcm buffer too small: need %d, have %d", samples, len(pcm))
	}

	for i := 0; i < samples; i++ {
		s16 := int16(binary.LittleEndian.Uint16(data[i*2:]))
		pcm[i] = float32(s16) / float32(math.MaxInt16)
	}

	return samples / d.channels, nil
}

// DecodePLC performs packet loss concealment, generating fill audio when
// a packet is missing. This prevents audible clicks/pops.
//
// Fallback implementation: fills with silence.
func (d *Decoder) DecodePLC(pcm []float32) (int, error) {
	for i := range pcm {
		pcm[i] = 0
	}
	return len(pcm) / d.channels, nil
}
