package db

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/probewatch/probewatch/internal/security"
	moderncsqlite "modernc.org/sqlite"
)

var (
	ErrTokenAlreadyConsumed              = errors.New("registration token already consumed")
	ErrTokenExpired                      = errors.New("token expired")
	ErrTokenInvalid                      = errors.New("invalid token")
	ErrTokenRevoked                      = errors.New("node token revoked")
	ErrNodeDeleted                       = errors.New("node deleted")
	ErrReplay                            = errors.New("request replay detected")
	ErrOAuthStateConsumed                = errors.New("oauth state already consumed")
	ErrOAuthStateExpired                 = errors.New("oauth state expired")
	ErrOAuthStateInvalid                 = errors.New("invalid oauth state")
	ErrOrganizationProviderCheckRequired = errors.New("organization allowlist requires provider membership check")
	ErrSessionNotFound                   = errors.New("session not found")
	ErrSessionExpired                    = errors.New("session expired")
	ErrSessionPolicyChanged              = errors.New("session authorization policy changed")
	ErrNodeUUIDConflict                  = errors.New("node UUID already exists")
	ErrTargetNotFound                    = errors.New("target not found")
	ErrTargetDisabled                    = errors.New("target is disabled")
	ErrTargetKindMismatch                = errors.New("target kind mismatch")
	ErrAlertNotFound                     = errors.New("alert not found")
	ErrAlertResolved                     = errors.New("alert already resolved")
	ErrTOTPPendingInvalid                = errors.New("invalid totp pending credential")
	ErrTOTPPendingExpired                = errors.New("totp pending credential expired")
	ErrTOTPPendingConsumed               = errors.New("totp pending credential already consumed")
)

const DefaultRegistrationTokenLifetime = 15 * time.Minute
const DefaultNodeTokenLifetime = 365 * 24 * time.Hour

const maxReplayCleanupRows = 1000
const maxAuthCleanupRows = 1000
const maxLifecycleCleanupRows = 500
const maxResourcePayloadBytes = 64 * 1024
const maxTargetIDLength = 128
const maxTargetNameLength = 128
const maxTargetHostLength = 253

const (
	AlertStatusOpen     = "open"
	AlertStatusAcked    = "acked"
	AlertStatusResolved = "resolved"
	AlertSeverityInfo   = "info"
	AlertSeverityWarn   = "warning"
	AlertSeverityCrit   = "critical"
)

type AlertEvent struct {
	ID              string
	NodeID          string
	Fingerprint     string
	Category        string
	TargetID        string
	Reason          string
	Severity        string
	Status          string
	OccurrenceCount int
	FirstSeenAt     time.Time
	LastSeenAt      time.Time
	ResolvedAt      *time.Time
}

type AlertEvaluation struct {
	Category string
	TargetID string
	Reason   string
	Severity string
	Failing  bool
	// FingerprintDimension optionally namespaces the alert fingerprint so
	// evaluations for the same node+category+target can coexist without
	// resolving each other. Empty keeps the legacy fingerprint material
	// unchanged.
	FingerprintDimension string
}

type Store struct {
	db     *sql.DB
	pepper []byte
	mu     sync.Mutex
}

type RegistrationToken struct {
	ID        string
	Token     string
	ExpiresAt time.Time
}

type NodeInput struct {
	UUID string
	Name string
}

type Node struct {
	ID   string
	UUID string
	Name string
}

type LatestResult struct {
	ID        string
	CheckedAt time.Time
	Payload   []byte
}

type ResourceHistoryRecord struct {
	NodeID     string
	ReportedAt time.Time
	RecordedAt time.Time
	Payload    []byte
}
type ResultHistoryRecord struct {
	NodeID     string
	TargetID   string
	CheckedAt  time.Time
	RecordedAt time.Time
	Payload    []byte
}

type RegisteredNode struct {
	Node  Node
	Token string
}

type ResultTargetInput struct {
	ID   string
	Name string
	Kind string
	Host string
}

type TargetKind string

const (
	TargetKindTCP       TargetKind = "tcp"
	TargetKindHTTP      TargetKind = "http"
	TargetKindHTTPS     TargetKind = "https"
	TargetKindDNS       TargetKind = "dns"
	TargetKindMTR       TargetKind = "mtr"
	TargetKindMediaHTTP TargetKind = "media_http"
)

type TargetDefinition struct {
	ID      string
	Name    string
	Kind    TargetKind
	Host    string
	Enabled bool
	Payload []byte
}

type TargetRecord struct {
	TargetDefinition
	CreatedAt time.Time
	UpdatedAt time.Time
}

// AgentResultInput contains a validated, typed result payload ready for persistence.
type AgentResultInput struct {
	Kind      TargetKind
	TargetID  string
	CheckedAt time.Time
	Payload   []byte
}

// AgentReportInput contains validated report data and all writes that must share its replay transaction.
type AgentReportInput struct {
	NodeID          string
	RequestID       string
	ReplayExpiresAt time.Time
	Now             time.Time
	ReportedAt      time.Time
	ResourcePayload []byte
	Results         []AgentResultInput
}

type AdminUser struct {
	ID             string
	Provider       string
	ProviderUserID string
	Login          string
}

type Session struct {
	ID          string
	AdminUserID string
	ExpiresAt   time.Time
}

func OpenStore(databasePath string, pepper []byte) (*Store, error) {
	if len(pepper) == 0 {
		return nil, errors.New("token pepper must not be empty")
	}
	directory := filepath.Dir(databasePath)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	if err := os.Chmod(directory, 0700); err != nil {
		return nil, fmt.Errorf("set database directory permissions: %w", err)
	}
	databaseFile, err := os.OpenFile(databasePath, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, fmt.Errorf("create database file: %w", err)
	}
	if err := databaseFile.Close(); err != nil {
		return nil, fmt.Errorf("close database file: %w", err)
	}

	db, err := sql.Open("sqlite", sqliteDSN(databasePath))
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := os.Chmod(databasePath, 0600); err != nil {
		db.Close()
		return nil, fmt.Errorf("set database permissions: %w", err)
	}
	store := &Store{db: db, pepper: append([]byte(nil), pepper...)}
	if err := migrate(context.Background(), db); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) CreateOAuthState(ctx context.Context, stateDigest []byte, expiresAt time.Time) (string, error) {
	id, err := randomID()
	if err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO oauth_states (id, state_digest, expires_at) VALUES (?, ?, ?)`, id, stateDigest, unixNano(expiresAt))
	if err != nil {
		return "", fmt.Errorf("create oauth state: %w", err)
	}
	return id, nil
}

func (s *Store) ConsumeOAuthState(ctx context.Context, stateDigest []byte, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin oauth state consume: %w", err)
	}
	defer tx.Rollback()

	var id string
	var expiresAt int64
	var consumedAt sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT id, expires_at, consumed_at FROM oauth_states WHERE state_digest = ?`, stateDigest).Scan(&id, &expiresAt, &consumedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrOAuthStateInvalid
	}
	if err != nil {
		return fmt.Errorf("find oauth state: %w", err)
	}
	if consumedAt.Valid {
		return ErrOAuthStateConsumed
	}
	if now.UnixNano() >= expiresAt {
		return ErrOAuthStateExpired
	}
	result, err := tx.ExecContext(ctx, `UPDATE oauth_states SET consumed_at = ? WHERE id = ? AND consumed_at IS NULL`, unixNano(now), id)
	if err != nil {
		return fmt.Errorf("consume oauth state: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect oauth state consume: %w", err)
	}
	if affected != 1 {
		return ErrOAuthStateConsumed
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit oauth state consume: %w", err)
	}
	return nil
}

type LifecycleCleanupResult struct {
	ResourceHistory       int64
	NetworkResultsHistory int64
	MTRResultsHistory     int64
	MediaResultsHistory   int64
	AuditEvents           int64
	ResolvedAlertEvents   int64
	RegistrationTokens    int64
	NodeTokens            int64
	RequestReplays        int64
}

func (s *Store) CleanupLifecycle(ctx context.Context, now time.Time) (LifecycleCleanupResult, error) {
	var result LifecycleCleanupResult
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("begin lifecycle cleanup: %w", err)
	}
	defer tx.Rollback()

	deleteBatch := func(query, resource string, args ...any) (int64, error) {
		res, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return 0, fmt.Errorf("cleanup %s: %w", resource, err)
		}
		count, err := res.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("inspect cleanup %s: %w", resource, err)
		}
		return count, nil
	}
	cutoff30 := unixNano(now.Add(-30 * 24 * time.Hour))
	cutoff90 := unixNano(now.Add(-90 * 24 * time.Hour))
	history := []struct {
		name  string
		query string
		args  []any
		set   *int64
	}{
		{"resource history", `DELETE FROM resource_history WHERE rowid IN (SELECT rowid FROM resource_history WHERE reported_at < ? ORDER BY reported_at, rowid LIMIT ?)`, []any{cutoff30, maxLifecycleCleanupRows}, &result.ResourceHistory},
		{"network results history", `DELETE FROM network_results_history WHERE rowid IN (SELECT rowid FROM network_results_history WHERE checked_at < ? ORDER BY checked_at, rowid LIMIT ?)`, []any{cutoff30, maxLifecycleCleanupRows}, &result.NetworkResultsHistory},
		{"mtr results history", `DELETE FROM mtr_results_history WHERE rowid IN (SELECT rowid FROM mtr_results_history WHERE checked_at < ? ORDER BY checked_at, rowid LIMIT ?)`, []any{cutoff30, maxLifecycleCleanupRows}, &result.MTRResultsHistory},
		{"media results history", `DELETE FROM media_results_history WHERE rowid IN (SELECT rowid FROM media_results_history WHERE checked_at < ? ORDER BY checked_at, rowid LIMIT ?)`, []any{cutoff30, maxLifecycleCleanupRows}, &result.MediaResultsHistory},
		{"audit events", `DELETE FROM audit_events WHERE rowid IN (SELECT rowid FROM audit_events WHERE created_at < ? ORDER BY created_at, rowid LIMIT ?)`, []any{cutoff90, maxLifecycleCleanupRows}, &result.AuditEvents},
		{"resolved alert events", `DELETE FROM alert_events WHERE rowid IN (SELECT rowid FROM alert_events WHERE status = 'resolved' AND resolved_at IS NOT NULL AND resolved_at < ? ORDER BY resolved_at, rowid LIMIT ?)`, []any{cutoff90, maxLifecycleCleanupRows}, &result.ResolvedAlertEvents},
		{"registration tokens", `DELETE FROM registration_tokens WHERE rowid IN (SELECT rowid FROM registration_tokens WHERE expires_at <= ? ORDER BY expires_at, rowid LIMIT ?)`, []any{unixNano(now), maxLifecycleCleanupRows}, &result.RegistrationTokens},
		{"node tokens", `DELETE FROM node_tokens WHERE rowid IN (SELECT rowid FROM node_tokens WHERE expires_at <= ? OR revoked_at IS NOT NULL ORDER BY COALESCE(revoked_at, expires_at), rowid LIMIT ?)`, []any{unixNano(now), maxLifecycleCleanupRows}, &result.NodeTokens},
		{"request replays", `DELETE FROM request_replays WHERE rowid IN (SELECT rowid FROM request_replays WHERE expires_at <= ? ORDER BY expires_at, rowid LIMIT ?)`, []any{unixNano(now), maxLifecycleCleanupRows}, &result.RequestReplays},
	}
	for _, item := range history {
		count, err := deleteBatch(item.query, item.name, item.args...)
		if err != nil {
			return LifecycleCleanupResult{}, err
		}
		*item.set = count
	}
	if err := tx.Commit(); err != nil {
		return LifecycleCleanupResult{}, fmt.Errorf("commit lifecycle cleanup: %w", err)
	}
	return result, nil
}
func (s *Store) CleanupExpiredOAuthStates(ctx context.Context, now time.Time) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin oauth state cleanup: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `DELETE FROM oauth_states WHERE rowid IN (SELECT rowid FROM oauth_states WHERE expires_at <= ? ORDER BY expires_at, rowid LIMIT ?)`, unixNano(now), maxAuthCleanupRows)
	if err != nil {
		return 0, fmt.Errorf("cleanup oauth states: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("inspect oauth state cleanup: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit oauth state cleanup: %w", err)
	}
	return deleted, nil
}

func (s *Store) UpsertAdminUser(ctx context.Context, provider, providerUserID, login string, now time.Time) (AdminUser, error) {
	id, err := randomID()
	if err != nil {
		return AdminUser{}, err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO admin_users (id, provider, provider_user_id, login, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(provider, provider_user_id) DO UPDATE SET login = excluded.login, updated_at = excluded.updated_at`,
		id, provider, providerUserID, login, unixNano(now), unixNano(now))
	if err != nil {
		return AdminUser{}, fmt.Errorf("upsert admin user: %w", err)
	}
	var user AdminUser
	if err := s.db.QueryRowContext(ctx, `SELECT id, provider, provider_user_id, login FROM admin_users WHERE provider = ? AND provider_user_id = ?`, provider, providerUserID).Scan(&user.ID, &user.Provider, &user.ProviderUserID, &user.Login); err != nil {
		return AdminUser{}, fmt.Errorf("read admin user: %w", err)
	}
	return user, nil
}

func (s *Store) GetAdminUser(ctx context.Context, adminUserID string) (AdminUser, error) {
	var user AdminUser
	err := s.db.QueryRowContext(ctx, `SELECT id, provider, provider_user_id, login FROM admin_users WHERE id = ?`, adminUserID).Scan(&user.ID, &user.Provider, &user.ProviderUserID, &user.Login)
	if errors.Is(err, sql.ErrNoRows) {
		return AdminUser{}, sql.ErrNoRows
	}
	if err != nil {
		return AdminUser{}, fmt.Errorf("get admin user: %w", err)
	}
	return user, nil
}

// FindAllowedAdminUser evaluates the explicit login allowlist. Organization
// membership is provider-owned and must be checked by the OAuth client.
func (s *Store) FindAllowedAdminUser(ctx context.Context, provider, providerUserID string, allowedUsers []string, allowedOrg string, now time.Time) (bool, error) {
	// Organization membership is provider-owned and is intentionally not inferred from SQLite.
	// A non-empty organization policy must be evaluated by the OAuth provider client.
	if strings.TrimSpace(allowedOrg) != "" {
		return false, ErrOrganizationProviderCheckRequired
	}
	var login string
	err := s.db.QueryRowContext(ctx, `SELECT login FROM admin_users WHERE provider = ? AND provider_user_id = ?`, provider, providerUserID).Scan(&login)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("find admin user: %w", err)
	}
	_ = now
	for _, allowed := range allowedUsers {
		if strings.EqualFold(strings.TrimSpace(allowed), login) {
			return true, nil
		}
	}
	return false, nil
}

// GetAdminUserTOTP returns the stored Base32 TOTP secret and whether
// two-factor authentication is enabled for the admin user.
func (s *Store) GetAdminUserTOTP(ctx context.Context, adminUserID string) ([]byte, bool, error) {
	var secret []byte
	var enabled int64
	err := s.db.QueryRowContext(ctx, `SELECT totp_secret, totp_enabled FROM admin_users WHERE id = ?`, adminUserID).Scan(&secret, &enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, sql.ErrNoRows
	}
	if err != nil {
		return nil, false, fmt.Errorf("get admin totp: %w", err)
	}
	return secret, enabled != 0, nil
}

// SetAdminUserTOTP persists the TOTP secret and enabled flag. A nil secret
// clears the stored value; disabling must always clear the secret.
func (s *Store) SetAdminUserTOTP(ctx context.Context, adminUserID string, secret []byte, enabled bool, now time.Time) error {
	if secret == nil {
		secret = []byte{}
	}
	enabledValue := 0
	if enabled {
		enabledValue = 1
	}
	result, err := s.db.ExecContext(ctx, `UPDATE admin_users SET totp_secret = ?, totp_enabled = ?, updated_at = ? WHERE id = ?`, secret, enabledValue, unixNano(now), adminUserID)
	if err != nil {
		return fmt.Errorf("set admin totp: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect admin totp update: %w", err)
	}
	if affected != 1 {
		return sql.ErrNoRows
	}
	return nil
}

// CreateTOTPPendingState issues a single-use, short-lived pending credential
// for an administrator who authenticated via OAuth but still owes a TOTP code.
// The plaintext is returned once; only its peppered digest is stored.
func (s *Store) CreateTOTPPendingState(ctx context.Context, adminUserID string, now time.Time, lifetime time.Duration) (string, error) {
	plaintext, err := security.GenerateToken()
	if err != nil {
		return "", err
	}
	id, err := randomID()
	if err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO totp_pending_states (id, token_digest, admin_user_id, expires_at) VALUES (?, ?, ?, ?)`, id, security.Digest(s.pepper, plaintext), adminUserID, unixNano(now.Add(lifetime)))
	if err != nil {
		return "", fmt.Errorf("create totp pending state: %w", err)
	}
	return plaintext, nil
}

// GetTOTPPendingState resolves a pending credential to its admin user without
// consuming it, so a mistyped code can be retried within the lifetime.
func (s *Store) GetTOTPPendingState(ctx context.Context, plaintext string, now time.Time) (string, error) {
	var adminUserID string
	var expiresAt int64
	var consumedAt sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT admin_user_id, expires_at, consumed_at FROM totp_pending_states WHERE token_digest = ?`, security.Digest(s.pepper, plaintext)).Scan(&adminUserID, &expiresAt, &consumedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrTOTPPendingInvalid
	}
	if err != nil {
		return "", fmt.Errorf("find totp pending state: %w", err)
	}
	if consumedAt.Valid {
		return "", ErrTOTPPendingConsumed
	}
	if now.UnixNano() >= expiresAt {
		return "", ErrTOTPPendingExpired
	}
	return adminUserID, nil
}

// ConsumeTOTPPendingState atomically marks a pending credential used. The
// conditional update ensures a credential can complete at most one login.
func (s *Store) ConsumeTOTPPendingState(ctx context.Context, plaintext string, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE totp_pending_states SET consumed_at = ? WHERE token_digest = ? AND consumed_at IS NULL AND expires_at > ?`, unixNano(now), security.Digest(s.pepper, plaintext), unixNano(now))
	if err != nil {
		return fmt.Errorf("consume totp pending state: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect totp pending state consume: %w", err)
	}
	if affected != 1 {
		return ErrTOTPPendingInvalid
	}
	return nil
}

func (s *Store) CleanupExpiredTOTPPendingStates(ctx context.Context, now time.Time) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin totp pending state cleanup: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `DELETE FROM totp_pending_states WHERE rowid IN (SELECT rowid FROM totp_pending_states WHERE expires_at <= ? OR consumed_at IS NOT NULL ORDER BY expires_at, rowid LIMIT ?)`, unixNano(now), maxAuthCleanupRows)
	if err != nil {
		return 0, fmt.Errorf("cleanup totp pending states: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("inspect totp pending state cleanup: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit totp pending state cleanup: %w", err)
	}
	return deleted, nil
}

func (s *Store) CreateSession(ctx context.Context, id string, sessionValue []byte, adminUserID string, expiresAt, createdAt time.Time) error {
	return s.CreateSessionWithPolicy(ctx, id, sessionValue, adminUserID, expiresAt, createdAt, nil)
}

func (s *Store) CreateSessionWithPolicy(ctx context.Context, id string, sessionValue []byte, adminUserID string, expiresAt, createdAt time.Time, policyDigest []byte) error {
	if policyDigest == nil {
		policyDigest = []byte{}
	}
	initialCSRF := security.Digest(s.pepper, "initial-csrf:"+string(sessionValue))
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (id, admin_user_id, session_digest, csrf_digest, policy_digest, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, id, adminUserID, security.Digest(s.pepper, string(sessionValue)), initialCSRF, policyDigest, unixNano(expiresAt), unixNano(createdAt))
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (s *Store) RotateCSRF(ctx context.Context, sessionValue []byte, now time.Time) (string, error) {
	token, err := security.GenerateToken()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.ExecContext(ctx, `UPDATE sessions SET csrf_digest = ? WHERE session_digest = ? AND expires_at > ?`, security.Digest(s.pepper, token), security.Digest(s.pepper, string(sessionValue)), unixNano(now))
	if err != nil {
		return "", fmt.Errorf("rotate csrf token: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return "", fmt.Errorf("inspect csrf token rotation: %w", err)
	}
	if affected != 1 {
		return "", ErrSessionNotFound
	}
	return token, nil
}

// ClaimCSRF atomically consumes the current token and stores its replacement.
// The replacement is returned to the caller only after the handler succeeds.
func (s *Store) ClaimCSRF(ctx context.Context, sessionValue []byte, token string, now time.Time) (string, error) {
	nextToken, err := security.GenerateToken()
	if err != nil {
		return "", err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin csrf token consume: %w", err)
	}
	defer tx.Rollback()
	var storedDigest []byte
	err = tx.QueryRowContext(ctx, `SELECT csrf_digest FROM sessions WHERE session_digest = ? AND expires_at > ?`, security.Digest(s.pepper, string(sessionValue)), unixNano(now)).Scan(&storedDigest)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrSessionNotFound
	}
	if err != nil {
		return "", fmt.Errorf("find csrf token: %w", err)
	}
	if !security.Verify(s.pepper, token, storedDigest) {
		return "", ErrTokenInvalid
	}
	result, err := tx.ExecContext(ctx, `UPDATE sessions SET csrf_digest = ? WHERE session_digest = ? AND csrf_digest = ? AND expires_at > ?`, security.Digest(s.pepper, nextToken), security.Digest(s.pepper, string(sessionValue)), storedDigest, unixNano(now))
	if err != nil {
		return "", fmt.Errorf("rotate consumed csrf token: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return "", fmt.Errorf("inspect consumed csrf token: %w", err)
	}
	if affected != 1 {
		return "", ErrTokenInvalid
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit csrf token consume: %w", err)
	}
	return nextToken, nil
}

func (s *Store) CSRFTokenIsCurrent(ctx context.Context, sessionValue []byte, token string, now time.Time) (bool, error) {
	var storedDigest []byte
	err := s.db.QueryRowContext(ctx, `SELECT csrf_digest FROM sessions WHERE session_digest = ? AND expires_at > ?`, security.Digest(s.pepper, string(sessionValue)), unixNano(now)).Scan(&storedDigest)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrSessionNotFound
	}
	if err != nil {
		return false, fmt.Errorf("find current csrf token: %w", err)
	}
	return security.Verify(s.pepper, token, storedDigest), nil
}

func (s *Store) GetSession(ctx context.Context, sessionValue []byte, now time.Time) (Session, error) {
	return s.getSession(ctx, sessionValue, now, nil, false)
}

func (s *Store) GetSessionWithPolicy(ctx context.Context, sessionValue []byte, now time.Time, policyDigest []byte) (Session, error) {
	return s.getSession(ctx, sessionValue, now, policyDigest, true)
}

func (s *Store) getSession(ctx context.Context, sessionValue []byte, now time.Time, policyDigest []byte, checkPolicy bool) (Session, error) {
	var session Session
	var expiresAt int64
	var storedPolicy []byte
	err := s.db.QueryRowContext(ctx, `SELECT id, admin_user_id, expires_at, policy_digest FROM sessions WHERE session_digest = ?`, security.Digest(s.pepper, string(sessionValue))).Scan(&session.ID, &session.AdminUserID, &expiresAt, &storedPolicy)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrSessionNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("get session: %w", err)
	}
	session.ExpiresAt = time.Unix(0, expiresAt).UTC()
	if now.UnixNano() >= expiresAt {
		return Session{}, ErrSessionExpired
	}
	if checkPolicy && !bytes.Equal(storedPolicy, policyDigest) {
		return Session{}, ErrSessionPolicyChanged
	}
	return session, nil
}

func (s *Store) CleanupExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin session cleanup: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE rowid IN (SELECT rowid FROM sessions WHERE expires_at <= ? ORDER BY expires_at, rowid LIMIT ?)`, unixNano(now), maxAuthCleanupRows)
	if err != nil {
		return 0, fmt.Errorf("cleanup sessions: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("inspect session cleanup: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit session cleanup: %w", err)
	}
	return deleted, nil
}

func (s *Store) DeleteSession(ctx context.Context, sessionValue []byte) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE session_digest = ?`, security.Digest(s.pepper, string(sessionValue))); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (s *Store) CreateRegistrationToken(ctx context.Context, lifetime time.Duration) (RegistrationToken, error) {
	if lifetime == 0 {
		lifetime = DefaultRegistrationTokenLifetime
	}
	if lifetime < 0 {
		return RegistrationToken{}, errors.New("registration token lifetime must be positive")
	}
	plaintext, err := security.GenerateToken()
	if err != nil {
		return RegistrationToken{}, err
	}
	id, err := randomID()
	if err != nil {
		return RegistrationToken{}, err
	}
	expiresAt := time.Now().Add(lifetime).UTC()
	_, err = s.db.ExecContext(ctx, `INSERT INTO registration_tokens (id, token_digest, expires_at, created_at) VALUES (?, ?, ?, ?)`, id, security.Digest(s.pepper, plaintext), unixNano(expiresAt), unixNano(time.Now()))
	if err != nil {
		return RegistrationToken{}, fmt.Errorf("create registration token: %w", err)
	}
	return RegistrationToken{ID: id, Token: plaintext, ExpiresAt: expiresAt}, nil
}

// ConsumeRegistrationToken atomically marks a registration token consumed.
// It returns false without an error when the conditional update affects no rows.
func (s *Store) ConsumeRegistrationToken(ctx context.Context, plaintext string, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin token consume: %w", err)
	}
	defer tx.Rollback()

	var id string
	var storedDigest []byte
	var expiresAt int64
	var consumedAt sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT id, token_digest, expires_at, consumed_at FROM registration_tokens WHERE token_digest = ?`, security.Digest(s.pepper, plaintext)).Scan(&id, &storedDigest, &expiresAt, &consumedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrTokenInvalid
	}
	if err != nil {
		return false, fmt.Errorf("find registration token: %w", err)
	}
	if !security.Verify(s.pepper, plaintext, storedDigest) {
		return false, ErrTokenInvalid
	}
	if consumedAt.Valid {
		return false, ErrTokenAlreadyConsumed
	}
	if now.UnixNano() >= expiresAt {
		return false, ErrTokenExpired
	}
	result, err := tx.ExecContext(ctx, `UPDATE registration_tokens SET consumed_at = ? WHERE id = ? AND consumed_at IS NULL`, unixNano(now), id)
	if err != nil {
		return false, fmt.Errorf("consume registration token: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("inspect registration token consume: %w", err)
	}
	if affected == 0 {
		return false, nil
	}
	if affected != 1 {
		return false, fmt.Errorf("consume registration token affected %d rows", affected)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit token consume: %w", err)
	}
	return true, nil
}

func (s *Store) RegisterNode(ctx context.Context, registrationToken string, input NodeInput, now time.Time) (RegisteredNode, error) {
	return s.RegisterNodeWithTTL(ctx, registrationToken, input, now, DefaultNodeTokenLifetime)
}

func (s *Store) RegisterNodeWithTTL(ctx context.Context, registrationToken string, input NodeInput, now time.Time, lifetime time.Duration) (RegisteredNode, error) {
	if lifetime <= 0 {
		return RegisteredNode{}, errors.New("node token lifetime must be positive")
	}
	if !security.IsRFC4122UUID(input.UUID) || strings.TrimSpace(input.Name) == "" {
		return RegisteredNode{}, errors.New("node UUID and name are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RegisteredNode{}, fmt.Errorf("begin node registration: %w", err)
	}
	defer tx.Rollback()
	var registrationID string
	var storedDigest []byte
	var expiresAt, consumedAt int64
	err = tx.QueryRowContext(ctx, `SELECT id, token_digest, expires_at, COALESCE(consumed_at, 0) FROM registration_tokens WHERE token_digest = ?`, security.Digest(s.pepper, registrationToken)).Scan(&registrationID, &storedDigest, &expiresAt, &consumedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return RegisteredNode{}, ErrTokenInvalid
	}
	if err != nil {
		return RegisteredNode{}, fmt.Errorf("find registration token: %w", err)
	}
	if !security.Verify(s.pepper, registrationToken, storedDigest) {
		return RegisteredNode{}, ErrTokenInvalid
	}
	if consumedAt != 0 {
		return RegisteredNode{}, ErrTokenAlreadyConsumed
	}
	if now.UnixNano() >= expiresAt {
		return RegisteredNode{}, ErrTokenExpired
	}
	var existingID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM nodes WHERE uuid = ?`, input.UUID).Scan(&existingID); err == nil {
		return RegisteredNode{}, ErrNodeUUIDConflict
	} else if !errors.Is(err, sql.ErrNoRows) {
		return RegisteredNode{}, fmt.Errorf("find node UUID: %w", err)
	}

	nodeID, err := randomID()
	if err != nil {
		return RegisteredNode{}, err
	}
	nodeToken, err := security.GenerateToken()
	if err != nil {
		return RegisteredNode{}, err
	}
	nodeTokenID, err := randomID()
	if err != nil {
		return RegisteredNode{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO nodes (id, uuid, name, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`, nodeID, input.UUID, input.Name, "unknown", unixNano(now), unixNano(now)); err != nil {
		return RegisteredNode{}, fmt.Errorf("insert node: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO node_tokens (id, node_id, token_digest, token_prefix, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?)`, nodeTokenID, nodeID, security.Digest(s.pepper, nodeToken), tokenPrefix(nodeToken), unixNano(now.Add(lifetime)), unixNano(now)); err != nil {
		return RegisteredNode{}, fmt.Errorf("insert node token: %w", err)
	}
	result, err := tx.ExecContext(ctx, `UPDATE registration_tokens SET consumed_at = ? WHERE id = ? AND consumed_at IS NULL`, unixNano(now), registrationID)
	if err != nil {
		return RegisteredNode{}, fmt.Errorf("consume registration token: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return RegisteredNode{}, fmt.Errorf("inspect registration token consume: %w", err)
	}
	if affected != 1 {
		return RegisteredNode{}, ErrTokenAlreadyConsumed
	}
	if err := tx.Commit(); err != nil {
		return RegisteredNode{}, fmt.Errorf("commit node registration: %w", err)
	}
	return RegisteredNode{Node: Node{ID: nodeID, UUID: input.UUID, Name: input.Name}, Token: nodeToken}, nil
}

// ListNodes returns active nodes without token material or token metadata.
func (s *Store) ListNodes(ctx context.Context) ([]Node, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, uuid, name FROM nodes WHERE deleted_at IS NULL ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	defer rows.Close()
	nodes := make([]Node, 0)
	for rows.Next() {
		var node Node
		if err := rows.Scan(&node.ID, &node.UUID, &node.Name); err != nil {
			return nil, fmt.Errorf("scan node: %w", err)
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read nodes: %w", err)
	}
	return nodes, nil
}

func (s *Store) GetNodeByUUID(ctx context.Context, uuid string) (Node, error) {
	var node Node
	err := s.db.QueryRowContext(ctx, `SELECT id, uuid, name FROM nodes WHERE uuid = ? AND deleted_at IS NULL`, uuid).Scan(&node.ID, &node.UUID, &node.Name)
	if err != nil {
		return Node{}, err
	}
	return node, nil
}

func (s *Store) CreateNetworkTarget(ctx context.Context, input ResultTargetInput, now time.Time) error {
	return s.createResultTarget(ctx, `INSERT INTO network_targets (id, name, kind, host, payload, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, input, now)
}

func (s *Store) CreateMTRTarget(ctx context.Context, input ResultTargetInput, now time.Time) error {
	return s.createResultTarget(ctx, `INSERT INTO mtr_targets (id, name, host, payload, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`, input, now)
}

func (s *Store) CreateMediaDetector(ctx context.Context, input ResultTargetInput, now time.Time) error {
	return s.createResultTarget(ctx, `INSERT INTO media_detectors (id, name, host, payload, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`, input, now)
}

func (s *Store) CreateTarget(ctx context.Context, definition TargetDefinition, now time.Time) (TargetRecord, error) {
	if err := validateTargetDefinition(definition); err != nil {
		return TargetRecord{}, err
	}
	targetTable, err := targetTable(definition.Kind)
	if err != nil {
		return TargetRecord{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TargetRecord{}, fmt.Errorf("begin target create: %w", err)
	}
	defer tx.Rollback()
	query := `INSERT INTO ` + targetTable + ` (id, name, ` + targetKindColumn(definition.Kind) + `, host, payload, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	if definition.Kind == TargetKindMTR || definition.Kind == TargetKindMediaHTTP {
		query = `INSERT INTO ` + targetTable + ` (id, name, host, payload, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`
	}
	args := []any{definition.ID, definition.Name}
	if definition.Kind != TargetKindMTR && definition.Kind != TargetKindMediaHTTP {
		args = append(args, string(definition.Kind))
	}
	args = append(args, definition.Host)
	args = append(args, definition.Payload, boolInt(definition.Enabled), unixNano(now), unixNano(now))
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return TargetRecord{}, fmt.Errorf("create target: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return TargetRecord{}, fmt.Errorf("commit target create: %w", err)
	}
	return TargetRecord{TargetDefinition: definition, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}, nil
}

func (s *Store) ListTargets(ctx context.Context, kind TargetKind) ([]TargetRecord, error) {
	targetTable, err := targetTable(kind)
	if err != nil {
		return nil, err
	}
	query := `SELECT id, name, host, payload, enabled, created_at, updated_at FROM ` + targetTable + ` ORDER BY created_at, id`
	if kind == TargetKindMediaHTTP {
		query = `SELECT id, name, host, payload, enabled, created_at, updated_at FROM ` + targetTable + ` ORDER BY created_at, id`
	}
	if kind != TargetKindMTR && kind != TargetKindMediaHTTP {
		query = `SELECT id, name, kind, host, payload, enabled, created_at, updated_at FROM ` + targetTable + ` WHERE kind = ? ORDER BY created_at, id`
	}
	var rows *sql.Rows
	if kind != TargetKindMTR && kind != TargetKindMediaHTTP {
		rows, err = s.db.QueryContext(ctx, query, string(kind))
	} else {
		rows, err = s.db.QueryContext(ctx, query)
	}
	if err != nil {
		return nil, fmt.Errorf("list targets: %w", err)
	}
	defer rows.Close()
	targets := make([]TargetRecord, 0)
	for rows.Next() {
		var target TargetRecord
		var kindValue string
		var enabled int
		var createdAt, updatedAt int64
		if kind == TargetKindMTR {
			if err := rows.Scan(&target.ID, &target.Name, &target.Host, &target.Payload, &enabled, &createdAt, &updatedAt); err != nil {
				return nil, fmt.Errorf("scan target: %w", err)
			}
		} else if kind == TargetKindMediaHTTP {
			if err := rows.Scan(&target.ID, &target.Name, &target.Host, &target.Payload, &enabled, &createdAt, &updatedAt); err != nil {
				return nil, fmt.Errorf("scan target: %w", err)
			}
		} else if err := rows.Scan(&target.ID, &target.Name, &kindValue, &target.Host, &target.Payload, &enabled, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan target: %w", err)
		}
		target.Kind = TargetKind(kind)
		if kindValue != "" {
			target.Kind = TargetKind(kindValue)
		}
		target.Enabled = enabled != 0
		target.CreatedAt = time.Unix(0, createdAt).UTC()
		target.UpdatedAt = time.Unix(0, updatedAt).UTC()
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read targets: %w", err)
	}
	return targets, nil
}

func (s *Store) GetTarget(ctx context.Context, kind TargetKind, id string) (TargetRecord, error) {
	return s.getTarget(ctx, kind, id, false)
}

func (s *Store) LookupEnabledTarget(ctx context.Context, kind TargetKind, id string) (TargetRecord, error) {
	target, err := s.getTarget(ctx, kind, id, true)
	if err != nil {
		return TargetRecord{}, err
	}
	if !target.Enabled {
		return TargetRecord{}, ErrTargetDisabled
	}
	return target, nil
}

func (s *Store) UpdateTarget(ctx context.Context, kind TargetKind, id string, definition TargetDefinition, now time.Time) (TargetRecord, error) {
	if err := validateTargetID(id); err != nil {
		return TargetRecord{}, err
	}
	definition.ID = id
	definition.Kind = kind
	if err := validateTargetDefinition(definition); err != nil {
		return TargetRecord{}, err
	}
	targetTable, err := targetTable(kind)
	if err != nil {
		return TargetRecord{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TargetRecord{}, fmt.Errorf("begin target update: %w", err)
	}
	defer tx.Rollback()
	createdAt, err := verifyTargetInTx(ctx, tx, kind, targetTable, id)
	if err != nil {
		if errors.Is(err, ErrTargetNotFound) && targetIDExistsInOtherTableTx(ctx, tx, targetTable, id) {
			return TargetRecord{}, ErrTargetKindMismatch
		}
		return TargetRecord{}, err
	}
	query := `UPDATE ` + targetTable + ` SET name = ?, host = ?, payload = ?, enabled = ?, updated_at = ? WHERE id = ?`
	args := []any{definition.Name, definition.Host, definition.Payload, boolInt(definition.Enabled), unixNano(now), id}
	if kind == TargetKindTCP || kind == TargetKindHTTP || kind == TargetKindHTTPS || kind == TargetKindDNS {
		query = `UPDATE ` + targetTable + ` SET name = ?, kind = ?, host = ?, payload = ?, enabled = ?, updated_at = ? WHERE id = ?`
		args = []any{definition.Name, string(kind), definition.Host, definition.Payload, boolInt(definition.Enabled), unixNano(now), id}
	}
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return TargetRecord{}, fmt.Errorf("update target: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return TargetRecord{}, fmt.Errorf("inspect target update: %w", err)
	}
	if affected != 1 {
		return TargetRecord{}, ErrTargetNotFound
	}
	if err := tx.Commit(); err != nil {
		return TargetRecord{}, fmt.Errorf("commit target update: %w", err)
	}
	return TargetRecord{TargetDefinition: definition, CreatedAt: time.Unix(0, createdAt).UTC(), UpdatedAt: now.UTC()}, nil
}

func (s *Store) DeleteTarget(ctx context.Context, kind TargetKind, id string) error {
	if err := validateTargetID(id); err != nil {
		return err
	}
	targetTable, err := targetTable(kind)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin target delete: %w", err)
	}
	defer tx.Rollback()
	if _, err := verifyTargetInTx(ctx, tx, kind, targetTable, id); err != nil {
		if errors.Is(err, ErrTargetNotFound) && targetIDExistsInOtherTableTx(ctx, tx, targetTable, id) {
			return ErrTargetKindMismatch
		}
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM `+targetTable+` WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete target: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect target delete: %w", err)
	}
	if affected != 1 {
		return ErrTargetNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit target delete: %w", err)
	}
	return nil
}

func (s *Store) getTarget(ctx context.Context, kind TargetKind, id string, enabledOnly bool) (TargetRecord, error) {
	if err := validateTargetID(id); err != nil {
		return TargetRecord{}, ErrTargetNotFound
	}
	targetTable, err := targetTable(kind)
	if err != nil {
		return TargetRecord{}, ErrTargetKindMismatch
	}
	query := `SELECT id, name, host, payload, enabled, created_at, updated_at FROM ` + targetTable + ` WHERE id = ?`
	if kind != TargetKindMTR && kind != TargetKindMediaHTTP {
		query = `SELECT id, name, kind, host, payload, enabled, created_at, updated_at FROM ` + targetTable + ` WHERE id = ?`
	}
	if kind == TargetKindMediaHTTP {
		query = `SELECT id, name, host, payload, enabled, created_at, updated_at FROM ` + targetTable + ` WHERE id = ?`
	}
	var target TargetRecord
	var kindValue string
	var enabled int
	var createdAt, updatedAt int64
	var scanErr error
	if kind == TargetKindMTR {
		scanErr = s.db.QueryRowContext(ctx, query, id).Scan(&target.ID, &target.Name, &target.Host, &target.Payload, &enabled, &createdAt, &updatedAt)
	} else if kind == TargetKindMediaHTTP {
		scanErr = s.db.QueryRowContext(ctx, query, id).Scan(&target.ID, &target.Name, &target.Host, &target.Payload, &enabled, &createdAt, &updatedAt)
	} else {
		scanErr = s.db.QueryRowContext(ctx, query, id).Scan(&target.ID, &target.Name, &kindValue, &target.Host, &target.Payload, &enabled, &createdAt, &updatedAt)
	}
	if errors.Is(scanErr, sql.ErrNoRows) {
		if s.targetIDExistsInOtherTable(ctx, kind, id) {
			return TargetRecord{}, ErrTargetKindMismatch
		}
		return TargetRecord{}, ErrTargetNotFound
	}
	if scanErr != nil {
		return TargetRecord{}, fmt.Errorf("get target: %w", scanErr)
	}
	target.Kind = kind
	if kindValue != "" && TargetKind(kindValue) != kind {
		return TargetRecord{}, ErrTargetKindMismatch
	}
	target.Enabled = enabled != 0
	if enabledOnly && !target.Enabled {
		return TargetRecord{}, ErrTargetDisabled
	}
	target.CreatedAt = time.Unix(0, createdAt).UTC()
	target.UpdatedAt = time.Unix(0, updatedAt).UTC()
	return target, nil
}

func (s *Store) targetIDExistsInOtherTable(ctx context.Context, kind TargetKind, id string) bool {
	currentTable, err := targetTable(kind)
	if err != nil {
		return false
	}
	return targetIDExistsInOtherTableWithTables(ctx, s.db, currentTable, id)
}

func targetIDExistsInOtherTableTx(ctx context.Context, tx *sql.Tx, currentTable, id string) bool {
	return targetIDExistsInOtherTableWithTables(ctx, tx, currentTable, id)
}

type targetQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func targetIDExistsInOtherTableWithTables(ctx context.Context, querier targetQuerier, currentTable, id string) bool {
	for _, table := range []string{"network_targets", "mtr_targets", "media_detectors"} {
		if table == currentTable {
			continue
		}
		var exists int
		if err := querier.QueryRowContext(ctx, `SELECT 1 FROM `+table+` WHERE id = ? LIMIT 1`, id).Scan(&exists); err == nil {
			return true
		}
	}
	return false
}

func validateTargetDefinition(definition TargetDefinition) error {
	if err := validateTargetKind(definition.Kind); err != nil {
		return err
	}
	if err := validateTargetID(definition.ID); err != nil {
		return err
	}
	if strings.TrimSpace(definition.Name) == "" || len([]byte(definition.Name)) > maxTargetNameLength {
		return fmt.Errorf("target name must be 1-%d bytes", maxTargetNameLength)
	}
	if strings.TrimSpace(definition.Host) == "" || len([]byte(definition.Host)) > maxTargetHostLength {
		return fmt.Errorf("target host must be 1-%d bytes", maxTargetHostLength)
	}
	if !json.Valid(definition.Payload) {
		return errors.New("target payload must be valid JSON")
	}
	if len(definition.Payload) > maxResourcePayloadBytes {
		return errors.New("target payload exceeds 64 KiB")
	}
	return nil
}

func validateTargetID(id string) error {
	if strings.TrimSpace(id) == "" || len([]byte(id)) > maxTargetIDLength {
		return fmt.Errorf("target ID must be 1-%d bytes", maxTargetIDLength)
	}
	return nil
}

func verifyTargetInTx(ctx context.Context, tx *sql.Tx, kind TargetKind, table, id string) (int64, error) {
	var createdAt int64
	if kind == TargetKindTCP || kind == TargetKindHTTP || kind == TargetKindHTTPS || kind == TargetKindDNS {
		var storedKind string
		if err := tx.QueryRowContext(ctx, `SELECT kind, created_at FROM `+table+` WHERE id = ?`, id).Scan(&storedKind, &createdAt); errors.Is(err, sql.ErrNoRows) {
			return 0, ErrTargetNotFound
		} else if err != nil {
			return 0, fmt.Errorf("find target: %w", err)
		}
		if TargetKind(storedKind) != kind {
			return 0, ErrTargetKindMismatch
		}
		return createdAt, nil
	}
	if err := tx.QueryRowContext(ctx, `SELECT created_at FROM `+table+` WHERE id = ?`, id).Scan(&createdAt); errors.Is(err, sql.ErrNoRows) {
		return 0, ErrTargetNotFound
	} else if err != nil {
		return 0, fmt.Errorf("find target: %w", err)
	}
	return createdAt, nil
}

func validateTargetKind(kind TargetKind) error {
	switch kind {
	case TargetKindTCP, TargetKindHTTP, TargetKindHTTPS, TargetKindDNS, TargetKindMTR, TargetKindMediaHTTP:
		return nil
	default:
		return ErrTargetKindMismatch
	}
}

func targetTable(kind TargetKind) (string, error) {
	switch kind {
	case TargetKindTCP, TargetKindHTTP, TargetKindHTTPS, TargetKindDNS:
		return "network_targets", nil
	case TargetKindMTR:
		return "mtr_targets", nil
	case TargetKindMediaHTTP:
		return "media_detectors", nil
	default:
		return "", ErrTargetKindMismatch
	}
}

func targetKindColumn(kind TargetKind) string {
	if kind == TargetKindTCP || kind == TargetKindHTTP || kind == TargetKindHTTPS || kind == TargetKindDNS {
		return "kind"
	}
	return ""
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (s *Store) createResultTarget(ctx context.Context, query string, input ResultTargetInput, now time.Time) error {
	if strings.TrimSpace(input.ID) == "" || strings.TrimSpace(input.Name) == "" {
		return errors.New("result target fields are required")
	}
	var args []any
	switch {
	case strings.Contains(query, "network_targets"):
		if strings.TrimSpace(input.Host) == "" {
			return errors.New("result target fields are required")
		}
		args = []any{input.ID, input.Name, input.Kind, input.Host, []byte(`{}`), unixNano(now), unixNano(now)}
	case strings.Contains(query, "mtr_targets"):
		if strings.TrimSpace(input.Host) == "" {
			return errors.New("result target fields are required")
		}
		args = []any{input.ID, input.Name, input.Host, []byte(`{}`), unixNano(now), unixNano(now)}
	case strings.Contains(query, "media_detectors"):
		if strings.TrimSpace(input.Host) == "" {
			return errors.New("result target fields are required")
		}
		args = []any{input.ID, input.Name, input.Host, []byte(`{}`), unixNano(now), unixNano(now)}
	default:
		return errors.New("invalid result target query")
	}
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("create result target: %w", err)
	}
	return nil
}

func (s *Store) UpsertResourceLatest(ctx context.Context, nodeID string, reportedAt time.Time, payload []byte) error {
	if !json.Valid(payload) {
		return errors.New("resource payload must be valid JSON")
	}
	if len(payload) > maxResourcePayloadBytes {
		return errors.New("resource payload exceeds 64 KiB")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin resource latest upsert: %w", err)
	}
	defer tx.Rollback()

	if err := ensureNodeInTx(ctx, tx, nodeID); err != nil {
		return fmt.Errorf("find resource node: %w", err)
	}
	if err := upsertResourceLatestTx(ctx, tx, nodeID, reportedAt, payload); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit resource latest upsert: %w", err)
	}
	return nil
}

func (s *Store) GetResourceLatest(ctx context.Context, nodeID string) (time.Time, []byte, error) {
	var reportedAt int64
	var payload []byte
	if err := s.db.QueryRowContext(ctx, `SELECT reported_at, payload FROM resource_latest WHERE node_id = ?`, nodeID).Scan(&reportedAt, &payload); err != nil {
		return time.Time{}, nil, err
	}
	return time.Unix(0, reportedAt).UTC(), payload, nil
}

func (s *Store) GetResourceHistory(ctx context.Context, nodeID string, from, to time.Time, limit int) ([]ResourceHistoryRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	fromNano := unixNano(from)
	toNano := unixNano(to)
	span := toNano - fromNano

	var rows *sql.Rows
	var err error

	// If time window is wider than 30 minutes, compute downsampling bucket size
	bucketSize := span / int64(limit)
	if bucketSize > int64(15*time.Second) {
		query := `
SELECT reported_at, recorded_at, payload
FROM (
    SELECT reported_at, recorded_at, payload,
           ROW_NUMBER() OVER (PARTITION BY (reported_at / ?) ORDER BY reported_at ASC) as rn
    FROM resource_history
    WHERE node_id = ? AND reported_at >= ? AND reported_at <= ?
)
WHERE rn = 1
ORDER BY reported_at ASC
LIMIT ?`
		rows, err = s.db.QueryContext(ctx, query, bucketSize, nodeID, fromNano, toNano, limit)
	} else {
		query := `
SELECT reported_at, recorded_at, payload
FROM resource_history
WHERE node_id = ? AND reported_at >= ? AND reported_at <= ?
ORDER BY reported_at ASC
LIMIT ?`
		rows, err = s.db.QueryContext(ctx, query, nodeID, fromNano, toNano, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("query resource history: %w", err)
	}
	defer rows.Close()
	out := make([]ResourceHistoryRecord, 0)
	for rows.Next() {
		var r ResourceHistoryRecord
		var a, b int64
		if err := rows.Scan(&a, &b, &r.Payload); err != nil {
			return nil, fmt.Errorf("scan resource history: %w", err)
		}
		r.NodeID = nodeID
		r.ReportedAt = time.Unix(0, a).UTC()
		r.RecordedAt = time.Unix(0, b).UTC()
		if !json.Valid(r.Payload) {
			return nil, errors.New("invalid resource history payload")
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read resource history: %w", err)
	}
	return out, nil
}
func (s *Store) GetResultHistory(ctx context.Context, kind TargetKind, nodeID string, from, to time.Time, limit int) ([]ResultHistoryRecord, error) {
	table, col, err := historyTable(kind)
	if err != nil {
		return nil, err
	}
	q := fmt.Sprintf(`SELECT %s, checked_at, recorded_at, payload FROM %s WHERE node_id = ? AND checked_at >= ? AND checked_at <= ? ORDER BY checked_at DESC, id DESC LIMIT ?`, col, table)
	rows, err := s.db.QueryContext(ctx, q, nodeID, unixNano(from), unixNano(to), limit)
	if err != nil {
		return nil, fmt.Errorf("query result history: %w", err)
	}
	defer rows.Close()
	out := make([]ResultHistoryRecord, 0)
	for rows.Next() {
		var r ResultHistoryRecord
		var a, b int64
		if err := rows.Scan(&r.TargetID, &a, &b, &r.Payload); err != nil {
			return nil, fmt.Errorf("scan result history: %w", err)
		}
		r.NodeID = nodeID
		r.CheckedAt = time.Unix(0, a).UTC()
		r.RecordedAt = time.Unix(0, b).UTC()
		if !json.Valid(r.Payload) {
			return nil, errors.New("invalid result history payload")
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read result history: %w", err)
	}
	return out, nil
}

func historyTable(kind TargetKind) (string, string, error) {
	switch kind {
	case TargetKindTCP, TargetKindHTTP, TargetKindHTTPS, TargetKindDNS:
		return "network_results_history", "target_id", nil
	case TargetKindMTR:
		return "mtr_results_history", "target_id", nil
	case TargetKindMediaHTTP:
		return "media_results_history", "detector_id", nil
	default:
		return "", "", errors.New("invalid history result kind")
	}
}

func (s *Store) UpsertNetworkLatest(ctx context.Context, nodeID, targetID string, checkedAt time.Time, payload []byte) error {
	return s.upsertLatestResult(ctx, "network_results_latest", "target_id", nodeID, targetID, checkedAt, payload)
}

func (s *Store) GetNetworkLatest(ctx context.Context, nodeID, targetID string) (time.Time, []byte, error) {
	var checkedAt int64
	var payload []byte
	if err := s.db.QueryRowContext(ctx, `SELECT checked_at, payload FROM network_results_latest WHERE node_id = ? AND target_id = ?`, nodeID, targetID).Scan(&checkedAt, &payload); err != nil {
		return time.Time{}, nil, err
	}
	return time.Unix(0, checkedAt).UTC(), payload, nil
}

func (s *Store) ListNetworkLatest(ctx context.Context, nodeID string) ([]LatestResult, error) {
	return s.listLatestResults(ctx, `SELECT target_id, checked_at, payload FROM network_results_latest WHERE node_id = ? ORDER BY target_id`, nodeID)
}

func (s *Store) UpsertMTRLatest(ctx context.Context, nodeID, targetID string, checkedAt time.Time, payload []byte) error {
	return s.upsertLatestResult(ctx, "mtr_results_latest", "target_id", nodeID, targetID, checkedAt, payload)
}

func (s *Store) GetMTRLatest(ctx context.Context, nodeID, targetID string) (time.Time, []byte, error) {
	var checkedAt int64
	var payload []byte
	if err := s.db.QueryRowContext(ctx, `SELECT checked_at, payload FROM mtr_results_latest WHERE node_id = ? AND target_id = ?`, nodeID, targetID).Scan(&checkedAt, &payload); err != nil {
		return time.Time{}, nil, err
	}
	return time.Unix(0, checkedAt).UTC(), payload, nil
}

func (s *Store) ListMTRLatest(ctx context.Context, nodeID string) ([]LatestResult, error) {
	return s.listLatestResults(ctx, `SELECT target_id, checked_at, payload FROM mtr_results_latest WHERE node_id = ? ORDER BY target_id`, nodeID)
}

func (s *Store) UpsertMediaLatest(ctx context.Context, nodeID, detectorID string, checkedAt time.Time, payload []byte) error {
	return s.upsertLatestResult(ctx, "media_results_latest", "detector_id", nodeID, detectorID, checkedAt, payload)
}

func (s *Store) GetMediaLatest(ctx context.Context, nodeID, detectorID string) (time.Time, []byte, error) {
	var checkedAt int64
	var payload []byte
	if err := s.db.QueryRowContext(ctx, `SELECT checked_at, payload FROM media_results_latest WHERE node_id = ? AND detector_id = ?`, nodeID, detectorID).Scan(&checkedAt, &payload); err != nil {
		return time.Time{}, nil, err
	}
	return time.Unix(0, checkedAt).UTC(), payload, nil
}

func (s *Store) ListMediaLatest(ctx context.Context, nodeID string) ([]LatestResult, error) {
	return s.listLatestResults(ctx, `SELECT detector_id, checked_at, payload FROM media_results_latest WHERE node_id = ? ORDER BY detector_id`, nodeID)
}

func (s *Store) listLatestResults(ctx context.Context, query string, nodeID string) ([]LatestResult, error) {
	rows, err := s.db.QueryContext(ctx, query, nodeID)
	if err != nil {
		return nil, fmt.Errorf("list latest results: %w", err)
	}
	defer rows.Close()
	results := make([]LatestResult, 0)
	for rows.Next() {
		var result LatestResult
		var checkedAt int64
		if err := rows.Scan(&result.ID, &checkedAt, &result.Payload); err != nil {
			return nil, fmt.Errorf("scan latest result: %w", err)
		}
		result.CheckedAt = time.Unix(0, checkedAt).UTC()
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read latest results: %w", err)
	}
	return results, nil
}

func (s *Store) upsertLatestResult(ctx context.Context, table, targetColumn, nodeID, targetID string, checkedAt time.Time, payload []byte) error {
	if !json.Valid(payload) {
		return errors.New("latest result payload must be valid JSON")
	}
	if len(payload) > maxResourcePayloadBytes {
		return errors.New("latest result payload exceeds 64 KiB")
	}
	var query string
	switch {
	case table == "network_results_latest" && targetColumn == "target_id":
		query = `INSERT INTO network_results_latest (node_id, target_id, payload, checked_at) VALUES (?, ?, ?, ?) ON CONFLICT(node_id, target_id) DO UPDATE SET payload = excluded.payload, checked_at = excluded.checked_at WHERE excluded.checked_at >= network_results_latest.checked_at`
	case table == "mtr_results_latest" && targetColumn == "target_id":
		query = `INSERT INTO mtr_results_latest (node_id, target_id, payload, checked_at) VALUES (?, ?, ?, ?) ON CONFLICT(node_id, target_id) DO UPDATE SET payload = excluded.payload, checked_at = excluded.checked_at WHERE excluded.checked_at >= mtr_results_latest.checked_at`
	case table == "media_results_latest" && targetColumn == "detector_id":
		query = `INSERT INTO media_results_latest (node_id, detector_id, payload, checked_at) VALUES (?, ?, ?, ?) ON CONFLICT(node_id, detector_id) DO UPDATE SET payload = excluded.payload, checked_at = excluded.checked_at WHERE excluded.checked_at >= media_results_latest.checked_at`
	default:
		return errors.New("invalid latest result table")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin latest result upsert: %w", err)
	}
	defer tx.Rollback()

	if err := ensureNodeInTx(ctx, tx, nodeID); err != nil {
		return fmt.Errorf("find latest result node: %w", err)
	}
	if err := upsertLatestResultTx(ctx, tx, query, nodeID, targetID, checkedAt, payload); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit latest result upsert: %w", err)
	}
	return nil
}

func (s *Store) PersistAgentReport(ctx context.Context, input AgentReportInput) error {
	if err := validateAgentReportInput(input); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin agent report persistence: %w", err)
	}
	defer tx.Rollback()
	if err := ensureNodeInTx(ctx, tx, input.NodeID); err != nil {
		return fmt.Errorf("find agent report node: %w", err)
	}
	if err := insertRequestReplayTx(ctx, tx, input.NodeID, input.RequestID, input.ReplayExpiresAt, input.Now); err != nil {
		return err
	}
	if err := upsertResourceLatestTx(ctx, tx, input.NodeID, input.ReportedAt, input.ResourcePayload); err != nil {
		return err
	}
	for _, result := range input.Results {
		if err := evaluateMTRPathChangeAlertTx(ctx, tx, input.NodeID, result, input.Now); err != nil {
			return err
		}
		if err := upsertAgentResultTx(ctx, tx, input.NodeID, result); err != nil {
			return err
		}
		if err := evaluateResultAlertTx(ctx, tx, input.NodeID, result, input.Now); err != nil {
			return err
		}
	}
	if err := evaluateResourceAlertTx(ctx, tx, input.NodeID, input.ResourcePayload, input.Now); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit agent report persistence: %w", err)
	}
	return nil
}

func (s *Store) PersistAgentResult(ctx context.Context, nodeID, requestID string, replayExpiresAt, now time.Time, result AgentResultInput) error {
	if err := validateAgentResultInput(result); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin agent result persistence: %w", err)
	}
	defer tx.Rollback()
	if err := ensureNodeInTx(ctx, tx, nodeID); err != nil {
		return fmt.Errorf("find agent result node: %w", err)
	}
	if err := insertRequestReplayTx(ctx, tx, nodeID, requestID, replayExpiresAt, now); err != nil {
		return err
	}
	if err := evaluateMTRPathChangeAlertTx(ctx, tx, nodeID, result, now); err != nil {
		return err
	}
	if err := upsertAgentResultTx(ctx, tx, nodeID, result); err != nil {
		return err
	}
	if err := evaluateResultAlertTx(ctx, tx, nodeID, result, now); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit agent result persistence: %w", err)
	}
	return nil
}

func validateAgentReportInput(input AgentReportInput) error {
	if strings.TrimSpace(input.NodeID) == "" || strings.TrimSpace(input.RequestID) == "" {
		return errors.New("agent report identity is required")
	}
	if err := validateJSONPayload(input.ResourcePayload, "resource"); err != nil {
		return err
	}
	for _, result := range input.Results {
		if err := validateAgentResultInput(result); err != nil {
			return err
		}
	}
	return nil
}

func validateAgentResultInput(input AgentResultInput) error {
	if err := validateTargetID(input.TargetID); err != nil {
		return err
	}
	if input.CheckedAt.IsZero() {
		return errors.New("agent result checked_at is required")
	}
	if err := validateJSONPayload(input.Payload, "latest result"); err != nil {
		return err
	}
	if _, err := latestResultQuery(input.Kind); err != nil {
		return err
	}
	return nil
}

func validateJSONPayload(payload []byte, name string) error {
	if !json.Valid(payload) {
		return fmt.Errorf("%s payload must be valid JSON", name)
	}
	if len(payload) > maxResourcePayloadBytes {
		return fmt.Errorf("%s payload exceeds 64 KiB", name)
	}
	return nil
}

func ensureNodeInTx(ctx context.Context, tx *sql.Tx, nodeID string) error {
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM nodes WHERE id = ?`, nodeID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return sql.ErrNoRows
	} else if err != nil {
		return err
	}
	return nil
}

func upsertResourceLatestTx(ctx context.Context, tx *sql.Tx, nodeID string, reportedAt time.Time, payload []byte) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO resource_history (node_id, payload, reported_at, recorded_at) VALUES (?, ?, ?, ?)`, nodeID, payload, unixNano(reportedAt), unixNano(time.Now().UTC())); err != nil {
		return fmt.Errorf("insert resource history: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO resource_latest (node_id, payload, reported_at)
		VALUES (?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET payload = excluded.payload, reported_at = excluded.reported_at
		WHERE excluded.reported_at >= resource_latest.reported_at`,
		nodeID, payload, unixNano(reportedAt)); err != nil {
		return fmt.Errorf("upsert resource latest: %w", err)
	}
	return nil
}

func evaluateResultAlertTx(ctx context.Context, tx *sql.Tx, nodeID string, result AgentResultInput, now time.Time) error {
	var raw struct {
		Status  string `json:"status"`
		Reached bool   `json:"reached"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(result.Payload, &raw); err != nil {
		return err
	}
	e := AlertEvaluation{TargetID: result.TargetID, Severity: AlertSeverityWarn}
	switch result.Kind {
	case TargetKindTCP, TargetKindHTTP, TargetKindHTTPS, TargetKindDNS:
		e.Category = "network"
		e.Failing = raw.Status == "timeout" || raw.Status == "blocked" || (raw.Status != "" && raw.Status != "success")
		e.Reason = raw.Status
	case TargetKindMediaHTTP:
		e.Category = "media"
		e.Failing = raw.Status != "available"
		e.Reason = raw.Status
	case TargetKindMTR:
		e.Category = "mtr"
		e.Failing = !raw.Reached || raw.Error == "unsupported" || raw.Error != ""
		e.Reason = raw.Error
		if e.Reason == "" && !raw.Reached {
			e.Reason = "unreached"
		}
	}
	if e.Reason == "" {
		e.Reason = "failure"
	}
	return evaluateAlertTx(ctx, tx, nodeID, e, now)
}

const alertReasonPathChanged = "path_changed"

// mtrPathHop mirrors the route fingerprint hop material: latency is
// deliberately excluded so only topology changes alter the fingerprint.
type mtrPathHop struct {
	TTL      int    `json:"ttl"`
	IP       string `json:"ip,omitempty"`
	TimedOut bool   `json:"timed_out,omitempty"`
}

// mtrPathSample is the route fingerprint of one MTR result payload. The
// fingerprint is empty when the payload carries no usable path data.
type mtrPathSample struct {
	fingerprint string
	reached     bool
}

// mtrPathSampleFromPayload derives the route fingerprint from an MTR result
// payload, mirroring the monitor-side fingerprint (destination, reached and
// per-hop TTL/IP/timed-out). The client-supplied fingerprint field is ignored
// so the comparison never mixes hash schemes.
func mtrPathSampleFromPayload(payload []byte) mtrPathSample {
	var parsed struct {
		DestinationIP string       `json:"destination_ip"`
		Hops          []mtrPathHop `json:"hops"`
		Reached       bool         `json:"reached"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil || len(parsed.Hops) == 0 {
		return mtrPathSample{}
	}
	encoded, err := json.Marshal(struct {
		Destination string       `json:"destination"`
		Reached     bool         `json:"reached"`
		Hops        []mtrPathHop `json:"hops"`
	}{parsed.DestinationIP, parsed.Reached, parsed.Hops})
	if err != nil {
		return mtrPathSample{}
	}
	sum := sha256.Sum256(encoded)
	return mtrPathSample{fingerprint: hex.EncodeToString(sum[:]), reached: parsed.Reached}
}

// evaluateMTRPathChangeAlertTx raises a transient "path_changed" event alert
// when an MTR result observes a different route than the stored latest for the
// same node+target. It must run before upsertAgentResultTx so the stored latest
// payload is still the previous observation. Both results must have reached the
// destination so first reports and unreachable routes never raise the alert,
// and results older than the stored latest are skipped because they would not
// replace it. The alert lives on its own fingerprint dimension, so the
// reachability evaluation never resolves it: a path change is an event, not a
// persistent failure state.
func evaluateMTRPathChangeAlertTx(ctx context.Context, tx *sql.Tx, nodeID string, result AgentResultInput, now time.Time) error {
	if result.Kind != TargetKindMTR {
		return nil
	}
	var checkedAt int64
	var previous []byte
	err := tx.QueryRowContext(ctx, `SELECT checked_at, payload FROM mtr_results_latest WHERE node_id = ? AND target_id = ?`, nodeID, result.TargetID).Scan(&checkedAt, &previous)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("find previous mtr result: %w", err)
	}
	if unixNano(result.CheckedAt) < checkedAt {
		return nil
	}
	previousPath := mtrPathSampleFromPayload(previous)
	currentPath := mtrPathSampleFromPayload(result.Payload)
	if previousPath.fingerprint == "" || currentPath.fingerprint == "" {
		return nil
	}
	if !previousPath.reached || !currentPath.reached {
		return nil
	}
	if previousPath.fingerprint == currentPath.fingerprint {
		return nil
	}
	return evaluateAlertTx(ctx, tx, nodeID, AlertEvaluation{
		Category:             "mtr",
		TargetID:             result.TargetID,
		Reason:               alertReasonPathChanged,
		Severity:             AlertSeverityWarn,
		Failing:              true,
		FingerprintDimension: alertReasonPathChanged,
	}, now)
}

func evaluateResourceAlertTx(ctx context.Context, tx *sql.Tx, nodeID string, payload []byte, now time.Time) error {
	var r struct {
		CPUPercent           float64 `json:"cpu_percent"`
		MemoryTotalBytes     uint64  `json:"memory_total_bytes"`
		MemoryUsedBytes      uint64  `json:"memory_used_bytes"`
		FilesystemTotalBytes uint64  `json:"filesystem_total_bytes"`
		FilesystemUsedBytes  uint64  `json:"filesystem_used_bytes"`
	}
	if err := json.Unmarshal(payload, &r); err != nil {
		return err
	}
	checks := []AlertEvaluation{{Category: "resource", TargetID: "cpu", Reason: "cpu_high", Severity: AlertSeverityCrit, Failing: r.CPUPercent > 90}, {Category: "resource", TargetID: "memory", Reason: "memory_high", Severity: AlertSeverityCrit, Failing: r.MemoryTotalBytes > 0 && float64(r.MemoryUsedBytes)/float64(r.MemoryTotalBytes)*100 > 90}, {Category: "resource", TargetID: "filesystem", Reason: "filesystem_high", Severity: AlertSeverityCrit, Failing: r.FilesystemTotalBytes > 0 && float64(r.FilesystemUsedBytes)/float64(r.FilesystemTotalBytes)*100 > 90}}
	for _, e := range checks {
		if err := evaluateAlertTx(ctx, tx, nodeID, e, now); err != nil {
			return err
		}
	}
	return nil
}

func alertFingerprint(nodeID string, e AlertEvaluation) string {
	dimension := ""
	if e.FingerprintDimension != "" {
		dimension = "\x00" + e.FingerprintDimension
	}
	h := sha256.Sum256([]byte(nodeID + "\x00" + e.Category + "\x00" + e.TargetID + dimension))
	return hex.EncodeToString(h[:])
}

func evaluateAlertTx(ctx context.Context, tx *sql.Tx, nodeID string, e AlertEvaluation, now time.Time) error {
	if strings.TrimSpace(e.Category) == "" || len(e.Category) > 64 || len(e.TargetID) > maxTargetIDLength || len(e.Reason) > 128 {
		return errors.New("alert fields out of bounds")
	}
	fp := alertFingerprint(nodeID, e)
	var lastSeen int64
	err := tx.QueryRowContext(ctx, `SELECT last_seen_at FROM alert_events WHERE node_id=? AND fingerprint=?`, nodeID, fp).Scan(&lastSeen)
	if err == nil && now.UnixNano() < lastSeen {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if e.Failing {
		return upsertAlertTx(ctx, tx, nodeID, fp, e, now)
	}
	return resolveAlertTx(ctx, tx, nodeID, fp, now)
}

func upsertAlertTx(ctx context.Context, tx *sql.Tx, nodeID, fingerprint string, e AlertEvaluation, now time.Time) error {
	id, err := randomID()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO alert_events (id,node_id,fingerprint,category,target_id,reason,severity,status,occurrence_count,first_seen_at,last_seen_at) VALUES (?,?,?,?,?,?,?,'open',1,?,?) ON CONFLICT(node_id,fingerprint) DO UPDATE SET severity=excluded.severity,status='open',occurrence_count=alert_events.occurrence_count+1,last_seen_at=excluded.last_seen_at,resolved_at=NULL`, id, nodeID, fingerprint, e.Category, e.TargetID, e.Reason, e.Severity, unixNano(now), unixNano(now))
	return err
}

func resolveAlertTx(ctx context.Context, tx *sql.Tx, nodeID, fingerprint string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `UPDATE alert_events SET status='resolved', resolved_at=?, last_seen_at=? WHERE node_id=? AND fingerprint=? AND status <> 'resolved'`, unixNano(now), unixNano(now), nodeID, fingerprint)
	return err
}

func (s *Store) EvaluateAlert(ctx context.Context, nodeID string, evaluation AlertEvaluation, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = ensureNodeInTx(ctx, tx, nodeID); err != nil {
		return err
	}
	if err = evaluateAlertTx(ctx, tx, nodeID, evaluation, now); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) UpsertAlert(ctx context.Context, nodeID string, evaluation AlertEvaluation, now time.Time) error {
	evaluation.Failing = true
	return s.EvaluateAlert(ctx, nodeID, evaluation, now)
}
func (s *Store) ResolveAlert(ctx context.Context, nodeID string, evaluation AlertEvaluation, now time.Time) error {
	evaluation.Failing = false
	return s.EvaluateAlert(ctx, nodeID, evaluation, now)
}

type AlertQuery struct {
	Statuses []string
	From     time.Time
	To       time.Time
	Limit    int
}

func (s *Store) ListAlerts(ctx context.Context, query AlertQuery) ([]AlertEvent, error) {
	if query.Limit <= 0 || query.Limit > 1000 {
		query.Limit = 100
	}
	args := make([]any, 0, len(query.Statuses)+3)
	where := make([]string, 0, 3)
	if len(query.Statuses) > 0 {
		marks := make([]string, len(query.Statuses))
		for i, status := range query.Statuses {
			marks[i] = "?"
			args = append(args, status)
		}
		where = append(where, "status IN ("+strings.Join(marks, ",")+")")
	}
	if !query.From.IsZero() {
		where = append(where, "last_seen_at >= ?")
		args = append(args, unixNano(query.From))
	}
	if !query.To.IsZero() {
		where = append(where, "last_seen_at <= ?")
		args = append(args, unixNano(query.To))
	}
	sqlQuery := `SELECT id,node_id,fingerprint,category,target_id,reason,severity,status,occurrence_count,first_seen_at,last_seen_at,resolved_at FROM alert_events`
	if len(where) > 0 {
		sqlQuery += " WHERE " + strings.Join(where, " AND ")
	}
	sqlQuery += " ORDER BY last_seen_at DESC, id DESC LIMIT ?"
	args = append(args, query.Limit)
	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	alerts := make([]AlertEvent, 0)
	for rows.Next() {
		var a AlertEvent
		var first, last int64
		var resolved sql.NullInt64
		if err := rows.Scan(&a.ID, &a.NodeID, &a.Fingerprint, &a.Category, &a.TargetID, &a.Reason, &a.Severity, &a.Status, &a.OccurrenceCount, &first, &last, &resolved); err != nil {
			return nil, err
		}
		a.FirstSeenAt = time.Unix(0, first).UTC()
		a.LastSeenAt = time.Unix(0, last).UTC()
		if resolved.Valid {
			t := time.Unix(0, resolved.Int64).UTC()
			a.ResolvedAt = &t
		}
		alerts = append(alerts, a)
	}
	return alerts, rows.Err()
}

func (s *Store) AckAlert(ctx context.Context, alertID, actorID string, now time.Time) (AlertEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AlertEvent{}, err
	}
	defer tx.Rollback()
	var a AlertEvent
	var first, last int64
	var resolved sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT id,node_id,fingerprint,category,target_id,reason,severity,status,occurrence_count,first_seen_at,last_seen_at,resolved_at FROM alert_events WHERE id = ?`, alertID).Scan(&a.ID, &a.NodeID, &a.Fingerprint, &a.Category, &a.TargetID, &a.Reason, &a.Severity, &a.Status, &a.OccurrenceCount, &first, &last, &resolved)
	if errors.Is(err, sql.ErrNoRows) {
		return AlertEvent{}, ErrAlertNotFound
	}
	if err != nil {
		return AlertEvent{}, err
	}
	if a.Status == AlertStatusResolved {
		return AlertEvent{}, ErrAlertResolved
	}
	if a.Status == AlertStatusOpen {
		if _, err = tx.ExecContext(ctx, `UPDATE alert_events SET status='acked' WHERE id=? AND status='open'`, alertID); err != nil {
			return AlertEvent{}, err
		}
		if err = insertAudit(ctx, tx, "ack", a.NodeID, actorID, now); err != nil {
			return AlertEvent{}, err
		}
		a.Status = AlertStatusAcked
	}
	a.FirstSeenAt = time.Unix(0, first).UTC()
	a.LastSeenAt = time.Unix(0, last).UTC()
	if resolved.Valid {
		t := time.Unix(0, resolved.Int64).UTC()
		a.ResolvedAt = &t
	}
	if err = tx.Commit(); err != nil {
		return AlertEvent{}, err
	}
	return a, nil
}

func upsertAgentResultTx(ctx context.Context, tx *sql.Tx, nodeID string, result AgentResultInput) error {
	query, err := latestResultQuery(result.Kind)

	if err != nil {
		return err
	}
	return upsertLatestResultTx(ctx, tx, query, nodeID, result.TargetID, result.CheckedAt, result.Payload)
}

func latestResultQuery(kind TargetKind) (string, error) {
	switch kind {
	case TargetKindTCP, TargetKindHTTP, TargetKindHTTPS, TargetKindDNS:
		return `INSERT INTO network_results_latest (node_id, target_id, payload, checked_at) VALUES (?, ?, ?, ?) ON CONFLICT(node_id, target_id) DO UPDATE SET payload = excluded.payload, checked_at = excluded.checked_at WHERE excluded.checked_at >= network_results_latest.checked_at`, nil
	case TargetKindMTR:
		return `INSERT INTO mtr_results_latest (node_id, target_id, payload, checked_at) VALUES (?, ?, ?, ?) ON CONFLICT(node_id, target_id) DO UPDATE SET payload = excluded.payload, checked_at = excluded.checked_at WHERE excluded.checked_at >= mtr_results_latest.checked_at`, nil
	case TargetKindMediaHTTP:
		return `INSERT INTO media_results_latest (node_id, detector_id, payload, checked_at) VALUES (?, ?, ?, ?) ON CONFLICT(node_id, detector_id) DO UPDATE SET payload = excluded.payload, checked_at = excluded.checked_at WHERE excluded.checked_at >= media_results_latest.checked_at`, nil
	default:
		return "", ErrTargetKindMismatch
	}
}

func upsertLatestResultTx(ctx context.Context, tx *sql.Tx, query, nodeID, targetID string, checkedAt time.Time, payload []byte) error {
	var historyTableName, historyColumn string
	switch {
	case strings.Contains(query, "network_results_latest"):
		historyTableName, historyColumn = "network_results_history", "target_id"
	case strings.Contains(query, "mtr_results_latest"):
		historyTableName, historyColumn = "mtr_results_history", "target_id"
	case strings.Contains(query, "media_results_latest"):
		historyTableName, historyColumn = "media_results_history", "detector_id"
	default:
		return errors.New("invalid latest result history table")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO `+historyTableName+` (node_id, `+historyColumn+`, payload, checked_at, recorded_at) VALUES (?, ?, ?, ?, ?)`, nodeID, targetID, payload, unixNano(checkedAt), unixNano(time.Now().UTC())); err != nil {
		return fmt.Errorf("insert result history: %w", err)
	}
	if _, err := tx.ExecContext(ctx, query, nodeID, targetID, payload, unixNano(checkedAt)); err != nil {
		return fmt.Errorf("upsert latest result: %w", err)
	}
	return nil
}

func (s *Store) AuthenticateNodeToken(ctx context.Context, plaintext string, now time.Time) (Node, error) {
	var node Node
	var storedDigest []byte
	var deletedAt sql.NullInt64
	var revokedAt sql.NullInt64
	var expiresAt int64
	err := s.db.QueryRowContext(ctx, `SELECT n.id, n.uuid, n.name, t.token_digest, n.deleted_at, t.revoked_at, t.expires_at FROM node_tokens t JOIN nodes n ON n.id = t.node_id WHERE t.token_digest = ? ORDER BY t.created_at DESC LIMIT 1`, security.Digest(s.pepper, plaintext)).Scan(&node.ID, &node.UUID, &node.Name, &storedDigest, &deletedAt, &revokedAt, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, ErrTokenInvalid
	}
	if err != nil {
		return Node{}, fmt.Errorf("authenticate node token: %w", err)
	}
	if !security.Verify(s.pepper, plaintext, storedDigest) {
		return Node{}, ErrTokenInvalid
	}
	if now.UnixNano() >= expiresAt {
		return Node{}, ErrTokenExpired
	}
	if deletedAt.Valid {
		return Node{}, ErrNodeDeleted
	}
	if revokedAt.Valid {
		return Node{}, ErrTokenRevoked
	}
	return node, nil
}

func (s *Store) RevokeNodeToken(ctx context.Context, nodeID, actorID string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE node_tokens SET revoked_at = ? WHERE node_id = ? AND revoked_at IS NULL`, unixNano(now), nodeID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect node token revoke: %w", err)
	}
	if affected == 0 {
		return ErrTokenRevoked
	}
	if err := insertAudit(ctx, tx, "revoke", nodeID, actorID, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RotateNodeToken(ctx context.Context, nodeID, actorID string, now time.Time) (string, error) {
	return s.RotateNodeTokenWithTTL(ctx, nodeID, actorID, now, DefaultNodeTokenLifetime)
}

func (s *Store) RotateNodeTokenWithTTL(ctx context.Context, nodeID, actorID string, now time.Time, lifetime time.Duration) (string, error) {
	if lifetime <= 0 {
		return "", errors.New("node token lifetime must be positive")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var deletedAt sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT deleted_at FROM nodes WHERE id = ?`, nodeID).Scan(&deletedAt); errors.Is(err, sql.ErrNoRows) {
		return "", ErrNodeDeleted
	} else if err != nil {
		return "", err
	}
	if deletedAt.Valid {
		return "", ErrNodeDeleted
	}
	newToken, err := security.GenerateToken()
	if err != nil {
		return "", err
	}
	result, err := tx.ExecContext(ctx, `UPDATE node_tokens SET revoked_at = ? WHERE node_id = ? AND revoked_at IS NULL`, unixNano(now), nodeID)
	if err != nil {
		return "", err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return "", fmt.Errorf("inspect node token rotation: %w", err)
	}
	if affected == 0 {
		return "", ErrTokenRevoked
	}
	tokenID, err := randomID()
	if err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO node_tokens (id, node_id, token_digest, token_prefix, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?)`, tokenID, nodeID, security.Digest(s.pepper, newToken), tokenPrefix(newToken), unixNano(now.Add(lifetime)), unixNano(now)); err != nil {
		return "", err
	}
	if err := insertAudit(ctx, tx, "rotate", nodeID, actorID, now); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return newToken, nil
}

func (s *Store) DeleteNode(ctx context.Context, nodeID, actorID string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE nodes SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`, unixNano(now), unixNano(now), nodeID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect node deletion: %w", err)
	}
	if affected == 0 {
		return ErrNodeDeleted
	}
	if _, err := tx.ExecContext(ctx, `UPDATE node_tokens SET revoked_at = ? WHERE node_id = ? AND revoked_at IS NULL`, unixNano(now), nodeID); err != nil {
		return err
	}
	if err := insertAudit(ctx, tx, "delete", nodeID, actorID, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) InsertRequestReplay(ctx context.Context, nodeID, requestID string, expiresAt, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin request replay insert: %w", err)
	}
	defer tx.Rollback()
	if err := insertRequestReplayTx(ctx, tx, nodeID, requestID, expiresAt, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit request replay insert: %w", err)
	}
	return nil
}

func insertRequestReplayTx(ctx context.Context, tx *sql.Tx, nodeID, requestID string, expiresAt, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM request_replays
		WHERE rowid IN (
			SELECT rowid FROM request_replays
			WHERE expires_at <= ?
			ORDER BY expires_at, rowid
			LIMIT ?
		)`, unixNano(now), maxReplayCleanupRows); err != nil {
		return fmt.Errorf("cleanup expired request replays: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO request_replays (node_id, request_id, expires_at, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(node_id, request_id) DO UPDATE SET
			expires_at = excluded.expires_at,
			created_at = excluded.created_at
		WHERE request_replays.expires_at <= excluded.created_at`, nodeID, requestID, unixNano(expiresAt), unixNano(now))
	if err != nil {
		return fmt.Errorf("insert request replay: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect request replay insert: %w", err)
	}
	if affected == 0 {
		return ErrReplay
	}
	if affected != 1 {
		return fmt.Errorf("insert request replay affected %d rows", affected)
	}
	return nil
}

func (s *Store) CleanupExpiredReplays(ctx context.Context, now time.Time) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin replay cleanup: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		DELETE FROM request_replays
		WHERE rowid IN (
			SELECT rowid FROM request_replays
			WHERE expires_at <= ?
			ORDER BY expires_at, rowid
			LIMIT ?
		)`, unixNano(now), maxReplayCleanupRows)
	if err != nil {
		return 0, fmt.Errorf("cleanup request replays: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("inspect replay cleanup: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit replay cleanup: %w", err)
	}
	return deleted, nil
}

func insertAudit(ctx context.Context, tx *sql.Tx, action, nodeID, actorID string, now time.Time) error {
	id, err := randomID()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_events (id, action, node_id, actor_id, created_at) VALUES (?, ?, ?, ?, ?)`, id, action, nodeID, actorID, unixNano(now))
	return err
}

func randomID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate ID: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

func unixNano(value time.Time) int64 { return value.UTC().UnixNano() }

func sqliteDSN(databasePath string) string {
	return databasePath + "?_txlock=immediate&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
}

// Backup creates a consistent online SQLite backup without copying live WAL files.
func (s *Store) Backup(ctx context.Context, destination string) error {
	if strings.TrimSpace(destination) == "" {
		return errors.New("backup destination must not be empty")
	}
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return fmt.Errorf("create backup directory: %w", err)
	}
	if err := os.Chmod(parent, 0700); err != nil {
		return fmt.Errorf("set backup directory permissions: %w", err)
	}
	file, err := os.OpenFile(destination, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create backup file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close backup file: %w", err)
	}
	if err := os.Chmod(destination, 0600); err != nil {
		return fmt.Errorf("set backup file permissions: %w", err)
	}

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire backup connection: %w", err)
	}
	defer conn.Close()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := conn.Raw(func(driverConn any) error {
		backuper, ok := driverConn.(interface {
			NewBackup(string) (*moderncsqlite.Backup, error)
		})
		if !ok {
			return errors.New("SQLite driver does not support online backup")
		}
		backup, err := backuper.NewBackup(destination)
		if err != nil {
			return fmt.Errorf("start SQLite backup: %w", err)
		}
		finished := false
		defer func() {
			if !finished {
				_ = backup.Finish()
			}
		}()
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			more, err := backup.Step(128)
			if err != nil {
				return fmt.Errorf("copy SQLite backup pages: %w", err)
			}
			if !more {
				if err := backup.Finish(); err != nil {
					return fmt.Errorf("finish SQLite backup: %w", err)
				}
				finished = true
				return nil
			}
		}
	}); err != nil {
		return err
	}
	if err := os.Chmod(destination, 0600); err != nil {
		return fmt.Errorf("restore backup file permissions: %w", err)
	}
	return nil
}

func tokenPrefix(token string) string {
	if len(token) > 8 {
		return token[:8]
	}
	return token
}

type NotificationChannel struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"` // "telegram", "discord", "wecom", "bark", "webhook"
	Config    string    `json:"config"`
	Enabled   bool      `json:"enabled"`
	Events    string    `json:"events"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (s *Store) ListNotificationChannels(ctx context.Context) ([]NotificationChannel, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, type, config, enabled, events, created_at, updated_at FROM notification_channels ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list notification channels: %w", err)
	}
	defer rows.Close()

	channels := make([]NotificationChannel, 0)
	for rows.Next() {
		var ch NotificationChannel
		var enabled int
		var createdAt, updatedAt int64
		if err := rows.Scan(&ch.ID, &ch.Name, &ch.Type, &ch.Config, &enabled, &ch.Events, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan notification channel: %w", err)
		}
		ch.Enabled = enabled != 0
		ch.CreatedAt = time.Unix(0, createdAt).UTC()
		ch.UpdatedAt = time.Unix(0, updatedAt).UTC()
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

func (s *Store) GetNotificationChannel(ctx context.Context, id string) (NotificationChannel, error) {
	var ch NotificationChannel
	var enabled int
	var createdAt, updatedAt int64
	err := s.db.QueryRowContext(ctx, `SELECT id, name, type, config, enabled, events, created_at, updated_at FROM notification_channels WHERE id = ?`, id).
		Scan(&ch.ID, &ch.Name, &ch.Type, &ch.Config, &enabled, &ch.Events, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return NotificationChannel{}, errors.New("notification channel not found")
	}
	if err != nil {
		return NotificationChannel{}, fmt.Errorf("get notification channel: %w", err)
	}
	ch.Enabled = enabled != 0
	ch.CreatedAt = time.Unix(0, createdAt).UTC()
	ch.UpdatedAt = time.Unix(0, updatedAt).UTC()
	return ch, nil
}

func (s *Store) UpsertNotificationChannel(ctx context.Context, ch NotificationChannel) error {
	now := time.Now().UTC()
	enabledInt := 0
	if ch.Enabled {
		enabledInt = 1
	}
	events := ch.Events
	if events == "" {
		events = "[]"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO notification_channels (id, name, type, config, enabled, events, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			type = excluded.type,
			config = excluded.config,
			enabled = excluded.enabled,
			events = excluded.events,
			updated_at = excluded.updated_at
	`, ch.ID, ch.Name, ch.Type, ch.Config, enabledInt, events, unixNano(now), unixNano(now))
	if err != nil {
		return fmt.Errorf("upsert notification channel: %w", err)
	}
	return nil
}

func (s *Store) DeleteNotificationChannel(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM notification_channels WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete notification channel: %w", err)
	}
	return nil
}

func (s *Store) GetSetting(ctx context.Context, key string, fallback string) (string, error) {
	var val string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM system_settings WHERE key = ?`, key).Scan(&val)
	if errors.Is(err, sql.ErrNoRows) {
		return fallback, nil
	}
	if err != nil {
		return fallback, err
	}
	return val, nil
}

func (s *Store) SetSetting(ctx context.Context, key, val string) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO system_settings (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, key, val, unixNano(now))
	if err != nil {
		return fmt.Errorf("set setting %s: %w", key, err)
	}
	return nil
}

func (s *Store) GetAllSettings(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM system_settings`)
	if err != nil {
		return nil, fmt.Errorf("get all settings: %w", err)
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		settings[k] = v
	}
	return settings, rows.Err()
}

