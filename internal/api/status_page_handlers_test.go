package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/probewatch/probewatch/internal/db"
)

func TestPublicStatusPageAndIncidents(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	server := NewServer(cfg, service)
	handler := server.Handler()
	session, csrf := task4AdminSession(t, service, store)

	// 1. GET /api/public/status-page (unauthenticated public request)
	pubReq := httptest.NewRequest(http.MethodGet, "/api/public/status-page", nil)
	pubRec := httptest.NewRecorder()
	handler.ServeHTTP(pubRec, pubReq)

	if pubRec.Code != http.StatusOK {
		t.Fatalf("GET /api/public/status-page status = %d: %s", pubRec.Code, pubRec.Body.String())
	}
	var pubResp db.PublicStatusPageResponse
	if err := json.Unmarshal(pubRec.Body.Bytes(), &pubResp); err != nil {
		t.Fatalf("unmarshal public status response: %v", err)
	}
	if pubResp.OverallStatus != "operational" {
		t.Errorf("expected operational status, got %s", pubResp.OverallStatus)
	}
	if pubResp.Config.Title == "" {
		t.Error("expected non-empty status page title")
	}

	// 2. GET /api/public/incidents
	incReq := httptest.NewRequest(http.MethodGet, "/api/public/incidents", nil)
	incRec := httptest.NewRecorder()
	handler.ServeHTTP(incRec, incReq)
	if incRec.Code != http.StatusOK {
		t.Fatalf("GET /api/public/incidents status = %d", incRec.Code)
	}

	// 3. Admin: GET /api/admin/status-page
	adminCfgReq := httptest.NewRequest(http.MethodGet, "/api/admin/status-page", nil)
	adminCfgReq.AddCookie(task4SessionCookie(session))
	adminCfgRec := httptest.NewRecorder()
	handler.ServeHTTP(adminCfgRec, adminCfgReq)
	if adminCfgRec.Code != http.StatusOK {
		t.Fatalf("GET /api/admin/status-page status = %d", adminCfgRec.Code)
	}

	// 4. Admin: PUT /api/admin/status-page
	updatePayload := `{
		"title": "ProbeWatch Cloud Status",
		"description": "Enterprise Node and Network Status",
		"announcement": "System update scheduled",
		"show_uptime_days": 90,
		"components": [
			{"id": "comp_1", "name": "Core Gateway", "group": "Gateway", "show_latency": true, "order": 1}
		]
	}`
	csrf = task4CSRF(t, handler, session)
	putRec, _ := task4AdminWriteWithCSRF(t, handler, http.MethodPut, session, csrf, "/api/admin/status-page", updatePayload)
	if putRec.Code != http.StatusOK {
		t.Fatalf("PUT /api/admin/status-page status = %d: %s", putRec.Code, putRec.Body.String())
	}

	// 5. Admin: POST /api/admin/incidents (Create an incident)
	createIncPayload := `{
		"title": "Database Read Replica Latency",
		"status": "investigating",
		"impact": "minor",
		"is_maintenance": false,
		"message": "Investigating slow read queries in US-West."
	}`
	csrf = task4CSRF(t, handler, session)
	createIncRec, _ := task4AdminWriteWithCSRF(t, handler, http.MethodPost, session, csrf, "/api/admin/incidents", createIncPayload)
	if createIncRec.Code != http.StatusCreated {
		t.Fatalf("POST /api/admin/incidents status = %d: %s", createIncRec.Code, createIncRec.Body.String())
	}
	var createdInc db.Incident
	if err := json.Unmarshal(createIncRec.Body.Bytes(), &createdInc); err != nil {
		t.Fatalf("unmarshal created incident: %v", err)
	}
	if createdInc.ID == "" || createdInc.Title != "Database Read Replica Latency" {
		t.Errorf("unexpected created incident: %+v", createdInc)
	}

	// 6. Admin: POST /api/admin/incidents/{id}/updates (Add an update)
	addUpdPayload := `{
		"status": "monitoring",
		"message": "Replica resynced, monitoring latency metrics."
	}`
	csrf = task4CSRF(t, handler, session)
	updRec, _ := task4AdminWriteWithCSRF(t, handler, http.MethodPost, session, csrf, "/api/admin/incidents/"+createdInc.ID+"/updates", addUpdPayload)
	if updRec.Code != http.StatusOK {
		t.Fatalf("POST /api/admin/incidents/{id}/updates status = %d: %s", updRec.Code, updRec.Body.String())
	}

	// 7. Verify Public Status Page reflects incident status
	pubReq2 := httptest.NewRequest(http.MethodGet, "/api/public/status-page", nil)
	pubRec2 := httptest.NewRecorder()
	handler.ServeHTTP(pubRec2, pubReq2)
	var pubResp2 db.PublicStatusPageResponse
	_ = json.Unmarshal(pubRec2.Body.Bytes(), &pubResp2)
	if len(pubResp2.ActiveIncidents) == 0 {
		t.Error("expected active incident in public status response")
	}

	// 8. Admin: Resolve Incident
	resolveUpdPayload := `{
		"status": "resolved",
		"message": "Incident fully resolved."
	}`
	csrf = task4CSRF(t, handler, session)
	resRec, _ := task4AdminWriteWithCSRF(t, handler, http.MethodPost, session, csrf, "/api/admin/incidents/"+createdInc.ID+"/updates", resolveUpdPayload)
	if resRec.Code != http.StatusOK {
		t.Fatalf("Resolve update failed: %d", resRec.Code)
	}

	// 9. Admin: Delete Incident
	csrf = task4CSRF(t, handler, session)
	delRec, _ := task4AdminWriteWithCSRF(t, handler, http.MethodDelete, session, csrf, "/api/admin/incidents/"+createdInc.ID, "{}")
	if delRec.Code != http.StatusOK {
		t.Fatalf("DELETE /api/admin/incidents/{id} status = %d: %s", delRec.Code, delRec.Body.String())
	}

	// 10. Verify audit log was recorded for status page and incidents
	auditReq := httptest.NewRequest(http.MethodGet, "/api/audit-logs", nil)
	auditReq.AddCookie(task4SessionCookie(session))
	auditRec := httptest.NewRecorder()
	handler.ServeHTTP(auditRec, auditReq)
	if auditRec.Code != http.StatusOK {
		t.Fatalf("GET /api/audit-logs status = %d", auditRec.Code)
	}
	if !bytes.Contains(auditRec.Body.Bytes(), []byte("incident.create")) {
		t.Error("expected incident.create in audit logs")
	}
}
