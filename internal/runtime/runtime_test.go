package runtime

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/config"
)

func TestControlPlaneServesAndShutsDownWithContext(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- StartControlPlaneContext(ctx, config.Config{
			Environment:    "development",
			ListenAddress:  address,
			DatabasePath:   filepath.Join(t.TempDir(), "probe.db"),
			PublicBaseURL:  "http://" + address,
			SessionSecret:  "test-session-secret-that-is-long-enough",
			MaxRequestBody: 1024,
		})
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		response, requestErr := http.Get("http://" + address + "/healthz")
		if requestErr == nil {
			_ = response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("health status = %d, want 200", response.StatusCode)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("control plane did not become ready: %v", requestErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("control plane shutdown error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("control plane did not shut down")
	}
}
