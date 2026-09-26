package monitor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/protocol"
)

func TestSpeedtestRunnerSuccess(t *testing.T) {
	var headCount int64
	var getCount int64
	var postCount int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			atomic.AddInt64(&headCount, 1)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			atomic.AddInt64(&getCount, 1)
			w.Header().Set("Content-Type", "application/octet-stream")
			// Return 1MB of data
			data := make([]byte, 64*1024)
			for i := 0; i < 16; i++ {
				_, _ = w.Write(data)
			}
		case http.MethodPost:
			atomic.AddInt64(&postCount, 1)
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	runner := &SpeedtestRunner{
		Client: server.Client(),
	}

	task := protocol.CheckTask{
		ID:            "speed-task-1",
		Kind:          "speedtest",
		Host:          "mock-speedtest",
		Port:          80,
		ServerURL:     server.URL,
		DownloadBytes: 512 * 1024,
		UploadBytes:   256 * 1024,
		TimeoutMS:     5000,
	}

	res := runner.Run(context.Background(), task)
	if res.Status != "ok" {
		t.Fatalf("expected status ok, got %q (err: %s)", res.Status, res.Error)
	}
	if res.BytesReceived < 512*1024 {
		t.Errorf("expected at least 512KB received, got %d", res.BytesReceived)
	}
	if res.BytesSent < 256*1024 {
		t.Errorf("expected at least 256KB sent, got %d", res.BytesSent)
	}
	if res.DownloadSpeedMbps <= 0 {
		t.Errorf("expected positive download speed, got %f", res.DownloadSpeedMbps)
	}
	if res.UploadSpeedMbps <= 0 {
		t.Errorf("expected positive upload speed, got %f", res.UploadSpeedMbps)
	}
	if err := res.Validate(); err != nil {
		t.Fatalf("result validation failed: %v", err)
	}
}

func TestSpeedtestRunnerInvalidURL(t *testing.T) {
	runner := &SpeedtestRunner{}
	task := protocol.CheckTask{
		ID:        "speed-task-2",
		Kind:      "speedtest",
		ServerURL: "ftp://invalid-scheme.example.com",
	}

	res := runner.Run(context.Background(), task)
	if res.Status != "error" {
		t.Fatalf("expected error status for invalid scheme, got %q", res.Status)
	}
	if !strings.Contains(res.Error, "invalid speedtest target URL scheme") {
		t.Fatalf("unexpected error message: %q", res.Error)
	}
}

func TestSpeedtestRunnerUnreachable(t *testing.T) {
	runner := &SpeedtestRunner{
		Timeout: 200 * time.Millisecond,
	}
	task := protocol.CheckTask{
		ID:        "speed-task-3",
		Kind:      "speedtest",
		ServerURL: "http://127.0.0.1:59999/unreachable",
		TimeoutMS: 200,
	}

	res := runner.Run(context.Background(), task)
	if res.Status != "error" {
		t.Fatalf("expected error for unreachable server, got %q", res.Status)
	}
}
