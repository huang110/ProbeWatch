package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

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
