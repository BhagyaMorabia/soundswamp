// Package session manages client connections, authentication, and session lifecycle
// for the SoundSwarm server.
package session

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"strings"
	gosync "sync"
	"time"

	"github.com/soundswarm/soundswarm/internal/protocol"
)

const (
	// SessionIDLength is the character count for human-readable session codes.
	SessionIDLength = 6

	// TokenLength is the byte length of the cryptographic session token.
	TokenLength = 32 // 256 bits

	// HeartbeatInterval is how often clients must send a heartbeat.
	HeartbeatInterval = 3 * time.Second

	// HeartbeatTimeout evicts clients after this duration without a heartbeat.
	HeartbeatTimeout = 10 * time.Second
)

// Role defines a client's function in the swarm.
type Role int

const (
	RoleHost    Role = iota // The laptop server
	RoleSpeaker             // A phone acting as a speaker
)

func (r Role) String() string {
	switch r {
	case RoleHost:
		return "host"
	case RoleSpeaker:
		return "speaker"
	default:
		return "unknown"
	}
}

// Client represents a connected device in the swarm.
type Client struct {
	ID            string
	Name          string
	Platform      string // "android", "ios", "windows", "test"
	Addr          *net.UDPAddr
	TCPConn       net.Conn
	Role          Role
	ChannelAssign protocol.ChannelMask
	JoinedAt      time.Time
	LastHeartbeat time.Time
	P95Jitter     float64
	FrameSize     int    // current Opus frame size in ms (10 or 40)
	AppState      string // "foreground" or "background"
	// WriteFunc is the mutex-protected TCP write function for this connection.
	// Set by clientReadLoop after the connection is established.
	WriteFunc func(msg interface{}) error
	mu        gosync.RWMutex
}

// UpdateHeartbeat records the current time as the last heartbeat.
func (c *Client) UpdateHeartbeat() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.LastHeartbeat = time.Now()
}

// IsStale returns true if the client hasn't sent a heartbeat within the timeout.
func (c *Client) IsStale() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return time.Since(c.LastHeartbeat) > HeartbeatTimeout
}

// SetJitter updates the client's P95 jitter measurement.
func (c *Client) SetJitter(p95Ms float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.P95Jitter = p95Ms
}

// SetChannel updates the client's surround channel assignment.
func (c *Client) SetChannel(ch protocol.ChannelMask) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ChannelAssign = ch
}

// SetFrameSize updates the client's Opus frame size.
func (c *Client) SetFrameSize(ms int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.FrameSize = ms
}

// SetAppState updates the client's foreground/background state.
func (c *Client) SetAppState(state string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.AppState = state
}

// SetWriteFunc stores the mutex-protected write function for this client's conn.
func (c *Client) SetWriteFunc(fn func(msg interface{}) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.WriteFunc = fn
}

// Send sends a message to this client using the safe write function.
// Returns an error if WriteFunc is not yet set (connection not ready).
func (c *Client) Send(msg interface{}) error {
	c.mu.RLock()
	fn := c.WriteFunc
	c.mu.RUnlock()
	if fn == nil {
		return nil
	}
	return fn(msg)
}

// Info returns a snapshot of the client's state for API responses.
func (c *Client) Info() protocol.ClientInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return protocol.ClientInfo{
		ID:       c.ID,
		Name:     c.Name,
		Platform: c.Platform,
		Channel:  c.ChannelAssign,
		JitterMs: c.P95Jitter,
		Connected: true,
	}
}

// Session represents an active SoundSwarm streaming session.
type Session struct {
	ID        string // human-readable 6-char code
	Token     string // 256-bit hex-encoded token
	CreatedAt time.Time
	Clients   map[string]*Client
	mu        gosync.RWMutex
	logger    *slog.Logger

	// Callbacks
	OnClientJoin   func(client *Client)
	OnClientLeave  func(clientID string)
}

// NewSession creates a new streaming session with a random ID and token.
func NewSession(logger *slog.Logger) (*Session, error) {
	id, err := generateSessionID()
	if err != nil {
		return nil, fmt.Errorf("generate session ID: %w", err)
	}

	token, err := generateToken()
	if err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}

	return &Session{
		ID:        id,
		Token:     token,
		CreatedAt: time.Now(),
		Clients:   make(map[string]*Client),
		logger:    logger,
	}, nil
}

// AddClient registers a new client in the session after token validation.
func (s *Session) AddClient(token, deviceName, platform string, tcpConn net.Conn) (*Client, error) {
	if token != s.Token {
		return nil, fmt.Errorf("invalid session token")
	}

	clientID, err := generateClientID()
	if err != nil {
		return nil, fmt.Errorf("generate client ID: %w", err)
	}

	client := &Client{
		ID:            clientID,
		Name:          deviceName,
		Platform:      platform,
		TCPConn:       tcpConn,
		Role:          RoleSpeaker,
		ChannelAssign: protocol.ChannelStereoMix,
		JoinedAt:      time.Now(),
		LastHeartbeat: time.Now(),
		FrameSize:     10,
		AppState:      "foreground",
	}

	s.mu.Lock()
	s.Clients[clientID] = client
	s.mu.Unlock()

	s.logger.Info("client joined",
		"client_id", clientID,
		"name", deviceName,
		"platform", platform,
	)

	if s.OnClientJoin != nil {
		s.OnClientJoin(client)
	}

	return client, nil
}

// RemoveClient disconnects and removes a client from the session.
func (s *Session) RemoveClient(clientID string) {
	s.mu.Lock()
	client, exists := s.Clients[clientID]
	if exists {
		delete(s.Clients, clientID)
	}
	s.mu.Unlock()

	if !exists {
		return
	}

	// Close TCP connection
	if client.TCPConn != nil {
		client.TCPConn.Close()
	}

	s.logger.Info("client removed",
		"client_id", clientID,
		"name", client.Name,
	)

	if s.OnClientLeave != nil {
		s.OnClientLeave(clientID)
	}
}

// GetClient returns a client by ID, or nil if not found.
func (s *Session) GetClient(clientID string) *Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Clients[clientID]
}

// ClientCount returns the number of connected clients.
func (s *Session) ClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.Clients)
}

// ClientList returns a snapshot of all connected clients' info.
func (s *Session) ClientList() []protocol.ClientInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]protocol.ClientInfo, 0, len(s.Clients))
	for _, c := range s.Clients {
		list = append(list, c.Info())
	}
	return list
}

// ForEachClient iterates over all clients with the session lock held.
func (s *Session) ForEachClient(fn func(client *Client)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.Clients {
		fn(c)
	}
}

// EvictStaleClients removes clients that haven't sent a heartbeat in time.
// Returns the list of evicted client IDs.
func (s *Session) EvictStaleClients() []string {
	s.mu.Lock()
	var stale []string
	for id, c := range s.Clients {
		if c.IsStale() {
			stale = append(stale, id)
		}
	}
	s.mu.Unlock()

	for _, id := range stale {
		s.RemoveClient(id)
	}
	return stale
}

// Manager coordinates session lifecycle and heartbeat monitoring.
type Manager struct {
	session  *Session
	mu       gosync.RWMutex
	logger   *slog.Logger
	stopChan chan struct{}
}

// NewManager creates a new session manager.
func NewManager(logger *slog.Logger) *Manager {
	return &Manager{
		logger:   logger,
		stopChan: make(chan struct{}),
	}
}

// CreateSession initializes a new session. Only one session is active at a time.
func (m *Manager) CreateSession() (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, err := NewSession(m.logger)
	if err != nil {
		return nil, err
	}
	m.session = session

	m.logger.Info("session created",
		"session_id", session.ID,
	)

	return session, nil
}

// GetSession returns the current active session.
func (m *Manager) GetSession() *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.session
}

// StartHeartbeatMonitor begins periodic stale client eviction.
func (m *Manager) StartHeartbeatMonitor() {
	go func() {
		ticker := time.NewTicker(HeartbeatTimeout / 2)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				session := m.GetSession()
				if session == nil {
					continue
				}
				evicted := session.EvictStaleClients()
				for _, id := range evicted {
					m.logger.Warn("client evicted (heartbeat timeout)", "client_id", id)
				}

			case <-m.stopChan:
				return
			}
		}
	}()
}

// Stop shuts down the session manager.
func (m *Manager) Stop() {
	close(m.stopChan)
}

// generateSessionID creates a random 6-character uppercase alphanumeric code.
func generateSessionID() (string, error) {
	const charset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // removed confusing chars: I/1/O/0
	buf := make([]byte, SessionIDLength)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = charset[int(buf[i])%len(charset)]
	}
	return string(buf), nil
}

// generateToken creates a 256-bit cryptographically random hex token.
func generateToken() (string, error) {
	buf := make([]byte, TokenLength)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// generateClientID creates a unique client identifier.
func generateClientID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "cli_" + hex.EncodeToString(buf), nil
}

// Ensure strings import is used
var _ = strings.TrimSpace
