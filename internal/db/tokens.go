package db

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrAPITokenNotFound = errors.New("api token not found")
	ErrAPITokenExpired  = errors.New("api token expired")
	ErrAPITokenDisabled = errors.New("api token disabled")
	ErrAPITokenInvalid  = errors.New("invalid api token")
)

const (
	APITokenPrefix = "pbw_pat_"
)

// APIToken represents a developer personal access token stored in the database.
type APIToken struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	TokenPrefix  string     `json:"token_prefix"`
	TokenHash    string     `json:"-"`
	UserID       string     `json:"user_id"`
	Role         string     `json:"role"`
	Scopes       string     `json:"scopes"`
	AllowedNodes string     `json:"allowed_nodes"`
	ExpiresAt    *time.Time `json:"expires_at"`
	LastUsedAt   *time.Time `json:"last_used_at"`
	Disabled     bool       `json:"disabled"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// CreateAPITokenInput represents the parameter bag for creating a new API token.
type CreateAPITokenInput struct {
	Name         string
	UserID       string
	Role         string
	Scopes       string
	AllowedNodes string
	ExpiresIn    *time.Duration
}

// HashAPIToken computes the SHA-256 hex digest of a raw token string.
func HashAPIToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// HasScope checks whether a token's comma-separated scopes satisfy the required scope.
func HasScope(scopes, required string) bool {
	scopes = strings.TrimSpace(scopes)
	if scopes == "" || scopes == "*" {
		return true
	}
	parts := strings.Split(scopes, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "*" || p == required {
			return true
		}
		// Prefix wildcard match, e.g. "read:*" matches "read:nodes"
		if strings.HasSuffix(p, ":*") {
			prefix := strings.TrimSuffix(p, ":*")
			if strings.HasPrefix(required, prefix+":") {
				return true
			}
		}
	}
	return false
}

// CreateAPIToken generates a new secure API token, saves its hash, and returns the APIToken record and raw token string.
func (s *Store) CreateAPIToken(ctx context.Context, in CreateAPITokenInput, now time.Time) (*APIToken, string, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return nil, "", errors.New("token name is required")
	}
	if in.UserID == "" {
		return nil, "", errors.New("user ID is required")
	}

	role := strings.ToLower(strings.TrimSpace(in.Role))
	if role == "" {
		role = RoleOperator
	}
	if role != RoleAdmin && role != RoleOperator && role != RoleViewer {
		return nil, "", fmt.Errorf("invalid token role %q", in.Role)
	}

	scopes := strings.TrimSpace(in.Scopes)
	if scopes == "" {
		scopes = "*"
	}

	allowedNodes := strings.TrimSpace(in.AllowedNodes)
	if allowedNodes == "" {
		allowedNodes = "*"
	}

	// Generate 32 bytes of secure random material (64 hex characters)
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, "", fmt.Errorf("generate random token bytes: %w", err)
	}
	rawHex := hex.EncodeToString(tokenBytes)
	rawToken := APITokenPrefix + rawHex

	// Token prefix for identification in the UI: pbw_pat_ + first 8 hex chars + ...
	displayPrefix := APITokenPrefix + rawHex[:8] + "..."

	tokenHash := HashAPIToken(rawToken)

	var expiresAt *time.Time
	if in.ExpiresIn != nil && *in.ExpiresIn > 0 {
		exp := now.Add(*in.ExpiresIn)
		expiresAt = &exp
	}

	tokenIDBytes := make([]byte, 16)
	if _, err := rand.Read(tokenIDBytes); err != nil {
		return nil, "", fmt.Errorf("generate token id: %w", err)
	}
	id := hex.EncodeToString(tokenIDBytes)

	var expiresAtSec sql.NullInt64
	if expiresAt != nil {
		expiresAtSec = sql.NullInt64{Int64: expiresAt.Unix(), Valid: true}
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO api_tokens (
			id, name, token_prefix, token_hash, user_id, role,
			scopes, allowed_nodes, expires_at, last_used_at, disabled,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, 0, ?, ?)
	`, id, in.Name, displayPrefix, tokenHash, in.UserID, role,
		scopes, allowedNodes, expiresAtSec, now.Unix(), now.Unix())
	if err != nil {
		return nil, "", fmt.Errorf("insert api token: %w", err)
	}

	token := &APIToken{
		ID:           id,
		Name:         in.Name,
		TokenPrefix:  displayPrefix,
		TokenHash:    tokenHash,
		UserID:       in.UserID,
		Role:         role,
		Scopes:       scopes,
		AllowedNodes: allowedNodes,
		ExpiresAt:    expiresAt,
		LastUsedAt:   nil,
		Disabled:     false,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	return token, rawToken, nil
}

// GetAPITokenByHash finds a token by its SHA-256 hash.
func (s *Store) GetAPITokenByHash(ctx context.Context, hash string) (*APIToken, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, token_prefix, token_hash, user_id, role,
		       scopes, allowed_nodes, expires_at, last_used_at, disabled,
		       created_at, updated_at
		FROM api_tokens
		WHERE token_hash = ?
	`, hash)

	var t APIToken
	var expSec, usedSec sql.NullInt64
	var disabledInt int
	var createdSec, updatedSec int64

	err := row.Scan(
		&t.ID, &t.Name, &t.TokenPrefix, &t.TokenHash, &t.UserID, &t.Role,
		&t.Scopes, &t.AllowedNodes, &expSec, &usedSec, &disabledInt,
		&createdSec, &updatedSec,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAPITokenNotFound
		}
		return nil, fmt.Errorf("query api token: %w", err)
	}

	t.Disabled = disabledInt != 0
	t.CreatedAt = time.Unix(createdSec, 0).UTC()
	t.UpdatedAt = time.Unix(updatedSec, 0).UTC()
	if expSec.Valid {
		exp := time.Unix(expSec.Int64, 0).UTC()
		t.ExpiresAt = &exp
	}
	if usedSec.Valid {
		used := time.Unix(usedSec.Int64, 0).UTC()
		t.LastUsedAt = &used
	}

	return &t, nil
}

// ListAPITokens returns all tokens for a user (or all tokens if isAdmin is true and userID is empty).
func (s *Store) ListAPITokens(ctx context.Context, userID string, isAdmin bool) ([]APIToken, error) {
	var query string
	var args []interface{}

	if isAdmin && userID == "" {
		query = `
			SELECT id, name, token_prefix, token_hash, user_id, role,
			       scopes, allowed_nodes, expires_at, last_used_at, disabled,
			       created_at, updated_at
			FROM api_tokens
			ORDER BY created_at DESC
		`
	} else {
		query = `
			SELECT id, name, token_prefix, token_hash, user_id, role,
			       scopes, allowed_nodes, expires_at, last_used_at, disabled,
			       created_at, updated_at
			FROM api_tokens
			WHERE user_id = ?
			ORDER BY created_at DESC
		`
		args = append(args, userID)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list api tokens: %w", err)
	}
	defer rows.Close()

	var tokens []APIToken
	for rows.Next() {
		var t APIToken
		var expSec, usedSec sql.NullInt64
		var disabledInt int
		var createdSec, updatedSec int64

		if err := rows.Scan(
			&t.ID, &t.Name, &t.TokenPrefix, &t.TokenHash, &t.UserID, &t.Role,
			&t.Scopes, &t.AllowedNodes, &expSec, &usedSec, &disabledInt,
			&createdSec, &updatedSec,
		); err != nil {
			return nil, fmt.Errorf("scan api token: %w", err)
		}

		t.Disabled = disabledInt != 0
		t.CreatedAt = time.Unix(createdSec, 0).UTC()
		t.UpdatedAt = time.Unix(updatedSec, 0).UTC()
		if expSec.Valid {
			exp := time.Unix(expSec.Int64, 0).UTC()
			t.ExpiresAt = &exp
		}
		if usedSec.Valid {
			used := time.Unix(usedSec.Int64, 0).UTC()
			t.LastUsedAt = &used
		}

		tokens = append(tokens, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate api tokens: %w", err)
	}

	return tokens, nil
}

// UpdateAPITokenLastUsed updates the last_used_at timestamp of a token.
func (s *Store) UpdateAPITokenLastUsed(ctx context.Context, id string, t time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE api_tokens
		SET last_used_at = ?
		WHERE id = ?
	`, t.Unix(), id)
	return err
}

// ToggleAPITokenDisabled enables or disables an API token.
func (s *Store) ToggleAPITokenDisabled(ctx context.Context, id string, disabled bool, userID string, isAdmin bool) error {
	var query string
	var args []interface{}
	disVal := 0
	if disabled {
		disVal = 1
	}

	if isAdmin {
		query = `UPDATE api_tokens SET disabled = ?, updated_at = ? WHERE id = ?`
		args = []interface{}{disVal, time.Now().UTC().Unix(), id}
	} else {
		query = `UPDATE api_tokens SET disabled = ?, updated_at = ? WHERE id = ? AND user_id = ?`
		args = []interface{}{disVal, time.Now().UTC().Unix(), id, userID}
	}

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("toggle api token disabled: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrAPITokenNotFound
	}
	return nil
}

// DeleteAPIToken deletes an API token.
func (s *Store) DeleteAPIToken(ctx context.Context, id string, userID string, isAdmin bool) error {
	var query string
	var args []interface{}

	if isAdmin {
		query = `DELETE FROM api_tokens WHERE id = ?`
		args = []interface{}{id}
	} else {
		query = `DELETE FROM api_tokens WHERE id = ? AND user_id = ?`
		args = []interface{}{id, userID}
	}

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("delete api token: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrAPITokenNotFound
	}
	return nil
}
