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

	"github.com/probewatch/probewatch/internal/auth"
	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/protocol"
)

func setupTestServerForIPQA(t *testing.T) (*Server, *auth.Service, *db.Store, db.Node) {
	t.Helper()
	service, store := newTask4Auth(t)
	srv := NewServer(task4Config(), service)
	ctx := context.Background()

	// Register test node
	token, err := store.CreateRegistrationToken(ctx, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	reg := task4Register(t, srv.Handler(), protocol.RegisterRequest{
		NodeUUID:          "010ae432-2c08-4eef-9133-18289643549f",
		Name:              "Test Node 1",
		RegistrationToken: token.Token,
	})
	node := mustNode(t, store, reg.NodeUUID)

	// Persist snapshot with IPQA
	proxy := true
	snapshot := protocol.ResourceSnapshot{
		Hostname: "test-vps",
		IPQA: &protocol.IPQAInfo{
			Enabled:           true,
			Installed:         true,
			LastCheckedAt:     time.Now().UTC().Unix(),
			LatestArchiveDate: "2026-10-06",
			HighestSeverity:   "WARNING",
			AlertCount:        2,
			WarningCount:      2,
			IPv4: &protocol.IPQAFamilyInfo{
				IPType:    "hosting",
				Country:   "US",
				Region:    "California",
				ASN:       "AS15169",
				Proxy:     &proxy,
				RiskLevel: "medium",
			},
			Changes: []protocol.IPQAChange{
				{
					Category:  "factor",
					Severity:  "WARNING",
					Before:    "Proxy: false",
					After:     "Proxy: true",
					ChangedAt: time.Now().UTC().Unix(),
				},
			},
		},
	}
	payload, _ := json.Marshal(snapshot)
	_ = store.PersistAgentReport(ctx, db.AgentReportInput{
		NodeID:          node.ID,
		RequestID:       "req-ipqa-1",
		ReplayExpiresAt: time.Now().Add(time.Hour),
		Now:             time.Now(),
		ReportedAt:      time.Now(),
		ResourcePayload: payload,
	})

	return srv, service, store, node
}

func TestPublicIPQualityHandler_IncludesIPQAWithoutLeakingSensitiveData(t *testing.T) {
	srv, _, _, node := setupTestServerForIPQA(t)

	req := httptest.NewRequest(http.MethodGet, "/api/public/nodes/"+node.UUID+"/ip-quality", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var res map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	dto, ok := res["ip_quality"].(map[string]any)
	if !ok || dto == nil {
		t.Fatalf("expected ip_quality in response: %v", res)
	}

	// Verify whitelisted IPQA fields
	if dto["ipqa_enabled"] != true {
		t.Fatalf("expected ipqa_enabled = true, got %v", dto["ipqa_enabled"])
	}
	if dto["ipqa_installed"] != true {
		t.Fatalf("expected ipqa_installed = true, got %v", dto["ipqa_installed"])
	}
	if dto["highest_severity"] != "WARNING" {
		t.Fatalf("expected highest_severity = WARNING, got %v", dto["highest_severity"])
	}
	if dto["alert_count"] != float64(2) {
		t.Fatalf("expected alert_count = 2, got %v", dto["alert_count"])
	}
	if dto["has_recent_changes"] != true {
		t.Fatalf("expected has_recent_changes = true, got %v", dto["has_recent_changes"])
	}

	// Verify NO sensitive leakages
	bodyStr := rec.Body.String()
	for _, forbidden := range []string{"token", "password", "path", ".ipqa", "alerts.log", "127.0.0.1"} {
		if bytes.Contains([]byte(strings.ToLower(bodyStr)), []byte(forbidden)) {
			t.Fatalf("forbidden token %q leaked in public response: %s", forbidden, bodyStr)
		}
	}
}

func TestAdminIPQA_RequiresAuthentication(t *testing.T) {
	srv, _, _, node := setupTestServerForIPQA(t)

	for _, path := range []string{
		"/api/nodes/" + node.UUID + "/ipqa",
		"/api/nodes/" + node.UUID + "/ipqa/history",
		"/api/nodes/" + node.UUID + "/ipqa/changes",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for unauthenticated %s, got %d", path, rec.Code)
		}
	}
}

func TestAdminIPQA_AuthenticatedEndpoints(t *testing.T) {
	srv, service, store, node := setupTestServerForIPQA(t)
	ctx := context.Background()

	session, csrfToken := task4AdminSession(t, service, store)

	// 1. GET /api/nodes/:uuid/ipqa
	req := httptest.NewRequest(http.MethodGet, "/api/nodes/"+node.UUID+"/ipqa", nil)
	req.AddCookie(task4SessionCookie(session))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	var ipqaResp AdminIPQAResponse
	if err := json.NewDecoder(rec.Body).Decode(&ipqaResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !ipqaResp.Enabled || !ipqaResp.Installed || ipqaResp.AlertCount != 2 {
		t.Fatalf("unexpected ipqa response: %+v", ipqaResp)
	}

	// 2. GET /api/nodes/:uuid/ipqa/changes
	reqChanges := httptest.NewRequest(http.MethodGet, "/api/nodes/"+node.UUID+"/ipqa/changes", nil)
	reqChanges.AddCookie(task4SessionCookie(session))
	recChanges := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recChanges, reqChanges)
	if recChanges.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recChanges.Code)
	}

	// 3. POST /api/nodes/:uuid/ipqa/sync (with CSRF)
	reqSync := httptest.NewRequest(http.MethodPost, "/api/nodes/"+node.UUID+"/ipqa/sync", nil)
	reqSync.AddCookie(task4SessionCookie(session))
	reqSync.Header.Set("X-CSRF-Token", csrfToken)
	recSync := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recSync, reqSync)
	if recSync.Code != http.StatusOK {
		t.Fatalf("expected 200 for sync, got %d (body: %s)", recSync.Code, recSync.Body.String())
	}

	// Verify audit log entry was created for sync
	auditLogs, total, err := store.ListAuditLogs(ctx, 10, 0, "node.ipqa_sync")
	if err != nil || total < 1 || len(auditLogs) < 1 {
		t.Fatalf("expected audit log entry for node.ipqa_sync, err: %v, total: %d", err, total)
	}

	// 4. POST /api/nodes/:uuid/ipqa/test (dry-run, default force_send=false)
	testBody := bytes.NewBufferString(`{"window": "today"}`)
	reqTest := httptest.NewRequest(http.MethodPost, "/api/nodes/"+node.UUID+"/ipqa/test", testBody)
	reqTest.AddCookie(task4SessionCookie(session))
	reqTest.Header.Set("X-CSRF-Token", csrfToken)
	recTest := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recTest, reqTest)
	if recTest.Code != http.StatusOK {
		t.Fatalf("expected 200 for test dry run, got %d (body: %s)", recTest.Code, recTest.Body.String())
	}
	var testResp map[string]any
	_ = json.NewDecoder(recTest.Body).Decode(&testResp)
	if testResp["dry_run"] != true || testResp["notification_sent"] != false {
		t.Fatalf("expected dry_run = true, notification_sent = false, got %+v", testResp)
	}
}
