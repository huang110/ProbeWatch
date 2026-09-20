package security

import (
	"net/netip"
	"strings"
	"testing"
)

func TestIsBlockedAddressRejectsBlockedRanges(t *testing.T) {
	tests := []struct {
		name string
		addr string
	}{
		{"ipv4 loopback", "127.0.0.1"},
		{"ipv6 loopback", "::1"},
		{"ipv4 private 10", "10.1.2.3"},
		{"ipv4 private 172", "172.16.10.20"},
		{"ipv4 private 192", "192.168.1.2"},
		{"ipv6 unique local", "fd12:3456::1"},
		{"ipv4 link local", "169.254.1.1"},
		{"ipv6 link local", "fe80::1"},
		{"ipv4 unspecified", "0.0.0.0"},
		{"ipv6 unspecified", "::"},
		{"ipv4 multicast", "224.0.0.1"},
		{"ipv6 multicast", "ff02::1"},
		{"ipv4 metadata", "169.254.169.254"},
		{"alternate metadata", "100.100.100.200"},
		{"mapped loopback", "::ffff:127.0.0.1"},
		{"mapped private", "::ffff:192.168.1.1"},
		{"mapped metadata", "::ffff:169.254.169.254"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			addr, err := netip.ParseAddr(test.addr)
			if err != nil {
				t.Fatal(err)
			}
			if !IsBlockedAddress(addr) {
				t.Fatalf("IsBlockedAddress(%q) = false, want true", test.addr)
			}
		})
	}
}

func TestIsBlockedAddressAllowsPublicAddresses(t *testing.T) {
	for _, value := range []string{"8.8.8.8", "1.1.1.1", "2001:4860:4860::8888"} {
		addr, err := netip.ParseAddr(value)
		if err != nil {
			t.Fatal(err)
		}
		if IsBlockedAddress(addr) {
			t.Fatalf("IsBlockedAddress(%q) = true, want false", value)
		}
	}
}

func TestValidateResolvedIPsChecksEveryAddress(t *testing.T) {
	public, err := netip.ParseAddr("93.184.216.34")
	if err != nil {
		t.Fatal(err)
	}
	blocked, err := netip.ParseAddr("100.100.100.200")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateResolvedIPs([]netip.Addr{public, blocked}); err == nil {
		t.Fatal("ValidateResolvedIPs accepted a blocked address")
	}
	if err := ValidateResolvedIPs([]netip.Addr{public}); err != nil {
		t.Fatalf("ValidateResolvedIPs rejected a public address: %v", err)
	}
	if err := ValidateResolvedIPs([]netip.Addr{{}}); err == nil {
		t.Fatal("ValidateResolvedIPs accepted an invalid address")
	}
}

func TestValidateHostAcceptsPublicIPAndDomainWithoutResolving(t *testing.T) {
	for _, host := range []string{"example.com", "api.example.com", "8.8.8.8", "2001:4860:4860::8888"} {
		if err := ValidateHost(host); err != nil {
			t.Errorf("ValidateHost(%q) returned error: %v", host, err)
		}
	}
}

func TestValidateHostRejectsBlockedAddresses(t *testing.T) {
	for _, host := range []string{
		"127.0.0.1",
		"10.0.0.1",
		"172.16.0.1",
		"192.168.0.1",
		"169.254.169.254",
		"100.100.100.200",
		"[::1]",
		"::ffff:127.0.0.1",
	} {
		if err := ValidateHost(host); err == nil {
			t.Errorf("ValidateHost(%q) accepted a blocked host", host)
		}
	}
}

func TestValidateHostRejectsMalformedOrURLLikeInput(t *testing.T) {
	for _, host := range []string{
		"",
		" ",
		"example .com",
		"example.com/path",
		"https://example.com",
		"user@example.com",
		"example.com:443",
		"example..com",
		"-example.com",
		"example-.com",
		"example.com.",
		strings.Repeat("a", 64) + ".com",
		strings.Repeat("a", 254),
	} {
		if err := ValidateHost(host); err == nil {
			t.Errorf("ValidateHost(%q) accepted malformed host", host)
		}
	}
}

func TestValidateURLAcceptsSafeHTTPAndHTTPSURLs(t *testing.T) {
	for _, raw := range []string{
		"https://example.com/health",
		"http://8.8.8.8:8080/status",
		"https://[2001:4860:4860::8888]/probe",
	} {
		parsed, err := ValidateURL(raw, []string{"http", "https"})
		if err != nil {
			t.Errorf("ValidateURL(%q) returned error: %v", raw, err)
			continue
		}
		if parsed == nil || parsed.Hostname() == "" {
			t.Errorf("ValidateURL(%q) returned an invalid parsed URL: %#v", raw, parsed)
		}
	}
}

func TestValidateURLRejectsUnsafeOrMalformedURLs(t *testing.T) {
	tests := []string{
		"example.com/path",
		"//example.com/path",
		"https:///path",
		"ftp://example.com",
		"file:///etc/passwd",
		"https://user:pass@example.com",
		"https://user@example.com",
		"https://example.com/path?token=secret",
		"https://example.com/path?",
		"https://example.com/path#fragment",
		"https://example.com/path#",
		"https://example.com:0/path",
		"https://example.com:65536/path",
		"https://example.com:bad/path",
		"https://example.com:443:444/path",
		"https://127.0.0.1/path",
		"https://169.254.169.254/latest/meta-data",
		"https://[::ffff:169.254.169.254]/latest/meta-data",
		"https://[::1]/path",
		"https://example.com\\@127.0.0.1/path",
	}

	for _, raw := range tests {
		if _, err := ValidateURL(raw, []string{"http", "https"}); err == nil {
			t.Errorf("ValidateURL(%q) accepted unsafe URL", raw)
		}
	}
}

func TestValidateURLHonorsAllowedSchemes(t *testing.T) {
	if _, err := ValidateURL("https://example.com", []string{"http"}); err == nil {
		t.Fatal("ValidateURL accepted a scheme outside allowedSchemes")
	}
	if _, err := ValidateURL("https://example.com", nil); err == nil {
		t.Fatal("ValidateURL accepted a URL with no allowed schemes")
	}
}

func TestValidateURLRejectsOversizedURLAndPath(t *testing.T) {
	if _, err := ValidateURL("", []string{"http", "https"}); err == nil {
		t.Fatal("ValidateURL accepted an empty URL")
	}
	oversizedURL := "https://example.com/" + strings.Repeat("a", 4096)
	if _, err := ValidateURL(oversizedURL, []string{"http", "https"}); err == nil {
		t.Fatal("ValidateURL accepted a URL over the maximum length")
	}
	oversizedPath := "https://example.com/" + strings.Repeat("a", 2048)
	if _, err := ValidateURL(oversizedPath, []string{"http", "https"}); err == nil {
		t.Fatal("ValidateURL accepted a path over the maximum length")
	}
	boundedPath := "https://example.com/" + strings.Repeat("a", 2000)
	if _, err := ValidateURL(boundedPath, []string{"http", "https"}); err != nil {
		t.Fatalf("ValidateURL rejected a bounded path: %v", err)
	}
}
