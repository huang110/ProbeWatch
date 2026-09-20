package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/auth"
	"github.com/probewatch/probewatch/internal/config"
	"github.com/probewatch/probewatch/internal/db"
)

func TestUnauthenticatedAPIRequestReturnsJSON401(t *testing.T) {
	service := newMiddlewareAuth(t, "development")
	handler := NewMiddleware(service, config.Config{PublicBaseURL: "http://127.0.0.1:8080", MaxRequestBody: 1024}).RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	request := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status/content type = %d/%q", response.Code, response.Header().Get("Content-Type"))
	}
}

func TestWriteWithoutCSRFIsForbidden(t *testing.T) {
	service, session := newAuthenticatedMiddleware(t, "development")
	handler := NewMiddleware(service, config.Config{PublicBaseURL: "http://127.0.0.1:8080", MaxRequestBody: 1024}).RequireAuth(NewMiddleware(service, config.Config{PublicBaseURL: "http://127.0.0.1:8080", MaxRequestBody: 1024}).RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })))
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/nodes", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(session)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", response.Code)
	}
}

func TestSameOriginWriteWithCSRFIsAccepted(t *testing.T) {
	service, session := newAuthenticatedMiddleware(t, "development")
	token := issueCSRF(t, service, session)
	handler := NewMiddleware(service, config.Config{PublicBaseURL: "http://127.0.0.1:8080", MaxRequestBody: 1024}).RequireAuth(NewMiddleware(service, config.Config{PublicBaseURL: "http://127.0.0.1:8080", MaxRequestBody: 1024}).RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })))
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/nodes", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://127.0.0.1:8080")
	request.Header.Set("X-CSRF-Token", token)
	request.AddCookie(session)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", response.Code)
	}
}

func TestWriteWithExpiredOrDeletedSessionIsUnauthorized(t *testing.T) {
	service, session := newAuthenticatedMiddleware(t, "development")
	handler := NewMiddleware(service, config.Config{PublicBaseURL: "http://127.0.0.1:8080", MaxRequestBody: 1024}).RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	expiredService, expiredSession := newExpiredMiddlewareAuth(t)
	expiredHandler := NewMiddleware(expiredService, config.Config{PublicBaseURL: "http://127.0.0.1:8080", MaxRequestBody: 1024}).RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	expiredRequest := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/nodes", strings.NewReader(`{}`))
	expiredRequest.Header.Set("Content-Type", "application/json")
	expiredRequest.Header.Set("Origin", "http://127.0.0.1:8080")
	expiredRequest.Header.Set("X-CSRF-Token", "invalid")
	expiredRequest.AddCookie(expiredSession)
	expiredResponse := httptest.NewRecorder()
	expiredHandler.ServeHTTP(expiredResponse, expiredRequest)
	if expiredResponse.Code != http.StatusUnauthorized {
		t.Fatalf("expired session status = %d, want 401", expiredResponse.Code)
	}

	deletedSession := newSessionCookie(t, service, session)
	logoutRequest := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	logoutRequest.AddCookie(deletedSession)
	service.Logout(httptest.NewRecorder(), logoutRequest)
	deletedRequest := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/nodes", strings.NewReader(`{}`))
	deletedRequest.Header.Set("Content-Type", "application/json")
	deletedRequest.Header.Set("Origin", "http://127.0.0.1:8080")
	deletedRequest.Header.Set("X-CSRF-Token", "invalid")
	deletedRequest.AddCookie(deletedSession)
	deletedResponse := httptest.NewRecorder()
	handler.ServeHTTP(deletedResponse, deletedRequest)
	if deletedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("deleted session status = %d, want 401", deletedResponse.Code)
	}
}

func TestCSRFTokenIsBoundToItsSession(t *testing.T) {
	service, session := newAuthenticatedMiddleware(t, "development")
	otherSession := newSessionCookie(t, service, session)
	token := issueCSRF(t, service, session)
	handler := NewMiddleware(service, config.Config{PublicBaseURL: "http://127.0.0.1:8080", MaxRequestBody: 1024}).RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/nodes", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://127.0.0.1:8080")
	request.Header.Set("X-CSRF-Token", token)
	request.AddCookie(otherSession)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-session CSRF status = %d, want 403", response.Code)
	}
}

func TestSuccessfulWriteRotatesCSRFTokenAndRejectsReplay(t *testing.T) {
	service, session := newAuthenticatedMiddleware(t, "development")
	handler := NewMiddleware(service, config.Config{PublicBaseURL: "http://127.0.0.1:8080", MaxRequestBody: 1024}).RequireAuth(NewMiddleware(service, config.Config{PublicBaseURL: "http://127.0.0.1:8080", MaxRequestBody: 1024}).RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })))
	initial := issueCSRF(t, service, session)
	first := csrfWriteRequest(session, initial)
	firstResponse := httptest.NewRecorder()
	handler.ServeHTTP(firstResponse, first)
	if firstResponse.Code != http.StatusNoContent {
		t.Fatalf("first write status = %d, want 204", firstResponse.Code)
	}
	next := firstResponse.Header().Get("X-CSRF-Token")
	if next == "" || next == initial {
		t.Fatalf("rotated token = %q, want a new token", next)
	}

	replayResponse := httptest.NewRecorder()
	handler.ServeHTTP(replayResponse, csrfWriteRequest(session, initial))
	if replayResponse.Code != http.StatusForbidden {
		t.Fatalf("replayed write status = %d, want 403", replayResponse.Code)
	}
	currentResponse := httptest.NewRecorder()
	handler.ServeHTTP(currentResponse, csrfWriteRequest(session, next))
	if currentResponse.Code != http.StatusNoContent {
		t.Fatalf("rotated-token write status = %d, want 204", currentResponse.Code)
	}
}

func TestFailedWriteDoesNotRotateCSRFToken(t *testing.T) {
	service, session := newAuthenticatedMiddleware(t, "development")
	token := issueCSRF(t, service, session)
	failing := NewMiddleware(service, config.Config{PublicBaseURL: "http://127.0.0.1:8080", MaxRequestBody: 1024}).RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) }))
	failedResponse := httptest.NewRecorder()
	failing.ServeHTTP(failedResponse, csrfWriteRequest(session, token))
	if failedResponse.Code != http.StatusInternalServerError {
		t.Fatalf("failed write status = %d, want 500", failedResponse.Code)
	}

	succeeding := NewMiddleware(service, config.Config{PublicBaseURL: "http://127.0.0.1:8080", MaxRequestBody: 1024}).RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	successResponse := httptest.NewRecorder()
	succeeding.ServeHTTP(successResponse, csrfWriteRequest(session, token))
	if successResponse.Code != http.StatusForbidden {
		t.Fatalf("retry after failed write status = %d, want 403", successResponse.Code)
	}
}

func TestConcurrentSameCSRFTokenRunsAtMostOneHandler(t *testing.T) {
	service, session := newAuthenticatedMiddleware(t, "development")
	token := issueCSRF(t, service, session)
	var executions atomic.Int32
	handler := NewMiddleware(service, config.Config{PublicBaseURL: "http://127.0.0.1:8080", MaxRequestBody: 1024}).RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		executions.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))

	responses := make([]*httptest.ResponseRecorder, 2)
	var group sync.WaitGroup
	for i := range responses {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			responses[i] = httptest.NewRecorder()
			handler.ServeHTTP(responses[i], csrfWriteRequest(session, token))
		}(i)
	}
	group.Wait()

	if got := executions.Load(); got > 1 {
		t.Fatalf("same-token handler executions = %d, want at most 1", got)
	}
	successes := 0
	for _, response := range responses {
		if response.Code == http.StatusNoContent {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("same-token successful responses = %d, want 1", successes)
	}
}

func TestConcurrentCSRFRefreshAndWriteKeepWriteReplacementUsable(t *testing.T) {
	service, session := newAuthenticatedMiddleware(t, "development")
	token := issueCSRF(t, service, session)
	entered := make(chan struct{})
	continueHandler := make(chan struct{})
	handler := NewMiddleware(service, config.Config{PublicBaseURL: "http://127.0.0.1:8080", MaxRequestBody: 1024}).RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-continueHandler
		w.WriteHeader(http.StatusNoContent)
	}))

	writeResponse := httptest.NewRecorder()
	writeDone := make(chan struct{})
	go func() {
		handler.ServeHTTP(writeResponse, csrfWriteRequest(session, token))
		close(writeDone)
	}()
	<-entered

	refreshResponse := httptest.NewRecorder()
	refreshDone := make(chan struct{})
	go func() {
		refresh := httptest.NewRequest(http.MethodGet, "/api/csrf", nil)
		refresh.AddCookie(session)
		service.CSRFHandler(refreshResponse, refresh)
		close(refreshDone)
	}()

	select {
	case <-refreshDone:
		t.Fatal("CSRF refresh completed before the in-flight write released its claim")
	case <-time.After(25 * time.Millisecond):
	}
	close(continueHandler)
	<-writeDone
	if writeResponse.Code != http.StatusNoContent {
		t.Fatalf("write status = %d, want 204", writeResponse.Code)
	}
	replacement := writeResponse.Header().Get("X-CSRF-Token")
	if replacement == "" {
		t.Fatal("write did not return a CSRF replacement")
	}
	if writeResponse.Header().Get("X-CSRF-Refresh-Required") != "true" {
		t.Fatal("write did not require a refresh when a refresh raced with it")
	}
	<-refreshDone
	var refreshPayload struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(refreshResponse.Body.Bytes(), &refreshPayload); err != nil {
		t.Fatal(err)
	}
	if refreshPayload.Token == "" {
		t.Fatal("concurrent refresh did not return a current token")
	}
	if response := performCSRFWrite(service, session, refreshPayload.Token); response != http.StatusNoContent {
		t.Fatalf("refresh replacement status = %d, want 204", response)
	}
}

func TestSuccessfulWriteReturnsReplacementForAny2xxStatus(t *testing.T) {
	for _, status := range []int{http.StatusCreated, http.StatusNoContent, http.StatusMultipleChoices - 1} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			service, session := newAuthenticatedMiddleware(t, "development")
			token := issueCSRF(t, service, session)
			handler := NewMiddleware(service, config.Config{PublicBaseURL: "http://127.0.0.1:8080", MaxRequestBody: 1024}).RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, csrfWriteRequest(session, token))
			if response.Code != status {
				t.Fatalf("status = %d, want %d", response.Code, status)
			}
			if response.Header().Get("X-CSRF-Token") == "" {
				t.Fatal("successful 2xx write did not return a replacement token")
			}
		})
	}
}

func TestCrossOriginWriteIsRejected(t *testing.T) {
	service, session := newAuthenticatedMiddleware(t, "development")
	token := issueCSRF(t, service, session)
	handler := NewMiddleware(service, config.Config{PublicBaseURL: "http://127.0.0.1:8080", MaxRequestBody: 1024}).RequireAuth(NewMiddleware(service, config.Config{PublicBaseURL: "http://127.0.0.1:8080", MaxRequestBody: 1024}).RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })))
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/nodes", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://evil.example")
	request.Header.Set("X-CSRF-Token", token)
	request.AddCookie(session)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", response.Code)
	}
}

func TestMalformedAndUserInfoOriginsAreRejected(t *testing.T) {
	service, session := newAuthenticatedMiddleware(t, "development")
	token := issueCSRF(t, service, session)
	handler := NewMiddleware(service, config.Config{PublicBaseURL: "http://127.0.0.1:8080", MaxRequestBody: 1024}).RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for _, origin := range []string{"http://user:password@127.0.0.1:8080", "http://[127.0.0.1", "http:127.0.0.1:8080"} {
		t.Run(origin, func(t *testing.T) {
			request := csrfWriteRequest(session, token)
			request.Header.Set("Origin", origin)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden {
				t.Fatalf("origin %q status = %d, want 403", origin, response.Code)
			}
		})
	}
}

func TestSameOriginTreatsDefaultPortsAsEquivalent(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "https://example.test/api/write", nil)
	request.Header.Set("Origin", "https://example.test")
	if !sameOrigin(request, "https://example.test:443") {
		t.Fatal("same-origin check rejected equivalent default HTTPS port")
	}
	request.Header.Set("Origin", "http://example.test")
	if sameOrigin(request, "http://example.test:80") == false {
		t.Fatal("same-origin check rejected equivalent default HTTP port")
	}
}

func TestDifferentSessionsDoNotBlockEachOtherDuringCSRFHandlers(t *testing.T) {
	service, session := newAuthenticatedMiddleware(t, "development")
	otherSession := newSessionCookie(t, service, session)
	token := issueCSRF(t, service, session)
	otherToken := issueCSRF(t, service, otherSession)
	entered := make(chan struct{})
	release := make(chan struct{})
	var firstRequest atomic.Bool
	handler := NewMiddleware(service, config.Config{PublicBaseURL: "http://127.0.0.1:8080", MaxRequestBody: 1024}).RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if firstRequest.CompareAndSwap(false, true) {
			close(entered)
			<-release
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, csrfWriteRequest(session, token))
	}()
	<-entered
	secondDone := make(chan struct{})
	go func() {
		defer close(secondDone)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, csrfWriteRequest(otherSession, otherToken))
	}()
	select {
	case <-secondDone:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("CSRF handler for another session blocked behind an in-flight session")
	}
	close(release)
	<-firstDone
}

func TestOversizedBodyAndWrongContentTypeAreRejected(t *testing.T) {
	service, session := newAuthenticatedMiddleware(t, "development")
	handler := NewMiddleware(service, config.Config{PublicBaseURL: "http://127.0.0.1:8080", MaxRequestBody: 4}).RequireAuth(NewMiddleware(service, config.Config{PublicBaseURL: "http://127.0.0.1:8080", MaxRequestBody: 4}).RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })))

	for name, values := range map[string][]string{"body": {"{}", "application/json"}, "content": {"{}", "text/plain"}} {
		requestBody := values[0]
		if name == "body" {
			requestBody = `{"too":"large"}`
		}
		request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/nodes", strings.NewReader(requestBody))
		request.Header.Set("Content-Type", values[1])
		request.Header.Set("Origin", "http://127.0.0.1:8080")
		request.Header.Set("X-CSRF-Token", "invalid")
		request.AddCookie(session)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest && response.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("%s status = %d, want 400 or 413", name, response.Code)
		}
	}
}

func newMiddlewareAuth(t *testing.T, environment string) *auth.Service {
	service, _ := newAuthenticatedMiddleware(t, environment)
	return service
}

func newAuthenticatedMiddleware(t *testing.T, environment string) (*auth.Service, *http.Cookie) {
	t.Helper()
	store, err := db.OpenStore(t.TempDir()+"/probe.db", []byte("test-pepper"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	admin, err := store.UpsertAdminUser(context.Background(), "github", "42", "alice", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	service := auth.NewService(config.Config{Environment: environment, PublicBaseURL: "http://127.0.0.1:8080", SessionSecret: "test-session-secret-that-is-long-enough"}, store, auth.ProviderEndpoints{})
	value, err := service.CreateSessionForUser(context.Background(), admin.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return service, &http.Cookie{Name: "probewatch_session", Value: value}
}

func newSessionCookie(t *testing.T, service *auth.Service, base *http.Cookie) *http.Cookie {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(base)
	admin, ok := service.Authenticate(request, time.Now())
	if !ok {
		t.Fatal("base session did not authenticate")
	}
	value, err := service.CreateSessionForUser(context.Background(), admin.AdminUserID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: "probewatch_session", Value: value}
}

func newExpiredMiddlewareAuth(t *testing.T) (*auth.Service, *http.Cookie) {
	t.Helper()
	store, err := db.OpenStore(t.TempDir()+"/probe.db", []byte("test-pepper"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	admin, err := store.UpsertAdminUser(context.Background(), "github", "expired", "alice", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	value := "expired-session"
	if err := store.CreateSession(context.Background(), "expired-id", []byte(value), admin.ID, time.Now().Add(-time.Minute), time.Now().Add(-2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	service := auth.NewService(config.Config{Environment: "development", PublicBaseURL: "http://127.0.0.1:8080", SessionSecret: "test-session-secret-that-is-long-enough"}, store, auth.ProviderEndpoints{})
	return service, &http.Cookie{Name: "probewatch_session", Value: value}
}

func issueCSRF(t *testing.T, service *auth.Service, session *http.Cookie) string {
	t.Helper()
	var payload struct {
		Token string `json:"token"`
	}
	request := httptest.NewRequest(http.MethodGet, "/api/csrf", nil)
	request.AddCookie(session)
	response := httptest.NewRecorder()
	service.CSRFHandler(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("CSRF status = %d", response.Code)
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Token
}

func csrfWriteRequest(session *http.Cookie, token string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/nodes", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://127.0.0.1:8080")
	request.Header.Set("X-CSRF-Token", token)
	request.AddCookie(session)
	return request
}

func performCSRFWrite(service *auth.Service, session *http.Cookie, token string) int {
	handler := NewMiddleware(service, config.Config{PublicBaseURL: "http://127.0.0.1:8080", MaxRequestBody: 1024}).RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, csrfWriteRequest(session, token))
	return response.Code
}
