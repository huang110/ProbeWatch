package backup

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/db"
)

func TestS3ClientPutAndDelete(t *testing.T) {
	var receivedAuth string
	var receivedDate string
	var receivedPayloadHash string
	var receivedBody string

	mockS3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		receivedDate = r.Header.Get("x-amz-date")
		receivedPayloadHash = r.Header.Get("x-amz-content-sha256")

		if r.Method == http.MethodPut {
			buf := make([]byte, 1024)
			n, _ := r.Body.Read(buf)
			receivedBody = string(buf[:n])
			w.WriteHeader(http.StatusOK)
		} else if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer mockS3.Close()

	cfg := S3Config{
		Enabled:        true,
		Endpoint:       mockS3.URL,
		Region:         "us-east-1",
		Bucket:         "test-bucket",
		Prefix:         "backups",
		AccessKey:      "AKIAIOSFODNN7EXAMPLE",
		SecretKey:      "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		ForcePathStyle: true,
	}

	client := NewS3Client(cfg)
	client.httpClient = &http.Client{Timeout: 5 * time.Second, Transport: http.DefaultTransport}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1. PutObject
	data := []byte("test-backup-payload-content")
	if err := client.PutObject(ctx, "test.db.gz", data, "application/gzip"); err != nil {
		t.Fatalf("put object failed: %v", err)
	}

	if !strings.HasPrefix(receivedAuth, "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/") {
		t.Errorf("invalid authorization header: %s", receivedAuth)
	}
	if receivedDate == "" {
		t.Errorf("missing x-amz-date header")
	}
	if receivedPayloadHash == "" {
		t.Errorf("missing x-amz-content-sha256 header")
	}
	if receivedBody != string(data) {
		t.Errorf("payload mismatch, got: %s", receivedBody)
	}

	// 2. DeleteObject
	if err := client.DeleteObject(ctx, "test.db.gz"); err != nil {
		t.Fatalf("delete object failed: %v", err)
	}
}

func TestBackupObjectValidationRejectsUnsafeAndOversizedNames(t *testing.T) {
	if err := validateBackupObjectName(" backup.db"); err == nil {
		t.Fatal("leading whitespace in backup filename must be rejected")
	}
	if err := validateBackupObjectKey("safe/prefix/backup.db"); err != nil {
		t.Fatalf("valid object key rejected: %v", err)
	}
	if err := validateBackupObjectKey("safe/../backup.db"); err == nil {
		t.Fatal("object key traversal must be rejected")
	}
	if err := validateBackupObjectKey(strings.Repeat("a", maxBackupPathLength+1)); err == nil {
		t.Fatal("oversized object key must be rejected")
	}
}

func TestWebDAVClientPutAndDelete(t *testing.T) {
	var receivedAuth string
	var receivedMethod string

	mockDAV := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		receivedMethod = r.Method
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusCreated)
		} else if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer mockDAV.Close()

	cfg := WebDAVConfig{
		Enabled:  true,
		URL:      mockDAV.URL + "/remote.php/webdav/backups",
		Username: "admin",
		Password: "password123",
	}

	client := NewWebDAVClient(cfg)
	client.httpClient = &http.Client{Timeout: 5 * time.Second, Transport: http.DefaultTransport}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.PutFile(ctx, "snap.db.gz", []byte("snap-data")); err != nil {
		t.Fatalf("put file: %v", err)
	}
	if !strings.HasPrefix(receivedAuth, "Basic ") {
		t.Errorf("expected Basic auth header, got: %s", receivedAuth)
	}
	if receivedMethod != http.MethodPut {
		t.Errorf("expected PUT method, got: %s", receivedMethod)
	}

	if err := client.DeleteFile(ctx, "snap.db.gz"); err != nil {
		t.Fatalf("delete file: %v", err)
	}
}

func TestFileUploadMethodsStreamWithContentLength(t *testing.T) {
	payload := strings.Repeat("backup-payload-", 1024)
	filePath := filepath.Join(t.TempDir(), "snapshot.db.gz")
	if err := os.WriteFile(filePath, []byte(payload), 0600); err != nil {
		t.Fatal(err)
	}

	t.Run("s3", func(t *testing.T) {
		var got int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = r.ContentLength
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != payload {
				t.Errorf("payload mismatch: got %d bytes", len(body))
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewS3Client(S3Config{
			Endpoint:  server.URL,
			Region:    "us-east-1",
			Bucket:    "test-bucket",
			AccessKey: "access",
			SecretKey: "secret",
		})
		client.httpClient = &http.Client{Timeout: 5 * time.Second, Transport: http.DefaultTransport}
		if err := client.PutObjectFile(context.Background(), "snapshot.db.gz", filePath, "application/gzip"); err != nil {
			t.Fatal(err)
		}
		if got != int64(len(payload)) {
			t.Fatalf("content length = %d, want %d", got, len(payload))
		}
	})

	t.Run("webdav", func(t *testing.T) {
		var got int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = r.ContentLength
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != payload {
				t.Errorf("payload mismatch: got %d bytes", len(body))
			}
			w.WriteHeader(http.StatusCreated)
		}))
		defer server.Close()

		client := NewWebDAVClient(WebDAVConfig{URL: server.URL})
		client.httpClient = &http.Client{Timeout: 5 * time.Second, Transport: http.DefaultTransport}
		if err := client.PutFilePath(context.Background(), "snapshot.db.gz", filePath); err != nil {
			t.Fatal(err)
		}
		if got != int64(len(payload)) {
			t.Fatalf("content length = %d, want %d", got, len(payload))
		}
	})
}

func TestSchedulerConfigMasking(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := db.OpenStore(filepath.Join(tmpDir, "test.db"), []byte("12345678901234567890123456789012"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	s := NewScheduler(store, tmpDir)
	ctx := context.Background()

	cfg := Config{
		Enabled:        true,
		IntervalHours:  12,
		RetentionCount: 5,
		S3: S3Config{
			Enabled:   true,
			Bucket:    "my-bucket",
			AccessKey: "my-key",
			SecretKey: "super-secret-password-123",
		},
		WebDAV: WebDAVConfig{
			Enabled:  true,
			URL:      "https://dav.example.com",
			Username: "user",
			Password: "dav-password",
		},
	}

	if err := s.SaveConfig(ctx, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	loaded, err := s.LoadConfig(ctx)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if loaded.IntervalHours != 12 || loaded.RetentionCount != 5 {
		t.Fatalf("unexpected loaded config: %+v", loaded)
	}
	if loaded.S3.SecretKey != "super-secret-password-123" {
		t.Fatalf("secret key mismatch: %s", loaded.S3.SecretKey)
	}

	masked := loaded.Masked()
	if !strings.Contains(masked.S3.SecretKey, "••••") {
		t.Fatalf("expected masked secret key, got: %s", masked.S3.SecretKey)
	}
	if !strings.Contains(masked.WebDAV.Password, "••••") {
		t.Fatalf("expected masked webdav password, got: %s", masked.WebDAV.Password)
	}
}

func TestSchedulerRunOnceAndRetention(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := db.OpenStore(filepath.Join(tmpDir, "test.db"), []byte("12345678901234567890123456789012"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	backupDir := filepath.Join(tmpDir, "backups")
	_ = os.MkdirAll(backupDir, 0700)

	s := NewScheduler(store, backupDir)
	ctx := context.Background()

	// Configure retention to 2
	_ = s.SaveConfig(ctx, Config{
		Enabled:        true,
		IntervalHours:  24,
		RetentionCount: 2,
	})

	// Run backup 3 times with unique prefix so filenames don't collide within the same second
	for i := 0; i < 3; i++ {
		time.Sleep(10 * time.Millisecond)
		report, err := s.RunOnce(ctx, fmt.Sprintf("test-backup-%d", i))
		if err != nil {
			t.Fatalf("run once %d failed: %v", i, err)
		}
		if report.BackupInfo.Filename == "" {
			t.Fatalf("expected filename in report")
		}
	}

	// Verify only 2 backups retained
	list, err := db.ListBackups(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 backups retained, got %d", len(list))
	}

	// Test dry-run verification on the latest backup
	res, err := s.VerifyBackup(ctx, list[0].Filename)
	if err != nil {
		t.Fatalf("verify backup failed: %v", err)
	}
	if res.Integrity != "ok" {
		t.Fatalf("expected integrity ok, got: %s", res.Integrity)
	}
	if res.TablesCount <= 0 {
		t.Fatalf("expected positive table count, got: %d", res.TablesCount)
	}
	if res.SHA256 == "" {
		t.Fatalf("expected non-empty sha256")
	}
}
