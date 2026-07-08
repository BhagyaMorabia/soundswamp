// Package discovery provides device pairing via QR codes and mDNS.
package discovery

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"

	qrcode "github.com/skip2/go-qrcode"
)

// Payload is the data encoded in the QR code for device pairing.
type Payload struct {
	IP      string `json:"ip"`
	TCPPort int    `json:"tcp_port"`
	UDPPort int    `json:"udp_port"`
	Token   string `json:"token"`
	Session string `json:"session"`
}

// QRGenerator creates QR codes for device discovery.
type QRGenerator struct {
	logger *slog.Logger
}

// NewQRGenerator creates a new QR code generator.
func NewQRGenerator(logger *slog.Logger) *QRGenerator {
	return &QRGenerator{logger: logger}
}

// GeneratePNG creates a QR code PNG image encoding the discovery payload.
// Returns the PNG bytes.
func (g *QRGenerator) GeneratePNG(payload Payload, size int) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	png, err := qrcode.Encode(string(data), qrcode.Medium, size)
	if err != nil {
		return nil, fmt.Errorf("generate QR code: %w", err)
	}

	g.logger.Info("QR code generated",
		"session", payload.Session,
		"ip", payload.IP,
		"tcp_port", payload.TCPPort,
		"udp_port", payload.UDPPort,
	)

	return png, nil
}

// GetLocalIP detects the primary local network IP address,
// filtering out loopback and virtual adapter addresses.
func GetLocalIP() (string, error) {
	// Dial a public IP via UDP to force the OS to determine the preferred outbound interface.
	// This does not actually send any packets over the network.
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "", fmt.Errorf("failed to determine local ip: %w", err)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String(), nil
}

// GetLocalIPForInterface returns the IPv4 address of a specific network interface.
// Useful for binding to the hotspot interface specifically.
func GetLocalIPForInterface(ifaceName string) (string, error) {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return "", fmt.Errorf("interface %s not found: %w", ifaceName, err)
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return "", fmt.Errorf("get addrs for %s: %w", ifaceName, err)
	}

	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		if ip := ipNet.IP.To4(); ip != nil {
			return ip.String(), nil
		}
	}

	return "", fmt.Errorf("no IPv4 address on interface %s", ifaceName)
}
