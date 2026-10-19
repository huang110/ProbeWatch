package db

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/security"
)

const (
	RoleAdmin    = "admin"
	RoleOperator = "operator"
	RoleViewer   = "viewer"
)

const (
	passwordHashPrefix      = "pbkdf2-sha256"
	passwordHashIterations  = 210000
	passwordHashSaltBytes   = 16
	passwordHashOutputBytes = 32
)

var (
	ErrUserNotFound        = errors.New("user not found")
	ErrUserAlreadyExists   = errors.New("user with this login already exists")
	ErrCannotDeleteSelf    = errors.New("cannot delete current logged-in user")
	ErrLastAdminProtection = errors.New("cannot demote or disable the last active administrator")
	ErrAccountDisabled     = errors.New("user account is disabled")
	ErrInvalidCredentials  = errors.New("invalid credentials")
)

type CreateUserInput struct {
	Login        string `json:"login"`
	Password     string `json:"password"`
	DisplayName  string `json:"display_name"`
	Role         string `json:"role"`
	AllowedNodes string `json:"allowed_nodes"`
}

type UpdateUserInput struct {
	DisplayName  *string `json:"display_name"`
	Role         *string `json:"role"`
	AllowedNodes *string `json:"allowed_nodes"`
	Disabled     *bool   `json:"disabled"`
	NewPassword  *string `json:"new_password"`
}

// CanAccessNode returns whether the user has permission to view/operate on a given node UUID.
func (u AdminUser) CanAccessNode(nodeUUID string) bool {
	// Role and node scope are independent dimensions. An administrator with
	// an explicit allow-list (including a PAT-derived intersection) must not
	// regain fleet-wide access merely because of the role.
	if strings.TrimSpace(u.AllowedNodes) == "*" {
		return true
	}
	if strings.TrimSpace(u.AllowedNodes) == "" {
		return false
	}
	parts := strings.Split(u.AllowedNodes, ",")
	for _, p := range parts {
		if strings.TrimSpace(p) == nodeUUID {
			return true
		}
	}
	return false
}

// CanWrite returns whether the user has general write permissions.
func (u AdminUser) CanWrite() bool {
	return !u.Disabled && (u.Role == RoleAdmin || u.Role == RoleOperator)
}

// IsAdmin returns true if the user is an active administrator.
func (u AdminUser) IsAdmin() bool {
	return !u.Disabled && u.Role == RoleAdmin
}

// IsOperator returns true if the user is an active operator.
func (u AdminUser) IsOperator() bool {
	return !u.Disabled && u.Role == RoleOperator
}

// IsViewer returns true if the user is a read-only viewer.
func (u AdminUser) IsViewer() bool {
	return u.Role == RoleViewer
}

// DisplayNameOrLogin returns the user's display name if non-empty, otherwise login or provider user ID.
func (u AdminUser) DisplayNameOrLogin() string {
	if strings.TrimSpace(u.DisplayName) != "" {
		return u.DisplayName
	}
	if strings.TrimSpace(u.Login) != "" {
		return u.Login
	}
	return u.ProviderUserID
}

// HashUserPassword generates a deliberately slow, salted password digest using
// PBKDF2-HMAC-SHA256 and the server pepper. The versioned format lets old
// installations transparently upgrade legacy fast hashes after a successful
// login without invalidating existing accounts.
func HashUserPassword(password string, pepper []byte) (string, error) {
	salt := make([]byte, passwordHashSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	derived := derivePasswordKey(password, pepper, salt, passwordHashIterations, passwordHashOutputBytes)
	return fmt.Sprintf("%s$%d$%s$%s", passwordHashPrefix, passwordHashIterations, hex.EncodeToString(salt), hex.EncodeToString(derived)), nil
}

// VerifyUserPassword checks the given password against a salted digest.
func VerifyUserPassword(storedHash, password string, pepper []byte) bool {
	parts := strings.Split(storedHash, "$")
	if len(parts) == 4 && parts[0] == passwordHashPrefix {
		iterations, err := strconv.Atoi(parts[1])
		if err != nil || iterations < 10000 || iterations > 1000000 {
			return false
		}
		salt, err := hex.DecodeString(parts[2])
		if err != nil || len(salt) < 16 || len(salt) > 64 {
			return false
		}
		expectedHash, err := hex.DecodeString(parts[3])
		if err != nil || len(expectedHash) != passwordHashOutputBytes {
			return false
		}
		actualHash := derivePasswordKey(password, pepper, salt, iterations, len(expectedHash))
		return subtle.ConstantTimeCompare(actualHash, expectedHash) == 1
	}

	// Legacy format: <salt-hex>$<HMAC-SHA256>. Keep this branch solely for
	// migration; successful authentication upgrades it to PBKDF2 below.
	if len(parts) != 2 {
		return false
	}
	salt, err := hex.DecodeString(parts[0])
	if err != nil || len(salt) == 0 {
		return false
	}
	expectedHash, err := hex.DecodeString(parts[1])
	if err != nil || len(expectedHash) != sha256.Size {
		return false
	}
	actualHash := security.Digest(append(append([]byte(nil), salt...), pepper...), password)
	return subtle.ConstantTimeCompare(actualHash, expectedHash) == 1
}

func derivePasswordKey(password string, pepper, salt []byte, iterations, outputLen int) []byte {
	material := make([]byte, 0, len(password)+1+len(pepper))
	material = append(material, password...)
	material = append(material, 0)
	material = append(material, pepper...)
	return pbkdf2HMACSHA256(material, salt, iterations, outputLen)
}

// pbkdf2HMACSHA256 is kept local to avoid pulling a large crypto dependency
// into the small agent/control-plane binaries.
func pbkdf2HMACSHA256(password, salt []byte, iterations, keyLen int) []byte {
	if iterations <= 0 || keyLen <= 0 {
		return nil
	}
	blocks := (keyLen + sha256.Size - 1) / sha256.Size
	out := make([]byte, 0, blocks*sha256.Size)
	for block := 1; block <= blocks; block++ {
		mac := hmac.New(sha256.New, password)
		_, _ = mac.Write(salt)
		mac.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for i := 1; i < iterations; i++ {
			mac = hmac.New(sha256.New, password)
			_, _ = mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		out = append(out, t...)
	}
	return out[:keyLen]
}

// ListAdminUsers returns all registered administrators, operators, and viewers.
func (s *Store) ListAdminUsers(ctx context.Context) ([]AdminUser, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, provider, provider_user_id, login,
		       COALESCE(role, 'admin'),
		       COALESCE(display_name, ''),
		       COALESCE(allowed_nodes, '*'),
		       COALESCE(disabled, 0),
		       created_at, updated_at
		FROM admin_users
		ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list admin users: %w", err)
	}
	defer rows.Close()

	var users []AdminUser
	for rows.Next() {
		var u AdminUser
		var disabledInt int
		var createdAtNano, updatedAtNano int64
		if err := rows.Scan(
			&u.ID, &u.Provider, &u.ProviderUserID, &u.Login,
			&u.Role, &u.DisplayName, &u.AllowedNodes,
			&disabledInt, &createdAtNano, &updatedAtNano,
		); err != nil {
			return nil, fmt.Errorf("scan admin user: %w", err)
		}
		u.Disabled = disabledInt != 0
		u.CreatedAt = time.Unix(0, createdAtNano).UTC()
		u.UpdatedAt = time.Unix(0, updatedAtNano).UTC()
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate admin users: %w", err)
	}
	return users, nil
}

// CreateLocalUser creates a new local user with role and node scoping.
func (s *Store) CreateLocalUser(ctx context.Context, input CreateUserInput, now time.Time) (AdminUser, error) {
	login := strings.TrimSpace(input.Login)
	if login == "" {
		return AdminUser{}, errors.New("login username cannot be empty")
	}
	if len(input.Password) < 6 {
		return AdminUser{}, errors.New("password must be at least 6 characters")
	}
	role := strings.ToLower(strings.TrimSpace(input.Role))
	if role != RoleAdmin && role != RoleOperator && role != RoleViewer {
		role = RoleViewer
	}
	allowedNodes := strings.TrimSpace(input.AllowedNodes)
	if allowedNodes == "" {
		allowedNodes = "*"
	}

	// Check login uniqueness
	var count int
	_ = s.db.QueryRowContext(ctx, `SELECT count(*) FROM admin_users WHERE login = ?`, login).Scan(&count)
	if count > 0 {
		return AdminUser{}, ErrUserAlreadyExists
	}

	pwHash, err := HashUserPassword(input.Password, s.pepper)
	if err != nil {
		return AdminUser{}, err
	}

	id, err := randomID()
	if err != nil {
		return AdminUser{}, err
	}

	nowNano := unixNano(now)
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO admin_users (
			id, provider, provider_user_id, login, password_hash,
			display_name, role, allowed_nodes, disabled, created_at, updated_at
		) VALUES (?, 'local', ?, ?, ?, ?, ?, ?, 0, ?, ?)
	`, id, id, login, pwHash, input.DisplayName, role, allowedNodes, nowNano, nowNano)
	if err != nil {
		return AdminUser{}, fmt.Errorf("insert user: %w", err)
	}

	return s.GetAdminUser(ctx, id)
}

// UpdateAdminUser updates profile, role, node scoping, password or disabled state.
func (s *Store) UpdateAdminUser(ctx context.Context, id string, input UpdateUserInput, now time.Time) (AdminUser, error) {
	current, err := s.GetAdminUser(ctx, id)
	if err != nil {
		return AdminUser{}, err
	}

	// Protect against demoting or disabling the last active admin
	if current.Role == RoleAdmin && !current.Disabled {
		isDemoting := input.Role != nil && *input.Role != RoleAdmin
		isDisabling := input.Disabled != nil && *input.Disabled
		if isDemoting || isDisabling {
			activeAdmins, countErr := s.CountActiveAdmins(ctx)
			if countErr == nil && activeAdmins <= 1 {
				return AdminUser{}, ErrLastAdminProtection
			}
		}
	}

	displayName := current.DisplayName
	if input.DisplayName != nil {
		displayName = strings.TrimSpace(*input.DisplayName)
	}
	role := current.Role
	if input.Role != nil {
		r := strings.ToLower(strings.TrimSpace(*input.Role))
		if r == RoleAdmin || r == RoleOperator || r == RoleViewer {
			role = r
		}
	}
	allowedNodes := current.AllowedNodes
	if input.AllowedNodes != nil {
		allowedNodes = strings.TrimSpace(*input.AllowedNodes)
	}
	disabled := current.Disabled
	if input.Disabled != nil {
		disabled = *input.Disabled
	}

	nowNano := unixNano(now)
	disabledInt := 0
	if disabled {
		disabledInt = 1
	}

	if input.NewPassword != nil && strings.TrimSpace(*input.NewPassword) != "" {
		if len(*input.NewPassword) < 6 {
			return AdminUser{}, errors.New("new password must be at least 6 characters")
		}
		pwHash, err := HashUserPassword(*input.NewPassword, s.pepper)
		if err != nil {
			return AdminUser{}, err
		}
		_, err = s.db.ExecContext(ctx, `
			UPDATE admin_users
			SET display_name = ?, role = ?, allowed_nodes = ?, disabled = ?, password_hash = ?, updated_at = ?
			WHERE id = ?
		`, displayName, role, allowedNodes, disabledInt, pwHash, nowNano, id)
		if err != nil {
			return AdminUser{}, fmt.Errorf("update user with password: %w", err)
		}
	} else {
		_, err = s.db.ExecContext(ctx, `
			UPDATE admin_users
			SET display_name = ?, role = ?, allowed_nodes = ?, disabled = ?, updated_at = ?
			WHERE id = ?
		`, displayName, role, allowedNodes, disabledInt, nowNano, id)
		if err != nil {
			return AdminUser{}, fmt.Errorf("update user: %w", err)
		}
	}

	return s.GetAdminUser(ctx, id)
}

// DeleteAdminUser deletes a team member.
func (s *Store) DeleteAdminUser(ctx context.Context, id string) error {
	current, err := s.GetAdminUser(ctx, id)
	if err != nil {
		return err
	}
	if current.Role == RoleAdmin && !current.Disabled {
		activeAdmins, err := s.CountActiveAdmins(ctx)
		if err == nil && activeAdmins <= 1 {
			return ErrLastAdminProtection
		}
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM admin_users WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrUserNotFound
	}
	return nil
}

// CountActiveAdmins counts how many non-disabled administrators exist.
func (s *Store) CountActiveAdmins(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM admin_users
		WHERE role = 'admin' AND COALESCE(disabled, 0) = 0
	`).Scan(&count)
	return count, err
}

// AuthenticateLocalUser verifies login credentials for a local database user.
func (s *Store) AuthenticateLocalUser(ctx context.Context, login, password string) (AdminUser, error) {
	var user AdminUser
	var passwordHash string
	var disabledInt int
	var createdAtNano, updatedAtNano int64

	err := s.db.QueryRowContext(ctx, `
		SELECT id, provider, provider_user_id, login, password_hash,
		       COALESCE(role, 'admin'),
		       COALESCE(display_name, ''),
		       COALESCE(allowed_nodes, '*'),
		       COALESCE(disabled, 0),
		       created_at, updated_at
		FROM admin_users
		WHERE login = ?`, login).Scan(
		&user.ID, &user.Provider, &user.ProviderUserID, &user.Login, &passwordHash,
		&user.Role, &user.DisplayName, &user.AllowedNodes,
		&disabledInt, &createdAtNano, &updatedAtNano,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return AdminUser{}, ErrInvalidCredentials
	}
	if err != nil {
		return AdminUser{}, fmt.Errorf("query local user: %w", err)
	}

	user.Disabled = disabledInt != 0
	user.CreatedAt = time.Unix(0, createdAtNano).UTC()
	user.UpdatedAt = time.Unix(0, updatedAtNano).UTC()

	if user.Disabled {
		return AdminUser{}, ErrAccountDisabled
	}

	if passwordHash == "" || !VerifyUserPassword(passwordHash, password, s.pepper) {
		return AdminUser{}, ErrInvalidCredentials
	}
	if !strings.HasPrefix(passwordHash, passwordHashPrefix+"$") {
		if upgraded, hashErr := HashUserPassword(password, s.pepper); hashErr == nil {
			// A successful login remains valid even if a best-effort migration
			// cannot be persisted; the next login can retry the upgrade.
			_, _ = s.db.ExecContext(ctx, `UPDATE admin_users SET password_hash = ?, updated_at = ? WHERE id = ?`, upgraded, unixNano(time.Now().UTC()), user.ID)
		}
	}

	return user, nil
}
