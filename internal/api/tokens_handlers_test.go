package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/db"
)

func TestOpenAPIAndDocsEndpoints(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	server := NewServer(cfg, service)
	handler := server.Handler()

	// 1. GET /api/openapi.json
	req := httptest.NewRequest(http.MethodGet, "/api/openapi.json", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/openapi.json status = %d", rec.Code)
	}
	var spec map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
		t.Fatalf("unmarshal openapi: %v", err)
	}
	if spec["openapi"] != "3.0.3" {
		t.Fatalf("expected openapi 3.0.3, got %v", spec["openapi"])
	}

	// 2. GET /docs
	docsReq := httptest.NewRequest(http.MethodGet, "/docs", nil)
	docsRec := httptest.NewRecorder()
	handler.ServeHTTP(docsRec, docsReq)
	if docsRec.Code != http.StatusOK {
		t.Fatalf("GET /docs status = %d", docsRec.Code)
	}
	if !strings.Contains(docsRec.Body.String(), "swagger-ui") {
		t.Fatal("expected /docs to contain swagger-ui")
	}
}

func TestAPITokenCRUDAndAuthentication(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	server := NewServer(cfg, service)
	handler := server.Handler()
	session, csrf := task4AdminSession(t, service, store)

	// 1. Create a Personal Access Token as admin
	createPayload := `{
		"name": "Integration Test Token",
		"role": "operator",
		"scopes": "read:nodes,write:alerts",
		"allowed_nodes": "*",
		"expires_in_days": 30
	}`
	createRec, _ := task4AdminWriteWithCSRF(t, handler, http.MethodPost, session, csrf, "/api/tokens", createPayload)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create token status = %d, body = %s", createRec.Code, createRec.Body.String())
	}

	var createResp map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("unmarshal create token response: %v", err)
	}

	rawToken, ok := createResp["raw_token"].(string)
	if !ok || !strings.HasPrefix(rawToken, "pbw_pat_") {
		t.Fatalf("expected raw_token starting with pbw_pat_, got %v", createResp["raw_token"])
	}

	tokenObj := createResp["token"].(map[string]any)
	tokenID := tokenObj["id"].(string)

	// 2. Authenticate to /api/me using the raw PAT via Bearer token
	meReq := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+rawToken)
	meRec := httptest.NewRecorder()
	handler.ServeHTTP(meRec, meReq)
	if meRec.Code != http.StatusOK {
		t.Fatalf("PAT /api/me status = %d, body = %s", meRec.Code, meRec.Body.String())
	}

	var meResp map[string]any
	if err := json.Unmarshal(meRec.Body.Bytes(), &meResp); err != nil {
		t.Fatalf("unmarshal me response: %v", err)
	}
	if meResp["role"] != "operator" {
		t.Fatalf("expected role operator, got %v", meResp["role"])
	}
	if meResp["provider"] != "token" {
		t.Fatalf("expected provider token, got %v", meResp["provider"])
	}

	// 3. Authenticate with X-API-Key header
	apiKeyReq := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	apiKeyReq.Header.Set("X-API-Key", rawToken)
	apiKeyRec := httptest.NewRecorder()
	handler.ServeHTTP(apiKeyRec, apiKeyReq)
	if apiKeyRec.Code != http.StatusOK {
		t.Fatalf("X-API-Key /api/me status = %d", apiKeyRec.Code)
	}

	// 4. List tokens
	listReq := httptest.NewRequest(http.MethodGet, "/api/tokens", nil)
	listReq.AddCookie(task4SessionCookie(session))
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list tokens status = %d", listRec.Code)
	}
	var tokenList []map[string]any
	_ = json.Unmarshal(listRec.Body.Bytes(), &tokenList)
	if len(tokenList) != 1 {
		t.Fatalf("expected 1 token, got %d", len(tokenList))
	}

	// 5. Disable the token
	csrf = task4CSRF(t, handler, session)
	disableRec, _ := task4AdminWriteWithCSRF(t, handler, http.MethodPut, session, csrf, "/api/tokens/"+tokenID, `{"disabled":true}`)
	if disableRec.Code != http.StatusOK {
		t.Fatalf("disable token status = %d", disableRec.Code)
	}

	// Disabled token request should now return 403 Forbidden
	meReqDisabled := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	meReqDisabled.Header.Set("Authorization", "Bearer "+rawToken)
	meRecDisabled := httptest.NewRecorder()
	handler.ServeHTTP(meRecDisabled, meReqDisabled)
	if meRecDisabled.Code != http.StatusForbidden {
		t.Fatalf("disabled token status = %d, want 403", meRecDisabled.Code)
	}

	// 6. Delete the token
	csrf = task4CSRF(t, handler, session)
	delRec, _ := task4AdminWriteWithCSRF(t, handler, http.MethodDelete, session, csrf, "/api/tokens/"+tokenID, `{}`)
	if delRec.Code != http.StatusOK {
		t.Fatalf("delete token status = %d", delRec.Code)
	}

	// Deleted token request should now return 401 Unauthorized
	meReqDeleted := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	meReqDeleted.Header.Set("Authorization", "Bearer "+rawToken)
	meRecDeleted := httptest.NewRecorder()
	handler.ServeHTTP(meRecDeleted, meReqDeleted)
	if meRecDeleted.Code != http.StatusUnauthorized {
		t.Fatalf("deleted token status = %d, want 401", meRecDeleted.Code)
	}

	// 7. Verify Audit Logs captured the events
	auditReq := httptest.NewRequest(http.MethodGet, "/api/audit-logs", nil)
	auditReq.AddCookie(task4SessionCookie(session))
	auditRec := httptest.NewRecorder()
	handler.ServeHTTP(auditRec, auditReq)
	if auditRec.Code != http.StatusOK {
		t.Fatalf("get audit logs status = %d, body = %s", auditRec.Code, auditRec.Body.String())
	}
	var auditResp map[string]any
	_ = json.Unmarshal(auditRec.Body.Bytes(), &auditResp)
	logs := auditResp["logs"].([]any)
	if len(logs) < 3 {
		t.Fatalf("expected at least 3 audit logs (create, update, delete), got %d", len(logs))
	}
}

func TestAPITokenFollowsOwnerPrivilegesAndScopes(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	handler := NewServer(cfg, service).Handler()
	session, csrf := task4AdminSession(t, service, store)

	create := func(role, scopes, allowed string) string {
		t.Helper()
		body := fmt.Sprintf(`{"name":"audit-%s","role":%q,"scopes":%q,"allowed_nodes":%q}`, role, role, scopes, allowed)
		var nextCSRF string
		resp, nextCSRF := task4AdminWriteWithCSRF(t, handler, http.MethodPost, session, csrf, "/api/tokens", body)
		if nextCSRF != "" {
			csrf = nextCSRF
		}
		if resp.Code != http.StatusCreated {
			t.Fatalf("create PAT status=%d body=%s", resp.Code, resp.Body.String())
		}
		var payload struct {
			RawToken string `json:"raw_token"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil || payload.RawToken == "" {
			t.Fatalf("create PAT response=%s err=%v", resp.Body.String(), err)
		}
		return payload.RawToken
	}

	// A token initially created as administrator must not retain administrator
	// privileges after its owner is demoted and restricted to a node subset.
	pat := create(db.RoleAdmin, "*", "*")
	readNodesPAT := create(db.RoleAdmin, "read:nodes", "*")
	users, err := store.ListAdminUsers(context.Background())
	if err != nil || len(users) == 0 {
		t.Fatalf("list users: %v", err)
	}
	owner := users[0]
	// Keep one active administrator so the last-admin guard does not prevent the
	// privilege transition under test.
	if _, err := store.UpsertAdminUser(context.Background(), "github", "second-admin", "second-admin", time.Now().UTC()); err != nil {
		t.Fatalf("create second admin: %v", err)
	}
	newRole := db.RoleViewer
	allowed := "node-a,node-b"
	if _, err := store.UpdateAdminUser(context.Background(), owner.ID, db.UpdateUserInput{Role: &newRole, AllowedNodes: &allowed}, time.Now().UTC()); err != nil {
		t.Fatalf("demote owner: %v", err)
	}

	meReq := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+pat)
	meRec := httptest.NewRecorder()
	handler.ServeHTTP(meRec, meReq)
	if meRec.Code != http.StatusOK {
		t.Fatalf("PAT /api/me after demotion status=%d body=%s", meRec.Code, meRec.Body.String())
	}
	var me map[string]any
	if err := json.Unmarshal(meRec.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	if me["role"] != db.RoleViewer || me["allowed_nodes"] != allowed {
		t.Fatalf("PAT privilege drift: role=%v allowed_nodes=%v", me["role"], me["allowed_nodes"])
	}

	// Viewer owner cannot use an administrator PAT to mutate token state.
	writeReq := httptest.NewRequest(http.MethodPost, "/api/tokens", strings.NewReader(`{"name":"should-fail"}`))
	writeReq.Header.Set("Authorization", "Bearer "+pat)
	writeReq.Header.Set("Content-Type", "application/json")
	writeRec := httptest.NewRecorder()
	handler.ServeHTTP(writeRec, writeReq)
	if writeRec.Code != http.StatusForbidden {
		t.Fatalf("demoted PAT write status=%d body=%s", writeRec.Code, writeRec.Body.String())
	}

	// Resource scopes are enforced instead of being decorative metadata.
	nodesReq := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	nodesReq.Header.Set("Authorization", "Bearer "+readNodesPAT)
	nodesRec := httptest.NewRecorder()
	handler.ServeHTTP(nodesRec, nodesReq)
	if nodesRec.Code != http.StatusOK {
		t.Fatalf("read:nodes /api/nodes status=%d body=%s", nodesRec.Code, nodesRec.Body.String())
	}
	alertsReq := httptest.NewRequest(http.MethodGet, "/api/alerts", nil)
	alertsReq.Header.Set("Authorization", "Bearer "+readNodesPAT)
	alertsRec := httptest.NewRecorder()
	handler.ServeHTTP(alertsRec, alertsReq)
	if alertsRec.Code != http.StatusForbidden {
		t.Fatalf("read:nodes /api/alerts status=%d body=%s", alertsRec.Code, alertsRec.Body.String())
	}
}
