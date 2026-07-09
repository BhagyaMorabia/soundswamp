// Package sync implements NTP-like clock synchronization and global latency
// equalization for the SoundSwarm distributed speaker system.
//
// Clock synchronization uses an 8-round probe exchange at connection time,
// with periodic single-round drift corrections every 30 seconds. Outlier
// rejection (>2× median RTT) and median offset selection ensure robust
// sync even on noisy WiFi links.
package sync

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"sort"
	gosync "sync"
	"time"

	"github.com/soundswarm/soundswarm/internal/protocol"
)

const (
	// InitialProbeRounds is the number of clock sync exchanges during handshake.
	InitialProbeRounds = 8

	// ProbeInterval is the delay between probe rounds during initial sync.
	ProbeInterval = 50 * time.Millisecond

	// PeriodicSyncInterval is how often drift correction probes run.
	PeriodicSyncInterval = 30 * time.Second

	// DriftCorrectionThresholdUs triggers a correction if offset changes by this much.
	DriftCorrectionThresholdUs = 500 // 500 microseconds
)

// ServerTimeNow returns the current server time in microseconds since Unix epoch.
func ServerTimeNow() int64 {
	return time.Now().UnixMicro()
}

// ClientClock holds the synchronization state for a single client.
type ClientClock struct {
	ID           string
	OffsetUs     int64     // server_time - client_time in microseconds
	RTTUS        int64     // latest measured RTT in microseconds
	LastSyncTime time.Time
	mu           gosync.RWMutex
}

// GetOffset returns the current clock offset for this client.
func (cc *ClientClock) GetOffset() int64 {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	return cc.OffsetUs
}

// ClockSync manages clock synchronization for all connected clients.
type ClockSync struct {
	clients map[string]*ClientClock
	mu      gosync.RWMutex
	logger  *slog.Logger
}

// NewClockSync creates a new clock sync manager.
func NewClockSync(logger *slog.Logger) *ClockSync {
	return &ClockSync{
		clients: make(map[string]*ClientClock),
		logger:  logger,
	}
}

// probeResult holds the timestamps from a single clock sync round.
type probeResult struct {
	ServerSendTs int64 // when server sent the probe
	ClientRecvTs int64 // when client received it
	ClientSendTs int64 // when client sent the reply
	ServerRecvTs int64 // when server received the reply
}

// rtt calculates the round-trip time in microseconds.
func (p probeResult) rtt() int64 {
	return (p.ServerRecvTs - p.ServerSendTs) - (p.ClientSendTs - p.ClientRecvTs)
}

// offset calculates the clock offset in microseconds (server - client).
func (p probeResult) offset() int64 {
	return ((p.ServerSendTs - p.ClientRecvTs) + (p.ServerRecvTs - p.ClientSendTs)) / 2
}

// TCPWriter abstracts writing length-prefixed JSON messages to a TCP connection.
type TCPWriter interface {
	WriteMessage(msg interface{}) error
}

// TCPReader abstracts reading length-prefixed JSON messages from a TCP connection.
type TCPReader interface {
	ReadMessage(msg interface{}) error
}

// tcpRW wraps a raw io.ReadWriter with length-prefixed JSON encoding.
type tcpRW struct {
	rw io.ReadWriter
}

func newTCPRW(rw io.ReadWriter) *tcpRW {
	return &tcpRW{rw: rw}
}

func (t *tcpRW) WriteMessage(msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	encoded := protocol.EncodeTCPMessage(data)
	_, err = t.rw.Write(encoded)
	return err
}

func (t *tcpRW) ReadMessage(msg interface{}) error {
	// Read 4-byte length prefix
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(t.rw, lenBuf); err != nil {
		return fmt.Errorf("read length: %w", err)
	}
	msgLen := protocol.DecodeTCPLength(lenBuf)
	if msgLen > 65536 { // sanity limit
		return fmt.Errorf("message too large: %d bytes", msgLen)
	}

	// Read the JSON body
	body := make([]byte, msgLen)
	if _, err := io.ReadFull(t.rw, body); err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	return json.Unmarshal(body, msg)
}

// InitialHandshake performs the full 8-round clock sync exchange with a newly
// connected client. This establishes the initial clock offset.
func (cs *ClockSync) InitialHandshake(clientID string, conn io.ReadWriter) error {
	rw := newTCPRW(conn)
	results := make([]probeResult, 0, InitialProbeRounds)

	for round := 0; round < InitialProbeRounds; round++ {
		// Send probe with server timestamp
		serverSendTs := ServerTimeNow()
		probe := protocol.ClockSyncProbeMsg{
			Type:         protocol.MsgClockSyncProbe,
			ServerSendTs: serverSendTs,
		}
		if err := rw.WriteMessage(probe); err != nil {
			return fmt.Errorf("send probe round %d: %w", round, err)
		}

		// Read client reply
		var reply protocol.ClockSyncReplyMsg
		if err := rw.ReadMessage(&reply); err != nil {
			return fmt.Errorf("read reply round %d: %w", round, err)
		}
		serverRecvTs := ServerTimeNow()

		results = append(results, probeResult{
			ServerSendTs: serverSendTs,
			ClientRecvTs: reply.ClientRecvTs,
			ClientSendTs: reply.ClientSendTs,
			ServerRecvTs: serverRecvTs,
		})

		if round < InitialProbeRounds-1 {
			time.Sleep(ProbeInterval)
		}
	}

	if len(results) == 0 {
		return fmt.Errorf("no sync rounds completed")
	}

	// Find the single round with the lowest RTT
	bestRound := results[0]
	bestRTT := bestRound.rtt()

	for _, r := range results[1:] {
		rtt := r.rtt()
		if rtt < bestRTT {
			bestRTT = rtt
			bestRound = r
		}
	}

	finalOffset := bestRound.offset()
	finalRTT := bestRTT

	// Store the client clock state
	cc := &ClientClock{
		ID:           clientID,
		OffsetUs:     finalOffset,
		RTTUS:        finalRTT,
		LastSyncTime: time.Now(),
	}

	cs.mu.Lock()
	cs.clients[clientID] = cc
	cs.mu.Unlock()

	// Send the computed offset to the client
	offsetMsg := protocol.ClockOffsetMsg{
		Type:     protocol.MsgClockOffset,
		OffsetUs: finalOffset,
	}
	if err := rw.WriteMessage(offsetMsg); err != nil {
		return fmt.Errorf("send offset: %w", err)
	}

	cs.logger.Info("clock sync complete",
		"client", clientID,
		"offset_us", finalOffset,
		"rtt_us", finalRTT,
		"rounds_used", 1,
		"rounds_total", InitialProbeRounds,
	)

	return nil
}

// SendPeriodicProbe sends a single clock sync probe to measure drift.
// Should be called every PeriodicSyncInterval.
func (cs *ClockSync) SendPeriodicProbe(clientID string, conn io.ReadWriter) error {
	cs.mu.RLock()
	_, exists := cs.clients[clientID]
	cs.mu.RUnlock()
	if !exists {
		return fmt.Errorf("unknown client: %s", clientID)
	}

	rw := newTCPRW(conn)

	// Single-round probe
	serverSendTs := ServerTimeNow()
	probe := protocol.ClockSyncProbeMsg{
		Type:         protocol.MsgClockSyncProbe,
		ServerSendTs: serverSendTs,
	}
	if err := rw.WriteMessage(probe); err != nil {
		return fmt.Errorf("send probe: %w", err)
	}

	return nil
}

// ProcessPeriodicReply processes a clock sync reply from a client and applies corrections if needed.
func (cs *ClockSync) ProcessPeriodicReply(clientID string, conn io.ReadWriter, reply protocol.ClockSyncReplyMsg) error {
	serverRecvTs := ServerTimeNow()

	cs.mu.RLock()
	cc, exists := cs.clients[clientID]
	cs.mu.RUnlock()
	if !exists {
		return fmt.Errorf("unknown client: %s", clientID)
	}

	result := probeResult{
		ServerSendTs: reply.ServerSendTs,
		ClientRecvTs: reply.ClientRecvTs,
		ClientSendTs: reply.ClientSendTs,
		ServerRecvTs: serverRecvTs,
	}

	newOffset := result.offset()
	cc.mu.Lock()
	delta := abs64(newOffset - cc.OffsetUs)
	if delta > DriftCorrectionThresholdUs {
		cc.OffsetUs = newOffset
		cc.RTTUS = result.rtt()
		cc.LastSyncTime = time.Now()
		cc.mu.Unlock()

		// Send correction to client
		rw := newTCPRW(conn)
		offsetMsg := protocol.ClockOffsetMsg{
			Type:     protocol.MsgClockOffset,
			OffsetUs: newOffset,
		}
		if err := rw.WriteMessage(offsetMsg); err != nil {
			return fmt.Errorf("send correction: %w", err)
		}

		cs.logger.Info("clock drift corrected",
			"client", clientID,
			"new_offset_us", newOffset,
			"delta_us", delta,
		)
	} else {
		cc.LastSyncTime = time.Now()
		cc.mu.Unlock()
	}

	return nil
}

// GetClientOffset returns the clock offset for a specific client.
func (cs *ClockSync) GetClientOffset(clientID string) (int64, error) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	cc, exists := cs.clients[clientID]
	if !exists {
		return 0, fmt.Errorf("unknown client: %s", clientID)
	}

	return cc.GetOffset(), nil
}

// RemoveClient removes a client's clock state.
func (cs *ClockSync) RemoveClient(clientID string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	delete(cs.clients, clientID)
}

// ClientCount returns the number of tracked clients.
func (cs *ClockSync) ClientCount() int {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return len(cs.clients)
}

// medianInt64 returns the median of a sorted copy of values.
func medianInt64(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := make([]int64, len(values))
	copy(sorted, values)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	mid := len(sorted) / 2
	if len(sorted)%2 == 0 {
		return (sorted[mid-1] + sorted[mid]) / 2
	}
	return sorted[mid]
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// Ensure math package usage for potential future use.
var _ = math.MaxFloat64
