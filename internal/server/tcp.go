// Package server — TCP control server for client management.
//
// The TCP server handles the control plane: client authentication, clock sync,
// jitter reports, heartbeats, channel assignments, and all typed JSON messages
// defined in the protocol package.
//
// Each client connection runs in its own goroutine with a dedicated read loop.
// Messages are length-prefixed (4-byte big-endian) JSON.
package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	gosync "sync"
	"time"

	"github.com/soundswarm/soundswarm/internal/protocol"
	"github.com/soundswarm/soundswarm/internal/session"
	"github.com/soundswarm/soundswarm/internal/stream"
	ssync "github.com/soundswarm/soundswarm/internal/sync"
)

// TCPConfig holds configuration for the TCP control server.
type TCPConfig struct {
	Port           int
	SessionManager *session.Manager
	StreamManager  *stream.Manager
	ClockSync      *ssync.ClockSync
	LatencyEq      *ssync.LatencyEqualizer
	OnClientJoin   func(clientID string)
	OnClientLeave  func(clientID string)
	OnUIUpdate     func()
	Logger         *slog.Logger
}

// TCPServer handles the control plane for all client connections.
type TCPServer struct {
	config   TCPConfig
	listener net.Listener
	logger   *slog.Logger
	quit     chan struct{}
}

// NewTCPServer creates a new TCP control server.
func NewTCPServer(config TCPConfig) *TCPServer {
	return &TCPServer{
		config: config,
		logger: config.Logger,
		quit:   make(chan struct{}),
	}
}

// Start begins listening for TCP connections.
func (s *TCPServer) Start() error {
	listener, err := net.Listen("tcp4", fmt.Sprintf(":%d", s.config.Port))
	if err != nil {
		return fmt.Errorf("TCP listen on port %d: %w", s.config.Port, err)
	}
	s.listener = listener

	s.logger.Info("TCP control server started", "port", s.config.Port)

	go s.acceptLoop()
	return nil
}

// Stop shuts down the TCP server.
func (s *TCPServer) Stop() {
	close(s.quit)
	if s.listener != nil {
		s.listener.Close()
	}
}

// acceptLoop accepts incoming TCP connections.
func (s *TCPServer) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.quit:
				return
			default:
				s.logger.Error("TCP accept error", "error", err)
				continue
			}
		}

		go s.handleConnection(conn)
	}
}

// handleConnection manages a single client's TCP connection lifecycle.
func (s *TCPServer) handleConnection(conn net.Conn) {
	remoteAddr := conn.RemoteAddr().String()
	s.logger.Info("new TCP connection", "remote", remoteAddr)

	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(15 * time.Second)
	}

	defer conn.Close()

	// Step 1: Read the JOIN_REQUEST
	msg, err := readTCPMessage(conn)
	if err != nil {
		s.logger.Error("failed to read join request", "remote", remoteAddr, "error", err)
		return
	}

	var joinReq protocol.JoinRequestMsg
	if err := json.Unmarshal(msg, &joinReq); err != nil {
		s.logger.Error("invalid join request", "remote", remoteAddr, "error", err)
		return
	}

	if joinReq.Type != protocol.MsgJoinRequest {
		s.logger.Error("expected JOIN_REQUEST", "remote", remoteAddr, "got", joinReq.Type)
		return
	}

	// Step 2: Validate session token
	sess := s.config.SessionManager.GetSession()
	if sess == nil {
		sendTCPMessage(conn, protocol.JoinRejectMsg{
			Type:   protocol.MsgJoinReject,
			Reason: "no active session",
		})
		return
	}

	client, err := sess.AddClient(joinReq.Token, joinReq.DeviceName, joinReq.Platform, conn)
	if err != nil {
		s.logger.Warn("join rejected", "remote", remoteAddr, "error", err)
		sendTCPMessage(conn, protocol.JoinRejectMsg{
			Type:   protocol.MsgJoinReject,
			Reason: err.Error(),
		})
		return
	}

	// Step 3: Send JOIN_ACCEPT
	acceptMsg := protocol.JoinAcceptMsg{
		Type:      protocol.MsgJoinAccept,
		ClientID:  client.ID,
		SessionID: sess.ID,
		UDPPort:   s.config.StreamManager.LocalAddr().Port,
	}
	if err := sendTCPMessage(conn, acceptMsg); err != nil {
		s.logger.Error("failed to send join accept", "client", client.ID, "error", err)
		sess.RemoveClient(client.ID)
		return
	}

	s.logger.Info("client authenticated",
		"client_id", client.ID,
		"name", client.Name,
		"platform", client.Platform,
	)

	// Step 4: Perform clock synchronization handshake
	if err := s.config.ClockSync.InitialHandshake(client.ID, conn); err != nil {
		s.logger.Error("clock sync failed", "client", client.ID, "error", err)
		sess.RemoveClient(client.ID)
		return
	}

	// We use the UDP port the client explicitly sent us in the JSON payload.
	tcpAddr := conn.RemoteAddr().(*net.TCPAddr)
	udpAddr := &net.UDPAddr{
		IP:   tcpAddr.IP,
		Port: joinReq.UDPPort,
	}
	s.config.StreamManager.AddClient(client.ID, udpAddr, client.ChannelAssign)

	// Send the current global latency
	latencyMsg := protocol.SetGlobalLatencyMsg{
		Type:     protocol.MsgSetGlobalLatency,
		TargetMs: s.config.LatencyEq.GlobalDelay(),
	}
	sendTCPMessage(conn, latencyMsg)

	// Send current client list
	clientListMsg := protocol.ClientListMsg{
		Type:    protocol.MsgClientList,
		Clients: sess.ClientList(),
	}
	sendTCPMessage(conn, clientListMsg)

	// Notify UI
	if s.config.OnClientJoin != nil {
		s.config.OnClientJoin(client.ID)
	}
	if s.config.OnUIUpdate != nil {
		s.config.OnUIUpdate()
	}

	// Step 6: Enter the message read loop
	s.clientReadLoop(client, conn, sess)

	// Cleanup on disconnect
	s.logger.Info("client disconnected", "client_id", client.ID, "name", client.Name)
	sess.RemoveClient(client.ID)
	s.config.StreamManager.RemoveClient(client.ID)
	s.config.ClockSync.RemoveClient(client.ID)
	s.config.LatencyEq.RemoveClient(client.ID)

	if s.config.OnClientLeave != nil {
		s.config.OnClientLeave(client.ID)
	}
	if s.config.OnUIUpdate != nil {
		s.config.OnUIUpdate()
	}
}

// clientReadLoop processes incoming TCP messages from a connected client.
func (s *TCPServer) clientReadLoop(client *session.Client, conn net.Conn, sess *session.Session) {
	// F23 fix: guard all TCP writes to this conn behind a single mutex.
	// Two goroutines write to conn concurrently:
	//   (1) the periodic clock sync goroutine (syncTicker)
	//   (2) BroadcastToAll() called from HTTP handlers
	// net.Conn.Write is NOT goroutine-safe. Without this mutex, partial writes
	// can interleave and corrupt the 4-byte length prefix framing, causing the
	// C++ client to misread message lengths and crash.
	var writeMu gosync.Mutex
	safeWrite := func(msg interface{}) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return sendTCPMessage(conn, msg)
	}
	// Start periodic clock sync in background
	syncTicker := time.NewTicker(ssync.PeriodicSyncInterval)
	defer syncTicker.Stop()

	syncDone := make(chan struct{})
	go func() {
		defer close(syncDone)
		for {
			select {
			case <-syncTicker.C:
				if err := s.config.ClockSync.SendPeriodicProbe(client.ID, conn); err != nil {
					s.logger.Warn("periodic sync failed", "client", client.ID, "error", err)
				}
			case <-s.quit:
				return
			}
		}
	}()

	// Store safeWrite on the client so BroadcastToAll can use the same mutex.
	client.SetWriteFunc(safeWrite)

	defer func() {
		syncTicker.Stop()
		<-syncDone
	}()

	for {
		msg, err := readTCPMessage(conn)
		if err != nil {
			if err == io.EOF {
				return // clean disconnect
			}
			s.logger.Error("read error", "client", client.ID, "error", err)
			return
		}

		// Determine message type
		var envelope protocol.TCPMessage
		if err := json.Unmarshal(msg, &envelope); err != nil {
			s.logger.Warn("invalid message from client", "client", client.ID, "error", err)
			continue
		}

		switch envelope.Type {
		case protocol.MsgHeartbeat:
			client.UpdateHeartbeat()

		case protocol.MsgJitterReport:
			var report protocol.JitterReportMsg
			if err := json.Unmarshal(msg, &report); err != nil {
				s.logger.Warn("invalid jitter report", "client", client.ID, "error", err)
				continue
			}
			client.SetJitter(report.P95Ms)
			s.config.LatencyEq.ReportJitter(client.ID, report.P95Ms)

		case protocol.MsgClockSyncReply:
			var reply protocol.ClockSyncReplyMsg
			if err := json.Unmarshal(msg, &reply); err != nil {
				s.logger.Warn("invalid clock sync reply", "client", client.ID, "error", err)
				continue
			}
			if err := s.config.ClockSync.ProcessPeriodicReply(client.ID, conn, reply); err != nil {
				s.logger.Warn("failed to process clock sync reply", "client", client.ID, "error", err)
			}

		case protocol.MsgAppStateChange:
			var stateMsg protocol.AppStateChangeMsg
			if err := json.Unmarshal(msg, &stateMsg); err != nil {
				s.logger.Warn("invalid app state message", "client", client.ID, "error", err)
				continue
			}
			client.SetAppState(stateMsg.State)
			s.logger.Info("client app state changed",
				"client", client.ID,
				"state", stateMsg.State,
			)

		default:
			s.logger.Warn("unknown message type",
				"client", client.ID,
				"type", envelope.Type,
			)
		}
	}
}

// BroadcastToAll sends a TCP message to all connected clients.
// Uses each client's mutex-protected Send() method to avoid concurrent write
// races between this goroutine and the per-client periodic sync goroutine (F23 fix).
func (s *TCPServer) BroadcastToAll(msg interface{}) {
	sess := s.config.SessionManager.GetSession()
	if sess == nil {
		return
	}

	sess.ForEachClient(func(client *session.Client) {
		go func(c *session.Client) {
			if err := c.Send(msg); err != nil {
				s.logger.Warn("broadcast failed",
					"client", c.ID,
					"error", err,
				)
			}
		}(client)
	})
}

// --- TCP message helpers ---

// readTCPMessage reads a single length-prefixed JSON message from a TCP connection.
func readTCPMessage(conn net.Conn) ([]byte, error) {
	// Read 4-byte length prefix
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(conn, lenBuf); err != nil {
		return nil, err
	}

	msgLen := protocol.DecodeTCPLength(lenBuf)
	if msgLen > 65536 {
		return nil, fmt.Errorf("message too large: %d bytes", msgLen)
	}

	// Read the JSON body
	body := make([]byte, msgLen)
	if _, err := io.ReadFull(conn, body); err != nil {
		return nil, err
	}

	return body, nil
}

// sendTCPMessage writes a length-prefixed JSON message to a TCP connection.
func sendTCPMessage(conn net.Conn, msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	encoded := protocol.EncodeTCPMessage(data)
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	defer conn.SetWriteDeadline(time.Time{}) // Clear deadline after write
	_, err = conn.Write(encoded)
	return err
}

// Port returns the configured TCP port.
func (s *TCPServer) Port() int {
	return s.config.Port
}
