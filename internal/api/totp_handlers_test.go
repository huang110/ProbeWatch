package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/config"
	"github.com/probewatch/probewatch/internal/db"
)

// apiTestTOTPCode computes the 6-digit TOTP code for a Base32 secret,
// independently exercising the wire format the API relies on.
func apiTestTOTPCode(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatal(err)
	}
	var message [8]byte
	binary.BigEndian.PutUint64(message[:], uint64(at.Unix()/30))
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(message[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := (uint32(sum[offset])&0x7f)<<24 |
		(uint32(sum[offset+1])&0xff)<<16 |
		(uint32(sum[offset+2])&0xff)<<8 |
		uint32(sum[offset+3])&0xff
	return fmt.Sprintf("%06d", value%1_000_000)
}

type totpTestEnv struct {
	handler http.Handler
	store   *db.Store
	session string
	adminID string
}

func newTOTPServer(t *testing.T) totpTestEnv {
	t.Helper()
	provider := newServerFakeGitHub(t, "alice", nil)
	service, store := newServerAuth(t, provider, []string{"alice"}, "")
	t.Cleanup(func() { store.Close() })
	handler := NewServer(config.Config{Environment: "development", PublicBaseURL: "http://127.0.0.1:8080", MaxRequestBody: 1 << 20}, service).Handler()
	admin, err := store.UpsertAdminUser(context.Background(), "github", "provider-42", "alice", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	session, err := service.CreateSessionForUser(context.Background(), admin.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return totpTestEnv{handler: handler, store: store, session: session, adminID: admin.ID}
}

func serverCSRFToken(t *testing.T, handler http.Handler, sessionCookie *http.Cookie) string {
	t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/csrf", nil)
	request.AddCookie(sessionCookie)
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("csrf status = %d, want 200", response.Code)
	}
	var payload struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || payload.Token == "" {
		t.Fatalf("csrf payload = %q", response.Body.String())
	}
	return payload.Token
}

func TestTOTPSetupRequiresAuthentication(t *testing.T) {
	env := newTOTPServer(t)
	handler := env.handler
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/totp/setup", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated setup status = %d, want 401", response.Code)
	}
	for _, path := range []string{"/api/totp/enable", "/api/totp/disable"} {
		write := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080"+path, strings.NewReader(`{"code":"123456"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "http://127.0.0.1:8080")
		handler.ServeHTTP(write, request)
		if write.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated %s status = %d, want 401", path, write.Code)
		}
	}
}

func TestTOTPEnableDisableFlowWithCSRFAndOrigin(t *testing.T) {
	env := newTOTPServer(t)
	handler := env.handler
	sessionCookie := &http.Cookie{Name: "probewatch_session", Value: env.session}

	setup := httptest.NewRecorder()
	setupRequest := httptest.NewRequest(http.MethodGet, "/api/totp/setup", nil)
	setupRequest.AddCookie(sessionCookie)
	handler.ServeHTTP(setup, setupRequest)
	if setup.Code != http.StatusOK {
		t.Fatalf("setup status = %d, want 200", setup.Code)
	}
	var enrollment struct {
		Enabled    bool   `json:"enabled"`
		Secret     string `json:"secret"`
		OTPAuthURL string `json:"otpauth_url"`
	}
	if err := json.Unmarshal(setup.Body.Bytes(), &enrollment); err != nil {
		t.Fatal(err)
	}
	if enrollment.Enabled || enrollment.Secret == "" || !strings.HasPrefix(enrollment.OTPAuthURL, "otpauth://totp/") {
		t.Fatalf("setup enrollment = %#v", enrollment)
	}
	if !strings.Contains(enrollment.OTPAuthURL, "secret="+enrollment.Secret) {
		t.Fatalf("otpauth URL %q does not carry the secret", enrollment.OTPAuthURL)
	}

	csrfToken := serverCSRFToken(t, handler, sessionCookie)
	// A request that reaches a handler consumes and rotates the CSRF token
	// even when the handler itself rejects it, so fetch a fresh token per
	// attempt from here on.
	freshCSRF := func() string { return serverCSRFToken(t, handler, sessionCookie) }

	postTOTP := func(path, code, csrf, origin string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080"+path, strings.NewReader(`{"code":"`+code+`"}`))
		request.Header.Set("Content-Type", "application/json")
		if origin != "" {
			request.Header.Set("Origin", origin)
		}
		if csrf != "" {
			request.Header.Set("X-CSRF-Token", csrf)
		}
		request.AddCookie(sessionCookie)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}

	if got := postTOTP("/api/totp/enable", apiTestTOTPCode(t, enrollment.Secret, time.Now()), "", "http://127.0.0.1:8080").Code; got != http.StatusForbidden {
		t.Fatalf("enable without CSRF token status = %d, want 403", got)
	}
	if got := postTOTP("/api/totp/enable", apiTestTOTPCode(t, enrollment.Secret, time.Now()), csrfToken, "").Code; got != http.StatusForbidden {
		t.Fatalf("enable without Origin status = %d, want 403", got)
	}
	if got := postTOTP("/api/totp/enable", apiTestTOTPCode(t, enrollment.Secret, time.Now()), csrfToken, "https://evil.example").Code; got != http.StatusForbidden {
		t.Fatalf("enable with cross-origin Origin status = %d, want 403", got)
	}
	invalid := postTOTP("/api/totp/enable", "000000", freshCSRF(), "http://127.0.0.1:8080")
	if invalid.Code != http.StatusForbidden {
		t.Fatalf("enable with an invalid code status = %d, want 403", invalid.Code)
	}
	var state struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(invalid.Body.Bytes(), &state); err != nil || state.Enabled {
		t.Fatalf("invalid-code response = %q", invalid.Body.String())
	}

	enabledResponse := postTOTP("/api/totp/enable", apiTestTOTPCode(t, enrollment.Secret, time.Now()), freshCSRF(), "http://127.0.0.1:8080")
	if enabledResponse.Code != http.StatusOK {
		t.Fatalf("enable with a valid code status = %d, want 200", enabledResponse.Code)
	}
	if err := json.Unmarshal(enabledResponse.Body.Bytes(), &state); err != nil || !state.Enabled {
		t.Fatalf("enable response = %q", enabledResponse.Body.String())
	}

	afterEnable := httptest.NewRecorder()
	afterEnableRequest := httptest.NewRequest(http.MethodGet, "/api/totp/setup", nil)
	afterEnableRequest.AddCookie(sessionCookie)
	handler.ServeHTTP(afterEnable, afterEnableRequest)
	if afterEnable.Code != http.StatusOK || strings.Contains(afterEnable.Body.String(), "secret") {
		t.Fatalf("enabled setup response leaks enrollment material: %d %q", afterEnable.Code, afterEnable.Body.String())
	}

	disabledResponse := postTOTP("/api/totp/disable", apiTestTOTPCode(t, enrollment.Secret, time.Now()), freshCSRF(), "http://127.0.0.1:8080")
	if disabledResponse.Code != http.StatusOK {
		t.Fatalf("disable with a valid code status = %d, want 200", disabledResponse.Code)
	}
	if err := json.Unmarshal(disabledResponse.Body.Bytes(), &state); err != nil || state.Enabled {
		t.Fatalf("disable response = %q", disabledResponse.Body.String())
	}
	if got := postTOTP("/api/totp/disable", "123456", freshCSRF(), "http://127.0.0.1:8080").Code; got != http.StatusConflict {
		t.Fatalf("disable when not enabled status = %d, want 409", got)
	}
	// After disable the secret is cleared, so enable without setup conflicts.
	if got := postTOTP("/api/totp/enable", "123456", freshCSRF(), "http://127.0.0.1:8080").Code; got != http.StatusConflict {
		t.Fatalf("enable without setup status = %d, want 409", got)
	}
	if got := postTOTP("/api/totp/setup", "", csrfToken, "http://127.0.0.1:8080").Code; got != http.StatusMethodNotAllowed {
		t.Fatalf("POST setup status = %d, want 405", got)
	}
}

func TestOAuthCallbackEnforcesTwoFactorBeforeSession(t *testing.T) {
	env := newTOTPServer(t)
	handler := env.handler
	secret := "JBSWY3DPEHPK3PXP"
	// The fake GitHub provider identifies "alice" as provider user 42; that
	// is the admin row the OAuth callback resolves, so enroll it directly.
	admin, err := env.store.UpsertAdminUser(context.Background(), "github", "42", "alice", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := env.store.SetAdminUserTOTP(context.Background(), admin.ID, []byte(secret), true, time.Now()); err != nil {
		t.Fatal(err)
	}

	start := httptest.NewRecorder()
	handler.ServeHTTP(start, httptest.NewRequest(http.MethodGet, "/auth/github", nil))
	if start.Code != http.StatusFound {
		t.Fatalf("OAuth start status = %d, want 302", start.Code)
	}
	stateCookie := findCookie(start.Result().Cookies(), "probewatch_oauth_state")
	started, err := url.Parse(start.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}

	callback := httptest.NewRecorder()
	callbackRequest := httptest.NewRequest(http.MethodGet, "/auth/github/callback?code=valid&state="+url.QueryEscape(started.Query().Get("state")), nil)
	callbackRequest.AddCookie(stateCookie)
	handler.ServeHTTP(callback, callbackRequest)
	if callback.Code != http.StatusFound || callback.Header().Get("Location") != "/login/2fa" {
		t.Fatalf("two-factor callback = %d %q, want 302 /login/2fa", callback.Code, callback.Header().Get("Location"))
	}
	pendingCookie := findCookie(callback.Result().Cookies(), "probewatch_totp_pending")
	if pendingCookie == nil || pendingCookie.Value == "" {
		t.Fatal("two-factor callback did not set the pending cookie")
	}
	if findCookie(callback.Result().Cookies(), "probewatch_session") != nil {
		t.Fatal("two-factor callback issued a session cookie")
	}

	me := httptest.NewRecorder()
	handler.ServeHTTP(me, httptest.NewRequest(http.MethodGet, "/api/me", nil))
	if me.Code != http.StatusUnauthorized {
		t.Fatalf("/api/me before verification status = %d, want 401", me.Code)
	}

	postVerify := func(cookies []*http.Cookie, code, origin string, method string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, "http://127.0.0.1:8080/auth/totp/verify", strings.NewReader(`{"code":"`+code+`"}`))
		request.Header.Set("Content-Type", "application/json")
		if origin != "" {
			request.Header.Set("Origin", origin)
		}
		for _, cookie := range cookies {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}

	if got := postVerify(nil, "123456", "http://127.0.0.1:8080", http.MethodPost).Code; got != http.StatusUnauthorized {
		t.Fatalf("verify without pending cookie status = %d, want 401", got)
	}
	if got := postVerify([]*http.Cookie{pendingCookie}, "123456", "http://127.0.0.1:8080", http.MethodGet).Code; got != http.StatusMethodNotAllowed {
		t.Fatalf("GET verify status = %d, want 405", got)
	}
	if got := postVerify([]*http.Cookie{pendingCookie}, "123456", "https://evil.example", http.MethodPost).Code; got != http.StatusForbidden {
		t.Fatalf("cross-origin verify status = %d, want 403", got)
	}
	if got := postVerify([]*http.Cookie{pendingCookie}, "000000", "http://127.0.0.1:8080", http.MethodPost).Code; got != http.StatusForbidden {
		t.Fatalf("verify with a wrong code status = %d, want 403", got)
	}

	verified := postVerify([]*http.Cookie{pendingCookie}, apiTestTOTPCode(t, secret, time.Now()), "http://127.0.0.1:8080", http.MethodPost)
	if verified.Code != http.StatusOK {
		t.Fatalf("verify with a valid code status = %d, want 200", verified.Code)
	}
	sessionCookie := findCookie(verified.Result().Cookies(), "probewatch_session")
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatal("successful verify did not issue a session cookie")
	}

	meAfter := httptest.NewRecorder()
	meAfterRequest := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	meAfterRequest.AddCookie(sessionCookie)
	handler.ServeHTTP(meAfter, meAfterRequest)
	if meAfter.Code != http.StatusOK || !strings.Contains(meAfter.Body.String(), `"login":"alice"`) {
		t.Fatalf("/api/me after verification = %d %q", meAfter.Code, meAfter.Body.String())
	}
	replay := postVerify([]*http.Cookie{pendingCookie}, apiTestTOTPCode(t, secret, time.Now()), "http://127.0.0.1:8080", http.MethodPost)
	if replay.Code != http.StatusUnauthorized {
		t.Fatalf("replayed pending credential status = %d, want 401", replay.Code)
	}
}
