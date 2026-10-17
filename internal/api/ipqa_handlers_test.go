package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	reqSync := httptest.NewRequest(http.MethodPost, "/api/nodes/"+node.UUID+"/ipqa/sync", bytes.NewBufferString(`{}`))
	reqSync.AddCookie(task4SessionCookie(session))
	reqSync.Header.Set("X-CSRF-Token", csrfToken)
	reqSync.Header.Set("Content-Type", "application/json")
	reqSync.Header.Set("Origin", "http://127.0.0.1:8080")
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
	testCSRF := task4CSRF(t, srv.Handler(), session)
	testBody := bytes.NewBufferString(`{"window": "today"}`)
	reqTest := httptest.NewRequest(http.MethodPost, "/api/nodes/"+node.UUID+"/ipqa/test", testBody)
	reqTest.AddCookie(task4SessionCookie(session))
	reqTest.Header.Set("X-CSRF-Token", testCSRF)
	reqTest.Header.Set("Content-Type", "application/json")
	reqTest.Header.Set("Origin", "http://127.0.0.1:8080")
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

func TestPopulatePublicIPQualityDTO_DualStackAndIPv6Fallback(t *testing.T) {
	proxyTrue := true
	vpnFalse := false
	scoreV4 := 13.18
	scoreV6 := 20.2

	legacy := &protocol.IPQualityInfo{
		IPType:       "unknown",
		Country:      "SG",
		Region:       "Central Singapore",
		City:         "Singapore",
		ASN:          "AS16509",
		Organization: "Amazon.com, Inc.",
		Risk:         "unknown",
	}
	qa := &protocol.IPQAInfo{
		Enabled:   true,
		Installed: true,
		State:     "enabled",
		IPv4: &protocol.IPQAFamilyInfo{
			RiskScore:    &scoreV4,
			RiskLevel:    "low",
			IPType:       "广播IP",
			Proxy:        &proxyTrue,
			VPN:          &vpnFalse,
			SourceScores: map[string]float64{"AbuseIPDB": 0, "IPQS": 75},
		},
		IPv6: &protocol.IPQAFamilyInfo{
			RiskScore:    &scoreV6,
			RiskLevel:    "low",
			IPType:       "广播IP",
			Proxy:        &proxyTrue,
			SourceScores: map[string]float64{"AbuseIPDB": 0, "IPQS": 95},
		},
	}

	dto := populatePublicIPQualityDTO(legacy, qa)
	if dto == nil {
		t.Fatal("expected non-nil dto")
	}
	if dto.Risk != "low" || dto.IPType != "广播IP" || dto.Source != "IPQA" {
		t.Fatalf("expected IPQA priority fields (low, 广播IP, IPQA), got risk=%q ipType=%q source=%q", dto.Risk, dto.IPType, dto.Source)
	}
	if dto.IPv4 == nil || dto.IPv6 == nil {
		t.Fatalf("expected both IPv4 and IPv6 family objects in DTO, got ipv4=%v ipv6=%v", dto.IPv4, dto.IPv6)
	}
	if dto.IPv4.SourceScores["IPQS"] != 75 || dto.IPv6.SourceScores["IPQS"] != 95 {
		t.Fatalf("expected distinct IPv4 (75) and IPv6 (95) IPQS scores, got v4=%v v6=%v", dto.IPv4.SourceScores, dto.IPv6.SourceScores)
	}
	if dto.Proxy == nil || *dto.Proxy != true {
		t.Fatalf("expected proxy=true from IPQA, got %v", dto.Proxy)
	}

	// IPv6-only fallback
	qaV6Only := &protocol.IPQAInfo{
		Enabled:   true,
		Installed: true,
		State:     "enabled",
		IPv6:      qa.IPv6,
	}
	dtoV6 := populatePublicIPQualityDTO(nil, qaV6Only)
	if dtoV6 == nil || !dtoV6.HasIPv6 || dtoV6.HasIPv4 {
		t.Fatalf("expected IPv6-only DTO flags, got %+v", dtoV6)
	}
	if dtoV6.Scores["IPQS"] != 95 || dtoV6.Risk != "low" {
		t.Fatalf("expected IPv6 fallback scores and risk, got scores=%v risk=%q", dtoV6.Scores, dtoV6.Risk)
	}
}

func TestIPQANotificationCooldownUnifiedKey(t *testing.T) {
	srv, service, store, node1 := setupTestServerForIPQA(t)
	ctx := context.Background()

	// Ensure notifier is enabled by registering a notification channel
	nowUTC := time.Now().UTC()
	err := store.UpsertNotificationChannel(ctx, db.NotificationChannel{
		ID:        "chan-1",
		Name:      "Test-Channel",
		Type:      "telegram",
		Config:    `{"bot_token":"test-token","chat_id":"test-chat"}`,
		Enabled:   true,
		CreatedAt: nowUTC,
		UpdatedAt: nowUTC,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Register a second node with different ID and UUID
	token2, err := store.CreateRegistrationToken(ctx, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	reg2 := task4Register(t, srv.Handler(), protocol.RegisterRequest{
		NodeUUID:          "020ae432-2c08-4eef-9133-18289643549f",
		Name:              "Test Node 2",
		RegistrationToken: token2.Token,
	})
	node2 := mustNode(t, store, reg2.NodeUUID)

	// Verify node1.ID != node1.UUID
	if node1.ID == node1.UUID {
		t.Fatalf("node.ID should differ from node.UUID, got ID=%s UUID=%s", node1.ID, node1.UUID)
	}

	session, _ := task4AdminSession(t, service, store)

	// Reset cooldown state before tests
	resetCooldowns := func() {
		ipqaCooldownMu.Lock()
		ipqaLastNotify = make(map[string]time.Time)
		ipqaNotifiedKeys = make(map[string]struct{})
		ipqaCooldownMu.Unlock()
	}

	sendManualTest := func(node db.Node) *httptest.ResponseRecorder {
		csrf := task4CSRF(t, srv.Handler(), session)
		req := httptest.NewRequest(http.MethodPost, "/api/nodes/"+node.UUID+"/ipqa/test", bytes.NewBufferString(`{"window":"today","force_send":true}`))
		req.AddCookie(task4SessionCookie(session))
		req.Header.Set("X-CSRF-Token", csrf)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://127.0.0.1:8080")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec
	}

	makeIPQAInfo := func(date string, sev string, alertCount int) *protocol.IPQAInfo {
		return &protocol.IPQAInfo{
			Enabled:           true,
			Installed:         true,
			LastCheckedAt:     time.Now().UTC().Unix(),
			LatestArchiveDate: date,
			HighestSeverity:   sev,
			AlertCount:        alertCount,
		}
	}

	// Scenario 1: Auto IPQA alert sets node.ID cooldown -> subsequent manual test blocked by 5m cooldown
	t.Run("AutoReportBlocksSubsequentManualTest", func(t *testing.T) {
		resetCooldowns()
		now := time.Now().UTC()
		info := makeIPQAInfo("2026-10-15-A", "CRITICAL", 3)

		// Trigger auto report alert
		srv.handleIPQAReport(ctx, node1, info, now)

		// Verify cooldown was recorded under node1.ID (NOT node1.UUID)
		ipqaCooldownMu.Lock()
		_, hasID := ipqaLastNotify[node1.ID]
		_, hasUUID := ipqaLastNotify[node1.UUID]
		ipqaCooldownMu.Unlock()

		if !hasID {
			t.Fatalf("expected cooldown key to be node.ID (%s)", node1.ID)
		}
		if hasUUID {
			t.Fatalf("cooldown key should NOT be node.UUID (%s)", node1.UUID)
		}

		// Subsequent manual test for node1 within 5m must be blocked with HTTP 429
		rec := sendManualTest(node1)
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("expected 429 Too Many Requests, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	// Scenario 2: Manual test sets node.ID cooldown -> subsequent auto IPQA alert blocked
	t.Run("ManualTestBlocksSubsequentAutoReport", func(t *testing.T) {
		resetCooldowns()

		// First manual test succeeds
		rec := sendManualTest(node1)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for initial manual test, got %d: %s", rec.Code, rec.Body.String())
		}

		// Check cooldown map
		ipqaCooldownMu.Lock()
		lastSent, hasID := ipqaLastNotify[node1.ID]
		_, hasUUID := ipqaLastNotify[node1.UUID]
		ipqaCooldownMu.Unlock()

		if !hasID {
			t.Fatalf("manual test must write cooldown key under node.ID (%s)", node1.ID)
		}
		if hasUUID {
			t.Fatalf("manual test must NOT write cooldown key under node.UUID (%s)", node1.UUID)
		}

		// Now simulate auto report arriving 30 seconds later
		info := makeIPQAInfo("2026-10-15-B", "WARNING", 1)
		srv.handleIPQAReport(ctx, node1, info, lastSent.Add(30*time.Second))

		// Dedupe key must NOT have been marked as sent because cooldown blocked it
		dedupeKey := fmt.Sprintf("%s:%s:%s", node1.ID, "2026-10-15-B", "warning")
		ipqaCooldownMu.Lock()
		_, wasNotified := ipqaNotifiedKeys[dedupeKey]
		ipqaCooldownMu.Unlock()

		if wasNotified {
			t.Fatalf("auto report should have been suppressed by cooldown from manual test")
		}
	})

	// Scenario 3: Two different nodes do not affect each other
	t.Run("CooldownIsolationBetweenNodes", func(t *testing.T) {
		resetCooldowns()

		// Set cooldown on node1
		rec1 := sendManualTest(node1)
		if rec1.Code != http.StatusOK {
			t.Fatalf("expected node1 manual test 200, got %d", rec1.Code)
		}

		// node2 manual test should still succeed
		rec2 := sendManualTest(node2)
		if rec2.Code != http.StatusOK {
			t.Fatalf("expected node2 manual test 200, got %d: %s", rec2.Code, rec2.Body.String())
		}

		// node2 auto report should be blocked by its OWN manual test, not node1
		info := makeIPQAInfo("2026-10-15-C", "CRITICAL", 2)
		srv.handleIPQAReport(ctx, node2, info, time.Now().UTC())

		dedupeKey2 := fmt.Sprintf("%s:%s:%s", node2.ID, "2026-10-15-C", "critical")
		ipqaCooldownMu.Lock()
		_, wasNotified2 := ipqaNotifiedKeys[dedupeKey2]
		ipqaCooldownMu.Unlock()
		if wasNotified2 {
			t.Fatalf("node2 auto report should be blocked by node2's own cooldown")
		}
	})

	// Scenario 4: Concurrency test - concurrent manual test calls only send one
	t.Run("ConcurrentManualTestsCooldownRace", func(t *testing.T) {
		resetCooldowns()
		const concurrentCalls = 10
		type resInfo struct {
			code int
			body string
		}
		resChan := make(chan resInfo, concurrentCalls)

		sessions := make([]string, concurrentCalls)
		csrfTokens := make([]string, concurrentCalls)
		for i := 0; i < concurrentCalls; i++ {
			s, c := task4AdminSession(t, service, store)
			sessions[i] = s
			csrfTokens[i] = c
		}

		for i := 0; i < concurrentCalls; i++ {
			idx := i
			go func() {
				req := httptest.NewRequest(http.MethodPost, "/api/nodes/"+node1.UUID+"/ipqa/test", bytes.NewBufferString(`{"window":"today","force_send":true}`))
				req.AddCookie(task4SessionCookie(sessions[idx]))
				req.Header.Set("X-CSRF-Token", csrfTokens[idx])
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Origin", "http://127.0.0.1:8080")
				rec := httptest.NewRecorder()
				srv.Handler().ServeHTTP(rec, req)
				resChan <- resInfo{code: rec.Code, body: rec.Body.String()}
			}()
		}

		status200Count := 0
		status429Count := 0
		var otherCodes []string
		for i := 0; i < concurrentCalls; i++ {
			res := <-resChan
			if res.code == http.StatusOK {
				status200Count++
			} else if res.code == http.StatusTooManyRequests {
				status429Count++
			} else {
				otherCodes = append(otherCodes, fmt.Sprintf("code=%d body=%s", res.code, res.body))
			}
		}

		if len(otherCodes) > 0 {
			t.Logf("other response codes: %v", otherCodes)
		}

		if status200Count != 1 {
			t.Errorf("expected exactly 1 concurrent test to succeed with 200, got %d", status200Count)
		}
		if status429Count != concurrentCalls-1 {
			t.Errorf("expected %d calls to be 429 Too Many Requests, got %d", concurrentCalls-1, status429Count)
		}
	})

	// Scenario 5: Expiration allows sending again after 5 minutes
	t.Run("CooldownExpiryAllowsSubsequentNotification", func(t *testing.T) {
		resetCooldowns()

		// Set cooldown timestamp to 6 minutes ago
		sixMinutesAgo := time.Now().UTC().Add(-6 * time.Minute)
		ipqaCooldownMu.Lock()
		ipqaLastNotify[node1.ID] = sixMinutesAgo
		ipqaCooldownMu.Unlock()

		// Manual test should succeed now
		rec := sendManualTest(node1)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK after cooldown expiry, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestNodeIPQASync_HeaderPropagation(t *testing.T) {
	service, store := newTask4Auth(t)
	srv := NewServer(task4Config(), service)
	ctx := context.Background()

	// 1. Register test node
	regToken, err := store.CreateRegistrationToken(ctx, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	registered := task4Register(t, srv.Handler(), protocol.RegisterRequest{
		NodeUUID:          "a10ae432-2c08-4eef-9133-18289643549a",
		Name:              "Sync Propagation Node",
		RegistrationToken: regToken.Token,
	})
	node := mustNode(t, store, registered.NodeUUID)

	session, csrfToken := task4AdminSession(t, service, store)

	// 2. Initial report from node before sync -> should NOT have X-Probe-Sync-IPQA
	report1 := protocol.ReportRequest{
		NodeUUID:   node.UUID,
		ReportedAt: time.Now().Unix(),
		Resource: protocol.ResourceSnapshot{
			Hostname: "sync-node",
		},
	}
	res1 := task4AgentPost(t, srv.Handler(), "/api/agent/v1/report", registered.NodeToken, node.UUID, "rep-init", report1)
	if res1.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", res1.Code)
	}
	if res1.Header().Get("X-Probe-Sync-IPQA") != "" {
		t.Fatalf("expected empty X-Probe-Sync-IPQA before sync, got %q", res1.Header().Get("X-Probe-Sync-IPQA"))
	}

	// 3. Admin requests manual sync
	reqSync := httptest.NewRequest(http.MethodPost, "/api/nodes/"+node.UUID+"/ipqa/sync", bytes.NewBufferString(`{}`))
	reqSync.AddCookie(task4SessionCookie(session))
	reqSync.Header.Set("X-CSRF-Token", csrfToken)
	reqSync.Header.Set("Content-Type", "application/json")
	reqSync.Header.Set("Origin", "http://127.0.0.1:8080")
	recSync := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recSync, reqSync)
	if recSync.Code != http.StatusOK {
		t.Fatalf("expected 200 for sync, got %d (body: %s)", recSync.Code, recSync.Body.String())
	}

	// 4. Next report from node -> MUST have X-Probe-Sync-IPQA = true
	report2 := protocol.ReportRequest{
		NodeUUID:   node.UUID,
		ReportedAt: time.Now().Unix(),
		Resource: protocol.ResourceSnapshot{
			Hostname: "sync-node",
		},
	}
	res2 := task4AgentPost(t, srv.Handler(), "/api/agent/v1/report", registered.NodeToken, node.UUID, "rep-sync", report2)
	if res2.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", res2.Code)
	}
	if res2.Header().Get("X-Probe-Sync-IPQA") != "true" {
		t.Fatalf("expected X-Probe-Sync-IPQA=true on first report after sync, got %q", res2.Header().Get("X-Probe-Sync-IPQA"))
	}

	// 5. Subsequent report -> MUST be cleared
	report3 := protocol.ReportRequest{
		NodeUUID:   node.UUID,
		ReportedAt: time.Now().Unix(),
		Resource: protocol.ResourceSnapshot{
			Hostname: "sync-node",
		},
	}
	res3 := task4AgentPost(t, srv.Handler(), "/api/agent/v1/report", registered.NodeToken, node.UUID, "rep-after", report3)
	if res3.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", res3.Code)
	}
	if res3.Header().Get("X-Probe-Sync-IPQA") != "" {
		t.Fatalf("expected empty X-Probe-Sync-IPQA on subsequent report, got %q", res3.Header().Get("X-Probe-Sync-IPQA"))
	}
}
