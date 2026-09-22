package auth

import (
	"context"
	"encoding/base32"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/db"
)

// rfc6238Secret is the ASCII secret used by the RFC 6238 test vectors
// ("12345678901234567890" repeated to 20 bytes for SHA1).
const rfc6238Secret = "12345678901234567890"

func TestTOTPMatchesRFC6238SHA1Vectors(t *testing.T) {
	secret := []byte(rfc6238Secret)
	vectors := []struct {
		unix int64
		code string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
		{20000000000, "353130"},
	}
	for _, vector := range vectors {
		if got := hotp(secret, uint64(vector.unix)/totpStepSeconds); got != vector.code {
			t.Fatalf("hotp(T=%d) = %q, want %q", vector.unix, got, vector.code)
		}
	}
}

func TestGenerateTOTPSecretYields20RandomBytes(t *testing.T) {
	first, err := generateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	second, err := generateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeTOTPSecret(first)
	if err != nil {
		t.Fatalf("generated secret is not valid Base32: %v", err)
	}
	if len(decoded) != totpSecretBytes {
		t.Fatalf("secret entropy bytes = %d, want %d", len(decoded), totpSecretBytes)
	}
	if first == second {
		t.Fatal("two generated secrets are identical")
	}
	if _, err := decodeTOTPSecret(strings.ToLower(first)); err != nil {
		t.Fatalf("lowercase Base32 secret rejected: %v", err)
	}
	if _, err := decodeTOTPSecret(""); err == nil {
		t.Fatal("empty secret accepted")
	}
}

func TestVerifyTOTPCodeAcceptsAdjacentWindowsAndRejectsOutside(t *testing.T) {
	secret := []byte(rfc6238Secret)
	now := time.Unix(1234567890, 0)
	current := hotp(secret, uint64(now.Unix()/totpStepSeconds))
	previous := hotp(secret, uint64(now.Unix()/totpStepSeconds)-1)
	next := hotp(secret, uint64(now.Unix()/totpStepSeconds)+1)
	twoAgo := hotp(secret, uint64(now.Unix()/totpStepSeconds)-2)

	if !verifyTOTPCode(secret, current, now) {
		t.Fatal("current-window code rejected")
	}
	if !verifyTOTPCode(secret, previous, now) {
		t.Fatal("previous-window code rejected")
	}
	if !verifyTOTPCode(secret, next, now) {
		t.Fatal("next-window code rejected")
	}
	if verifyTOTPCode(secret, twoAgo, now) {
		t.Fatal("code from two windows ago accepted")
	}
	if verifyTOTPCode(secret, "000000", now) && current != "000000" {
		t.Fatal("wrong code accepted")
	}
	other := []byte("abcdefghijklmnopqrst")
	if verifyTOTPCode(other, current, now) && hotp(other, uint64(now.Unix()/totpStepSeconds)) != current {
		t.Fatal("code from a different secret accepted")
	}
}

func TestVerifyTOTPCodeRejectsMalformedInput(t *testing.T) {
	secret := []byte(rfc6238Secret)
	for _, code := range []string{"", "12345", "1234567", "12a456", "abcdef", "12 4a56", "-12345"} {
		if verifyTOTPCode(secret, code, time.Unix(1234567890, 0)) {
			t.Fatalf("malformed code %q accepted", code)
		}
	}
	if !verifyTOTPCode(secret, "005 924", time.Unix(1234567890, 0)) {
		t.Fatal("grouped paste of a valid code rejected")
	}
}

func TestTOTPAuthURLShape(t *testing.T) {
	secret, err := generateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	otpauth := totpAuthURL(secret, "alice")
	if !strings.HasPrefix(otpauth, "otpauth://totp/ProbeWatch:alice?") {
		t.Fatalf("otpauth URL = %q", otpauth)
	}
	for _, want := range []string{"secret=" + secret, "issuer=ProbeWatch", "algorithm=SHA1", "digits=6", "period=30"} {
		if !strings.Contains(otpauth, want) {
			t.Fatalf("otpauth URL %q missing %q", otpauth, want)
		}
	}
}

func totpCodeAt(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatal(err)
	}
	return hotp(key, uint64(at.Unix()/totpStepSeconds))
}

func TestSetupEnableDisableTOTPFlow(t *testing.T) {
	provider := newFakeGitHub(t, "alice")
	defer provider.Close()
	service, store := newAuthService(t, provider, []string{"alice"})
	admin, err := store.UpsertAdminUser(context.Background(), "github", "totp-flow", "alice", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()

	if err := service.DisableTOTP(context.Background(), admin.ID, "123456", now); err == nil {
		t.Fatal("disable without enrollment unexpectedly succeeded")
	}

	secret, otpauth, err := service.SetupTOTP(context.Background(), admin.ID, "alice", now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(otpauth, secret) {
		t.Fatalf("otpauth URL does not carry the secret: %q", otpauth)
	}
	storedSecret, enabled, err := store.GetAdminUserTOTP(context.Background(), admin.ID)
	if err != nil || enabled || string(storedSecret) != secret {
		t.Fatalf("setup did not persist the pending secret: enabled=%v err=%v", enabled, err)
	}

	if err := service.EnableTOTP(context.Background(), admin.ID, "000000", now); err == nil {
		t.Fatal("enable accepted an invalid code")
	}
	if _, enabled, _ := store.GetAdminUserTOTP(context.Background(), admin.ID); enabled {
		t.Fatal("invalid code enabled two-factor")
	}
	if err := service.EnableTOTP(context.Background(), admin.ID, totpCodeAt(t, secret, now), now); err != nil {
		t.Fatalf("enable with a valid code failed: %v", err)
	}
	if _, enabled, _ := store.GetAdminUserTOTP(context.Background(), admin.ID); !enabled {
		t.Fatal("two-factor not enabled after a valid code")
	}
	if err := service.EnableTOTP(context.Background(), admin.ID, totpCodeAt(t, secret, now), now); err == nil {
		t.Fatal("double enable unexpectedly succeeded")
	}

	if err := service.DisableTOTP(context.Background(), admin.ID, "999999", now.Add(time.Second)); err == nil {
		t.Fatal("disable accepted an invalid code")
	}
	if _, enabled, _ := store.GetAdminUserTOTP(context.Background(), admin.ID); !enabled {
		t.Fatal("invalid code disabled two-factor")
	}
	if err := service.DisableTOTP(context.Background(), admin.ID, totpCodeAt(t, secret, now.Add(time.Second)), now.Add(time.Second)); err != nil {
		t.Fatalf("disable with a valid code failed: %v", err)
	}
	storedSecret, enabled, err = store.GetAdminUserTOTP(context.Background(), admin.ID)
	if err != nil || enabled || len(storedSecret) != 0 {
		t.Fatalf("disable did not clear the secret: enabled=%v len=%d err=%v", enabled, len(storedSecret), err)
	}
	if err := service.DisableTOTP(context.Background(), admin.ID, totpCodeAt(t, secret, now), now); err == nil {
		t.Fatal("double disable unexpectedly succeeded")
	}
}

func enableTOTPForAdmin(t *testing.T, service *Service, adminID string) string {
	t.Helper()
	now := time.Now().UTC()
	secret, _, err := service.SetupTOTP(context.Background(), adminID, "alice", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.EnableTOTP(context.Background(), adminID, totpCodeAt(t, secret, now), now); err != nil {
		t.Fatal(err)
	}
	return secret
}

func TestCallbackRequiresTwoFactorAndVerifyIssuesSession(t *testing.T) {
	provider := newFakeGitHub(t, "alice")
	defer provider.Close()
	service, store := newAuthService(t, provider, []string{"alice"})
	// The fake GitHub provider identifies "alice" as provider user 42; the
	// callback will resolve and upsert exactly this admin row.
	admin, err := store.UpsertAdminUser(context.Background(), "github", "42", "alice", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	secret := enableTOTPForAdmin(t, service, admin.ID)

	start := httptest.NewRecorder()
	service.BeginOAuth(start, httptest.NewRequest(http.MethodGet, "/auth/github", nil))
	stateCookie := findCookie(start.Result().Cookies(), oauthStateCookieName)
	query, _ := url.ParseQuery(mustParseURL(t, start.Result().Header.Get("Location")).RawQuery)
	callback := httptest.NewRecorder()
	callbackRequest := httptest.NewRequest(http.MethodGet, "/auth/github/callback?code=valid&state="+query.Get("state"), nil)
	callbackRequest.AddCookie(stateCookie)
	service.Callback(callback, callbackRequest)

	if callback.Code != http.StatusFound {
		t.Fatalf("two-factor callback status = %d, want 302", callback.Code)
	}
	if location := callback.Header().Get("Location"); location != "/login/2fa" {
		t.Fatalf("two-factor callback redirect = %q, want /login/2fa", location)
	}
	pendingCookie := findCookie(callback.Result().Cookies(), totpPendingCookieName)
	if pendingCookie == nil || pendingCookie.Value == "" {
		t.Fatal("two-factor callback did not set the pending cookie")
	}
	if !pendingCookie.HttpOnly || pendingCookie.SameSite != http.SameSiteLaxMode || pendingCookie.MaxAge != int(totpPendingLifetime/time.Second) {
		t.Fatalf("pending cookie flags = %#v", pendingCookie)
	}
	if findCookie(callback.Result().Cookies(), sessionCookieName) != nil {
		t.Fatal("two-factor callback issued a session cookie before verification")
	}

	code := totpCodeAt(t, secret, time.Now())
	wrongCode := verifyRequest(t, service, pendingCookie.Value, "000000")
	if wrongCode.Code != http.StatusForbidden {
		t.Fatalf("verify with a wrong code status = %d, want 403", wrongCode.Code)
	}
	retry := verifyRequest(t, service, pendingCookie.Value, code)
	if retry.Code != http.StatusOK {
		t.Fatalf("verify with a valid code after a wrong attempt status = %d, want 200", retry.Code)
	}
	sessionCookie := findCookie(retry.Result().Cookies(), sessionCookieName)
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatal("successful verify did not issue a session cookie")
	}
	clearedPending := findCookie(retry.Result().Cookies(), totpPendingCookieName)
	if clearedPending == nil || clearedPending.MaxAge >= 0 {
		t.Fatal("successful verify did not clear the pending cookie")
	}
	authRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	authRequest.AddCookie(sessionCookie)
	if _, ok := service.Authenticate(authRequest, time.Now().Add(time.Minute)); !ok {
		t.Fatal("session issued by two-factor verification does not authenticate")
	}
	replay := verifyRequest(t, service, pendingCookie.Value, code)
	if replay.Code != http.StatusUnauthorized {
		t.Fatalf("replayed pending credential status = %d, want 401", replay.Code)
	}
}

func TestVerifyTOTPLoginRejectsWrongMethodOriginAndMissingPending(t *testing.T) {
	provider := newFakeGitHub(t, "alice")
	defer provider.Close()
	service, store := newAuthService(t, provider, []string{"alice"})
	admin, err := store.UpsertAdminUser(context.Background(), "github", "totp-guard", "alice", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	enableTOTPForAdmin(t, service, admin.ID)

	get := httptest.NewRequest(http.MethodGet, "/auth/totp/verify", nil)
	get.Header.Set("Origin", "http://127.0.0.1:8080")
	getResponse := httptest.NewRecorder()
	service.VerifyTOTPLogin(getResponse, get)
	if getResponse.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET verify status = %d, want 405", getResponse.Code)
	}

	crossOrigin := httptest.NewRequest(http.MethodPost, "/auth/totp/verify", strings.NewReader(`{"code":"123456"}`))
	crossOrigin.Header.Set("Content-Type", "application/json")
	crossOrigin.Header.Set("Origin", "https://evil.example")
	crossOriginResponse := httptest.NewRecorder()
	service.VerifyTOTPLogin(crossOriginResponse, crossOrigin)
	if crossOriginResponse.Code != http.StatusForbidden {
		t.Fatalf("cross-origin verify status = %d, want 403", crossOriginResponse.Code)
	}

	missingPending := httptest.NewRequest(http.MethodPost, "/auth/totp/verify", strings.NewReader(`{"code":"123456"}`))
	missingPending.Header.Set("Content-Type", "application/json")
	missingPending.Header.Set("Origin", "http://127.0.0.1:8080")
	missingPendingResponse := httptest.NewRecorder()
	service.VerifyTOTPLogin(missingPendingResponse, missingPending)
	if missingPendingResponse.Code != http.StatusUnauthorized {
		t.Fatalf("verify without pending cookie status = %d, want 401", missingPendingResponse.Code)
	}

	expired, err := store.CreateTOTPPendingState(context.Background(), admin.ID, time.Now().Add(-totpPendingLifetime-time.Second), totpPendingLifetime)
	if err != nil {
		t.Fatal(err)
	}
	expiredResponse := verifyRequest(t, service, expired, "123456")
	if expiredResponse.Code != http.StatusUnauthorized {
		t.Fatalf("verify with expired pending credential status = %d, want 401", expiredResponse.Code)
	}

	if _, err := store.GetTOTPPendingState(context.Background(), "unknown-token", time.Now()); err != db.ErrTOTPPendingInvalid {
		t.Fatalf("unknown pending credential error = %v, want ErrTOTPPendingInvalid", err)
	}
}

func verifyRequest(t *testing.T, service *Service, pending, code string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/auth/totp/verify", strings.NewReader(mustJSON(t, map[string]string{"code": code})))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://127.0.0.1:8080")
	request.AddCookie(&http.Cookie{Name: totpPendingCookieName, Value: pending})
	response := httptest.NewRecorder()
	service.VerifyTOTPLogin(response, request)
	return response
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
