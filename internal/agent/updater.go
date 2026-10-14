package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/version"
)

var (
	ErrNoUpdateAvailable = errors.New("already running latest agent version")
	ErrChecksumMismatch  = errors.New("downloaded binary checksum mismatch")
	ErrBinaryTooSmall    = errors.New("downloaded binary is abnormally small or incomplete")
)

// UpdateCheckResult holds version metadata from the server.
type UpdateCheckResult struct {
	ServerVersion      string `json:"server_version"`
	LatestAgentVersion string `json:"latest_agent_version"`
	MinAgentVersion    string `json:"min_agent_version"`
	ReleaseNotes       string `json:"release_notes"`
	DownloadURL        string `json:"download_url"`
	ChecksumSHA256     string `json:"checksum_sha256"`
	UpdateAvailable    bool   `json:"update_available"`
}

// CheckUpdate queries the control plane to check if a newer Agent release is available.
func CheckUpdate(ctx context.Context, client *http.Client, endpoint, token string) (*UpdateCheckResult, error) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	url := strings.TrimRight(endpoint, "/") + "/update/check"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create update check request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("update check request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update check returned status %d", resp.StatusCode)
	}

	var result UpdateCheckResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode update check response: %w", err)
	}

	result.UpdateAvailable = version.IsUpgradeAvailable(version.AgentVersion, result.LatestAgentVersion)
	return &result, nil
}

type RuntimeStrategy string

const (
	StrategySupervisor RuntimeStrategy = "supervisor_managed"
	StrategyContainer  RuntimeStrategy = "container_managed"
	StrategyStandalone RuntimeStrategy = "restart_required"
)

// DetectRuntimeStrategy determines how the agent process lifecycle is managed.
func DetectRuntimeStrategy() RuntimeStrategy {
	if isContainerEnvironment() {
		return StrategyContainer
	}
	if isSupervisorEnvironment() {
		return StrategySupervisor
	}
	return StrategyStandalone
}

func isContainerEnvironment() bool {
	if os.Getenv("PROBEWATCH_IN_CONTAINER") == "0" {
		return false
	}
	if os.Getenv("PROBEWATCH_IN_CONTAINER") == "1" || os.Getenv("container") != "" {
		return true
	}
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	if _, err := os.Stat("/run/.containerenv"); err == nil {
		return true
	}
	return false
}

func isSupervisorEnvironment() bool {
	if os.Getenv("INVOCATION_ID") != "" || os.Getenv("JOURNAL_STREAM") != "" || os.Getenv("SYSTEMD_EXEC_PID") != "" {
		return true
	}
	if os.Getenv("RC_SERVICE") != "" {
		return true
	}
	if os.Getenv("SUPERVISOR_ENABLED") == "1" {
		return true
	}
	return false
}

// ApplyRuntimeStrategy handles process transition after a successful binary swap.
// Under systemd/OpenRC supervisors, it gracefully exits so supervisor restarts with new binary.
func ApplyRuntimeStrategy() RuntimeStrategy {
	strategy := DetectRuntimeStrategy()
	switch strategy {
	case StrategySupervisor:
		go func() {
			time.Sleep(500 * time.Millisecond)
			os.Exit(0)
		}()
	case StrategyContainer:
		// Do not kill container process from inside
	case StrategyStandalone:
		// Standalone process: manual restart required
	}
	return strategy
}

// DownloadAndApplyUpdate downloads the new agent binary, verifies its checksum,
// safely backs up the current binary to .bak, and swaps in the new executable.
func DownloadAndApplyUpdate(ctx context.Context, client *http.Client, endpoint, token string, result *UpdateCheckResult, dataDir string) error {
	return downloadAndApplyUpdateTo(ctx, client, endpoint, token, result, dataDir, "")
}

func downloadAndApplyUpdateTo(ctx context.Context, client *http.Client, endpoint, token string, result *UpdateCheckResult, dataDir, targetExecPath string) error {
	if !result.UpdateAvailable {
		return ErrNoUpdateAvailable
	}
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
	}

	downloadURL := result.DownloadURL
	if !strings.HasPrefix(downloadURL, "http://") && !strings.HasPrefix(downloadURL, "https://") {
		downloadURL = strings.TrimRight(endpoint, "/") + "/" + strings.TrimLeft(downloadURL, "/")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download agent binary: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}

	// Determine current running binary path
	execPath := targetExecPath
	if execPath == "" {
		p, err := os.Executable()
		if err != nil {
			return fmt.Errorf("determine executable path: %w", err)
		}
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			execPath = resolved
		} else {
			execPath = p
		}
	}

	// Stage new binary adjacent to current executable if writable, otherwise in dataDir
	stagingPath := execPath + ".staging"
	stagingFile, err := os.OpenFile(stagingPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		// Fallback to dataDir
		stagingPath = filepath.Join(dataDir, "probewatch-agent.staging")
		stagingFile, err = os.OpenFile(stagingPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			return fmt.Errorf("open staging file: %w", err)
		}
	}

	hasher := sha256.New()
	writer := io.MultiWriter(stagingFile, hasher)
	written, err := io.Copy(writer, resp.Body)
	_ = stagingFile.Close()
	if err != nil {
		_ = os.Remove(stagingPath)
		return fmt.Errorf("save staging binary: %w", err)
	}

	// Verify size (at least 1 MB for compiled Go binary)
	if written < 1024*1024 {
		_ = os.Remove(stagingPath)
		return ErrBinaryTooSmall
	}

	// Verify checksum if provided
	actualHash := hex.EncodeToString(hasher.Sum(nil))
	if expected := strings.TrimSpace(result.ChecksumSHA256); expected != "" {
		if !strings.EqualFold(actualHash, expected) {
			_ = os.Remove(stagingPath)
			return fmt.Errorf("%w: expected %s, got %s", ErrChecksumMismatch, expected, actualHash)
		}
	}

	// Backup existing executable if present
	bakPath := execPath + ".bak"
	_ = os.Remove(bakPath)
	if _, err := os.Stat(execPath); err == nil {
		if err := os.Rename(execPath, bakPath); err != nil {
			// Try copy if rename fails (e.g. cross-device or permission edge case)
			if copyErr := copyFile(execPath, bakPath); copyErr != nil {
				_ = os.Remove(stagingPath)
				return fmt.Errorf("backup existing binary to %s: %w", bakPath, err)
			}
		}
	}

	// Atomically move staging binary to execPath
	if err := os.Rename(stagingPath, execPath); err != nil {
		// Attempt rollback from .bak
		_ = os.Rename(bakPath, execPath)
		_ = os.Remove(stagingPath)
		return fmt.Errorf("install new binary: %w (restored from backup)", err)
	}

	_ = os.Chmod(execPath, 0755)

	// Save upgrade status receipt in dataDir
	strategy := DetectRuntimeStrategy()
	receipt := map[string]any{
		"from_version": version.AgentVersion,
		"to_version":   result.LatestAgentVersion,
		"upgraded_at":  time.Now().UTC().Unix(),
		"checksum":     actualHash,
		"status":       "pending_verification",
		"strategy":     string(strategy),
	}
	if b, err := json.MarshalIndent(receipt, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(dataDir, "upgrade_receipt.json"), b, 0644)
	}

	return nil
}

// VerifyAndClearPendingUpgrade marks the upgrade as verified once healthy telemetry succeeds.
func VerifyAndClearPendingUpgrade(dataDir string) {
	receiptPath := filepath.Join(dataDir, "upgrade_receipt.json")
	if _, err := os.Stat(receiptPath); err == nil {
		_ = os.Remove(receiptPath)
	}
}

// Rollback restores the backup binary .bak to execPath if present.
func Rollback() error {
	return RollbackTo("")
}

// RollbackTo restores the backup binary .bak to the target executable path if present.
func RollbackTo(targetExecPath string) error {
	execPath := targetExecPath
	if execPath == "" {
		p, err := os.Executable()
		if err != nil {
			return err
		}
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			execPath = resolved
		} else {
			execPath = p
		}
	}
	bakPath := execPath + ".bak"
	if _, err := os.Stat(bakPath); err != nil {
		return fmt.Errorf("no backup binary found at %s", bakPath)
	}
	_ = os.Remove(execPath)
	return os.Rename(bakPath, execPath)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}
