package backup

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/probewatch/probewatch/internal/db"
)

const (
	SettingKeyBackupConfig = "backup_scheduler_config"
	DefaultIntervalHours   = 24
	DefaultRetentionCount  = 7
)

// Config represents full backup and remote replication configuration.
type Config struct {
	Enabled        bool         `json:"enabled"`
	IntervalHours  int          `json:"interval_hours"`
	RetentionCount int          `json:"retention_count"`
	S3             S3Config     `json:"s3"`
	WebDAV         WebDAVConfig `json:"webdav"`
}

// Masked returns a safe copy of Config with secrets masked.
func (c Config) Masked() Config {
	m := c
	m.S3 = c.S3.Masked()
	m.WebDAV = c.WebDAV.Masked()
	return m
}

// RunReport documents the outcome of an automated or manual backup execution.
type RunReport struct {
	BackupInfo   db.BackupInfo `json:"backup_info"`
	S3Uploaded   bool          `json:"s3_uploaded"`
	S3Error      string        `json:"s3_error,omitempty"`
	WebDAVUploaded bool        `json:"webdav_uploaded"`
	WebDAVError  string        `json:"webdav_error,omitempty"`
	PrunedCount  int           `json:"pruned_count"`
	DurationMS   int64         `json:"duration_ms"`
}

// VerificationResult contains the detailed diagnostic report of a dry-run restore check.
type VerificationResult struct {
	Filename     string    `json:"filename"`
	SizeBytes    int64     `json:"size_bytes"`
	SHA256       string    `json:"sha256"`
	Integrity    string    `json:"integrity"`
	TablesCount  int       `json:"tables_count"`
	DurationMS   int64     `json:"duration_ms"`
	VerifiedAt   time.Time `json:"verified_at"`
}

// Scheduler handles periodic database snapshots, remote sync, and retention cleanup.
type Scheduler struct {
	store     *db.Store
	backupDir string

	mu        sync.RWMutex
	running   bool
	cancel    context.CancelFunc
}

// NewScheduler creates an initialized backup Scheduler.
func NewScheduler(store *db.Store, backupDir string) *Scheduler {
	return &Scheduler{
		store:     store,
		backupDir: backupDir,
	}
}

// SetStore dynamically updates the active store.
func (s *Scheduler) SetStore(newStore *db.Store) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store = newStore
}

// LoadConfig loads the backup configuration from system settings.
func (s *Scheduler) LoadConfig(ctx context.Context) (Config, error) {
	def := Config{
		Enabled:        false,
		IntervalHours:  DefaultIntervalHours,
		RetentionCount: DefaultRetentionCount,
		S3: S3Config{
			Region: "us-east-1",
		},
	}

	val, err := s.store.GetSetting(ctx, SettingKeyBackupConfig, "")
	if err != nil || val == "" {
		return def, nil
	}

	var cfg Config
	if err := json.Unmarshal([]byte(val), &cfg); err != nil {
		return def, nil
	}
	if cfg.IntervalHours <= 0 {
		cfg.IntervalHours = DefaultIntervalHours
	}
	if cfg.RetentionCount <= 0 {
		cfg.RetentionCount = DefaultRetentionCount
	}
	return cfg, nil
}

// SaveConfig persists the backup configuration to system settings.
func (s *Scheduler) SaveConfig(ctx context.Context, cfg Config) error {
	if cfg.IntervalHours <= 0 {
		cfg.IntervalHours = DefaultIntervalHours
	}
	if cfg.RetentionCount <= 0 {
		cfg.RetentionCount = DefaultRetentionCount
	}

	// Preserve secret keys if user left them masked
	existing, _ := s.LoadConfig(ctx)
	if strings.Contains(cfg.S3.SecretKey, "••••") {
		cfg.S3.SecretKey = existing.S3.SecretKey
	}
	if strings.Contains(cfg.WebDAV.Password, "••••") {
		cfg.WebDAV.Password = existing.WebDAV.Password
	}

	b, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return s.store.SetSetting(ctx, SettingKeyBackupConfig, string(b))
}

// Start launches the background scheduler loop.
func (s *Scheduler) Start(parentCtx context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parentCtx)
	s.cancel = cancel
	s.running = true
	s.mu.Unlock()

	go s.loop(ctx)
}

// Stop stops the scheduler.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	s.running = false
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *Scheduler) loop(ctx context.Context) {
	// Check every hour whether a scheduled backup is due
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cfg, err := s.LoadConfig(ctx)
			if err != nil || !cfg.Enabled {
				continue
			}

			// Check if latest backup is older than interval
			backups, err := db.ListBackups(s.backupDir)
			if err == nil {
				interval := time.Duration(cfg.IntervalHours) * time.Hour
				if len(backups) == 0 || time.Since(backups[0].CreatedAt) >= interval {
					_, _ = s.RunOnce(ctx, "scheduled")
				}
			}
		}
	}
}

// RunOnce performs an immediate backup, uploads to enabled cloud destinations, and prunes old backups.
func (s *Scheduler) RunOnce(ctx context.Context, prefix string) (RunReport, error) {
	start := time.Now()
	if prefix == "" {
		prefix = "probewatch-backup"
	}

	cfg, _ := s.LoadConfig(ctx)

	// 1. Create local consistent atomic backup
	info, err := s.store.CreateBackup(ctx, s.backupDir, prefix)
	if err != nil {
		return RunReport{}, fmt.Errorf("create hot backup: %w", err)
	}

	backupFilePath := filepath.Join(s.backupDir, info.Filename)
	backupBytes, err := os.ReadFile(backupFilePath)
	if err != nil {
		return RunReport{}, fmt.Errorf("read backup file: %w", err)
	}

	report := RunReport{
		BackupInfo: info,
	}

	// 2. Upload to S3 if configured & enabled
	if cfg.S3.Enabled && cfg.S3.Bucket != "" {
		s3Client := NewS3Client(cfg.S3)
		if err := s3Client.PutObject(ctx, info.Filename, backupBytes, "application/gzip"); err != nil {
			report.S3Error = err.Error()
		} else {
			report.S3Uploaded = true
		}
	}

	// 3. Upload to WebDAV if configured & enabled
	if cfg.WebDAV.Enabled && cfg.WebDAV.URL != "" {
		webdavClient := NewWebDAVClient(cfg.WebDAV)
		if err := webdavClient.PutFile(ctx, info.Filename, backupBytes); err != nil {
			report.WebDAVError = err.Error()
		} else {
			report.WebDAVUploaded = true
		}
	}

	// 4. Prune local backups exceeding retention count
	retention := cfg.RetentionCount
	if retention <= 0 {
		retention = DefaultRetentionCount
	}

	backups, err := db.ListBackups(s.backupDir)
	if err == nil && len(backups) > retention {
		for i := retention; i < len(backups); i++ {
			if err := db.DeleteBackup(s.backupDir, backups[i].Filename); err == nil {
				report.PrunedCount++
			}
		}
	}

	report.DurationMS = time.Since(start).Milliseconds()
	return report, nil
}

// VerifyBackup runs a dry-run verification on a candidate backup without touching active data.
func (s *Scheduler) VerifyBackup(ctx context.Context, filename string) (VerificationResult, error) {
	start := time.Now()
	if !db.IsValidBackupFilename(filename) {
		return VerificationResult{}, errors.New("invalid backup filename")
	}

	backupPath := filepath.Join(s.backupDir, filename)
	fi, err := os.Stat(backupPath)
	if err != nil {
		return VerificationResult{}, fmt.Errorf("stat backup file: %w", err)
	}

	// 1. Decompress into temporary sandbox candidate
	tempSandbox := filepath.Join(s.backupDir, fmt.Sprintf(".sandbox-verify-%d.db", time.Now().UnixNano()))
	defer os.Remove(tempSandbox)

	hasher := sha256.New()

	if strings.HasSuffix(filename, ".gz") {
		gzFile, err := os.Open(backupPath)
		if err != nil {
			return VerificationResult{}, fmt.Errorf("open backup file: %w", err)
		}
		defer gzFile.Close()

		gzReader, err := gzip.NewReader(io.TeeReader(gzFile, hasher))
		if err != nil {
			return VerificationResult{}, fmt.Errorf("create gzip reader: %w", err)
		}
		defer gzReader.Close()

		outFile, err := os.OpenFile(tempSandbox, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			return VerificationResult{}, err
		}
		if _, err := io.Copy(outFile, gzReader); err != nil {
			outFile.Close()
			return VerificationResult{}, fmt.Errorf("decompress sandbox: %w", err)
		}
		outFile.Close()
	} else {
		src, err := os.Open(backupPath)
		if err != nil {
			return VerificationResult{}, err
		}
		defer src.Close()

		outFile, err := os.OpenFile(tempSandbox, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			return VerificationResult{}, err
		}
		if _, err := io.Copy(outFile, io.TeeReader(src, hasher)); err != nil {
			outFile.Close()
			return VerificationResult{}, err
		}
		outFile.Close()
	}

	// 2. Open candidate database and perform integrity check
	dsn := tempSandbox + "?_pragma=busy_timeout(5000)"
	testDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return VerificationResult{}, fmt.Errorf("open sandbox database: %w", err)
	}
	defer testDB.Close()

	var checkResult string
	err = testDB.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&checkResult)
	if err != nil {
		return VerificationResult{}, fmt.Errorf("integrity check error: %w", err)
	}
	if checkResult != "ok" {
		return VerificationResult{}, fmt.Errorf("integrity check failed: %s", checkResult)
	}

	// 3. Count tables in candidate database
	var tableCount int
	_ = testDB.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table'").Scan(&tableCount)

	return VerificationResult{
		Filename:    filename,
		SizeBytes:   fi.Size(),
		SHA256:      hex.EncodeToString(hasher.Sum(nil)),
		Integrity:   checkResult,
		TablesCount: tableCount,
		DurationMS:  time.Since(start).Milliseconds(),
		VerifiedAt:  time.Now().UTC(),
	}, nil
}
