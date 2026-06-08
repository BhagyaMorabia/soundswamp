// Package discovery — mDNS service registration for automatic device discovery.
package discovery

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/grandcat/zeroconf"
)

const (
	// ServiceType is the mDNS service type for SoundSwarm.
	ServiceType = "_soundswarm._tcp"

	// ServiceDomain is the mDNS domain.
	ServiceDomain = "local."
)

// MDNSServer registers the SoundSwarm service for automatic discovery on the local network.
type MDNSServer struct {
	server *zeroconf.Server
	logger *slog.Logger
}

// NewMDNSServer creates and registers a new mDNS service advertisement.
func NewMDNSServer(port int, sessionID string, logger *slog.Logger) (*MDNSServer, error) {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "soundswarm-server"
	}

	// TXT records carry session metadata
	txtRecords := []string{
		fmt.Sprintf("session=%s", sessionID),
		"version=4",
	}

	server, err := zeroconf.Register(
		hostname,     // instance name
		ServiceType,  // service type
		ServiceDomain, // domain
		port,         // port
		txtRecords,   // TXT records
		nil,          // interfaces (nil = all)
	)
	if err != nil {
		return nil, fmt.Errorf("register mDNS service: %w", err)
	}

	logger.Info("mDNS service registered",
		"service", ServiceType,
		"port", port,
		"session", sessionID,
		"hostname", hostname,
	)

	return &MDNSServer{
		server: server,
		logger: logger,
	}, nil
}

// Stop unregisters the mDNS service.
func (m *MDNSServer) Stop() {
	if m.server != nil {
		m.server.Shutdown()
		m.logger.Info("mDNS service stopped")
	}
}

// DiscoverServers scans the local network for SoundSwarm servers.
// This is used by clients (including the test client) to find the server.
func DiscoverServers(ctx context.Context) ([]*zeroconf.ServiceEntry, error) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return nil, fmt.Errorf("create resolver: %w", err)
	}

	entries := make(chan *zeroconf.ServiceEntry, 10)
	var results []*zeroconf.ServiceEntry

	go func() {
		for entry := range entries {
			results = append(results, entry)
		}
	}()

	err = resolver.Browse(ctx, ServiceType, ServiceDomain, entries)
	if err != nil {
		return nil, fmt.Errorf("browse: %w", err)
	}

	<-ctx.Done()
	return results, nil
}
