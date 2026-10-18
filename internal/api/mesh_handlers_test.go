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

func meshAgentConfigGet(t *testing.T, handler http.Handler, token, reqID string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/agent/v1/config", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-Probe-Timestamp", strconv.FormatInt(time.Now().UTC().Unix(), 10))
	request.Header.Set("X-Probe-Request-ID", reqID)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestMeshMatrixHandler(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	server := NewServer(cfg, service)
	handler := server.Handler()
	session, _ := task4AdminSession(t, service, store)
	ctx := context.Background()
	now := time.Now().UTC()

	// Register 3 nodes: Singapore, Tokyo, Silicon Valley
	nodeSG := publicStatusRegisterNode(t, handler, store, task4NodeUUID1, "SG-Singapore")
	nodeTYO := publicStatusRegisterNode(t, handler, store, task4NodeUUID2, "TYO-Tokyo")
	nodeSFO := publicStatusRegisterNode(t, handler, store, "98765432-1234-4321-8765-123456789abc", "SFO-SiliconValley")

	// Set IP / resource latest for each node
	sgSnapshot := protocol.ResourceSnapshot{IPv4: "198.51.100.1", Hostname: "sg-node-01"}
	payloadSG, _ := json.Marshal(sgSnapshot)
	_ = store.UpsertResourceLatest(ctx, nodeSG.ID, now, payloadSG)

	tyoSnapshot := protocol.ResourceSnapshot{IPv4: "198.51.100.2", Hostname: "tyo-node-01"}
	payloadTYO, _ := json.Marshal(tyoSnapshot)
	_ = store.UpsertResourceLatest(ctx, nodeTYO.ID, now, payloadTYO)

	sfoSnapshot := protocol.ResourceSnapshot{IPv4: "198.51.100.3", Hostname: "sfo-node-01"}
	payloadSFO, _ := json.Marshal(sfoSnapshot)
	_ = store.UpsertResourceLatest(ctx, nodeSFO.ID, now, payloadSFO)

	// Create targets representing node IPs
	tgtTYO, err := store.CreateTarget(ctx, db.TargetDefinition{
		ID:      "tgt-to-tyo",
		Name:    "TYO Target",
		Kind:    db.TargetKindTCP,
		Host:    "198.51.100.2",
		Enabled: true,
		Payload: []byte(`{"port":80}`),
	}, now)
	if err != nil {
		t.Fatalf("create tgtTYO: %v", err)
	}

	tgtSFO, err := store.CreateTarget(ctx, db.TargetDefinition{
		ID:      "tgt-to-sfo",
		Name:    "SFO Target",
		Kind:    db.TargetKindTCP,
		Host:    "198.51.100.3",
		Enabled: true,
		Payload: []byte(`{"port":80}`),
	}, now)
	if err != nil {
		t.Fatalf("create tgtSFO: %v", err)
	}

	// 1. SG -> TYO probe: 65ms (good)
	resSGtoTYO := protocol.NetworkResult{
		Host:      "198.51.100.2",
		Port:      80,
		Status:    "up",
		LatencyMS: 65,
		CheckedAt: now.Unix(),
	}
	paySGtoTYO, _ := json.Marshal(resSGtoTYO)
	_ = store.UpsertNetworkLatest(ctx, nodeSG.ID, tgtTYO.ID, now, paySGtoTYO)

	// 2. TYO -> SFO probe: 105ms (fair)
	resTYOtoSFO := protocol.NetworkResult{
		Host:      "198.51.100.3",
		Port:      80,
		Status:    "up",
		LatencyMS: 105,
		CheckedAt: now.Unix(),
	}
	payTYOtoSFO, _ := json.Marshal(resTYOtoSFO)
	_ = store.UpsertNetworkLatest(ctx, nodeTYO.ID, tgtSFO.ID, now, payTYOtoSFO)

	// 3. SG -> SFO direct probe: 280ms (high/suboptimal!)
	resSGtoSFO := protocol.NetworkResult{
		Host:      "198.51.100.3",
		Port:      80,
		Status:    "up",
		LatencyMS: 280,
		CheckedAt: now.Unix(),
	}
	paySGtoSFO, _ := json.Marshal(resSGtoSFO)
	_ = store.UpsertNetworkLatest(ctx, nodeSG.ID, tgtSFO.ID, now, paySGtoSFO)

	// Test public endpoint
	reqPublic := httptest.NewRequest(http.MethodGet, "/api/public/mesh-matrix", nil)
	recPublic := httptest.NewRecorder()
	handler.ServeHTTP(recPublic, reqPublic)
	if recPublic.Code != http.StatusOK {
		t.Fatalf("expected 200 for public mesh matrix, got %d: %s", recPublic.Code, recPublic.Body.String())
	}
	var publicResp MeshMatrixResponse
	if err := json.Unmarshal(recPublic.Body.Bytes(), &publicResp); err != nil {
		t.Fatalf("decode public mesh response: %v", err)
	}
	for _, node := range publicResp.Nodes {
		if node.UUID != "" || node.IP != "" || node.Hostname != "" || len(node.Tags) != 0 {
			t.Fatalf("public mesh response leaked node identity data: %+v", node)
		}
	}
	publicIndices := make(map[string]int)
	for i, node := range publicResp.Nodes {
		publicIndices[node.Name] = i
	}
	if publicResp.Matrix[publicIndices["SG-Singapore"]][publicIndices["TYO-Tokyo"]].LatencyMS != 65 {
		t.Fatalf("public mesh matrix lost direct probe result: %+v", publicResp.Matrix)
	}

	// Test authenticated endpoint
	reqAuth := httptest.NewRequest(http.MethodGet, "/api/mesh-matrix", nil)
	reqAuth.AddCookie(task4SessionCookie(session))
	recAuth := httptest.NewRecorder()
	handler.ServeHTTP(recAuth, reqAuth)
	if recAuth.Code != http.StatusOK {
		t.Fatalf("expected 200 for authenticated mesh matrix, got %d: %s", recAuth.Code, recAuth.Body.String())
	}

	var resp MeshMatrixResponse
	if err := json.Unmarshal(recAuth.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(resp.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(resp.Nodes))
	}
	if len(resp.Matrix) != 3 {
		t.Fatalf("expected 3x3 matrix, got %d rows", len(resp.Matrix))
	}

	// Find index of SG, TYO, SFO in nodes
	nodeIndices := make(map[string]int)
	for i, n := range resp.Nodes {
		nodeIndices[n.ID] = i
	}
	idxSG := nodeIndices[nodeSG.ID]
	idxTYO := nodeIndices[nodeTYO.ID]
	idxSFO := nodeIndices[nodeSFO.ID]

	// Check self cell
	cellSelf := resp.Matrix[idxSG][idxSG]
	if cellSelf.Status != "self" || cellSelf.LatencyMS != 0 {
		t.Errorf("expected self cell 0ms, got status=%s latency=%f", cellSelf.Status, cellSelf.LatencyMS)
	}

	// Check SG -> TYO cell (65ms, good)
	cellSGtoTYO := resp.Matrix[idxSG][idxTYO]
	if cellSGtoTYO.LatencyMS != 65 || cellSGtoTYO.Status != "good" {
		t.Errorf("expected SG->TYO 65ms good, got %f ms, status=%s", cellSGtoTYO.LatencyMS, cellSGtoTYO.Status)
	}

	// Check SG -> SFO direct cell (280ms, high)
	cellSGtoSFO := resp.Matrix[idxSG][idxSFO]
	if cellSGtoSFO.LatencyMS != 280 || cellSGtoSFO.Status != "high" {
		t.Errorf("expected SG->SFO 280ms high, got %f ms, status=%s", cellSGtoSFO.LatencyMS, cellSGtoSFO.Status)
	}

	// Check Relay optimization: SG -> SFO via TYO (65 + 105 = 170ms vs 280ms, savings 110ms)
	if len(resp.Relays) == 0 {
		t.Fatalf("expected at least 1 relay recommendation, got 0")
	}

	foundRelay := false
	for _, r := range resp.Relays {
		if r.FromNodeID == nodeSG.ID && r.ToNodeID == nodeSFO.ID {
			foundRelay = true
			if r.RelayNodeID != nodeTYO.ID {
				t.Errorf("expected relay node to be TYO, got %s", r.RelayNodeID)
			}
			if r.RelayMS != 170 {
				t.Errorf("expected relay MS 170, got %f", r.RelayMS)
			}
			if r.SavingsMS != 110 {
				t.Errorf("expected savings 110, got %f", r.SavingsMS)
			}
			if r.SavingsPercent < 39 || r.SavingsPercent > 40 {
				t.Errorf("expected ~39.3%% savings, got %f%%", r.SavingsPercent)
			}
		}
	}
	if !foundRelay {
		t.Errorf("did not find expected relay from SG to SFO")
	}

	// Check stats
	if resp.Stats.TotalNodes != 3 {
		t.Errorf("expected 3 total nodes, got %d", resp.Stats.TotalNodes)
	}
	if resp.Stats.OnlineNodes != 3 {
		t.Errorf("expected 3 online nodes, got %d", resp.Stats.OnlineNodes)
	}
}

func TestPatchNode(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	server := NewServer(cfg, service)
	handler := server.Handler()
	session, csrf := task4AdminSession(t, service, store)

	node := publicStatusRegisterNode(t, handler, store, task4NodeUUID1, "Original-Name")

	// 1. Update name and tags with string
	patchBody1 := `{"name":"Singapore-Edge-01","tags":"sg,asia,edge,prod"}`
	rec1, csrf := task4AdminWriteWithCSRF(t, handler, http.MethodPatch, session, csrf, "/api/nodes/"+node.UUID, patchBody1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("expected 200 on PATCH /api/nodes, got %d: %s", rec1.Code, rec1.Body.String())
	}

	var nodeSummary nodeResponse
	if err := json.Unmarshal(rec1.Body.Bytes(), &nodeSummary); err != nil {
		t.Fatalf("unmarshal nodeSummary: %v", err)
	}
	if nodeSummary.Name != "Singapore-Edge-01" {
		t.Errorf("expected name Singapore-Edge-01, got %s", nodeSummary.Name)
	}
	if nodeSummary.Tags != "sg,asia,edge,prod" {
		t.Errorf("expected tags sg,asia,edge,prod, got %s", nodeSummary.Tags)
	}

	// 2. Update tags with array
	patchBody2 := `{"tags":["jp", "tokyo"]}`
	rec2, csrf := task4AdminWriteWithCSRF(t, handler, http.MethodPatch, session, csrf, "/api/nodes/"+node.ID, patchBody2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 on PATCH with array tags, got %d: %s", rec2.Code, rec2.Body.String())
	}
	var nodeSummary2 nodeResponse
	_ = json.Unmarshal(rec2.Body.Bytes(), &nodeSummary2)
	if nodeSummary2.Name != "Singapore-Edge-01" {
		t.Errorf("name should be unchanged, got %s", nodeSummary2.Name)
	}
	if nodeSummary2.Tags != "jp,tokyo" {
		t.Errorf("expected tags jp,tokyo, got %s", nodeSummary2.Tags)
	}

	// 3. Reject invalid name
	patchBodyInvalid := `{"name":""}`
	rec3, _ := task4AdminWriteWithCSRF(t, handler, http.MethodPatch, session, csrf, "/api/nodes/"+node.ID, patchBodyInvalid)
	if rec3.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on empty name, got %d", rec3.Code)
	}

	// 4. Reject unauthenticated request
	reqUnauth := httptest.NewRequest(http.MethodPatch, "/api/nodes/"+node.ID, strings.NewReader(`{"name":"NewName"}`))
	recUnauth := httptest.NewRecorder()
	handler.ServeHTTP(recUnauth, reqUnauth)
	if recUnauth.Code != http.StatusUnauthorized && recUnauth.Code != http.StatusForbidden {
		t.Fatalf("expected 401/403 for unauthenticated patch, got %d", recUnauth.Code)
	}
}

func TestNotificationChannelValidationAndCreatedAt(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	server := NewServer(cfg, service)
	handler := server.Handler()
	session, csrf := task4AdminSession(t, service, store)
	ctx := context.Background()

	// 1. Create a channel
	createBody := `{"name":"Telegram Alert","type":"telegram","config":"{\"bot_token\":\"12345:abc\",\"chat_id\":\"98765\"}","enabled":true,"events":"[\"offline\"]"}`
	recCreate, csrf := task4AdminWriteWithCSRF(t, handler, http.MethodPost, session, csrf, "/api/alerts/channels", createBody)
	if recCreate.Code != http.StatusCreated {
		t.Fatalf("expected 201 on create channel, got %d: %s", recCreate.Code, recCreate.Body.String())
	}
	var created db.NotificationChannel
	if err := json.Unmarshal(recCreate.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal created channel: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected non-empty channel ID")
	}

	origCh, err := store.GetNotificationChannel(ctx, created.ID)
	if err != nil {
		t.Fatalf("failed to retrieve channel from store: %v", err)
	}
	origCreatedAt := origCh.CreatedAt

	// 2. Reject update with empty name or type
	badBody := `{"name":"","type":"telegram","config":"{}"}`
	recBad, _ := task4AdminWriteWithCSRF(t, handler, http.MethodPut, session, csrf, "/api/alerts/channels/"+created.ID, badBody)
	if recBad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on empty name, got %d", recBad.Code)
	}

	csrf = task4CSRF(t, handler, session)

	// 3. Update channel and verify CreatedAt is preserved
	updateBody := `{"name":"Updated Channel","type":"telegram","config":"{\"bot_token\":\"******\",\"chat_id\":\"98765\"}","enabled":true,"events":"[\"system\"]"}`
	recUp, _ := task4AdminWriteWithCSRF(t, handler, http.MethodPut, session, csrf, "/api/alerts/channels/"+created.ID, updateBody)
	if recUp.Code != http.StatusOK {
		t.Fatalf("expected 200 on update channel, got %d: %s", recUp.Code, recUp.Body.String())
	}

	updatedCh, err := store.GetNotificationChannel(ctx, created.ID)
	if err != nil {
		t.Fatalf("failed to retrieve updated channel: %v", err)
	}
	if updatedCh.Name != "Updated Channel" {
		t.Errorf("expected name 'Updated Channel', got %s", updatedCh.Name)
	}
	if !updatedCh.CreatedAt.Equal(origCreatedAt) {
		t.Errorf("expected CreatedAt %v to be preserved, got %v", origCreatedAt, updatedCh.CreatedAt)
	}
}

func TestTargetScopingWithNodeTagsAndIDs(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	server := NewServer(cfg, service)
	handler := server.Handler()
	session, csrf := task4AdminSession(t, service, store)
	ctx := context.Background()
	now := time.Now().UTC()

	// Register 2 nodes
	regToken1, err := store.CreateRegistrationToken(ctx, 15*time.Minute)
	if err != nil {
		t.Fatalf("create regToken1: %v", err)
	}
	regNode1, err := store.RegisterNode(ctx, regToken1.Token, db.NodeInput{
		UUID: task4NodeUUID1,
		Name: "Node-SG",
	}, now)
	if err != nil {
		t.Fatalf("register node1: %v", err)
	}
	_ = store.UpdateNode(ctx, regNode1.Node.ID, "Node-SG", "asia,singapore")

	regToken2, err := store.CreateRegistrationToken(ctx, 15*time.Minute)
	if err != nil {
		t.Fatalf("create regToken2: %v", err)
	}
	regNode2, err := store.RegisterNode(ctx, regToken2.Token, db.NodeInput{
		UUID: task4NodeUUID2,
		Name: "Node-US",
	}, now)
	if err != nil {
		t.Fatalf("register node2: %v", err)
	}
	_ = store.UpdateNode(ctx, regNode2.Node.ID, "Node-US", "us,california")

	// Target A: Global (empty node_tags and node_ids)
	bodyA := `{"id":"tgt-global","name":"Global Check","kind":"tcp","host":"1.1.1.1","port":53}`
	recA, csrf := task4AdminWriteWithCSRF(t, handler, http.MethodPost, session, csrf, "/api/targets", bodyA)
	if recA.Code != http.StatusCreated {
		t.Fatalf("create global target: %d: %s", recA.Code, recA.Body.String())
	}

	// Target B: Scoped to tag "asia"
	bodyB := `{"id":"tgt-asia","name":"Asia Only","kind":"tcp","host":"8.8.8.8","port":53,"node_tags":["asia"]}`
	recB, csrf := task4AdminWriteWithCSRF(t, handler, http.MethodPost, session, csrf, "/api/targets", bodyB)
	if recB.Code != http.StatusCreated {
		t.Fatalf("create asia target: %d: %s", recB.Code, recB.Body.String())
	}

	// Target C: Scoped to Node 2 UUID
	bodyC := `{"id":"tgt-us-node","name":"US Node Only","kind":"tcp","host":"9.9.9.9","port":53,"node_ids":["` + task4NodeUUID2 + `"]}`
	recC, _ := task4AdminWriteWithCSRF(t, handler, http.MethodPost, session, csrf, "/api/targets", bodyC)
	if recC.Code != http.StatusCreated {
		t.Fatalf("create us node target: %d: %s", recC.Code, recC.Body.String())
	}

	// Now pull agent config as Node 1 (SG, has tag "asia")
	recConfig1 := meshAgentConfigGet(t, handler, regNode1.Token, "req-node-1")
	if recConfig1.Code != http.StatusOK {
		t.Fatalf("agent config node1: %d: %s", recConfig1.Code, recConfig1.Body.String())
	}
	var conf1 protocol.AgentConfigResponse
	_ = json.Unmarshal(recConfig1.Body.Bytes(), &conf1)

	// Node 1 should receive Global target and Asia target, but NOT US Node target
	if len(conf1.Tasks) != 2 {
		t.Fatalf("expected 2 tasks for Node 1, got %d", len(conf1.Tasks))
	}
	taskIDs1 := map[string]bool{conf1.Tasks[0].ID: true, conf1.Tasks[1].ID: true}
	if !taskIDs1["tgt-global"] || !taskIDs1["tgt-asia"] {
		t.Errorf("Node 1 expected tgt-global and tgt-asia, got %v", conf1.Tasks)
	}

	// Now pull agent config as Node 2 (US, has tag "us", uuid matches Target C)
	recConfig2 := meshAgentConfigGet(t, handler, regNode2.Token, "req-node-2")
	if recConfig2.Code != http.StatusOK {
		t.Fatalf("agent config node2: %d: %s", recConfig2.Code, recConfig2.Body.String())
	}
	var conf2 protocol.AgentConfigResponse
	_ = json.Unmarshal(recConfig2.Body.Bytes(), &conf2)

	// Node 2 should receive Global target and US Node target, but NOT Asia target
	if len(conf2.Tasks) != 2 {
		t.Fatalf("expected 2 tasks for Node 2, got %d", len(conf2.Tasks))
	}
	taskIDs2 := map[string]bool{conf2.Tasks[0].ID: true, conf2.Tasks[1].ID: true}
	if !taskIDs2["tgt-global"] || !taskIDs2["tgt-us-node"] {
		t.Errorf("Node 2 expected tgt-global and tgt-us-node, got %v", conf2.Tasks)
	}
}

func TestMatchesTargetNodeNameAndMeshHelpers(t *testing.T) {
	// 1. Target node name matching tests
	if matchesTargetNodeName("香港三网回程", "香港") {
		t.Errorf("expected false for substring match '香港三网回程' vs node '香港'")
	}
	if !matchesTargetNodeName("香港", "香港") {
		t.Errorf("expected true for exact match '香港'")
	}
	if !matchesTargetNodeName("to-香港", "香港") {
		t.Errorf("expected true for 'to-香港'")
	}
	if !matchesTargetNodeName("mesh-香港-tcp", "香港") {
		t.Errorf("expected true for 'mesh-香港-tcp'")
	}
	if !matchesTargetNodeName("香港 - 联通", "香港") {
		t.Errorf("expected true for '香港 - 联通'")
	}
	if !matchesTargetNodeName("node-01 -> 香港", "香港") {
		t.Errorf("expected true for 'node-01 -> 香港'")
	}

	// 2. Generic hostname tests
	if !isGenericHostname("debian") {
		t.Errorf("expected true for 'debian'")
	}
	if !isGenericHostname("localhost") {
		t.Errorf("expected true for 'localhost'")
	}
	if !isGenericHostname("ubuntu") {
		t.Errorf("expected true for 'ubuntu'")
	}
	if isGenericHostname("hk-node-01") {
		t.Errorf("expected false for 'hk-node-01'")
	}

	// 3. Public mesh IP tests
	if isPublicMeshIP("10.0.0.1") {
		t.Errorf("expected false for private 10.0.0.1")
	}
	if isPublicMeshIP("192.168.1.1") {
		t.Errorf("expected false for private 192.168.1.1")
	}
	if isPublicMeshIP("100.64.0.1") {
		t.Errorf("expected false for CGNAT 100.64.0.1")
	}
	if isPublicMeshIP("127.0.0.1") {
		t.Errorf("expected false for loopback 127.0.0.1")
	}
	if !isPublicMeshIP("198.51.100.1") {
		t.Errorf("expected true for public 198.51.100.1")
	}
	if !isPublicMeshIP("2606:4700:4700::1111") {
		t.Errorf("expected true for public IPv6")
	}
}
