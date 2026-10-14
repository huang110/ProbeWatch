package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
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
