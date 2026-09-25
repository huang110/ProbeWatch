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
	if prefix == "" {
		prefix = "probewatch-backup"
	}
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return BackupInfo{}, fmt.Errorf("create backup directory: %w", err)
	}

	timestamp := time.Now().UTC().Format("20060102-150405")
	tempRaw := filepath.Join(backupDir, fmt.Sprintf(".tmp-%s-%s-%d.db", prefix, timestamp, time.Now().UnixNano()%10000))
	finalName := fmt.Sprintf("%s-%s.db.gz", prefix, timestamp)
	finalPath := filepath.Join(backupDir, finalName)

	_ = os.Remove(tempRaw)
	_ = os.Remove(finalPath)

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

	destFile, err := os.OpenFile(finalPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return BackupInfo{}, fmt.Errorf("create compressed backup file: %w", err)
	}
	defer destFile.Close()

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

	// 1. Take a pre-restore safety snapshot first!
	_, _ = s.CreateBackup(ctx, backupDir, "probewatch-pre-restore")

	// 2. Prepare uncompressed temporary restore target
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
		if _, err := io.Copy(outFile, gzReader); err != nil {
			outFile.Close()
			return nil, fmt.Errorf("decompress backup: %w", err)
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
		if _, err := io.Copy(dst, src); err != nil {
			dst.Close()
			return nil, err
		}
		dst.Close()
	}

	// 3. Verify SQLite integrity of the restored candidate BEFORE touching active database
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

	// 4. Close current active connection
	_ = s.db.Close()

	// 5. Overwrite the database file
	if err := os.Rename(tempRestored, targetPath); err != nil {
		if err := copyFile(tempRestored, targetPath); err != nil {
			return nil, fmt.Errorf("replace database file: %w", err)
		}
	}
	_ = os.Chmod(targetPath, 0600)

	// Clean up stale WAL and SHM journal files
	_ = os.Remove(targetPath + "-wal")
	_ = os.Remove(targetPath + "-shm")

	// 6. Re-open and verify migrations
	newStore, err := OpenStore(targetPath, pepper)
	if err != nil {
		return nil, fmt.Errorf("reopen restored store: %w", err)
	}

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
