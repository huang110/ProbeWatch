package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"database/sql"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/db"
)

const (
	// totpSecretBytes is the RFC 4226 recommended secret length (160 bits).
	totpSecretBytes = 20
	// totpStepSeconds is the RFC 6238 time step X in seconds.
	totpStepSeconds = 30
	// totpDigits is the code length produced and accepted.
	totpDigits = 6
	// totpWindow is how many steps on either side of the current one are
	// accepted to tolerate clock drift.
	totpWindow = 1
	// totpIssuerName labels the otpauth:// enrollment URL.
	totpIssuerName = "ProbeWatch"

	// totpPendingCookieName carries the short-lived intermediate credential
	// between the OAuth callback and the /login/2fa verification page.
	totpPendingCookieName = "probewatch_totp_pending"
	totpPendingLifetime   = 5 * time.Minute
	maxTOTPVerifyBody     = 4 * 1024
)

var (
	ErrTOTPCodeInvalid    = errors.New("totp code invalid")
	ErrTOTPSetupRequired  = errors.New("totp setup required")
	ErrTOTPAlreadyEnabled = errors.New("totp already enabled")
	ErrTOTPNotEnabled     = errors.New("totp not enabled")
)

// generateTOTPSecret returns a 20-byte crypto/rand secret encoded as
// unpadded Base32, the form authenticator apps expect.
func generateTOTPSecret() (string, error) {
	raw := make([]byte, totpSecretBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate totp secret: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw), nil
}

// decodeTOTPSecret accepts padded or unpadded Base32 with arbitrary spacing.
func decodeTOTPSecret(secret string) ([]byte, error) {
	normalized := strings.ToUpper(strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '=' {
			return -1
		}
		return r
	}, strings.TrimSpace(secret)))
	if normalized == "" {
		return nil, errors.New("totp secret empty")
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(normalized)
}

// hotp implements the RFC 4226 HMAC-SHA1 construction with dynamic truncation.
func hotp(secret []byte, counter uint64) string {
	var message [8]byte
	binary.BigEndian.PutUint64(message[:], counter)
	mac := hmac.New(sha1.New, secret)
	_, _ = mac.Write(message[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := (uint32(sum[offset])&0x7f)<<24 |
		(uint32(sum[offset+1])&0xff)<<16 |
		(uint32(sum[offset+2])&0xff)<<8 |
		uint32(sum[offset+3])&0xff
	return fmt.Sprintf("%0*d", totpDigits, value%1_000_000)
}

// verifyTOTPCode checks a 6-digit code against the current 30-second step and
// one step on either side. Every candidate is compared with a constant-time
// comparison and the window is always fully evaluated so timing does not
// reveal which step matched.
func verifyTOTPCode(secret []byte, code string, now time.Time) bool {
	normalized := NormalizeTOTPCode(code)
	if len(normalized) != totpDigits || len(secret) == 0 {
		return false
	}
	for _, r := range normalized {
		if r < '0' || r > '9' {
			return false
		}
	}
	counter := now.Unix() / totpStepSeconds
	matched := 0
	for delta := int64(-totpWindow); delta <= totpWindow; delta++ {
		candidate := hotp(secret, uint64(counter+delta))
		if subtle.ConstantTimeCompare([]byte(normalized), []byte(candidate)) == 1 {
			matched = 1
		}
	}
	return matched == 1
}

// NormalizeTOTPCode strips surrounding and inner whitespace so users can
// paste codes grouped as "123 456".
func NormalizeTOTPCode(code string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' {
			return -1
		}
		return r
	}, strings.TrimSpace(code))
}

func totpAuthURL(secret, accountName string) string {
	query := url.Values{}
	query.Set("secret", secret)
	query.Set("issuer", totpIssuerName)
	query.Set("algorithm", "SHA1")
	query.Set("digits", fmt.Sprint(totpDigits))
	query.Set("period", fmt.Sprint(totpStepSeconds))
	return "otpauth://totp/" + url.PathEscape(totpIssuerName) + ":" + url.PathEscape(accountName) + "?" + query.Encode()
}

// SetupTOTP generates a fresh TOTP secret for the administrator and stores it
// in the disabled state. The enrollment URL and secret are returned once;
// calling setup again replaces any not-yet-confirmed secret.
func (s *Service) SetupTOTP(ctx context.Context, adminUserID, login string, now time.Time) (string, string, error) {
	secret, err := generateTOTPSecret()
	if err != nil {
		return "", "", err
	}
	if err := s.store.SetAdminUserTOTP(ctx, adminUserID, []byte(secret), false, now); err != nil {
		return "", "", err
	}
	return secret, totpAuthURL(secret, login), nil
}

// EnableTOTP confirms enrollment: the presented code must verify against the
// stored pending secret before the flag is persisted.
func (s *Service) EnableTOTP(ctx context.Context, adminUserID, code string, now time.Time) error {
	storedSecret, enabled, err := s.store.GetAdminUserTOTP(ctx, adminUserID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrTOTPSetupRequired
	}
	if err != nil {
		return err
	}
	if enabled {
		return ErrTOTPAlreadyEnabled
	}
	secret, err := decodeTOTPSecret(string(storedSecret))
	if err != nil || len(secret) != totpSecretBytes {
		return ErrTOTPSetupRequired
	}
	if !verifyTOTPCode(secret, code, now) {
		return ErrTOTPCodeInvalid
	}
	return s.store.SetAdminUserTOTP(ctx, adminUserID, storedSecret, true, now)
}

// DisableTOTP turns two-factor authentication off. The current code must
// verify so a hijacked session cannot silently drop the second factor.
func (s *Service) DisableTOTP(ctx context.Context, adminUserID, code string, now time.Time) error {
	storedSecret, enabled, err := s.store.GetAdminUserTOTP(ctx, adminUserID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrTOTPNotEnabled
	}
	if err != nil {
		return err
	}
	if !enabled {
		return ErrTOTPNotEnabled
	}
	secret, err := decodeTOTPSecret(string(storedSecret))
	if err != nil {
		return ErrTOTPCodeInvalid
	}
	if !verifyTOTPCode(secret, code, now) {
		return ErrTOTPCodeInvalid
	}
	return s.store.SetAdminUserTOTP(ctx, adminUserID, nil, false, now)
}

// startTOTPPendingLogin issues the intermediate credential for an
// administrator with two-factor enabled and points the browser at /login/2fa.
func (s *Service) startTOTPPendingLogin(w http.ResponseWriter, r *http.Request, adminID string) {
	now := time.Now().UTC()
	pending, err := s.store.CreateTOTPPendingState(r.Context(), adminID, now, totpPendingLifetime)
	if err != nil {
		logInternalError("create totp pending state", err)
		writeError(w, http.StatusInternalServerError, "authentication unavailable")
		return
	}
	setCookie(w, s.cfg, &http.Cookie{
		Name:     totpPendingCookieName,
		Value:    pending,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.Environment != "development",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(totpPendingLifetime / time.Second),
		Expires:  now.Add(totpPendingLifetime),
	})
	http.Redirect(w, r, "/login/2fa", http.StatusFound)
}

// VerifyTOTPLogin completes a two-factor login: POST /auth/totp/verify with
// the pending cookie and a 6-digit code. The pending credential is consumed
// only after the code verifies, so a mistyped code can be retried.
func (s *Service) VerifyTOTPLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.sameOriginRequest(r) {
		writeError(w, http.StatusForbidden, "same-origin request required")
		return
	}
	cookie, err := r.Cookie(totpPendingCookieName)
	if err != nil || cookie.Value == "" {
		writeError(w, http.StatusUnauthorized, "two-factor verification required")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxTOTPVerifyBody+1))
	if err != nil || len(body) > maxTOTPVerifyBody {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var payload struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(body, &payload) != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	now := time.Now().UTC()
	adminID, err := s.store.GetTOTPPendingState(r.Context(), cookie.Value, now)
	if err != nil {
		if errors.Is(err, db.ErrTOTPPendingInvalid) || errors.Is(err, db.ErrTOTPPendingExpired) || errors.Is(err, db.ErrTOTPPendingConsumed) {
			writeError(w, http.StatusUnauthorized, "two-factor verification required")
			return
		}
		logInternalError("get totp pending state", err)
		writeError(w, http.StatusInternalServerError, "authentication unavailable")
		return
	}
	storedSecret, enabled, err := s.store.GetAdminUserTOTP(r.Context(), adminID)
	if err != nil {
		logInternalError("get admin totp for login", err)
		writeError(w, http.StatusInternalServerError, "authentication unavailable")
		return
	}
	secret, decodeErr := decodeTOTPSecret(string(storedSecret))
	if !enabled || decodeErr != nil {
		writeError(w, http.StatusForbidden, "two-factor verification failed")
		return
	}
	if !verifyTOTPCode(secret, payload.Code, now) {
		writeError(w, http.StatusForbidden, "two-factor verification failed")
		return
	}
	if err := s.store.ConsumeTOTPPendingState(r.Context(), cookie.Value, now); err != nil {
		if errors.Is(err, db.ErrTOTPPendingInvalid) || errors.Is(err, db.ErrTOTPPendingExpired) || errors.Is(err, db.ErrTOTPPendingConsumed) {
			writeError(w, http.StatusUnauthorized, "two-factor verification required")
			return
		}
		logInternalError("consume totp pending state", err)
		writeError(w, http.StatusInternalServerError, "authentication unavailable")
		return
	}
	sessionValue, err := s.createSession(r.Context(), adminID, now)
	if err != nil {
		logInternalError("create session", err)
		writeError(w, http.StatusInternalServerError, "authentication unavailable")
		return
	}
	setCookie(w, s.cfg, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionValue,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.Environment != "development",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionLifetime / time.Second),
		Expires:  now.Add(sessionLifetime),
	})
	setCookie(w, s.cfg, &http.Cookie{Name: totpPendingCookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.cfg.Environment != "development", SameSite: http.SameSiteLaxMode})
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"status":"ok"}`)
}

// sameOriginRequest mirrors the API middleware's Origin enforcement for the
// cookie-authenticated verification POST.
func (s *Service) sameOriginRequest(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	want, err := url.Parse(s.cfg.PublicBaseURL)
	if err != nil || want.Scheme == "" || want.Host == "" {
		return false
	}
	have, err := url.Parse(origin)
	if err != nil || have.User != nil || want.User != nil || have.Scheme == "" || have.Host == "" || have.Path != "" || have.RawQuery != "" || have.Fragment != "" {
		return false
	}
	return sameURLOrigin(have, want)
}
