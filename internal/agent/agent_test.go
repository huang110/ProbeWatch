package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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

type fakeSpeedtestMonitor struct{ calls int }

func (f *fakeSpeedtestMonitor) Run(context.Context, protocol.CheckTask) protocol.SpeedtestResult {
	f.calls++
	return protocol.SpeedtestResult{Status: "ok", DownloadSpeedMbps: 100.0, UploadSpeedMbps: 50.0, TestedAt: 1}
}

type fakeSyntheticMonitor struct{ calls int }

func (f *fakeSyntheticMonitor) Run(context.Context, protocol.CheckTask) protocol.SyntheticResult {
	f.calls++
	return protocol.SyntheticResult{
		Status:     "ok",
		Protocol:   "https",
		TargetURL:  "https://api.example.com/health",
		StatusCode: 200,
		Timing: protocol.SyntheticTiming{
			TotalDurationMS: 45,
			DNSLookupMS:     2,
			TCPConnectMS:    10,
			TLSHandshakeMS:  15,
			TTFBMS:          12,
			TransferMS:      5,
		},
		Passed:    true,
		CheckedAt: 1720000000,
	}
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

// stubControlPlane serves a mutable agent configuration and captures reports,
// standing in for the control plane in fail-closed scheduling tests.
type stubControlPlane struct {
	mu      sync.Mutex
	config  protocol.AgentConfigResponse
	reports []protocol.ReportRequest
}

func (s *stubControlPlane) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/config":
			s.mu.Lock()
			config := s.config
			s.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(config)
		case "/report":
			body, err := io.ReadAll(io.LimitReader(r.Body, maxOutboxFileSize))
			if err != nil {
				http.Error(w, "unreadable report", http.StatusBadRequest)
				return
			}
			var report protocol.ReportRequest
			if err := json.Unmarshal(body, &report); err != nil {
				http.Error(w, "malformed report", http.StatusBadRequest)
				return
			}
			s.mu.Lock()
			s.reports = append(s.reports, report)
			s.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})
}

func (s *stubControlPlane) setConfig(config protocol.AgentConfigResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = config
}

func (s *stubControlPlane) receivedReports() []protocol.ReportRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]protocol.ReportRequest(nil), s.reports...)
}

func newStubRunner(t *testing.T, stub *stubControlPlane) *Runner {
	t.Helper()
	server := httptest.NewServer(stub.handler())
	t.Cleanup(server.Close)
	runner, err := New(config.Config{AgentEndpoint: server.URL, AgentNodeUUID: "6f1d2c66-1a10-4a3e-9a55-0d3ea1a2b7f1", AgentNodeToken: "node-secret"})
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

func staleWindowTask() protocol.CheckTask {
	return protocol.CheckTask{ID: "tcp-1", Kind: "tcp", Host: "example.com", Port: 443, TimeoutMS: 1000, MaxHops: 1, IntervalSeconds: 10, Enabled: true}
}

func TestReportStopsProbingAfterConfigMaxAgeAndRecoversAfterRefresh(t *testing.T) {
	stub := &stubControlPlane{}
	stub.setConfig(protocol.AgentConfigResponse{Tasks: []protocol.CheckTask{staleWindowTask()}, ConfigVersion: 100, ConfigMaxAgeSeconds: 30})
	runner := newStubRunner(t, stub)
	probe := &fakeNetworkMonitor{}
	runner.probe = probe
	now := time.Unix(10000, 0)
	runner.now = func() time.Time { return now }

	if err := runner.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if runner.lastConfigVersion != 100 || runner.configMaxAgeSeconds != 30 {
		t.Fatalf("after refresh version = %d, max age = %d, want 100/30", runner.lastConfigVersion, runner.configMaxAgeSeconds)
	}
	if !runner.lastRefreshSuccessAt.Equal(now) {
		t.Fatalf("lastRefreshSuccessAt = %v, want %v", runner.lastRefreshSuccessAt, now)
	}

	if err := runner.report(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(30 * time.Second)
	if err := runner.report(context.Background()); err != nil {
		t.Fatal(err)
	}
	if probe.calls != 2 {
		t.Fatalf("probe calls within the window = %d, want 2", probe.calls)
	}

	now = now.Add(time.Second)
	if err := runner.report(context.Background()); err != nil {
		t.Fatal(err)
	}
	if probe.calls != 2 {
		t.Fatalf("probe calls after the window = %d, want 2 (fail-closed stops new probes)", probe.calls)
	}
	reports := stub.receivedReports()
	if len(reports) != 3 {
		t.Fatalf("reports received = %d, want 3 (resource reporting continues)", len(reports))
	}
	stale := reports[len(reports)-1]
	if len(stale.Results) != 0 {
		t.Fatalf("stale report results = %#v, want empty", stale.Results)
	}
	if stale.Resource.OS == "" || stale.Resource.AgentVersion == "" {
		t.Fatalf("stale report resource = %#v, want populated identity", stale.Resource)
	}

	stub.setConfig(protocol.AgentConfigResponse{Tasks: []protocol.CheckTask{staleWindowTask()}, ConfigVersion: 200, ConfigMaxAgeSeconds: 30})
	now = now.Add(5 * time.Minute)
	if err := runner.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if runner.lastConfigVersion != 200 || !runner.lastRefreshSuccessAt.Equal(now) {
		t.Fatalf("after recovery version = %d, lastRefreshSuccessAt = %v, want 200/%v", runner.lastConfigVersion, runner.lastRefreshSuccessAt, now)
	}
	if err := runner.report(context.Background()); err != nil {
		t.Fatal(err)
	}
	if probe.calls != 3 {
		t.Fatalf("probe calls after recovery = %d, want 3 (probing resumed)", probe.calls)
	}
	reports = stub.receivedReports()
	resumed := reports[len(reports)-1]
	if len(resumed.Results) != 1 || resumed.Results[0].ID != "tcp-1" {
		t.Fatalf("recovered report results = %#v, want one tcp-1 result", resumed.Results)
	}
}

func TestReportWithoutConfigMaxAgeKeepsProbing(t *testing.T) {
	stub := &stubControlPlane{}
	stub.setConfig(protocol.AgentConfigResponse{Tasks: []protocol.CheckTask{staleWindowTask()}, ConfigVersion: 7})
	runner := newStubRunner(t, stub)
	probe := &fakeNetworkMonitor{}
	runner.probe = probe
	now := time.Unix(10000, 0)
	runner.now = func() time.Time { return now }

	if err := runner.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if runner.configMaxAgeSeconds != 0 {
		t.Fatalf("configMaxAgeSeconds = %d, want 0 without a served max age", runner.configMaxAgeSeconds)
	}
	if err := runner.report(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(48 * time.Hour)
	if err := runner.report(context.Background()); err != nil {
		t.Fatal(err)
	}
	if probe.calls != 2 {
		t.Fatalf("probe calls = %d, want 2 (legacy behavior without max age)", probe.calls)
	}
	reports := stub.receivedReports()
	if len(reports) != 2 || len(reports[len(reports)-1].Results) != 1 {
		t.Fatalf("reports without max age = %#v, want both carrying probe results", reports)
	}
}

func TestRefreshWithNewConfigVersionKeepsRunningSchedule(t *testing.T) {
	stub := &stubControlPlane{}
	stub.setConfig(protocol.AgentConfigResponse{Tasks: []protocol.CheckTask{staleWindowTask()}, ConfigVersion: 1, ConfigMaxAgeSeconds: 3600})
	runner := newStubRunner(t, stub)
	probe := &fakeNetworkMonitor{}
	runner.probe = probe
	now := time.Unix(1000, 0)
	runner.now = func() time.Time { return now }

	if err := runner.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runner.report(context.Background()); err != nil {
		t.Fatal(err)
	}
	if probe.calls != 1 {
		t.Fatalf("probe calls = %d, want 1", probe.calls)
	}

	stub.setConfig(protocol.AgentConfigResponse{Tasks: []protocol.CheckTask{staleWindowTask()}, ConfigVersion: 2, ConfigMaxAgeSeconds: 3600})
	now = now.Add(5 * time.Second)
	if err := runner.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if runner.lastConfigVersion != 2 {
		t.Fatalf("lastConfigVersion = %d, want 2", runner.lastConfigVersion)
	}
	if _, ok := runner.next["tcp-1"]; !ok {
		t.Fatal("version change reset the schedule of a surviving task")
	}
	if err := runner.report(context.Background()); err != nil {
		t.Fatal(err)
	}
	if probe.calls != 1 {
		t.Fatalf("probe calls before the kept interval = %d, want 1", probe.calls)
	}

	now = now.Add(5 * time.Second)
	if err := runner.report(context.Background()); err != nil {
		t.Fatal(err)
	}
	if probe.calls != 2 {
		t.Fatalf("probe calls after the interval = %d, want 2", probe.calls)
	}

	stub.setConfig(protocol.AgentConfigResponse{Tasks: []protocol.CheckTask{}, ConfigVersion: 3, ConfigMaxAgeSeconds: 3600})
	if err := runner.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := runner.next["tcp-1"]; ok {
		t.Fatal("removed task kept its schedule entry after refresh")
	}
}

func TestRefreshFailureLeavesLastRefreshStateUntouched(t *testing.T) {
	stub := &stubControlPlane{}
	stub.setConfig(protocol.AgentConfigResponse{Tasks: []protocol.CheckTask{staleWindowTask()}, ConfigVersion: 100, ConfigMaxAgeSeconds: 30})
	runner := newStubRunner(t, stub)
	now := time.Unix(10000, 0)
	runner.now = func() time.Time { return now }
	if err := runner.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	stub.setConfig(protocol.AgentConfigResponse{Tasks: []protocol.CheckTask{staleWindowTask()}, ConfigVersion: 101, ConfigMaxAgeSeconds: -1})
	now = now.Add(time.Minute)
	if err := runner.refresh(context.Background()); err == nil {
		t.Fatal("refresh accepted a config with an invalid max age")
	}
	if runner.lastConfigVersion != 100 || runner.configMaxAgeSeconds != 30 || !runner.lastRefreshSuccessAt.Equal(time.Unix(10000, 0)) {
		t.Fatalf("failed refresh mutated state: version %d, max age %d, refreshed at %v", runner.lastConfigVersion, runner.configMaxAgeSeconds, runner.lastRefreshSuccessAt)
	}
}

func TestReportRunsSpeedtestTask(t *testing.T) {
	stub := &stubControlPlane{}
	speedTask := protocol.CheckTask{
		ID:              "speed-1",
		Kind:            "speedtest",
		Host:            "speed.example.com",
		Port:            443,
		ServerURL:       "https://speed.example.com/__down",
		DownloadBytes:   1024 * 1024,
		UploadBytes:     512 * 1024,
		IntervalSeconds: 60,
		MaxHops:         10,
		Enabled:         true,
		TimeoutMS:       5000,
	}
	stub.setConfig(protocol.AgentConfigResponse{Tasks: []protocol.CheckTask{speedTask}})
	runner := newStubRunner(t, stub)
	fakeSpeed := &fakeSpeedtestMonitor{}
	runner.speedtest = fakeSpeed

	if err := runner.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runner.report(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fakeSpeed.calls != 1 {
		t.Fatalf("expected 1 speedtest call, got %d", fakeSpeed.calls)
	}
	reports := stub.receivedReports()
	if len(reports) != 1 {
		t.Fatalf("expected 1 report, got %d", len(reports))
	}
	if len(reports[0].Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(reports[0].Results))
	}
	res := reports[0].Results[0]
	if res.Kind != "speedtest" || res.Speedtest == nil || res.Speedtest.DownloadSpeedMbps != 100.0 {
		t.Fatalf("unexpected speedtest result: %#v", res)
	}
}

func TestReportRunsSyntheticTask(t *testing.T) {
	stub := &stubControlPlane{}
	syntheticTask := protocol.CheckTask{
		ID:              "synthetic-1",
		Kind:            "synthetic",
		Host:            "api.example.com",
		Port:            443,
		Method:          "GET",
		Path:            "/health",
		IntervalSeconds: 30,
		MaxHops:         20,
		Enabled:         true,
		TimeoutMS:       5000,
	}
	stub.setConfig(protocol.AgentConfigResponse{Tasks: []protocol.CheckTask{syntheticTask}})
	runner := newStubRunner(t, stub)
	fakeSyn := &fakeSyntheticMonitor{}
	runner.synthetic = fakeSyn

	if err := runner.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runner.report(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fakeSyn.calls != 1 {
		t.Fatalf("expected 1 synthetic call, got %d", fakeSyn.calls)
	}
	reports := stub.receivedReports()
	if len(reports) != 1 {
		t.Fatalf("expected 1 report, got %d", len(reports))
	}
	if len(reports[0].Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(reports[0].Results))
	}
	res := reports[0].Results[0]
	if res.Kind != "synthetic" || res.Synthetic == nil || res.Synthetic.StatusCode != 200 {
		t.Fatalf("unexpected synthetic result: %#v", res)
	}
}

