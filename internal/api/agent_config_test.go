package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/protocol"
)

func newAgentConfigFixture(t *testing.T) (http.Handler, *db.Store, string, string) {
	t.Helper()
	service, store := newTask4Auth(t)
	t.Cleanup(func() { _ = store.Close() })
	session, csrf := task4AdminSession(t, service, store)
	handler := NewServer(task4Config(), service).Handler()
	registration, _ := task4CreateRegistration(t, handler, session, csrf)
	response := task4Register(t, handler, protocol.RegisterRequest{
		RegistrationToken: registration.Token,
		NodeUUID:          "6f1d2c66-1a10-4a3e-9a55-0d3ea1a2b7f1",
		Name:              "config-node",
	})
	return handler, store, response.NodeToken, response.NodeUUID
}

func agentConfigGet(t *testing.T, handler http.Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/agent/v1/config", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-Probe-Timestamp", strconv.FormatInt(time.Now().UTC().Unix(), 10))
	request.Header.Set("X-Probe-Request-ID", "config-request-1")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestAgentConfigReturnsOnlyEnabledTargetsAsTasks(t *testing.T) {
	handler, store, token, _ := newAgentConfigFixture(t)
	now := time.Now().UTC()

	networkPayload, err := json.Marshal(agentTargetPayload{Port: 443, Path: "/health", TimeoutMS: 2000, IntervalSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTarget(context.Background(), db.TargetDefinition{ID: "tcp-1", Name: "TCP 443", Kind: db.TargetKindTCP, Host: "example.com", Enabled: true, Payload: networkPayload}, now); err != nil {
		t.Fatal(err)
	}
	disabledPayload, err := json.Marshal(agentTargetPayload{Port: 80, Path: "/", TimeoutMS: 2000, IntervalSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTarget(context.Background(), db.TargetDefinition{ID: "tcp-2", Name: "Disabled", Kind: db.TargetKindTCP, Host: "example.com", Enabled: false, Payload: disabledPayload}, now); err != nil {
		t.Fatal(err)
	}
	mtrPayload, err := json.Marshal(agentTargetPayload{Host: "1.1.1.1", MaxHops: 20, TimeoutMS: 5000, IntervalSeconds: 3600})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTarget(context.Background(), db.TargetDefinition{ID: "mtr-1", Name: "MTR Cloudflare", Kind: db.TargetKindMTR, Host: "1.1.1.1", Enabled: true, Payload: mtrPayload}, now); err != nil {
		t.Fatal(err)
	}

	response := agentConfigGet(t, handler, token)
	if response.Code != http.StatusOK {
		t.Fatalf("agent config status = %d, body = %q", response.Code, response.Body.String())
	}
	var payload protocol.AgentConfigResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Tasks) != 2 {
		t.Fatalf("task count = %d (%+v), want only the 2 enabled targets", len(payload.Tasks), payload.Tasks)
	}
	byID := make(map[string]protocol.CheckTask, len(payload.Tasks))
	for _, task := range payload.Tasks {
		byID[task.ID] = task
	}
	tcp, ok := byID["tcp-1"]
	if !ok {
		t.Fatal("enabled tcp target missing from agent config")
	}
	if tcp.Kind != "tcp" || tcp.Host != "example.com" || tcp.Port != 443 || tcp.Path != "/health" || tcp.TimeoutMS != 2000 || tcp.IntervalSeconds != 60 || !tcp.Enabled {
		t.Fatalf("tcp task mapping = %#v", tcp)
	}
	mtr, ok := byID["mtr-1"]
	if !ok {
		t.Fatal("enabled mtr target missing from agent config")
	}
	if mtr.Kind != "mtr" || mtr.Host != "1.1.1.1" || mtr.MaxHops != 20 || mtr.IntervalSeconds != 3600 {
		t.Fatalf("mtr task mapping = %#v", mtr)
	}
	if err := payload.Validate(); err != nil {
		t.Fatalf("agent config response must pass strict validation: %v", err)
	}
}

func TestAgentConfigRejectsWrongMethodAndMissingAuth(t *testing.T) {
	handler, _, token, _ := newAgentConfigFixture(t)

	post := httptest.NewRequest(http.MethodPost, "/api/agent/v1/config", nil)
	post.Header.Set("Authorization", "Bearer "+token)
	post.Header.Set("X-Probe-Timestamp", strconv.FormatInt(time.Now().UTC().Unix(), 10))
	post.Header.Set("X-Probe-Request-ID", "config-request-2")
	postRecorder := httptest.NewRecorder()
	handler.ServeHTTP(postRecorder, post)
	if postRecorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST agent config status = %d, want 405", postRecorder.Code)
	}

	unauthenticated := agentConfigGet(t, handler, "not-a-real-token")
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated agent config status = %d, want 401", unauthenticated.Code)
	}
}
