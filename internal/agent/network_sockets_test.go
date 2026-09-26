package agent

import (
	"testing"
)

func TestIsPublicAddress(t *testing.T) {
	tests := []struct {
		ip       string
		isPublic bool
	}{
		{"0.0.0.0", true},
		{"::", true},
		{"127.0.0.1", false},
		{"127.0.0.54", false},
		{"::1", false},
		{"10.0.0.1", false},
		{"192.168.1.100", false},
		{"172.16.0.5", false},
		{"172.31.255.255", false},
		{"169.254.1.1", false},
		{"fe80::1", false},
		{"8.8.8.8", true},
		{"1.1.1.1", true},
		{"2606:4700:4700::1111", true},
		{"invalid-ip", false},
	}

	for _, tt := range tests {
		got := isPublicAddress(tt.ip)
		if got != tt.isPublic {
			t.Errorf("isPublicAddress(%q) = %v; want %v", tt.ip, got, tt.isPublic)
		}
	}
}

func TestParseNetHexAddrIPv4(t *testing.T) {
	tests := []struct {
		raw      string
		wantIP   string
		wantPort int
		wantErr  bool
	}{
		{"00000000:0050", "0.0.0.0", 80, false},
		{"0100007F:0016", "127.0.0.1", 22, false},
		{"0100007F:1F90", "127.0.0.1", 8080, false},
		{"3600007F:0035", "127.0.0.54", 53, false},
		{"00000000:01BB", "0.0.0.0", 443, false},
		{"invalid", "", 0, true},
		{"00000000:ZZZZ", "", 0, true},
		{"123:0050", "", 0, true},
	}

	for _, tt := range tests {
		ip, port, err := parseNetHexAddr(tt.raw, false)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseNetHexAddr(%q, false) error = %v, wantErr %v", tt.raw, err, tt.wantErr)
			continue
		}
		if !tt.wantErr {
			if ip != tt.wantIP || port != tt.wantPort {
				t.Errorf("parseNetHexAddr(%q, false) = (%s, %d); want (%s, %d)", tt.raw, ip, port, tt.wantIP, tt.wantPort)
			}
		}
	}
}

func TestParseNetHexAddrIPv6(t *testing.T) {
	tests := []struct {
		raw      string
		wantIP   string
		wantPort int
		wantErr  bool
	}{
		{"00000000000000000000000000000000:0050", "::", 80, false},
		{"00000000000000000000000001000000:0016", "::1", 22, false},
		{"00000000000000000000000000000000:01BB", "::", 443, false},
		{"short:0050", "", 0, true},
	}

	for _, tt := range tests {
		ip, port, err := parseNetHexAddr(tt.raw, true)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseNetHexAddr(%q, true) error = %v, wantErr %v", tt.raw, err, tt.wantErr)
			continue
		}
		if !tt.wantErr {
			if ip != tt.wantIP || port != tt.wantPort {
				t.Errorf("parseNetHexAddr(%q, true) = (%s, %d); want (%s, %d)", tt.raw, ip, port, tt.wantIP, tt.wantPort)
			}
		}
	}
}
