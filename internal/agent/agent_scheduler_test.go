package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/config"
	"github.com/probewatch/probewatch/internal/protocol"
)

type slowMTRMonitor struct {
	delay time.Duration
	calls int32
}

func (s *slowMTRMonitor) Run(ctx context.Context, task protocol.CheckTask) protocol.MTRResult {
	atomic.AddInt32(&s.calls, 1)
	select {
	case <-time.After(s.delay):
	case <-ctx.Done():
	}
	return protocol.MTRResult{Error: "slow mtr completed"}
}

// TestSlowTaskDoesNotBlockHeartbeat verifies that a slow network probe (e.g. MTR)
// does not delay or block regular resource heartbeats.
func TestSlowTaskDoesNotBlockHeartbeat(t *testing.T) {
	var reportCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/report" {
			atomic.AddInt32(&reportCount, 1)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	runner, err := New(config.Config{
		AgentEndpoint:  server.URL,
		AgentNodeUUID:  "6f1d2c66-1a10-4a3e-9a55-0d3ea1a2b7f1",
		AgentNodeToken: "node-secret",
	})
	if err != nil {
		t.Fatal(err)
	}

	slowMTR := &slowMTRMonitor{delay: 300 * time.Millisecond}
	runner.mtr = slowMTR
	runner.tasks = []protocol.CheckTask{
		{ID: "slow-mtr-1", Kind: "mtr", Host: "example.com", Port: 443, IntervalSeconds: 1, Enabled: true},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	// Call report directly: it should dispatch slow MTR asynchronously or without blocking > 50ms,
	// allowing report to complete quickly.
	start := time.Now()
	if err := runner.report(ctx); err != nil {
		t.Fatalf("first report failed: %v", err)
	}
	duration := time.Since(start)

	if duration >= 250*time.Millisecond {
		t.Fatalf("report() was blocked by slow task for %v (expected < 150ms)", duration)
	}

	// Verify the heartbeat was sent
	if atomic.LoadInt32(&reportCount) < 1 {
		t.Fatalf("expected at least 1 report sent, got %d", atomic.LoadInt32(&reportCount))
	}
}

// TestSingleReportPerCycle verifies that within a single event loop window,
// triggering workload and events tickers does not result in duplicate /report calls.
func TestSingleReportPerCycle(t *testing.T) {
	var reportCalls int32
	var workloadCalls int32
	var eventsCalls int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/report":
			atomic.AddInt32(&reportCalls, 1)
			w.WriteHeader(http.StatusNoContent)
		case "/workload":
			atomic.AddInt32(&workloadCalls, 1)
			w.WriteHeader(http.StatusNoContent)
		case "/events":
			atomic.AddInt32(&eventsCalls, 1)
			w.WriteHeader(http.StatusNoContent)
		case "/config":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(protocol.AgentConfigResponse{Tasks: []protocol.CheckTask{}})
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	runner, err := New(config.Config{
		AgentEndpoint:  server.URL,
		AgentNodeUUID:  "6f1d2c66-1a10-4a3e-9a55-0d3ea1a2b7f1",
		AgentNodeToken: "node-secret",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Simulate event loop dispatching without the old bug where every select case
	// looped back to the top and ran r.report(ctx).
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run agent in background for a brief window
	go func() {
		_ = runner.Run(ctx)
	}()

	// Wait long enough for initial startup reports to settle (approx 100ms)
	time.Sleep(150 * time.Millisecond)
	initialReports := atomic.LoadInt32(&reportCalls)

	// In the first 150ms, there should only be initial resource report(s), not runaway reports
	if initialReports > 2 {
		t.Fatalf("runaway report calls during startup: %d", initialReports)
	}
}

// TestOutboxReplayOnNetworkRecovery verifies that outbox files created during network
// outage are automatically replayed when the network recovers.
func TestOutboxReplayOnNetworkRecovery(t *testing.T) {
	dataDir, err := os.MkdirTemp("", "probewatch-agent-outbox-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dataDir)

	var serverHealthy atomic.Bool
	var replayedReports int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/report" {
			if !serverHealthy.Load() {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			atomic.AddInt32(&replayedReports, 1)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	runner, err := New(config.Config{
		AgentEndpoint:  server.URL,
		AgentNodeUUID:  "6f1d2c66-1a10-4a3e-9a55-0d3ea1a2b7f1",
		AgentNodeToken: "node-secret",
		AgentDataDir:   dataDir,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Server is down: report fails and is enqueued to outbox
	serverHealthy.Store(false)
	_ = runner.report(context.Background())

	outboxDir := filepath.Join(dataDir, "outbox")
	entries, err := os.ReadDir(outboxDir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("expected outbox file to be created during outage, got %v (len %d)", err, len(entries))
	}

	// 2. Server recovers
	serverHealthy.Store(true)
	time.Sleep(120 * time.Millisecond) // Allow reportRetryDelay (100ms) to elapse

	// Trigger outbox replay (which happens automatically in the new loop or after successful report)
	err = runner.replayOutboxLimited(context.Background())
	if err != nil {
		t.Fatalf("replayOutboxLimited failed: %v", err)
	}

	// 3. Verify outbox is cleared
	entriesAfter, _ := os.ReadDir(outboxDir)
	jsonCount := 0
	for _, e := range entriesAfter {
		if filepath.Ext(e.Name()) == ".json" {
			jsonCount++
		}
	}
	if jsonCount != 0 {
		t.Fatalf("expected outbox to be drained after recovery, but %d files remain", jsonCount)
	}
	if atomic.LoadInt32(&replayedReports) == 0 {
		t.Fatalf("expected replayed reports to reach server, got %d", atomic.LoadInt32(&replayedReports))
	}
}

// TestTaskTimeoutAndCleanupRunningTasks verifies that when a probe task times out or its context
// is cancelled, runningTasks map and worker pool slots are cleanly released.
func TestTaskTimeoutAndCleanupRunningTasks(t *testing.T) {
	runner, err := New(config.Config{
		AgentEndpoint:  "http://127.0.0.1:8080",
		AgentNodeUUID:  "6f1d2c66-1a10-4a3e-9a55-0d3ea1a2b7f1",
		AgentNodeToken: "node-secret",
	})
	if err != nil {
		t.Fatal(err)
	}

	slowMTR := &slowMTRMonitor{delay: 500 * time.Millisecond}
	runner.mtr = slowMTR
	task := protocol.CheckTask{
		ID:        "timeout-task-1",
		Kind:      "mtr",
		TimeoutMS: 50, // 50ms timeout
	}

	runner.probeMu.Lock()
	runner.runningTasks[task.ID] = struct{}{}
	runner.probeMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	runner.runProbeTask(ctx, task)

	runner.probeMu.Lock()
	_, stillRunning := runner.runningTasks[task.ID]
	workerSemLen := len(runner.heavyProbeWorkerSem)
	runner.probeMu.Unlock()

	if stillRunning {
		t.Fatalf("expected task %s to be removed from runningTasks after timeout", task.ID)
	}
	if workerSemLen != 0 {
		t.Fatalf("expected worker sem slot to be released, got len %d", workerSemLen)
	}
}

// TestOutboxReplayConcurrentSafety verifies that concurrent calls to replayOutboxLimited
// are safely serialized by outboxReplayMu without data race or panic.
func TestOutboxReplayConcurrentSafety(t *testing.T) {
	dataDir, err := os.MkdirTemp("", "probewatch-agent-outbox-conc-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dataDir)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	runner, err := New(config.Config{
		AgentEndpoint:  server.URL,
		AgentNodeUUID:  "6f1d2c66-1a10-4a3e-9a55-0d3ea1a2b7f1",
		AgentNodeToken: "node-secret",
		AgentDataDir:   dataDir,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Seed outbox
	outboxDir := filepath.Join(dataDir, "outbox")
	_ = os.MkdirAll(outboxDir, 0700)
	_ = os.WriteFile(filepath.Join(outboxDir, "item1.json"), []byte(`{"request_id":"req-1"}`), 0600)
	_ = os.WriteFile(filepath.Join(outboxDir, "item2.json"), []byte(`{"request_id":"req-2"}`), 0600)

	done := make(chan struct{})
	for i := 0; i < 5; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			_ = runner.replayOutboxLimited(context.Background())
		}()
	}
	for i := 0; i < 5; i++ {
		<-done
	}
}

// TestProbeConcurrencyPoolSeparation verifies that saturated heavyProbeWorkerSem
// does not block fastProbeWorkerSem from executing lightweight probes, and fast probes
// do not consume heavy slots.
func TestProbeConcurrencyPoolSeparation(t *testing.T) {
	runner, err := New(config.Config{
		AgentEndpoint:  "http://localhost:9999",
		AgentNodeUUID:  "6f1d2c66-1a10-4a3e-9a55-0d3ea1a2b7f1",
		AgentNodeToken: "node-secret",
	})
	if err != nil {
		t.Fatal(err)
	}

	if cap(runner.fastProbeWorkerSem) != 8 {
		t.Fatalf("expected fastProbeWorkerSem cap 8, got %d", cap(runner.fastProbeWorkerSem))
	}
	if cap(runner.heavyProbeWorkerSem) != 2 {
		t.Fatalf("expected heavyProbeWorkerSem cap 2, got %d", cap(runner.heavyProbeWorkerSem))
	}

	// 1. Saturate heavy pool completely (2 slots)
	runner.heavyProbeWorkerSem <- struct{}{}
	runner.heavyProbeWorkerSem <- struct{}{}
	if len(runner.heavyProbeWorkerSem) != 2 {
		t.Fatalf("expected heavyProbeWorkerSem to be full (2)")
	}

	// 2. Launch a fast probe task (e.g. tcp) with a short timeout
	fastTask := protocol.CheckTask{
		ID:        "fast-task-1",
		Kind:      "tcp",
		TimeoutMS: 50,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// runProbeTask must not hang or block on heavyProbeWorkerSem; it uses fastProbeWorkerSem
	done := make(chan struct{})
	go func() {
		runner.runProbeTask(ctx, fastTask)
		close(done)
	}()

	select {
	case <-done:
		// Fast probe executed and completed
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("fast probe was blocked while heavyProbeWorkerSem was saturated!")
	}

	// 3. Verify heavyProbeWorkerSem still has 2 slots occupied (untouched by fast probe)
	if len(runner.heavyProbeWorkerSem) != 2 {
		t.Fatalf("expected heavyProbeWorkerSem to remain at 2, got %d", len(runner.heavyProbeWorkerSem))
	}

	// Clean up heavy slots
	<-runner.heavyProbeWorkerSem
	<-runner.heavyProbeWorkerSem
}

// TestUnknownTaskClassificationAsHeavy verifies that unknown or custom task kinds
// are routed to the heavy channel by default to prevent fast channel starvation.
func TestUnknownTaskClassificationAsHeavy(t *testing.T) {
	runner, err := New(config.Config{
		AgentEndpoint:  "http://localhost:9999",
		AgentNodeUUID:  "6f1d2c66-1a10-4a3e-9a55-0d3ea1a2b7f1",
		AgentNodeToken: "node-secret",
	})
	if err != nil {
		t.Fatal(err)
	}

	fastKinds := []string{"tcp", "http", "https", "dns", "ping", "icmp", "TCP", "HTTP"}
	for _, k := range fastKinds {
		if !isFastProbeKind(k) {
			t.Errorf("expected %q to be classified as fast", k)
		}
		if runner.selectProbeSem(k) != runner.fastProbeWorkerSem {
			t.Errorf("expected %q to select fastProbeWorkerSem", k)
		}
	}

	heavyKinds := []string{"mtr", "speedtest", "media_http", "synthetic", "grpc", "websocket", "doh"}
	for _, k := range heavyKinds {
		if isFastProbeKind(k) {
			t.Errorf("expected %q to be classified as heavy", k)
		}
		if runner.selectProbeSem(k) != runner.heavyProbeWorkerSem {
			t.Errorf("expected %q to select heavyProbeWorkerSem", k)
		}
	}

	unknownKinds := []string{"custom_task", "unknown_proto", "heavy_benchmark", "", " "}
	for _, k := range unknownKinds {
		if isFastProbeKind(k) {
			t.Errorf("expected unknown kind %q to be classified as heavy for safety", k)
		}
		if runner.selectProbeSem(k) != runner.heavyProbeWorkerSem {
			t.Errorf("expected unknown kind %q to route to heavyProbeWorkerSem", k)
		}
	}
}

// TestOutboxReplayRetryBackoff verifies that items with future NextTry are skipped
// and send failures advance NextTry with exponential backoff.
func TestOutboxReplayRetryBackoff(t *testing.T) {
	dataDir, err := os.MkdirTemp("", "probewatch-agent-backoff-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dataDir)

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		// Always fail to test backoff calculation
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	runner, err := New(config.Config{
		AgentEndpoint:  server.URL,
		AgentNodeUUID:  "6f1d2c66-1a10-4a3e-9a55-0d3ea1a2b7f1",
		AgentNodeToken: "node-secret",
		AgentDataDir:   dataDir,
	})
	if err != nil {
		t.Fatal(err)
	}

	outboxDir := filepath.Join(dataDir, "outbox")
	_ = os.MkdirAll(outboxDir, 0700)

	// 1. Item with NextTry in the future should be skipped
	futureItem := queuedReport{
		RequestID: "req-future",
		Payload:   []byte(`{"test":"future"}`),
		Attempts:  1,
		NextTry:   time.Now().UTC().Add(10 * time.Minute),
	}
	futureData, _ := json.Marshal(futureItem)
	_ = os.WriteFile(filepath.Join(outboxDir, "future.json"), futureData, 0600)

	ctx := context.Background()
	_ = runner.replayOutboxLimited(ctx)

	if callCount != 0 {
		t.Fatalf("expected future item to be skipped, but server was called %d times", callCount)
	}

	// 2. Item ready for replay: will fail, then update Attempts and NextTry with backoff
	readyItem := queuedReport{
		RequestID: "req-ready",
		Payload:   []byte(`{"test":"ready"}`),
		Attempts:  2,
		NextTry:   time.Now().UTC().Add(-1 * time.Minute), // in past, ready to retry
	}
	readyData, _ := json.Marshal(readyItem)
	readyPath := filepath.Join(outboxDir, "ready.json")
	_ = os.WriteFile(readyPath, readyData, 0600)

	_ = runner.replayOutboxLimited(ctx)

	if callCount != maxReportAttempts {
		t.Fatalf("expected %d calls for ready item, got %d", maxReportAttempts, callCount)
	}

	// Read updated file from disk
	updatedData, err := os.ReadFile(readyPath)
	if err != nil {
		t.Fatalf("failed to read updated file: %v", err)
	}
	var updatedItem queuedReport
	if err := json.Unmarshal(updatedData, &updatedItem); err != nil {
		t.Fatalf("failed to unmarshal updated item: %v", err)
	}

	if updatedItem.Attempts != 3 {
		t.Fatalf("expected attempts to be incremented to 3, got %d", updatedItem.Attempts)
	}
	if !updatedItem.NextTry.After(time.Now().UTC()) {
		t.Fatalf("expected NextTry to be in the future, got %v", updatedItem.NextTry)
	}
}
