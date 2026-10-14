package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/config"
	"github.com/probewatch/probewatch/internal/protocol"
)

func setupTestServerWithAuthMode(t *testing.T, strict bool) (http.Handler, string, string) {
	t.Helper()
	service, store := newTask4Auth(t)
	t.Cleanup(func() { store.Close() })

	cfg := config.Config{
		Environment:     "development",
		PublicBaseURL:   "http://127.0.0.1:8080",
		MaxRequestBody:  1 << 20,
		AgentClockSkew:  5 * time.Minute,
		AgentStrictAuth: strict,
	}

	handler := NewServer(cfg, service).Handler()

	regToken, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatalf("create registration token: %v", err)
	}

	nodeUUID := "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d"
	registered := task4Register(t, handler, protocol.RegisterRequest{
		RegistrationToken: regToken.Token,
		NodeUUID:          nodeUUID,
		Name:              "unification-test-node",
	})

	return handler, registered.NodeToken, registered.NodeUUID
}

func TestAgentAuthUnified_NormalWithTimestampAndRequestID(t *testing.T) {
	handler, token, nodeUUID := setupTestServerWithAuthMode(t, true)

	// Test workload endpoint with unified auth
	workload := protocol.NodeWorkloadReport{
		NodeUUID:        nodeUUID,
		ReportedAt:      time.Now().Unix(),
		DockerAvailable: true,
		DockerVersion:   "24.0.5",
	}
	body, _ := json.Marshal(workload)
	req := httptest.NewRequest(http.MethodPost, "/api/agent/v1/workload", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Probe-Timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	req.Header.Set("X-Probe-Request-ID", "req-normal-workload-1")
	req.Header.Set("X-Node-UUID", nodeUUID)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 No Content for normal workload, got %d: %s", rec.Code, rec.Body.String())
	}

	// Test events endpoint with unified auth
	events := protocol.SystemEventBatchReport{
		NodeUUID:   nodeUUID,
		ReportedAt: time.Now().Unix(),
		Events: []protocol.SystemEvent{
			{
				ID:         "evt-test-1",
				Category:   "service",
				Severity:   "info",
				Title:      "test event",
				Message:    "normal test event",
				Source:     "systemd",
				OccurredAt: time.Now().Unix(),
			},
		},
	}
	body, _ = json.Marshal(events)
	req = httptest.NewRequest(http.MethodPost, "/api/agent/v1/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Probe-Timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	req.Header.Set("X-Probe-Request-ID", "req-normal-events-1")
	req.Header.Set("X-Node-UUID", nodeUUID)

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 No Content for normal events, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAgentAuthUnified_TimestampExpiredStrictModeVsPermissive(t *testing.T) {
	expiredTime := time.Now().Add(-10 * time.Minute).Unix()

	// 1. Strict mode should REJECT expired timestamp (> 5 min)
	strictHandler, token, nodeUUID := setupTestServerWithAuthMode(t, true)
	workload := protocol.NodeWorkloadReport{
		NodeUUID:        nodeUUID,
		ReportedAt:      time.Now().Unix(),
		DockerAvailable: true,
	}
	body, _ := json.Marshal(workload)
	req := httptest.NewRequest(http.MethodPost, "/api/agent/v1/workload", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Probe-Timestamp", strconv.FormatInt(expiredTime, 10))
	req.Header.Set("X-Probe-Request-ID", "req-strict-expired-1")

	rec := httptest.NewRecorder()
	strictHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("strict mode expected 401 Unauthorized for expired timestamp, got %d: %s", rec.Code, rec.Body.String())
	}

	// 2. Permissive mode should ALLOW expired timestamp with warning log
	permissiveHandler, token, nodeUUID := setupTestServerWithAuthMode(t, false)
	req = httptest.NewRequest(http.MethodPost, "/api/agent/v1/workload", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Probe-Timestamp", strconv.FormatInt(expiredTime, 10))
	req.Header.Set("X-Probe-Request-ID", "req-permissive-expired-1")

	rec = httptest.NewRecorder()
	permissiveHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("permissive mode expected 204 No Content for expired timestamp, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAgentAuthUnified_ReplayProtectionRejectsDuplicates(t *testing.T) {
	handler, token, nodeUUID := setupTestServerWithAuthMode(t, true)

	workload := protocol.NodeWorkloadReport{
		NodeUUID:        nodeUUID,
		ReportedAt:      time.Now().Unix(),
		DockerAvailable: true,
	}
	body, _ := json.Marshal(workload)

	// First request with requestID
	req1 := httptest.NewRequest(http.MethodPost, "/api/agent/v1/workload", bytes.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("Authorization", "Bearer "+token)
	req1.Header.Set("X-Probe-Timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	req1.Header.Set("X-Probe-Request-ID", "replay-dup-test-id")

	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusNoContent {
		t.Fatalf("expected 204 No Content on first attempt, got %d: %s", rec1.Code, rec1.Body.String())
	}

	// Second request with exact same requestID (replay attack simulation)
	req2 := httptest.NewRequest(http.MethodPost, "/api/agent/v1/workload", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+token)
	req2.Header.Set("X-Probe-Timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	req2.Header.Set("X-Probe-Request-ID", "replay-dup-test-id")

	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict on replayed request ID, got %d: %s", rec2.Code, rec2.Body.String())
	}
}

func TestAgentAuthUnified_LegacyRequestsWithoutTimestampPassInPermissiveMode(t *testing.T) {
	// 1. Permissive mode (default): legacy requests without timestamp pass smoothly
	permissiveHandler, token, nodeUUID := setupTestServerWithAuthMode(t, false)

	workload := protocol.NodeWorkloadReport{
		NodeUUID:        nodeUUID,
		ReportedAt:      time.Now().Unix(),
		DockerAvailable: true,
	}
	body, _ := json.Marshal(workload)
	reqLegacy := httptest.NewRequest(http.MethodPost, "/api/agent/v1/workload", bytes.NewReader(body))
	reqLegacy.Header.Set("Content-Type", "application/json")
	reqLegacy.Header.Set("Authorization", "Bearer "+token)
	// Notice: NO X-Probe-Timestamp, NO X-Probe-Request-ID

	recLegacy := httptest.NewRecorder()
	permissiveHandler.ServeHTTP(recLegacy, reqLegacy)
	if recLegacy.Code != http.StatusNoContent {
		t.Fatalf("legacy request without timestamp should pass in permissive mode, got %d: %s", recLegacy.Code, recLegacy.Body.String())
	}

	// 2. Strict mode: legacy requests without timestamp are rejected
	strictHandler, token, _ := setupTestServerWithAuthMode(t, true)
	reqStrict := httptest.NewRequest(http.MethodPost, "/api/agent/v1/workload", bytes.NewReader(body))
	reqStrict.Header.Set("Content-Type", "application/json")
	reqStrict.Header.Set("Authorization", "Bearer "+token)

	recStrict := httptest.NewRecorder()
	strictHandler.ServeHTTP(recStrict, reqStrict)
	if recStrict.Code != http.StatusUnauthorized {
		t.Fatalf("legacy request without timestamp should be rejected in strict mode, got %d: %s", recStrict.Code, recStrict.Body.String())
	}
}

func TestAgentAuthUnified_NodeUUIDMismatch(t *testing.T) {
	handler, token, _ := setupTestServerWithAuthMode(t, false)

	req := httptest.NewRequest(http.MethodGet, "/api/agent/v1/config", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Node-UUID", "00000000-0000-0000-0000-000000000000") // Mismatched UUID

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict on X-Node-UUID mismatch, got %d: %s", rec.Code, rec.Body.String())
	}
}
