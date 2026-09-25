package db

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBackupAndRestoreHotDatabase(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	backupDir := filepath.Join(tempDir, "backups")
	pepper := []byte("test-pepper-12345678901234567890")

	store, err := OpenStore(dbPath, pepper)
	if err != nil {
		t.Fatalf("OpenStore failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Insert test data
	admin, err := store.UpsertAdminUser(ctx, "github", "user-1", "testuser", time.Now().UTC())
	if err != nil {
		t.Fatalf("UpsertAdminUser failed: %v", err)
	}

	// Create hot backup
	backup, err := store.CreateBackup(ctx, backupDir, "test-backup")
	if err != nil {
		t.Fatalf("CreateBackup failed: %v", err)
	}
	if backup.SizeBytes <= 0 || backup.SHA256 == "" || !backup.IsCompressed {
		t.Fatalf("unexpected backup info: %+v", backup)
	}

	// List backups
	list, err := ListBackups(backupDir)
	if err != nil {
		t.Fatalf("ListBackups failed: %v", err)
	}
	if len(list) != 1 || list[0].Filename != backup.Filename {
		t.Fatalf("unexpected list: %+v", list)
	}

	// Modify database after backup
	admin2, err := store.UpsertAdminUser(ctx, "github", "user-2", "seconduser", time.Now().UTC())
	if err != nil {
		t.Fatalf("UpsertAdminUser 2 failed: %v", err)
	}

	// Restore from backup
	restoredStore, err := store.RestoreBackup(ctx, backupDir, backup.Filename)
	if err != nil {
		t.Fatalf("RestoreBackup failed: %v", err)
	}
	defer restoredStore.Close()

	// Verify restored state: user-1 exists, user-2 does NOT exist
	gotAdmin, err := restoredStore.GetAdminUser(ctx, admin.ID)
	if err != nil || gotAdmin.Login != "testuser" {
		t.Fatalf("restored admin mismatch: got %v, err: %v", gotAdmin, err)
	}
	_, err = restoredStore.GetAdminUser(ctx, admin2.ID)
	if err == nil {
		t.Fatal("user-2 should not exist in restored database")
	}

	// Delete backup
	if err := DeleteBackup(backupDir, backup.Filename); err != nil {
		t.Fatalf("DeleteBackup failed: %v", err)
	}
	remaining, err := ListBackups(backupDir)
	if err != nil {
		t.Fatalf("ListBackups after delete failed: %v", err)
	}
	for _, b := range remaining {
		if b.Filename == backup.Filename {
			t.Fatalf("backup %s was not deleted", backup.Filename)
		}
	}
}

func TestBackupRejectsInvalidFilenames(t *testing.T) {
	for _, name := range []string{
		"",
		"../test.db",
		"/etc/passwd",
		"test.txt",
		"test..db",
		"nested/test.db.gz",
	} {
		if IsValidBackupFilename(name) {
			t.Errorf("expected IsValidBackupFilename(%q) = false", name)
		}
	}
}

func TestRestoreCorruptedFileFailsIntegrityCheck(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	backupDir := filepath.Join(tempDir, "backups")
	pepper := []byte("test-pepper-12345678901234567890")

	store, err := OpenStore(dbPath, pepper)
	if err != nil {
		t.Fatalf("OpenStore failed: %v", err)
	}
	defer store.Close()

	// Write a bogus corrupted file
	corruptName := "probewatch-corrupt.db"
	_ = os.MkdirAll(backupDir, 0700)
	_ = os.WriteFile(filepath.Join(backupDir, corruptName), []byte("this is not a sqlite db file!"), 0600)

	_, err = store.RestoreBackup(context.Background(), backupDir, corruptName)
	if err == nil {
		t.Fatal("expected restore of corrupt file to fail integrity check")
	}
}
