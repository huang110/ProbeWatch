package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/security"
)

const testPepper = "test-only-server-pepper"

func TestOpenStoreCreatesSecureDatabaseAndMigratesAllTables(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "private", "data")
	databasePath := filepath.Join(dataDir, "probewatch.db")

	store, err := OpenStore(databasePath, []byte(testPepper))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	for _, name := range []string{
		"admin_users", "sessions", "oauth_states", "registration_tokens", "nodes", "node_tokens",
		"resource_latest", "network_targets", "network_results_latest", "mtr_targets", "mtr_results_latest",
		"media_detectors", "media_results_latest", "request_replays", "audit_events", "alert_events",
	} {
		var count int
		if err := store.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("table %q was not migrated", name)
		}
	}

	if _, err := os.Stat(dataDir); err != nil {
		t.Fatalf("database directory was not created: %v", err)
	}
	if _, err := os.Stat(databasePath); err != nil {
		t.Fatalf("database file was not created: %v", err)
	}
	if runtime.GOOS != "windows" {
		assertFileMode(t, dataDir, 0700)
		assertFileMode(t, databasePath, 0600)
	} else {
		t.Log("Windows does not expose POSIX permission bits through os.FileMode; 0700/0600 cannot be asserted on this platform")
	}
}

func TestOpenStoreMigratesLegacyNodesSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-nodes.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(`
		CREATE TABLE nodes (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			status TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		);
		INSERT INTO nodes (id, name, status, created_at, updated_at)
		VALUES ('legacy-node-1', 'legacy', 'online', 1700000000000000000, 1700000000000000000);
		INSERT INTO nodes (id, name, status, created_at, updated_at)
		VALUES ('legacy-node-2', 'legacy-2', 'offline', 1700000000000000001, 1700000000000000001);
	`)
	if err != nil {
		legacy.Close()
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := OpenStore(path, []byte(testPepper))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	nodes, err := store.ListNodes(context.Background())
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("ListNodes returned %d nodes, want 2", len(nodes))
	}
	if nodes[0].UUID == "" || nodes[1].UUID == "" || nodes[0].UUID == nodes[1].UUID {
		t.Fatalf("migrated UUIDs = %#v, want distinct non-empty UUIDs", nodes)
	}
	for _, node := range nodes {
		if !security.IsRFC4122UUID(node.UUID) {
			t.Fatalf("migrated UUID %q is not RFC4122", node.UUID)
		}
	}
	var status string
	if err := store.db.QueryRow(`SELECT status FROM nodes WHERE id = 'legacy-node-1'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "online" {
		t.Fatalf("legacy status = %q, want online", status)
	}
	if _, err := store.db.Exec(`INSERT INTO nodes (id, uuid, name, status, created_at, updated_at) VALUES ('new-node', '550e8400-e29b-41d4-a716-446655440099', 'new', 'online', 1, 1)`); err != nil {
		t.Fatalf("insert migrated node: %v", err)
	}
	if _, err := store.db.Exec(`INSERT INTO nodes (id, uuid, name, status, created_at, updated_at) VALUES ('duplicate-node', ?, 'duplicate', 'online', 1, 1)`, nodes[0].UUID); err == nil {
		t.Fatal("duplicate UUID insert unexpectedly succeeded")
	}
}

func TestOpenStoreCreatesRequiredExplicitIndexes(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	for _, name := range []string{
		"node_tokens_active_idx",
		"node_tokens_expiry_idx",
		"request_replays_expiry_idx",
		"oauth_states_expiry_idx",
		"sessions_expiry_idx",
		"audit_events_node_idx",
	} {
		t.Run(name, func(t *testing.T) {
			var count int
			if err := store.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, name).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatalf("index %q was not migrated", name)
			}
		})
	}
}

func TestNodeTokenMigrationBackfillsExistingRowsWithDefaultExpiry(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "legacy.db")
	legacy, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(`
		PRAGMA foreign_keys = ON;
		CREATE TABLE nodes (id TEXT PRIMARY KEY, uuid TEXT NOT NULL UNIQUE, name TEXT NOT NULL, deleted_at INTEGER, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL);
		CREATE TABLE node_tokens (id TEXT PRIMARY KEY, node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE, token_digest BLOB NOT NULL UNIQUE, token_prefix TEXT NOT NULL, revoked_at INTEGER, created_at INTEGER NOT NULL);
		INSERT INTO nodes (id, uuid, name, created_at, updated_at) VALUES ('legacy-node', '550e8400-e29b-41d4-a716-446655440020', 'legacy', 1700000000000000000, 1700000000000000000);
		INSERT INTO node_tokens (id, node_id, token_digest, token_prefix, created_at) VALUES ('legacy-token', 'legacy-node', X'00', 'legacy', 1700000000000000000);
	`)
	if err != nil {
		legacy.Close()
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	before := time.Now().UTC()
	store, err := OpenStore(databasePath, []byte(testPepper))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var expiresAt int64
	if err := store.db.QueryRow(`SELECT expires_at FROM node_tokens WHERE id = 'legacy-token'`).Scan(&expiresAt); err != nil {
		t.Fatal(err)
	}
	expires := time.Unix(0, expiresAt).UTC()
	after := time.Now().UTC()
	if expires.Before(before.Add(DefaultNodeTokenLifetime)) || expires.After(after.Add(DefaultNodeTokenLifetime)) {
		t.Fatalf("backfilled expiry = %s, want approximately now + %s", expires, DefaultNodeTokenLifetime)
	}
}

func TestOAuthStateCanBeConsumedOnlyOnceAndExpiredStatesAreCleaned(t *testing.T) {
	store := openTestStore(t)
	now := time.Unix(1_700_000_000, 0).UTC()

	state, err := store.CreateOAuthState(context.Background(), []byte("state-value"), now.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if state == "" {
		t.Fatal("CreateOAuthState returned an empty ID")
	}
	if err := store.ConsumeOAuthState(context.Background(), []byte("state-value"), now); err != nil {
		t.Fatal(err)
	}
	if err := store.ConsumeOAuthState(context.Background(), []byte("state-value"), now); !errors.Is(err, ErrOAuthStateConsumed) {
		t.Fatalf("second OAuth state consume error = %v, want ErrOAuthStateConsumed", err)
	}

	if _, err := store.CreateOAuthState(context.Background(), []byte("expired"), now.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	deleted, err := store.CleanupExpiredOAuthStates(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("expired OAuth states deleted = %d, want 1", deleted)
	}
}

func TestAdminUserUpsertAndSessionLifecycleStoreOnlyDigests(t *testing.T) {
	store := openTestStore(t)
	now := time.Unix(1_700_000_000, 0).UTC()

	admin, err := store.UpsertAdminUser(context.Background(), "github", "42", "alice", now)
	if err != nil {
		t.Fatal(err)
	}
	if admin.ID == "" || admin.Login != "alice" {
		t.Fatalf("admin user = %#v", admin)
	}
	allowed, err := store.FindAllowedAdminUser(context.Background(), "github", "42", []string{"alice"}, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if !allowed {
		t.Fatal("allowlisted admin user was rejected")
	}

	plaintext := "session-plaintext"
	if err := store.CreateSession(context.Background(), "session-id", []byte(plaintext), admin.ID, now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	session, err := store.GetSession(context.Background(), []byte(plaintext), now)
	if err != nil {
		t.Fatal(err)
	}
	if session.AdminUserID != admin.ID {
		t.Fatalf("session admin ID = %q, want %q", session.AdminUserID, admin.ID)
	}
	if err := store.DeleteSession(context.Background(), []byte(plaintext)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetSession(context.Background(), []byte(plaintext), now); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("deleted session error = %v, want ErrSessionNotFound", err)
	}
}

func TestResourceLatestUpsertAndFetch(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	node := registerTestNode(t, store, "resource-latest-node")
	reportedAt := time.Unix(1_700_000_000, 123).UTC()
	payload := []byte(`{"hostname":"probe-1","memory_bytes":1024}`)

	if err := store.UpsertResourceLatest(context.Background(), node.Node.ID, reportedAt, payload); err != nil {
		t.Fatal(err)
	}
	reportedAt = reportedAt.Add(time.Second)
	payload = []byte(`{"hostname":"probe-1","memory_bytes":2048}`)
	if err := store.UpsertResourceLatest(context.Background(), node.Node.ID, reportedAt, payload); err != nil {
		t.Fatal(err)
	}

	gotReportedAt, gotPayload, err := store.GetResourceLatest(context.Background(), node.Node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !gotReportedAt.Equal(reportedAt) {
		t.Fatalf("reported at = %s, want %s", gotReportedAt, reportedAt)
	}
	if !bytes.Equal(gotPayload, payload) {
		t.Fatalf("payload = %s, want %s", gotPayload, payload)
	}
}

func TestRegisterNodeRejectsNonUUIDNodeBinding(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	registration, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RegisterNode(context.Background(), registration.Token, NodeInput{UUID: "node-1", Name: "invalid"}, time.Now().UTC()); err == nil {
		t.Fatal("RegisterNode accepted a non-RFC4122 UUID")
	}
	assertRowCount(t, store, `SELECT count(*) FROM nodes`, 0)
}

func TestResourceLatestDoesNotReplaceNewerTimestamp(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	node := registerTestNode(t, store, "550e8400-e29b-41d4-a716-446655440010")
	newer := time.Unix(1_700_000_100, 0).UTC()
	older := newer.Add(-time.Minute)
	if err := store.UpsertResourceLatest(context.Background(), node.Node.ID, newer, []byte(`{"status":"newer"}`)); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertResourceLatest(context.Background(), node.Node.ID, older, []byte(`{"status":"older"}`)); err != nil {
		t.Fatal(err)
	}
	gotAt, gotPayload, err := store.GetResourceLatest(context.Background(), node.Node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !gotAt.Equal(newer) || string(gotPayload) != `{"status":"newer"}` {
		t.Fatalf("stale resource replaced latest: (%s, %s)", gotAt, gotPayload)
	}
}

func TestResourceLatestRejectsInvalidJSON(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	node := registerTestNode(t, store, "invalid-resource-latest-node")
	if err := store.UpsertResourceLatest(context.Background(), node.Node.ID, time.Now(), []byte(`{"invalid"}`)); err == nil {
		t.Fatal("invalid JSON payload was accepted")
	}
	assertRowCount(t, store, `SELECT count(*) FROM resource_latest WHERE node_id = ?`, 0, node.Node.ID)
}

func TestResourceLatestRejectsOversizedPayload(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	node := registerTestNode(t, store, "oversized-resource-latest-node")
	payload := append([]byte(`{"payload":"`), bytes.Repeat([]byte{'a'}, 64*1024)...)
	payload = append(payload, []byte(`"}`)...)
	if err := store.UpsertResourceLatest(context.Background(), node.Node.ID, time.Now(), payload); err == nil {
		t.Fatal("oversized payload was accepted")
	}
	assertRowCount(t, store, `SELECT count(*) FROM resource_latest WHERE node_id = ?`, 0, node.Node.ID)
}

func TestResourceLatestRequiresExistingNode(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	if err := store.UpsertResourceLatest(context.Background(), "missing-node", time.Now(), []byte(`{"ok":true}`)); err == nil {
		t.Fatal("resource was accepted for a missing node")
	}
}

func TestLatestResultWrappers(t *testing.T) {
	tests := []struct {
		name   string
		seed   func(*testing.T, *Store, string, time.Time)
		upsert func(context.Context, string, string, time.Time, []byte) error
		get    func(context.Context, string, string) (time.Time, []byte, error)
	}{
		{name: "network", seed: seedNetworkTarget},
		{name: "mtr", seed: seedMTRTarget},
		{name: "media", seed: seedMediaDetector},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := openTestStore(t)
			node := registerTestNode(t, store, tt.name+"-latest-node")
			targetID := tt.name + "-target"
			now := time.Unix(1_700_000_000, 123).UTC()
			tt.seed(t, store, targetID, now)
			switch tt.name {
			case "network":
				tt.upsert = store.UpsertNetworkLatest
				tt.get = store.GetNetworkLatest
			case "mtr":
				tt.upsert = store.UpsertMTRLatest
				tt.get = store.GetMTRLatest
			case "media":
				tt.upsert = store.UpsertMediaLatest
				tt.get = store.GetMediaLatest
			}
			payload := []byte(`{"status":"ok"}`)
			if err := tt.upsert(context.Background(), node.Node.ID, targetID, now, payload); err != nil {
				t.Fatal(err)
			}
			gotAt, gotPayload, err := tt.get(context.Background(), node.Node.ID, targetID)
			if err != nil {
				t.Fatal(err)
			}
			if !gotAt.Equal(now) || !bytes.Equal(gotPayload, payload) {
				t.Fatalf("latest result = (%s, %s), want (%s, %s)", gotAt, gotPayload, now, payload)
			}
		})
	}
}

func TestLatestResultWrappersDoNotReplaceNewerTimestamp(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	node := registerTestNode(t, store, "550e8400-e29b-41d4-a716-446655440011")
	now := time.Unix(1_700_000_100, 0).UTC()
	seedNetworkTarget(t, store, "stale-target", now)
	if err := store.UpsertNetworkLatest(context.Background(), node.Node.ID, "stale-target", now, []byte(`{"status":"newer"}`)); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertNetworkLatest(context.Background(), node.Node.ID, "stale-target", now.Add(-time.Minute), []byte(`{"status":"older"}`)); err != nil {
		t.Fatal(err)
	}
	gotAt, gotPayload, err := store.GetNetworkLatest(context.Background(), node.Node.ID, "stale-target")
	if err != nil {
		t.Fatal(err)
	}
	if !gotAt.Equal(now) || string(gotPayload) != `{"status":"newer"}` {
		t.Fatalf("stale network result replaced latest: (%s, %s)", gotAt, gotPayload)
	}
}

func seedNetworkTarget(t *testing.T, store *Store, targetID string, now time.Time) {
	t.Helper()
	if _, err := store.db.Exec(`INSERT INTO network_targets (id, name, kind, host, payload, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, targetID, targetID, "tcp", "example.com", []byte(`{}`), unixNano(now), unixNano(now)); err != nil {
		t.Fatal(err)
	}
}

func seedMTRTarget(t *testing.T, store *Store, targetID string, now time.Time) {
	t.Helper()
	if _, err := store.db.Exec(`INSERT INTO mtr_targets (id, name, host, payload, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`, targetID, targetID, "example.com", []byte(`{}`), unixNano(now), unixNano(now)); err != nil {
		t.Fatal(err)
	}
}

func seedMediaDetector(t *testing.T, store *Store, detectorID string, now time.Time) {
	t.Helper()
	if _, err := store.db.Exec(`INSERT INTO media_detectors (id, name, host, payload, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`, detectorID, detectorID, "media.example.com", []byte(`{}`), unixNano(now), unixNano(now)); err != nil {
		t.Fatal(err)
	}
}

func TestLatestResultWrappersRejectInvalidJSON(t *testing.T) {
	store := openTestStore(t)
	node := registerTestNode(t, store, "invalid-latest-node")
	now := time.Now().UTC()
	if _, err := store.db.Exec(`INSERT INTO network_targets (id, name, kind, host, payload, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, "invalid-target", "invalid-target", "tcp", "example.com", []byte(`{}`), unixNano(now), unixNano(now)); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertNetworkLatest(context.Background(), node.Node.ID, "invalid-target", now, []byte(`{"invalid"}`)); err == nil {
		t.Fatal("invalid JSON payload was accepted")
	}
}

func TestBackupCreatesConsistentSecureDatabaseCopy(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source", "probe.db")
	destinationPath := filepath.Join(root, "backups", "nested", "probe-backup.db")
	store, err := OpenStore(sourcePath, []byte(testPepper))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.CreateRegistrationToken(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}

	if err := store.Backup(context.Background(), destinationPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(destinationPath); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		assertFileMode(t, filepath.Dir(destinationPath), 0700)
		assertFileMode(t, destinationPath, 0600)
	} else {
		t.Log("Windows does not expose POSIX permission bits through os.FileMode; backup mode cannot be asserted on this platform")
	}

	backup, err := OpenStore(destinationPath, []byte(testPepper))
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	assertRowCount(t, backup, `SELECT count(*) FROM registration_tokens`, 1)
}

func TestBackupDoesNotRequireWALSidecarAtDestination(t *testing.T) {
	root := t.TempDir()
	destinationPath := filepath.Join(root, "backup", "probe.db")
	store := openTestStore(t)
	if err := store.Backup(context.Background(), destinationPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(destinationPath + "-wal"); err == nil {
		t.Fatal("backup unexpectedly left a WAL sidecar at the destination")
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}

func TestOpenStoreConfiguresSQLitePragmas(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	var journalMode string
	if err := store.db.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}

	var foreignKeys int
	if err := store.db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}

	var busyTimeout int
	if err := store.db.QueryRow(`PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
		t.Fatal(err)
	}
	if busyTimeout != 5000 {
		t.Fatalf("busy_timeout = %d, want 5000", busyTimeout)
	}
}

func TestOpenStoreConfiguresSQLitePragmasOnNewConnection(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	store.db.SetMaxOpenConns(2)
	store.db.SetMaxIdleConns(0)
	conn, err := store.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	var foreignKeys, busyTimeout int
	if err := conn.QueryRowContext(context.Background(), `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(context.Background(), `PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 || busyTimeout != 5000 {
		t.Fatalf("new connection pragmas = foreign_keys:%d busy_timeout:%d, want 1 and 5000", foreignKeys, busyTimeout)
	}
}

func TestRegistrationTokenCanBeConsumedOnlyOnce(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	created, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	consumed, err := store.ConsumeRegistrationToken(context.Background(), created.Token, time.Now())
	if err != nil || !consumed {
		t.Fatalf("first consume = (%t, %v), want (true, nil)", consumed, err)
	}
	consumed, err = store.ConsumeRegistrationToken(context.Background(), created.Token, time.Now())
	if consumed || !errors.Is(err, ErrTokenAlreadyConsumed) {
		t.Fatalf("second consume = (%t, %v), want (false, ErrTokenAlreadyConsumed)", consumed, err)
	}
}

func TestExpiredRegistrationTokenCannotBeConsumed(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	created, err := store.CreateRegistrationToken(context.Background(), time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	consumed, err := store.ConsumeRegistrationToken(context.Background(), created.Token, created.ExpiresAt.Add(time.Second))
	if consumed || !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("consume = (%t, %v), want (false, ErrTokenExpired)", consumed, err)
	}
}

func TestConsumeRegistrationTokenReturnsFalseWhenConditionalUpdateAffectsZeroRows(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	created, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.db.Exec(`CREATE TRIGGER ignore_registration_token_consume BEFORE UPDATE OF consumed_at ON registration_tokens BEGIN SELECT RAISE(IGNORE); END`)
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Exec(`DROP TRIGGER ignore_registration_token_consume`)

	consumed, err := store.ConsumeRegistrationToken(context.Background(), created.Token, time.Now())
	if err != nil {
		t.Fatalf("consume error = %v, want nil", err)
	}
	if consumed {
		t.Fatal("consume returned true when conditional update affected zero rows")
	}
	var consumedAt any
	if err := store.db.QueryRow(`SELECT consumed_at FROM registration_tokens WHERE id = ?`, created.ID).Scan(&consumedAt); err != nil {
		t.Fatal(err)
	}
	if consumedAt != nil {
		t.Fatalf("registration token was marked consumed after zero-row update: %v", consumedAt)
	}
}

func TestCreateRegistrationTokenUsesDefaultLifetimeWhenZero(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	before := time.Now()
	created, err := store.CreateRegistrationToken(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	after := time.Now()
	if created.ExpiresAt.Before(before.Add(DefaultRegistrationTokenLifetime)) || created.ExpiresAt.After(after.Add(DefaultRegistrationTokenLifetime)) {
		t.Fatalf("default expiry = %s, want approximately %s after creation", created.ExpiresAt, DefaultRegistrationTokenLifetime)
	}
}

func TestCreateRegistrationTokenRejectsNegativeLifetime(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	if _, err := store.CreateRegistrationToken(context.Background(), -time.Second); err == nil {
		t.Fatal("negative registration token lifetime was accepted")
	}
}

func TestRegisterNodeRejectsExpiredRegistrationTokenWithoutRows(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	registration, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	now := registration.ExpiresAt.Add(time.Second)
	if _, err := store.RegisterNode(context.Background(), registration.Token, NodeInput{UUID: testUUID("expired-node"), Name: "expired node"}, now); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("RegisterNode error = %v, want ErrTokenExpired", err)
	}
	assertRowCount(t, store, `SELECT count(*) FROM nodes`, 0)
	assertRowCount(t, store, `SELECT count(*) FROM node_tokens`, 0)
}

func TestConcurrentRegisterNodeWithSameTokenHasSingleWinnerAndNoPartialRows(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	registration, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	const attempts = 8
	type result struct {
		node RegisteredNode
		err  error
	}
	results := make(chan result, attempts)
	var group sync.WaitGroup
	for i := 0; i < attempts; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			results <- resultFromRegisterNode(store, registration.Token, NodeInput{UUID: testUUID("concurrent-" + string(rune('a'+i))), Name: "concurrent node"})
		}(i)
	}
	group.Wait()
	close(results)

	winners := 0
	for result := range results {
		if result.err == nil {
			winners++
			continue
		}
		if !errors.Is(result.err, ErrTokenAlreadyConsumed) {
			t.Fatalf("concurrent RegisterNode error = %v, want ErrTokenAlreadyConsumed", result.err)
		}
	}
	if winners != 1 {
		t.Fatalf("successful concurrent registrations = %d, want 1", winners)
	}
	assertRowCount(t, store, `SELECT count(*) FROM nodes`, 1)
	assertRowCount(t, store, `SELECT count(*) FROM node_tokens`, 1)
	assertRowCount(t, store, `SELECT count(*) FROM nodes n LEFT JOIN node_tokens t ON t.node_id = n.id WHERE t.id IS NULL`, 0)
	assertRowCount(t, store, `SELECT count(*) FROM node_tokens t LEFT JOIN nodes n ON n.id = t.node_id WHERE n.id IS NULL`, 0)
}

func TestNodeTokenPlaintextIsNotPersisted(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	registration, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	node, err := store.RegisterNode(context.Background(), registration.Token, NodeInput{UUID: testUUID("node-uuid"), Name: "test node"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if node.Token == "" {
		t.Fatal("RegisterNode returned an empty plaintext token")
	}

	rows, err := store.db.Query(`SELECT token_digest, token_prefix FROM node_tokens`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var digest []byte
		var prefix string
		if err := rows.Scan(&digest, &prefix); err != nil {
			t.Fatal(err)
		}
		if string(digest) == node.Token {
			t.Fatalf("plaintext node token was persisted")
		}
		if len(digest) != 32 {
			t.Fatalf("stored digest length = %d, want 32", len(digest))
		}
		if prefix == "" || prefix == node.Token || len(prefix) >= len(node.Token) || !strings.HasPrefix(node.Token, prefix) {
			t.Fatalf("stored token prefix = %q, want a non-secret prefix of token", prefix)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestNodeTokenPrefixIsPersistedOnRotation(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	node := registerTestNode(t, store, "prefix-rotation-node")
	rotated, err := store.RotateNodeToken(context.Background(), node.Node.ID, "admin-1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var prefix string
	if err := store.db.QueryRow(`SELECT token_prefix FROM node_tokens WHERE node_id = ? AND revoked_at IS NULL`, node.Node.ID).Scan(&prefix); err != nil {
		t.Fatal(err)
	}
	if prefix == "" || prefix == rotated || len(prefix) >= len(rotated) || !strings.HasPrefix(rotated, prefix) {
		t.Fatalf("rotated token prefix = %q, want a non-secret prefix of rotated token", prefix)
	}
}

func TestNodeTokenExpiryRejectsAtBoundary(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	now := time.Unix(1_700_000_000, 0).UTC()
	registration, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	registered, err := store.RegisterNodeWithTTL(context.Background(), registration.Token, NodeInput{UUID: testUUID("expiry-node"), Name: "expiry node"}, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AuthenticateNodeToken(context.Background(), registered.Token, now.Add(time.Hour)); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("boundary authentication error = %v, want ErrTokenExpired", err)
	}
}

func TestNodeTokenRegistrationAndRotationPersistConfiguredExpiry(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	now := time.Unix(1_700_000_000, 0).UTC()
	const lifetime = 3 * time.Hour
	registration, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	registered, err := store.RegisterNodeWithTTL(context.Background(), registration.Token, NodeInput{UUID: testUUID("configured-expiry-node"), Name: "configured expiry node"}, now, lifetime)
	if err != nil {
		t.Fatal(err)
	}
	assertNodeTokenExpiry(t, store, registered.Node.ID, now.Add(lifetime))

	rotatedAt := now.Add(time.Hour)
	rotated, err := store.RotateNodeTokenWithTTL(context.Background(), registered.Node.ID, "admin-1", rotatedAt, 5*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AuthenticateNodeToken(context.Background(), rotated, rotatedAt.Add(5*time.Hour)); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("rotated boundary authentication error = %v, want ErrTokenExpired", err)
	}
	assertNodeTokenExpiry(t, store, registered.Node.ID, rotatedAt.Add(5*time.Hour))
}

func TestLegacyNodeTokenMethodsUseDefaultLifetime(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	now := time.Unix(1_700_000_000, 0).UTC()
	registration, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	registered, err := store.RegisterNode(context.Background(), registration.Token, NodeInput{UUID: testUUID("legacy-method-node"), Name: "legacy method node"}, now)
	if err != nil {
		t.Fatal(err)
	}
	assertNodeTokenExpiry(t, store, registered.Node.ID, now.Add(DefaultNodeTokenLifetime))

	rotated, err := store.RotateNodeToken(context.Background(), registered.Node.ID, "admin-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AuthenticateNodeToken(context.Background(), rotated, now); err != nil {
		t.Fatal(err)
	}
	assertNodeTokenExpiry(t, store, registered.Node.ID, now.Add(DefaultNodeTokenLifetime))
}

func TestRegistrationTokenPlaintextIsNotPersisted(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	registration, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var stored []byte
	if err := store.db.QueryRow(`SELECT token_digest FROM registration_tokens WHERE id = ?`, registration.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if string(stored) == registration.Token {
		t.Fatal("plaintext registration token was persisted")
	}
	if len(stored) != 32 {
		t.Fatalf("stored digest length = %d, want 32", len(stored))
	}
}

func TestRevokedNodeTokenCannotAuthenticate(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	node := registerTestNode(t, store, "revoke-node")
	if _, err := store.AuthenticateNodeToken(context.Background(), node.Token, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeNodeToken(context.Background(), node.Node.ID, "admin-1", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AuthenticateNodeToken(context.Background(), node.Token, time.Now()); !errors.Is(err, ErrTokenRevoked) {
		t.Fatalf("authentication error = %v, want ErrTokenRevoked", err)
	}
	assertAuditAction(t, store, "revoke", node.Node.ID)
}

func TestRotateNodeTokenInvalidatesOldTokenAndAudits(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	original := registerTestNode(t, store, "rotate-node")
	rotated, err := store.RotateNodeToken(context.Background(), original.Node.ID, "admin-1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if rotated == "" || rotated == original.Token {
		t.Fatal("rotation did not return a new token")
	}
	if _, err := store.AuthenticateNodeToken(context.Background(), original.Token, time.Now()); !errors.Is(err, ErrTokenRevoked) {
		t.Fatalf("old token authentication error = %v, want ErrTokenRevoked", err)
	}
	if _, err := store.AuthenticateNodeToken(context.Background(), rotated, time.Now()); err != nil {
		t.Fatalf("new token authentication error = %v", err)
	}
	assertAuditAction(t, store, "rotate", original.Node.ID)
}

func TestDeleteNodeAuditsAndPreventsAuthentication(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	node := registerTestNode(t, store, "delete-node")
	if err := store.DeleteNode(context.Background(), node.Node.ID, "admin-1", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AuthenticateNodeToken(context.Background(), node.Token, time.Now()); !errors.Is(err, ErrNodeDeleted) {
		t.Fatalf("authentication error = %v, want ErrNodeDeleted", err)
	}
	assertAuditAction(t, store, "delete", node.Node.ID)
}

func TestCleanupLifecycleRemovesOnlyEligibleRows(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	now := time.Unix(1_800_000_000, 0).UTC()
	old30 := unixNano(now.Add(-31 * 24 * time.Hour))
	old90 := unixNano(now.Add(-91 * 24 * time.Hour))
	fresh := unixNano(now.Add(-time.Hour))
	if _, err := store.db.Exec(`INSERT INTO nodes (id,uuid,name,created_at,updated_at) VALUES ('cleanup-node','550e8400-e29b-41d4-a716-446655440099','cleanup',?,?)`, fresh, fresh); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`INSERT INTO resource_history (id,node_id,payload,reported_at,recorded_at) VALUES (1,'cleanup-node',X'01',?,?), (2,'cleanup-node',X'02',?,?)`, old30, old30, fresh, fresh); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`INSERT INTO audit_events (id,action,created_at) VALUES ('old-audit','test',?), ('fresh-audit','test',?)`, old90, fresh); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`INSERT INTO registration_tokens (id,token_digest,expires_at,created_at) VALUES ('expired-reg',X'11',?,?), ('valid-reg',X'12',?,?)`, old30, old30, now.Add(time.Hour).UnixNano(), fresh); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`INSERT INTO request_replays (node_id,request_id,expires_at,created_at) VALUES ('n','expired',?,?), ('n','valid',?,?)`, old30, old30, now.Add(time.Hour).UnixNano(), fresh); err != nil {
		t.Fatal(err)
	}
	result, err := store.CleanupLifecycle(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if result.ResourceHistory != 1 || result.AuditEvents != 1 || result.RegistrationTokens != 1 || result.RequestReplays != 1 {
		t.Fatalf("cleanup result = %#v", result)
	}
	assertRowCount(t, store, `SELECT count(*) FROM resource_history`, 1)
	assertRowCount(t, store, `SELECT count(*) FROM audit_events`, 1)
	assertRowCount(t, store, `SELECT count(*) FROM registration_tokens`, 1)
	assertRowCount(t, store, `SELECT count(*) FROM request_replays`, 1)
}

func TestRequestIDReplayIsRejectedAndExpiredIDsAreCleaned(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	ctx := context.Background()
	expiresAt := time.Now().Add(time.Hour)
	if err := store.InsertRequestReplay(ctx, "node-1", "request-1", expiresAt, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertRequestReplay(ctx, "node-1", "request-1", expiresAt, time.Now()); !errors.Is(err, ErrReplay) {
		t.Fatalf("duplicate replay error = %v, want ErrReplay", err)
	}
	if err := store.InsertRequestReplay(ctx, "node-2", "request-1", expiresAt, time.Now()); err != nil {
		t.Fatalf("same request ID on another node failed: %v", err)
	}
	if err := store.InsertRequestReplay(ctx, "node-1", "old-request", time.Now().Add(-time.Minute), time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	deleted, err := store.CleanupExpiredReplays(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("deleted replay rows = %d, want 1", deleted)
	}
}

func TestInsertRequestReplayReusesExpiredRequestedKeyOutsideCleanupBatch(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	ctx := context.Background()
	now := time.Now().UTC()
	seedExpiredReplays(t, store, maxReplayCleanupRows+1, now.Add(-2*time.Minute))
	if _, err := store.db.Exec(`INSERT INTO request_replays (node_id, request_id, expires_at, created_at) VALUES (?, ?, ?, ?)`, "target-node", "target-request", unixNano(now.Add(-time.Minute)), unixNano(now.Add(-2*time.Minute))); err != nil {
		t.Fatal(err)
	}

	if err := store.InsertRequestReplay(ctx, "target-node", "target-request", now.Add(time.Hour), now); err != nil {
		t.Fatalf("InsertRequestReplay did not reuse the expired requested key: %v", err)
	}
	assertRowCount(t, store, `SELECT count(*) FROM request_replays WHERE node_id = 'target-node' AND request_id = 'target-request' AND expires_at <= ?`, 0, unixNano(now))
	assertRowCount(t, store, `SELECT count(*) FROM request_replays WHERE node_id = 'target-node' AND request_id = 'target-request' AND expires_at > ?`, 1, unixNano(now))
	assertRowCount(t, store, `SELECT count(*) FROM request_replays WHERE node_id = 'noise-node'`, 1)
}

func TestCleanupExpiredReplaysIsBoundedAndOrdered(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	now := time.Now().UTC()
	seedExpiredReplays(t, store, maxReplayCleanupRows+25, now.Add(-time.Minute))
	deleted, err := store.CleanupExpiredReplays(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != maxReplayCleanupRows {
		t.Fatalf("first cleanup deleted %d rows, want %d", deleted, maxReplayCleanupRows)
	}
	assertRowCount(t, store, `SELECT count(*) FROM request_replays`, 25)
	assertRowCount(t, store, `SELECT count(*) FROM request_replays WHERE request_id = 'noise-request-0000'`, 0)

	deleted, err = store.CleanupExpiredReplays(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 25 {
		t.Fatalf("second cleanup deleted %d rows, want 25", deleted)
	}
	deleted, err = store.CleanupExpiredReplays(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Fatalf("third cleanup deleted %d rows, want 0", deleted)
	}
}

func TestInsertRequestReplayCleansExpiredRowsWithoutSeparateCleanup(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	ctx := context.Background()
	now := time.Now()
	if err := store.InsertRequestReplay(ctx, "node-1", "expired-request", now.Add(-time.Minute), now.Add(-2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertRequestReplay(ctx, "node-1", "fresh-request", now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}

	assertRowCount(t, store, `SELECT count(*) FROM request_replays WHERE request_id = 'expired-request'`, 0)
	assertRowCount(t, store, `SELECT count(*) FROM request_replays WHERE request_id = 'fresh-request'`, 1)
}

func TestPersistAgentResultRollsBackReplayWhenResultPersistenceFails(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	node := registerTestNode(t, store, "atomic-result-node")
	if _, err := store.CreateTarget(context.Background(), TargetDefinition{ID: "atomic-target", Name: "Atomic target", Kind: TargetKindTCP, Host: "example.com", Enabled: true, Payload: []byte(`{"port":443}`)}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`CREATE TRIGGER fail_atomic_result BEFORE INSERT ON network_results_latest BEGIN SELECT RAISE(ABORT, 'forced result failure'); END`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	input := AgentResultInput{Kind: TargetKindTCP, TargetID: "atomic-target", CheckedAt: now, Payload: []byte(`{"status":"ok"}`)}
	if err := store.PersistAgentResult(context.Background(), node.Node.ID, "atomic-request", now.Add(time.Hour), now, input); err == nil {
		t.Fatal("PersistAgentResult accepted forced persistence failure")
	}
	assertRowCount(t, store, `SELECT count(*) FROM request_replays WHERE node_id = ? AND request_id = ?`, 0, node.Node.ID, "atomic-request")
	if _, err := store.db.Exec(`DROP TRIGGER fail_atomic_result`); err != nil {
		t.Fatal(err)
	}
	if err := store.PersistAgentResult(context.Background(), node.Node.ID, "atomic-request", now.Add(time.Hour), now, input); err != nil {
		t.Fatalf("retry after persistence failure = %v", err)
	}
	assertRowCount(t, store, `SELECT count(*) FROM request_replays WHERE node_id = ? AND request_id = ?`, 1, node.Node.ID, "atomic-request")
}

func TestConcurrentRegistrationTokenConsumeHasSingleWinner(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	created, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	const attempts = 8
	results := make(chan error, attempts)
	var group sync.WaitGroup
	for range attempts {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := store.ConsumeRegistrationToken(context.Background(), created.Token, time.Now())
			results <- err
		}()
	}
	group.Wait()
	close(results)

	winners := 0
	for err := range results {
		if err == nil {
			winners++
			continue
		}
		if !errors.Is(err, ErrTokenAlreadyConsumed) {
			t.Fatalf("concurrent consume error = %v, want ErrTokenAlreadyConsumed", err)
		}
	}
	if winners != 1 {
		t.Fatalf("successful concurrent consumes = %d, want 1", winners)
	}
}

func TestTwoStoresConcurrentRegistrationTokenConsumeHasSingleWinner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "shared.db")
	first, err := OpenStore(path, []byte(testPepper))
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := OpenStore(path, []byte(testPepper))
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	created, err := first.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	start := make(chan struct{})
	for _, store := range []*Store{first, second} {
		go func(store *Store) {
			<-start
			_, err := store.ConsumeRegistrationToken(context.Background(), created.Token, time.Now())
			results <- err
		}(store)
	}
	close(start)

	winners := 0
	for range 2 {
		if err := <-results; err == nil {
			winners++
		} else if !errors.Is(err, ErrTokenAlreadyConsumed) {
			t.Fatalf("cross-store consume error = %v, want ErrTokenAlreadyConsumed for the loser", err)
		}
	}
	if winners != 1 {
		t.Fatalf("cross-store successful consumes = %d, want 1", winners)
	}
}

func TestTwoStoresConcurrentRegisterNodeHasSingleWinnerAndNoPartialRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "shared.db")
	first, err := OpenStore(path, []byte(testPepper))
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := OpenStore(path, []byte(testPepper))
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	created, err := first.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	start := make(chan struct{})
	for i, store := range []*Store{first, second} {
		go func(i int, store *Store) {
			<-start
			_, err := store.RegisterNode(context.Background(), created.Token, NodeInput{UUID: testUUID("cross-store-node-" + string(rune('a'+i))), Name: "cross-store node"}, time.Now())
			results <- err
		}(i, store)
	}
	close(start)

	winners := 0
	for range 2 {
		if err := <-results; err == nil {
			winners++
		} else if !errors.Is(err, ErrTokenAlreadyConsumed) {
			t.Fatalf("cross-store registration error = %v, want ErrTokenAlreadyConsumed for the loser", err)
		}
	}
	if winners != 1 {
		t.Fatalf("cross-store successful registrations = %d, want 1", winners)
	}
	assertRowCount(t, first, `SELECT count(*) FROM nodes`, 1)
	assertRowCount(t, first, `SELECT count(*) FROM node_tokens`, 1)
	assertRowCount(t, first, `SELECT count(*) FROM nodes n LEFT JOIN node_tokens t ON t.node_id = n.id WHERE t.id IS NULL`, 0)
}

func TestRegisterNodeRollsBackAfterConsumeFailure(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	registration, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.db.Exec(`CREATE TRIGGER fail_registration_consume BEFORE UPDATE OF consumed_at ON registration_tokens BEGIN SELECT RAISE(ABORT, 'forced registration consume failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Exec(`DROP TRIGGER fail_registration_consume`)

	if _, err := store.RegisterNode(context.Background(), registration.Token, NodeInput{UUID: testUUID("rollback-node"), Name: "rollback node"}, time.Now()); err == nil {
		t.Fatal("RegisterNode accepted a forced post-write failure")
	}
	assertRowCount(t, store, `SELECT count(*) FROM nodes`, 0)
	assertRowCount(t, store, `SELECT count(*) FROM node_tokens`, 0)
	assertRowCount(t, store, `SELECT count(*) FROM registration_tokens WHERE consumed_at IS NOT NULL`, 0)
	assertRowCount(t, store, `SELECT count(*) FROM audit_events`, 0)
}

func TestRegisterNodeRollsBackAfterConditionalConsumeAffectsZeroRows(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	registration, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.db.Exec(`CREATE TRIGGER ignore_registration_consume_direct BEFORE UPDATE OF consumed_at ON registration_tokens BEGIN SELECT RAISE(IGNORE); END`)
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Exec(`DROP TRIGGER ignore_registration_consume_direct`)

	if _, err := store.RegisterNode(context.Background(), registration.Token, NodeInput{UUID: testUUID("zero-row-node"), Name: "zero row node"}, time.Now()); !errors.Is(err, ErrTokenAlreadyConsumed) {
		t.Fatalf("RegisterNode error = %v, want ErrTokenAlreadyConsumed", err)
	}
	assertRowCount(t, store, `SELECT count(*) FROM nodes`, 0)
	assertRowCount(t, store, `SELECT count(*) FROM node_tokens`, 0)
	assertRowCount(t, store, `SELECT count(*) FROM registration_tokens WHERE consumed_at IS NOT NULL`, 0)
	assertRowCount(t, store, `SELECT count(*) FROM audit_events`, 0)
}

func TestRotateNodeTokenRequiresAnActiveToken(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	node := registerTestNode(t, store, "rotate-revoked-node")
	if err := store.RevokeNodeToken(context.Background(), node.Node.ID, "admin-1", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RotateNodeToken(context.Background(), node.Node.ID, "admin-1", time.Now()); !errors.Is(err, ErrTokenRevoked) {
		t.Fatalf("rotation error = %v, want ErrTokenRevoked", err)
	}
	assertRowCount(t, store, `SELECT count(*) FROM node_tokens WHERE node_id = ?`, 1, node.Node.ID)
	assertRowCount(t, store, `SELECT count(*) FROM audit_events WHERE node_id = ? AND action = 'rotate'`, 0, node.Node.ID)
}

func resultFromRegisterNode(store *Store, token string, input NodeInput) struct {
	node RegisteredNode
	err  error
} {
	node, err := store.RegisterNode(context.Background(), token, input, time.Now())
	return struct {
		node RegisteredNode
		err  error
	}{node: node, err: err}
}

func assertRowCount(t *testing.T, store *Store, query string, want int, args ...any) {
	t.Helper()
	var got int
	if err := store.db.QueryRow(query, args...).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("row count for %q = %d, want %d", query, got, want)
	}
}

func assertNodeTokenExpiry(t *testing.T, store *Store, nodeID string, want time.Time) {
	t.Helper()
	var expiresAt int64
	if err := store.db.QueryRow(`SELECT expires_at FROM node_tokens WHERE node_id = ? AND revoked_at IS NULL`, nodeID).Scan(&expiresAt); err != nil {
		t.Fatal(err)
	}
	if got := time.Unix(0, expiresAt).UTC(); !got.Equal(want) {
		t.Fatalf("node token expiry = %s, want %s", got, want)
	}
}

func seedExpiredReplays(t *testing.T, store *Store, count int, expiresAt time.Time) {
	t.Helper()
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < count; i++ {
		if _, err := tx.Exec(`INSERT INTO request_replays (node_id, request_id, expires_at, created_at) VALUES (?, ?, ?, ?)`, "noise-node", fmt.Sprintf("noise-request-%04d", i), unixNano(expiresAt), unixNano(expiresAt.Add(-time.Minute))); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func seedExpiredOAuthStates(t *testing.T, store *Store, count int, expiresAt time.Time) {
	t.Helper()
	for i := 0; i < count; i++ {
		if _, err := store.CreateOAuthState(context.Background(), []byte(fmt.Sprintf("oauth-state-%04d", i)), expiresAt.Add(time.Duration(i)*time.Nanosecond)); err != nil {
			t.Fatal(err)
		}
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "data", "test.db"), []byte(testPepper))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func registerTestNode(t *testing.T, store *Store, uuid string) RegisteredNode {
	t.Helper()
	registration, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	node, err := store.RegisterNode(context.Background(), registration.Token, NodeInput{UUID: testUUID(uuid), Name: uuid}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return node
}

func testUUID(value string) string {
	if len(value) == 36 && value[8] == '-' && value[13] == '-' && value[18] == '-' && value[23] == '-' {
		return value
	}
	digest := sha256.Sum256([]byte(value))
	return "550e8400-e29b-41d4-a716-" + hex.EncodeToString(digest[:])[:12]
}

func assertAuditAction(t *testing.T, store *Store, action, nodeID string) {
	t.Helper()
	var count int
	if err := store.db.QueryRow(`SELECT count(*) FROM audit_events WHERE action = ? AND node_id = ?`, action, nodeID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("audit action %q count = %d, want 1", action, count)
	}
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %04o, want %04o", path, got, want)
	}
}
