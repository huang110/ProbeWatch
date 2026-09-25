package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/protocol"
)

func TestNodeBillingAPIEndpoints(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	handler := NewServer(cfg, service).Handler()
	session, csrf := task4AdminSession(t, service, store)

	// Register a test node
	token, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	registered := task4Register(t, handler, protocol.RegisterRequest{
		NodeUUID:          task4NodeUUID1,
		Name:              "Billing Node",
		RegistrationToken: token.Token,
	})
	nodeUUID := registered.NodeUUID

	// 1. Unauthenticated GET /api/nodes/{uuid}/billing -> 401
	unauth := httptest.NewRecorder()
	handler.ServeHTTP(unauth, httptest.NewRequest(http.MethodGet, "/api/nodes/"+nodeUUID+"/billing", nil))
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauth code = %d, want 401", unauth.Code)
	}

	// 2. Authenticated GET /api/nodes/{uuid}/billing -> 200 with defaults
	getReq := httptest.NewRequest(http.MethodGet, "/api/nodes/"+nodeUUID+"/billing", nil)
	getReq.AddCookie(task4SessionCookie(session))
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get billing code = %d, want 200", getRec.Code)
	}
	var info db.CycleTrafficInfo
	if err := json.Unmarshal(getRec.Body.Bytes(), &info); err != nil {
		t.Fatalf("unmarshal billing info: %v", err)
	}
	if info.ResetDay != 1 || info.AccountingMethod != "total" {
		t.Fatalf("unexpected defaults: %+v", info)
	}

	// 3. PUT /api/nodes/{uuid}/billing without CSRF -> 403
	putNoCSRF := task4AdminWrite(t, handler, http.MethodPut, session, "", "/api/nodes/"+nodeUUID+"/billing", `{"reset_day":15,"accounting_method":"max","traffic_quota_bytes":2147483648000}`)
	if putNoCSRF.Code != http.StatusForbidden {
		t.Fatalf("put without CSRF code = %d, want 403", putNoCSRF.Code)
	}

	// 4. PUT /api/nodes/{uuid}/billing with CSRF -> 200
	putRes, csrf := task4AdminWriteWithCSRF(t, handler, http.MethodPut, session, csrf, "/api/nodes/"+nodeUUID+"/billing", `{"reset_day":15,"accounting_method":"max","traffic_quota_bytes":2147483648000,"merchant":"DMIT"}`)
	if putRes.Code != http.StatusOK {
		t.Fatalf("put with CSRF code = %d, body = %s", putRes.Code, putRes.Body.String())
	}
	var updated db.CycleTrafficInfo
	if err := json.Unmarshal(putRes.Body.Bytes(), &updated); err != nil {
		t.Fatalf("unmarshal updated: %v", err)
	}
	if updated.ResetDay != 15 || updated.AccountingMethod != "max" || updated.Merchant != "DMIT" {
		t.Fatalf("unexpected updated info: %+v", updated)
	}

	// 5. POST /api/nodes/{uuid}/billing/reset with CSRF -> 200
	csrf = task4CSRF(t, handler, session)
	resetRes, _ := task4AdminWriteWithCSRF(t, handler, http.MethodPost, session, csrf, "/api/nodes/"+nodeUUID+"/billing/reset", "{}")
	if resetRes.Code != http.StatusOK {
		t.Fatalf("reset with CSRF code = %d, body = %s", resetRes.Code, resetRes.Body.String())
	}

	// 6. GET /api/public/nodes/{uuid}/billing (guest endpoint, no auth needed) -> 200
	pubReq := httptest.NewRequest(http.MethodGet, "/api/public/nodes/"+nodeUUID+"/billing", nil)
	pubRec := httptest.NewRecorder()
	handler.ServeHTTP(pubRec, pubReq)
	if pubRec.Code != http.StatusOK {
		t.Fatalf("public billing code = %d, want 200, body = %s", pubRec.Code, pubRec.Body.String())
	}
	var pubInfo map[string]any
	if err := json.Unmarshal(pubRec.Body.Bytes(), &pubInfo); err != nil {
		t.Fatalf("unmarshal public billing: %v", err)
	}
	if pubInfo["reset_day"] != float64(15) || pubInfo["accounting_method"] != "max" {
		t.Fatalf("unexpected public info: %+v", pubInfo)
	}
}
