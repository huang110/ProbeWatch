package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/terminal"
)

func TestTerminalStatusUnauthorized(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	server := NewServer(task4Config(), service)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/terminal/status", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", w.Code)
	}
}

func TestTerminalStatusAuthorized(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	server := NewServer(task4Config(), service)
	handler := server.Handler()
	session, _ := task4AdminSession(t, service, store)

	node := publicStatusRegisterNode(t, handler, store, task4NodeUUID1, "term-test-node")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/terminal/status", nil)
	req.AddCookie(task4SessionCookie(session))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp struct {
		Enabled bool `json:"enabled"`
		Nodes   []struct {
			ID             string `json:"id"`
			UUID           string `json:"uuid"`
			Name           string `json:"name"`
			TerminalOnline bool   `json:"terminal_online"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}
	if !resp.Enabled {
		t.Fatalf("expected enabled to be true")
	}
	if len(resp.Nodes) != 1 || resp.Nodes[0].ID != node.ID {
		t.Fatalf("expected 1 node with id %s, got: %+v", node.ID, resp.Nodes)
	}
	if resp.Nodes[0].TerminalOnline {
		t.Fatalf("expected terminal to be offline initially")
	}
}

func TestTerminalExecOffline(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	server := NewServer(task4Config(), service)
	handler := server.Handler()
	session, csrf := task4AdminSession(t, service, store)

	node := publicStatusRegisterNode(t, handler, store, task4NodeUUID2, "term-offline-node")

	body, _ := json.Marshal(map[string]any{
		"node_id": node.ID,
		"command": "uptime",
	})
	resp, _ := task4AdminPost(t, handler, session, csrf, "/api/admin/terminal/exec", string(body))

	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503 for offline node, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestTerminalExecDisabled(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	cfg.EnableRemoteTerminal = false
	server := NewServer(cfg, service)
	handler := server.Handler()
	session, csrf := task4AdminSession(t, service, store)

	node := publicStatusRegisterNode(t, handler, store, task4NodeUUID3, "term-disabled-node")

	body, _ := json.Marshal(map[string]any{
		"node_id": node.ID,
		"command": "uptime",
	})
	resp, _ := task4AdminPost(t, handler, session, csrf, "/api/admin/terminal/exec", string(body))

	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503 for disabled terminal, got %d", resp.Code)
	}
	if !strings.Contains(resp.Body.String(), "disabled") {
		t.Fatalf("expected disabled message, got: %s", resp.Body.String())
	}
}

func TestTerminalExecSuccess(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	server := NewServer(task4Config(), service)
	handler := server.Handler()
	session, csrf := task4AdminSession(t, service, store)

	node := publicStatusRegisterNode(t, handler, store, task4NodeUUID4, "term-live-node")

	// Spin up mock agent server to establish tunnel
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := terminal.Upgrade(w, r)
		if err != nil {
			return
		}
		// Register as tunnel in server
		server.terminalManager.RegisterTunnel(node.UUID, conn)
		defer server.terminalManager.UnregisterTunnel(node.UUID)

		for {
			var env terminal.Envelope
			if err := conn.ReadJSON(&env); err != nil {
				return
			}
			server.terminalManager.HandleTunnelMessage(node.UUID, env)
		}
	}))
	defer mockServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(mockServer.URL, "http")
	clientConn, err := terminal.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial mock agent: %v", err)
	}
	defer clientConn.Close()

	// Mock agent handling of TypeExec
	go func() {
		for {
			var env terminal.Envelope
			if err := clientConn.ReadJSON(&env); err != nil {
				return
			}
			if env.Type == terminal.TypeExec {
				_ = clientConn.WriteJSON(terminal.Envelope{
					Type:       terminal.TypeExecResult,
					ExecID:     env.ExecID,
					Stdout:     "Linux probewatch-node 6.1.0\n",
					ExitCode:   0,
					DurationMS: 42,
				})
			}
		}
	}()

	time.Sleep(50 * time.Millisecond)

	body, _ := json.Marshal(map[string]any{
		"node_id":     node.ID,
		"command":     "uname -a",
		"timeout_sec": 5,
	})
	resp, _ := task4AdminPost(t, handler, session, csrf, "/api/admin/terminal/exec", string(body))

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", resp.Code, resp.Body.String())
	}

	var res struct {
		ExitCode   int    `json:"exit_code"`
		Stdout     string `json:"stdout"`
		DurationMS int64  `json:"duration_ms"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", res.ExitCode)
	}
	if !strings.Contains(res.Stdout, "probewatch-node") {
		t.Fatalf("unexpected stdout: %s", res.Stdout)
	}

	// Verify audit event was logged
	var auditCount int
	auditCount, err = store.CountAuditEvents(context.Background(), "terminal_exec", node.ID)
	if err != nil || auditCount != 1 {
		t.Fatalf("expected 1 audit event for terminal_exec, got %d (err: %v)", auditCount, err)
	}
}
