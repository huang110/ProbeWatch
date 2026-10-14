package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestDockerConcurrentCollectionAndTimeout(t *testing.T) {
	// Create a mock Unix domain socket HTTP server simulating Docker daemon
	tmpDir, err := os.MkdirTemp("", "docker-mock-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	sockPath := filepath.Join(tmpDir, "docker.sock")
	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Skipf("cannot listen on unix socket on this platform: %v", err)
	}
	defer listener.Close()

	var activeStatsRequests int32
	var maxObservedConcurrency int32

	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/v1.24/version":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(dockerEngineVersion{Version: "24.0.5"})

			case r.URL.Path == "/v1.24/containers/json":
				// Return 6 running containers
				var list []dockerContainerListItem
				for i := 1; i <= 6; i++ {
					list = append(list, dockerContainerListItem{
						ID:      fmt.Sprintf("container-%d", i),
						Names:   []string{fmt.Sprintf("/worker-%d", i)},
						Image:   "worker:latest",
						State:   "running",
						Status:  "Up 2 hours",
					})
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(list)

			case len(r.URL.Path) > len("/v1.24/containers/") && r.URL.Path[len(r.URL.Path)-len("/stats"):] == "/stats":
				cur := atomic.AddInt32(&activeStatsRequests, 1)
				for {
					oldMax := atomic.LoadInt32(&maxObservedConcurrency)
					if cur <= oldMax || atomic.CompareAndSwapInt32(&maxObservedConcurrency, oldMax, cur) {
						break
					}
				}

				// If container-3, simulate a slow/hanging container that times out
				if r.URL.Path == "/v1.24/containers/container-3/stats" {
					time.Sleep(3 * time.Second)
				} else {
					time.Sleep(50 * time.Millisecond)
				}

				atomic.AddInt32(&activeStatsRequests, -1)

				var stats dockerContainerStats
				stats.MemoryStats.Usage = 100000000
				stats.MemoryStats.Limit = 500000000
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(stats)

			default:
				http.NotFound(w, r)
			}
		}),
	}
	go server.Serve(listener)
	defer server.Close()

	avail, ver, snapshots, err := CollectDockerWorkloadFromSocket(sockPath)
	if err != nil {
		t.Fatalf("CollectDockerWorkloadFromSocket failed: %v", err)
	}
	if !avail {
		t.Fatal("expected docker available=true")
	}
	if ver != "24.0.5" {
		t.Fatalf("expected version 24.0.5, got %q", ver)
	}
	if len(snapshots) != 6 {
		t.Fatalf("expected 6 snapshots, got %d", len(snapshots))
	}

	// Verify concurrency did not exceed the limit of 4
	maxConc := atomic.LoadInt32(&maxObservedConcurrency)
	if maxConc > 4 {
		t.Fatalf("max concurrent stats requests %d exceeded limit of 4", maxConc)
	}

	// Verify order is preserved: container-1 through container-6
	for i := 1; i <= 6; i++ {
		expectedID := fmt.Sprintf("container-%d", i)
		if snapshots[i-1].ID != expectedID {
			t.Errorf("snapshot[%d].ID = %q, want %q", i-1, snapshots[i-1].ID, expectedID)
		}
	}

	// Verify container-3 (slow/timeout) did not fail the entire batch or other containers
	if snapshots[0].MemoryUsageBytes != 100000000 {
		t.Errorf("container-1 stats missing or zero: %d", snapshots[0].MemoryUsageBytes)
	}
	if snapshots[1].MemoryUsageBytes != 100000000 {
		t.Errorf("container-2 stats missing or zero: %d", snapshots[1].MemoryUsageBytes)
	}
	// container-3 timed out, so memory should be 0, but basic snapshot metadata is intact
	if snapshots[2].ID != "container-3" {
		t.Errorf("container-3 metadata missing: %v", snapshots[2])
	}
}
