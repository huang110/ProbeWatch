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

func TestSanitizePublicSpeedtestResultsKeepsNodeNameAligned(t *testing.T) {
	response := SpeedtestResultsResponse{Rankings: []SpeedtestRankItem{
		{NodeID: "node-a"},
		{NodeID: "node-b"},
		{NodeID: "node-a"},
	}}
	sanitized := sanitizePublicSpeedtestResults(response)
	want := []string{"node-1", "node-2", "node-1"}
	wantNames := []string{"Node 1", "Node 2", "Node 1"}
	for i := range want {
		if sanitized.Rankings[i].NodeID != want[i] || sanitized.Rankings[i].NodeName != wantNames[i] {
			t.Fatalf("ranking[%d] = %+v, want ID %s and name %s", i, sanitized.Rankings[i], want[i], wantNames[i])
		}
	}
}

func TestSpeedtestTasksCRUDAndResults(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	server := NewServer(cfg, service)
	handler := server.Handler()
	session, csrf := task4AdminSession(t, service, store)
	ctx := context.Background()

	// 1. Create Speedtest Task
	createPayload := map[string]any{
		"id":               "speed-cloudflare",
		"name":             "Cloudflare Speed Benchmark",
		"server_url":       "https://speed.cloudflare.com/__down?bytes=10485760",
		"download_bytes":   10485760,
		"upload_bytes":     5242880,
		"interval_seconds": 1800,
		"node_tags":        []string{"edge", "asia"},
		"node_ids":         []string{},
		"enabled":          true,
	}
	body, _ := json.Marshal(createPayload)
	req := httptest.NewRequest(http.MethodPost, "/api/speedtest/tasks", bytes.NewReader(body))
	req.AddCookie(task4SessionCookie(session))
	req.Header.Set("X-CSRF-Token", csrf)
	req.Header.Set("Origin", "http://127.0.0.1:8080")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	// 2. List Speedtest Tasks
	reqList := httptest.NewRequest(http.MethodGet, "/api/speedtest/tasks", nil)
	reqList.AddCookie(task4SessionCookie(session))
	recList := httptest.NewRecorder()
	handler.ServeHTTP(recList, reqList)

	if recList.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", recList.Code)
	}
	var tasks []db.SpeedtestTaskRecord
	if err := json.Unmarshal(recList.Body.Bytes(), &tasks); err != nil {
		t.Fatalf("failed to decode tasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != "speed-cloudflare" {
		t.Fatalf("unexpected tasks list: %#v", tasks)
	}

	// 3. Patch Speedtest Task
	csrf = task4CSRF(t, handler, session)
	patchPayload := map[string]any{
		"interval_seconds": 3600,
	}
	patchBody, _ := json.Marshal(patchPayload)
	reqPatch := httptest.NewRequest(http.MethodPatch, "/api/speedtest/tasks/speed-cloudflare", bytes.NewReader(patchBody))
	reqPatch.AddCookie(task4SessionCookie(session))
	reqPatch.Header.Set("X-CSRF-Token", csrf)
	reqPatch.Header.Set("Origin", "http://127.0.0.1:8080")
	reqPatch.Header.Set("Content-Type", "application/json")
	recPatch := httptest.NewRecorder()
	handler.ServeHTTP(recPatch, reqPatch)

	if recPatch.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", recPatch.Code)
	}

	// 4. Register a Node and submit speedtest result via /api/agent/v1/speedtest-result
	csrf = task4CSRF(t, handler, session)
	reg, _ := task4CreateRegistration(t, handler, session, csrf)
	regResp := task4Register(t, handler, protocol.RegisterRequest{
		RegistrationToken: reg.Token,
		NodeUUID:          task4NodeUUID1,
		Name:              "HK-Edge-01",
	})
	node := mustNode(t, store, regResp.NodeUUID)
	// Before assigning the node, the scoped task must not be delivered.
	recCfgUnassigned := meshAgentConfigGet(t, handler, regResp.NodeToken, "cfg-req-unassigned")
	if recCfgUnassigned.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from unassigned config, got %d", recCfgUnassigned.Code)
	}
	var unassignedCfg protocol.AgentConfigResponse
	_ = json.Unmarshal(recCfgUnassigned.Body.Bytes(), &unassignedCfg)
	for _, tk := range unassignedCfg.Tasks {
		if tk.ID == "speed-cloudflare" {
			t.Fatal("task should not match node without required tags")
		}
	}
	// The real endpoint rejects results from an unassigned node. Assign the
	// node before submitting a result, representing the normal agent flow.
	if err := store.UpdateNode(ctx, node.ID, "HK-Edge-01", "edge,asia"); err != nil {
		t.Fatalf("tag speedtest node: %v", err)
	}
	now := time.Now().UTC()

	speedResEnv := protocol.SpeedtestResultEnvelope{
		TaskID: "speed-cloudflare",
		Result: protocol.SpeedtestResult{
			ServerName:        "Cloudflare Edge",
			ServerURL:         "https://speed.cloudflare.com/__down",
			DownloadSpeedMbps: 245.8,
			UploadSpeedMbps:   92.4,
			LatencyMS:         12,
			JitterMS:          1,
			BytesReceived:     10485760,
			BytesSent:         5242880,
			DurationMS:        1800,
			Status:            "ok",
			TestedAt:          now.Unix(),
		},
	}
	envBytes, _ := json.Marshal(speedResEnv)
	reqResult := httptest.NewRequest(http.MethodPost, "/api/agent/v1/speedtest-result", bytes.NewReader(envBytes))
	reqResult.Header.Set("Authorization", "Bearer "+regResp.NodeToken)
	reqResult.Header.Set("X-Probe-Timestamp", strconv.FormatInt(now.Unix(), 10))
	reqResult.Header.Set("X-Probe-Request-ID", "speedtest-req-001")
	reqResult.Header.Set("Content-Type", "application/json")
	recResult := httptest.NewRecorder()
	handler.ServeHTTP(recResult, reqResult)

	if recResult.Code != http.StatusNoContent {
		t.Fatalf("expected 204 No Content, got %d (body: %s)", recResult.Code, recResult.Body.String())
	}

	// 5. Query Speedtest Results Dashboard API
	reqResults := httptest.NewRequest(http.MethodGet, "/api/speedtest/results", nil)
	reqResults.AddCookie(task4SessionCookie(session))
	recResults := httptest.NewRecorder()
	handler.ServeHTTP(recResults, reqResults)

	if recResults.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d (body: %s)", recResults.Code, recResults.Body.String())
	}
	var resp SpeedtestResultsResponse
	if err := json.Unmarshal(recResults.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode speedtest results response: %v", err)
	}

	if resp.Stats.ActiveBenchmarkNodes != 1 {
		t.Errorf("expected 1 active benchmark node, got %d", resp.Stats.ActiveBenchmarkNodes)
	}
	if resp.Stats.MaxDownloadMbps != 245.8 {
		t.Errorf("expected max download 245.8, got %f", resp.Stats.MaxDownloadMbps)
	}
	if resp.Stats.MaxUploadMbps != 92.4 {
		t.Errorf("expected max upload 92.4, got %f", resp.Stats.MaxUploadMbps)
	}
	if len(resp.Rankings) != 1 || resp.Rankings[0].NodeID != node.ID {
		t.Fatalf("unexpected rankings: %#v", resp.Rankings)
	}

	// 6. Query Public Speedtest Results Endpoint
	reqPub := httptest.NewRequest(http.MethodGet, "/api/public/speedtest/results", nil)
	recPub := httptest.NewRecorder()
	handler.ServeHTTP(recPub, reqPub)
	if recPub.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on public endpoint, got %d", recPub.Code)
	}
	var publicResp SpeedtestResultsResponse
	if err := json.Unmarshal(recPub.Body.Bytes(), &publicResp); err != nil {
		t.Fatalf("failed to decode public speedtest response: %v", err)
	}
	if len(publicResp.Results) != 0 {
		t.Fatalf("public speedtest response leaked raw results: %#v", publicResp.Results)
	}
	if len(publicResp.Rankings) != 1 || publicResp.Rankings[0].NodeID == node.ID || publicResp.Rankings[0].ServerName != "Benchmark" {
		t.Fatalf("public speedtest ranking was not sanitized: %#v", publicResp.Rankings)
	}

	// 7. Verify the now-assigned task is delivered in /api/agent/v1/config
	recCfg := meshAgentConfigGet(t, handler, regResp.NodeToken, "cfg-req-1")
	if recCfg.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from config, got %d", recCfg.Code)
	}
	var cfgResp protocol.AgentConfigResponse
	_ = json.Unmarshal(recCfg.Body.Bytes(), &cfgResp)
	hasSpeed := false
	for _, tk := range cfgResp.Tasks {
		if tk.ID == "speed-cloudflare" {
			hasSpeed = true
		}
	}
	if !hasSpeed {
		t.Fatal("expected matching tagged node to receive speedtest task")
	}
	for _, tk := range cfgResp.Tasks {
		if tk.ID == "speed-cloudflare" {
			if tk.Kind != "speedtest" || tk.DownloadBytes != 10485760 {
				t.Fatalf("task misconfigured: %#v", tk)
			}
		}
	}

	// 8. Test Speedtest Run trigger
	csrf = task4CSRF(t, handler, session)
	reqRun := httptest.NewRequest(http.MethodPost, "/api/speedtest/run", strings.NewReader(`{}`))
	reqRun.AddCookie(task4SessionCookie(session))
	reqRun.Header.Set("X-CSRF-Token", csrf)
	reqRun.Header.Set("Origin", "http://127.0.0.1:8080")
	reqRun.Header.Set("Content-Type", "application/json")
	recRun := httptest.NewRecorder()
	handler.ServeHTTP(recRun, reqRun)
	if recRun.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from run trigger, got %d", recRun.Code)
	}

	// 9. Delete Speedtest Task
	csrf = task4CSRF(t, handler, session)
	reqDel := httptest.NewRequest(http.MethodDelete, "/api/speedtest/tasks/speed-cloudflare", nil)
	reqDel.AddCookie(task4SessionCookie(session))
	reqDel.Header.Set("X-CSRF-Token", csrf)
	reqDel.Header.Set("Origin", "http://127.0.0.1:8080")
	recDel := httptest.NewRecorder()
	handler.ServeHTTP(recDel, reqDel)
	if recDel.Code != http.StatusNoContent {
		t.Fatalf("expected 204 No Content on delete, got %d", recDel.Code)
	}
}

func TestSpeedtestHistoryScopedLimitDoesNotDropAuthorizedRows(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	server := NewServer(cfg, service)
	handler := server.Handler()
	_, _ = task4AdminSession(t, service, store)

	allowedNode := publicStatusRegisterNode(t, handler, store, task4NodeUUID1, "Allowed speed node")
	forbiddenNode := publicStatusRegisterNode(t, handler, store, task4NodeUUID2, "Forbidden speed node")
	now := time.Now().UTC().Truncate(time.Second)
	if err := store.CreateSpeedtestTask(context.Background(), db.SpeedtestTaskRecord{
		ID: "speed-scope-history", Name: "Scoped benchmark", ServerURL: "https://speed.example.com", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	oldAt := now.Add(-time.Hour)
	newAt := now.Add(-30 * time.Minute)
	for _, row := range []struct {
		node string
		at   time.Time
		dl   float64
	}{
		{allowedNode.ID, oldAt, 111},
		{forbiddenNode.ID, newAt, 222},
	} {
		payload, _ := json.Marshal(protocol.SpeedtestResult{DownloadSpeedMbps: row.dl, TestedAt: row.at.Unix(), Status: "ok"})
		if _, err := store.DB().Exec(`INSERT INTO speedtest_results_history (node_id, task_id, payload, download_speed_mbps, upload_speed_mbps, latency_ms, jitter_ms, tested_at, recorded_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			row.node, "speed-scope-history", payload, row.dl, 1, 2, 3, row.at.UnixNano(), row.at.UnixNano()); err != nil {
			t.Fatal(err)
		}
	}

	history, err := store.ListSpeedtestHistoryForNodes(context.Background(), []string{allowedNode.ID}, "speed-scope-history", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].NodeID != allowedNode.ID || history[0].DownloadSpeedMbps != 111 || history[0].Status != "ok" || !history[0].TestedAt.Equal(oldAt) {
		t.Fatalf("scoped speedtest history = %#v, want authorized older row", history)
	}
	if got, err := store.ListSpeedtestHistoryForNodes(context.Background(), nil, "speed-scope-history", 1); err != nil {
		t.Fatal(err)
	} else if len(got) != 0 {
		t.Fatalf("empty node scope returned %d rows, want 0", len(got))
	}
}
