package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/ai"
)

func TestAIDiagnoseEndpoint(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	server := NewServer(cfg, service)
	handler := server.Handler()

	ctx := context.Background()
	now := time.Now().UTC()

	// 1. Unauthorized request
	req := httptest.NewRequest(http.MethodGet, "/api/ai/diagnose", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", w.Code)
	}

	// 2. Authorized request
	adminUser, err := store.UpsertAdminUser(ctx, "local", "admin", "admin", now)
	if err != nil {
		t.Fatal(err)
	}
	sessionVal, err := service.CreateSessionForUser(ctx, adminUser.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	cookie := &http.Cookie{Name: "probewatch_session", Value: sessionVal}

	authReq := httptest.NewRequest(http.MethodGet, "/api/ai/diagnose", nil)
	authReq.AddCookie(cookie)
	authW := httptest.NewRecorder()
	handler.ServeHTTP(authW, authReq)

	if authW.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", authW.Code, authW.Body.String())
	}

	var report ai.DiagnosisReport
	if err := json.NewDecoder(authW.Body).Decode(&report); err != nil {
		t.Fatalf("failed to decode report: %v", err)
	}

	if report.HealthScore <= 0 || report.HealthStatus == "" {
		t.Fatalf("invalid report fields: %+v", report)
	}
}

func TestAIChatEndpoint(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	server := NewServer(cfg, service)
	handler := server.Handler()

	ctx := context.Background()
	now := time.Now().UTC()

	adminUser, err := store.UpsertAdminUser(ctx, "local", "admin", "admin", now)
	if err != nil {
		t.Fatal(err)
	}
	sessionVal, err := service.CreateSessionForUser(ctx, adminUser.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	cookie := &http.Cookie{Name: "probewatch_session", Value: sessionVal}

	chatPayload := []byte(`{"prompt":"分析全站网络延迟与丢包根因"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/ai/chat", bytes.NewReader(chatPayload))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var chatResp ai.AIChatResponse
	if err := json.NewDecoder(w.Body).Decode(&chatResp); err != nil {
		t.Fatalf("failed to decode chat response: %v", err)
	}

	if chatResp.Reply == "" || !strings.Contains(chatResp.Reply, "网络") {
		t.Fatalf("expected network analysis reply, got: %s", chatResp.Reply)
	}
}

func TestMCPConfigAndProtocolEndpoint(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	server := NewServer(cfg, service)
	handler := server.Handler()

	ctx := context.Background()
	now := time.Now().UTC()

	adminUser, err := store.UpsertAdminUser(ctx, "local", "admin", "admin", now)
	if err != nil {
		t.Fatal(err)
	}
	sessionVal, err := service.CreateSessionForUser(ctx, adminUser.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	cookie := &http.Cookie{Name: "probewatch_session", Value: sessionVal}

	// 1. Get MCP Config
	cfgReq := httptest.NewRequest(http.MethodGet, "/api/mcp/config", nil)
	cfgReq.AddCookie(cookie)
	cfgW := httptest.NewRecorder()
	handler.ServeHTTP(cfgW, cfgReq)

	if cfgW.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", cfgW.Code, cfgW.Body.String())
	}

	var mcpCfg struct {
		Token    string `json:"token"`
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(cfgW.Body).Decode(&mcpCfg); err != nil {
		t.Fatalf("decode mcp config: %v", err)
	}
	if mcpCfg.Token == "" || !strings.Contains(mcpCfg.Endpoint, "/mcp") {
		t.Fatalf("invalid mcp config: %+v", mcpCfg)
	}

	// 2. Test MCP Protocol Unauthenticated
	unauthReq := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	unauthW := httptest.NewRecorder()
	handler.ServeHTTP(unauthW, unauthReq)
	if unauthW.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for /mcp without token, got %d", unauthW.Code)
	}

	// 3. Test MCP Protocol Authenticated GET with Token
	authGetReq := httptest.NewRequest(http.MethodGet, "/mcp?token="+mcpCfg.Token, nil)
	authGetW := httptest.NewRecorder()
	handler.ServeHTTP(authGetW, authGetReq)
	if authGetW.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for /mcp?token, got %d: %s", authGetW.Code, authGetW.Body.String())
	}

	// 4. Test MCP Protocol JSON-RPC POST (initialize)
	initJSON := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	initReq := httptest.NewRequest(http.MethodPost, "/mcp?token="+mcpCfg.Token, bytes.NewReader(initJSON))
	initReq.Header.Set("Content-Type", "application/json")
	initW := httptest.NewRecorder()
	handler.ServeHTTP(initW, initReq)

	if initW.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for /mcp POST initialize, got %d: %s", initW.Code, initW.Body.String())
	}

	var jsonRpcResp ai.JSONRPCResponse
	if err := json.NewDecoder(initW.Body).Decode(&jsonRpcResp); err != nil {
		t.Fatalf("failed to decode json-rpc resp: %v", err)
	}
	if jsonRpcResp.Error != nil {
		t.Fatalf("unexpected json-rpc error: %+v", jsonRpcResp.Error)
	}
}
