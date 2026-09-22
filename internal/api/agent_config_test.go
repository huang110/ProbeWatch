package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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

func TestAgentConfigServesConfigVersionAndMaxAge(t *testing.T) {
	handler, store, token, _ := newAgentConfigFixture(t)
	now := time.Now().UTC()

	payload, err := json.Marshal(agentTargetPayload{Port: 443, Path: "/", TimeoutMS: 2000, IntervalSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTarget(context.Background(), db.TargetDefinition{ID: "tcp-version-1", Name: "TCP versioned", Kind: db.TargetKindTCP, Host: "example.com", Enabled: true, Payload: payload}, now); err != nil {
		t.Fatal(err)
	}

	before := time.Now().UTC().Add(-time.Second).Unix()
	response := agentConfigGet(t, handler, token)
	after := time.Now().UTC().Add(time.Second).Unix()
	if response.Code != http.StatusOK {
		t.Fatalf("agent config status = %d, body = %q", response.Code, response.Body.String())
	}
	var decoded protocol.AgentConfigResponse
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ConfigMaxAgeSeconds != 1800 {
		t.Fatalf("config_max_age_seconds = %d, want default 1800", decoded.ConfigMaxAgeSeconds)
	}
	if decoded.ConfigVersion < before || decoded.ConfigVersion > after {
		t.Fatalf("config_version = %d, want server unix seconds within [%d, %d]", decoded.ConfigVersion, before, after)
	}
	if err := decoded.Validate(); err != nil {
		t.Fatalf("served config must pass strict validation: %v", err)
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

func TestAgentConfigServesMediaRegionRulesAndValidates(t *testing.T) {
	handler, store, token, _ := newAgentConfigFixture(t)
	now := time.Now().UTC()

	rules := []protocol.RegionRule{{Region: "SG", Contains: "geo-SG"}, {Region: "US", Contains: "edge=us-west"}}
	mediaPayload, err := json.Marshal(agentTargetPayload{Port: 443, Path: "/manifest", TimeoutMS: 3000, IntervalSeconds: 60, RegionRules: rules})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTarget(context.Background(), db.TargetDefinition{ID: "media-region-1", Name: "Media SG", Kind: db.TargetKindMediaHTTP, Host: "media.example.com", Enabled: true, Payload: mediaPayload}, now); err != nil {
		t.Fatal(err)
	}
	legacyPayload, err := json.Marshal(agentTargetPayload{Port: 443, Path: "/manifest", TimeoutMS: 3000, IntervalSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTarget(context.Background(), db.TargetDefinition{ID: "media-legacy-1", Name: "Media legacy", Kind: db.TargetKindMediaHTTP, Host: "legacy.example.com", Enabled: true, Payload: legacyPayload}, now); err != nil {
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
	if err := payload.Validate(); err != nil {
		t.Fatalf("agent config with region rules must pass strict validation: %v", err)
	}
	byID := make(map[string]protocol.CheckTask, len(payload.Tasks))
	for _, task := range payload.Tasks {
		byID[task.ID] = task
	}
	withRules, ok := byID["media-region-1"]
	if !ok {
		t.Fatal("media target with rules missing from agent config")
	}
	if len(withRules.RegionRules) != 2 || withRules.RegionRules[0].Region != "SG" || withRules.RegionRules[1].Contains != "edge=us-west" {
		t.Fatalf("served region rules = %#v", withRules.RegionRules)
	}
	if withRules.Kind != "media_http" || withRules.Host != "media.example.com" || withRules.Path != "/manifest" {
		t.Fatalf("media task mapping = %#v", withRules)
	}

	legacy, ok := byID["media-legacy-1"]
	if !ok {
		t.Fatal("legacy media target missing from agent config")
	}
	if legacy.RegionRules != nil {
		t.Fatalf("legacy task rules = %#v, want none", legacy.RegionRules)
	}
	if strings.Contains(response.Body.String(), `"region_rules":[{"region":"US"`) && strings.Count(response.Body.String(), "region_rules") != 1 {
		t.Fatalf("legacy task leaked region_rules: %s", response.Body.String())
	}
}

func TestAgentConfigRejectsUnvalidatableMediaPayload(t *testing.T) {
	handler, store, token, _ := newAgentConfigFixture(t)
	now := time.Now().UTC()

	payload := []byte(`{"port":443,"path":"/manifest","timeout_ms":3000,"interval_seconds":60,"region_rules":[{"region":"SG US","contains":"geo-SG"}]}`)
	if _, err := store.CreateTarget(context.Background(), db.TargetDefinition{ID: "media-bad-1", Name: "Media bad", Kind: db.TargetKindMediaHTTP, Host: "media.example.com", Enabled: true, Payload: payload}, now); err != nil {
		t.Fatal(err)
	}

	response := agentConfigGet(t, handler, token)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("agent config status = %d %q, want 503 for unvalidatable payload", response.Code, response.Body.String())
	}
}
