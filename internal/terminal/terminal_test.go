package terminal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestWebSocketEcho(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := Upgrade(w, r)
		if err != nil {
			t.Errorf("upgrade error: %v", err)
			return
		}
		defer conn.Close()

		op, msg, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("read error: %v", err)
			return
		}
		if op != OpText || string(msg) != "hello probewatch" {
			t.Errorf("unexpected message: %s", string(msg))
			return
		}

		if err := conn.WriteMessage(OpText, []byte("echo: "+string(msg))); err != nil {
			t.Errorf("write error: %v", err)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	clientConn, err := Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial error: %v", err)
	}
	defer clientConn.Close()

	if err := clientConn.WriteMessage(OpText, []byte("hello probewatch")); err != nil {
		t.Fatalf("client write error: %v", err)
	}

	op, resp, err := clientConn.ReadMessage()
	if err != nil {
		t.Fatalf("client read error: %v", err)
	}
	if op != OpText || string(resp) != "echo: hello probewatch" {
		t.Fatalf("unexpected client response: %s", string(resp))
	}
}

func TestRunExec(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var cmd string
	if runtime.GOOS == "windows" {
		cmd = "echo probewatch_test"
	} else {
		cmd = "echo probewatch_test"
	}

	res := RunExec(ctx, cmd, 5)
	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d, stderr: %s", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "probewatch_test") {
		t.Fatalf("expected stdout to contain probewatch_test, got: %s", res.Stdout)
	}
	if res.DurationMS < 0 {
		t.Fatalf("invalid duration ms: %d", res.DurationMS)
	}
}

func TestRunExecTimeout(t *testing.T) {
	ctx := context.Background()

	var cmd string
	if runtime.GOOS == "windows" {
		cmd = "powershell -Command Start-Sleep -Seconds 5"
	} else {
		cmd = "sleep 5"
	}

	res := RunExec(ctx, cmd, 1)
	if res.ExitCode == 0 {
		t.Fatalf("expected non-zero exit code on timeout, got 0")
	}
}

func TestManagerExecuteCommand(t *testing.T) {
	mgr := NewManager()
	nodeUUID := "test-node-uuid-1"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := Upgrade(w, r)
		if err != nil {
			t.Errorf("upgrade error: %v", err)
			return
		}
		mgr.RegisterTunnel(nodeUUID, conn)
		defer mgr.UnregisterTunnel(nodeUUID)

		for {
			var env Envelope
			if err := conn.ReadJSON(&env); err != nil {
				return
			}
			mgr.HandleTunnelMessage(nodeUUID, env)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	clientConn, err := Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer clientConn.Close()

	// Mock agent client loop
	go func() {
		for {
			var env Envelope
			if err := clientConn.ReadJSON(&env); err != nil {
				return
			}
			if env.Type == TypeExec {
				_ = clientConn.WriteJSON(Envelope{
					Type:       TypeExecResult,
					ExecID:     env.ExecID,
					Stdout:     "output of " + env.Command,
					ExitCode:   0,
					DurationMS: 12,
				})
			}
		}
	}()

	// Wait briefly for tunnel registration
	time.Sleep(50 * time.Millisecond)

	if !mgr.IsNodeConnected(nodeUUID) {
		t.Fatalf("expected node to be connected")
	}

	res, err := mgr.ExecuteCommand(ctx, nodeUUID, "uptime", 5)
	if err != nil {
		t.Fatalf("execute command: %v", err)
	}
	if res.Stdout != "output of uptime" {
		t.Fatalf("unexpected stdout: %s", res.Stdout)
	}

	mgr.UnregisterTunnel(nodeUUID)
	if mgr.IsNodeConnected(nodeUUID) {
		t.Fatalf("expected node to be disconnected after unregister")
	}
}

func TestAgentClientIntegration(t *testing.T) {
	nodeUUID := "agent-uuid-42"
	receivedResult := make(chan Envelope, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Node-UUID") != nodeUUID {
			t.Errorf("wrong node uuid: %s", r.Header.Get("X-Node-UUID"))
		}
		conn, err := Upgrade(w, r)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()

		// Send exec command to agent
		_ = conn.WriteJSON(Envelope{
			Type:       TypeExec,
			ExecID:     "exec-test-1",
			Command:    "echo test_tunnel_ok",
			TimeoutSec: 5,
		})

		// Read response
		var env Envelope
		if err := conn.ReadJSON(&env); err == nil {
			receivedResult <- env
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := NewClient(server.URL, nodeUUID, "test-token")
	go client.Run(ctx)

	select {
	case <-ctx.Done():
		t.Fatalf("timeout waiting for client exec result")
	case res := <-receivedResult:
		if res.Type != TypeExecResult {
			t.Fatalf("expected TypeExecResult, got: %s", res.Type)
		}
		if res.ExitCode != 0 {
			t.Fatalf("expected exit code 0, got %d, stderr: %s", res.ExitCode, res.Stderr)
		}
		if !strings.Contains(res.Stdout, "test_tunnel_ok") {
			t.Fatalf("expected stdout containing test_tunnel_ok, got: %s", res.Stdout)
		}
	}
}

