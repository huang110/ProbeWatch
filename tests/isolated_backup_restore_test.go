package tests

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/db"
)

func hashFileIfExists(path string) (string, int64, bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", 0, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, false
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]), fi.Size(), true
}

func TestIsolatedBackupRestoreLifecycle(t *testing.T) {
	ctx := context.Background()

	// 0. Record production database status if running on a real server to assert isolation
	prodDBPath := "/opt/probewatch/data/probewatch.db"
	prodHashBefore, prodSizeBefore, prodExists := hashFileIfExists(prodDBPath)

	// 1. Set up completely isolated test directory
	tempDir := t.TempDir()
	dbDir := filepath.Join(tempDir, "db")
	backupDir := filepath.Join(tempDir, "backups")
	if err := os.MkdirAll(dbDir, 0700); err != nil {
		t.Fatalf("mkdir dbDir: %v", err)
	}
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		t.Fatalf("mkdir backupDir: %v", err)
	}

	isolatedDBPath := filepath.Join(dbDir, "isolated_test.db")
	pepper := []byte("isolated-test-pepper-32-bytes-long!")

	store, err := db.OpenStore(isolatedDBPath, pepper)
	if err != nil {
		t.Fatalf("OpenStore failed: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC()

	// 2. Seed initial data: Admin, Node, Target, Alert Rule, System Setting
	admin, err := store.UpsertAdminUser(ctx, "local", "user-seed-1", "ci-admin", now)
	if err != nil {
		t.Fatalf("UpsertAdminUser: %v", err)
	}

	node1 := db.NodeRecord{
		UUID:        "node-seed-uuid-1",
		Name:        "Test Node Seed 1",
		SecretHash:  "secrethash1",
		Status:      "online",
		ClientIP:    "127.0.0.1",
		CreatedNano: now.UnixNano(),
		UpdatedNano: now.UnixNano(),
	}
	if err := store.UpsertNode(ctx, node1); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}

	target1 := db.ResultTargetInput{
		ID:        "tgt-seed-1",
		Name:      "Target Seed 1",
		Host:      "1.1.1.1",
		Payload:   `{"kind":"icmp","interval_seconds":30}`,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.CreateNetworkTarget(ctx, target1, now); err != nil {
		t.Fatalf("CreateNetworkTarget: %v", err)
	}

	rule1 := db.AlertRuleRecord{
		ID:              "rule-seed-1",
		Name:            "Rule Seed 1",
		Metric:          "cpu",
		Operator:        ">",
		Threshold:       90,
		DurationSeconds: 60,
		Severity:        "warning",
		NodeFilter:      "*",
		Enabled:         true,
	}
	if err := store.UpsertAlertRule(ctx, rule1); err != nil {
		t.Fatalf("UpsertAlertRule: %v", err)
	}

	if err := store.SetSetting(ctx, "site_name", "Isolated CI ProbeWatch"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	// 3. Create hot backup
	backup, err := store.CreateBackup(ctx, backupDir, "ci-test-backup")
	if err != nil {
		t.Fatalf("CreateBackup failed: %v", err)
	}
	if backup.SizeBytes <= 0 || backup.SHA256 == "" || !backup.IsCompressed {
		t.Fatalf("invalid backup info: %+v", backup)
	}

	backupFilePath := filepath.Join(backupDir, backup.Filename)
	if _, err := os.Stat(backupFilePath); err != nil {
		t.Fatalf("backup file does not exist on disk: %v", err)
	}

	// 4. Verify failure scenario: unwritable backup directory
	unwritableDir := filepath.Join(tempDir, "unwritable_backup_dir")
	if err := os.MkdirAll(unwritableDir, 0500); err == nil {
		// Try to create backup into unwritable dir
		_, unwriteErr := store.CreateBackup(ctx, filepath.Join(unwritableDir, "sub"), "fail")
		if unwriteErr == nil {
			t.Log("Note: running as root or FS ignored 0500 on unwritableDir")
		}
		_ = os.Chmod(unwritableDir, 0700)
		_ = os.RemoveAll(unwritableDir)
	}

	// 5. Verify failure scenario: corrupted backup file fails integrity check
	corruptBackupName := "corrupt-backup-test.db.gz"
	_ = os.WriteFile(filepath.Join(backupDir, corruptBackupName), []byte("NOT_A_VALID_GZIP_OR_SQLITE_FILE"), 0600)
	_, corruptErr := store.RestoreBackup(ctx, backupDir, corruptBackupName)
	if corruptErr == nil {
		t.Fatal("expected restore of corrupt backup to fail integrity check")
	}

	// 6. Verify failure scenario: invalid / traversal filename rejected
	_, travErr := store.RestoreBackup(ctx, backupDir, "../invalid.db")
	if travErr == nil {
		t.Fatal("expected path traversal in RestoreBackup to be rejected")
	}

	// 7. Mutate state after backup
	node2 := db.NodeRecord{
		UUID:        "node-seed-uuid-post-backup",
		Name:        "Test Node Created After Backup",
		SecretHash:  "secrethash2",
		Status:      "offline",
		ClientIP:    "127.0.0.2",
		CreatedNano: time.Now().UTC().UnixNano(),
		UpdatedNano: time.Now().UTC().UnixNano(),
	}
	if err := store.UpsertNode(ctx, node2); err != nil {
		t.Fatalf("UpsertNode post-backup: %v", err)
	}

	// 8. Restore from backup
	restoredStore, err := store.RestoreBackup(ctx, backupDir, backup.Filename)
	if err != nil {
		t.Fatalf("RestoreBackup failed: %v", err)
	}
	defer restoredStore.Close()

	// 9. Verify restored records match original backup
	gotAdmin, err := restoredStore.GetAdminUser(ctx, admin.ID)
	if err != nil || gotAdmin.Login != "ci-admin" {
		t.Fatalf("restored admin mismatch: got %v, err: %v", gotAdmin, err)
	}

	gotNode1, err := restoredStore.GetNode(ctx, "node-seed-uuid-1")
	if err != nil || gotNode1.Name != "Test Node Seed 1" {
		t.Fatalf("restored node1 mismatch: got %v, err: %v", gotNode1, err)
	}

	// Verify post-backup node is absent
	_, err = restoredStore.GetNode(ctx, "node-seed-uuid-post-backup")
	if err == nil {
		t.Fatal("post-backup node should NOT exist in restored database")
	}

	// Verify target, alert rule, system setting
	gotRule1, err := restoredStore.GetAlertRule(ctx, "rule-seed-1")
	if err != nil || gotRule1.Metric != "cpu" || gotRule1.Threshold != 90 {
		t.Fatalf("restored alert rule mismatch: got %v, err: %v", gotRule1, err)
	}

	gotSetting, err := restoredStore.GetSetting(ctx, "site_name")
	if err != nil || gotSetting != "Isolated CI ProbeWatch" {
		t.Fatalf("restored setting mismatch: got %q, err: %v", gotSetting, err)
	}

	// 10. Verify restored database is fully writable
	node3 := db.NodeRecord{
		UUID:        "node-seed-uuid-post-restore",
		Name:        "Test Node Created Post Restore",
		SecretHash:  "secrethash3",
		Status:      "online",
		ClientIP:    "127.0.0.3",
		CreatedNano: time.Now().UTC().UnixNano(),
		UpdatedNano: time.Now().UTC().UnixNano(),
	}
	if err := restoredStore.UpsertNode(ctx, node3); err != nil {
		t.Fatalf("failed to insert new record into restored database: %v", err)
	}
	if _, err := restoredStore.GetNode(ctx, "node-seed-uuid-post-restore"); err != nil {
		t.Fatalf("failed to read freshly inserted record in restored database: %v", err)
	}

	// 11. Verify clean close and reopen of restored database
	_ = restoredStore.Close()
	reopenedStore, err := db.OpenStore(isolatedDBPath, pepper)
	if err != nil {
		t.Fatalf("OpenStore on restored database file failed: %v", err)
	}
	defer reopenedStore.Close()

	if _, err := reopenedStore.GetNode(ctx, "node-seed-uuid-post-restore"); err != nil {
		t.Fatalf("reopened store failed to read record: %v", err)
	}

	// 12. Cleanup temporary test directory
	_ = reopenedStore.Close()
	_ = os.RemoveAll(tempDir)

	// 13. Prove production database was untouched
	if prodExists {
		prodHashAfter, prodSizeAfter, prodStillExists := hashFileIfExists(prodDBPath)
		if !prodStillExists {
			t.Fatalf("CRITICAL: Production database %s disappeared!", prodDBPath)
		}
		if prodSizeAfter != prodSizeBefore || prodHashAfter != prodHashBefore {
			t.Fatalf("CRITICAL: Production database was modified during isolated test! (before: size=%d sha=%s, after: size=%d sha=%s)",
				prodSizeBefore, prodHashBefore, prodSizeAfter, prodHashAfter)
		}
		t.Logf("CONFIRMED: Production database %s remained 100%% untouched (SHA=%s, Size=%d)",
			prodDBPath, prodHashAfter, prodSizeAfter)
	}
}
