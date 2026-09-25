package auth

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/probewatch/probewatch/internal/config"
	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/security"
)

const (
	sessionCookieName    = "probewatch_session"
	oauthStateCookieName = "probewatch_oauth_state"
	oauthStateLifetime   = 10 * time.Minute
	sessionLifetime      = 24 * time.Hour
)

type ProviderEndpoints struct {
	AuthorizeURL              string
	TokenURL                  string
	UserURL                   string
	OrganizationsURL          string
	OrganizationMembershipURL string
}

type Service struct {
	cfg             config.Config
	storeMu         sync.RWMutex
	store           *db.Store
	provider        ProviderEndpoints
	client          *http.Client
	providerOrigins map[string]struct{}
	csrfRegistryMu  sync.Mutex
	csrfStates      map[string]*csrfState
}

func NewService(cfg config.Config, store *db.Store, provider ProviderEndpoints) *Service {
	providerOrigins := make(map[string]struct{})
	for _, endpoint := range []string{provider.TokenURL, provider.UserURL, provider.OrganizationsURL, provider.OrganizationMembershipURL} {
		if parsed, err := url.Parse(endpoint); err == nil && parsed.Scheme != "" && parsed.Host != "" {
			providerOrigins[originKey(parsed)] = struct{}{}
		}
	}
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{MaxResponseHeaderBytes: 32 << 10},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("provider redirect limit exceeded")
			}
			if _, ok := providerOrigins[originKey(req.URL)]; !ok {
				return errors.New("provider redirect changed origin")
			}
			return nil
		},
	}
	return &Service{cfg: cfg, store: store, provider: provider, providerOrigins: providerOrigins, client: client, csrfStates: make(map[string]*csrfState)}
}

// Store exposes the persistence boundary to the API layer without exposing it
// to HTTP callers or placing persistence logic in handlers.
func (s *Service) Store() *db.Store {
	s.storeMu.RLock()
	defer s.storeMu.RUnlock()
	return s.store
}

// SwapStore atomically replaces the active database store and returns the old store.
func (s *Service) SwapStore(newStore *db.Store) *db.Store {
	s.storeMu.Lock()
	defer s.storeMu.Unlock()
	old := s.store
	s.store = newStore
	return old
}

func (s *Service) BeginOAuth(w http.ResponseWriter, r *http.Request) {
	state, err := security.GenerateToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "authentication unavailable")
		return
	}
	now := time.Now().UTC()
	if _, err := s.Store().CreateOAuthState(r.Context(), security.Digest([]byte(s.cfg.SessionSecret), state), now.Add(oauthStateLifetime)); err != nil {
		logInternalError("create oauth state", err)
		writeError(w, http.StatusInternalServerError, "authentication unavailable")
		return
	}
	setCookie(w, s.cfg, &http.Cookie{
		Name:     oauthStateCookieName,
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.Environment != "development",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(oauthStateLifetime / time.Second),
		Expires:  now.Add(oauthStateLifetime),
	})

	query := url.Values{
		"client_id":     {s.cfg.GitHubClientID},
		"redirect_uri":  {s.cfg.GitHubRedirectURL},
		"response_type": {"code"},
		"scope":         {"read:user read:org"},
		"state":         {state},
	}
	redirect := s.provider.AuthorizeURL
	if parsed, err := url.Parse(redirect); err == nil {
		parsed.RawQuery = query.Encode()
		redirect = parsed.String()
	}
	http.Redirect(w, r, redirect, http.StatusFound)
}

func (s *Service) Callback(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(oauthStateCookieName)
	if err != nil || cookie.Value == "" || r.URL.Query().Get("state") == "" || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(r.URL.Query().Get("state"))) != 1 {
		writeError(w, http.StatusForbidden, "authentication failed")
		return
	}
	if err := s.Store().ConsumeOAuthState(r.Context(), security.Digest([]byte(s.cfg.SessionSecret), cookie.Value), time.Now().UTC()); err != nil {
		if errors.Is(err, db.ErrOAuthStateConsumed) || errors.Is(err, db.ErrOAuthStateExpired) || errors.Is(err, db.ErrOAuthStateInvalid) {
			writeError(w, http.StatusForbidden, "authentication failed")
			return
		}
		logInternalError("consume oauth state", err)
		writeError(w, http.StatusInternalServerError, "authentication unavailable")
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		writeError(w, http.StatusForbidden, "authentication failed")
		return
	}
	accessToken, err := s.exchangeCode(r.Context(), code)
	if err != nil {
		logInternalError("exchange oauth code", err)
		writeError(w, http.StatusInternalServerError, "authentication unavailable")
		return
	}
	providerUser, err := s.fetchUser(r.Context(), accessToken)
	if err != nil {
		logInternalError("fetch provider user", err)
		writeError(w, http.StatusInternalServerError, "authentication unavailable")
		return
	}
	allowed, err := s.allowed(r.Context(), accessToken, providerUser.Login)
	if err != nil {
		logInternalError("check provider authorization", err)
		writeError(w, http.StatusInternalServerError, "authentication unavailable")
		return
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "authentication failed")
		return
	}
	admin, err := s.Store().UpsertAdminUser(r.Context(), "github", providerUser.ID, providerUser.Login, time.Now().UTC())
	if err != nil {
		logInternalError("upsert admin user", err)
		writeError(w, http.StatusInternalServerError, "authentication unavailable")
		return
	}
	totpSecret, totpEnabled, err := s.Store().GetAdminUserTOTP(r.Context(), admin.ID)
	if err != nil {
		logInternalError("read admin totp state", err)
		writeError(w, http.StatusInternalServerError, "authentication unavailable")
		return
	}
	if totpEnabled && len(totpSecret) > 0 {
		// Two-factor administrators do not receive a session here. A
		// short-lived pending credential is issued instead and consumed by
		// POST /auth/totp/verify after the code check on /login/2fa.
		s.startTOTPPendingLogin(w, r, admin.ID)
		return
	}
	sessionValue, err := s.createSession(r.Context(), admin.ID, time.Now().UTC())
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
		Expires:  time.Now().UTC().Add(sessionLifetime),
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Service) createSession(ctx context.Context, adminID string, now time.Time) (string, error) {
	value, err := security.GenerateToken()
	if err != nil {
		return "", err
	}
	id, err := security.GenerateToken()
	if err != nil {
		return "", err
	}
	if err := s.Store().CreateSessionWithPolicy(ctx, id, []byte(value), adminID, now.Add(sessionLifetime), now, s.policyDigest()); err != nil {
		return "", err
	}
	return value, nil
}

func (s *Service) CreateSessionForUser(ctx context.Context, adminID string, now time.Time) (string, error) {
	value, err := s.createSession(ctx, adminID, now)
	if err != nil {
		return "", err
	}
	return value, nil
}

func (s *Service) Authenticate(r *http.Request, now time.Time) (db.Session, bool) {
	session, err := s.AuthenticateWithError(r, now)
	return session, err == nil
}

func (s *Service) AuthenticateWithError(r *http.Request, now time.Time) (db.Session, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return db.Session{}, db.ErrSessionNotFound
	}
	session, err := s.Store().GetSessionWithPolicy(r.Context(), []byte(cookie.Value), now, s.policyDigest())
	if err != nil {
		if !errors.Is(err, db.ErrSessionNotFound) && !errors.Is(err, db.ErrSessionExpired) && !errors.Is(err, db.ErrSessionPolicyChanged) {
			logInternalError("authenticate session", err)
		}
		if errors.Is(err, db.ErrSessionPolicyChanged) {
			_ = s.Store().DeleteSession(r.Context(), []byte(cookie.Value))
		}
		s.dropCSRFState(cookie.Value)
		return db.Session{}, err
	}
	return session, nil
}

func (s *Service) Logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		if err := s.Store().DeleteSession(r.Context(), []byte(cookie.Value)); err != nil {
			logInternalError("delete session", err)
			writeError(w, http.StatusInternalServerError, "logout unavailable")
			return
		}
		s.dropCSRFState(cookie.Value)
	}
	setCookie(w, s.cfg, &http.Cookie{Name: sessionCookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.cfg.Environment != "development", SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Service) AuthenticateLocalPassword(w http.ResponseWriter, r *http.Request, password string) error {
	trimmedAdminPassword := strings.TrimSpace(s.cfg.AdminPassword)
	if trimmedAdminPassword == "" {
		return errors.New("local password login is not configured")
	}
	if subtle.ConstantTimeCompare([]byte(password), []byte(trimmedAdminPassword)) != 1 {
		return errors.New("invalid credentials")
	}
	admin, err := s.Store().UpsertAdminUser(r.Context(), "local", "admin", "admin", time.Now().UTC())
	if err != nil {
		logInternalError("upsert local admin user", err)
		return err
	}
	totpSecret, totpEnabled, err := s.Store().GetAdminUserTOTP(r.Context(), admin.ID)
	if err != nil {
		logInternalError("read admin totp state", err)
		return err
	}
	if totpEnabled && len(totpSecret) > 0 {
		s.startTOTPPendingLogin(w, r, admin.ID)
		return nil
	}
	sessionValue, err := s.createSession(r.Context(), admin.ID, time.Now().UTC())
	if err != nil {
		logInternalError("create session", err)
		return err
	}
	setCookie(w, s.cfg, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionValue,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.Environment != "development",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionLifetime / time.Second),
		Expires:  time.Now().UTC().Add(sessionLifetime),
	})
	return nil
}

// IssueSessionCookie creates and sets an authenticated session cookie for the given admin user.
func (s *Service) IssueSessionCookie(w http.ResponseWriter, r *http.Request, adminID string) error {
	sessionValue, err := s.createSession(r.Context(), adminID, time.Now().UTC())
	if err != nil {
		logInternalError("create session", err)
		return err
	}
	setCookie(w, s.cfg, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionValue,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.Environment != "development",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionLifetime / time.Second),
		Expires:  time.Now().UTC().Add(sessionLifetime),
	})
	return nil
}

func (s *Service) CSRFHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if _, err := s.AuthenticateWithError(r, time.Now().UTC()); err != nil {
		if errors.Is(err, db.ErrSessionNotFound) || errors.Is(err, db.ErrSessionExpired) || errors.Is(err, db.ErrSessionPolicyChanged) {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		logInternalError("authenticate csrf session", err)
		writeError(w, http.StatusInternalServerError, "authentication unavailable")
		return
	}
	_, unlock := s.lockSessionCSRF(cookie.Value, true)
	defer func() {
		unlock()
	}()
	token, err := s.Store().RotateCSRF(r.Context(), []byte(cookie.Value), time.Now().UTC())
	if err != nil {
		if errors.Is(err, db.ErrSessionNotFound) {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		logInternalError("rotate csrf token", err)
		writeError(w, http.StatusInternalServerError, "csrf unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-CSRF-Token", token)
	_ = json.NewEncoder(w).Encode(map[string]string{"token": token})
}

func (s *Service) ClaimCSRF(r *http.Request, token string) (string, bool) {
	next, err := s.ClaimCSRFWithError(r, token)
	return next, err == nil
}

func (s *Service) ClaimCSRFWithError(r *http.Request, token string) (string, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" || token == "" {
		return "", db.ErrTokenInvalid
	}
	next, err := s.Store().ClaimCSRF(r.Context(), []byte(cookie.Value), token, time.Now().UTC())
	if err != nil && !errors.Is(err, db.ErrTokenInvalid) && !errors.Is(err, db.ErrSessionNotFound) {
		logInternalError("claim csrf token", err)
	}
	return next, err
}

type csrfState struct {
	mu      sync.Mutex
	pending atomic.Int32
	refs    int
}

func (s *Service) lockSessionCSRF(sessionValue string, pending bool) (*csrfState, func()) {
	s.csrfRegistryMu.Lock()
	state := s.csrfStates[sessionValue]
	if state == nil {
		state = &csrfState{}
		s.csrfStates[sessionValue] = state
	}
	state.refs++
	if pending {
		state.pending.Add(1)
	}
	s.csrfRegistryMu.Unlock()
	state.mu.Lock()
	return state, func() {
		state.mu.Unlock()
		if pending {
			state.pending.Add(-1)
		}
		s.csrfRegistryMu.Lock()
		state.refs--
		if state.refs == 0 && state.pending.Load() == 0 {
			delete(s.csrfStates, sessionValue)
		}
		s.csrfRegistryMu.Unlock()
	}
}

func (s *Service) LockSessionCSRF(r *http.Request) func() {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return func() {}
	}
	_, unlock := s.lockSessionCSRF(cookie.Value, false)
	return unlock
}

func (s *Service) CSRFRefreshPending(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return false
	}
	s.csrfRegistryMu.Lock()
	state := s.csrfStates[cookie.Value]
	s.csrfRegistryMu.Unlock()
	return state != nil && state.pending.Load() > 0
}

func (s *Service) dropCSRFState(sessionValue string) {
	s.csrfRegistryMu.Lock()
	defer s.csrfRegistryMu.Unlock()
	if state := s.csrfStates[sessionValue]; state != nil && state.refs == 0 && state.pending.Load() == 0 {
		delete(s.csrfStates, sessionValue)
	}
}

func (s *Service) csrfRegistrySize() int {
	s.csrfRegistryMu.Lock()
	defer s.csrfRegistryMu.Unlock()
	return len(s.csrfStates)
}

func (s *Service) CleanupExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	deleted, err := s.Store().CleanupExpiredSessions(ctx, now)
	s.csrfRegistryMu.Lock()
	for sessionValue, state := range s.csrfStates {
		if state.refs == 0 && state.pending.Load() == 0 {
			delete(s.csrfStates, sessionValue)
		}
	}
	s.csrfRegistryMu.Unlock()
	return deleted, err
}

func (s *Service) CleanupExpiredOAuthStates(ctx context.Context, now time.Time) (int64, error) {
	return s.Store().CleanupExpiredOAuthStates(ctx, now)
}

func (s *Service) CleanupExpiredTOTPPendingStates(ctx context.Context, now time.Time) (int64, error) {
	return s.Store().CleanupExpiredTOTPPendingStates(ctx, now)
}

func (s *Service) CurrentUser(r *http.Request, now time.Time) (db.AdminUser, error) {
	session, err := s.AuthenticateWithError(r, now)
	if err != nil {
		return db.AdminUser{}, err
	}
	user, err := s.Store().GetAdminUser(r.Context(), session.AdminUserID)
	if err != nil {
		logInternalError("get current admin user", err)
	}
	return user, err
}

type providerUser struct {
	ID    string
	Login string
}

func (s *Service) exchangeCode(ctx context.Context, code string) (string, error) {
	form := url.Values{"client_id": {s.cfg.GitHubClientID}, "client_secret": {s.cfg.GitHubClientSecret}, "code": {code}, "redirect_uri": {s.cfg.GitHubRedirectURL}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.provider.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", errors.New("provider token exchange failed")
	}
	body, err := readOAuthBody(response.Body)
	if err != nil {
		return "", err
	}
	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if json.Unmarshal(body, &payload) != nil || payload.AccessToken == "" {
		values, parseErr := url.ParseQuery(string(body))
		if parseErr != nil {
			return "", errors.New("provider token response invalid")
		}
		payload.AccessToken = values.Get("access_token")
	}
	if payload.AccessToken == "" {
		return "", errors.New("provider token response invalid")
	}
	return payload.AccessToken, nil
}

func (s *Service) fetchUser(ctx context.Context, accessToken string) (providerUser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.provider.UserURL, nil)
	if err != nil {
		return providerUser{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	response, err := s.client.Do(req)
	if err != nil {
		return providerUser{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return providerUser{}, errors.New("provider user lookup failed")
	}
	body, err := readOAuthBody(response.Body)
	if err != nil {
		return providerUser{}, err
	}
	var raw struct {
		ID    json.RawMessage `json:"id"`
		Login string          `json:"login"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return providerUser{}, err
	}
	var idString string
	if json.Unmarshal(raw.ID, &idString) != nil {
		var idNumber json.Number
		if err := json.Unmarshal(raw.ID, &idNumber); err != nil {
			return providerUser{}, errors.New("provider user ID invalid")
		}
		idString = idNumber.String()
	}
	if idString == "" || strings.TrimSpace(raw.Login) == "" {
		return providerUser{}, errors.New("provider user response incomplete")
	}
	return providerUser{ID: idString, Login: raw.Login}, nil
}

func (s *Service) allowed(ctx context.Context, accessToken, login string) (bool, error) {
	for _, allowed := range s.cfg.GitHubAllowedUsers {
		if strings.EqualFold(strings.TrimSpace(allowed), login) {
			return true, nil
		}
	}
	return s.organizationAllowed(ctx, accessToken)
}

func (s *Service) organizationAllowed(ctx context.Context, accessToken string) (bool, error) {
	if strings.TrimSpace(s.cfg.GitHubAllowedOrg) == "" || s.provider.OrganizationsURL == "" {
		return false, nil
	}
	if s.provider.OrganizationMembershipURL != "" {
		membershipURL := strings.Replace(s.provider.OrganizationMembershipURL, "{org}", url.PathEscape(strings.TrimSpace(s.cfg.GitHubAllowedOrg)), 1)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, membershipURL, nil)
		if err != nil {
			return false, err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Accept", "application/vnd.github+json")
		response, err := s.client.Do(req)
		if err != nil {
			return false, err
		}
		body, readErr := readOAuthBody(response.Body)
		response.Body.Close()
		if response.StatusCode == http.StatusNotFound {
			return false, nil
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return false, fmt.Errorf("organization membership lookup returned status %d", response.StatusCode)
		}
		if readErr != nil {
			return false, readErr
		}
		var membership struct {
			State string `json:"state"`
		}
		if err := json.Unmarshal(body, &membership); err != nil {
			return false, err
		}
		return strings.EqualFold(membership.State, "active"), nil
	}
	base, err := url.Parse(s.provider.OrganizationsURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return false, errors.New("organization endpoint invalid")
	}
	for page := 1; page <= 10; page++ {
		query := base.Query()
		query.Set("per_page", "100")
		query.Set("page", fmt.Sprint(page))
		base.RawQuery = query.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
		if err != nil {
			return false, err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Accept", "application/vnd.github+json")
		response, err := s.client.Do(req)
		if err != nil {
			return false, err
		}
		var organizations []struct {
			Login string `json:"login"`
		}
		body, readErr := readOAuthBody(response.Body)
		response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return false, fmt.Errorf("organization lookup returned status %d", response.StatusCode)
		}
		if readErr != nil {
			return false, readErr
		}
		if err := json.Unmarshal(body, &organizations); err != nil {
			return false, err
		}
		for _, organization := range organizations {
			if strings.EqualFold(strings.TrimSpace(organization.Login), strings.TrimSpace(s.cfg.GitHubAllowedOrg)) {
				return true, nil
			}
		}
		if !strings.Contains(response.Header.Get("Link"), `rel="next"`) || len(organizations) == 0 {
			return false, nil
		}
	}
	return false, nil
}

func (s *Service) policyDigest() []byte {
	users := append([]string(nil), s.cfg.GitHubAllowedUsers...)
	sort.Strings(users)
	policy := strings.Join(users, "\x00") + "\x00" + strings.TrimSpace(s.cfg.GitHubAllowedOrg)
	return security.Digest([]byte(s.cfg.SessionSecret), policy)
}

func sameURLOrigin(a, b *url.URL) bool {
	return originKey(a) == originKey(b)
}

func originKey(value *url.URL) string {
	return strings.ToLower(value.Scheme) + "://" + strings.ToLower(value.Hostname()) + ":" + normalizedPort(value)
}

func normalizedPort(value *url.URL) string {
	if value.Port() != "" {
		return value.Port()
	}
	if strings.EqualFold(value.Scheme, "https") {
		return "443"
	}
	return "80"
}

func logInternalError(operation string, err error) {
	slog.Error("authentication operation failed", "operation", operation, "error_class", fmt.Sprintf("%T", err))
}

const maxOAuthResponseBytes = 1 << 20

func readOAuthBody(reader io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, maxOAuthResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxOAuthResponseBytes {
		return nil, errors.New("provider response too large")
	}
	return body, nil
}

func setCookie(w http.ResponseWriter, cfg config.Config, cookie *http.Cookie) {
	http.SetCookie(w, cookie)
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.Copy(w, bytes.NewBufferString(fmt.Sprintf(`{"error":%q}`, message)))
}
