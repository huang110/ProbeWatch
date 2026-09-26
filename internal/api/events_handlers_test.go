package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/protocol"
)

func TestSystemEventsAndLogQueryHandlers(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	server := NewServer(cfg, service)
	handler := server.Handler()
	session, csrf := task4AdminSession(t, service, store)

	// 1. Register Edge Node
	reg, _ := task4CreateRegistration(t, handler, session, csrf)
	regResp := task4Register(t, handler, protocol.RegisterRequest{
		RegistrationToken: reg.Token,
		NodeUUID:          task4NodeUUID1,
		Name:              "Edge-Audit-01",
	})

	now := time.Now().UTC().Unix()

	// 2. Report System Events from Agent
	eventsReport := protocol.SystemEventBatchReport{
		NodeUUID:   task4NodeUUID1,
		ReportedAt: now,
		Events: []protocol.SystemEvent{
			{
				ID:         "evt-oom-1",
				Category:   "oom",
				Severity:   "critical",
				Title:      "Out of memory: Killed process 1234 (mysqld)",
				Message:    "total-vm:1048576kB, anon-rss:524288kB, file-rss:0kB",
				Source:     "kernel",
				OccurredAt: now - 30,
			},
			{
				ID:         "evt-ssh-1",
				Category:   "ssh_auth",
				Severity:   "warning",
				Title:      "SSH authentication failed for user 'root' from 10.0.0.99:54321",
				Message:    "Failed password for root from 10.0.0.99 port 54321 ssh2",
				Source:     "sshd",
				OccurredAt: now - 10,
			},
		},
	}

	payload, err := json.Marshal(eventsReport)
	if err != nil {
		t.Fatal(err)
	}

	// 2.1 Unauthorized report request should fail
	req := httptest.NewRequest(http.MethodPost, "/api/agent/v1/events", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rec.Code)
	}

	// 2.2 Authorized report request should succeed
	req = httptest.NewRequest(http.MethodPost, "/api/agent/v1/events", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+regResp.NodeToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d: %s", rec.Code, rec.Body.String())
	}

	// 3. Query Node Events via Admin API
	req = httptest.NewRequest(http.MethodGet, "/api/nodes/"+task4NodeUUID1+"/events", nil)
	req.AddCookie(task4SessionCookie(session))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get node events failed: %d, body: %s", rec.Code, rec.Body.String())
	}

	var nodeEventsResp struct {
		NodeID   string                 `json:"node_id"`
		NodeUUID string                 `json:"node_uuid"`
		NodeName string                 `json:"node_name"`
		Events   []db.SystemEventRecord `json:"events"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&nodeEventsResp); err != nil {
		t.Fatal(err)
	}
	if len(nodeEventsResp.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(nodeEventsResp.Events))
	}

	// 4. Query Events Overview via Fleet Overview API
	req = httptest.NewRequest(http.MethodGet, "/api/events/overview", nil)
	req.AddCookie(task4SessionCookie(session))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get events overview failed: %d, body: %s", rec.Code, rec.Body.String())
	}

	var overviewResp struct {
		TotalEvents24h    int64            `json:"total_events_24h"`
		CriticalEvents24h int64            `json:"critical_events_24h"`
		WarningEvents24h  int64            `json:"warning_events_24h"`
		CategoryCounts    map[string]int64 `json:"category_counts"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&overviewResp); err != nil {
		t.Fatal(err)
	}
	if overviewResp.TotalEvents24h != 2 {
		t.Fatalf("expected 2 total events, got %d", overviewResp.TotalEvents24h)
	}
	if overviewResp.CriticalEvents24h != 1 {
		t.Fatalf("expected 1 critical event, got %d", overviewResp.CriticalEvents24h)
	}
	if overviewResp.WarningEvents24h != 1 {
		t.Fatalf("expected 1 warning event, got %d", overviewResp.WarningEvents24h)
	}

	// 5. Query Public Events Overview
	req = httptest.NewRequest(http.MethodGet, "/api/public/events/overview", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get public events overview failed: %d", rec.Code)
	}

	// 6. Query Public Node Events
	req = httptest.NewRequest(http.MethodGet, "/api/public/nodes/"+task4NodeUUID1+"/events", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get public node events failed: %d", rec.Code)
	}

	// 7. Query System Logs for Node
	req = httptest.NewRequest(http.MethodGet, "/api/nodes/"+task4NodeUUID1+"/logs/query?lines=20", nil)
	req.AddCookie(task4SessionCookie(session))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("query node logs failed: %d, body: %s", rec.Code, rec.Body.String())
	}
}
