package monitor

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/probewatch/probewatch/internal/protocol"
	"github.com/probewatch/probewatch/internal/terminal"
)

func websocketAcceptForTest(key string) string {
	h := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(h[:])
}

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

	monitor := NewSyntheticMonitorWithOptions(true)
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

	monitor := NewSyntheticMonitorWithOptions(true)
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

func TestSyntheticExplicitStatusCodeAssertionCanPass(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	port, _ := strconv.Atoi(u.Port())
	monitor := NewSyntheticMonitorWithOptions(true)
	task := protocol.CheckTask{
		ID:        "syn-explicit-status",
		Kind:      "synthetic",
		Host:      u.Hostname(),
		Port:      port,
		Path:      "/",
		ServerURL: ts.URL,
		TimeoutMS: 5000,
		MaxHops:   10,
		Assertions: []protocol.SyntheticAssertionRule{
			{Source: "status_code", Operator: "equals", Target: "404"},
		},
	}

	res := monitor.ExecuteSynthetic(context.Background(), task)
	if !res.Passed {
		t.Fatalf("expected explicit 404 assertion to pass, got error: %s, failed assertion: %s", res.Error, res.FailedAssertion)
	}
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status code = %d, want 404", res.StatusCode)
	}
}

func TestSyntheticWebSocketHandshake(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := terminal.Upgrade(w, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer conn.Close()
		opcode, payload, err := conn.ReadMessage()
		if err == nil {
			_ = conn.WriteMessage(opcode, payload)
		}
	}))
	defer server.Close()

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse websocket test URL: %v", err)
	}
	port, _ := strconv.Atoi(u.Port())

	monitor := NewSyntheticMonitorWithOptions(true)
	task := protocol.CheckTask{
		ID:        "ws-1",
		Kind:      "websocket",
		Host:      u.Hostname(),
		Port:      port,
		ServerURL: "ws" + strings.TrimPrefix(server.URL, "http"),
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

func TestSyntheticWebSocketRejectsHandshakeOnlyServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Deliberately advertise an upgrade and then close without accepting a
		// data frame. A handshake-only endpoint must not be reported as an echo
		// monitor success.
		key := r.Header.Get("Sec-WebSocket-Key")
		accept := websocketAcceptForTest(key)
		w.Header().Set("Upgrade", "websocket")
		w.Header().Set("Connection", "Upgrade")
		w.Header().Set("Sec-WebSocket-Accept", accept)
		w.WriteHeader(http.StatusSwitchingProtocols)
	}))
	defer server.Close()

	monitor := NewSyntheticMonitorWithOptions(true)
	res := monitor.ExecuteSynthetic(context.Background(), protocol.CheckTask{
		ID:        "ws-no-echo",
		Kind:      "websocket",
		ServerURL: "ws" + strings.TrimPrefix(server.URL, "http"),
		TimeoutMS: 1000,
	})
	if res.Passed || res.WebSocketEcho {
		t.Fatalf("handshake-only WebSocket endpoint was incorrectly reported healthy: %#v", res)
	}
}

func TestSyntheticWebSocketRejectsBadAccept(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Upgrade", "websocket")
		w.Header().Set("Connection", "Upgrade")
		w.Header().Set("Sec-WebSocket-Accept", "invalid")
		w.WriteHeader(http.StatusSwitchingProtocols)
	}))
	defer server.Close()

	monitor := NewSyntheticMonitorWithOptions(true)
	res := monitor.ExecuteSynthetic(context.Background(), protocol.CheckTask{
		ID:        "ws-bad-accept",
		Kind:      "websocket",
		ServerURL: "ws" + strings.TrimPrefix(server.URL, "http"),
		TimeoutMS: 1000,
	})
	if res.Passed {
		t.Fatalf("bad WebSocket accept was incorrectly reported healthy: %#v", res)
	}
}

func TestSyntheticGRPCRejectsPlainTCP(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = conn.Write([]byte("this is only a TCP service"))
	}()

	monitor := NewSyntheticMonitorWithOptions(true)
	res := monitor.ExecuteSynthetic(context.Background(), protocol.CheckTask{
		ID:        "grpc-plain-tcp",
		Kind:      "grpc",
		ServerURL: "grpc://" + l.Addr().String(),
		TimeoutMS: 1000,
	})
	if res.Passed {
		t.Fatalf("plain TCP service was incorrectly reported as gRPC SERVING: %#v", res)
	}
	if res.GRPCStatus == "SERVING" {
		t.Fatalf("plain TCP service received SERVING status: %#v", res)
	}
}

func TestSyntheticGRPCCleartextRequiresSettings(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		prefaceAndSettings := make([]byte, 24+9)
		if _, err := io.ReadFull(conn, prefaceAndSettings); err != nil {
			return
		}
		// A legal empty server SETTINGS frame: length=0, type=4, stream=0.
		_, _ = conn.Write([]byte{0, 0, 0, 4, 0, 0, 0, 0, 0})
	}()

	monitor := NewSyntheticMonitorWithOptions(true)
	res := monitor.ExecuteSynthetic(context.Background(), protocol.CheckTask{
		ID:        "grpc-h2c",
		Kind:      "grpc",
		ServerURL: "grpc://" + l.Addr().String(),
		TimeoutMS: 1000,
	})
	if !res.Passed || res.GRPCStatus != "SERVING" {
		t.Fatalf("valid h2c SETTINGS exchange failed: %#v", res)
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
