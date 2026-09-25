package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/backup"
	"github.com/probewatch/probewatch/internal/db"
)

func (s *Server) backupDir() string {
	dbPath := s.cfg.DatabasePath
	if dbPath == "" {
		dbPath = "data/probewatch.db"
	}
	return filepath.Join(filepath.Dir(dbPath), "backups")
}

func (s *Server) backupRoute(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(trimmed, "/")
	// Expected parts: ["api", "system", "backups", ...]
	if len(parts) == 4 && parts[3] == "upload" && r.Method == http.MethodPost {
		s.uploadBackup(w, r)
		return
	}
	if isWriteMethod(r.Method) {
		NewMiddleware(s.service, s.cfg).RequireCSRF(http.HandlerFunc(s.backupWriteAction)).ServeHTTP(w, r)
		return
	}
	s.backupReadAction(w, r)
}

func (s *Server) backupReadAction(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(trimmed, "/")
	// Expected parts: ["api", "system", "backups", ...]
	if len(parts) == 3 {
		s.listBackups(w, r)
		return
	}
	if len(parts) == 4 && parts[3] == "config" {
		s.getBackupConfig(w, r)
		return
	}
	if len(parts) == 5 && parts[4] == "download" {
		s.downloadBackup(w, r, parts[3])
		return
	}
	writeJSONError(w, http.StatusNotFound, "not found")
}

func (s *Server) backupWriteAction(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(trimmed, "/")
	// Expected parts: ["api", "system", "backups", ...]
	if len(parts) == 3 {
		if r.Method == http.MethodPost {
			s.createBackup(w, r)
			return
		}
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if len(parts) == 4 {
		switch parts[3] {
		case "config":
			if r.Method == http.MethodPost {
				s.saveBackupConfig(w, r)
				return
			}
		case "test-s3":
			if r.Method == http.MethodPost {
				s.testS3Backup(w, r)
				return
			}
		case "test-webdav":
			if r.Method == http.MethodPost {
				s.testWebDAVBackup(w, r)
				return
			}
		case "export-now":
			if r.Method == http.MethodPost {
				s.exportBackupNow(w, r)
				return
			}
		default:
			if r.Method == http.MethodDelete {
				s.deleteBackup(w, r, parts[3])
				return
			}
		}
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if len(parts) == 5 {
		if parts[4] == "restore" && r.Method == http.MethodPost {
			s.restoreBackup(w, r, parts[3])
			return
		}
		if parts[4] == "verify" && r.Method == http.MethodPost {
			s.verifyBackupHandler(w, r, parts[3])
			return
		}
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSONError(w, http.StatusNotFound, "not found")
}

func (s *Server) listBackups(w http.ResponseWriter, r *http.Request) {
	backups, err := db.ListBackups(s.backupDir())
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "backups unavailable")
		return
	}
	writeJSON(w, http.StatusOK, backups)
}

func (s *Server) createBackup(w http.ResponseWriter, r *http.Request) {
	info, err := s.service.Store().CreateBackup(r.Context(), s.backupDir(), "probewatch-backup")
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "backup creation failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, info)
}

func (s *Server) getBackupConfig(w http.ResponseWriter, r *http.Request) {
	if s.backupScheduler == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "backup scheduler not initialized")
		return
	}
	cfg, err := s.backupScheduler.LoadConfig(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to load backup config")
		return
	}
	writeJSON(w, http.StatusOK, cfg.Masked())
}

func (s *Server) saveBackupConfig(w http.ResponseWriter, r *http.Request) {
	if s.backupScheduler == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "backup scheduler not initialized")
		return
	}
	var req backup.Config
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json payload")
		return
	}
	if err := s.backupScheduler.SaveConfig(r.Context(), req); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to save backup config: "+err.Error())
		return
	}
	saved, _ := s.backupScheduler.LoadConfig(r.Context())
	writeJSON(w, http.StatusOK, saved.Masked())
}

func (s *Server) testS3Backup(w http.ResponseWriter, r *http.Request) {
	var s3Cfg backup.S3Config
	if err := json.NewDecoder(io.LimitReader(r.Body, 32*1024)).Decode(&s3Cfg); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json payload")
		return
	}
	// Preserve existing secret key if masked
	if s.backupScheduler != nil && strings.Contains(s3Cfg.SecretKey, "••••") {
		existing, _ := s.backupScheduler.LoadConfig(r.Context())
		s3Cfg.SecretKey = existing.S3.SecretKey
	}
	client := backup.NewS3Client(s3Cfg)
	if err := client.TestConnection(r.Context()); err != nil {
		writeJSONError(w, http.StatusBadRequest, "s3 test failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"message": "S3 / R2 对象存储连接测试成功，写入与删除校验通过！",
	})
}

func (s *Server) testWebDAVBackup(w http.ResponseWriter, r *http.Request) {
	var davCfg backup.WebDAVConfig
	if err := json.NewDecoder(io.LimitReader(r.Body, 32*1024)).Decode(&davCfg); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json payload")
		return
	}
	// Preserve existing password if masked
	if s.backupScheduler != nil && strings.Contains(davCfg.Password, "••••") {
		existing, _ := s.backupScheduler.LoadConfig(r.Context())
		davCfg.Password = existing.WebDAV.Password
	}
	client := backup.NewWebDAVClient(davCfg)
	if err := client.TestConnection(r.Context()); err != nil {
		writeJSONError(w, http.StatusBadRequest, "webdav test failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"message": "WebDAV 远程服务器连接测试成功，写入与删除校验通过！",
	})
}

func (s *Server) exportBackupNow(w http.ResponseWriter, r *http.Request) {
	if s.backupScheduler == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "backup scheduler not initialized")
		return
	}
	report, err := s.backupScheduler.RunOnce(r.Context(), "probewatch-export")
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "backup export failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) verifyBackupHandler(w http.ResponseWriter, r *http.Request, filename string) {
	if s.backupScheduler == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "backup scheduler not initialized")
		return
	}
	res, err := s.backupScheduler.VerifyBackup(r.Context(), filename)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "verification failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) downloadBackup(w http.ResponseWriter, r *http.Request, filename string) {
	if !db.IsValidBackupFilename(filename) {
		writeJSONError(w, http.StatusBadRequest, "invalid backup filename")
		return
	}
	filePath := filepath.Join(s.backupDir(), filename)
	if _, err := os.Stat(filePath); err != nil {
		writeJSONError(w, http.StatusNotFound, "backup file not found")
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	if strings.HasSuffix(filename, ".gz") {
		w.Header().Set("Content-Type", "application/gzip")
	} else {
		w.Header().Set("Content-Type", "application/x-sqlite3")
	}
	http.ServeFile(w, r, filePath)
}

func (s *Server) restoreBackup(w http.ResponseWriter, r *http.Request, filename string) {
	if !db.IsValidBackupFilename(filename) {
		writeJSONError(w, http.StatusBadRequest, "invalid backup filename")
		return
	}
	newStore, err := s.service.Store().RestoreBackup(r.Context(), s.backupDir(), filename)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "restore failed: "+err.Error())
		return
	}
	oldStore := s.service.SwapStore(newStore)
	if oldStore != nil {
		_ = oldStore.Close()
	}
	if s.backupScheduler != nil {
		s.backupScheduler.SetStore(newStore)
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":   "ok",
		"message":  "database restored successfully",
		"filename": filename,
	})
}

func (s *Server) deleteBackup(w http.ResponseWriter, r *http.Request, filename string) {
	if !db.IsValidBackupFilename(filename) {
		writeJSONError(w, http.StatusBadRequest, "invalid backup filename")
		return
	}
	if err := db.DeleteBackup(s.backupDir(), filename); err != nil {
		writeJSONError(w, http.StatusNotFound, "delete failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":   "ok",
		"message":  "backup deleted",
		"filename": filename,
	})
}

func (s *Server) uploadBackup(w http.ResponseWriter, r *http.Request) {
	const maxUploadBytes = 512 << 20 // 512 MB
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeJSONError(w, http.StatusBadRequest, "failed to parse multipart form or file too large")
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	file, header, err := r.FormFile("backup_file")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "missing backup_file field in form")
		return
	}
	defer file.Close()

	originalName := header.Filename
	if !db.IsValidBackupFilename(originalName) {
		writeJSONError(w, http.StatusBadRequest, "invalid filename: must end with .db or .db.gz and contain only safe characters")
		return
	}

	backupDir := s.backupDir()
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "cannot create backup directory")
		return
	}

	timestamp := time.Now().UTC().Format("20060102-150405")
	var finalName string
	if strings.HasSuffix(originalName, ".db.gz") {
		base := strings.TrimSuffix(originalName, ".db.gz")
		finalName = fmt.Sprintf("%s-upload-%s.db.gz", base, timestamp)
	} else if strings.HasSuffix(originalName, ".db") {
		base := strings.TrimSuffix(originalName, ".db")
		finalName = fmt.Sprintf("%s-upload-%s.db", base, timestamp)
	} else {
		finalName = fmt.Sprintf("uploaded-%s-%s", timestamp, originalName)
	}

	if !db.IsValidBackupFilename(finalName) {
		writeJSONError(w, http.StatusBadRequest, "generated filename invalid")
		return
	}

	destPath := filepath.Join(backupDir, finalName)
	dst, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to create destination file")
		return
	}
	defer dst.Close()

	hasher := sha256.New()
	mw := io.MultiWriter(dst, hasher)
	written, err := io.Copy(mw, file)
	if err != nil {
		_ = os.Remove(destPath)
		writeJSONError(w, http.StatusInternalServerError, "failed to save uploaded file: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, db.BackupInfo{
		Filename:     finalName,
		SizeBytes:    written,
		CreatedAt:    time.Now().UTC(),
		SHA256:       hex.EncodeToString(hasher.Sum(nil)),
		IsCompressed: strings.HasSuffix(finalName, ".gz"),
	})
}
