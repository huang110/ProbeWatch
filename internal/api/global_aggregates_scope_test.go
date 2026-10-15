package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/ai"
	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/protocol"
)

func TestGlobalAggregatesAllowedNodeScope(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	server := NewServer(cfg, service)
	handler := server.Handler()

	ctx := context.Background()
	now := time.Now().UTC()

	// 1. Register Node 1 (Allowed) and Node 2 (Forbidden)
	regToken1, err := store.CreateRegistrationToken(ctx, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	reg1 := task4Register(t, handler, protocol.RegisterRequest{
		NodeUUID:          "550e8400-e29b-41d4-a716-446655440001",
		Name:              "Node-Allowed-1",
		RegistrationToken: regToken1.Token,
	})

	regToken2, err := store.CreateRegistrationToken(ctx, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	reg2 := task4Register(t, handler, protocol.RegisterRequest{
		NodeUUID:          "550e8400-e29b-41d4-a716-446655440002",
		Name:              "Node-Forbidden-2",
		RegistrationToken: regToken2.Token,
	})

	node1, err := store.GetNodeByUUID(ctx, reg1.NodeUUID)
	if err != nil {
		t.Fatal(err)
	}
	node2, err := store.GetNodeByUUID(ctx, reg2.NodeUUID)
	if err != nil {
		t.Fatal(err)
	}

	// 2. Seed Workloads & Containers
	// Node 1: Docker available, 2 containers (1 running, 1 stopped, 1 unhealthy)
	workload1 := protocol.NodeWorkloadReport{
		NodeUUID:          node1.UUID,
		ReportedAt:        now.Unix(),
		DockerAvailable:   true,
		DockerVersion:     "24.0.5",
		ContainersTotal:   2,
		ContainersRunning: 1,
		ContainersStopped: 1,
		Containers: []protocol.ContainerSnapshot{
			{
				ID:               "c1-node1",
				Names:            []string{"web-app-1"},
				Image:            "nginx:latest",
				State:            "running",
				Status:           "Up 2 hours",
				Health:           "unhealthy",
				CPUPercent:       15.5,
				MemoryUsageBytes: 256 * 1024 * 1024,
				MemoryLimitBytes: 1024 * 1024 * 1024,
				MemoryPercent:    25.0,
			},
			{
				ID:     "c2-node1",
				Names:  []string{"db-stopped-1"},
				Image:  "postgres:15",
				State:  "exited",
				Status: "Exited (0) 10 minutes ago",
				Health: "none",
			},
		},
	}
	if err := store.SaveNodeWorkload(ctx, node1.ID, workload1); err != nil {
		t.Fatal(err)
	}

	// Node 2: Docker available, 3 containers (3 running, 0 stopped, 0 unhealthy)
	workload2 := protocol.NodeWorkloadReport{
		NodeUUID:          node2.UUID,
		ReportedAt:        now.Unix(),
		DockerAvailable:   true,
		DockerVersion:     "24.0.5",
		ContainersTotal:   3,
		ContainersRunning: 3,
		ContainersStopped: 0,
		Containers: []protocol.ContainerSnapshot{
			{
				ID:               "c3-node2",
				Names:            []string{"cache-2"},
				Image:            "redis:alpine",
				State:            "running",
				Status:           "Up 5 hours",
				Health:           "healthy",
				CPUPercent:       40.0,
				MemoryUsageBytes: 512 * 1024 * 1024,
				MemoryLimitBytes: 1024 * 1024 * 1024,
				MemoryPercent:    50.0,
			},
		},
	}
	if err := store.SaveNodeWorkload(ctx, node2.ID, workload2); err != nil {
		t.Fatal(err)
	}

	// 3. Seed System Events
	// Node 1: critical security event
	if err := store.SaveSystemEvents(ctx, node1.ID, []protocol.SystemEvent{
		{
			Category:   "security",
			Severity:   "critical",
			Title:      "SSH brute force detected",
			Message:    "5 failed login attempts from 10.0.0.1",
			Source:     "sshd",
			OccurredAt: now.Unix(),
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Node 2: warning hardware event
	if err := store.SaveSystemEvents(ctx, node2.ID, []protocol.SystemEvent{
		{
			Category:   "hardware",
			Severity:   "warning",
			Title:      "Fan speed abnormal",
			Message:    "Chassis fan running at low RPM",
			Source:     "ipmi",
			OccurredAt: now.Unix(),
		},
	}); err != nil {
		t.Fatal(err)
	}

	// 4. Seed Alerts
	// Node 1 alert
	if err := store.EvaluateAlert(ctx, node1.ID, db.AlertEvaluation{
		FingerprintDimension: "alert-disk-node1",
		Category:             "resource",
		TargetID:             "rootfs",
		Reason:               "Disk space high",
		Severity:             "critical",
		Failing:              true,
	}, now); err != nil {
		t.Fatal(err)
	}

	// Node 2 alert
	if err := store.EvaluateAlert(ctx, node2.ID, db.AlertEvaluation{
		FingerprintDimension: "alert-net-node2",
		Category:             "network",
		TargetID:             "eth0",
		Reason:               "Packet loss detected",
		Severity:             "warning",
		Failing:              true,
	}, now); err != nil {
		t.Fatal(err)
	}

	// 5. Seed Resources for Node 1 (online), Node 2 stays offline (no report)
	res1 := protocol.ResourceSnapshot{
		CPUPercent:           10.0,
		MemoryUsedBytes:      1024 * 1024 * 1024,
		MemoryTotalBytes:     4 * 1024 * 1024 * 1024,
		FilesystemUsedBytes:  20 * 1024 * 1024 * 1024,
		FilesystemTotalBytes: 100 * 1024 * 1024 * 1024,
	}
	resBytes1, _ := json.Marshal(res1)
	if err := store.UpsertResourceLatest(ctx, node1.ID, now, resBytes1); err != nil {
		t.Fatal(err)
	}

	// 6. Create Users & Sessions
	// Admin user (full access)
	adminUser, err := store.UpsertAdminUser(ctx, "local", "admin-root", "admin", now)
	if err != nil {
		t.Fatal(err)
	}
	adminSession, err := service.CreateSessionForUser(ctx, adminUser.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	adminCookie := task4SessionCookie(adminSession)

	// Operator user with AllowedNodes = node1.UUID
	opUser, err := store.UpsertAdminUser(ctx, "local", "op-scoped", "operator", now)
	if err != nil {
		t.Fatal(err)
	}
	roleOp := db.RoleOperator
	allowedStr := node1.UUID
	_, err = store.UpdateAdminUser(ctx, opUser.ID, db.UpdateUserInput{
		Role:         &roleOp,
		AllowedNodes: &allowedStr,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	opSession, err := service.CreateSessionForUser(ctx, opUser.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	opCookie := task4SessionCookie(opSession)

	// Operator user with empty AllowedNodes = ""
	emptyUser, err := store.UpsertAdminUser(ctx, "local", "op-empty", "operator", now)
	if err != nil {
		t.Fatal(err)
	}
	emptyStr := ""
	_, err = store.UpdateAdminUser(ctx, emptyUser.ID, db.UpdateUserInput{
		Role:         &roleOp,
		AllowedNodes: &emptyStr,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	emptySession, err := service.CreateSessionForUser(ctx, emptyUser.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	emptyCookie := task4SessionCookie(emptySession)

	// ==========================================
	// Test 1: /api/containers/overview
	// ==========================================
	t.Run("ContainersOverview", func(t *testing.T) {
		// Unauthenticated -> 401
		reqUnauth := httptest.NewRequest(http.MethodGet, "/api/containers/overview", nil)
		wUnauth := httptest.NewRecorder()
		handler.ServeHTTP(wUnauth, reqUnauth)
		if wUnauth.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", wUnauth.Code)
		}

		// Admin -> full fleet (2 nodes, 5 containers, 1 unhealthy)
		reqAdmin := httptest.NewRequest(http.MethodGet, "/api/containers/overview", nil)
		reqAdmin.AddCookie(adminCookie)
		wAdmin := httptest.NewRecorder()
		handler.ServeHTTP(wAdmin, reqAdmin)
		if wAdmin.Code != http.StatusOK {
			t.Fatalf("admin: expected 200, got %d: %s", wAdmin.Code, wAdmin.Body.String())
		}
		var adminOverview db.FleetContainerOverview
		if err := json.NewDecoder(wAdmin.Body).Decode(&adminOverview); err != nil {
			t.Fatal(err)
		}
		if adminOverview.TotalNodes != 2 || adminOverview.TotalContainers != 5 || adminOverview.UnhealthyContainers != 1 {
			t.Fatalf("admin: unexpected overview counts: %+v", adminOverview)
		}
		if len(adminOverview.TopCPUContainers) != 2 {
			t.Fatalf("admin: expected 2 top CPU containers, got %d", len(adminOverview.TopCPUContainers))
		}

		// Operator Scoped -> node 1 only (1 node, 2 containers, 1 unhealthy)
		reqOp := httptest.NewRequest(http.MethodGet, "/api/containers/overview", nil)
		reqOp.AddCookie(opCookie)
		wOp := httptest.NewRecorder()
		handler.ServeHTTP(wOp, reqOp)
		if wOp.Code != http.StatusOK {
			t.Fatalf("op: expected 200, got %d: %s", wOp.Code, wOp.Body.String())
		}
		var opOverview db.FleetContainerOverview
		if err := json.NewDecoder(wOp.Body).Decode(&opOverview); err != nil {
			t.Fatal(err)
		}
		if opOverview.TotalNodes != 1 || opOverview.TotalContainers != 2 || opOverview.UnhealthyContainers != 1 {
			t.Fatalf("op: unexpected scoped counts: %+v", opOverview)
		}
		if len(opOverview.TopCPUContainers) != 1 || opOverview.TopCPUContainers[0].NodeID != node1.ID {
			t.Fatalf("op: expected top CPU from node1 only: %+v", opOverview.TopCPUContainers)
		}

		// Operator Empty -> 0 nodes, 0 containers
		reqEmpty := httptest.NewRequest(http.MethodGet, "/api/containers/overview", nil)
		reqEmpty.AddCookie(emptyCookie)
		wEmpty := httptest.NewRecorder()
		handler.ServeHTTP(wEmpty, reqEmpty)
		if wEmpty.Code != http.StatusOK {
			t.Fatalf("empty: expected 200, got %d", wEmpty.Code)
		}
		var emptyOverview db.FleetContainerOverview
		if err := json.NewDecoder(wEmpty.Body).Decode(&emptyOverview); err != nil {
			t.Fatal(err)
		}
		if emptyOverview.TotalNodes != 0 || emptyOverview.TotalContainers != 0 || len(emptyOverview.TopCPUContainers) != 0 {
			t.Fatalf("empty: expected zero counts, got %+v", emptyOverview)
		}
	})

	// ==========================================
	// Test 2: /api/events
	// ==========================================
	t.Run("FleetEvents", func(t *testing.T) {
		// Unauthenticated -> 401
		reqUnauth := httptest.NewRequest(http.MethodGet, "/api/events", nil)
		wUnauth := httptest.NewRecorder()
		handler.ServeHTTP(wUnauth, reqUnauth)
		if wUnauth.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", wUnauth.Code)
		}

		// Admin -> all 2 events
		reqAdmin := httptest.NewRequest(http.MethodGet, "/api/events", nil)
		reqAdmin.AddCookie(adminCookie)
		wAdmin := httptest.NewRecorder()
		handler.ServeHTTP(wAdmin, reqAdmin)
		if wAdmin.Code != http.StatusOK {
			t.Fatalf("admin: expected 200, got %d", wAdmin.Code)
		}
		var adminEvents struct {
			Events []db.SystemEventRecord `json:"events"`
		}
		_ = json.NewDecoder(wAdmin.Body).Decode(&adminEvents)
		if len(adminEvents.Events) != 2 {
			t.Fatalf("admin: expected 2 events, got %d", len(adminEvents.Events))
		}

		// Operator Scoped -> only Node 1 event
		reqOp := httptest.NewRequest(http.MethodGet, "/api/events", nil)
		reqOp.AddCookie(opCookie)
		wOp := httptest.NewRecorder()
		handler.ServeHTTP(wOp, reqOp)
		if wOp.Code != http.StatusOK {
			t.Fatalf("op: expected 200, got %d", wOp.Code)
		}
		var opEvents struct {
			Events []db.SystemEventRecord `json:"events"`
		}
		_ = json.NewDecoder(wOp.Body).Decode(&opEvents)
		if len(opEvents.Events) != 1 || opEvents.Events[0].NodeID != node1.ID {
			t.Fatalf("op: expected 1 event for node1, got %+v", opEvents.Events)
		}

		// Operator Scoped with ?node_id=node1.UUID -> returns Node 1 event
		reqOpNode1 := httptest.NewRequest(http.MethodGet, "/api/events?node_id="+node1.UUID, nil)
		reqOpNode1.AddCookie(opCookie)
		wOpNode1 := httptest.NewRecorder()
		handler.ServeHTTP(wOpNode1, reqOpNode1)
		if wOpNode1.Code != http.StatusOK {
			t.Fatalf("op node1: expected 200, got %d", wOpNode1.Code)
		}
		var opNode1Events struct {
			Events []db.SystemEventRecord `json:"events"`
		}
		_ = json.NewDecoder(wOpNode1.Body).Decode(&opNode1Events)
		if len(opNode1Events.Events) != 1 || opNode1Events.Events[0].NodeID != node1.ID {
			t.Fatalf("op node1: expected 1 event, got %+v", opNode1Events.Events)
		}

		// Operator Scoped with ?node_id=node2.UUID (Forbidden) -> empty list (no 500, no data leak)
		reqOpNode2 := httptest.NewRequest(http.MethodGet, "/api/events?node_id="+node2.UUID, nil)
		reqOpNode2.AddCookie(opCookie)
		wOpNode2 := httptest.NewRecorder()
		handler.ServeHTTP(wOpNode2, reqOpNode2)
		if wOpNode2.Code != http.StatusOK {
			t.Fatalf("op node2 forbidden: expected 200, got %d", wOpNode2.Code)
		}
		var opNode2Events struct {
			Events []db.SystemEventRecord `json:"events"`
		}
		_ = json.NewDecoder(wOpNode2.Body).Decode(&opNode2Events)
		if len(opNode2Events.Events) != 0 {
			t.Fatalf("op node2 forbidden: expected empty list, got %+v", opNode2Events.Events)
		}

		// Operator Empty -> empty list
		reqEmpty := httptest.NewRequest(http.MethodGet, "/api/events", nil)
		reqEmpty.AddCookie(emptyCookie)
		wEmpty := httptest.NewRecorder()
		handler.ServeHTTP(wEmpty, reqEmpty)
		if wEmpty.Code != http.StatusOK {
			t.Fatalf("empty: expected 200, got %d", wEmpty.Code)
		}
		var emptyEvents struct {
			Events []db.SystemEventRecord `json:"events"`
		}
		_ = json.NewDecoder(wEmpty.Body).Decode(&emptyEvents)
		if len(emptyEvents.Events) != 0 {
			t.Fatalf("empty: expected 0 events, got %d", len(emptyEvents.Events))
		}
	})

	// ==========================================
	// Test 3: /api/events/overview
	// ==========================================
	t.Run("EventsOverview", func(t *testing.T) {
		// Unauthenticated -> 401
		reqUnauth := httptest.NewRequest(http.MethodGet, "/api/events/overview", nil)
		wUnauth := httptest.NewRecorder()
		handler.ServeHTTP(wUnauth, reqUnauth)
		if wUnauth.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", wUnauth.Code)
		}

		// Admin -> full counts: 2 total, 1 critical, 1 warning, security:1, hardware:1
		reqAdmin := httptest.NewRequest(http.MethodGet, "/api/events/overview", nil)
		reqAdmin.AddCookie(adminCookie)
		wAdmin := httptest.NewRecorder()
		handler.ServeHTTP(wAdmin, reqAdmin)
		if wAdmin.Code != http.StatusOK {
			t.Fatalf("admin: expected 200, got %d", wAdmin.Code)
		}
		var adminOverview db.SystemEventsOverview
		_ = json.NewDecoder(wAdmin.Body).Decode(&adminOverview)
		if adminOverview.TotalEvents24h != 2 || adminOverview.CriticalEvents24h != 1 || adminOverview.WarningEvents24h != 1 {
			t.Fatalf("admin: unexpected overview counts: %+v", adminOverview)
		}
		if adminOverview.CategoryCounts["security"] != 1 || adminOverview.CategoryCounts["hardware"] != 1 {
			t.Fatalf("admin: unexpected category counts: %+v", adminOverview.CategoryCounts)
		}

		// Operator Scoped -> node 1 only: 1 total, 1 critical, 0 warning, security:1, hardware:0
		reqOp := httptest.NewRequest(http.MethodGet, "/api/events/overview", nil)
		reqOp.AddCookie(opCookie)
		wOp := httptest.NewRecorder()
		handler.ServeHTTP(wOp, reqOp)
		if wOp.Code != http.StatusOK {
			t.Fatalf("op: expected 200, got %d", wOp.Code)
		}
		var opOverview db.SystemEventsOverview
		_ = json.NewDecoder(wOp.Body).Decode(&opOverview)
		if opOverview.TotalEvents24h != 1 || opOverview.CriticalEvents24h != 1 || opOverview.WarningEvents24h != 0 {
			t.Fatalf("op: unexpected overview counts: %+v", opOverview)
		}
		if opOverview.CategoryCounts["security"] != 1 || opOverview.CategoryCounts["hardware"] != 0 {
			t.Fatalf("op: unexpected category counts: %+v", opOverview.CategoryCounts)
		}
		if len(opOverview.RecentEvents) != 1 || opOverview.RecentEvents[0].NodeID != node1.ID {
			t.Fatalf("op: expected 1 recent event for node1: %+v", opOverview.RecentEvents)
		}

		// Operator Empty -> 0 total
		reqEmpty := httptest.NewRequest(http.MethodGet, "/api/events/overview", nil)
		reqEmpty.AddCookie(emptyCookie)
		wEmpty := httptest.NewRecorder()
		handler.ServeHTTP(wEmpty, reqEmpty)
		if wEmpty.Code != http.StatusOK {
			t.Fatalf("empty: expected 200, got %d", wEmpty.Code)
		}
		var emptyOverview db.SystemEventsOverview
		_ = json.NewDecoder(wEmpty.Body).Decode(&emptyOverview)
		if emptyOverview.TotalEvents24h != 0 || len(emptyOverview.RecentEvents) != 0 {
			t.Fatalf("empty: expected zero counts, got %+v", emptyOverview)
		}
	})

	// ==========================================
	// Test 4: /api/alerts
	// ==========================================
	t.Run("Alerts", func(t *testing.T) {
		// Unauthenticated -> 401
		reqUnauth := httptest.NewRequest(http.MethodGet, "/api/alerts", nil)
		wUnauth := httptest.NewRecorder()
		handler.ServeHTTP(wUnauth, reqUnauth)
		if wUnauth.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", wUnauth.Code)
		}

		// Admin -> all 2 alerts
		reqAdmin := httptest.NewRequest(http.MethodGet, "/api/alerts", nil)
		reqAdmin.AddCookie(adminCookie)
		wAdmin := httptest.NewRecorder()
		handler.ServeHTTP(wAdmin, reqAdmin)
		if wAdmin.Code != http.StatusOK {
			t.Fatalf("admin: expected 200, got %d", wAdmin.Code)
		}
		var adminAlerts []alertResponse
		_ = json.NewDecoder(wAdmin.Body).Decode(&adminAlerts)
		if len(adminAlerts) != 2 {
			t.Fatalf("admin: expected 2 alerts, got %d", len(adminAlerts))
		}

		// Operator Scoped -> 1 alert (Node 1)
		reqOp := httptest.NewRequest(http.MethodGet, "/api/alerts", nil)
		reqOp.AddCookie(opCookie)
		wOp := httptest.NewRecorder()
		handler.ServeHTTP(wOp, reqOp)
		if wOp.Code != http.StatusOK {
			t.Fatalf("op: expected 200, got %d", wOp.Code)
		}
		var opAlerts []alertResponse
		_ = json.NewDecoder(wOp.Body).Decode(&opAlerts)
		if len(opAlerts) != 1 || opAlerts[0].NodeID != node1.ID {
			t.Fatalf("op: expected 1 alert for node1, got %+v", opAlerts)
		}

		// Operator Scoped with ?node_id=node2.UUID (Forbidden) -> empty list
		reqOpNode2 := httptest.NewRequest(http.MethodGet, "/api/alerts?node_id="+node2.UUID, nil)
		reqOpNode2.AddCookie(opCookie)
		wOpNode2 := httptest.NewRecorder()
		handler.ServeHTTP(wOpNode2, reqOpNode2)
		if wOpNode2.Code != http.StatusOK {
			t.Fatalf("op forbidden alert: expected 200, got %d", wOpNode2.Code)
		}
		var opNode2Alerts []alertResponse
		_ = json.NewDecoder(wOpNode2.Body).Decode(&opNode2Alerts)
		if len(opNode2Alerts) != 0 {
			t.Fatalf("op forbidden alert: expected empty list, got %+v", opNode2Alerts)
		}

		// Operator Empty -> empty list
		reqEmpty := httptest.NewRequest(http.MethodGet, "/api/alerts", nil)
		reqEmpty.AddCookie(emptyCookie)
		wEmpty := httptest.NewRecorder()
		handler.ServeHTTP(wEmpty, reqEmpty)
		if wEmpty.Code != http.StatusOK {
			t.Fatalf("empty: expected 200, got %d", wEmpty.Code)
		}
		var emptyAlerts []alertResponse
		_ = json.NewDecoder(wEmpty.Body).Decode(&emptyAlerts)
		if len(emptyAlerts) != 0 {
			t.Fatalf("empty: expected 0 alerts, got %d", len(emptyAlerts))
		}
	})

	// ==========================================
	// Test 5: /api/ai/diagnose
	// ==========================================
	t.Run("AIDiagnose", func(t *testing.T) {
		// Unauthenticated -> 401
		reqUnauth := httptest.NewRequest(http.MethodGet, "/api/ai/diagnose", nil)
		wUnauth := httptest.NewRecorder()
		handler.ServeHTTP(wUnauth, reqUnauth)
		if wUnauth.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", wUnauth.Code)
		}

		// Admin -> 2 nodes, 1 offline (Node 2 has offline finding)
		reqAdmin := httptest.NewRequest(http.MethodGet, "/api/ai/diagnose", nil)
		reqAdmin.AddCookie(adminCookie)
		wAdmin := httptest.NewRecorder()
		handler.ServeHTTP(wAdmin, reqAdmin)
		if wAdmin.Code != http.StatusOK {
			t.Fatalf("admin: expected 200, got %d: %s", wAdmin.Code, wAdmin.Body.String())
		}
		var adminDiag ai.DiagnosisReport
		if err := json.NewDecoder(wAdmin.Body).Decode(&adminDiag); err != nil {
			t.Fatal(err)
		}
		if adminDiag.Summary.TotalNodes != 2 || adminDiag.Summary.OfflineNodes != 1 {
			t.Fatalf("admin: unexpected diag summary: %+v", adminDiag.Summary)
		}
		// Confirm node2 is in findings
		foundNode2 := false
		for _, f := range adminDiag.Findings {
			if f.NodeID == node2.ID {
				foundNode2 = true
				break
			}
		}
		if !foundNode2 {
			t.Fatalf("admin: expected findings for node2")
		}

		// Operator Scoped -> 1 node, 0 offline, Node 2 NEVER appears in findings or summary
		reqOp := httptest.NewRequest(http.MethodGet, "/api/ai/diagnose", nil)
		reqOp.AddCookie(opCookie)
		wOp := httptest.NewRecorder()
		handler.ServeHTTP(wOp, reqOp)
		if wOp.Code != http.StatusOK {
			t.Fatalf("op: expected 200, got %d: %s", wOp.Code, wOp.Body.String())
		}
		var opDiag ai.DiagnosisReport
		if err := json.NewDecoder(wOp.Body).Decode(&opDiag); err != nil {
			t.Fatal(err)
		}
		if opDiag.Summary.TotalNodes != 1 || opDiag.Summary.OfflineNodes != 0 {
			t.Fatalf("op: expected 1 node and 0 offline, got %+v", opDiag.Summary)
		}
		for _, f := range opDiag.Findings {
			if f.NodeID == node2.ID {
				t.Fatalf("op: leak! node2 appeared in findings: %+v", f)
			}
		}
		if opDiag.HealthStatus == "" || opDiag.HealthScore <= 0 {
			t.Fatalf("op: invalid health score or status: %d / %s", opDiag.HealthScore, opDiag.HealthStatus)
		}
		if opDiag.MarkdownReport == "" {
			t.Fatalf("op: empty markdown report")
		}

		// Operator Empty -> 0 nodes, HealthScore = 100, HEALTHY, valid report
		reqEmpty := httptest.NewRequest(http.MethodGet, "/api/ai/diagnose", nil)
		reqEmpty.AddCookie(emptyCookie)
		wEmpty := httptest.NewRecorder()
		handler.ServeHTTP(wEmpty, reqEmpty)
		if wEmpty.Code != http.StatusOK {
			t.Fatalf("empty: expected 200, got %d: %s", wEmpty.Code, wEmpty.Body.String())
		}
		var emptyDiag ai.DiagnosisReport
		if err := json.NewDecoder(wEmpty.Body).Decode(&emptyDiag); err != nil {
			t.Fatal(err)
		}
		if emptyDiag.Summary.TotalNodes != 0 || emptyDiag.HealthScore != 100 || emptyDiag.HealthStatus != "HEALTHY" {
			t.Fatalf("empty: expected 0 nodes and score 100, got %+v", emptyDiag)
		}
		if len(emptyDiag.Findings) != 0 {
			t.Fatalf("empty: expected 0 findings, got %d", len(emptyDiag.Findings))
		}
	})

	// ==========================================
	// Test 6: Concurrency & Race Safety
	// ==========================================
	t.Run("ConcurrencyRace", func(t *testing.T) {
		var wg sync.WaitGroup
		endpoints := []string{
			"/api/containers/overview",
			"/api/events",
			"/api/events/overview",
			"/api/alerts",
			"/api/ai/diagnose",
		}
		cookies := []*http.Cookie{adminCookie, opCookie, emptyCookie}

		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(iter int) {
				defer wg.Done()
				endpoint := endpoints[iter%len(endpoints)]
				cookie := cookies[iter%len(cookies)]

				req := httptest.NewRequest(http.MethodGet, endpoint, nil)
				req.AddCookie(cookie)
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)
				if w.Code != http.StatusOK {
					t.Errorf("concurrent %s: expected 200, got %d", endpoint, w.Code)
				}
			}(i)
		}
		wg.Wait()
	})
}
