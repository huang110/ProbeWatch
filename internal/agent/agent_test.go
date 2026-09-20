package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/probewatch/probewatch/internal/config"
	"github.com/probewatch/probewatch/internal/protocol"
)

func TestNewRequiresOutboundAgentCredentials(t *testing.T) {
	if _, err := New(config.Config{}); err == nil {
		t.Fatal("New accepted an agent without endpoint or credentials")
	}
}

func TestDoJSONUsesBoundedAgentHeadersAndDecodesConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/config" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer node-secret" {
			t.Fatalf("authorization header = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("X-Probe-Timestamp") == "" || r.Header.Get("X-Probe-Request-ID") == "" {
			t.Fatal("agent metadata headers are missing")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(protocol.AgentConfigResponse{Tasks: []protocol.CheckTask{}})
	}))
	defer server.Close()

	runner, err := New(config.Config{AgentEndpoint: server.URL, AgentNodeUUID: "6f1d2c66-1a10-4a3e-9a55-0d3ea1a2b7f1", AgentNodeToken: "node-secret"})
	if err != nil {
		t.Fatal(err)
	}
	var response protocol.AgentConfigResponse
	if err := runner.doJSON(context.Background(), http.MethodGet, "/config", nil, &response); err != nil {
		t.Fatal(err)
	}
}

func TestDoJSONRejectsNonSuccessWithoutLeakingBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "secret internal details", http.StatusUnauthorized)
	}))
	defer server.Close()
	runner, err := New(config.Config{AgentEndpoint: server.URL, AgentNodeUUID: "6f1d2c66-1a10-4a3e-9a55-0d3ea1a2b7f1", AgentNodeToken: "node-secret"})
	if err != nil {
		t.Fatal(err)
	}
	err = runner.doJSON(context.Background(), http.MethodGet, "/config", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("error = %v, want bounded status only", err)
	}
}

func TestRunStopsOnContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/config" {
			_ = json.NewEncoder(w).Encode(protocol.AgentConfigResponse{})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	runner, err := New(config.Config{AgentEndpoint: server.URL, AgentNodeUUID: "6f1d2c66-1a10-4a3e-9a55-0d3ea1a2b7f1", AgentNodeToken: "node-secret"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runner.Run(ctx); err != nil && err != context.Canceled {
		t.Fatalf("Run cancellation error = %v", err)
	}
}
