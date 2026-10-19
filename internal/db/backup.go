package db

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type BackupInfo struct {
	Filename     string    `json:"filename"`
	SizeBytes    int64     `json:"size_bytes"`
	CreatedAt    time.Time `json:"created_at"`
	SHA256       string    `json:"sha256,omitempty"`
	IsCompressed bool      `json:"is_compressed"`
}

// maxExpandedBackupBytes bounds both compressed-backup expansion and direct
// SQLite restores. It prevents a small gzip bomb from exhausting a low-disk
// VPS before SQLite integrity checks can reject it.
const MaxExpandedBackupBytes int64 = 2 << 30

// IsValidBackupFilename enforces strict filename validation to prevent path traversal.
func IsValidBackupFilename(filename string) bool {
	if len(filename) == 0 || len(filename) > 128 {
		return false
	}
	if !strings.HasSuffix(filename, ".db") && !strings.HasSuffix(filename, ".db.gz") {
		return false
	}
	for _, ch := range filename {
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' || ch == '.') {
			return false
		}
	}
	if strings.Contains(filename, "..") || strings.ContainsAny(filename, `/\`) {
		return false
	}
	return true
}

// CreateBackup performs an atomic, consistent hot backup of the SQLite database
// using VACUUM INTO and compresses it with gzip into backupDir.
func (s *Store) CreateBackup(ctx context.Context, backupDir string, prefix string) (BackupInfo, error) {
	s.backupMu.Lock()
	defer s.backupMu.Unlock()
	return s.createBackup(ctx, backupDir, prefix)
}

// createBackup is the unlocked implementation used by RestoreBackup while
// it already holds backupMu. Keeping the lock at Store scope also protects
// callers that bypass the backup scheduler (for example the manual API).
func (s *Store) createBackup(ctx context.Context, backupDir string, prefix string) (BackupInfo, error) {
	if prefix == "" {
		prefix = "probewatch-backup"
	}
	if !isValidBackupPrefix(prefix) {
		return BackupInfo{}, errors.New("invalid backup prefix")
	}
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return BackupInfo{}, fmt.Errorf("create backup directory: %w", err)
	}

	timestamp := time.Now().UTC().Format("20060102-150405")
	tempRaw := filepath.Join(backupDir, fmt.Sprintf(".tmp-%s-%s-%d.db", prefix, timestamp, time.Now().UnixNano()%10000))
	finalName := fmt.Sprintf("%s-%s.db.gz", prefix, timestamp)

	_ = os.Remove(tempRaw)

	// Hot backup via parameterized VACUUM INTO
	_, err := s.db.ExecContext(ctx, "VACUUM INTO ?", tempRaw)
	if err != nil {
		_ = os.Remove(tempRaw)
		return BackupInfo{}, fmt.Errorf("sqlite vacuum into: %w", err)
	}
	defer os.Remove(tempRaw)

	// Compress with gzip
	srcFile, err := os.Open(tempRaw)
	if err != nil {
		return BackupInfo{}, fmt.Errorf("open raw backup: %w", err)
	}
	defer srcFile.Close()

	// O_EXCL prevents two Store instances sharing the same database directory
	// from silently replacing a snapshot generated in the same second.
	var destFile *os.File
	for suffix := 0; ; suffix++ {
		if suffix == 0 {
			finalName = fmt.Sprintf("%s-%s.db.gz", prefix, timestamp)
		} else {
			finalName = fmt.Sprintf("%s-%s-%d.db.gz", prefix, timestamp, suffix)
		}
		finalPath := filepath.Join(backupDir, finalName)
		destFile, err = os.OpenFile(finalPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return BackupInfo{}, fmt.Errorf("create compressed backup file: %w", err)
		}
		break
	}
	defer destFile.Close()
	finalPath := destFile.Name()

	hasher := sha256.New()
	mw := io.MultiWriter(destFile, hasher)
	gzWriter := gzip.NewWriter(mw)

	if _, err := io.Copy(gzWriter, srcFile); err != nil {
		_ = gzWriter.Close()
		_ = os.Remove(finalPath)
		return BackupInfo{}, fmt.Errorf("compress backup: %w", err)
	}
	if err := gzWriter.Close(); err != nil {
		_ = os.Remove(finalPath)
		return BackupInfo{}, fmt.Errorf("close gzip writer: %w", err)
	}

	fi, err := destFile.Stat()
	if err != nil {
		return BackupInfo{}, fmt.Errorf("stat backup: %w", err)
	}

	return BackupInfo{
		Filename:     finalName,
		SizeBytes:    fi.Size(),
		CreatedAt:    fi.ModTime().UTC(),
		SHA256:       hex.EncodeToString(hasher.Sum(nil)),
		IsCompressed: true,
	}, nil
}

func isValidBackupPrefix(prefix string) bool {
	if len(prefix) == 0 || len(prefix) > 64 {
		return false
	}
	for _, ch := range prefix {
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_') {
			return false
		}
	}
	return true
}

// ListBackups enumerates all backup files in backupDir sorted by creation time descending.
func ListBackups(backupDir string) ([]BackupInfo, error) {
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return nil, fmt.Errorf("create backup directory: %w", err)
	}
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return nil, fmt.Errorf("read backup directory: %w", err)
	}
	backups := make([]BackupInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		name := entry.Name()
		if !IsValidBackupFilename(name) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		backups = append(backups, BackupInfo{
			Filename:     name,
			SizeBytes:    info.Size(),
			CreatedAt:    info.ModTime().UTC(),
			IsCompressed: strings.HasSuffix(name, ".gz"),
		})
	}
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt.After(backups[j].CreatedAt)
	})
	return backups, nil
}

// DeleteBackup safely deletes a backup file from backupDir.
func DeleteBackup(backupDir string, filename string) error {
	if !IsValidBackupFilename(filename) {
		return errors.New("invalid backup filename")
	}
	path := filepath.Join(backupDir, filename)
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return errors.New("backup file not found")
		}
		return err
	}
	return nil
}

// RestoreBackup verifies and restores the database from a backup file,
// returning an initialized and migrated Store ready for immediate use.
func (s *Store) RestoreBackup(ctx context.Context, backupDir string, filename string) (*Store, error) {
	s.backupMu.Lock()
	defer s.backupMu.Unlock()

	if !IsValidBackupFilename(filename) {
		return nil, errors.New("invalid backup filename")
	}
	backupPath := filepath.Join(backupDir, filename)
	if _, err := os.Stat(backupPath); err != nil {
		return nil, fmt.Errorf("backup file not found: %w", err)
	}

	targetPath := s.DatabasePath()
	pepper := s.Pepper()
	if targetPath == "" {
		return nil, errors.New("database path unknown")
	}

	// 1. Prepare uncompressed temporary restore target
	tempRestored := filepath.Join(backupDir, fmt.Sprintf(".restore-tmp-%d.db", time.Now().UnixNano()))
	defer os.Remove(tempRestored)

	if strings.HasSuffix(filename, ".gz") {
		gzFile, err := os.Open(backupPath)
		if err != nil {
			return nil, fmt.Errorf("open compressed backup: %w", err)
		}
		defer gzFile.Close()

		gzReader, err := gzip.NewReader(gzFile)
		if err != nil {
			return nil, fmt.Errorf("create gzip reader: %w", err)
		}
		defer gzReader.Close()

		outFile, err := os.OpenFile(tempRestored, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			return nil, fmt.Errorf("create temp restore file: %w", err)
		}
		written, err := io.Copy(outFile, io.LimitReader(gzReader, MaxExpandedBackupBytes+1))
		if err != nil {
			outFile.Close()
			return nil, fmt.Errorf("decompress backup: %w", err)
		}
		if written > MaxExpandedBackupBytes {
			outFile.Close()
			return nil, fmt.Errorf("decompressed backup exceeds %d bytes", MaxExpandedBackupBytes)
		}
		outFile.Close()
	} else {
		src, err := os.Open(backupPath)
		if err != nil {
			return nil, err
		}
		defer src.Close()

		dst, err := os.OpenFile(tempRestored, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			return nil, err
		}
		written, err := io.Copy(dst, io.LimitReader(src, MaxExpandedBackupBytes+1))
		if err != nil {
			dst.Close()
			return nil, err
		}
		if written > MaxExpandedBackupBytes {
			dst.Close()
			return nil, fmt.Errorf("backup exceeds %d bytes", MaxExpandedBackupBytes)
		}
		dst.Close()
	}

	// 2. Verify SQLite integrity of the restored candidate BEFORE touching active database
	testDB, err := sql.Open("sqlite", sqliteDSN(tempRestored))
	if err != nil {
		return nil, fmt.Errorf("verify restored database: %w", err)
	}
	var checkResult string
	err = testDB.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&checkResult)
	testDB.Close()
	if err != nil || checkResult != "ok" {
		return nil, fmt.Errorf("restored database integrity check failed: %s (%v)", checkResult, err)
	}

	// 3. Take a pre-restore safety snapshot and fail closed if it cannot be
	// created. Never replace the active database without a rollback snapshot.
	if _, err := s.createBackup(ctx, backupDir, "probewatch-pre-restore"); err != nil {
		return nil, fmt.Errorf("create pre-restore backup: %w", err)
	}

	// 4. Checkpoint and close the active connection. Keep the original file in
	// place until the candidate has been opened successfully so a failed
	// replace/migration cannot leave the service pointing at a closed Store.
	if _, err := s.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return nil, fmt.Errorf("checkpoint active database: %w", err)
	}
	if err := s.db.Close(); err != nil {
		return nil, fmt.Errorf("close active database: %w", err)
	}
	reopenActive := func() error {
		recovered, openErr := OpenStore(targetPath, pepper)
		if openErr != nil {
			return openErr
		}
		s.db = recovered.db
		recovered.db = nil // ownership transferred to the original Store
		return nil
	}

	oldPath := filepath.Join(filepath.Dir(targetPath), fmt.Sprintf(".restore-old-%d.db", time.Now().UnixNano()))
	_ = os.Remove(oldPath)
	if err := os.Rename(targetPath, oldPath); err != nil {
		_ = reopenActive()
		return nil, fmt.Errorf("stage active database for restore: %w", err)
	}
	_ = os.Remove(targetPath + "-wal")
	_ = os.Remove(targetPath + "-shm")

	rollback := func() error {
		_ = os.Remove(targetPath)
		_ = os.Remove(targetPath + "-wal")
		_ = os.Remove(targetPath + "-shm")
		if err := os.Rename(oldPath, targetPath); err != nil {
			return err
		}
		return reopenActive()
	}

	// 5. Install the candidate only after the old database has been staged.
	if err := os.Rename(tempRestored, targetPath); err != nil {
		if rollbackErr := rollback(); rollbackErr != nil {
			return nil, fmt.Errorf("replace database file: %w (rollback failed: %v)", err, rollbackErr)
		}
		return nil, fmt.Errorf("replace database file: %w", err)
	}
	_ = os.Chmod(targetPath, 0600)

	// 6. Re-open and verify migrations. If opening fails, restore the original
	// file and reopen it in-place before returning the error.
	newStore, err := OpenStore(targetPath, pepper)
	if err != nil {
		if rollbackErr := rollback(); rollbackErr != nil {
			return nil, fmt.Errorf("reopen restored store: %w (rollback failed: %v)", err, rollbackErr)
		}
		return nil, fmt.Errorf("reopen restored store: %w", err)
	}
	_ = os.Remove(oldPath)
	_ = os.Remove(oldPath + "-wal")
	_ = os.Remove(oldPath + "-shm")

	return newStore, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
