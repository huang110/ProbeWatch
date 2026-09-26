package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/probewatch/probewatch/internal/db"
)

func TestAlertRulesAPI(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	handler := NewServer(cfg, service).Handler()
	session, csrf := task4AdminSession(t, service, store)

	// 1. Unauthenticated GET /api/alerts/rules -> 401
	unauth := httptest.NewRecorder()
	handler.ServeHTTP(unauth, httptest.NewRequest(http.MethodGet, "/api/alerts/rules", nil))
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauth code = %d, want 401", unauth.Code)
	}

	// 2. Authenticated GET /api/alerts/rules -> 200 (seeded rules)
	getReq := httptest.NewRequest(http.MethodGet, "/api/alerts/rules", nil)
	getReq.AddCookie(task4SessionCookie(session))
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("list rules code = %d, want 200", getRec.Code)
	}
	var rules []db.AlertRule
	if err := json.Unmarshal(getRec.Body.Bytes(), &rules); err != nil {
		t.Fatalf("unmarshal rules: %v", err)
	}
	if len(rules) < 3 {
		t.Fatalf("expected at least 3 seeded rules, got %d", len(rules))
	}

	// 3. POST /api/alerts/rules without CSRF -> 403
	newRulePayload := `{"id":"rule-test-load","name":"Load1 Warning","metric":"load1","operator":">","threshold":4.0,"severity":"warning","node_filter":"*","enabled":true}`
	postNoCSRF := task4AdminWrite(t, handler, http.MethodPost, session, "", "/api/alerts/rules", newRulePayload)
	if postNoCSRF.Code != http.StatusForbidden {
		t.Fatalf("post without CSRF code = %d, want 403", postNoCSRF.Code)
	}

	// 4. POST /api/alerts/rules with CSRF -> 201
	postRes, csrf := task4AdminWriteWithCSRF(t, handler, http.MethodPost, session, csrf, "/api/alerts/rules", newRulePayload)
	if postRes.Code != http.StatusCreated {
		t.Fatalf("post rule code = %d, body = %s", postRes.Code, postRes.Body.String())
	}
	var created db.AlertRule
	if err := json.Unmarshal(postRes.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal created rule: %v", err)
	}
	if created.ID != "rule-test-load" || created.Threshold != 4.0 || !created.Enabled {
		t.Fatalf("unexpected created rule: %+v", created)
	}

	// 5. GET /api/alerts/rules/rule-test-load -> 200
	getSingleReq := httptest.NewRequest(http.MethodGet, "/api/alerts/rules/rule-test-load", nil)
	getSingleReq.AddCookie(task4SessionCookie(session))
	getSingleRec := httptest.NewRecorder()
	handler.ServeHTTP(getSingleRec, getSingleReq)
	if getSingleRec.Code != http.StatusOK {
		t.Fatalf("get rule code = %d, want 200", getSingleRec.Code)
	}

	// 6. PUT /api/alerts/rules/rule-test-load with CSRF -> 200
	updatePayload := `{"name":"Load1 High Warning","metric":"load1","operator":">=","threshold":5.5,"severity":"critical","node_filter":"*","enabled":true}`
	putRes, csrf := task4AdminWriteWithCSRF(t, handler, http.MethodPut, session, csrf, "/api/alerts/rules/rule-test-load", updatePayload)
	if putRes.Code != http.StatusOK {
		t.Fatalf("put rule code = %d, body = %s", putRes.Code, putRes.Body.String())
	}
	var updated db.AlertRule
	if err := json.Unmarshal(putRes.Body.Bytes(), &updated); err != nil {
		t.Fatalf("unmarshal updated rule: %v", err)
	}
	if updated.Threshold != 5.5 || updated.Severity != "critical" {
		t.Fatalf("unexpected updated rule: %+v", updated)
	}

	// 7. POST /api/alerts/rules/rule-test-load/toggle with CSRF -> 200
	toggleRes, csrf := task4AdminWriteWithCSRF(t, handler, http.MethodPost, session, csrf, "/api/alerts/rules/rule-test-load/toggle", "{}")
	if toggleRes.Code != http.StatusOK {
		t.Fatalf("toggle rule code = %d, body = %s", toggleRes.Code, toggleRes.Body.String())
	}
	var toggleOut map[string]any
	_ = json.Unmarshal(toggleRes.Body.Bytes(), &toggleOut)
	if toggleOut["enabled"] != false {
		t.Fatalf("expected enabled false, got %v", toggleOut["enabled"])
	}

	// 8. DELETE /api/alerts/rules/rule-test-load with CSRF -> 200
	delRes, csrf := task4AdminWriteWithCSRF(t, handler, http.MethodDelete, session, csrf, "/api/alerts/rules/rule-test-load", "")
	if delRes.Code != http.StatusOK {
		t.Fatalf("delete rule code = %d, body = %s", delRes.Code, delRes.Body.String())
	}

	// 9. GET /api/alerts/rules/rule-test-load -> 404
	get404Req := httptest.NewRequest(http.MethodGet, "/api/alerts/rules/rule-test-load", nil)
	get404Req.AddCookie(task4SessionCookie(session))
	get404Rec := httptest.NewRecorder()
	handler.ServeHTTP(get404Rec, get404Req)
	if get404Rec.Code != http.StatusNotFound {
		t.Fatalf("get deleted rule code = %d, want 404", get404Rec.Code)
	}

	// 10. POST composite rule with CSRF -> 201
	compPayload := `{"id":"rule-test-comp","name":"Composite Memory & CPU","expression_type":"composite","logic":"AND","consecutive_count":3,"conditions":[{"metric":"cpu","operator":">","threshold":90.0},{"metric":"memory","operator":">","threshold":85.0}],"severity":"critical","enabled":true}`
	compRes, _ := task4AdminWriteWithCSRF(t, handler, http.MethodPost, session, csrf, "/api/alerts/rules", compPayload)
	if compRes.Code != http.StatusCreated {
		t.Fatalf("create composite rule code = %d, body = %s", compRes.Code, compRes.Body.String())
	}
	var compCreated db.AlertRule
	_ = json.Unmarshal(compRes.Body.Bytes(), &compCreated)
	if compCreated.ExpressionType != "composite" || compCreated.Logic != "AND" || compCreated.ConsecutiveCount != 3 || len(compCreated.Conditions) != 2 {
		t.Fatalf("unexpected composite rule: %+v", compCreated)
	}
}

