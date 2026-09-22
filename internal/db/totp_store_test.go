package db

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func adminUserColumnNames(t *testing.T, store *Store) map[string]bool {
	t.Helper()
	rows, err := store.db.Query(`PRAGMA table_info(admin_users)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns := make(map[string]bool)
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return columns
}

func TestOpenStoreAddsAdminTOTPColumns(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	columns := adminUserColumnNames(t, store)
	if !columns["totp_secret"] || !columns["totp_enabled"] {
		t.Fatalf("admin_users is missing TOTP columns: %v", columns)
	}

	admin, err := store.UpsertAdminUser(context.Background(), "github", "totp-default", "alice", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	secret, enabled, err := store.GetAdminUserTOTP(context.Background(), admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if enabled || len(secret) != 0 {
		t.Fatalf("fresh admin has TOTP state: secret=%q enabled=%v", secret, enabled)
	}
}

func TestOpenStoreMigratesLegacyAdminUsersWithTOTPIdempotently(t *testing.T) {
	path := t.TempDir() + "/legacy-admin.db"
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(`
		CREATE TABLE admin_users (
			id TEXT PRIMARY KEY,
			provider TEXT NOT NULL,
			provider_user_id TEXT NOT NULL,
			login TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			UNIQUE(provider, provider_user_id)
		);
		INSERT INTO admin_users (id, provider, provider_user_id, login, created_at, updated_at)
		VALUES ('legacy-admin', 'github', '42', 'alice', 1700000000000000000, 1700000000000000000);
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
		t.Fatalf("first migration failed: %v", err)
	}
	columns := adminUserColumnNames(t, store)
	if !columns["totp_secret"] || !columns["totp_enabled"] {
		store.Close()
		t.Fatalf("legacy admin_users did not gain TOTP columns: %v", columns)
	}
	var login string
	if err := store.db.QueryRow(`SELECT login FROM admin_users WHERE id = 'legacy-admin'`).Scan(&login); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if login != "alice" {
		store.Close()
		t.Fatalf("legacy admin row damaged by migration: login=%q", login)
	}
	if err := store.SetAdminUserTOTP(context.Background(), "legacy-admin", []byte("SECRETBASE32"), true, time.Now()); err != nil {
		store.Close()
		t.Fatalf("set totp on migrated row: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopening runs the migration again; it must be a no-op and keep state.
	reopened, err := OpenStore(path, []byte(testPepper))
	if err != nil {
		t.Fatalf("second migration failed: %v", err)
	}
	defer reopened.Close()
	secret, enabled, err := reopened.GetAdminUserTOTP(context.Background(), "legacy-admin")
	if err != nil {
		t.Fatal(err)
	}
	if !enabled || string(secret) != "SECRETBASE32" {
		t.Fatalf("reopened store lost TOTP state: secret=%q enabled=%v", secret, enabled)
	}
}

func TestSetAdminUserTOTPRoundTripAndUnknownAdmin(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	admin, err := store.UpsertAdminUser(context.Background(), "github", "totp-roundtrip", "alice", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := store.SetAdminUserTOTP(ctx, admin.ID, []byte("JBSWY3DPEHPK3PXP"), true, time.Now()); err != nil {
		t.Fatal(err)
	}
	secret, enabled, err := store.GetAdminUserTOTP(ctx, admin.ID)
	if err != nil || !enabled || string(secret) != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("round trip mismatch: secret=%q enabled=%v err=%v", secret, enabled, err)
	}

	if err := store.SetAdminUserTOTP(ctx, admin.ID, nil, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	secret, enabled, err = store.GetAdminUserTOTP(ctx, admin.ID)
	if err != nil || enabled || len(secret) != 0 {
		t.Fatalf("disable did not clear state: secret=%q enabled=%v err=%v", secret, enabled, err)
	}

	if err := store.SetAdminUserTOTP(ctx, "missing-admin", []byte("X"), true, time.Now()); err != sql.ErrNoRows {
		t.Fatalf("set totp for unknown admin error = %v, want sql.ErrNoRows", err)
	}
	if _, _, err := store.GetAdminUserTOTP(ctx, "missing-admin"); err != sql.ErrNoRows {
		t.Fatalf("get totp for unknown admin error = %v, want sql.ErrNoRows", err)
	}
}

func TestTOTPPendingStateLifecycle(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	admin, err := store.UpsertAdminUser(context.Background(), "github", "totp-pending", "alice", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().UTC()

	token, err := store.CreateTOTPPendingState(ctx, admin.ID, now, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Fatal("pending credential is empty")
	}
	for attempt := 0; attempt < 2; attempt++ {
		resolved, err := store.GetTOTPPendingState(ctx, token, now.Add(time.Second))
		if err != nil {
			t.Fatalf("get pending state (attempt %d): %v", attempt, err)
		}
		if resolved != admin.ID {
			t.Fatalf("pending state resolved to %q, want %q", resolved, admin.ID)
		}
	}
	if err := store.ConsumeTOTPPendingState(ctx, token, now.Add(2*time.Second)); err != nil {
		t.Fatalf("consume pending state: %v", err)
	}
	if _, err := store.GetTOTPPendingState(ctx, token, now.Add(3*time.Second)); err != ErrTOTPPendingConsumed {
		t.Fatalf("consumed pending state error = %v, want ErrTOTPPendingConsumed", err)
	}
	if err := store.ConsumeTOTPPendingState(ctx, token, now.Add(4*time.Second)); err != ErrTOTPPendingInvalid {
		t.Fatalf("double consume error = %v, want ErrTOTPPendingInvalid", err)
	}

	expired, err := store.CreateTOTPPendingState(ctx, admin.ID, now.Add(-10*time.Minute), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetTOTPPendingState(ctx, expired, now); err != ErrTOTPPendingExpired {
		t.Fatalf("expired pending state error = %v, want ErrTOTPPendingExpired", err)
	}
	if _, err := store.GetTOTPPendingState(ctx, "unknown-token", now); err != ErrTOTPPendingInvalid {
		t.Fatalf("unknown pending state error = %v, want ErrTOTPPendingInvalid", err)
	}

	if _, err := store.CreateTOTPPendingState(ctx, "missing-admin", now, time.Minute); err == nil {
		t.Fatal("pending state created for unknown admin")
	}

	live, err := store.CreateTOTPPendingState(ctx, admin.ID, now, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := store.CleanupExpiredTOTPPendingStates(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if deleted < 2 {
		t.Fatalf("cleanup deleted %d rows, want at least the expired and consumed rows", deleted)
	}
	if _, err := store.GetTOTPPendingState(ctx, expired, now); err != ErrTOTPPendingInvalid {
		t.Fatal("cleanup kept the expired pending state")
	}
	if _, err := store.GetTOTPPendingState(ctx, token, now); err != ErrTOTPPendingInvalid {
		t.Fatal("cleanup kept the consumed pending state")
	}
	if _, err := store.GetTOTPPendingState(ctx, live, now); err != nil {
		t.Fatalf("cleanup removed the live pending state: %v", err)
	}
}
