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
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/probewatch/probewatch/internal/version"
)

var (
	ErrNoUpdateAvailable  = errors.New("already running latest agent version")
	ErrChecksumMismatch   = errors.New("downloaded binary checksum mismatch")
	ErrChecksumRequired   = errors.New("update metadata missing checksum")
	ErrInvalidDownloadURL = errors.New("update download URL is not trusted")
	ErrBinaryTooSmall     = errors.New("downloaded binary is abnormally small or incomplete")
	ErrBinaryTooLarge     = errors.New("downloaded binary exceeds the maximum size")
)

const maxAgentBinaryBytes int64 = 256 << 20

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
	checkURL, err := updateCheckURL(endpoint, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return nil, fmt.Errorf("create update check URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checkURL, nil)
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

// updateCheckURL appends the running agent platform so the control plane can
// return a matching binary and checksum. Keeping this in the agent (rather
// than relying on server defaults) is important for Windows and ARM agents.
func updateCheckURL(endpoint, goos, goarch string) (string, error) {
	base, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || base.Scheme == "" || base.Host == "" || base.User != nil || base.Opaque != "" || (base.Scheme != "http" && base.Scheme != "https") {
		return "", ErrInvalidDownloadURL
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/update/check"
	base.RawPath = ""
	q := base.Query()
	q.Set("os", strings.ToLower(strings.TrimSpace(goos)))
	q.Set("arch", strings.ToLower(strings.TrimSpace(goarch)))
	base.RawQuery = q.Encode()
	return base.String(), nil
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

	downloadURL, err := trustedDownloadURL(endpoint, result.DownloadURL)
	if err != nil {
		return err
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
	// Bound the client-side staging write as well as the server-side response.
	// A misconfigured or compromised endpoint must not be able to fill the
	// agent's disk before checksum validation fails.
	written, err := io.Copy(writer, io.LimitReader(resp.Body, maxAgentBinaryBytes+1))
	_ = stagingFile.Close()
	if err != nil {
		_ = os.Remove(stagingPath)
		return fmt.Errorf("save staging binary: %w", err)
	}
	if written > maxAgentBinaryBytes {
		_ = os.Remove(stagingPath)
		return ErrBinaryTooLarge
	}

	// Verify size (at least 1 MB for compiled Go binary)
	if written < 1024*1024 {
		_ = os.Remove(stagingPath)
		return ErrBinaryTooSmall
	}

	// A release without an integrity value must never replace the running
	// executable. The control plane is responsible for publishing SHA-256.
	actualHash := hex.EncodeToString(hasher.Sum(nil))
	expected := strings.TrimSpace(result.ChecksumSHA256)
	if expected == "" {
		_ = os.Remove(stagingPath)
		return ErrChecksumRequired
	}
	if !isSHA256Hex(expected) {
		_ = os.Remove(stagingPath)
		return fmt.Errorf("%w: invalid SHA-256 metadata", ErrChecksumRequired)
	}
	if !strings.EqualFold(actualHash, expected) {
		_ = os.Remove(stagingPath)
		return fmt.Errorf("%w: expected %s, got %s", ErrChecksumMismatch, expected, actualHash)
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
		if linkErr, ok := err.(*os.LinkError); ok && linkErr.Err == syscall.EXDEV {
			if copyErr := copyFile(stagingPath, execPath); copyErr == nil {
				_ = os.Remove(stagingPath)
				_ = os.Chmod(execPath, 0755)
				goto installed
			}
		}
		// Attempt rollback from .bak
		_ = os.Rename(bakPath, execPath)
		_ = os.Remove(stagingPath)
		return fmt.Errorf("install new binary: %w (restored from backup)", err)
	}
installed:

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

func isSHA256Hex(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// trustedDownloadURL only permits relative URLs or an absolute URL on the
// same origin as the control plane. This prevents a compromised/misconfigured
// update response from sending the node token to an arbitrary host.
func trustedDownloadURL(endpoint, raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", ErrInvalidDownloadURL
	}
	base, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || base.Scheme == "" || base.Host == "" || base.User != nil || (base.Scheme != "http" && base.Scheme != "https") {
		return "", ErrInvalidDownloadURL
	}
	ref, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", ErrInvalidDownloadURL
	}
	baseForResolve := *base
	if ref.Scheme == "" && ref.Host == "" && !strings.HasPrefix(ref.Path, "/") && !strings.HasSuffix(baseForResolve.Path, "/") {
		baseForResolve.Path += "/"
	}
	resolved := baseForResolve.ResolveReference(ref)
	if !sameUpdateOrigin(resolved, base) || resolved.User != nil {
		return "", ErrInvalidDownloadURL
	}
	return resolved.String(), nil
}

// sameUpdateOrigin compares the actual network origin rather than URL.Host
// verbatim. An endpoint written as https://host:443 and a release URL written
// as https://host are the same origin, while a different explicit port is not.
func sameUpdateOrigin(a, b *url.URL) bool {
	if a == nil || b == nil || !strings.EqualFold(a.Scheme, b.Scheme) || !strings.EqualFold(a.Hostname(), b.Hostname()) {
		return false
	}
	port := func(u *url.URL) string {
		if p := u.Port(); p != "" {
			return p
		}
		if strings.EqualFold(u.Scheme, "https") {
			return "443"
		}
		return "80"
	}
	return port(a) == port(b)
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
