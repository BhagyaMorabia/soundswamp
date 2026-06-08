// Package protocol defines the SoundSwarm wire format for UDP audio packets
// and TCP control messages.
//
// UDP Packet Layout (17-byte header + variable payload):
//
//	┌──────────┬──────────┬──────────────┬──────────────────┐
//	│ Ver (1B) │ Type(1B) │ SeqNum (4B)  │ Timestamp (8B µs)│
//	├──────────┴──────────┼──────────────┼──────────────────┤
//	│ ChanMask (1B)       │ PayloadLen(2B)│ Payload (var)   │
//	└─────────────────────┴──────────────┴──────────────────┘
package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	// ProtocolVersion is the current wire protocol version.
	ProtocolVersion uint8 = 1

	// HeaderSize is the fixed-size UDP packet header in bytes.
	HeaderSize = 17

	// MaxPayloadSize caps a single Opus frame to prevent amplification attacks.
	// Opus at 512kbps with 60ms frames produces ~3840 bytes. 4096 is generous.
	MaxPayloadSize = 4096

	// MaxPacketSize is the largest valid UDP packet we accept.
	MaxPacketSize = HeaderSize + MaxPayloadSize
)

// PacketType identifies the purpose of a UDP packet.
type PacketType uint8

const (
	PacketTypeAudio          PacketType = 0x01
	PacketTypeClockSyncProbe PacketType = 0x02
	PacketTypeClockSyncReply PacketType = 0x03
)

// ChannelMask identifies which surround channel an audio packet carries.
type ChannelMask uint8

const (
	ChannelStereoMix    ChannelMask = 0xFF
	ChannelFrontLeft    ChannelMask = 0x00
	ChannelFrontRight   ChannelMask = 0x01
	ChannelCenter       ChannelMask = 0x02
	ChannelLFE          ChannelMask = 0x03
	ChannelSurroundLeft ChannelMask = 0x04
	ChannelSurroundRight ChannelMask = 0x05
	ChannelBackLeft     ChannelMask = 0x06 // 7.1 extension
	ChannelBackRight    ChannelMask = 0x07 // 7.1 extension
)

// ChannelName returns a human-readable name for a channel mask.
func (c ChannelMask) String() string {
	switch c {
	case ChannelStereoMix:
		return "Stereo Mix"
	case ChannelFrontLeft:
		return "Front Left"
	case ChannelFrontRight:
		return "Front Right"
	case ChannelCenter:
		return "Center"
	case ChannelLFE:
		return "LFE"
	case ChannelSurroundLeft:
		return "Surround Left"
	case ChannelSurroundRight:
		return "Surround Right"
	case ChannelBackLeft:
		return "Back Left"
	case ChannelBackRight:
		return "Back Right"
	default:
		return fmt.Sprintf("Unknown(0x%02X)", uint8(c))
	}
}

// Packet represents a fully parsed SoundSwarm UDP packet.
type Packet struct {
	Version     uint8
	Type        PacketType
	SeqNum      uint32
	TimestampUs int64       // server capture timestamp in microseconds since epoch
	ChannelMask ChannelMask
	Payload     []byte
}

var (
	ErrPacketTooShort  = errors.New("packet shorter than header size")
	ErrBadVersion      = errors.New("unsupported protocol version")
	ErrPayloadTooLarge = errors.New("payload exceeds maximum size")
	ErrPayloadMismatch = errors.New("declared payload length does not match actual data")
)

// Marshal serializes a Packet into a byte slice ready for UDP transmission.
// The returned slice is newly allocated; the caller owns it.
func (p *Packet) Marshal() ([]byte, error) {
	if len(p.Payload) > MaxPayloadSize {
		return nil, ErrPayloadTooLarge
	}

	buf := make([]byte, HeaderSize+len(p.Payload))

	buf[0] = p.Version
	buf[1] = uint8(p.Type)
	binary.BigEndian.PutUint32(buf[2:6], p.SeqNum)
	binary.BigEndian.PutUint64(buf[6:14], uint64(p.TimestampUs))
	buf[14] = uint8(p.ChannelMask)
	binary.BigEndian.PutUint16(buf[15:17], uint16(len(p.Payload)))

	copy(buf[HeaderSize:], p.Payload)
	return buf, nil
}

// MarshalInto writes the packet into a pre-allocated buffer to avoid allocation
// on the hot path. Returns the number of bytes written.
func (p *Packet) MarshalInto(buf []byte) (int, error) {
	needed := HeaderSize + len(p.Payload)
	if len(buf) < needed {
		return 0, fmt.Errorf("buffer too small: need %d, have %d", needed, len(buf))
	}
	if len(p.Payload) > MaxPayloadSize {
		return 0, ErrPayloadTooLarge
	}

	buf[0] = p.Version
	buf[1] = uint8(p.Type)
	binary.BigEndian.PutUint32(buf[2:6], p.SeqNum)
	binary.BigEndian.PutUint64(buf[6:14], uint64(p.TimestampUs))
	buf[14] = uint8(p.ChannelMask)
	binary.BigEndian.PutUint16(buf[15:17], uint16(len(p.Payload)))
	copy(buf[HeaderSize:], p.Payload)

	return needed, nil
}

// Unmarshal parses raw UDP bytes into a Packet. The Payload field references
// a sub-slice of data; callers must copy it if they need to retain it beyond
// the lifetime of data.
func Unmarshal(data []byte) (*Packet, error) {
	if len(data) < HeaderSize {
		return nil, ErrPacketTooShort
	}

	ver := data[0]
	if ver != ProtocolVersion {
		return nil, fmt.Errorf("%w: got %d, want %d", ErrBadVersion, ver, ProtocolVersion)
	}

	payloadLen := int(binary.BigEndian.Uint16(data[15:17]))
	if payloadLen > MaxPayloadSize {
		return nil, ErrPayloadTooLarge
	}
	if len(data) < HeaderSize+payloadLen {
		return nil, fmt.Errorf("%w: header says %d bytes, packet has %d after header",
			ErrPayloadMismatch, payloadLen, len(data)-HeaderSize)
	}

	return &Packet{
		Version:     ver,
		Type:        PacketType(data[1]),
		SeqNum:      binary.BigEndian.Uint32(data[2:6]),
		TimestampUs: int64(binary.BigEndian.Uint64(data[6:14])),
		ChannelMask: ChannelMask(data[14]),
		Payload:     data[HeaderSize : HeaderSize+payloadLen],
	}, nil
}

// -------------------------------------------------------------------
// TCP Control Messages
// -------------------------------------------------------------------
// TCP messages use a 4-byte big-endian length prefix followed by a JSON body.
// All JSON messages contain a "type" field.

// TCPMessageType enumerates all control message types.
type TCPMessageType string

const (
	// Client → Server
	MsgJoinRequest    TCPMessageType = "JOIN_REQUEST"
	MsgHeartbeat      TCPMessageType = "HEARTBEAT"
	MsgJitterReport   TCPMessageType = "JITTER_REPORT"
	MsgClockSyncReply TCPMessageType = "CLOCK_SYNC_REPLY"
	MsgAppStateChange TCPMessageType = "APP_STATE_CHANGE"

	// Server → Client
	MsgJoinAccept       TCPMessageType = "JOIN_ACCEPT"
	MsgJoinReject       TCPMessageType = "JOIN_REJECT"
	MsgSetGlobalLatency TCPMessageType = "SET_GLOBAL_LATENCY"
	MsgClockSyncProbe   TCPMessageType = "CLOCK_SYNC_PROBE"
	MsgClockOffset      TCPMessageType = "CLOCK_OFFSET"
	MsgChannelAssign    TCPMessageType = "CHANNEL_ASSIGN"
	MsgSetFrameSize     TCPMessageType = "SET_FRAME_SIZE"
	MsgClientList       TCPMessageType = "CLIENT_LIST"
	MsgSessionEnded     TCPMessageType = "SESSION_ENDED"
)

// TCPMessage is the envelope for all TCP control messages.
type TCPMessage struct {
	Type TCPMessageType `json:"type"`
}

// --- Client → Server messages ---

type JoinRequestMsg struct {
	Type       TCPMessageType `json:"type"`
	Token      string         `json:"token"`
	DeviceName string         `json:"device_name"`
	Platform   string         `json:"platform"` // "android", "ios", "windows", "macos", "linux", "test"
	UDPPort    int            `json:"udp_port"`
}

type HeartbeatMsg struct {
	Type TCPMessageType `json:"type"`
}

type JitterReportMsg struct {
	Type  TCPMessageType `json:"type"`
	P95Ms float64        `json:"p95_ms"`
}

type ClockSyncReplyMsg struct {
	Type         TCPMessageType `json:"type"`
	ServerSendTs int64          `json:"server_send_ts"` // echoed back
	ClientRecvTs int64          `json:"client_recv_ts"`
	ClientSendTs int64          `json:"client_send_ts"`
}

type AppStateChangeMsg struct {
	Type  TCPMessageType `json:"type"`
	State string         `json:"state"` // "background" or "foreground"
}

// --- Server → Client messages ---

type JoinAcceptMsg struct {
	Type      TCPMessageType `json:"type"`
	ClientID  string         `json:"client_id"`
	SessionID string         `json:"session_id"`
	UDPPort   int            `json:"udp_port"`
}

type JoinRejectMsg struct {
	Type   TCPMessageType `json:"type"`
	Reason string         `json:"reason"`
}

type SetGlobalLatencyMsg struct {
	Type     TCPMessageType `json:"type"`
	TargetMs float64        `json:"target_ms"`
}

type ClockSyncProbeMsg struct {
	Type         TCPMessageType `json:"type"`
	ServerSendTs int64          `json:"server_send_ts"`
}

type ClockOffsetMsg struct {
	Type     TCPMessageType `json:"type"`
	OffsetUs int64          `json:"offset_us"` // server_time - client_time
}

type ChannelAssignMsg struct {
	Type    TCPMessageType `json:"type"`
	Channel ChannelMask    `json:"channel"`
}

type SetFrameSizeMsg struct {
	Type    TCPMessageType `json:"type"`
	FrameMs int            `json:"frame_ms"`
}

type ClientInfo struct {
	ID            string      `json:"id"`
	Name          string      `json:"name"`
	Platform      string      `json:"platform"`
	Channel       ChannelMask `json:"channel"`
	JitterMs      float64     `json:"jitter_ms"`
	Connected     bool        `json:"connected"`
}

type ClientListMsg struct {
	Type    TCPMessageType `json:"type"`
	Clients []ClientInfo   `json:"clients"`
}

type SessionEndedMsg struct {
	Type TCPMessageType `json:"type"`
}

// EncodeTCPMessage prepends a 4-byte big-endian length prefix to a JSON payload.
func EncodeTCPMessage(jsonPayload []byte) []byte {
	msg := make([]byte, 4+len(jsonPayload))
	binary.BigEndian.PutUint32(msg[:4], uint32(len(jsonPayload)))
	copy(msg[4:], jsonPayload)
	return msg
}

// DecodeTCPLength reads the 4-byte length prefix from a TCP stream header.
func DecodeTCPLength(header []byte) uint32 {
	return binary.BigEndian.Uint32(header[:4])
}
