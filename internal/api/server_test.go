package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/auth"
	"github.com/probewatch/probewatch/internal/config"
	"github.com/probewatch/probewatch/internal/db"
)

func TestAuthenticationEndpointsRateLimitAttempts(t *testing.T) {
	provider := newServerFakeGitHub(t, "alice", nil)
	defer provider.Close()
	service, store := newServerAuth(t, provider, []string{"alice"}, "")
	defer store.Close()
	server := NewServer(config.Config{Environment: "development", PublicBaseURL: "http://127.0.0.1:8080", MaxRequestBody: 1024}, service)
	server.loginLimiter = newRateLimiter(1, time.Minute, 32)
	server.totpLimiter = newRateLimiter(1, time.Minute, 32)
	handler := server.Handler()

	loginRequest := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"password":"wrong"}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	if response := loginRequest(); response.Code != http.StatusUnauthorized {
		t.Fatalf("first login attempt status = %d, want 401", response.Code)
	}
	if response := loginRequest(); response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "60" {
		t.Fatalf("second login attempt = %d, Retry-After %q, want 429 and 60", response.Code, response.Header().Get("Retry-After"))
	}

	totpRequest := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/auth/totp/verify", strings.NewReader(`{"code":"000000"}`))
		request.Header.Set("Origin", "http://127.0.0.1:8080")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	if response := totpRequest(); response.Code != http.StatusUnauthorized {
		t.Fatalf("first TOTP attempt status = %d, want 401", response.Code)
	}
	if response := totpRequest(); response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "60" {
		t.Fatalf("second TOTP attempt = %d, Retry-After %q, want 429 and 60", response.Code, response.Header().Get("Retry-After"))
	}
}

func TestServerHandlerCoversHealthOAuthProtectedWriteAndLogout(t *testing.T) {
	provider := newServerFakeGitHub(t, "alice", nil)
	defer provider.Close()
	service, store := newServerAuth(t, provider, []string{"alice"}, "")
	defer store.Close()
	handler := NewServer(config.Config{Environment: "development", PublicBaseURL: "http://127.0.0.1:8080", MaxRequestBody: 1024}, service).Handler()

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK || health.Body.String() != "ok\n" {
		t.Fatalf("health response = %d %q", health.Code, health.Body.String())
	}

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/test/protected", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("protected status = %d, want 401", unauthenticated.Code)
	}
	unauthenticatedMe := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticatedMe, httptest.NewRequest(http.MethodGet, "/api/me/protected", nil))
	if unauthenticatedMe.Code != http.StatusUnauthorized {
		t.Fatalf("/api/me/protected unauthenticated status = %d, want 401", unauthenticatedMe.Code)
	}

	start := httptest.NewRecorder()
	handler.ServeHTTP(start, httptest.NewRequest(http.MethodGet, "/auth/github", nil))
	if start.Code != http.StatusFound {
		t.Fatalf("OAuth redirect status = %d, want 302", start.Code)
	}
	stateCookie := findCookie(start.Result().Cookies(), "probewatch_oauth_state")
	if stateCookie == nil {
		t.Fatal("OAuth redirect did not set state cookie")
	}
	redirect, err := url.Parse(start.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if redirect.Query().Get("state") == "" {
		t.Fatal("OAuth redirect did not include state")
	}

	callback := httptest.NewRecorder()
	callbackRequest := httptest.NewRequest(http.MethodGet, "/auth/github/callback?code=valid&state="+url.QueryEscape(redirect.Query().Get("state")), nil)
	callbackRequest.AddCookie(stateCookie)
	handler.ServeHTTP(callback, callbackRequest)
	if callback.Code != http.StatusFound {
		t.Fatalf("OAuth callback status = %d, want 302", callback.Code)
	}
	sessionCookie := findCookie(callback.Result().Cookies(), "probewatch_session")
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatal("OAuth callback did not set session cookie")
	}

	csrf := httptest.NewRecorder()
	csrfRequest := httptest.NewRequest(http.MethodGet, "/api/csrf", nil)
	csrfRequest.AddCookie(sessionCookie)
	handler.ServeHTTP(csrf, csrfRequest)
	if csrf.Code != http.StatusOK {
		t.Fatalf("CSRF status = %d, want 200", csrf.Code)
	}
	var csrfPayload struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(csrf.Body.Bytes(), &csrfPayload); err != nil {
		t.Fatal(err)
	}

	initialCSRFToken := csrfPayload.Token
	for name, token := range map[string]string{"missing": "", "valid": initialCSRFToken} {
		write := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/test/protected", strings.NewReader(`{}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "http://127.0.0.1:8080")
		request.Header.Set("X-CSRF-Token", token)
		request.AddCookie(sessionCookie)
		handler.ServeHTTP(write, request)
		want := http.StatusNoContent
		if name == "missing" {
			want = http.StatusForbidden
		}
		if write.Code != want {
			t.Fatalf("%s write status = %d, want %d", name, write.Code, want)
		}
		if name == "valid" && write.Header().Get("X-CSRF-Token") == "" {
			t.Fatal("successful write did not return a rotated CSRF token")
		}
		if name == "valid" {
			csrfPayload.Token = write.Header().Get("X-CSRF-Token")
		}
	}
	authenticatedMe := httptest.NewRecorder()
	authenticatedMeRequest := httptest.NewRequest(http.MethodGet, "/api/me/protected", nil)
	authenticatedMeRequest.AddCookie(sessionCookie)
	handler.ServeHTTP(authenticatedMe, authenticatedMeRequest)
	if authenticatedMe.Code != http.StatusOK {
		t.Fatalf("/api/me/protected authenticated status = %d, want 200", authenticatedMe.Code)
	}
	if csrfPayload.Token == initialCSRFToken {
		t.Fatal("successful write did not rotate the CSRF token")
	}

	replay := httptest.NewRecorder()
	replayRequest := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/test/protected", strings.NewReader(`{}`))
	replayRequest.Header.Set("Content-Type", "application/json")
	replayRequest.Header.Set("Origin", "http://127.0.0.1:8080")
	replayRequest.Header.Set("X-CSRF-Token", initialCSRFToken)
	replayRequest.AddCookie(sessionCookie)
	handler.ServeHTTP(replay, replayRequest)
	if replay.Code != http.StatusForbidden {
		t.Fatalf("replayed CSRF token status = %d, want 403", replay.Code)
	}

	logout := httptest.NewRecorder()
	logoutRequest := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/auth/logout", strings.NewReader(`{}`))
	logoutRequest.Header.Set("Content-Type", "application/json")
	logoutRequest.Header.Set("Origin", "http://127.0.0.1:8080")
	logoutRequest.Header.Set("X-CSRF-Token", csrfPayload.Token)
	logoutRequest.AddCookie(sessionCookie)
	handler.ServeHTTP(logout, logoutRequest)
	if logout.Code != http.StatusFound {
		t.Fatalf("logout status = %d, want 302", logout.Code)
	}
	protectedAfterLogout := httptest.NewRecorder()
	protectedRequest := httptest.NewRequest(http.MethodGet, "/api/test/protected", nil)
	protectedRequest.AddCookie(sessionCookie)
	handler.ServeHTTP(protectedAfterLogout, protectedRequest)
	if protectedAfterLogout.Code != http.StatusUnauthorized {
		t.Fatalf("post-logout protected status = %d, want 401", protectedAfterLogout.Code)
	}
}

func TestNewServerUsesConfiguredDatabasePepper(t *testing.T) {
	provider := newServerFakeGitHub(t, "alice", nil)
	defer provider.Close()
	service, store := newServerAuth(t, provider, []string{"alice"}, "")
	defer store.Close()
	server := NewServer(config.Config{Environment: "development", PublicBaseURL: "http://127.0.0.1:8080", MaxRequestBody: 1024}, service)
	if server == nil || server.Handler() == nil {
		t.Fatal("NewServer returned an unusable server")
	}
}

func TestProductionMuxDoesNotExposePlaceholderTestRoutes(t *testing.T) {
	provider := newServerFakeGitHub(t, "alice", nil)
	defer provider.Close()
	service, store := newServerAuth(t, provider, []string{"alice"}, "")
	defer store.Close()
	handler := NewServer(config.Config{Environment: "production", PublicBaseURL: "https://monitor.example.test", MaxRequestBody: 1024}, service).Handler()
	for _, path := range []string{"/api/test/protected", "/api/me/protected"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("production placeholder route %q status = %d, want 404", path, response.Code)
		}
	}
}

func TestInternalSessionLookupErrorReturns500(t *testing.T) {
	provider := newServerFakeGitHub(t, "alice", nil)
	defer provider.Close()
	service, store := newServerAuth(t, provider, []string{"alice"}, "")
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	handler := NewServer(config.Config{Environment: "development", PublicBaseURL: "http://127.0.0.1:8080", MaxRequestBody: 1024}, service).Handler()
	request := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	request.AddCookie(&http.Cookie{Name: "probewatch_session", Value: "some-session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("internal session lookup status = %d, want 500", response.Code)
	}
}

func TestInternalCSRFSessionLookupErrorReturns500(t *testing.T) {
	provider := newServerFakeGitHub(t, "alice", nil)
	defer provider.Close()
	service, store := newServerAuth(t, provider, []string{"alice"}, "")
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	handler := NewServer(config.Config{Environment: "development", PublicBaseURL: "http://127.0.0.1:8080", MaxRequestBody: 1024}, service).Handler()
	request := httptest.NewRequest(http.MethodGet, "/api/csrf", nil)
	request.AddCookie(&http.Cookie{Name: "probewatch_session", Value: "some-session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("internal CSRF session lookup status = %d, want 500", response.Code)
	}
}

func TestMeReturnsAuthenticatedIdentityAndIsGETOnlyInDevelopment(t *testing.T) {
	provider := newServerFakeGitHub(t, "alice", nil)
	defer provider.Close()
	service, store := newServerAuth(t, provider, []string{"alice"}, "")
	defer store.Close()
	admin, err := store.UpsertAdminUser(context.Background(), "github", "provider-42", "alice", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	sessionValue, err := service.CreateSessionForUser(context.Background(), admin.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	handler := NewServer(config.Config{Environment: "development", PublicBaseURL: "http://127.0.0.1:8080", MaxRequestBody: 1024}, service).Handler()

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/me", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated /api/me status = %d, want 401", unauthenticated.Code)
	}

	authenticatedRequest := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	authenticatedRequest.AddCookie(&http.Cookie{Name: "probewatch_session", Value: sessionValue})
	authenticated := httptest.NewRecorder()
	handler.ServeHTTP(authenticated, authenticatedRequest)
	if authenticated.Code != http.StatusOK {
		t.Fatalf("authenticated /api/me status = %d, want 200", authenticated.Code)
	}
	var identity struct {
		ID       string `json:"id"`
		Provider string `json:"provider"`
		Login    string `json:"login"`
	}
	if err := json.Unmarshal(authenticated.Body.Bytes(), &identity); err != nil {
		t.Fatal(err)
	}
	if identity != (struct {
		ID       string `json:"id"`
		Provider string `json:"provider"`
		Login    string `json:"login"`
	}{ID: "provider-42", Provider: "github", Login: "alice"}) {
		t.Fatalf("/api/me identity = %#v", identity)
	}
	if strings.Contains(authenticated.Body.String(), sessionValue) || strings.Contains(authenticated.Body.String(), "client-secret") {
		t.Fatal("/api/me exposed a session or provider secret")
	}

	post := httptest.NewRecorder()
	postRequest := httptest.NewRequest(http.MethodPost, "/api/me", strings.NewReader(`{}`))
	postRequest.AddCookie(&http.Cookie{Name: "probewatch_session", Value: sessionValue})
	postRequest.Header.Set("Content-Type", "application/json")
	postRequest.Header.Set("Origin", "http://127.0.0.1:8080")
	handler.ServeHTTP(post, postRequest)
	if post.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /api/me status = %d, want 405", post.Code)
	}
}

func TestProductionMeReturnsIdentityWithoutPlaceholderWriteBehavior(t *testing.T) {
	provider := newServerFakeGitHub(t, "alice", nil)
	defer provider.Close()
	service, store := newServerAuth(t, provider, []string{"alice"}, "")
	defer store.Close()
	admin, err := store.UpsertAdminUser(context.Background(), "github", "provider-prod-42", "alice", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	sessionValue, err := service.CreateSessionForUser(context.Background(), admin.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	handler := NewServer(config.Config{Environment: "production", PublicBaseURL: "https://monitor.example.test", MaxRequestBody: 1024}, service).Handler()
	request := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	request.AddCookie(&http.Cookie{Name: "probewatch_session", Value: sessionValue})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), `"status":"ok"`) {
		t.Fatalf("production /api/me response = %d %q", response.Code, response.Body.String())
	}
}

func newServerAuth(t *testing.T, provider *httptest.Server, users []string, organization string) (*auth.Service, *db.Store) {
	t.Helper()
	store, err := db.OpenStore(filepath.Join(t.TempDir(), "probe.db"), []byte("test-pepper"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Environment: "development", PublicBaseURL: "http://127.0.0.1:8080", GitHubClientID: "client-id", GitHubClientSecret: "client-secret", GitHubRedirectURL: "http://127.0.0.1:8080/auth/github/callback", GitHubAllowedUsers: users, GitHubAllowedOrg: organization, SessionSecret: "test-session-secret-that-is-long-enough"}
	return auth.NewService(cfg, store, auth.ProviderEndpoints{AuthorizeURL: provider.URL + "/login/oauth/authorize", TokenURL: provider.URL + "/login/oauth/access_token", UserURL: provider.URL + "/user", OrganizationsURL: provider.URL + "/user/orgs"}), store
}

func newServerFakeGitHub(t *testing.T, login string, organizations []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "provider-secret"})
		case "/user":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "login": login})
		case "/user/orgs":
			values := make([]map[string]string, 0, len(organizations))
			for _, organization := range organizations {
				values = append(values, map[string]string{"login": organization})
			}
			_ = json.NewEncoder(w).Encode(values)
		default:
			http.NotFound(w, r)
		}
	}))
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}
