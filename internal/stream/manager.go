// Package stream manages UDP audio streaming to all connected clients.
// It receives encoded Opus frames from the capture→encode pipeline and
// distributes them to each client with proper timestamps and channel assignments.
package stream

import (
	"log/slog"
	"net"
	gosync "sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/ipv4"

	"github.com/soundswarm/soundswarm/internal/protocol"
	ssync "github.com/soundswarm/soundswarm/internal/sync"
)

var payloadPool = gosync.Pool{
	New: func() interface{} {
		b := make([]byte, protocol.MaxPayloadSize)
		return &b
	},
}

// ClientStream holds per-client streaming state.
type ClientStream struct {
	ID            string
	Addr          *net.UDPAddr
	SeqNum        uint32
	ChannelAssign protocol.ChannelMask
	FrameSize     int  // current Opus frame size in ms
	Active        bool
}

// broadcastPacket encapsulates a frame for asynchronous transmission.
type broadcastPacket struct {
	opusData         []byte
	channel          protocol.ChannelMask
	captureTimestamp int64
	codecFlag        protocol.CodecFlag
}

// Manager handles UDP audio distribution to all connected clients.
type Manager struct {
	conn      *net.UDPConn
	clients   map[string]*ClientStream
	mu        gosync.RWMutex
	logger    *slog.Logger
	running   atomic.Bool
	stopChan  chan struct{}

	// Channel for decoupling audio capture from network I/O
	broadcastChan chan broadcastPacket

	// Packet buffer pool to reduce allocation on the hot path
	packetBuf []byte

	// Stats
	totalPacketsSent atomic.Int64
	totalBytesSent   atomic.Int64
	
	// Pacing
	lastSendUnixNano atomic.Int64
}

// NewManager creates a new stream manager bound to the given UDP port.
func NewManager(port int, logger *slog.Logger) (*Manager, error) {
	addr := &net.UDPAddr{
		IP:   net.IPv4zero,
		Port: port,
	}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return nil, err
	}

	// Set write buffer to 2MB for burst capacity
	conn.SetWriteBuffer(2 * 1024 * 1024)

	// Enable QoS / DSCP Expedited Forwarding (0xB8)
	if p4 := ipv4.NewConn(conn); p4 != nil {
		_ = p4.SetTOS(0xB8)
	}

	m := &Manager{
		conn:          conn,
		clients:       make(map[string]*ClientStream),
		logger:        logger,
		stopChan:      make(chan struct{}),
		broadcastChan: make(chan broadcastPacket, 200), // Buffer for 200 frames (2 seconds)
		packetBuf:     make([]byte, protocol.MaxPacketSize),
	}
	
	go m.broadcastLoop()
	return m, nil
}

func (m *Manager) broadcastLoop() {
	for {
		select {
		case <-m.stopChan:
			return
		case pkt := <-m.broadcastChan:
			m.doSendAudio(pkt.opusData, pkt.channel, pkt.captureTimestamp, pkt.codecFlag)
			
			// Return payload to pool
			if cap(pkt.opusData) == protocol.MaxPayloadSize {
				buf := pkt.opusData[:cap(pkt.opusData)]
				payloadPool.Put(&buf)
			}
		}
	}
}

// LocalAddr returns the local UDP address this manager is bound to.
func (m *Manager) LocalAddr() *net.UDPAddr {
	return m.conn.LocalAddr().(*net.UDPAddr)
}

// AddClient registers a new client for audio streaming.
func (m *Manager) AddClient(clientID string, addr *net.UDPAddr, channel protocol.ChannelMask) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.clients[clientID] = &ClientStream{
		ID:            clientID,
		Addr:          addr,
		SeqNum:        0,
		ChannelAssign: channel,
		FrameSize:     10,
		Active:        true,
	}

	m.logger.Info("stream client added",
		"client_id", clientID,
		"addr", addr.String(),
		"channel", channel.String(),
	)
}

// RemoveClient stops streaming to a client.
func (m *Manager) RemoveClient(clientID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.clients, clientID)
}

// SetChannel updates a client's surround channel assignment.
func (m *Manager) SetChannel(clientID string, channel protocol.ChannelMask) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cs, ok := m.clients[clientID]; ok {
		cs.ChannelAssign = channel
	}
}

// SetFrameSize updates a client's Opus frame size (for iOS background fallback).
func (m *Manager) SetFrameSize(clientID string, frameMs int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cs, ok := m.clients[clientID]; ok {
		cs.FrameSize = frameMs
	}
}

// SendAudio queues an encoded audio frame to all active clients.
func (m *Manager) SendAudio(opusData []byte, channel protocol.ChannelMask, captureTimestamp int64, codecFlag protocol.CodecFlag) {
	// Copy data using the zero-allocation pool
	ptr := payloadPool.Get().(*[]byte)
	dataCopy := (*ptr)[:len(opusData)]
	copy(dataCopy, opusData)

	select {
	case m.broadcastChan <- broadcastPacket{
		opusData:         dataCopy,
		channel:          channel,
		captureTimestamp: captureTimestamp,
		codecFlag:        codecFlag,
	}:
	default:
		m.logger.Warn("broadcast channel full, dropping audio frame")
	}
}

func (m *Manager) doSendAudio(opusData []byte, channel protocol.ChannelMask, captureTimestamp int64, codecFlag protocol.CodecFlag) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, cs := range m.clients {
		if !cs.Active {
			continue
		}

		// Only send this channel's data to clients assigned to it,
		// or send stereo mix to clients with ChannelStereoMix
		if channel != protocol.ChannelStereoMix &&
			cs.ChannelAssign != protocol.ChannelStereoMix &&
			cs.ChannelAssign != channel {
			continue
		}

		cs.SeqNum++
		pkt := protocol.Packet{
			Version:     protocol.ProtocolVersion,
			Type:        protocol.PacketTypeAudio,
			SeqNum:      cs.SeqNum,
			TimestampUs: captureTimestamp,
			ChannelMask: channel,
			Codec:       codecFlag,
			Payload:     opusData,
		}

		n, err := pkt.MarshalInto(m.packetBuf)
		if err != nil {
			m.logger.Error("failed to marshal packet",
				"client_id", cs.ID,
				"error", err,
			)
			continue
		}

		_, err = m.conn.WriteToUDP(m.packetBuf[:n], cs.Addr)
		if err != nil {
			m.logger.Error("failed to send packet",
				"client_id", cs.ID,
				"addr", cs.Addr.String(),
				"error", err,
			)
			continue
		}

		sent := m.totalPacketsSent.Add(1)
		m.totalBytesSent.Add(int64(n))
        
		if sent%100 == 0 {
			m.logger.Info("debug: sent 100 UDP packets", "client_id", cs.ID, "addr", cs.Addr.String(), "last_size", n)
		}
	}
}

// SendClockSyncProbe sends a clock sync probe to a specific client over UDP.
// This is separate from the TCP-based initial handshake — used for fast
// periodic corrections.
func (m *Manager) SendClockSyncProbe(clientID string) {
	m.mu.RLock()
	cs, exists := m.clients[clientID]
	m.mu.RUnlock()
	if !exists {
		return
	}

	pkt := protocol.Packet{
		Version:     protocol.ProtocolVersion,
		Type:        protocol.PacketTypeClockSyncProbe,
		SeqNum:      0,
		TimestampUs: ssync.ServerTimeNow(),
		ChannelMask: 0,
		Payload:     nil,
	}

	data, err := pkt.Marshal()
	if err != nil {
		return
	}

	m.conn.WriteToUDP(data, cs.Addr)
}

// StartReceiver starts a goroutine that reads incoming UDP packets from clients.
// This handles clock sync replies and client-initiated messages.
func (m *Manager) StartReceiver() {
	m.running.Store(true)
	go func() {
		buf := make([]byte, protocol.MaxPacketSize)
		for m.running.Load() {
			m.conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			n, remoteAddr, err := m.conn.ReadFromUDP(buf)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				if !m.running.Load() {
					return
				}
				m.logger.Error("UDP read error", "error", err)
				continue
			}

			pkt, err := protocol.Unmarshal(buf[:n])
			if err != nil {
				m.logger.Warn("invalid UDP packet",
					"addr", remoteAddr.String(),
					"error", err,
				)
				continue
			}

			switch pkt.Type {
			case protocol.PacketTypeClockSyncReply:
				// Process clock sync reply (handled by clock sync engine)
				_ = pkt // forwarded to clock sync via callback if needed

			default:
				m.logger.Warn("unexpected UDP packet type",
					"type", pkt.Type,
					"addr", remoteAddr.String(),
				)
			}
		}
	}()
}

// Stop shuts down the stream manager.
func (m *Manager) Stop() {
	m.running.Store(false)
	close(m.stopChan)
	m.conn.Close()
}

// Stats returns streaming statistics.
func (m *Manager) Stats() (packetsSent int64, bytesSent int64) {
	return m.totalPacketsSent.Load(), m.totalBytesSent.Load()
}

// ClientCount returns the number of active stream clients.
func (m *Manager) ClientCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.clients)
}
