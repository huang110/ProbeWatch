package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var (
	ErrWebAuthnChallengeNotFound = errors.New("webauthn challenge not found or expired")
	ErrWebAuthnCredentialNotFound = errors.New("webauthn credential not found")
)

type WebAuthnCredential struct {
	ID           string     `json:"id"`
	AdminID      string     `json:"admin_id"`
	Name         string     `json:"name"`
	CredentialID []byte     `json:"credential_id"`
	PublicKey    []byte     `json:"public_key"`
	Algorithm    int64      `json:"algorithm"`
	SignCount    uint32     `json:"sign_count"`
	AAGUID       []byte     `json:"aaguid"`
	CreatedAt    time.Time  `json:"created_at"`
	LastUsedAt   *time.Time `json:"last_used_at"`
}

// SaveWebAuthnChallenge persists a short-lived cryptographic challenge.
func (s *Store) SaveWebAuthnChallenge(ctx context.Context, id string, challenge []byte, challengeType, adminID string, expiresAt time.Time) error {
	now := time.Now().UTC()
	// Opportunistically prune expired challenges
	_, _ = s.db.ExecContext(ctx, `DELETE FROM webauthn_challenges WHERE expires_at < ?`, unixNano(now))

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO webauthn_challenges (id, challenge, challenge_type, admin_id, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, id, challenge, challengeType, adminID, unixNano(now), unixNano(expiresAt))
	if err != nil {
		return fmt.Errorf("save webauthn challenge: %w", err)
	}
	return nil
}

// GetAndConsumeWebAuthnChallenge retrieves and immediately consumes a challenge for single-use verification.
func (s *Store) GetAndConsumeWebAuthnChallenge(ctx context.Context, id, challengeType string, now time.Time) ([]byte, string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, "", fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var challenge []byte
	var adminID string
	var expiresAt int64

	err = tx.QueryRowContext(ctx, `
		SELECT challenge, admin_id, expires_at
		FROM webauthn_challenges
		WHERE id = ? AND challenge_type = ?
	`, id, challengeType).Scan(&challenge, &adminID, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrWebAuthnChallengeNotFound
	}
	if err != nil {
		return nil, "", fmt.Errorf("query challenge: %w", err)
	}

	if unixNano(now) > expiresAt {
		_, _ = tx.ExecContext(ctx, `DELETE FROM webauthn_challenges WHERE id = ?`, id)
		_ = tx.Commit()
		return nil, "", ErrWebAuthnChallengeNotFound
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM webauthn_challenges WHERE id = ?`, id); err != nil {
		return nil, "", fmt.Errorf("consume challenge: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, "", fmt.Errorf("commit consume challenge: %w", err)
	}

	return challenge, adminID, nil
}

// CreateWebAuthnCredential stores a newly registered passkey / security key.
func (s *Store) CreateWebAuthnCredential(ctx context.Context, cred WebAuthnCredential) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO webauthn_credentials (id, admin_id, name, credential_id, public_key, algorithm, sign_count, aaguid, created_at, last_used_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, cred.ID, cred.AdminID, cred.Name, cred.CredentialID, cred.PublicKey, cred.Algorithm, cred.SignCount, cred.AAGUID, unixNano(cred.CreatedAt), nil)
	if err != nil {
		return fmt.Errorf("insert webauthn credential: %w", err)
	}
	return nil
}

// ListWebAuthnCredentials lists all registered passkeys for an admin.
func (s *Store) ListWebAuthnCredentials(ctx context.Context, adminID string) ([]WebAuthnCredential, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, admin_id, name, credential_id, public_key, algorithm, sign_count, aaguid, created_at, last_used_at
		FROM webauthn_credentials
		WHERE admin_id = ?
		ORDER BY created_at DESC
	`, adminID)
	if err != nil {
		return nil, fmt.Errorf("list webauthn credentials: %w", err)
	}
	defer rows.Close()

	var list []WebAuthnCredential
	for rows.Next() {
		var c WebAuthnCredential
		var createdAt int64
		var lastUsedAt sql.NullInt64
		if err := rows.Scan(&c.ID, &c.AdminID, &c.Name, &c.CredentialID, &c.PublicKey, &c.Algorithm, &c.SignCount, &c.AAGUID, &createdAt, &lastUsedAt); err != nil {
			return nil, fmt.Errorf("scan webauthn credential: %w", err)
		}
		c.CreatedAt = time.Unix(0, createdAt).UTC()
		if lastUsedAt.Valid {
			t := time.Unix(0, lastUsedAt.Int64).UTC()
			c.LastUsedAt = &t
		}
		list = append(list, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows err: %w", err)
	}
	return list, nil
}

// GetWebAuthnCredentialByCredID looks up a credential by its raw credential ID (used during assertion / login).
func (s *Store) GetWebAuthnCredentialByCredID(ctx context.Context, credentialID []byte) (WebAuthnCredential, error) {
	var c WebAuthnCredential
	var createdAt int64
	var lastUsedAt sql.NullInt64

	err := s.db.QueryRowContext(ctx, `
		SELECT id, admin_id, name, credential_id, public_key, algorithm, sign_count, aaguid, created_at, last_used_at
		FROM webauthn_credentials
		WHERE credential_id = ?
	`, credentialID).Scan(&c.ID, &c.AdminID, &c.Name, &c.CredentialID, &c.PublicKey, &c.Algorithm, &c.SignCount, &c.AAGUID, &createdAt, &lastUsedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return WebAuthnCredential{}, ErrWebAuthnCredentialNotFound
	}
	if err != nil {
		return WebAuthnCredential{}, fmt.Errorf("query webauthn credential: %w", err)
	}
	c.CreatedAt = time.Unix(0, createdAt).UTC()
	if lastUsedAt.Valid {
		t := time.Unix(0, lastUsedAt.Int64).UTC()
		c.LastUsedAt = &t
	}
	return c, nil
}

// DeleteWebAuthnCredential removes a passkey.
func (s *Store) DeleteWebAuthnCredential(ctx context.Context, id, adminID string) error {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM webauthn_credentials
		WHERE id = ? AND admin_id = ?
	`, id, adminID)
	if err != nil {
		return fmt.Errorf("delete webauthn credential: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrWebAuthnCredentialNotFound
	}
	return nil
}

// RenameWebAuthnCredential updates the human-readable label for a passkey.
func (s *Store) RenameWebAuthnCredential(ctx context.Context, id, adminID, name string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE webauthn_credentials
		SET name = ?
		WHERE id = ? AND admin_id = ?
	`, name, id, adminID)
	if err != nil {
		return fmt.Errorf("rename webauthn credential: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrWebAuthnCredentialNotFound
	}
	return nil
}

// UpdateWebAuthnCredentialUsage increments the sign count and updates last_used_at.
func (s *Store) UpdateWebAuthnCredentialUsage(ctx context.Context, id string, signCount uint32, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE webauthn_credentials
		SET sign_count = ?, last_used_at = ?
		WHERE id = ?
	`, signCount, unixNano(now), id)
	if err != nil {
		return fmt.Errorf("update webauthn credential usage: %w", err)
	}
	return nil
}
