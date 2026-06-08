// Package surround implements PCM channel demuxing for surround sound audio.
//
// When WASAPI/BlackHole captures multi-channel audio (5.1, 7.1), the samples
// arrive interleaved: [FL0, FR0, C0, LFE0, SL0, SR0, FL1, FR1, ...].
// This package splits them into per-channel buffers for independent streaming
// to assigned devices.
//
// This is the Fix D simplification: raw PCM array index splitting instead of
// FFmpeg for the standard loopback capture path. FFmpeg is reserved for
// direct-file streaming mode (Phase 4+) where the OS mixer may have
// downmixed to stereo.
package surround

import (
	"fmt"

	"github.com/soundswarm/soundswarm/internal/protocol"
)

// ChannelLayout defines the standard channel ordering for a given speaker configuration.
type ChannelLayout struct {
	Name     string
	Channels int
	Mapping  []protocol.ChannelMask
}

// Standard channel layouts matching WASAPI/CoreAudio channel ordering.
var (
	LayoutStereo = ChannelLayout{
		Name:     "Stereo",
		Channels: 2,
		Mapping: []protocol.ChannelMask{
			protocol.ChannelFrontLeft,
			protocol.ChannelFrontRight,
		},
	}

	Layout51 = ChannelLayout{
		Name:     "5.1 Surround",
		Channels: 6,
		Mapping: []protocol.ChannelMask{
			protocol.ChannelFrontLeft,
			protocol.ChannelFrontRight,
			protocol.ChannelCenter,
			protocol.ChannelLFE,
			protocol.ChannelSurroundLeft,
			protocol.ChannelSurroundRight,
		},
	}

	Layout71 = ChannelLayout{
		Name:     "7.1 Surround",
		Channels: 8,
		Mapping: []protocol.ChannelMask{
			protocol.ChannelFrontLeft,
			protocol.ChannelFrontRight,
			protocol.ChannelCenter,
			protocol.ChannelLFE,
			protocol.ChannelBackLeft,
			protocol.ChannelBackRight,
			protocol.ChannelSurroundLeft,
			protocol.ChannelSurroundRight,
		},
	}
)

// LayoutForChannels returns the standard layout for a given channel count.
func LayoutForChannels(channels int) (ChannelLayout, error) {
	switch channels {
	case 2:
		return LayoutStereo, nil
	case 6:
		return Layout51, nil
	case 8:
		return Layout71, nil
	default:
		return ChannelLayout{}, fmt.Errorf("unsupported channel count: %d", channels)
	}
}

// Demuxer splits interleaved multi-channel PCM into per-channel buffers.
type Demuxer struct {
	layout     ChannelLayout
	perChannel int // samples per channel per call
}

// NewDemuxer creates a new channel demuxer for the given layout.
func NewDemuxer(layout ChannelLayout) *Demuxer {
	return &Demuxer{
		layout: layout,
	}
}

// Demux splits interleaved PCM into per-channel mono buffers.
//
// Input:  interleaved float32 [FL0, FR0, C0, LFE0, SL0, SR0, FL1, FR1, ...]
// Output: [][]float32 where output[i] is the mono buffer for channel i
//
// The input length must be a multiple of the channel count.
func (d *Demuxer) Demux(interleaved []float32) ([][]float32, error) {
	channels := d.layout.Channels
	if len(interleaved)%channels != 0 {
		return nil, fmt.Errorf("input length %d is not a multiple of channel count %d",
			len(interleaved), channels)
	}

	perChannel := len(interleaved) / channels
	output := make([][]float32, channels)
	for ch := 0; ch < channels; ch++ {
		output[ch] = make([]float32, perChannel)
	}

	for i := 0; i < len(interleaved); i += channels {
		sample := i / channels
		for ch := 0; ch < channels; ch++ {
			output[ch][sample] = interleaved[i+ch]
		}
	}

	return output, nil
}

// DemuxInto writes demuxed audio into pre-allocated buffers to avoid allocation.
// Each buffer in output must have length >= len(interleaved) / channels.
func (d *Demuxer) DemuxInto(interleaved []float32, output [][]float32) error {
	channels := d.layout.Channels
	if len(interleaved)%channels != 0 {
		return fmt.Errorf("input length %d is not a multiple of channel count %d",
			len(interleaved), channels)
	}
	if len(output) < channels {
		return fmt.Errorf("output has %d buffers, need %d", len(output), channels)
	}

	perChannel := len(interleaved) / channels
	for ch := 0; ch < channels; ch++ {
		if len(output[ch]) < perChannel {
			return fmt.Errorf("output[%d] has %d samples, need %d", ch, len(output[ch]), perChannel)
		}
	}

	for i := 0; i < len(interleaved); i += channels {
		sample := i / channels
		for ch := 0; ch < channels; ch++ {
			output[ch][sample] = interleaved[i+ch]
		}
	}

	return nil
}

// DownmixToStereo converts multi-channel PCM to stereo using ITU-R BS.775 coefficients.
// This is used when a client is assigned the stereo mix channel in surround mode.
func DownmixToStereo(channels [][]float32, layout ChannelLayout) ([]float32, error) {
	if len(channels) != layout.Channels {
		return nil, fmt.Errorf("expected %d channels, got %d", layout.Channels, len(channels))
	}

	if layout.Channels == 2 {
		// Already stereo — interleave directly
		n := len(channels[0])
		out := make([]float32, n*2)
		for i := 0; i < n; i++ {
			out[i*2] = channels[0][i]
			out[i*2+1] = channels[1][i]
		}
		return out, nil
	}

	// 5.1 downmix: ITU-R BS.775
	// L_out = FL + 0.707*C + 0.707*SL
	// R_out = FR + 0.707*C + 0.707*SR
	// LFE is typically discarded in downmix (phones can't reproduce it usefully)
	const centerMix = 0.707
	const surroundMix = 0.707

	n := len(channels[0])
	out := make([]float32, n*2)

	for i := 0; i < n; i++ {
		var fl, fr, c, sl, sr float32

		fl = channels[0][i]
		fr = channels[1][i]
		if layout.Channels >= 3 {
			c = channels[2][i]
		}
		// channels[3] = LFE, skipped
		if layout.Channels >= 5 {
			sl = channels[4][i]
		}
		if layout.Channels >= 6 {
			sr = channels[5][i]
		}

		out[i*2] = fl + centerMix*c + surroundMix*sl
		out[i*2+1] = fr + centerMix*c + surroundMix*sr
	}

	return out, nil
}

// Layout returns the demuxer's channel layout.
func (d *Demuxer) Layout() ChannelLayout {
	return d.layout
}
