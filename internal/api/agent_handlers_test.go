package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/auth"
	"github.com/probewatch/probewatch/internal/config"
	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/protocol"
)

const (
	task4NodeUUID1 = "550e8400-e29b-41d4-a716-446655440000"
	task4NodeUUID2 = "550e8400-e29b-41d4-a716-446655440001"
	task4NodeUUID3 = "550e8400-e29b-41d4-a716-446655440002"
	task4NodeUUID4 = "550e8400-e29b-41d4-a716-446655440003"
	task4NodeUUID5 = "550e8400-e29b-41d4-a716-446655440004"
	task4NodeUUID6 = "550e8400-e29b-41d4-a716-446655440005"
)

func TestAgentRegistrationConsumesTokenAndPersistsLatestReport(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	server := NewServer(cfg, service)
	handler := server.Handler()
	session, csrf := task4AdminSession(t, service, store)

	registration, csrf := task4CreateRegistration(t, handler, session, csrf)
	registerBody := protocol.RegisterRequest{
		RegistrationToken: registration.Token,
		NodeUUID:          task4NodeUUID1,
		Name:              "Task 4 node",
		System:            &protocol.RegistrationSystem{OS: "linux", Arch: "amd64", Hostname: "task4-host", AgentVersion: "test"},
	}
	registered := task4Register(t, handler, registerBody)
	if registered.NodeToken == "" || registered.NodeUUID != registerBody.NodeUUID {
		t.Fatalf("registration response = %#v", registered)
	}
	nodes, err := store.ListNodes(context.Background())
	if err != nil || len(nodes) != 1 {
		t.Fatalf("nodes = %#v, err = %v", nodes, err)
	}
	if err := store.CreateNetworkTarget(context.Background(), db.ResultTargetInput{ID: "target-1", Name: "target-1", Kind: "tcp", Host: "example.com"}, time.Now()); err != nil {
		t.Fatal(err)
	}

	report := protocol.ReportRequest{
		NodeUUID:   registerBody.NodeUUID,
		ReportedAt: time.Now().Unix(),
		Resource:   protocol.ResourceSnapshot{Hostname: "task4-host", CPUPercent: 12.5},
		Results: []protocol.CheckResult{{
			ID:   "target-1",
			Kind: "tcp",
			Network: &protocol.NetworkResult{
				Host:      "example.com",
				Port:      443,
				Status:    "ok",
				CheckedAt: time.Now().Unix(),
			},
		}},
	}
	reportResponse := task4AgentPost(t, handler, "/api/agent/v1/report", registered.NodeToken, report.NodeUUID, "report-1", report)
	if reportResponse.Code != http.StatusNoContent {
		t.Fatalf("report status = %d, body = %q", reportResponse.Code, reportResponse.Body.String())
	}

	resourceAt, resourcePayload, err := store.GetResourceLatest(context.Background(), nodes[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if resourceAt.Unix() != report.ReportedAt || string(resourcePayload) != `{"cpu_percent":12.5,"hostname":"task4-host"}` {
		t.Fatalf("resource latest = (%s, %s)", resourceAt, resourcePayload)
	}
	checkedAt, resultPayload, err := store.GetNetworkLatest(context.Background(), nodes[0].ID, "target-1")
	if err != nil {
		t.Fatal(err)
	}
	if checkedAt.Unix() != report.Results[0].Network.CheckedAt || !strings.Contains(string(resultPayload), `"host":"example.com"`) {
		t.Fatalf("network latest = (%s, %s)", checkedAt, resultPayload)
	}
}

func TestAgentRegistrationAndRotationUseConfiguredNodeTokenTTL(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	cfg.AgentNodeTokenTTL = 2 * time.Hour
	handler := NewServer(cfg, service).Handler()

	registration, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	registered := task4Register(t, handler, protocol.RegisterRequest{
		RegistrationToken: registration.Token,
		NodeUUID:          task4NodeUUID1,
		Name:              "configured TTL node",
	})
	now := time.Now().UTC()
	if _, err := store.AuthenticateNodeToken(context.Background(), registered.NodeToken, now.Add(2*time.Hour+time.Second)); !errors.Is(err, db.ErrTokenExpired) {
		t.Fatalf("configured registration token authentication error = %v, want ErrTokenExpired", err)
	}

	session, csrf := task4AdminSession(t, service, store)
	nodes, err := store.ListNodes(context.Background())
	if err != nil || len(nodes) != 1 {
		t.Fatalf("nodes = %#v, err = %v", nodes, err)
	}
	response, _ := task4AdminPost(t, handler, session, csrf, "/api/nodes/"+nodes[0].ID+"/rotate-token", `{}`)
	if response.Code != http.StatusOK {
		t.Fatalf("rotate status = %d, body = %q", response.Code, response.Body.String())
	}
	var rotated struct {
		NodeToken string `json:"node_token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &rotated); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AuthenticateNodeToken(context.Background(), rotated.NodeToken, time.Now().UTC().Add(2*time.Hour+time.Second)); !errors.Is(err, db.ErrTokenExpired) {
		t.Fatalf("configured rotation token authentication error = %v, want ErrTokenExpired", err)
	}
}

func TestAdminRegistrationTokenUsesConfiguredTTL(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	cfg.AgentTokenTTL = 2 * time.Minute
	handler := NewServer(cfg, service).Handler()
	session, csrf := task4AdminSession(t, service, store)
	started := time.Now().UTC()
	registration, _ := task4CreateRegistration(t, handler, session, csrf)
	remaining := time.Until(registration.Expires).Round(time.Second)
	if remaining < time.Minute || remaining > 2*time.Minute {
		t.Fatalf("registration token remaining lifetime = %s, want approximately %s", remaining, cfg.AgentTokenTTL)
	}
	if registration.Expires.Before(started.Add(cfg.AgentTokenTTL-5*time.Second)) || registration.Expires.After(started.Add(cfg.AgentTokenTTL+5*time.Second)) {
		t.Fatalf("registration token expiry = %s, want approximately %s after %s", registration.Expires, cfg.AgentTokenTTL, started)
	}
}

func TestAgentRegistrationRejectsOmittedAndNullSystem(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	handler := NewServer(task4Config(), service).Handler()

	for name, system := range map[string]string{"omitted": "", "null": `"system":null`} {
		t.Run(name, func(t *testing.T) {
			registration, err := store.CreateRegistrationToken(context.Background(), time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			body := `{"registration_token":"` + registration.Token + `","node_uuid":"` + task4NodeUUID6 + `","name":"strict node"`
			if system != "" {
				body += `,` + system
			}
			body += `}`
			response := task4RawJSONRequest(t, handler, http.MethodPost, "/api/agent/v1/register", body)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("registration status = %d, body = %q", response.Code, response.Body.String())
			}
		})
	}
	if nodes, err := store.ListNodes(context.Background()); err != nil || len(nodes) != 0 {
		t.Fatalf("nodes after rejected system payloads = %#v, err = %v", nodes, err)
	}
}

func TestAgentRegistrationRejectsExpiredReusedAndConflictingUUIDs(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	handler := NewServer(task4Config(), service).Handler()

	expired, err := store.CreateRegistrationToken(context.Background(), time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	task4RegisterExpect(t, handler, protocol.RegisterRequest{
		RegistrationToken: expired.Token,
		NodeUUID:          task4NodeUUID2,
		Name:              "expired",
	}, http.StatusUnauthorized)

	registration, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	request := protocol.RegisterRequest{RegistrationToken: registration.Token, NodeUUID: task4NodeUUID3, Name: "first"}
	task4Register(t, handler, request)
	task4RegisterExpect(t, handler, request, http.StatusUnauthorized)

	otherRegistration, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	task4RegisterExpect(t, handler, protocol.RegisterRequest{
		RegistrationToken: otherRegistration.Token,
		NodeUUID:          request.NodeUUID,
		Name:              "conflicting",
	}, http.StatusConflict)
}

func TestAgentAuthRejectsSkewReplayAndUUIDMismatch(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	handler := NewServer(task4Config(), service).Handler()
	registration, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	registered := task4Register(t, handler, protocol.RegisterRequest{
		RegistrationToken: registration.Token,
		NodeUUID:          task4NodeUUID4,
		Name:              "auth node",
	})
	report := protocol.ReportRequest{NodeUUID: task4NodeUUID4, ReportedAt: time.Now().Unix(), Resource: protocol.ResourceSnapshot{}}

	stale := task4AgentPostAt(t, handler, "/api/agent/v1/report", registered.NodeToken, report.NodeUUID, "stale", time.Now().Add(-time.Hour), report)
	if stale.Code != http.StatusUnauthorized {
		t.Fatalf("stale report status = %d, want 401", stale.Code)
	}

	valid := task4AgentPostAt(t, handler, "/api/agent/v1/report", registered.NodeToken, report.NodeUUID, "replay", time.Now(), report)
	if valid.Code != http.StatusNoContent {
		t.Fatalf("valid report status = %d, want 204", valid.Code)
	}
	replay := task4AgentPostAt(t, handler, "/api/agent/v1/report", registered.NodeToken, report.NodeUUID, "replay", time.Now(), report)
	if replay.Code != http.StatusConflict {
		t.Fatalf("replay status = %d, want 409", replay.Code)
	}

	report.NodeUUID = task4NodeUUID5
	mismatch := task4AgentPostAt(t, handler, "/api/agent/v1/report", registered.NodeToken, report.NodeUUID, "mismatch", time.Now(), report)
	if mismatch.Code != http.StatusConflict {
		t.Fatalf("UUID mismatch status = %d, want 409", mismatch.Code)
	}
	wrongToken := task4AgentPostAt(t, handler, "/api/agent/v1/report", "wrong-token", report.NodeUUID, "wrong-token", time.Now(), report)
	if wrongToken.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token status = %d, want 401", wrongToken.Code)
	}
}

func TestIndividualResultEndpointsUseStrictEnvelopes(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	handler := NewServer(task4Config(), service).Handler()
	registration, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	registered := task4Register(t, handler, protocol.RegisterRequest{
		RegistrationToken: registration.Token,
		NodeUUID:          task4NodeUUID5,
		Name:              "envelope node",
	})
	if err := store.CreateNetworkTarget(context.Background(), db.ResultTargetInput{ID: "network-1", Name: "network-1", Kind: "tcp", Host: "example.com"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateMTRTarget(context.Background(), db.ResultTargetInput{ID: "mtr-1", Name: "mtr-1", Host: "example.com"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateMediaDetector(context.Background(), db.ResultTargetInput{ID: "media-1", Name: "media-1", Host: "example.com"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()

	network := protocol.NetworkResultEnvelope{TargetID: "network-1", Result: protocol.NetworkResult{Host: "example.com", Port: 443, CheckedAt: now}}
	response := task4AgentPost(t, handler, "/api/agent/v1/network-result", registered.NodeToken, "envelope-node", "network-1", network)
	if response.Code != http.StatusNoContent {
		t.Fatalf("network result status = %d, body = %q", response.Code, response.Body.String())
	}
	mtr := protocol.MTRResultEnvelope{TargetID: "mtr-1", Result: protocol.MTRResult{Host: "example.com", Reached: true, CheckedAt: now}}
	response = task4AgentPost(t, handler, "/api/agent/v1/mtr-result", registered.NodeToken, "envelope-node", "mtr-1", mtr)
	if response.Code != http.StatusNoContent {
		t.Fatalf("mtr result status = %d, body = %q", response.Code, response.Body.String())
	}
	media := protocol.MediaResultEnvelope{DetectorID: "media-1", Result: protocol.MediaResult{Detector: "custom", Status: "available", CheckedAt: now}}
	response = task4AgentPost(t, handler, "/api/agent/v1/media-result", registered.NodeToken, "envelope-node", "media-1", media)
	if response.Code != http.StatusNoContent {
		t.Fatalf("media result status = %d, body = %q", response.Code, response.Body.String())
	}
	nodes, err := store.ListNodes(context.Background())
	if err != nil || len(nodes) != 1 {
		t.Fatalf("nodes = %#v, err = %v", nodes, err)
	}
	if _, _, err := store.GetNetworkLatest(context.Background(), nodes[0].ID, "network-1"); err != nil {
		t.Fatal(err)
	}

	unknown := []byte(`{"target_id":"network-2","result":{"host":"example.com","port":443},"headers":{}}`)
	unknownResponse := task4RawAgentPost(t, handler, "/api/agent/v1/network-result", registered.NodeToken, "envelope-node", "network-unknown", now, unknown)
	if unknownResponse.Code != http.StatusBadRequest {
		t.Fatalf("unknown envelope field status = %d", unknownResponse.Code)
	}

	oversizedID := strings.Repeat("x", 129)
	invalid := protocol.NetworkResultEnvelope{TargetID: oversizedID, Result: protocol.NetworkResult{Host: "example.com", Port: 443}}
	invalidResponse := task4AgentPost(t, handler, "/api/agent/v1/network-result", registered.NodeToken, "envelope-node", "network-invalid", invalid)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("oversized target ID status = %d", invalidResponse.Code)
	}
}

func TestAdminNodeListRotateAndRevokeRequireCSRF(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	server := NewServer(cfg, service)
	handler := server.Handler()
	session, csrf := task4AdminSession(t, service, store)
	registration, csrf := task4CreateRegistration(t, handler, session, csrf)
	registered := task4Register(t, handler, protocol.RegisterRequest{RegistrationToken: registration.Token, NodeUUID: task4NodeUUID6, Name: "admin node"})

	list := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	listRequest.AddCookie(task4SessionCookie(session))
	handler.ServeHTTP(list, listRequest)
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), registered.NodeToken) || strings.Contains(list.Body.String(), "digest") {
		t.Fatalf("node list exposed secret or failed: %d %q", list.Code, list.Body.String())
	}

	withoutCSRF := httptest.NewRecorder()
	nodes, err := store.ListNodes(context.Background())
	if err != nil || len(nodes) != 1 {
		t.Fatalf("nodes = %#v, err = %v", nodes, err)
	}
	nodeID := nodes[0].ID
	rotateRequest := httptest.NewRequest(http.MethodPost, "/api/nodes/"+nodeID+"/rotate-token", strings.NewReader(`{}`))
	rotateRequest.Header.Set("Content-Type", "application/json")
	rotateRequest.Header.Set("Origin", cfg.PublicBaseURL)
	rotateRequest.AddCookie(task4SessionCookie(session))
	handler.ServeHTTP(withoutCSRF, rotateRequest)
	if withoutCSRF.Code != http.StatusForbidden {
		t.Fatalf("rotate without CSRF status = %d", withoutCSRF.Code)
	}

	rotated, nextCSRF := task4AdminPost(t, handler, session, csrf, "/api/nodes/"+nodeID+"/rotate-token", `{}`)
	if rotated.Code != http.StatusOK {
		t.Fatalf("rotate status = %d, body = %q", rotated.Code, rotated.Body.String())
	}
	var rotatedPayload struct {
		NodeToken string `json:"node_token"`
	}
	if err := json.Unmarshal(rotated.Body.Bytes(), &rotatedPayload); err != nil || rotatedPayload.NodeToken == "" || rotatedPayload.NodeToken == registered.NodeToken {
		t.Fatalf("rotation payload = %#v, err = %v", rotatedPayload, err)
	}
	oldReport := protocol.ReportRequest{NodeUUID: registered.NodeUUID, ReportedAt: time.Now().Unix(), Resource: protocol.ResourceSnapshot{}}
	oldTokenResponse := task4AgentPost(t, handler, "/api/agent/v1/report", registered.NodeToken, registered.NodeUUID, "old-token", oldReport)
	if oldTokenResponse.Code != http.StatusUnauthorized {
		t.Fatalf("old token after rotation status = %d", oldTokenResponse.Code)
	}

	revoked, _ := task4AdminPost(t, handler, session, nextCSRF, "/api/nodes/"+nodeID+"/revoke", `{}`)
	if revoked.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d, body = %q", revoked.Code, revoked.Body.String())
	}
	newTokenResponse := task4AgentPost(t, handler, "/api/agent/v1/report", rotatedPayload.NodeToken, registered.NodeUUID, "new-token", oldReport)
	if newTokenResponse.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token status = %d", newTokenResponse.Code)
	}
}

func TestAgentBodyLimitAndRateLimitReturnGenericJSONErrors(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	server := NewServer(task4Config(), service)
	server.agentLimiter = newRateLimiter(1, time.Minute, 32)
	handler := server.Handler()
	registration, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	registered := task4Register(t, handler, protocol.RegisterRequest{RegistrationToken: registration.Token, NodeUUID: task4NodeUUID1, Name: "limit node"})
	report := protocol.ReportRequest{NodeUUID: registered.NodeUUID, ReportedAt: time.Now().Unix(), Resource: protocol.ResourceSnapshot{}}
	first := task4AgentPost(t, handler, "/api/agent/v1/report", registered.NodeToken, registered.NodeUUID, "limit-1", report)
	if first.Code != http.StatusNoContent {
		t.Fatalf("first rate-limited request status = %d", first.Code)
	}
	second := task4AgentPost(t, handler, "/api/agent/v1/report", registered.NodeToken, registered.NodeUUID, "limit-2", report)
	if second.Code != http.StatusTooManyRequests || strings.Contains(second.Body.String(), "sqlite") {
		t.Fatalf("rate-limit response = %d %q", second.Code, second.Body.String())
	}

	server = NewServer(config.Config{Environment: "development", PublicBaseURL: task4Config().PublicBaseURL, MaxRequestBody: 64}, service)
	handler = server.Handler()
	body := []byte(`{"node_uuid":"` + task4NodeUUID1 + `","reported_at":1,"resource":{"hostname":"` + strings.Repeat("x", 100) + `"},"results":[]}`)
	tooLarge := task4RawAgentPost(t, handler, "/api/agent/v1/report", registered.NodeToken, registered.NodeUUID, "too-large", time.Now().Unix(), body)
	if tooLarge.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized report status = %d", tooLarge.Code)
	}
}

func TestAgentResultsRejectFutureAndDoNotReplaceNewerLatest(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	handler := NewServer(task4Config(), service).Handler()
	registration, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	registered := task4Register(t, handler, protocol.RegisterRequest{RegistrationToken: registration.Token, NodeUUID: task4NodeUUID2, Name: "timestamp node"})
	if err := store.CreateNetworkTarget(context.Background(), db.ResultTargetInput{ID: "timestamp-target", Name: "timestamp-target", Kind: "tcp", Host: "example.com"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	fresh := protocol.NetworkResultEnvelope{TargetID: "timestamp-target", Result: protocol.NetworkResult{Host: "example.com", Port: 443, Status: "fresh", CheckedAt: now.Unix()}}
	if response := task4AgentPostAt(t, handler, "/api/agent/v1/network-result", registered.NodeToken, registered.NodeUUID, "timestamp-fresh", now, fresh); response.Code != http.StatusNoContent {
		t.Fatalf("fresh result status = %d, body = %q", response.Code, response.Body.String())
	}
	stale := fresh
	stale.Result.Status = "stale"
	stale.Result.CheckedAt = now.Add(-time.Minute).Unix()
	if response := task4AgentPostAt(t, handler, "/api/agent/v1/network-result", registered.NodeToken, registered.NodeUUID, "timestamp-stale", now, stale); response.Code != http.StatusNoContent {
		t.Fatalf("stale result status = %d, body = %q", response.Code, response.Body.String())
	}
	checkedAt, payload, err := store.GetNetworkLatest(context.Background(), mustNode(t, store, registered.NodeUUID).ID, "timestamp-target")
	if err != nil {
		t.Fatal(err)
	}
	if checkedAt.Unix() != now.Unix() || !strings.Contains(string(payload), `"status":"fresh"`) {
		t.Fatalf("stale result replaced latest: (%s, %s)", checkedAt, payload)
	}
	future := fresh
	future.Result.CheckedAt = now.Add(6 * time.Minute).Unix()
	if response := task4AgentPostAt(t, handler, "/api/agent/v1/network-result", registered.NodeToken, registered.NodeUUID, "timestamp-future", now, future); response.Code != http.StatusBadRequest {
		t.Fatalf("future result status = %d, want 400", response.Code)
	}
}

func TestAgentPayloadTimestampsRejectOutsideConfiguredSkew(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	handler := NewServer(task4Config(), service).Handler()
	registration, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	registered := task4Register(t, handler, protocol.RegisterRequest{RegistrationToken: registration.Token, NodeUUID: task4NodeUUID6, Name: "payload timestamp node"})
	now := time.Now().UTC()
	oldReport := protocol.ReportRequest{NodeUUID: registered.NodeUUID, ReportedAt: now.Add(-6 * time.Minute).Unix(), Resource: protocol.ResourceSnapshot{}}
	oldResponse := task4AgentPostAt(t, handler, "/api/agent/v1/report", registered.NodeToken, registered.NodeUUID, "old-payload", now, oldReport)
	if oldResponse.Code != http.StatusBadRequest {
		t.Fatalf("old report payload status = %d, want 400", oldResponse.Code)
	}
	futureReport := oldReport
	futureReport.ReportedAt = now.Add(6 * time.Minute).Unix()
	futureResponse := task4AgentPostAt(t, handler, "/api/agent/v1/report", registered.NodeToken, registered.NodeUUID, "future-payload", now, futureReport)
	if futureResponse.Code != http.StatusBadRequest {
		t.Fatalf("future report payload status = %d, want 400", futureResponse.Code)
	}
}

func TestAuthenticatedMalformedAgentAttemptsConsumeRateQuota(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	server := NewServer(task4Config(), service)
	server.agentLimiter = newRateLimiter(2, time.Minute, 2)
	handler := server.Handler()
	registration, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	registered := task4Register(t, handler, protocol.RegisterRequest{RegistrationToken: registration.Token, NodeUUID: task4NodeUUID3, Name: "rate node"})
	for index := 0; index < 2; index++ {
		response := task4RawAgentPost(t, handler, "/api/agent/v1/report", registered.NodeToken, registered.NodeUUID, "malformed-"+strconv.Itoa(index), time.Now().Unix(), []byte(`{"unknown":true}`))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("malformed attempt %d status = %d, want 400", index, response.Code)
		}
	}
	limited := task4RawAgentPost(t, handler, "/api/agent/v1/report", registered.NodeToken, registered.NodeUUID, "malformed-3", time.Now().Unix(), []byte(`{"unknown":true}`))
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("third malformed attempt status = %d, want 429", limited.Code)
	}
	if got := len(server.agentLimiter.entries); got != 1 {
		t.Fatalf("rate registry entries = %d, want 1 authenticated node entry", got)
	}
	unauthenticated := task4RawAgentPost(t, handler, "/api/agent/v1/report", "not-a-token", task4NodeUUID4, "unauthenticated", time.Now().Unix(), []byte(`{"unknown":true}`))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated attempt status = %d, want 401", unauthenticated.Code)
	}
	if got := len(server.agentLimiter.entries); got != 1 {
		t.Fatalf("unauthenticated attempt changed rate registry size to %d", got)
	}
}

func TestAdminJSONWritesRejectNullWithoutSideEffects(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	handler := NewServer(cfg, service).Handler()
	session, csrf := task4AdminSession(t, service, store)
	response, _ := task4AdminPost(t, handler, session, csrf, "/api/registration-tokens", "null")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("null registration body status = %d, want 400", response.Code)
	}
	nodes, err := store.ListNodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 0 {
		t.Fatalf("null registration body created %d nodes", len(nodes))
	}

	registration, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	registered := task4Register(t, handler, protocol.RegisterRequest{RegistrationToken: registration.Token, NodeUUID: task4NodeUUID4, Name: "null action node"})
	nodes, err = store.ListNodes(context.Background())
	if err != nil || len(nodes) != 1 {
		t.Fatalf("nodes after registration = %#v, err = %v", nodes, err)
	}
	csrf = task4CSRF(t, handler, session)
	response, _ = task4AdminPost(t, handler, session, csrf, "/api/nodes/"+nodes[0].ID+"/revoke", "null")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("null node action body status = %d, want 400", response.Code)
	}
	if _, err := store.AuthenticateNodeToken(context.Background(), registered.NodeToken, time.Now().UTC()); err != nil {
		t.Fatalf("null node action revoked token: %v", err)
	}
}

func mustNode(t *testing.T, store *db.Store, uuid string) db.Node {
	t.Helper()
	nodes, err := store.ListNodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range nodes {
		if node.UUID == uuid {
			return node
		}
	}
	t.Fatalf("node %q not found", uuid)
	return db.Node{}
}

func task4Config() config.Config {
	return config.Config{Environment: "development", PublicBaseURL: "http://127.0.0.1:8080", MaxRequestBody: 1 << 20, AgentClockSkew: 5 * time.Minute}
}

func newTask4Auth(t *testing.T) (*auth.Service, *db.Store) {
	t.Helper()
	provider := newServerFakeGitHub(t, "alice", nil)
	t.Cleanup(provider.Close)
	service, store := newServerAuth(t, provider, []string{"alice"}, "")
	return service, store
}

func task4AdminSession(t *testing.T, service *auth.Service, store *db.Store) (string, string) {
	t.Helper()
	admin, err := store.UpsertAdminUser(context.Background(), "github", "task4-admin", "alice", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	session, err := service.CreateSessionForUser(context.Background(), admin.ID, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	return session, task4CSRF(t, NewServer(task4Config(), service).Handler(), session)
}

func task4CSRF(t *testing.T, handler http.Handler, session string) string {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/csrf", nil)
	request.AddCookie(task4SessionCookie(session))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("CSRF status = %d, body = %q", response.Code, response.Body.String())
	}
	var payload struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || payload.Token == "" {
		t.Fatalf("CSRF payload = %q, err = %v", response.Body.String(), err)
	}
	return payload.Token
}

func task4SessionCookie(value string) *http.Cookie {
	return &http.Cookie{Name: "probewatch_session", Value: value}
}

type task4RegistrationResponse struct {
	Token    string    `json:"registration_token"`
	Endpoint string    `json:"endpoint"`
	Expires  time.Time `json:"expires_at"`
}

func task4CreateRegistration(t *testing.T, handler http.Handler, session, csrf string) (task4RegistrationResponse, string) {
	t.Helper()
	response, nextCSRF := task4AdminPost(t, handler, session, csrf, "/api/registration-tokens", `{}`)
	if response.Code != http.StatusOK {
		t.Fatalf("create registration status = %d, body = %q", response.Code, response.Body.String())
	}
	var payload task4RegistrationResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Token == "" || payload.Endpoint == "" || payload.Expires.IsZero() {
		t.Fatalf("registration payload = %#v", payload)
	}
	return payload, nextCSRF
}

func task4Register(t *testing.T, handler http.Handler, request protocol.RegisterRequest) protocol.RegisterResponse {
	t.Helper()
	request = task4WithSystem(request)
	response := task4JSONRequest(t, handler, http.MethodPost, "/api/agent/v1/register", request, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("register status = %d, body = %q", response.Code, response.Body.String())
	}
	var payload protocol.RegisterResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func task4RegisterExpect(t *testing.T, handler http.Handler, request protocol.RegisterRequest, want int) {
	t.Helper()
	request = task4WithSystem(request)
	response := task4JSONRequest(t, handler, http.MethodPost, "/api/agent/v1/register", request, nil)
	if response.Code != want || !strings.Contains(response.Header().Get("Content-Type"), "application/json") || strings.Contains(response.Body.String(), "token_digest") {
		t.Fatalf("register response = %d %q, want %d generic JSON", response.Code, response.Body.String(), want)
	}
}

func task4WithSystem(request protocol.RegisterRequest) protocol.RegisterRequest {
	if request.System == nil {
		request.System = &protocol.RegistrationSystem{OS: "linux", Arch: "amd64", Hostname: "task4-host", AgentVersion: "test"}
	}
	return request
}

func task4AgentPost(t *testing.T, handler http.Handler, path, token, nodeUUID, requestID string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return task4AgentPostAt(t, handler, path, token, nodeUUID, requestID, time.Now(), body)
}

func task4AgentPostAt(t *testing.T, handler http.Handler, path, token, nodeUUID, requestID string, timestamp time.Time, body any) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return task4RawAgentPost(t, handler, path, token, nodeUUID, requestID, timestamp.Unix(), payload)
}

func task4RawAgentPost(t *testing.T, handler http.Handler, path, token, nodeUUID, requestID string, timestamp int64, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-Probe-Timestamp", stringInt64(timestamp))
	request.Header.Set("X-Probe-Request-ID", requestID)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func task4JSONRequest(t *testing.T, handler http.Handler, method, path string, body any, headers http.Header) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, path, strings.NewReader(string(payload)))
	request.Header.Set("Content-Type", "application/json")
	for key, values := range headers {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func task4RawJSONRequest(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func task4AdminPost(t *testing.T, handler http.Handler, session, csrf, path, body string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080"+path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://127.0.0.1:8080")
	request.Header.Set("X-CSRF-Token", csrf)
	request.AddCookie(task4SessionCookie(session))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response, response.Header().Get("X-CSRF-Token")
}

func stringInt64(value int64) string {
	return strconv.FormatInt(value, 10)
}
