package security

import (
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

const maxHostnameLength = 253
const maxTargetURLLength = 4096
const maxTargetPathLength = 2048

var blockedMetadataAddresses = [...]netip.Addr{
	netip.MustParseAddr("169.254.169.254"),
	netip.MustParseAddr("100.100.100.200"),
}

// IsBlockedAddress reports whether addr belongs to a non-public address range.
func IsBlockedAddress(addr netip.Addr) bool {
	if !addr.IsValid() {
		return true
	}
	addr = addr.Unmap()
	if addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsUnspecified() || addr.IsMulticast() {
		return true
	}
	for _, blocked := range blockedMetadataAddresses {
		if addr == blocked {
			return true
		}
	}
	return false
}

// ValidateResolvedIPs validates all addresses returned for a target hostname.
func ValidateResolvedIPs(addresses []netip.Addr) error {
	if len(addresses) == 0 {
		return fmt.Errorf("target resolved to no addresses")
	}
	for _, addr := range addresses {
		if IsBlockedAddress(addr) {
			return fmt.Errorf("target resolved to blocked address %q", addr)
		}
	}
	return nil
}

// ValidateHost validates a hostname or IP literal without performing DNS resolution.
func ValidateHost(host string) error {
	if host == "" || len(host) > maxHostnameLength || strings.TrimSpace(host) != host {
		return fmt.Errorf("invalid host")
	}
	if strings.ContainsAny(host, "/?#@\\[]") {
		return fmt.Errorf("invalid host syntax")
	}

	if addr, err := netip.ParseAddr(host); err == nil {
		if addr.Zone() != "" || IsBlockedAddress(addr) {
			return fmt.Errorf("blocked host address")
		}
		return nil
	}

	if strings.EqualFold(host, "localhost") || strings.EqualFold(host, "localhost.localdomain") || strings.EqualFold(host, "ip6-localhost") || strings.EqualFold(host, "ip6-loopback") {
		return fmt.Errorf("local hostname is not allowed")
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return fmt.Errorf("invalid hostname label")
		}
		for _, character := range label {
			if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-') {
				return fmt.Errorf("invalid hostname character")
			}
		}
	}
	return nil
}

// ValidateURL validates an HTTP(S) target URL without resolving or connecting to it.
func ValidateURL(raw string, allowedSchemes []string) (*url.URL, error) {
	if raw == "" || len(raw) > maxTargetURLLength {
		return nil, fmt.Errorf("target URL exceeds maximum length of %d bytes", maxTargetURLLength)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse target URL: %w", err)
	}
	if !parsed.IsAbs() || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.Contains(raw, "#") || parsed.Opaque != "" {
		return nil, fmt.Errorf("target URL must be an absolute URL without credentials, query, or fragment")
	}
	if len(parsed.EscapedPath()) > maxTargetPathLength {
		return nil, fmt.Errorf("target URL path exceeds maximum length of %d bytes", maxTargetPathLength)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" || !containsScheme(allowedSchemes, scheme) {
		return nil, fmt.Errorf("target URL scheme is not allowed")
	}
	if err := validateExplicitPort(parsed.Host); err != nil {
		return nil, err
	}
	if err := ValidateHost(parsed.Hostname()); err != nil {
		return nil, fmt.Errorf("invalid target URL host: %w", err)
	}
	return parsed, nil
}

func containsScheme(allowedSchemes []string, scheme string) bool {
	for _, allowed := range allowedSchemes {
		if strings.EqualFold(allowed, scheme) {
			return true
		}
	}
	return false
}

func validateExplicitPort(host string) error {
	port := ""
	switch {
	case strings.HasPrefix(host, "["):
		closing := strings.IndexByte(host, ']')
		if closing < 0 {
			return fmt.Errorf("invalid target URL host")
		}
		remainder := host[closing+1:]
		if remainder == "" {
			return nil
		}
		if !strings.HasPrefix(remainder, ":") {
			return fmt.Errorf("invalid target URL host")
		}
		port = remainder[1:]
	case strings.Count(host, ":") == 0:
		return nil
	case strings.Count(host, ":") == 1:
		port = host[strings.LastIndexByte(host, ':')+1:]
	default:
		return fmt.Errorf("invalid target URL host")
	}
	if port == "" {
		return fmt.Errorf("invalid target URL port")
	}
	for _, character := range port {
		if character < '0' || character > '9' {
			return fmt.Errorf("invalid target URL port")
		}
	}
	value, err := strconv.Atoi(port)
	if err != nil || value < 1 || value > 65535 {
		return fmt.Errorf("invalid target URL port")
	}
	return nil
}
