package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/db"
)

func TestAlertSilencesAndFlappingAPI(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	handler := NewServer(cfg, service).Handler()
	session, csrf := task4AdminSession(t, service, store)

	// 1. Unauthenticated GET /api/alerts/silences -> 401
	unauth := httptest.NewRecorder()
	handler.ServeHTTP(unauth, httptest.NewRequest(http.MethodGet, "/api/alerts/silences", nil))
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauth code = %d, want 401", unauth.Code)
	}

	// 2. Authenticated GET /api/alerts/silences -> 200 (empty list initially)
	getReq := httptest.NewRequest(http.MethodGet, "/api/alerts/silences", nil)
	getReq.AddCookie(task4SessionCookie(session))
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("list silences code = %d, want 200", getRec.Code)
	}
	var silences []db.AlertSilence
	if err := json.Unmarshal(getRec.Body.Bytes(), &silences); err != nil {
		t.Fatalf("unmarshal silences: %v", err)
	}
	if len(silences) != 0 {
		t.Fatalf("expected 0 initial silences, got %d", len(silences))
	}

	// 3. POST /api/alerts/silences with CSRF -> 201 Created
	now := time.Now().UTC()
	silencePayload := `{"id":"silence-maint-1","name":"Database Maintenance","node_filter":"*","category":"resource","starts_at":` +
		jsonNum(now.Unix()) + `,"ends_at":` + jsonNum(now.Add(2*time.Hour).Unix()) + `,"reason":"Scheduled backup"}`
	postRes, csrf := task4AdminWriteWithCSRF(t, handler, http.MethodPost, session, csrf, "/api/alerts/silences", silencePayload)
	if postRes.Code != http.StatusCreated {
		t.Fatalf("create silence code = %d, body = %s", postRes.Code, postRes.Body.String())
	}
	var created db.AlertSilence
	if err := json.Unmarshal(postRes.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal created silence: %v", err)
	}
	if created.ID != "silence-maint-1" || created.Reason != "Scheduled backup" {
		t.Fatalf("unexpected created silence: %+v", created)
	}

	// 4. GET /api/alerts/silences -> 200, count = 1
	getReq2 := httptest.NewRequest(http.MethodGet, "/api/alerts/silences", nil)
	getReq2.AddCookie(task4SessionCookie(session))
	getRec2 := httptest.NewRecorder()
	handler.ServeHTTP(getRec2, getReq2)
	if getRec2.Code != http.StatusOK {
		t.Fatalf("list silences code = %d", getRec2.Code)
	}
	var silences2 []db.AlertSilence
	_ = json.Unmarshal(getRec2.Body.Bytes(), &silences2)
	if len(silences2) != 1 {
		t.Fatalf("expected 1 silence, got %d", len(silences2))
	}

	// 5. GET /api/alerts/flapping -> 200, array
	flapReq := httptest.NewRequest(http.MethodGet, "/api/alerts/flapping", nil)
	flapReq.AddCookie(task4SessionCookie(session))
	flapRec := httptest.NewRecorder()
	handler.ServeHTTP(flapRec, flapReq)
	if flapRec.Code != http.StatusOK {
		t.Fatalf("list flapping code = %d", flapRec.Code)
	}

	// 6. DELETE /api/alerts/silences/silence-maint-1 with CSRF -> 200
	delRes, _ := task4AdminWriteWithCSRF(t, handler, http.MethodDelete, session, csrf, "/api/alerts/silences/silence-maint-1", "")
	if delRes.Code != http.StatusOK {
		t.Fatalf("delete silence code = %d, body = %s", delRes.Code, delRes.Body.String())
	}

	// 7. Verify deleted
	getReq3 := httptest.NewRequest(http.MethodGet, "/api/alerts/silences", nil)
	getReq3.AddCookie(task4SessionCookie(session))
	getRec3 := httptest.NewRecorder()
	handler.ServeHTTP(getRec3, getReq3)
	var silences3 []db.AlertSilence
	_ = json.Unmarshal(getRec3.Body.Bytes(), &silences3)
	if len(silences3) != 0 {
		t.Fatalf("expected 0 silences after delete, got %d", len(silences3))
	}
}

func jsonNum(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
