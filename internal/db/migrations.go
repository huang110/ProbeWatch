package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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
CREATE TABLE IF NOT EXISTS totp_pending_states (
    id TEXT PRIMARY KEY,
    token_digest BLOB NOT NULL UNIQUE,
    admin_user_id TEXT NOT NULL REFERENCES admin_users(id) ON DELETE CASCADE,
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
CREATE INDEX IF NOT EXISTS registration_tokens_expiry_idx ON registration_tokens(expires_at);
CREATE TABLE IF NOT EXISTS nodes (
    id TEXT PRIMARY KEY,
    uuid TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT '',
    tags TEXT NOT NULL DEFAULT '',
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
CREATE TABLE IF NOT EXISTS resource_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    payload BLOB NOT NULL,
    reported_at INTEGER NOT NULL,
    recorded_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS resource_history_node_time_idx ON resource_history(node_id, reported_at, id);
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
CREATE TABLE IF NOT EXISTS network_results_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    target_id TEXT NOT NULL REFERENCES network_targets(id) ON DELETE CASCADE,
    payload BLOB NOT NULL,
    checked_at INTEGER NOT NULL,
    recorded_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS network_results_history_key_time_idx ON network_results_history(node_id, target_id, checked_at, id);
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
CREATE TABLE IF NOT EXISTS mtr_results_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    target_id TEXT NOT NULL REFERENCES mtr_targets(id) ON DELETE CASCADE,
    payload BLOB NOT NULL,
    checked_at INTEGER NOT NULL,
    recorded_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS mtr_results_history_key_time_idx ON mtr_results_history(node_id, target_id, checked_at, id);
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
CREATE TABLE IF NOT EXISTS media_results_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    detector_id TEXT NOT NULL REFERENCES media_detectors(id) ON DELETE CASCADE,
    payload BLOB NOT NULL,
    checked_at INTEGER NOT NULL,
    recorded_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS media_results_history_key_time_idx ON media_results_history(node_id, detector_id, checked_at, id);
CREATE TABLE IF NOT EXISTS speedtest_tasks (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    server_url TEXT NOT NULL,
    download_bytes INTEGER NOT NULL DEFAULT 10485760,
    upload_bytes INTEGER NOT NULL DEFAULT 5242880,
    interval_seconds INTEGER NOT NULL DEFAULT 3600,
    node_tags TEXT NOT NULL DEFAULT '',
    node_ids TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS speedtest_results_latest (
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    task_id TEXT NOT NULL REFERENCES speedtest_tasks(id) ON DELETE CASCADE,
    payload BLOB NOT NULL,
    download_speed_mbps REAL NOT NULL DEFAULT 0,
    upload_speed_mbps REAL NOT NULL DEFAULT 0,
    latency_ms INTEGER NOT NULL DEFAULT 0,
    jitter_ms INTEGER NOT NULL DEFAULT 0,
    tested_at INTEGER NOT NULL,
    PRIMARY KEY(node_id, task_id)
);
CREATE TABLE IF NOT EXISTS speedtest_results_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    task_id TEXT NOT NULL REFERENCES speedtest_tasks(id) ON DELETE CASCADE,
    payload BLOB NOT NULL,
    download_speed_mbps REAL NOT NULL DEFAULT 0,
    upload_speed_mbps REAL NOT NULL DEFAULT 0,
    latency_ms INTEGER NOT NULL DEFAULT 0,
    jitter_ms INTEGER NOT NULL DEFAULT 0,
    tested_at INTEGER NOT NULL,
    recorded_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS speedtest_results_history_key_time_idx ON speedtest_results_history(node_id, task_id, tested_at, id);
CREATE INDEX IF NOT EXISTS speedtest_results_history_time_idx ON speedtest_results_history(tested_at DESC);
CREATE TABLE IF NOT EXISTS request_replays (
    node_id TEXT NOT NULL,
    request_id TEXT NOT NULL,
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY(node_id, request_id)
);
CREATE INDEX IF NOT EXISTS request_replays_expiry_idx ON request_replays(expires_at);
CREATE INDEX IF NOT EXISTS oauth_states_expiry_idx ON oauth_states(expires_at);
CREATE INDEX IF NOT EXISTS totp_pending_states_expiry_idx ON totp_pending_states(expires_at);
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
CREATE INDEX IF NOT EXISTS audit_events_created_idx ON audit_events(created_at);
CREATE TABLE IF NOT EXISTS alert_events (
    id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    fingerprint TEXT NOT NULL,
    category TEXT NOT NULL,
    target_id TEXT NOT NULL,
    reason TEXT NOT NULL,
    severity TEXT NOT NULL CHECK (severity IN ('info', 'warning', 'critical')),
    status TEXT NOT NULL CHECK (status IN ('open', 'acked', 'resolved')),
    occurrence_count INTEGER NOT NULL DEFAULT 1 CHECK (occurrence_count > 0),
    first_seen_at INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL,
    resolved_at INTEGER,
    UNIQUE(node_id, fingerprint)
);
CREATE INDEX IF NOT EXISTS alert_events_node_status_idx ON alert_events(node_id, status, last_seen_at);
CREATE INDEX IF NOT EXISTS alert_events_resolved_idx ON alert_events(status, resolved_at);
CREATE INDEX IF NOT EXISTS alert_events_fingerprint_idx ON alert_events(fingerprint);
CREATE TABLE IF NOT EXISTS resource_history_hourly (
    window_start INTEGER NOT NULL,
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    sample_count INTEGER NOT NULL,
    cpu_avg REAL NOT NULL,
    cpu_max REAL NOT NULL,
    mem_used_ratio_avg REAL NOT NULL,
    mem_used_ratio_max REAL NOT NULL,
    disk_used_ratio_avg REAL NOT NULL,
    disk_used_ratio_max REAL NOT NULL,
    rx_min INTEGER NOT NULL,
    rx_max INTEGER NOT NULL,
    tx_min INTEGER NOT NULL,
    tx_max INTEGER NOT NULL,
    PRIMARY KEY(window_start, node_id)
);
CREATE INDEX IF NOT EXISTS resource_history_hourly_node_idx ON resource_history_hourly(node_id, window_start);
CREATE TABLE IF NOT EXISTS resource_history_daily (
    window_start INTEGER NOT NULL,
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    sample_count INTEGER NOT NULL,
    cpu_avg REAL NOT NULL,
    cpu_max REAL NOT NULL,
    mem_used_ratio_avg REAL NOT NULL,
    mem_used_ratio_max REAL NOT NULL,
    disk_used_ratio_avg REAL NOT NULL,
    disk_used_ratio_max REAL NOT NULL,
    rx_min INTEGER NOT NULL,
    rx_max INTEGER NOT NULL,
    tx_min INTEGER NOT NULL,
    tx_max INTEGER NOT NULL,
    PRIMARY KEY(window_start, node_id)
);
CREATE INDEX IF NOT EXISTS resource_history_daily_node_idx ON resource_history_daily(node_id, window_start);
CREATE TABLE IF NOT EXISTS network_results_history_hourly (
    window_start INTEGER NOT NULL,
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    target_id TEXT NOT NULL REFERENCES network_targets(id) ON DELETE CASCADE,
    total INTEGER NOT NULL,
    success INTEGER NOT NULL,
    failure INTEGER NOT NULL,
    latency_avg REAL NOT NULL,
    latency_max INTEGER NOT NULL,
    latency_count INTEGER NOT NULL,
    PRIMARY KEY(window_start, node_id, target_id)
);
CREATE INDEX IF NOT EXISTS network_results_history_hourly_node_idx ON network_results_history_hourly(node_id, window_start);
CREATE TABLE IF NOT EXISTS network_results_history_daily (
    window_start INTEGER NOT NULL,
    node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    target_id TEXT NOT NULL REFERENCES network_targets(id) ON DELETE CASCADE,
    total INTEGER NOT NULL,
    success INTEGER NOT NULL,
    failure INTEGER NOT NULL,
    latency_avg REAL NOT NULL,
    latency_max INTEGER NOT NULL,
    latency_count INTEGER NOT NULL,
    PRIMARY KEY(window_start, node_id, target_id)
);
CREATE INDEX IF NOT EXISTS network_results_history_daily_node_idx ON network_results_history_daily(node_id, window_start);
CREATE TABLE IF NOT EXISTS notification_channels (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    config TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    events TEXT NOT NULL DEFAULT '[]',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS system_settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS node_billing_settings (
    node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    reset_day INTEGER NOT NULL DEFAULT 1,
    reset_time TEXT NOT NULL DEFAULT '00:00:00',
    traffic_quota_bytes INTEGER NOT NULL DEFAULT 0,
    accounting_method TEXT NOT NULL DEFAULT 'total',
    included_interfaces TEXT NOT NULL DEFAULT '*',
    last_reset_at INTEGER NOT NULL DEFAULT 0,
    last_reset_rx_bytes INTEGER NOT NULL DEFAULT 0,
    last_reset_tx_bytes INTEGER NOT NULL DEFAULT 0,
    bonus_quota_bytes INTEGER NOT NULL DEFAULT 0,
    merchant TEXT NOT NULL DEFAULT '',
    price REAL NOT NULL DEFAULT 0,
    currency TEXT NOT NULL DEFAULT 'CNY',
    cycle TEXT NOT NULL DEFAULT 'month',
    start_date TEXT NOT NULL DEFAULT '',
    due_date TEXT NOT NULL DEFAULT '',
    auto_renew INTEGER NOT NULL DEFAULT 1,
    updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS alert_rules (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    metric TEXT NOT NULL,
    operator TEXT NOT NULL DEFAULT '>',
    threshold REAL NOT NULL,
    duration_seconds INTEGER NOT NULL DEFAULT 0,
    severity TEXT NOT NULL DEFAULT 'warning',
    node_filter TEXT NOT NULL DEFAULT '*',
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS alert_rules_metric_idx ON alert_rules(metric, enabled);
CREATE TABLE IF NOT EXISTS webauthn_credentials (
    id TEXT PRIMARY KEY,
    admin_id TEXT NOT NULL REFERENCES admin_users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    credential_id BLOB NOT NULL UNIQUE,
    public_key BLOB NOT NULL,
    algorithm INTEGER NOT NULL DEFAULT -7,
    sign_count INTEGER NOT NULL DEFAULT 0,
    aaguid BLOB NOT NULL DEFAULT X'',
    created_at INTEGER NOT NULL,
    last_used_at INTEGER
);
CREATE INDEX IF NOT EXISTS webauthn_credentials_admin_idx ON webauthn_credentials(admin_id);
CREATE INDEX IF NOT EXISTS webauthn_credentials_cred_idx ON webauthn_credentials(credential_id);
CREATE TABLE IF NOT EXISTS webauthn_challenges (
    id TEXT PRIMARY KEY,
    challenge BLOB NOT NULL,
    challenge_type TEXT NOT NULL,
    admin_id TEXT,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS webauthn_challenges_expiry_idx ON webauthn_challenges(expires_at);
CREATE TABLE IF NOT EXISTS api_tokens (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    token_prefix TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    user_id TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'operator',
    scopes TEXT NOT NULL DEFAULT '*',
    allowed_nodes TEXT NOT NULL DEFAULT '*',
    expires_at INTEGER,
    last_used_at INTEGER,
    disabled INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS api_tokens_hash_idx ON api_tokens(token_hash);
CREATE INDEX IF NOT EXISTS api_tokens_user_idx ON api_tokens(user_id);
CREATE TABLE IF NOT EXISTS audit_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    actor_id TEXT NOT NULL,
    actor_name TEXT NOT NULL,
    actor_type TEXT NOT NULL,
    action TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    detail TEXT NOT NULL DEFAULT '',
    ip_address TEXT NOT NULL DEFAULT '',
    status_code INTEGER NOT NULL DEFAULT 200,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS audit_logs_created_idx ON audit_logs(created_at DESC);
CREATE INDEX IF NOT EXISTS audit_logs_action_idx ON audit_logs(action);
CREATE INDEX IF NOT EXISTS audit_logs_actor_idx ON audit_logs(actor_id);
CREATE TABLE IF NOT EXISTS status_page_configs (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    announcement TEXT NOT NULL DEFAULT '',
    show_uptime_days INTEGER NOT NULL DEFAULT 90,
    custom_css TEXT NOT NULL DEFAULT '',
    components TEXT NOT NULL DEFAULT '[]',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS incidents (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    status TEXT NOT NULL,
    impact TEXT NOT NULL DEFAULT 'minor',
    is_maintenance INTEGER NOT NULL DEFAULT 0,
    scheduled_start_at INTEGER,
    scheduled_end_at INTEGER,
    created_at INTEGER NOT NULL,
    resolved_at INTEGER,
    updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS incidents_created_idx ON incidents(created_at DESC);
CREATE INDEX IF NOT EXISTS incidents_status_idx ON incidents(status);
CREATE TABLE IF NOT EXISTS incident_updates (
    id TEXT PRIMARY KEY,
    incident_id TEXT NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
    status TEXT NOT NULL,
    message TEXT NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS incident_updates_incident_idx ON incident_updates(incident_id, created_at ASC);
`

func migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode = WAL; PRAGMA foreign_keys = ON; PRAGMA busy_timeout = 5000;`); err != nil {
		return fmt.Errorf("configure SQLite: %w", err)
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	if err := ensureNodesSchema(ctx, db); err != nil {
		return err
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
	if err := ensureAdminTOTP(ctx, db); err != nil {
		return err
	}
	if err := ensureMediaDetectorHost(ctx, db); err != nil {
		return err
	}
	if err := ensureDefaultAlertRules(ctx, db); err != nil {
		return err
	}
	if err := ensureUserRBAC(ctx, db); err != nil {
		return err
	}
	if err := ensureAPITokensAndAudit(ctx, db); err != nil {
		return err
	}
	if err := ensureStatusPageTables(ctx, db); err != nil {
		return err
	}
	if err := ensureSpeedtestTables(ctx, db); err != nil {
		return err
	}
	if err := ensureSyntheticTables(ctx, db); err != nil {
		return err
	}
	return nil
}

// ensureAdminTOTP idempotently adds the TOTP columns to admin_users. The
// pending-state table is covered by the CREATE TABLE IF NOT EXISTS statements
// in the schema, which run on every migration pass.
func ensureAdminTOTP(ctx context.Context, db *sql.DB) error {
	columns := make(map[string]bool)
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(admin_users)`)
	if err != nil {
		return fmt.Errorf("inspect admin_users schema: %w", err)
	}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return fmt.Errorf("scan admin_users schema: %w", err)
		}
		columns[strings.ToLower(name)] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read admin_users schema: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close admin_users schema: %w", err)
	}

	if !columns["totp_secret"] {
		if _, err := db.ExecContext(ctx, `ALTER TABLE admin_users ADD COLUMN totp_secret BLOB`); err != nil {
			return fmt.Errorf("add admin_users totp_secret column: %w", err)
		}
	}
	if !columns["totp_enabled"] {
		if _, err := db.ExecContext(ctx, `ALTER TABLE admin_users ADD COLUMN totp_enabled INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("add admin_users totp_enabled column: %w", err)
		}
	}
	return nil
}

func ensureNodesSchema(ctx context.Context, db *sql.DB) error {
	columns := make(map[string]bool)
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(nodes)`)
	if err != nil {
		return fmt.Errorf("inspect nodes schema: %w", err)
	}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return fmt.Errorf("scan nodes schema: %w", err)
		}
		columns[strings.ToLower(name)] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read nodes schema: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close nodes schema: %w", err)
	}

	if !columns["uuid"] {
		if _, err := db.ExecContext(ctx, `ALTER TABLE nodes ADD COLUMN uuid TEXT`); err != nil {
			return fmt.Errorf("add nodes uuid column: %w", err)
		}
	}
	if !columns["deleted_at"] {
		if _, err := db.ExecContext(ctx, `ALTER TABLE nodes ADD COLUMN deleted_at INTEGER`); err != nil {
			return fmt.Errorf("add nodes deleted_at column: %w", err)
		}
	}
	if !columns["tags"] {
		if _, err := db.ExecContext(ctx, `ALTER TABLE nodes ADD COLUMN tags TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add nodes tags column: %w", err)
		}
	}

	rows, err = db.QueryContext(ctx, `SELECT id FROM nodes WHERE uuid IS NULL OR uuid = '' ORDER BY id`)
	if err != nil {
		return fmt.Errorf("find nodes missing uuid: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("scan node id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read nodes missing uuid: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close nodes missing uuid: %w", err)
	}
	for _, id := range ids {
		uuid := stableNodeUUID(id)
		if _, err := db.ExecContext(ctx, `UPDATE nodes SET uuid = ? WHERE id = ? AND (uuid IS NULL OR uuid = '')`, uuid, id); err != nil {
			return fmt.Errorf("backfill node %q uuid: %w", id, err)
		}
	}
	if _, err := db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS nodes_uuid_idx ON nodes(uuid)`); err != nil {
		return fmt.Errorf("create nodes uuid index: %w", err)
	}
	return nil
}

func stableNodeUUID(id string) string {
	digest := sha256.Sum256([]byte("probewatch-node:" + id))
	digest[6] = (digest[6] & 0x0f) | 0x50
	digest[8] = (digest[8] & 0x3f) | 0x80
	hexDigest := hex.EncodeToString(digest[:])
	return hexDigest[0:8] + "-" + hexDigest[8:12] + "-" + hexDigest[12:16] + "-" + hexDigest[16:20] + "-" + hexDigest[20:32]
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

func ensureDefaultAlertRules(ctx context.Context, db *sql.DB) error {
	var count int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM alert_rules`).Scan(&count)
	if err != nil {
		return fmt.Errorf("count alert rules: %w", err)
	}
	if count > 0 {
		return nil
	}
	now := time.Now().UTC().UnixNano()
	rules := []struct {
		id, name, metric, op, severity string
		threshold                      float64
	}{
		{"rule-default-cpu", "CPU 持续过高告警", "cpu", ">", "warning", 85.0},
		{"rule-default-memory", "物理内存超限告警", "memory", ">", "warning", 90.0},
		{"rule-default-disk", "磁盘根分区耗尽告警", "disk", ">", "critical", 90.0},
		{"rule-default-traffic", "周期流量耗尽预警", "traffic_percent", ">", "warning", 85.0},
	}
	for _, r := range rules {
		_, err := db.ExecContext(ctx, `
			INSERT OR IGNORE INTO alert_rules (id, name, metric, operator, threshold, duration_seconds, severity, node_filter, enabled, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, 0, ?, '*', 1, ?, ?)
		`, r.id, r.name, r.metric, r.op, r.threshold, r.severity, now, now)
		if err != nil {
			return fmt.Errorf("seed alert rule %s: %w", r.id, err)
		}
	}
	return nil
}

// ensureUserRBAC idempotently ensures the role, display_name, allowed_nodes,
// password_hash, and disabled columns exist on admin_users.
func ensureUserRBAC(ctx context.Context, db *sql.DB) error {
	columns := make(map[string]bool)
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(admin_users)`)
	if err != nil {
		return fmt.Errorf("inspect admin_users schema: %w", err)
	}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return fmt.Errorf("scan admin_users schema: %w", err)
		}
		columns[strings.ToLower(name)] = true
	}
	_ = rows.Close()

	if !columns["role"] {
		if _, err := db.ExecContext(ctx, `ALTER TABLE admin_users ADD COLUMN role TEXT NOT NULL DEFAULT 'admin'`); err != nil {
			return fmt.Errorf("add admin_users role: %w", err)
		}
	}
	if !columns["password_hash"] {
		if _, err := db.ExecContext(ctx, `ALTER TABLE admin_users ADD COLUMN password_hash TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add admin_users password_hash: %w", err)
		}
	}
	if !columns["display_name"] {
		if _, err := db.ExecContext(ctx, `ALTER TABLE admin_users ADD COLUMN display_name TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add admin_users display_name: %w", err)
		}
	}
	if !columns["allowed_nodes"] {
		if _, err := db.ExecContext(ctx, `ALTER TABLE admin_users ADD COLUMN allowed_nodes TEXT NOT NULL DEFAULT '*'`); err != nil {
			return fmt.Errorf("add admin_users allowed_nodes: %w", err)
		}
	}
	if !columns["disabled"] {
		if _, err := db.ExecContext(ctx, `ALTER TABLE admin_users ADD COLUMN disabled INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("add admin_users disabled: %w", err)
		}
	}
	return nil
}

// ensureAPITokensAndAudit idempotently creates the api_tokens and audit_logs tables and indexes.
func ensureAPITokensAndAudit(ctx context.Context, db *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS api_tokens (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			token_prefix TEXT NOT NULL,
			token_hash TEXT NOT NULL UNIQUE,
			user_id TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'operator',
			scopes TEXT NOT NULL DEFAULT '*',
			allowed_nodes TEXT NOT NULL DEFAULT '*',
			expires_at INTEGER,
			last_used_at INTEGER,
			disabled INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS api_tokens_hash_idx ON api_tokens(token_hash);`,
		`CREATE INDEX IF NOT EXISTS api_tokens_user_idx ON api_tokens(user_id);`,
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			actor_id TEXT NOT NULL,
			actor_name TEXT NOT NULL,
			actor_type TEXT NOT NULL,
			action TEXT NOT NULL,
			resource_type TEXT NOT NULL,
			resource_id TEXT NOT NULL,
			detail TEXT NOT NULL DEFAULT '',
			ip_address TEXT NOT NULL DEFAULT '',
			status_code INTEGER NOT NULL DEFAULT 200,
			created_at INTEGER NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS audit_logs_created_idx ON audit_logs(created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS audit_logs_action_idx ON audit_logs(action);`,
		`CREATE INDEX IF NOT EXISTS audit_logs_actor_idx ON audit_logs(actor_id);`,
	}

	for _, q := range queries {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("ensure api_tokens and audit_logs: %w", err)
		}
	}
	return nil
}

// ensureStatusPageTables idempotently creates the status_page_configs, incidents, and incident_updates tables.
func ensureStatusPageTables(ctx context.Context, db *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS status_page_configs (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			announcement TEXT NOT NULL DEFAULT '',
			show_uptime_days INTEGER NOT NULL DEFAULT 90,
			custom_css TEXT NOT NULL DEFAULT '',
			components TEXT NOT NULL DEFAULT '[]',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS incidents (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			status TEXT NOT NULL,
			impact TEXT NOT NULL DEFAULT 'minor',
			is_maintenance INTEGER NOT NULL DEFAULT 0,
			scheduled_start_at INTEGER,
			scheduled_end_at INTEGER,
			created_at INTEGER NOT NULL,
			resolved_at INTEGER,
			updated_at INTEGER NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS incidents_created_idx ON incidents(created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS incidents_status_idx ON incidents(status);`,
		`CREATE TABLE IF NOT EXISTS incident_updates (
			id TEXT PRIMARY KEY,
			incident_id TEXT NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
			status TEXT NOT NULL,
			message TEXT NOT NULL,
			created_at INTEGER NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS incident_updates_incident_idx ON incident_updates(incident_id, created_at ASC);`,
	}

	for _, q := range queries {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("ensure status page tables: %w", err)
		}
	}
	return nil
}

// ensureSpeedtestTables idempotently creates speedtest_tasks, speedtest_results_latest, and speedtest_results_history tables.
func ensureSpeedtestTables(ctx context.Context, db *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS speedtest_tasks (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			server_url TEXT NOT NULL,
			download_bytes INTEGER NOT NULL DEFAULT 10485760,
			upload_bytes INTEGER NOT NULL DEFAULT 5242880,
			interval_seconds INTEGER NOT NULL DEFAULT 3600,
			node_tags TEXT NOT NULL DEFAULT '',
			node_ids TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS speedtest_results_latest (
			node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
			task_id TEXT NOT NULL REFERENCES speedtest_tasks(id) ON DELETE CASCADE,
			payload BLOB NOT NULL,
			download_speed_mbps REAL NOT NULL DEFAULT 0,
			upload_speed_mbps REAL NOT NULL DEFAULT 0,
			latency_ms INTEGER NOT NULL DEFAULT 0,
			jitter_ms INTEGER NOT NULL DEFAULT 0,
			tested_at INTEGER NOT NULL,
			PRIMARY KEY(node_id, task_id)
		);`,
		`CREATE TABLE IF NOT EXISTS speedtest_results_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
			task_id TEXT NOT NULL REFERENCES speedtest_tasks(id) ON DELETE CASCADE,
			payload BLOB NOT NULL,
			download_speed_mbps REAL NOT NULL DEFAULT 0,
			upload_speed_mbps REAL NOT NULL DEFAULT 0,
			latency_ms INTEGER NOT NULL DEFAULT 0,
			jitter_ms INTEGER NOT NULL DEFAULT 0,
			tested_at INTEGER NOT NULL,
			recorded_at INTEGER NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS speedtest_results_history_key_time_idx ON speedtest_results_history(node_id, task_id, tested_at, id);`,
		`CREATE INDEX IF NOT EXISTS speedtest_results_history_time_idx ON speedtest_results_history(tested_at DESC);`,
	}

	for _, q := range queries {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("ensure speedtest tables: %w", err)
		}
	}
	return nil
}

func ensureSyntheticTables(ctx context.Context, db *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS synthetic_targets (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			protocol TEXT NOT NULL,
			target_url TEXT NOT NULL,
			method TEXT NOT NULL DEFAULT 'GET',
			headers TEXT NOT NULL DEFAULT '{}',
			body_payload TEXT NOT NULL DEFAULT '',
			assertions TEXT NOT NULL DEFAULT '[]',
			grpc_service TEXT NOT NULL DEFAULT '',
			timeout_ms INTEGER NOT NULL DEFAULT 5000,
			interval_seconds INTEGER NOT NULL DEFAULT 60,
			node_tags TEXT NOT NULL DEFAULT '',
			node_ids TEXT NOT NULL DEFAULT '',
			consensus_nodes INTEGER NOT NULL DEFAULT 1,
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS synthetic_results_latest (
			node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
			target_id TEXT NOT NULL REFERENCES synthetic_targets(id) ON DELETE CASCADE,
			payload BLOB NOT NULL,
			protocol TEXT NOT NULL,
			status_code INTEGER NOT NULL DEFAULT 0,
			passed INTEGER NOT NULL DEFAULT 1,
			failed_assertion TEXT NOT NULL DEFAULT '',
			dns_ms INTEGER NOT NULL DEFAULT 0,
			connect_ms INTEGER NOT NULL DEFAULT 0,
			tls_ms INTEGER NOT NULL DEFAULT 0,
			ttfb_ms INTEGER NOT NULL DEFAULT 0,
			total_ms INTEGER NOT NULL DEFAULT 0,
			checked_at INTEGER NOT NULL,
			PRIMARY KEY(node_id, target_id)
		);`,
		`CREATE TABLE IF NOT EXISTS synthetic_results_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
			target_id TEXT NOT NULL REFERENCES synthetic_targets(id) ON DELETE CASCADE,
			payload BLOB NOT NULL,
			protocol TEXT NOT NULL,
			status_code INTEGER NOT NULL DEFAULT 0,
			passed INTEGER NOT NULL DEFAULT 1,
			failed_assertion TEXT NOT NULL DEFAULT '',
			dns_ms INTEGER NOT NULL DEFAULT 0,
			connect_ms INTEGER NOT NULL DEFAULT 0,
			tls_ms INTEGER NOT NULL DEFAULT 0,
			ttfb_ms INTEGER NOT NULL DEFAULT 0,
			total_ms INTEGER NOT NULL DEFAULT 0,
			checked_at INTEGER NOT NULL,
			recorded_at INTEGER NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS synthetic_results_history_key_time_idx ON synthetic_results_history(node_id, target_id, checked_at, id);`,
		`CREATE INDEX IF NOT EXISTS synthetic_results_history_time_idx ON synthetic_results_history(checked_at DESC);`,
	}

	for _, q := range queries {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("ensure synthetic tables: %w", err)
		}
	}
	return nil
}


