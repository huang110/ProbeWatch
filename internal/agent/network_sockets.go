package agent

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/probewatch/probewatch/internal/protocol"
)

// SocketCollector defines the interface for collecting socket statistics and listening ports.
type SocketCollector interface {
	Collect() (*protocol.SocketStats, []protocol.ListeningPort, error)
}

// defaultSocketCollector returns the platform-specific socket collector.
func defaultSocketCollector() SocketCollector {
	return newPlatformSocketCollector()
}

// isPublicAddress checks whether a bound IP address is reachable from the public internet.
func isPublicAddress(ipStr string) bool {
	ipStr = strings.TrimSpace(ipStr)
	if ipStr == "0.0.0.0" || ipStr == "::" {
		return true
	}

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}

	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() {
		return false
	}

	return true
}

// parseNetHexAddr parses "IP:Port" hex string from /proc/net format.
func parseNetHexAddr(raw string, isIPv6 bool) (string, int, error) {
	parts := strings.SplitN(raw, ":", 2)
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("invalid address format: %s", raw)
	}

	port64, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil {
		return "", 0, fmt.Errorf("invalid port hex: %s", parts[1])
	}
	port := int(port64)

	hexIP := parts[0]
	if !isIPv6 {
		if len(hexIP) != 8 {
			return "", 0, fmt.Errorf("invalid ipv4 hex length: %d", len(hexIP))
		}
		n, err := strconv.ParseUint(hexIP, 16, 32)
		if err != nil {
			return "", 0, err
		}
		ip := net.IPv4(byte(n), byte(n>>8), byte(n>>16), byte(n>>24))
		return ip.String(), port, nil
	}

	// IPv6: 32 hex chars representing four 32-bit little-endian words
	if len(hexIP) != 32 {
		return "", 0, fmt.Errorf("invalid ipv6 hex length: %d", len(hexIP))
	}
	var ipBytes [16]byte
	for i := 0; i < 4; i++ {
		chunk := hexIP[i*8 : (i+1)*8]
		word, err := strconv.ParseUint(chunk, 16, 32)
		if err != nil {
			return "", 0, err
		}
		ipBytes[i*4] = byte(word)
		ipBytes[i*4+1] = byte(word >> 8)
		ipBytes[i*4+2] = byte(word >> 16)
		ipBytes[i*4+3] = byte(word >> 24)
	}
	return net.IP(ipBytes[:]).String(), port, nil
}
