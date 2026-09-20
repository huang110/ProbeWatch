package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const schema = `
CREATE TABLE IF NOT EXISTS admin_users (
    id TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    provider_user_id TEXT NOT NULL,
    login TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(provider, provider_user_id)
);
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    admin_user_id TEXT NOT NULL REFERENCES admin_users(id) ON DELETE CASCADE,
    session_digest BLOB NOT NULL UNIQUE,
    csrf_digest BLOB NOT NULL DEFAULT X'',
    policy_digest BLOB NOT NULL DEFAULT X'',
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS oauth_states (
    id TEXT PRIMARY KEY,
    state_digest BLOB NOT NULL UNIQUE,
    expires_at INTEGER NOT NULL,
    consumed_at INTEGER
);
CREATE TABLE IF NOT EXISTS registration_tokens (
    id TEXT PRIMARY KEY,
    token_digest BLOB NOT NULL UNIQUE,
    expires_at INTEGER NOT NULL,
    consumed_at INTEGER,
    created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS nodes (
    id TEXT PRIMARY KEY,
    uuid TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    deleted_at INTEGER,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
	CREATE TABLE IF NOT EXISTS node_tokens (
    id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    token_digest BLOB NOT NULL UNIQUE,
		token_prefix TEXT NOT NULL,
		revoked_at INTEGER,
		expires_at INTEGER NOT NULL,
		created_at INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS node_tokens_active_idx ON node_tokens(token_digest, revoked_at);
CREATE TABLE IF NOT EXISTS resource_latest (
    node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    payload BLOB NOT NULL,
    reported_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS network_targets (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    kind TEXT NOT NULL,
    host TEXT NOT NULL,
    payload BLOB NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS network_results_latest (
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    target_id TEXT NOT NULL REFERENCES network_targets(id) ON DELETE CASCADE,
    payload BLOB NOT NULL,
    checked_at INTEGER NOT NULL,
    PRIMARY KEY(node_id, target_id)
);
CREATE TABLE IF NOT EXISTS mtr_targets (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    host TEXT NOT NULL,
    payload BLOB NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS mtr_results_latest (
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    target_id TEXT NOT NULL REFERENCES mtr_targets(id) ON DELETE CASCADE,
    payload BLOB NOT NULL,
    checked_at INTEGER NOT NULL,
    PRIMARY KEY(node_id, target_id)
);
CREATE TABLE IF NOT EXISTS media_detectors (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    host TEXT NOT NULL,
    payload BLOB NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS media_results_latest (
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    detector_id TEXT NOT NULL REFERENCES media_detectors(id) ON DELETE CASCADE,
    payload BLOB NOT NULL,
    checked_at INTEGER NOT NULL,
    PRIMARY KEY(node_id, detector_id)
);
CREATE TABLE IF NOT EXISTS request_replays (
    node_id TEXT NOT NULL,
    request_id TEXT NOT NULL,
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY(node_id, request_id)
);
CREATE INDEX IF NOT EXISTS request_replays_expiry_idx ON request_replays(expires_at);
CREATE INDEX IF NOT EXISTS oauth_states_expiry_idx ON oauth_states(expires_at);
CREATE INDEX IF NOT EXISTS sessions_expiry_idx ON sessions(expires_at);
CREATE TABLE IF NOT EXISTS audit_events (
    id TEXT PRIMARY KEY,
    action TEXT NOT NULL,
    node_id TEXT,
    actor_id TEXT,
    metadata BLOB,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS audit_events_node_idx ON audit_events(node_id, created_at);
`

func migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode = WAL; PRAGMA foreign_keys = ON; PRAGMA busy_timeout = 5000;`); err != nil {
		return fmt.Errorf("configure SQLite: %w", err)
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	if err := ensureNodeTokenPrefix(ctx, db); err != nil {
		return err
	}
	if err := ensureNodeTokenExpiry(ctx, db); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS node_tokens_expiry_idx ON node_tokens(expires_at)`); err != nil {
		return fmt.Errorf("create node token expiry index: %w", err)
	}
	if err := ensureSessionCSRF(ctx, db); err != nil {
		return err
	}
	if err := ensureSessionPolicy(ctx, db); err != nil {
		return err
	}
	if err := ensureTargetEnabled(ctx, db); err != nil {
		return err
	}
	if err := ensureMediaDetectorHost(ctx, db); err != nil {
		return err
	}
	return nil
}

func ensureTargetEnabled(ctx context.Context, db *sql.DB) error {
	for _, table := range []string{"network_targets", "mtr_targets", "media_detectors"} {
		hasEnabled := false
		rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
		if err != nil {
			return fmt.Errorf("inspect %s enabled schema: %w", table, err)
		}
		for rows.Next() {
			var cid, notNull, primaryKey int
			var name, columnType string
			var defaultValue sql.NullString
			if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
				rows.Close()
				return fmt.Errorf("scan %s enabled schema: %w", table, err)
			}
			if strings.EqualFold(name, "enabled") {
				hasEnabled = true
				break
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("read %s enabled schema: %w", table, err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close %s enabled schema: %w", table, err)
		}
		if !hasEnabled {
			if _, err := db.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1`); err != nil {
				return fmt.Errorf("add %s enabled column: %w", table, err)
			}
		}
	}
	return nil
}

func ensureMediaDetectorHost(ctx context.Context, db *sql.DB) error {
	hasHost := false
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(media_detectors)`)
	if err != nil {
		return fmt.Errorf("inspect media detector host schema: %w", err)
	}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return fmt.Errorf("scan media detector host schema: %w", err)
		}
		if strings.EqualFold(name, "host") {
			hasHost = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read media detector host schema: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close media detector host schema: %w", err)
	}
	if !hasHost {
		if _, err := db.ExecContext(ctx, `ALTER TABLE media_detectors ADD COLUMN host TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add media detector host: %w", err)
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE media_detectors SET host = json_extract(payload, '$.host') WHERE host = '' AND json_valid(payload) AND json_extract(payload, '$.host') IS NOT NULL`); err != nil {
		return fmt.Errorf("backfill media detector hosts: %w", err)
	}
	return nil
}

func ensureNodeTokenExpiry(ctx context.Context, db *sql.DB) error {
	hasExpiry := false
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(node_tokens)`)
	if err != nil {
		return fmt.Errorf("inspect node_tokens expiry schema: %w", err)
	}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return fmt.Errorf("scan node_tokens expiry schema: %w", err)
		}
		if strings.EqualFold(name, "expires_at") {
			hasExpiry = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read node_tokens expiry schema: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close node_tokens expiry schema: %w", err)
	}
	if !hasExpiry {
		if _, err := db.ExecContext(ctx, `ALTER TABLE node_tokens ADD COLUMN expires_at INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("add node token expiry: %w", err)
		}
	}
	backfill := time.Now().UTC().Add(DefaultNodeTokenLifetime)
	if _, err := db.ExecContext(ctx, `UPDATE node_tokens SET expires_at = ? WHERE expires_at IS NULL OR expires_at = 0`, unixNano(backfill)); err != nil {
		return fmt.Errorf("backfill node token expiries: %w", err)
	}
	return nil
}

func ensureNodeTokenPrefix(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(node_tokens)`)
	if err != nil {
		return fmt.Errorf("inspect node_tokens schema: %w", err)
	}
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return fmt.Errorf("scan node_tokens schema: %w", err)
		}
		if strings.EqualFold(name, "token_prefix") {
			if err := rows.Close(); err != nil {
				return fmt.Errorf("close node_tokens schema: %w", err)
			}
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read node_tokens schema: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close node_tokens schema: %w", err)
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE node_tokens ADD COLUMN token_prefix TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("add node token prefix: %w", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE node_tokens SET token_prefix = lower(substr(hex(token_digest), 1, 8)) WHERE token_prefix = ''`); err != nil {
		return fmt.Errorf("backfill node token prefixes: %w", err)
	}
	return nil
}

func ensureSessionCSRF(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(sessions)`)
	if err != nil {
		return fmt.Errorf("inspect sessions schema: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return fmt.Errorf("scan sessions schema: %w", err)
		}
		if strings.EqualFold(name, "csrf_digest") {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read sessions schema: %w", err)
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE sessions ADD COLUMN csrf_digest BLOB NOT NULL DEFAULT X''`); err != nil {
		return fmt.Errorf("add session csrf digest: %w", err)
	}
	return nil
}

func ensureSessionPolicy(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(sessions)`)
	if err != nil {
		return fmt.Errorf("inspect sessions policy schema: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return fmt.Errorf("scan sessions policy schema: %w", err)
		}
		if strings.EqualFold(name, "policy_digest") {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read sessions policy schema: %w", err)
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE sessions ADD COLUMN policy_digest BLOB NOT NULL DEFAULT X''`); err != nil {
		return fmt.Errorf("add session policy digest: %w", err)
	}
	return nil
}
