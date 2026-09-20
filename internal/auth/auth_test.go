package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/config"
	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/security"
)

func TestOAuthStateIsSingleUse(t *testing.T) {
	provider := newFakeGitHub(t, "alice")
	defer provider.Close()
	service, _ := newAuthService(t, provider, []string{"alice"})

	start := httptest.NewRecorder()
	service.BeginOAuth(start, httptest.NewRequest(http.MethodGet, "/auth/github", nil))
	stateCookie := start.Result().Cookies()[0]
	redirect := start.Result().Header.Get("Location")
	query, _ := url.ParseQuery(mustParseURL(t, redirect).RawQuery)

	for attempt := 0; attempt < 2; attempt++ {
		req := httptest.NewRequest(http.MethodGet, "/auth/github/callback?code=valid&state="+url.QueryEscape(query.Get("state")), nil)
		req.AddCookie(stateCookie)
		response := httptest.NewRecorder()
		service.Callback(response, req)
		if attempt == 0 && response.Code != http.StatusFound {
			t.Fatalf("first callback status = %d, want 302", response.Code)
		}
		if attempt == 1 && response.Code != http.StatusForbidden {
			t.Fatalf("reused callback status = %d, want 403", response.Code)
		}
	}

}

func TestOAuthStateHas32BytesOfEntropy(t *testing.T) {
	provider := newFakeGitHub(t, "alice")
	defer provider.Close()
	service, _ := newAuthService(t, provider, []string{"alice"})

	response := httptest.NewRecorder()
	service.BeginOAuth(response, httptest.NewRequest(http.MethodGet, "/auth/github", nil))
	cookie := findCookie(response.Result().Cookies(), oauthStateCookieName)
	if cookie == nil {
		t.Fatal("OAuth state cookie was not set")
	}
	if got := len(securityTokenBytes(t, cookie.Value)); got != security.TokenEntropyBytes {
		t.Fatalf("OAuth state entropy bytes = %d, want %d", got, security.TokenEntropyBytes)
	}
}

func TestOAuthStateMismatchAndExpiryAreRejected(t *testing.T) {
	provider := newFakeGitHub(t, "alice")
	defer provider.Close()
	service, _ := newAuthService(t, provider, []string{"alice"})

	start := httptest.NewRecorder()
	service.BeginOAuth(start, httptest.NewRequest(http.MethodGet, "/auth/github", nil))
	cookie := findCookie(start.Result().Cookies(), oauthStateCookieName)
	query, _ := url.ParseQuery(mustParseURL(t, start.Result().Header.Get("Location")).RawQuery)
	mismatch := httptest.NewRequest(http.MethodGet, "/auth/github/callback?code=valid&state=mismatch", nil)
	mismatch.AddCookie(cookie)
	mismatchResponse := httptest.NewRecorder()
	service.Callback(mismatchResponse, mismatch)
	if mismatchResponse.Code != http.StatusForbidden {
		t.Fatalf("mismatched callback status = %d, want 403", mismatchResponse.Code)
	}

	expiredState, err := security.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.store.CreateOAuthState(context.Background(), security.Digest([]byte(service.cfg.SessionSecret), expiredState), time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	expired := httptest.NewRequest(http.MethodGet, "/auth/github/callback?code=valid&state="+url.QueryEscape(expiredState), nil)
	expired.AddCookie(&http.Cookie{Name: oauthStateCookieName, Value: expiredState})
	expiredResponse := httptest.NewRecorder()
	service.Callback(expiredResponse, expired)
	if expiredResponse.Code != http.StatusForbidden {
		t.Fatalf("expired callback status = %d, want 403", expiredResponse.Code)
	}
	if query.Get("state") == "" {
		t.Fatal("OAuth redirect did not include state")
	}
}

func TestUnauthorizedGitHubUserIsRejected(t *testing.T) {
	provider := newFakeGitHub(t, "mallory")
	defer provider.Close()
	service, _ := newAuthService(t, provider, []string{"alice"})

	response := completeOAuth(t, service)
	if response.Code != http.StatusForbidden {
		t.Fatalf("unauthorized callback status = %d, want 403", response.Code)
	}
	if strings.Contains(response.Body.String(), "mallory") || strings.Contains(response.Body.String(), "access") {
		t.Fatalf("provider/user details leaked in response: %q", response.Body.String())
	}
}

func TestAllowedGitHubOrganizationIsAccepted(t *testing.T) {
	provider := newFakeGitHubWithOrganizations(t, "mallory", []string{"probe-admins"})
	defer provider.Close()
	service, _ := newAuthServiceWithOrganization(t, provider, nil, "probe-admins")

	response := completeOAuth(t, service)
	if response.Code != http.StatusFound {
		t.Fatalf("organization-allowed callback status = %d, want 302", response.Code)
	}
}

func TestOAuthClientRejectsCrossOriginRedirectsAndHasTimeout(t *testing.T) {
	var evilHits atomic.Int32
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		evilHits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "login": "alice"})
	}))
	defer evil.Close()

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "provider-secret"})
		case "/user":
			http.Redirect(w, r, evil.URL+"/user", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	service, _ := newAuthService(t, provider, []string{"alice"})

	if service.client.Timeout <= 0 {
		t.Fatal("OAuth client has no finite timeout")
	}
	response := completeOAuth(t, service)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("cross-origin OAuth redirect status = %d, want 500", response.Code)
	}
	if evilHits.Load() != 0 {
		t.Fatal("OAuth client followed a cross-origin redirect")
	}
}

func TestOAuthClientRejectsOversizedProviderResponses(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "provider-secret"})
		case "/user":
			_, _ = io.WriteString(w, strings.Repeat("x", maxOAuthResponseBytes+1))
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	service, _ := newAuthService(t, provider, []string{"alice"})

	response := completeOAuth(t, service)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("oversized OAuth response status = %d, want 500", response.Code)
	}
}

func TestAllowedGitHubOrganizationOnLaterPageIsAccepted(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "provider-secret"})
		case "/user":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "login": "mallory"})
		case "/user/orgs":
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			if page <= 1 {
				organizations := make([]map[string]string, 100)
				for i := range organizations {
					organizations[i] = map[string]string{"login": "other-org"}
				}
				w.Header().Set("Link", "<"+r.URL.String()+"&page=2>; rel=\"next\"")
				_ = json.NewEncoder(w).Encode(organizations)
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]string{{"login": "probe-admins"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	service, _ := newAuthServiceWithOrganization(t, provider, nil, "probe-admins")

	response := completeOAuth(t, service)
	if response.Code != http.StatusFound {
		t.Fatalf("later-page organization callback status = %d, want 302", response.Code)
	}
}

func TestDirectOrganizationMembershipCheckAcceptsActiveMember(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "provider-secret"})
		case "/user":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "login": "mallory"})
		case "/user/orgs":
			_ = json.NewEncoder(w).Encode([]map[string]string{})
		case "/user/memberships/orgs/probe-admins":
			_ = json.NewEncoder(w).Encode(map[string]string{"state": "active"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	store, err := db.OpenStore(t.TempDir()+"/probe.db", []byte("test-pepper"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := NewService(config.Config{Environment: "development", PublicBaseURL: "http://127.0.0.1:8080", GitHubClientID: "client-id", GitHubClientSecret: "client-secret", GitHubRedirectURL: "http://127.0.0.1:8080/auth/github/callback", GitHubAllowedOrg: "probe-admins", SessionSecret: "test-session-secret-that-is-long-enough"}, store, ProviderEndpoints{AuthorizeURL: provider.URL + "/authorize", TokenURL: provider.URL + "/login/oauth/access_token", UserURL: provider.URL + "/user", OrganizationsURL: provider.URL + "/user/orgs", OrganizationMembershipURL: provider.URL + "/user/memberships/orgs/{org}"})
	if response := completeOAuth(t, service); response.Code != http.StatusFound {
		t.Fatalf("direct membership callback status = %d, want 302", response.Code)
	}
}

func TestGitHubOrganizationAllowlistRejectsNonMemberAndDefaultsToDeny(t *testing.T) {
	provider := newFakeGitHubWithOrganizations(t, "mallory", []string{"other-org"})
	defer provider.Close()
	service, _ := newAuthServiceWithOrganization(t, provider, nil, "probe-admins")
	if response := completeOAuth(t, service); response.Code != http.StatusForbidden {
		t.Fatalf("non-member callback status = %d, want 403", response.Code)
	}

	noPolicy, _ := newAuthServiceWithOrganization(t, provider, nil, "")
	if response := completeOAuth(t, noPolicy); response.Code != http.StatusForbidden {
		t.Fatalf("no-policy callback status = %d, want 403", response.Code)
	}
}

func TestAllowedGitHubUserIsAcceptedAndSessionCookieIsSecureOutsideDevelopment(t *testing.T) {
	provider := newFakeGitHub(t, "alice")
	defer provider.Close()
	service, _ := newAuthServiceWithEnvironment(t, provider, []string{"alice"}, "production")

	response := completeOAuth(t, service)
	if response.Code != http.StatusFound {
		t.Fatalf("allowed callback status = %d, want 302", response.Code)
	}
	cookie := findCookie(response.Result().Cookies(), sessionCookieName)
	if cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || !cookie.Secure {
		t.Fatalf("session cookie flags = %#v", cookie)
	}
	if cookie.MaxAge <= 0 || cookie.Expires.IsZero() {
		t.Fatalf("session cookie expiry missing: %#v", cookie)
	}
}

func TestSessionExpires(t *testing.T) {
	provider := newFakeGitHub(t, "alice")
	defer provider.Close()
	service, _ := newAuthService(t, provider, []string{"alice"})
	var admin db.AdminUser
	admin, err := service.store.UpsertAdminUser(context.Background(), "github", "expired", "alice", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := service.store.CreateSession(context.Background(), "id", []byte("session"), admin.ID, time.Now().Add(-time.Minute), time.Now().Add(-2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
	if _, ok := service.Authenticate(request, time.Now()); ok {
		t.Fatal("expired session authenticated")
	}
}

func TestSessionIsRevokedWhenAuthorizationPolicyChanges(t *testing.T) {
	provider := newFakeGitHub(t, "alice")
	defer provider.Close()
	service, store := newAuthService(t, provider, []string{"alice"})
	admin, err := store.UpsertAdminUser(context.Background(), "github", "policy-change", "alice", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	value, err := service.CreateSessionForUser(context.Background(), admin.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	service.cfg.GitHubAllowedUsers = []string{"mallory"}

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: value})
	if _, ok := service.Authenticate(request, time.Now()); ok {
		t.Fatal("session remained authenticated after authorization policy changed")
	}
	if _, err := store.GetSession(context.Background(), []byte(value), time.Now()); !errors.Is(err, db.ErrSessionNotFound) {
		t.Fatalf("policy-changed session lookup error = %v, want ErrSessionNotFound", err)
	}
}

func TestCSRFLockRegistryDeletesIdleEntries(t *testing.T) {
	provider := newFakeGitHub(t, "alice")
	defer provider.Close()
	service, _ := newAuthService(t, provider, []string{"alice"})

	for i := 0; i < 200; i++ {
		request := httptest.NewRequest(http.MethodPost, "/api/write", nil)
		request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: fmt.Sprintf("session-%d", i)})
		unlock := service.LockSessionCSRF(request)
		unlock()
	}
	if got := service.csrfRegistrySize(); got != 0 {
		t.Fatalf("idle CSRF registry entries = %d, want 0", got)
	}
}

func TestLogoutInvalidatesSession(t *testing.T) {
	provider := newFakeGitHub(t, "alice")
	defer provider.Close()
	service, store := newAuthService(t, provider, []string{"alice"})
	admin, err := store.UpsertAdminUser(context.Background(), "github", "logout", "alice", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	session, err := service.CreateSessionForUser(context.Background(), admin.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	response := httptest.NewRecorder()
	service.Logout(response, request)
	if response.Code != http.StatusFound {
		t.Fatalf("logout status = %d, want 302", response.Code)
	}
	check := httptest.NewRequest(http.MethodGet, "/", nil)
	check.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	if _, ok := service.Authenticate(check, time.Now()); ok {
		t.Fatal("logged-out session authenticated")
	}
}

func TestCSRFHandlerIsGETOnlyAndDoesNotRotateOnOtherMethods(t *testing.T) {
	provider := newFakeGitHub(t, "alice")
	defer provider.Close()
	service, store := newAuthService(t, provider, []string{"alice"})
	admin, err := store.UpsertAdminUser(context.Background(), "github", "csrf-method", "alice", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	session, err := service.CreateSessionForUser(context.Background(), admin.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	get := httptest.NewRequest(http.MethodGet, "/api/csrf", nil)
	get.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	getResponse := httptest.NewRecorder()
	service.CSRFHandler(getResponse, get)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("GET CSRF status = %d, want 200", getResponse.Code)
	}
	var payload struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(getResponse.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}

	post := httptest.NewRequest(http.MethodPost, "/api/csrf", nil)
	post.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	postResponse := httptest.NewRecorder()
	service.CSRFHandler(postResponse, post)
	if postResponse.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST CSRF status = %d, want 405", postResponse.Code)
	}
	if _, err := store.ClaimCSRF(context.Background(), []byte(session), payload.Token, time.Now()); err != nil {
		t.Fatalf("non-GET CSRF request rotated token: %v", err)
	}
}

func TestLogoutReturns500AndKeepsCookieWhenSessionDeletionFails(t *testing.T) {
	provider := newFakeGitHub(t, "alice")
	defer provider.Close()
	service, store := newAuthService(t, provider, []string{"alice"})
	admin, err := store.UpsertAdminUser(context.Background(), "github", "logout-error", "alice", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	session, err := service.CreateSessionForUser(context.Background(), admin.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	response := httptest.NewRecorder()
	service.Logout(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("logout failure status = %d, want 500", response.Code)
	}
	if response.Header().Get("Set-Cookie") != "" {
		t.Fatal("logout failure cleared the session cookie")
	}
	if response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("logout failure content type = %q, want application/json", response.Header().Get("Content-Type"))
	}
}

func newAuthService(t *testing.T, provider *httptest.Server, users []string) (*Service, *db.Store) {
	return newAuthServiceWithOrganization(t, provider, users, "")
}

func newAuthServiceWithEnvironment(t *testing.T, provider *httptest.Server, users []string, environment string) (*Service, *db.Store) {
	return newAuthServiceWithOrganizationAndEnvironment(t, provider, users, "", environment)
}

func newAuthServiceWithOrganization(t *testing.T, provider *httptest.Server, users []string, organization string) (*Service, *db.Store) {
	return newAuthServiceWithOrganizationAndEnvironment(t, provider, users, organization, "development")
}

func newAuthServiceWithOrganizationAndEnvironment(t *testing.T, provider *httptest.Server, users []string, organization, environment string) (*Service, *db.Store) {
	t.Helper()
	store, err := db.OpenStore(t.TempDir()+"/probe.db", []byte("test-pepper"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	cfg := config.Config{
		Environment:        environment,
		PublicBaseURL:      "http://127.0.0.1:8080",
		GitHubClientID:     "client-id",
		GitHubClientSecret: "client-secret",
		GitHubRedirectURL:  "http://127.0.0.1:8080/auth/github/callback",
		GitHubAllowedUsers: users,
		GitHubAllowedOrg:   organization,
		SessionSecret:      "test-session-secret-that-is-long-enough",
	}
	return NewService(cfg, store, ProviderEndpoints{AuthorizeURL: provider.URL + "/login/oauth/authorize", TokenURL: provider.URL + "/login/oauth/access_token", UserURL: provider.URL + "/user", OrganizationsURL: provider.URL + "/user/orgs"}), store
}

func completeOAuth(t *testing.T, service *Service) *httptest.ResponseRecorder {
	t.Helper()
	start := httptest.NewRecorder()
	service.BeginOAuth(start, httptest.NewRequest(http.MethodGet, "/auth/github", nil))
	cookie := start.Result().Cookies()[0]
	query, _ := url.ParseQuery(mustParseURL(t, start.Result().Header.Get("Location")).RawQuery)
	req := httptest.NewRequest(http.MethodGet, "/auth/github/callback?code=valid&state="+url.QueryEscape(query.Get("state")), nil)
	req.AddCookie(cookie)
	response := httptest.NewRecorder()
	service.Callback(response, req)
	return response
}

func newFakeGitHub(t *testing.T, login string) *httptest.Server {
	return newFakeGitHubWithOrganizations(t, login, nil)
}

func newFakeGitHubWithOrganizations(t *testing.T, login string, organizations []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"access_token": "provider-secret"})
		case "/user":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"id": 42, "login": login})
		case "/user/orgs":
			w.Header().Set("Content-Type", "application/json")
			values := make([]map[string]string, 0, len(organizations))
			for _, organization := range organizations {
				values = append(values, map[string]string{"login": organization})
			}
			json.NewEncoder(w).Encode(values)
		default:
			http.NotFound(w, r)
		}
	}))
}

func securityTokenBytes(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}
