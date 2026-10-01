package terminal

import "testing"

func TestBuildWebSocketURLAvoidsDuplicateAgentPrefix(t *testing.T) {
	tests := []struct {
		endpoint string
		want     string
	}{
		{"https://example.test/api/agent/v1", "wss://example.test/api/agent/v1/terminal/tunnel"},
		{"https://example.test", "wss://example.test/api/agent/v1/terminal/tunnel"},
		{"http://127.0.0.1:8080/api/agent/v1/", "ws://127.0.0.1:8080/api/agent/v1/terminal/tunnel"},
	}
	for _, tt := range tests {
		c := NewClient(tt.endpoint, "node", "token")
		if got := c.buildWebSocketURL(); got != tt.want {
			t.Errorf("endpoint %q: got %q, want %q", tt.endpoint, got, tt.want)
		}
	}
}
