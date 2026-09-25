package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUsersAPIAndRBACOperations(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	server := NewServer(cfg, service)
	handler := server.Handler()
	session, csrf := task4AdminSession(t, service, store)

	// 1. Unauthenticated request returns 401
	unauthReq := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	unauthRec := httptest.NewRecorder()
	handler.ServeHTTP(unauthRec, unauthReq)
	if unauthRec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth status = %d, want 401", unauthRec.Code)
	}

	// 2. Admin lists users
	listReq := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	listReq.AddCookie(task4SessionCookie(session))
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list users status = %d, body = %s", listRec.Code, listRec.Body.String())
	}
	var usersList []map[string]any
	if err := json.Unmarshal(listRec.Body.Bytes(), &usersList); err != nil {
		t.Fatalf("unmarshal users: %v", err)
	}
	if len(usersList) == 0 {
		t.Fatal("expected at least 1 user (admin)")
	}

	// 3. Admin creates an Operator user with scoped nodes
	createPayload := `{
		"login": "operator_tom",
		"password": "TomPassword#123",
		"display_name": "Tom Operator",
		"role": "operator",
		"allowed_nodes": "node-uuid-a, node-uuid-b"
	}`
	csrf = task4CSRF(t, handler, session)
	createRec, _ := task4AdminWriteWithCSRF(t, handler, http.MethodPost, session, csrf, "/api/users", createPayload)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create user status = %d, body = %s", createRec.Code, createRec.Body.String())
	}
	var createdUser map[string]any
	_ = json.Unmarshal(createRec.Body.Bytes(), &createdUser)
	tomID := createdUser["id"].(string)
	if createdUser["role"] != "operator" {
		t.Fatalf("created user role = %v, want operator", createdUser["role"])
	}

	// 4. Admin creates a Viewer user
	viewerPayload := `{
		"login": "viewer_lucy",
		"password": "LucyPassword#123",
		"display_name": "Lucy Viewer",
		"role": "viewer",
		"allowed_nodes": "*"
	}`
	csrf = task4CSRF(t, handler, session)
	viewerRec, _ := task4AdminWriteWithCSRF(t, handler, http.MethodPost, session, csrf, "/api/users", viewerPayload)
	if viewerRec.Code != http.StatusCreated {
		t.Fatalf("create viewer status = %d, body = %s", viewerRec.Code, viewerRec.Body.String())
	}

	// 5. Operator logs in with username and password
	loginBody := `{"username":"operator_tom","password":"TomPassword#123"}`
	loginReq := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("operator login status = %d, body = %s", loginRec.Code, loginRec.Body.String())
	}

	// Extract operator session cookie
	var operatorCookie *http.Cookie
	for _, c := range loginRec.Result().Cookies() {
		if c.Name == "probewatch_session" {
			operatorCookie = c
			break
		}
	}
	if operatorCookie == nil {
		t.Fatal("missing session cookie in operator login")
	}

	// 6. Operator calls /api/me
	meReq := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	meReq.AddCookie(operatorCookie)
	meRec := httptest.NewRecorder()
	handler.ServeHTTP(meRec, meReq)
	if meRec.Code != http.StatusOK {
		t.Fatalf("me status = %d, body = %s", meRec.Code, meRec.Body.String())
	}
	var meData map[string]any
	_ = json.Unmarshal(meRec.Body.Bytes(), &meData)
	if meData["role"] != "operator" || meData["can_write"] != true || meData["is_admin"] != false {
		t.Fatalf("unexpected operator me data: %+v", meData)
	}

	// 7. Operator trying to manage users is rejected with 403 Forbidden
	opListUsersReq := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	opListUsersReq.AddCookie(operatorCookie)
	opListUsersRec := httptest.NewRecorder()
	handler.ServeHTTP(opListUsersRec, opListUsersReq)
	if opListUsersRec.Code != http.StatusForbidden {
		t.Fatalf("operator access /api/users = %d, want 403", opListUsersRec.Code)
	}

	// 8. Viewer logs in and tests write rejection
	vLoginBody := `{"username":"viewer_lucy","password":"LucyPassword#123"}`
	vLoginReq := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(vLoginBody))
	vLoginReq.Header.Set("Content-Type", "application/json")
	vLoginRec := httptest.NewRecorder()
	handler.ServeHTTP(vLoginRec, vLoginReq)
	if vLoginRec.Code != http.StatusOK {
		t.Fatalf("viewer login status = %d, body = %s", vLoginRec.Code, vLoginRec.Body.String())
	}

	var viewerCookie *http.Cookie
	for _, c := range vLoginRec.Result().Cookies() {
		if c.Name == "probewatch_session" {
			viewerCookie = c
			break
		}
	}
	if viewerCookie == nil {
		t.Fatal("missing session cookie in viewer login")
	}

	// Viewer fetches CSRF
	vCsrfReq := httptest.NewRequest(http.MethodGet, "/api/csrf", nil)
	vCsrfReq.AddCookie(viewerCookie)
	vCsrfRec := httptest.NewRecorder()
	handler.ServeHTTP(vCsrfRec, vCsrfReq)
	var vCsrfData map[string]any
	_ = json.Unmarshal(vCsrfRec.Body.Bytes(), &vCsrfData)
	vCsrf := vCsrfData["token"].(string)

	// Viewer attempts write operation -> rejected with 403 Forbidden
	writeAttempt := httptest.NewRequest(http.MethodPost, "/api/targets", strings.NewReader(`{"name":"New","kind":"tcp","host":"1.1.1.1"}`))
	writeAttempt.AddCookie(viewerCookie)
	writeAttempt.Header.Set("Content-Type", "application/json")
	writeAttempt.Header.Set("X-CSRF-Token", vCsrf)
	writeAttemptRec := httptest.NewRecorder()
	handler.ServeHTTP(writeAttemptRec, writeAttempt)
	if writeAttemptRec.Code != http.StatusForbidden {
		t.Fatalf("viewer write attempt status = %d, want 403", writeAttemptRec.Code)
	}

	// 9. Admin updates user
	updatePayload := `{"display_name":"Tom Lead","role":"operator","allowed_nodes":"*"}`
	csrf = task4CSRF(t, handler, session)
	upRec, _ := task4AdminWriteWithCSRF(t, handler, http.MethodPut, session, csrf, "/api/users/"+tomID, updatePayload)
	if upRec.Code != http.StatusOK {
		t.Fatalf("update user status = %d, body = %s", upRec.Code, upRec.Body.String())
	}

	// 10. Admin deletes user
	csrf = task4CSRF(t, handler, session)
	delRec, _ := task4AdminWriteWithCSRF(t, handler, http.MethodDelete, session, csrf, "/api/users/"+tomID, "{}")
	if delRec.Code != http.StatusOK {
		t.Fatalf("delete user status = %d, body = %s", delRec.Code, delRec.Body.String())
	}
}
