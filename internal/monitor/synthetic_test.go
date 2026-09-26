package monitor

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/probewatch/probewatch/internal/protocol"
)

func TestSyntheticHTTPAssertionsPass(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom-Env", "production")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","code":0,"data":{"service":"gateway","alive":true}}`))
	}))
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	port, _ := strconv.Atoi(u.Port())

	monitor := NewSyntheticMonitor()
	task := protocol.CheckTask{
		ID:        "syn-1",
		Kind:      "synthetic",
		Host:      u.Hostname(),
		Port:      port,
		Path:      "/",
		ServerURL: ts.URL,
		TimeoutMS: 5000,
		MaxHops:   10,
		Assertions: []protocol.SyntheticAssertionRule{
			{Source: "status_code", Operator: "equals", Target: "200"},
			{Source: "header", Property: "X-Custom-Env", Operator: "equals", Target: "production"},
			{Source: "body_regex", Operator: "contains", Target: `"service":"gateway"`},
			{Source: "jsonpath", Property: "status", Operator: "equals", Target: "ok"},
			{Source: "jsonpath", Property: "data.service", Operator: "equals", Target: "gateway"},
			{Source: "max_latency_ms", Operator: "less_than", Target: "10000"},
		},
	}

	res := monitor.ExecuteSynthetic(context.Background(), task)
	if !res.Passed {
		t.Fatalf("expected synthetic probe to pass, got failed assertion: %s, error: %s", res.FailedAssertion, res.Error)
	}
	if res.StatusCode != 200 {
		t.Fatalf("expected status code 200, got %d", res.StatusCode)
	}
	if res.Timing.TotalDurationMS < 0 {
		t.Fatalf("invalid total duration: %d", res.Timing.TotalDurationMS)
	}
}

func TestSyntheticHTTPAssertionFails(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"status":"error","message":"internal server error"}`))
	}))
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	port, _ := strconv.Atoi(u.Port())

	monitor := NewSyntheticMonitor()
	task := protocol.CheckTask{
		ID:        "syn-fail",
		Kind:      "synthetic",
		Host:      u.Hostname(),
		Port:      port,
		Path:      "/",
		ServerURL: ts.URL,
		TimeoutMS: 5000,
		MaxHops:   10,
		Assertions: []protocol.SyntheticAssertionRule{
			{Source: "status_code", Operator: "equals", Target: "200"},
		},
	}

	res := monitor.ExecuteSynthetic(context.Background(), task)
	if res.Passed {
		t.Fatal("expected synthetic probe to fail on status 500")
	}
	if !strings.Contains(res.FailedAssertion, "status_code") {
		t.Fatalf("expected failed assertion to mention status_code, got %s", res.FailedAssertion)
	}
	if res.Status != "failing" {
		t.Fatalf("expected status 'failing', got %s", res.Status)
	}
}

func TestSyntheticWebSocketHandshake(t *testing.T) {
	// Start TCP listener simulating WebSocket upgrade server
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer l.Close()

	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		buf := make([]byte, 1024)
		n, _ := conn.Read(buf)
		req := string(buf[:n])
		if strings.Contains(req, "Upgrade: websocket") {
			resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=\r\n\r\n"
			_, _ = conn.Write([]byte(resp))
		}
	}()

	addr := l.Addr().String()
	parts := strings.Split(addr, ":")
	port, _ := strconv.Atoi(parts[1])

	monitor := NewSyntheticMonitor()
	task := protocol.CheckTask{
		ID:        "ws-1",
		Kind:      "websocket",
		Host:      "127.0.0.1",
		Port:      port,
		ServerURL: fmt.Sprintf("ws://%s", addr),
		TimeoutMS: 3000,
		MaxHops:   10,
	}

	res := monitor.ExecuteSynthetic(context.Background(), task)
	if !res.Passed {
		t.Fatalf("expected websocket probe to pass, got error: %s", res.Error)
	}
	if res.StatusCode != 101 {
		t.Fatalf("expected status 101, got %d", res.StatusCode)
	}
	if !res.WebSocketEcho {
		t.Fatal("expected WebSocketEcho to be true")
	}
}

func TestExtractJSONProperty(t *testing.T) {
	jsonStr := `{"code":0,"data":{"user":{"name":"Alice","role":"admin"}},"tags":["ops","sec"]}`

	if val := extractJSONProperty(jsonStr, "code"); val != "0" {
		t.Fatalf("expected '0', got %q", val)
	}
	if val := extractJSONProperty(jsonStr, "data.user.name"); val != "Alice" {
		t.Fatalf("expected 'Alice', got %q", val)
	}
	if val := extractJSONProperty(jsonStr, "data.user.role"); val != "admin" {
		t.Fatalf("expected 'admin', got %q", val)
	}
	if val := extractJSONProperty(jsonStr, "nonexistent"); val != "" {
		t.Fatalf("expected empty string for nonexistent key, got %q", val)
	}
}

func TestBuildDNSQueryWire(t *testing.T) {
	wire := buildDNSQueryWire("cloudflare.com")
	if len(wire) < 12 {
		t.Fatalf("DNS wire format query too short: %d", len(wire))
	}
	// Check QNAME encoding
	if !strings.Contains(string(wire), "cloudflare") {
		t.Fatal("DNS wire query missing domain labels")
	}
}
