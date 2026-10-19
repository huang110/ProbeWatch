package backup

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/probewatch/probewatch/internal/security"
)

const (
	maxBackupEndpointLength = 4096
	maxBackupPathLength     = 2048
)

// parseBackupEndpoint validates the URL syntax without resolving it. The
// transport performs the actual public-address check immediately before each
// connection, which also prevents DNS rebinding from turning a safe hostname
// into a loopback/metadata request.
func parseBackupEndpoint(raw, defaultURL string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = defaultURL
	}
	if len(raw) == 0 || len(raw) > maxBackupEndpointLength {
		return nil, fmt.Errorf("backup endpoint is empty or too long")
	}
	for _, r := range raw {
		if unicode.IsControl(r) {
			return nil, fmt.Errorf("backup endpoint contains control characters")
		}
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	if strings.Contains(raw, "#") {
		return nil, fmt.Errorf("backup endpoint must not contain fragments")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, fmt.Errorf("backup endpoint must be an absolute HTTP(S) URL without credentials, query, or fragment")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("backup endpoint scheme is not allowed")
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("backup endpoint host is required")
	}
	// Backup requests carry database contents and, for authenticated remote
	// stores, credentials. Plain HTTP is acceptable only for an explicitly
	// local test endpoint; allowing arbitrary remote HTTP would silently send
	// those secrets in cleartext on a public VPS.
	if u.Scheme == "http" && !isLoopbackBackupHost(u.Hostname()) {
		return nil, fmt.Errorf("backup endpoint must use HTTPS unless it is loopback")
	}
	if strings.HasSuffix(u.Host, ":") {
		return nil, fmt.Errorf("invalid backup endpoint port")
	}
	if port := u.Port(); port != "" {
		value, portErr := strconv.Atoi(port)
		if portErr != nil || value < 1 || value > 65535 {
			return nil, fmt.Errorf("invalid backup endpoint port")
		}
	}
	if len(u.EscapedPath()) > maxBackupPathLength {
		return nil, fmt.Errorf("backup endpoint path is too long")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u, nil
}

func isLoopbackBackupHost(host string) bool {
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.IsLoopback()
}

func validateBackupObjectName(name string) error {
	if name == "" || strings.TrimSpace(name) != name || len(name) > maxBackupPathLength || name == "." || name == ".." || strings.ContainsAny(name, `/\\?#`) {
		return fmt.Errorf("invalid remote backup filename")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return fmt.Errorf("invalid remote backup filename")
		}
	}
	return nil
}

func validateBackupObjectKey(key string) error {
	if key == "" || strings.TrimSpace(key) != key || len(key) > maxBackupPathLength || strings.ContainsAny(key, `\?#`) {
		return fmt.Errorf("invalid remote backup object key")
	}
	for _, segment := range strings.Split(key, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("invalid remote backup object key")
		}
	}
	for _, r := range key {
		if unicode.IsControl(r) {
			return fmt.Errorf("invalid remote backup object key")
		}
	}
	return nil
}

func backupHTTPClient(allowPrivateNetwork bool) *http.Client {
	return &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			Proxy:                  nil,
			MaxResponseHeaderBytes: 32 << 10,
			TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12},
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				return backupDialContext(ctx, network, address, allowPrivateNetwork)
			},
		},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return fmt.Errorf("backup redirects are disabled")
		},
	}
}

func backupDialContext(ctx context.Context, network, address string, allowPrivateNetwork bool) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if allowPrivateNetwork {
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
	if literal, parseErr := netip.ParseAddr(host); parseErr == nil {
		if security.IsBlockedAddress(literal) {
			return nil, fmt.Errorf("backup target address is blocked: %s", literal)
		}
		return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(literal.String(), port))
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if err := security.ValidateResolvedIPs(addresses); err != nil {
		return nil, fmt.Errorf("backup target address is blocked: %w", err)
	}
	// Use a resolved public address for the actual connection, while net/http
	// retains the original hostname for the HTTP Host header and TLS SNI.
	return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(addresses[0].String(), port))
}
