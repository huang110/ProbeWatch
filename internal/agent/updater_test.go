package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/version"
)

func TestCheckUpdate(t *testing.T) {
	ctx := context.Background()

	// 1. Mock server offering a newer version
	newerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/update/check" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"server_version":       "0.9.0",
			"latest_agent_version": "0.9.0",
			"download_url":         "/update/download",
			"release_notes":        "v0.9.0 test release",
		})
	}))
	defer newerServer.Close()

	res, err := CheckUpdate(ctx, newerServer.Client(), newerServer.URL, "test-token")
	if err != nil {
		t.Fatalf("CheckUpdate: %v", err)
	}
	if !res.UpdateAvailable {
		t.Fatalf("expected update available for 0.9.0 (current %s)", version.AgentVersion)
	}
	if res.LatestAgentVersion != "0.9.0" {
		t.Fatalf("got latest version %s, want 0.9.0", res.LatestAgentVersion)
	}

	// 2. Mock server offering current version -> no update available
	currentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"server_version":       version.ServerVersion,
			"latest_agent_version": version.AgentVersion,
		})
	}))
	defer currentServer.Close()

	res2, err := CheckUpdate(ctx, currentServer.Client(), currentServer.URL, "")
	if err != nil {
		t.Fatalf("CheckUpdate current: %v", err)
	}
	if res2.UpdateAvailable {
		t.Fatal("expected no update available when versions match")
	}
}

func TestDownloadAndApplyUpdateValidations(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()

	// 1. Reject if no update available
	err := DownloadAndApplyUpdate(ctx, nil, "http://localhost", "", &UpdateCheckResult{UpdateAvailable: false}, dataDir)
	if err != ErrNoUpdateAvailable {
		t.Fatalf("expected ErrNoUpdateAvailable, got %v", err)
	}

	// 2. Small binary rejection (< 1MB)
	smallServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("too small binary content"))
	}))
	defer smallServer.Close()

	errSmall := DownloadAndApplyUpdate(ctx, smallServer.Client(), smallServer.URL, "", &UpdateCheckResult{
		UpdateAvailable:    true,
		DownloadURL:        smallServer.URL,
		LatestAgentVersion: "9.9.9",
	}, dataDir)
	if errSmall != ErrBinaryTooSmall {
		t.Fatalf("expected ErrBinaryTooSmall, got %v", errSmall)
	}

	// 3. Checksum mismatch rejection
	largeContent := make([]byte, 1024*1024+10)
	copy(largeContent, []byte("large valid binary simulation"))
	largeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(largeContent)
	}))
	defer largeServer.Close()

	errMismatch := DownloadAndApplyUpdate(ctx, largeServer.Client(), largeServer.URL, "", &UpdateCheckResult{
		UpdateAvailable:    true,
		DownloadURL:        largeServer.URL,
		LatestAgentVersion: "9.9.9",
		ChecksumSHA256:     "0000000000000000000000000000000000000000000000000000000000000000",
	}, dataDir)
	if errMismatch == nil || !errors.Is(errMismatch, ErrChecksumMismatch) {
		t.Fatalf("expected ErrChecksumMismatch, got %v", errMismatch)
	}
}

func TestVerifyAndClearPendingUpgrade(t *testing.T) {
	dataDir := t.TempDir()
	receiptPath := filepath.Join(dataDir, "upgrade_receipt.json")
	if err := os.WriteFile(receiptPath, []byte(`{"status":"pending"}`), 0644); err != nil {
		t.Fatal(err)
	}
	VerifyAndClearPendingUpgrade(dataDir)
	if _, err := os.Stat(receiptPath); !os.IsNotExist(err) {
		t.Fatal("expected receipt to be removed after verification")
	}
}

func TestChecksumMismatchPreservesOriginalBinary(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	_ = os.MkdirAll(dataDir, 0755)

	targetBin := filepath.Join(tmpDir, "probewatch-agent")
	origContent := []byte("original-valid-agent-binary-v0.9.3")
	if err := os.WriteFile(targetBin, origContent, 0755); err != nil {
		t.Fatal(err)
	}

	payload := make([]byte, 1024*1024+50)
	copy(payload, []byte("new-corrupted-or-mismatched-binary"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(payload)
	}))
	defer server.Close()

	err := downloadAndApplyUpdateTo(context.Background(), server.Client(), server.URL, "", &UpdateCheckResult{
		UpdateAvailable:    true,
		DownloadURL:        server.URL,
		LatestAgentVersion: "0.9.4",
		ChecksumSHA256:     "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
	}, dataDir, targetBin)

	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("expected ErrChecksumMismatch, got %v", err)
	}

	// Verify original binary is 100% preserved
	after, err := os.ReadFile(targetBin)
	if err != nil {
		t.Fatalf("target binary disappeared: %v", err)
	}
	if string(after) != string(origContent) {
		t.Fatalf("target binary was overwritten or corrupted: got %s, want %s", string(after), string(origContent))
	}

	// Verify staging file was cleaned up
	if _, err := os.Stat(targetBin + ".staging"); !os.IsNotExist(err) {
		t.Fatal("staging file was not cleaned up after error")
	}
}

func TestInterruptedDownloadPreservesOriginalBinary(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	_ = os.MkdirAll(dataDir, 0755)

	targetBin := filepath.Join(tmpDir, "probewatch-agent")
	origContent := []byte("original-agent-before-interrupt")
	if err := os.WriteFile(targetBin, origContent, 0755); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Send initial chunk and sleep
		w.Write([]byte("chunk-1-"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		time.Sleep(500 * time.Millisecond)
		w.Write([]byte("chunk-2"))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_ = downloadAndApplyUpdateTo(ctx, server.Client(), server.URL, "", &UpdateCheckResult{
		UpdateAvailable:    true,
		DownloadURL:        server.URL,
		LatestAgentVersion: "0.9.4",
	}, dataDir, targetBin)

	// Verify original binary was not modified or corrupted
	after, err := os.ReadFile(targetBin)
	if err != nil {
		t.Fatalf("failed to read binary after interrupt: %v", err)
	}
	if string(after) != string(origContent) {
		t.Fatalf("original binary corrupted by interrupted download: %s", string(after))
	}
}

func TestSuccessfulUpgradeReceiptAndRollback(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	_ = os.MkdirAll(dataDir, 0755)

	targetBin := filepath.Join(tmpDir, "probewatch-agent")
	origContent := []byte("agent-v0.9.3-production")
	if err := os.WriteFile(targetBin, origContent, 0755); err != nil {
		t.Fatal(err)
	}

	// Valid payload > 1MB
	newBinary := make([]byte, 1024*1024+64)
	copy(newBinary, []byte("agent-v0.9.4-upgraded-binary"))
	h := sha256.Sum256(newBinary)
	expectedHash := hex.EncodeToString(h[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(newBinary)
	}))
	defer server.Close()

	err := downloadAndApplyUpdateTo(context.Background(), server.Client(), server.URL, "", &UpdateCheckResult{
		UpdateAvailable:    true,
		DownloadURL:        server.URL,
		LatestAgentVersion: "0.9.4",
		ChecksumSHA256:     expectedHash,
	}, dataDir, targetBin)
	if err != nil {
		t.Fatalf("downloadAndApplyUpdateTo failed: %v", err)
	}

	// 1. Verify targetBin now has the new binary
	current, err := os.ReadFile(targetBin)
	if err != nil || len(current) != len(newBinary) {
		t.Fatalf("target binary not swapped correctly: len=%d, err=%v", len(current), err)
	}

	// 2. Verify backup file .bak exists and has original content
	bakContent, err := os.ReadFile(targetBin + ".bak")
	if err != nil || string(bakContent) != string(origContent) {
		t.Fatalf("backup file mismatch: got %s, want %s, err=%v", string(bakContent), string(origContent), err)
	}

	// 3. Verify receipt file was written with status pending_verification
	receiptPath := filepath.Join(dataDir, "upgrade_receipt.json")
	receiptData, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatalf("receipt file not found: %v", err)
	}
	var receipt map[string]any
	if err := json.Unmarshal(receiptData, &receipt); err != nil {
		t.Fatalf("unmarshal receipt: %v", err)
	}
	if receipt["status"] != "pending_verification" {
		t.Errorf("receipt status = %v, want pending_verification", receipt["status"])
	}
	if receipt["to_version"] != "0.9.4" {
		t.Errorf("receipt to_version = %v, want 0.9.4", receipt["to_version"])
	}

	// 4. Test Rollback restores original binary
	if err := RollbackTo(targetBin); err != nil {
		t.Fatalf("RollbackTo failed: %v", err)
	}
	restored, err := os.ReadFile(targetBin)
	if err != nil || string(restored) != string(origContent) {
		t.Fatalf("rollback failed to restore original binary: got %s, want %s", string(restored), string(origContent))
	}
}

func TestRuntimeStrategyDetection(t *testing.T) {
	// 1. Test container detection
	t.Setenv("PROBEWATCH_IN_CONTAINER", "1")
	if strat := DetectRuntimeStrategy(); strat != StrategyContainer {
		t.Errorf("expected StrategyContainer, got %s", strat)
	}

	// 2. Test systemd supervisor detection (disable container override)
	t.Setenv("PROBEWATCH_IN_CONTAINER", "0")
	t.Setenv("container", "")
	t.Setenv("INVOCATION_ID", "test-systemd-invocation-id-123")
	if strat := DetectRuntimeStrategy(); strat != StrategySupervisor {
		t.Errorf("expected StrategySupervisor for systemd, got %s", strat)
	}

	// 3. Test OpenRC detection
	t.Setenv("INVOCATION_ID", "")
	t.Setenv("RC_SERVICE", "probewatch-agent")
	if strat := DetectRuntimeStrategy(); strat != StrategySupervisor {
		t.Errorf("expected StrategySupervisor for OpenRC, got %s", strat)
	}

	// 4. Test standalone fallback
	t.Setenv("RC_SERVICE", "")
	if strat := DetectRuntimeStrategy(); strat != StrategyStandalone {
		t.Errorf("expected StrategyStandalone, got %s", strat)
	}
}
