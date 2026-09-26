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
	reg, _ := task4CreateRegistration(t, handler, session, csrf)
	regResp := task4Register(t, handler, protocol.RegisterRequest{
		RegistrationToken: reg.Token,
		NodeUUID:          task4NodeUUID1,
		Name:              "HK-Edge-01",
	})
	node := mustNode(t, store, regResp.NodeUUID)
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

	// 7. Verify Task Scoping in /api/agent/v1/config
	// Node has no tags, so matching node_tags ["edge", "asia"] should not deliver it
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
	if hasSpeed {
		t.Fatal("task should not match node without required tags")
	}

	// Now tag the node with "edge"
	_ = store.UpdateNode(ctx, node.ID, "HK-Edge-01", "edge,asia")
	recCfg2 := meshAgentConfigGet(t, handler, regResp.NodeToken, "cfg-req-2")
	var cfgResp2 protocol.AgentConfigResponse
	_ = json.Unmarshal(recCfg2.Body.Bytes(), &cfgResp2)
	hasSpeed2 := false
	for _, tk := range cfgResp2.Tasks {
		if tk.ID == "speed-cloudflare" {
			hasSpeed2 = true
			if tk.Kind != "speedtest" || tk.DownloadBytes != 10485760 {
				t.Fatalf("task misconfigured: %#v", tk)
			}
		}
	}
	if !hasSpeed2 {
		t.Fatal("expected matching tagged node to receive speedtest task")
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
