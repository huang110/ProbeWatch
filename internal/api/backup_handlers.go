package api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	if len(parts) == 5 && parts[4] == "restore" {
		if r.Method == http.MethodPost {
			s.restoreBackup(w, r, parts[3])
			return
		}
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if len(parts) == 4 && r.Method == http.MethodDelete {
		s.deleteBackup(w, r, parts[3])
		return
	}
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

func (s *Server) downloadBackup(w http.ResponseWriter, r *http.Request, filename string) {
	if !db.IsValidBackupFilename(filename) {
		writeJSONError(w, http.StatusBadRequest, "invalid backup filename")
		return
	}
	filePath := filepath.Join(s.backupDir(), filename)
	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSONError(w, http.StatusNotFound, "backup not found")
			return
		}
		writeJSONError(w, http.StatusServiceUnavailable, "backup read failed")
		return
	}
	defer file.Close()

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	if strings.HasSuffix(filename, ".gz") {
		w.Header().Set("Content-Type", "application/gzip")
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	_, _ = io.Copy(w, file)
}

func (s *Server) deleteBackup(w http.ResponseWriter, r *http.Request, filename string) {
	if !db.IsValidBackupFilename(filename) {
		writeJSONError(w, http.StatusBadRequest, "invalid backup filename")
		return
	}
	if err := db.DeleteBackup(s.backupDir(), filename); err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "backup deletion failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
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
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "database restored successfully",
	})
}

func (s *Server) uploadBackup(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r, s.cfg.PublicBaseURL) {
		writeJSONError(w, http.StatusForbidden, "same-origin request required")
		return
	}
	unlock := s.service.LockSessionCSRF(r)
	defer unlock()
	nextToken, err := s.service.ClaimCSRFWithError(r, r.Header.Get("X-CSRF-Token"))
	if err != nil {
		writeJSONError(w, http.StatusForbidden, "csrf validation failed")
		return
	}
	w.Header().Set("X-CSRF-Token", nextToken)

	// Limit upload size to 256MB
	r.Body = http.MaxBytesReader(w, r.Body, 256<<20)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid multipart form: "+err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "file field required")
		return
	}
	defer file.Close()

	rawName := filepath.Base(header.Filename)
	if !strings.HasSuffix(rawName, ".db") && !strings.HasSuffix(rawName, ".db.gz") {
		writeJSONError(w, http.StatusBadRequest, "only .db and .db.gz backup files are allowed")
		return
	}

	timestamp := time.Now().UTC().Format("20060102-150405")
	ext := ".db.gz"
	if strings.HasSuffix(rawName, ".db") && !strings.HasSuffix(rawName, ".db.gz") {
		ext = ".db"
	}
	savedName := fmt.Sprintf("probewatch-upload-%s%s", timestamp, ext)
	savedPath := filepath.Join(s.backupDir(), savedName)

	if err := os.MkdirAll(s.backupDir(), 0700); err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "failed to create backup directory")
		return
	}

	dest, err := os.OpenFile(savedPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "failed to save uploaded file")
		return
	}
	defer dest.Close()

	hasher := sha256.New()
	mw := io.MultiWriter(dest, hasher)
	written, err := io.Copy(mw, file)
	if err != nil {
		_ = os.Remove(savedPath)
		writeJSONError(w, http.StatusServiceUnavailable, "failed to write file")
		return
	}

	writeJSON(w, http.StatusCreated, db.BackupInfo{
		Filename:     savedName,
		SizeBytes:    written,
		CreatedAt:    time.Now().UTC(),
		SHA256:       hex.EncodeToString(hasher.Sum(nil)),
		IsCompressed: strings.HasSuffix(savedName, ".gz"),
	})
}
