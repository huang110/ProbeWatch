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
	if certResp.Certificates[0].Subject != "tz.115yu.us.ci" {
		t.Fatalf("cert subject = %q, want tz.115yu.us.ci", certResp.Certificates[0].Subject)
	}
	if certResp.Certificates[0].TargetName != "ProbeWatch Portal" {
		t.Fatalf("target name = %q, want ProbeWatch Portal", certResp.Certificates[0].TargetName)
	}

	// 2. Test GET /api/certificates with admin session
	adminCertReq := httptest.NewRequest(http.MethodGet, "/api/certificates", nil)
	adminCertReq.AddCookie(task4SessionCookie(session))
	adminCertRec := httptest.NewRecorder()
	handler.ServeHTTP(adminCertRec, adminCertReq)
	if adminCertRec.Code != http.StatusOK {
		t.Fatalf("GET /api/certificates returned %d", adminCertRec.Code)
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
	now := time.Now().UTC()

	synRes := protocol.SyntheticResult{
		Status:     "ok",
		Protocol:   "https",
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
	if overview.Targets[0].ConsensusStatus != "healthy" {
		t.Fatalf("expected consensus healthy, got %s", overview.Targets[0].ConsensusStatus)
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

