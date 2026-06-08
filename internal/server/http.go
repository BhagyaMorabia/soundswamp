// Package server provides the HTTP and TCP servers for SoundSwarm.
//
// The HTTP server handles:
//   - Web UI serving (GET /)
//   - QR code endpoint (GET /api/qr)
//   - Session info API (GET /api/session)
//   - WebSocket for real-time UI updates (GET /api/ws)
//   - Captive portal spoofing (Fix C): /hotspot-detect.html and /generate_204
package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	gosync "sync"
	"time"

	"github.com/soundswarm/soundswarm/internal/discovery"
	"github.com/soundswarm/soundswarm/internal/protocol"
	"github.com/soundswarm/soundswarm/internal/session"
	"github.com/soundswarm/soundswarm/internal/stream"
	ssync "github.com/soundswarm/soundswarm/internal/sync"
	"github.com/soundswarm/soundswarm/web"
)

// WebSocketHub manages connected WebSocket clients for real-time UI updates.
type WebSocketHub struct {
	clients map[*wsConn]bool
	mu      gosync.RWMutex
}

type wsConn struct {
	conn   net.Conn
	send   chan []byte
	closed bool
}

func newWebSocketHub() *WebSocketHub {
	return &WebSocketHub{
		clients: make(map[*wsConn]bool),
	}
}

// Broadcast sends a JSON message to all connected WebSocket clients.
func (h *WebSocketHub) Broadcast(msg interface{}) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for client := range h.clients {
		select {
		case client.send <- data:
		default:
			// Client's send buffer is full, skip
		}
	}
}

// HTTPConfig holds configuration for the HTTP server.
type HTTPConfig struct {
	Port           int
	UDPPort        int
	HotspotMode    bool
	SessionManager *session.Manager
	StreamManager  *stream.Manager
	LatencyEq      *ssync.LatencyEqualizer
	QRGenerator    *discovery.QRGenerator
	Logger         *slog.Logger
}

// HTTPServer serves the web UI and API endpoints.
type HTTPServer struct {
	config   HTTPConfig
	server   *http.Server
	wsHub    *WebSocketHub
	logger   *slog.Logger
}

// NewHTTPServer creates a new HTTP server.
func NewHTTPServer(config HTTPConfig) *HTTPServer {
	s := &HTTPServer{
		config: config,
		wsHub:  newWebSocketHub(),
		logger: config.Logger,
	}

	mux := http.NewServeMux()

	// --- Static web UI ---
	mux.Handle("GET /", http.FileServer(http.FS(web.FS)))

	// --- API endpoints ---
	mux.HandleFunc("GET /api/qr", s.handleQR)
	mux.HandleFunc("GET /api/session", s.handleSession)
	mux.HandleFunc("GET /api/stats", s.handleStats)
	mux.HandleFunc("POST /api/channel", s.handleChannelAssign)
	mux.HandleFunc("POST /api/kick", s.handleKick)

	// --- Captive portal spoofing (Fix C) ---
	// Apple captive portal detection
	mux.HandleFunc("GET /hotspot-detect.html", s.handleAppleCaptivePortal)
	// Android captive portal detection
	mux.HandleFunc("GET /generate_204", s.handleAndroidCaptivePortal)
	// Additional Android captive portal URLs
	mux.HandleFunc("GET /gen_204", s.handleAndroidCaptivePortal)
	mux.HandleFunc("GET /connectivitycheck.gstatic.com/generate_204", s.handleAndroidCaptivePortal)

	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", config.Port),
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return s
}

// Start begins serving HTTP requests.
func (s *HTTPServer) Start() error {
	s.logger.Info("HTTP server starting", "port", s.config.Port)
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("HTTP server error", "error", err)
		}
	}()
	return nil
}

// Stop gracefully shuts down the HTTP server.
func (s *HTTPServer) Stop() error {
	return s.server.Close()
}

// BroadcastUpdate sends a state update to all connected WebSocket clients.
func (s *HTTPServer) BroadcastUpdate() {
	sess := s.config.SessionManager.GetSession()
	if sess == nil {
		return
	}

	update := map[string]interface{}{
		"type":          "STATE_UPDATE",
		"session_id":    sess.ID,
		"clients":       sess.ClientList(),
		"global_delay":  s.config.LatencyEq.GlobalDelay(),
		"client_count":  sess.ClientCount(),
	}

	s.wsHub.Broadcast(update)
}

// --- Handler implementations ---

func (s *HTTPServer) handleQR(w http.ResponseWriter, r *http.Request) {
	sess := s.config.SessionManager.GetSession()
	if sess == nil {
		http.Error(w, "no active session", http.StatusServiceUnavailable)
		return
	}

	localIP, err := discovery.GetLocalIP()
	if err != nil {
		http.Error(w, "failed to detect local IP", http.StatusInternalServerError)
		return
	}

	payload := discovery.Payload{
		IP:      localIP,
		TCPPort: s.config.Port + 1, // TCP control port = HTTP port + 1
		UDPPort: s.config.UDPPort,
		Token:   sess.Token,
		Session: sess.ID,
	}

	png, err := s.config.QRGenerator.GeneratePNG(payload, 512)
	if err != nil {
		http.Error(w, "failed to generate QR code", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Write(png)
}

func (s *HTTPServer) handleSession(w http.ResponseWriter, r *http.Request) {
	sess := s.config.SessionManager.GetSession()
	if sess == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"active": false,
		})
		return
	}

	packets, bytes := s.config.StreamManager.Stats()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"active":        true,
		"session_id":    sess.ID,
		"created_at":    sess.CreatedAt,
		"clients":       sess.ClientList(),
		"client_count":  sess.ClientCount(),
		"global_delay":  s.config.LatencyEq.GlobalDelay(),
		"packets_sent":  packets,
		"bytes_sent":    bytes,
	})
}

func (s *HTTPServer) handleStats(w http.ResponseWriter, r *http.Request) {
	packets, bytes := s.config.StreamManager.Stats()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"packets_sent":  packets,
		"bytes_sent":    bytes,
		"global_delay":  s.config.LatencyEq.GlobalDelay(),
		"stream_clients": s.config.StreamManager.ClientCount(),
	})
}

func (s *HTTPServer) handleChannelAssign(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClientID string `json:"client_id"`
		Channel  uint8  `json:"channel"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	sess := s.config.SessionManager.GetSession()
	if sess == nil {
		http.Error(w, "no active session", http.StatusServiceUnavailable)
		return
	}

	client := sess.GetClient(req.ClientID)
	if client == nil {
		http.Error(w, "client not found", http.StatusNotFound)
		return
	}

	// Update both session client and stream manager
	channel := protocol.ChannelMask(req.Channel)
	client.SetChannel(channel)
	s.config.StreamManager.SetChannel(req.ClientID, channel)

	s.BroadcastUpdate()
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *HTTPServer) handleKick(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClientID string `json:"client_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	sess := s.config.SessionManager.GetSession()
	if sess == nil {
		http.Error(w, "no active session", http.StatusServiceUnavailable)
		return
	}

	sess.RemoveClient(req.ClientID)
	s.config.StreamManager.RemoveClient(req.ClientID)

	s.BroadcastUpdate()
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Captive portal handlers (Fix C) ---

// handleAppleCaptivePortal responds to Apple's captive portal detection probe.
// Apple devices send GET /hotspot-detect.html to captive.apple.com.
// If the response contains "<HTML><HEAD><TITLE>Success</TITLE></HEAD><BODY>Success</BODY></HTML>",
// iOS considers the network to have internet access and stops showing the
// captive portal notification. The OS keeps WiFi as the default route.
func (s *HTTPServer) handleAppleCaptivePortal(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "<HTML><HEAD><TITLE>Success</TITLE></HEAD><BODY>Success</BODY></HTML>")
}

// handleAndroidCaptivePortal responds to Android's captive portal detection probe.
// Android sends GET /generate_204 to connectivitycheck.gstatic.com.
// Responding with HTTP 204 No Content tells Android the network has internet.
func (s *HTTPServer) handleAndroidCaptivePortal(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// writeJSON is a helper to write JSON responses.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// Port returns the configured HTTP port.
func (s *HTTPServer) Port() int {
	return s.config.Port
}

// WSHub returns the WebSocket hub for external broadcast access.
func (s *HTTPServer) WSHub() *WebSocketHub {
	return s.wsHub
}
