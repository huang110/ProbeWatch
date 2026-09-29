package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/version"
)

type UpdateCheckResponse struct {
	ServerVersion      string `json:"server_version"`
	LatestAgentVersion string `json:"latest_agent_version"`
	MinAgentVersion    string `json:"min_agent_version"`
	ReleaseNotes       string `json:"release_notes"`
	DownloadURL        string `json:"download_url"`
	ChecksumSHA256     string `json:"checksum_sha256,omitempty"`
}

// agentUpdateCheck returns metadata about the latest release.
func (s *Server) agentUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	w.Header().Set("Cache-Control", "no-store")

	// Calculate checksum if binary is available locally
	var checksum string
	binaryPath := s.findAgentBinaryPath("linux", "amd64")
	if binaryPath != "" {
		if sum, err := computeFileSHA256(binaryPath); err == nil {
			checksum = sum
		}
	}

	downloadURL := strings.TrimRight(s.cfg.PublicBaseURL, "/") + "/api/agent/v1/update/download"
	if s.cfg.PublicBaseURL == "" {
		downloadURL = "/api/agent/v1/update/download"
	}

	resp := UpdateCheckResponse{
		ServerVersion:      version.ServerVersion,
		LatestAgentVersion: version.AgentVersion,
		MinAgentVersion:    version.MinAgentVersion,
		ReleaseNotes:       fmt.Sprintf("ProbeWatch Agent v%s: current stable agent release", version.AgentVersion),
		DownloadURL:        downloadURL,
		ChecksumSHA256:     checksum,
	}
	writeJSON(w, http.StatusOK, resp)
}

// agentUpdateDownload serves the compiled agent binary for edge nodes to download.
func (s *Server) agentUpdateDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	w.Header().Set("Cache-Control", "no-store")

	q := r.URL.Query()
	targetOS := strings.ToLower(strings.TrimSpace(q.Get("os")))
	if targetOS == "" {
		targetOS = "linux"
	}
	targetArch := strings.ToLower(strings.TrimSpace(q.Get("arch")))
	if targetArch == "" {
		targetArch = "amd64"
	}

	// Security: strictly validate os and arch parameters
	if targetOS != "linux" && targetOS != "darwin" && targetOS != "windows" {
		writeJSONError(w, http.StatusBadRequest, "unsupported target os")
		return
	}
	if targetArch != "amd64" && targetArch != "arm64" {
		writeJSONError(w, http.StatusBadRequest, "unsupported target arch")
		return
	}

	binaryPath := s.findAgentBinaryPath(targetOS, targetArch)
	if binaryPath == "" {
		writeJSONError(w, http.StatusNotFound, fmt.Sprintf("agent binary for %s/%s not staged on server", targetOS, targetArch))
		return
	}

	info, err := os.Stat(binaryPath)
	if err != nil || info.IsDir() {
		writeJSONError(w, http.StatusNotFound, "binary not available")
		return
	}

	checksum, err := computeFileSHA256(binaryPath)
	if err == nil && checksum != "" {
		w.Header().Set("X-Checksum-SHA256", checksum)
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="probewatch-agent-%s-%s"`, targetOS, targetArch))
	http.ServeFile(w, r, binaryPath)
}

// publicVersion provides public version info for dashboards and status checks.
func (s *Server) publicVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	writeJSON(w, http.StatusOK, map[string]string{
		"server_version":       version.ServerVersion,
		"latest_agent_version": version.AgentVersion,
		"min_agent_version":    version.MinAgentVersion,
	})
}

// findAgentBinaryPath checks standard release and binary locations on the server.
func (s *Server) findAgentBinaryPath(osName, archName string) string {
	candidates := []string{
		// Specific arch release naming
		filepath.Join("/opt/probewatch/downloads", fmt.Sprintf("probewatch-agent-%s-%s", osName, archName)),
		filepath.Join("/var/lib/probewatch/releases", fmt.Sprintf("probewatch-agent-%s-%s", osName, archName)),
	}
	// Generic install locations do not encode their target platform. They are
	// safe fallbacks only for the server's native Linux/amd64 release; using
	// them for another target could serve an incompatible executable.
	if osName == "linux" && archName == "amd64" {
		candidates = append(candidates,
			filepath.Join("/opt/probewatch-agent", "probewatch-agent"),
			filepath.Join("/opt/probewatch", "probewatch-agent"),
			filepath.Join("/usr/local/bin", "probewatch-agent"),
			"./probewatch-agent",
			"./cmd/probewatch-agent/probewatch-agent",
		)
	}

	// On Linux the server can inspect the candidate's embedded version. This
	// prevents an old staged artifact from being advertised while the running
	// canonical binary is current (or the reverse). Other target platforms
	// cannot be executed on the server, so they retain the deterministic path
	// order above.
	if osName == "linux" && archName == "amd64" {
		for _, path := range candidates {
			if isUsableAgentBinary(path) && agentBinaryMatchesVersion(path, version.AgentVersion) {
				return path
			}
		}
		// Never serve an unverified Linux/amd64 artifact. An old staged file
		// must not be advertised with the current release metadata.
		return ""
	}
	for _, path := range candidates {
		if isUsableAgentBinary(path) {
			return path
		}
	}
	return ""
}

func isUsableAgentBinary(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 1024*1024
}

var agentVersionPattern = regexp.MustCompile(`ProbeWatch Agent v([0-9]+(?:\.[0-9]+){0,2})`)

func agentBinaryMatchesVersion(path, expected string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").CombinedOutput()
	if err != nil || ctx.Err() != nil {
		return false
	}
	actual, ok := parseAgentVersion(out)
	return ok && version.Compare(actual, expected) == 0
}

func parseAgentVersion(output []byte) (string, bool) {
	m := agentVersionPattern.FindSubmatch(output)
	if len(m) != 2 {
		return "", false
	}
	return string(m[1]), true
}

func computeFileSHA256(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}
