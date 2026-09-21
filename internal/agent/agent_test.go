package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/config"
	"github.com/probewatch/probewatch/internal/protocol"
)

type fakeNetworkMonitor struct{ calls int }

func (f *fakeNetworkMonitor) Run(context.Context, protocol.CheckTask) protocol.NetworkResult {
	f.calls++
	return protocol.NetworkResult{Status: "network"}
}

type fakeMediaMonitor struct{ calls int }

func (f *fakeMediaMonitor) Run(context.Context, protocol.CheckTask) protocol.MediaResult {
	f.calls++
	return protocol.MediaResult{Status: "media", Detector: "fake"}
}

type fakeMTRMonitor struct{ calls int }

func (f *fakeMTRMonitor) Run(context.Context, protocol.CheckTask) protocol.MTRResult {
	f.calls++
	return protocol.MTRResult{Error: "unsupported: fake"}
}

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

func TestReportDispatchesAllMonitorKinds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer server.Close()
	runner, err := New(config.Config{AgentEndpoint: server.URL, AgentNodeUUID: "6f1d2c66-1a10-4a3e-9a55-0d3ea1a2b7f1", AgentNodeToken: "node-secret"})
	if err != nil {
		t.Fatal(err)
	}
	network, media, mtr := &fakeNetworkMonitor{}, &fakeMediaMonitor{}, &fakeMTRMonitor{}
	runner.probe, runner.media, runner.mtr = network, media, mtr
	runner.tasks = []protocol.CheckTask{
		{ID: "tcp", Kind: "tcp", Host: "example.com", Port: 443, IntervalSeconds: 10, Enabled: true},
		{ID: "media", Kind: "media_http", Host: "example.com", Port: 443, Path: "/", IntervalSeconds: 10, Enabled: true},
		{ID: "mtr", Kind: "mtr", Host: "example.com", Port: 443, MaxHops: 1, IntervalSeconds: 10, Enabled: true},
	}
	if err := runner.report(context.Background()); err != nil {
		t.Fatal(err)
	}
	if network.calls != 1 || media.calls != 1 || mtr.calls != 1 {
		t.Fatalf("calls = %d/%d/%d", network.calls, media.calls, mtr.calls)
	}
}

func TestReportHonorsIntervalsAndDisabledTasks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer server.Close()
	runner, err := New(config.Config{AgentEndpoint: server.URL, AgentNodeUUID: "6f1d2c66-1a10-4a3e-9a55-0d3ea1a2b7f1", AgentNodeToken: "node-secret"})
	if err != nil {
		t.Fatal(err)
	}
	probe := &fakeNetworkMonitor{}
	runner.probe = probe
	now := time.Unix(1000, 0)
	runner.now = func() time.Time { return now }
	runner.tasks = []protocol.CheckTask{
		{ID: "fast", Kind: "tcp", Host: "example.com", Port: 443, IntervalSeconds: 10, Enabled: true},
		{ID: "disabled", Kind: "tcp", Host: "example.com", Port: 443, IntervalSeconds: 10, Enabled: false},
	}
	if err := runner.report(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runner.report(context.Background()); err != nil {
		t.Fatal(err)
	}
	if probe.calls != 1 {
		t.Fatalf("calls before interval = %d", probe.calls)
	}
	now = now.Add(10 * time.Second)
	if err := runner.report(context.Background()); err != nil {
		t.Fatal(err)
	}
	if probe.calls != 2 {
		t.Fatalf("calls after interval = %d", probe.calls)
	}
}

func TestOutboxPersistsAndReplaysReport(t *testing.T) {
	dir := t.TempDir()
	available := false
	var requestIDs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestIDs = append(requestIDs, r.Header.Get("X-Probe-Request-ID"))
		if !available {
			http.Error(w, "unavailable", http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	runner, err := New(config.Config{AgentEndpoint: server.URL, AgentNodeUUID: "6f1d2c66-1a10-4a3e-9a55-0d3ea1a2b7f1", AgentNodeToken: "node-secret", AgentDataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	runner.client.Timeout = time.Second
	payload := []byte(`{"node_uuid":"6f1d2c66-1a10-4a3e-9a55-0d3ea1a2b7f1"}`)
	if err := runner.enqueueReport(queuedReport{RequestID: "fixed-request", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	outbox := filepath.Join(dir, "outbox")
	entries, err := os.ReadDir(outbox)
	if err != nil || len(entries) != 1 {
		t.Fatalf("outbox entries = %d, err=%v", len(entries), err)
	}
	info, err := entries[0].Info()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("file mode = %v", info.Mode().Perm())
	}
	if err := runner.replayOutbox(context.Background()); err == nil && len(requestIDs) == 0 {
		t.Fatal("replay did not attempt request")
	}
	available = true
	if err := runner.replayOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	remaining, err := os.ReadDir(outbox)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Fatalf("remaining outbox files = %d", len(remaining))
	}
	if len(requestIDs) < 2 || requestIDs[0] != requestIDs[1] {
		t.Fatalf("request ids = %#v, want reuse", requestIDs)
	}
}

func TestOutboxRejectsOversizeAndBadFiles(t *testing.T) {
	dir := t.TempDir()
	runner, err := New(config.Config{AgentEndpoint: "http://127.0.0.1:1", AgentNodeUUID: "6f1d2c66-1a10-4a3e-9a55-0d3ea1a2b7f1", AgentNodeToken: "node-secret", AgentDataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.enqueueReport(queuedReport{RequestID: "too-large", Payload: make([]byte, maxOutboxFileSize+1)}); err == nil {
		t.Fatal("oversize report accepted")
	}
	outbox, err := runner.outboxDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outbox, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outbox, "bad.json"), []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runner.replayOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outbox, "bad.json")); !os.IsNotExist(err) {
		t.Fatalf("bad file still exists: %v", err)
	}
}

func TestOutboxRejectsUnsafeDataDir(t *testing.T) {
	runner, err := New(config.Config{AgentEndpoint: "http://127.0.0.1:1", AgentNodeUUID: "6f1d2c66-1a10-4a3e-9a55-0d3ea1a2b7f1", AgentNodeToken: "node-secret", AgentDataDir: "."})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.enqueueReport(queuedReport{RequestID: "x", Payload: []byte("{}")}); err == nil {
		t.Fatal("unsafe data dir accepted")
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
