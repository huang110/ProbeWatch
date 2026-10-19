package api

import (
	"bytes"
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

func TestCertificatesAndDNSMatrixHandlers(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	server := NewServer(cfg, service)
	handler := server.Handler()
	session, _ := task4AdminSession(t, service, store)
	ctx := context.Background()
	now := time.Now().UTC()

	// Register 2 nodes
	node1 := publicStatusRegisterNode(t, handler, store, task4NodeUUID1, "Node-Singapore")
	node2 := publicStatusRegisterNode(t, handler, store, task4NodeUUID2, "Node-Tokyo")

	// Create HTTPS target
	targetHTTPS, err := store.CreateTarget(ctx, db.TargetDefinition{
		ID:      "tgt-https",
		Name:    "ProbeWatch Portal",
		Kind:    db.TargetKindHTTPS,
		Host:    "tz.115yu.us.ci",
		Enabled: true,
		Payload: []byte(`{"port":443,"path":"/"}`),
	}, now)
	if err != nil {
		t.Fatalf("create targetHTTPS: %v", err)
	}

	// Create DNS target
	targetDNS, err := store.CreateTarget(ctx, db.TargetDefinition{
		ID:      "tgt-dns",
		Name:    "Core DNS",
		Kind:    db.TargetKindDNS,
		Host:    "one.one.one.one",
		Enabled: true,
		Payload: []byte(`{"port":53,"dns_type":"A"}`),
	}, now)
	if err != nil {
		t.Fatalf("create targetDNS: %v", err)
	}

	// Insert latest network result for HTTPS target (with TLS cert)
	resHTTPS := protocol.NetworkResult{
		Host:       "tz.115yu.us.ci",
		Port:       443,
		Status:     "success",
		StatusCode: 200,
		LatencyMS:  45,
		CheckedAt:  now.Unix(),
		TLSCert: &protocol.TLSCertResult{
			Subject:      "tz.115yu.us.ci",
			Issuer:       "Let's Encrypt",
			DNSNames:     []string{"tz.115yu.us.ci"},
			NotBefore:    now.Add(-30 * 24 * time.Hour).Unix(),
			NotAfter:     now.Add(60 * 24 * time.Hour).Unix(),
			DaysLeft:     60,
			Protocol:     "TLS 1.3",
			CipherSuite:  "TLS_AES_128_GCM_SHA256",
			IsExpired:    false,
			ExpiringSoon: false,
		},
	}
	payloadHTTPS, _ := json.Marshal(resHTTPS)
	if err := store.UpsertNetworkLatest(ctx, node1.ID, targetHTTPS.ID, now, payloadHTTPS); err != nil {
		t.Fatalf("upsert network latest HTTPS: %v", err)
	}

	// Insert consistent DNS results from node1 and node2
	resDNS1 := protocol.NetworkResult{
		Host:      "one.one.one.one",
		Port:      53,
		Status:    "success",
		LatencyMS: 12,
		CheckedAt: now.Unix(),
		DNS: &protocol.DNSResult{
			Records:     []string{"1.1.1.1", "1.0.0.1"},
			QueryTimeMS: 8,
		},
	}
	payloadDNS1, _ := json.Marshal(resDNS1)
	if err := store.UpsertNetworkLatest(ctx, node1.ID, targetDNS.ID, now, payloadDNS1); err != nil {
		t.Fatalf("upsert DNS node1: %v", err)
	}

	resDNS2 := protocol.NetworkResult{
		Host:      "one.one.one.one",
		Port:      53,
		Status:    "success",
		LatencyMS: 15,
		CheckedAt: now.Unix(),
		DNS: &protocol.DNSResult{
			Records:     []string{"1.0.0.1", "1.1.1.1"}, // same IPs, different order
			QueryTimeMS: 10,
		},
	}
	payloadDNS2, _ := json.Marshal(resDNS2)
	if err := store.UpsertNetworkLatest(ctx, node2.ID, targetDNS.ID, now, payloadDNS2); err != nil {
		t.Fatalf("upsert DNS node2: %v", err)
	}

	// 1. Test GET /api/public/certificates
	pubCertReq := httptest.NewRequest(http.MethodGet, "/api/public/certificates", nil)
	pubCertRec := httptest.NewRecorder()
	handler.ServeHTTP(pubCertRec, pubCertReq)
	if pubCertRec.Code != http.StatusOK {
		t.Fatalf("GET /api/public/certificates returned %d: %s", pubCertRec.Code, pubCertRec.Body.String())
	}
	var certResp CertificatesResponse
	if err := json.Unmarshal(pubCertRec.Body.Bytes(), &certResp); err != nil {
		t.Fatalf("unmarshal cert response: %v", err)
	}
	if certResp.Total != 1 || certResp.ValidCount != 1 || certResp.ExpiredCount != 0 {
		t.Fatalf("cert stats = %#v, want Total=1, Valid=1", certResp)
	}
	if certResp.Certificates[0].Subject != "" || certResp.Certificates[0].Host != "" || certResp.Certificates[0].NodeID != "" {
		t.Fatalf("public certificate response leaked identity fields: %#v", certResp.Certificates[0])
	}
	if certResp.Certificates[0].TargetName != "HTTPS target 1" {
		t.Fatalf("public target name = %q, want sanitized label", certResp.Certificates[0].TargetName)
	}

	// 2. Test GET /api/certificates with admin session
	adminCertReq := httptest.NewRequest(http.MethodGet, "/api/certificates", nil)
	adminCertReq.AddCookie(task4SessionCookie(session))
	adminCertRec := httptest.NewRecorder()
	handler.ServeHTTP(adminCertRec, adminCertReq)
	if adminCertRec.Code != http.StatusOK {
		t.Fatalf("GET /api/certificates returned %d", adminCertRec.Code)
	}
	var adminCertResp CertificatesResponse
	if err := json.Unmarshal(adminCertRec.Body.Bytes(), &adminCertResp); err != nil {
		t.Fatalf("unmarshal admin cert response: %v", err)
	}
	if len(adminCertResp.Certificates) != 1 || adminCertResp.Certificates[0].Subject != "tz.115yu.us.ci" || adminCertResp.Certificates[0].TargetName != "ProbeWatch Portal" {
		t.Fatalf("authenticated certificate response lost private details: %#v", adminCertResp)
	}

	// 3. Test GET /api/public/dns-matrix (Consistent case)
	pubDNSReq := httptest.NewRequest(http.MethodGet, "/api/public/dns-matrix", nil)
	pubDNSRec := httptest.NewRecorder()
	handler.ServeHTTP(pubDNSRec, pubDNSReq)
	if pubDNSRec.Code != http.StatusOK {
		t.Fatalf("GET /api/public/dns-matrix returned %d: %s", pubDNSRec.Code, pubDNSRec.Body.String())
	}
	var dnsResp DNSMatrixResponse
	if err := json.Unmarshal(pubDNSRec.Body.Bytes(), &dnsResp); err != nil {
		t.Fatalf("unmarshal dns response: %v", err)
	}
	if dnsResp.TotalTargets != 1 || dnsResp.ConsistentCount != 1 || dnsResp.DivergentCount != 0 {
		t.Fatalf("dns matrix stats = %#v, want Total=1, Consistent=1", dnsResp)
	}
	if !dnsResp.Matrix[0].IsConsistent {
		t.Fatalf("target dns matrix is_consistent = false, want true")
	}
	if len(dnsResp.Matrix[0].Nodes) != 2 {
		t.Fatalf("matrix nodes count = %d, want 2", len(dnsResp.Matrix[0].Nodes))
	}

	// 4. Test DNS Divergence: Introduce a divergent node (e.g. poisoning or split-horizon)
	node3 := publicStatusRegisterNode(t, handler, store, task4NodeUUID3, "Node-Frankfurt")
	resDNS3 := protocol.NetworkResult{
		Host:      "one.one.one.one",
		Port:      53,
		Status:    "success",
		LatencyMS: 30,
		CheckedAt: now.Unix(),
		DNS: &protocol.DNSResult{
			Records:     []string{"198.51.100.1"}, // completely divergent IP!
			QueryTimeMS: 22,
		},
	}
	payloadDNS3, _ := json.Marshal(resDNS3)
	if err := store.UpsertNetworkLatest(ctx, node3.ID, targetDNS.ID, now, payloadDNS3); err != nil {
		t.Fatalf("upsert DNS node3: %v", err)
	}

	divDNSReq := httptest.NewRequest(http.MethodGet, "/api/dns-matrix", nil)
	divDNSReq.AddCookie(task4SessionCookie(session))
	divDNSRec := httptest.NewRecorder()
	handler.ServeHTTP(divDNSRec, divDNSReq)
	if divDNSRec.Code != http.StatusOK {
		t.Fatalf("GET /api/dns-matrix returned %d", divDNSRec.Code)
	}
	var divResp DNSMatrixResponse
	if err := json.Unmarshal(divDNSRec.Body.Bytes(), &divResp); err != nil {
		t.Fatalf("unmarshal divergent dns response: %v", err)
	}
	if divResp.DivergentCount != 1 || divResp.ConsistentCount != 0 {
		t.Fatalf("divergent count = %d, want 1; consistent count = %d, want 0", divResp.DivergentCount, divResp.ConsistentCount)
	}
	if divResp.Matrix[0].IsConsistent {
		t.Fatalf("expected matrix to detect divergence, got is_consistent = true")
	}
	if divResp.Matrix[0].UniqueSetsCount != 2 {
		t.Fatalf("unique sets count = %d, want 2", divResp.Matrix[0].UniqueSetsCount)
	}
	if len(divResp.Matrix[0].DivergentNodes) != 1 || divResp.Matrix[0].DivergentNodes[0] != "Node-Frankfurt" {
		t.Fatalf("divergent nodes = %#v, want [Node-Frankfurt]", divResp.Matrix[0].DivergentNodes)
	}
}

func TestSanitizePublicDNSMatrixKeepsDivergentNodeNumbers(t *testing.T) {
	response := DNSMatrixResponse{Matrix: []DNSTargetMatrix{{
		DivergentNodes: []string{"edge-3"},
		Nodes: []DNSNodeRecord{
			{NodeID: "edge-1", NodeName: "edge-1"},
			{NodeID: "edge-2", NodeName: "edge-2"},
			{NodeID: "edge-3", NodeName: "edge-3"},
		},
	}}}
	sanitized := sanitizePublicDNSMatrix(response)
	if len(sanitized.Matrix) != 1 || len(sanitized.Matrix[0].DivergentNodes) != 1 {
		t.Fatalf("unexpected sanitized matrix: %+v", sanitized)
	}
	if got := sanitized.Matrix[0].DivergentNodes[0]; got != "Node 3" {
		t.Fatalf("divergent node = %q, want Node 3", got)
	}
}

func TestSyntheticTargetsCRUDAndResults(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	server := NewServer(cfg, service)
	handler := server.Handler()
	session, csrf := task4AdminSession(t, service, store)

	// 1. Create Synthetic Target
	createPayload := map[string]any{
		"id":               "syn-api-test",
		"name":             "Production API Health",
		"protocol":         "https",
		"target_url":       "https://api.example.com/healthz",
		"method":           "GET",
		"headers":          map[string]string{"X-Test": "Synthetic"},
		"timeout_ms":       4000,
		"interval_seconds": 30,
		"consensus_nodes":  2,
		"node_tags":        []string{"edge", "prod"},
		"node_ids":         []string{},
		"assertions": []map[string]any{
			{
				"source":   "status_code",
				"operator": "equals",
				"target":   "200",
			},
		},
		"enabled": true,
	}
	body, _ := json.Marshal(createPayload)
	req := httptest.NewRequest(http.MethodPost, "/api/synthetic/targets", bytes.NewReader(body))
	req.AddCookie(task4SessionCookie(session))
	req.Header.Set("X-CSRF-Token", csrf)
	req.Header.Set("Origin", "http://127.0.0.1:8080")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	if newCsrf := rec.Header().Get("X-CSRF-Token"); newCsrf != "" {
		csrf = newCsrf
	}

	// 2. List Synthetic Targets
	reqList := httptest.NewRequest(http.MethodGet, "/api/synthetic/targets", nil)
	reqList.AddCookie(task4SessionCookie(session))
	recList := httptest.NewRecorder()
	handler.ServeHTTP(recList, reqList)

	if recList.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", recList.Code)
	}
	var targets []db.SyntheticTargetRecord
	if err := json.Unmarshal(recList.Body.Bytes(), &targets); err != nil {
		t.Fatalf("unmarshal targets: %v", err)
	}
	if len(targets) != 1 || targets[0].ID != "syn-api-test" {
		t.Fatalf("unexpected targets: %#v", targets)
	}

	// 3. Update Synthetic Target
	updatePayload := map[string]any{
		"name": "Production API Health (Updated)",
	}
	updateBody, _ := json.Marshal(updatePayload)
	reqUpdate := httptest.NewRequest(http.MethodPatch, "/api/synthetic/targets/syn-api-test", bytes.NewReader(updateBody))
	reqUpdate.AddCookie(task4SessionCookie(session))
	reqUpdate.Header.Set("X-CSRF-Token", csrf)
	reqUpdate.Header.Set("Origin", "http://127.0.0.1:8080")
	reqUpdate.Header.Set("Content-Type", "application/json")
	recUpdate := httptest.NewRecorder()
	handler.ServeHTTP(recUpdate, reqUpdate)

	if recUpdate.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on update, got %d: %s", recUpdate.Code, recUpdate.Body.String())
	}
	if newCsrf := recUpdate.Header().Get("X-CSRF-Token"); newCsrf != "" {
		csrf = newCsrf
	}

	// 4. Ingest Synthetic Result from Agent
	reg, nextCSRF := task4CreateRegistration(t, handler, session, csrf)
	csrf = nextCSRF
	regResp := task4Register(t, handler, protocol.RegisterRequest{
		RegistrationToken: reg.Token,
		NodeUUID:          task4NodeUUID1,
		Name:              "Edge Node Singapore",
	})
	// The target is scoped to edge/prod nodes. Keep the fixture aligned with
	// the production binding check so an agent cannot submit a result for a
	// target it would never receive in its configuration.
	if err := store.UpdateNode(context.Background(), mustNode(t, store, regResp.NodeUUID).ID, "Edge Node Singapore", "edge,prod"); err != nil {
		t.Fatalf("tag synthetic node: %v", err)
	}
	now := time.Now().UTC()

	synRes := protocol.SyntheticResult{
		Status:     "ok",
		Protocol:   "agent-forged-protocol",
		TargetURL:  "https://api.example.com/healthz",
		StatusCode: 200,
		Timing: protocol.SyntheticTiming{
			TotalDurationMS: 35,
			DNSLookupMS:     2,
			TCPConnectMS:    10,
			TLSHandshakeMS:  12,
			TTFBMS:          8,
			TransferMS:      3,
		},
		Passed:    true,
		CheckedAt: now.Unix(),
	}
	env := protocol.SyntheticResultEnvelope{
		TargetID: "syn-api-test",
		Result:   synRes,
	}
	envBytes, _ := json.Marshal(env)
	agentReq := httptest.NewRequest(http.MethodPost, "/api/agent/v1/synthetic-result", bytes.NewReader(envBytes))
	agentReq.Header.Set("Authorization", "Bearer "+regResp.NodeToken)
	agentReq.Header.Set("X-Probe-Request-ID", "syn-req-1")
	agentReq.Header.Set("X-Probe-Timestamp", strconv.FormatInt(now.Unix(), 10))
	agentReq.Header.Set("Content-Type", "application/json")
	agentRec := httptest.NewRecorder()
	handler.ServeHTTP(agentRec, agentReq)

	if agentRec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 NoContent, got %d: %s", agentRec.Code, agentRec.Body.String())
	}

	// 5. Test GET /api/synthetic/results & /api/public/synthetic/results
	reqResults := httptest.NewRequest(http.MethodGet, "/api/public/synthetic/results", nil)
	recResults := httptest.NewRecorder()
	handler.ServeHTTP(recResults, reqResults)

	if recResults.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for synthetic results, got %d: %s", recResults.Code, recResults.Body.String())
	}
	var overview SyntheticResultsOverviewResponse
	if err := json.Unmarshal(recResults.Body.Bytes(), &overview); err != nil {
		t.Fatalf("unmarshal overview: %v", err)
	}
	if overview.TotalTargets != 1 || len(overview.Targets) != 1 {
		t.Fatalf("unexpected overview targets count: %d", overview.TotalTargets)
	}
	if overview.Targets[0].ConsensusStatus != "degraded" {
		t.Fatalf("expected consensus degraded while only one of two required nodes reported, got %s", overview.Targets[0].ConsensusStatus)
	}
	publicBody := recResults.Body.String()
	for _, secret := range []string{
		"https://api.example.com/healthz",
		"syn-api-test",
		"X-Test",
		"Production API Health",
		"Edge Node Singapore",
	} {
		if strings.Contains(publicBody, secret) {
			t.Fatalf("public synthetic response leaked %q: %s", secret, publicBody)
		}
	}
	if strings.Contains(publicBody, "target_url") || strings.Contains(publicBody, "payload") || strings.Contains(publicBody, "failed_assertion") {
		t.Fatalf("public synthetic response contains private fields: %s", publicBody)
	}
	if !strings.Contains(publicBody, `"protocol":"https"`) {
		t.Fatalf("public synthetic response did not derive protocol from configured target: %s", publicBody)
	}

	// 6. Test GET /api/synthetic/history
	reqHist := httptest.NewRequest(http.MethodGet, "/api/synthetic/history?target_id=syn-api-test", nil)
	reqHist.AddCookie(task4SessionCookie(session))
	recHist := httptest.NewRecorder()
	handler.ServeHTTP(recHist, reqHist)

	if recHist.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for synthetic history, got %d", recHist.Code)
	}
	var history []db.SyntheticHistoryRecord
	if err := json.Unmarshal(recHist.Body.Bytes(), &history); err != nil {
		t.Fatalf("unmarshal history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 history record, got %d", len(history))
	}

	// 7. Delete Synthetic Target
	reqDel := httptest.NewRequest(http.MethodDelete, "/api/synthetic/targets/syn-api-test", nil)
	reqDel.AddCookie(task4SessionCookie(session))
	reqDel.Header.Set("X-CSRF-Token", csrf)
	reqDel.Header.Set("Origin", "http://127.0.0.1:8080")
	reqDel.Header.Set("Content-Type", "application/json")
	recDel := httptest.NewRecorder()
	handler.ServeHTTP(recDel, reqDel)

	if recDel.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on delete, got %d", recDel.Code)
	}
}

func TestSyntheticHistoryScopedLimitDoesNotDropAuthorizedRows(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	server := NewServer(cfg, service)
	handler := server.Handler()
	// Seed the bootstrap administrator so the store's last-admin protection
	// allows creation of a scoped operator below.
	_, _ = task4AdminSession(t, service, store)

	allowedNode := publicStatusRegisterNode(t, handler, store, task4NodeUUID1, "Allowed synthetic node")
	forbiddenNode := publicStatusRegisterNode(t, handler, store, task4NodeUUID2, "Forbidden synthetic node")
	now := time.Now().UTC().Truncate(time.Second)
	if err := store.CreateSyntheticTarget(context.Background(), db.SyntheticTargetRecord{
		ID: "syn-scope-history", Name: "Scoped history", Protocol: "https", TargetURL: "https://example.com", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	oldAt := now.Add(-time.Hour)
	newAt := now.Add(-30 * time.Minute)
	for _, row := range []struct {
		nodeID string
		at     time.Time
		total  int
	}{
		{allowedNode.ID, oldAt, 111},
		{forbiddenNode.ID, newAt, 222},
	} {
		if _, err := store.DB().Exec(`INSERT INTO synthetic_results_history (node_id, target_id, payload, protocol, status_code, passed, dns_ms, connect_ms, tls_ms, ttfb_ms, total_ms, checked_at, recorded_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			row.nodeID, "syn-scope-history", []byte(`{"status":"ok"}`), "https", 200, 1, 1, 2, 3, 4, row.total, row.at.UnixNano(), row.at.UnixNano()); err != nil {
			t.Fatal(err)
		}
	}

	user, err := store.UpsertAdminUser(context.Background(), "local", "scoped-synthetic-reader", "scoped", now)
	if err != nil {
		t.Fatal(err)
	}
	role := db.RoleOperator
	allowedNodes := allowedNode.UUID
	if _, err := store.UpdateAdminUser(context.Background(), user.ID, db.UpdateUserInput{Role: &role, AllowedNodes: &allowedNodes}, now); err != nil {
		t.Fatal(err)
	}
	session, err := service.CreateSessionForUser(context.Background(), user.ID, now)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/synthetic/history?target_id=syn-scope-history&limit=1", nil)
	req.AddCookie(task4SessionCookie(session))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("scoped synthetic history status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var history []RestrictedSyntheticHistoryRecord
	if err := json.Unmarshal(rec.Body.Bytes(), &history); err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].TotalMS != 111 || !history[0].CheckedAt.Equal(oldAt) {
		t.Fatalf("scoped synthetic history = %#v, want authorized older sample", history)
	}
}

func TestSyntheticTargetRejectsOversizedAgentFields(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	server := NewServer(cfg, service)
	handler := server.Handler()
	session, csrf := task4AdminSession(t, service, store)

	oversizedHeaders := make(map[string]string, 33)
	for i := 0; i < 33; i++ {
		oversizedHeaders["X-Probe-Header-"+strconv.Itoa(i)] = "value"
	}
	body, _ := json.Marshal(map[string]any{
		"id":         "syn-too-many-headers",
		"name":       "Too many headers",
		"protocol":   "https",
		"target_url": "https://example.com/healthz",
		"headers":    oversizedHeaders,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/synthetic/targets", bytes.NewReader(body))
	req.AddCookie(task4SessionCookie(session))
	req.Header.Set("X-CSRF-Token", csrf)
	req.Header.Set("Origin", "http://127.0.0.1:8080")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("oversized headers status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if next := rec.Header().Get("X-CSRF-Token"); next != "" {
		csrf = next
	}

	session, csrf = task4AdminSession(t, service, store)
	validBody, _ := json.Marshal(map[string]any{
		"id":         "syn-too-large-body",
		"name":       "Large body",
		"protocol":   "https",
		"target_url": "https://example.com/healthz",
	})
	createReq := httptest.NewRequest(http.MethodPost, "/api/synthetic/targets", bytes.NewReader(validBody))
	createReq.AddCookie(task4SessionCookie(session))
	createReq.Header.Set("X-CSRF-Token", csrf)
	createReq.Header.Set("Origin", "http://127.0.0.1:8080")
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("valid synthetic target status = %d, body = %s", createRec.Code, createRec.Body.String())
	}
	if next := createRec.Header().Get("X-CSRF-Token"); next != "" {
		csrf = next
	}

	updateBody, _ := json.Marshal(map[string]any{"body_payload": strings.Repeat("x", 65537)})
	updateReq := httptest.NewRequest(http.MethodPatch, "/api/synthetic/targets/syn-too-large-body", bytes.NewReader(updateBody))
	updateReq.AddCookie(task4SessionCookie(session))
	updateReq.Header.Set("X-CSRF-Token", csrf)
	updateReq.Header.Set("Origin", "http://127.0.0.1:8080")
	updateReq.Header.Set("Content-Type", "application/json")
	updateRec := httptest.NewRecorder()
	handler.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusBadRequest {
		t.Fatalf("oversized body status = %d, body = %s", updateRec.Code, updateRec.Body.String())
	}
}

func TestSyntheticSimulationTest(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	server := NewServer(cfg, service)
	handler := server.Handler()
	session, csrf := task4AdminSession(t, service, store)

	// Mock target server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"up","version":"1.0"}`))
	}))
	defer ts.Close()

	testReq := map[string]any{
		"protocol":   "http",
		"target_url": ts.URL,
		"method":     "GET",
		"timeout_ms": 3000,
		"assertions": []map[string]any{
			{
				"source":   "status_code",
				"operator": "equals",
				"target":   "200",
			},
			{
				"source":   "jsonpath",
				"property": "status",
				"operator": "equals",
				"target":   "up",
			},
		},
	}
	body, _ := json.Marshal(testReq)
	req := httptest.NewRequest(http.MethodPost, "/api/synthetic/test", bytes.NewReader(body))
	req.AddCookie(task4SessionCookie(session))
	req.Header.Set("X-CSRF-Token", csrf)
	req.Header.Set("Origin", "http://127.0.0.1:8080")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for synthetic test, got %d: %s", rec.Code, rec.Body.String())
	}

	var res protocol.SyntheticResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal simulation result: %v", err)
	}
	if res.StatusCode != 200 || !res.Passed {
		t.Fatalf("expected test probe to pass, got: %#v", res)
	}
}
