package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/probewatch/probewatch/internal/version"
)

func TestUpdateEndpoints(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	handler := NewServer(cfg, service).Handler()

	// 1. GET /api/public/version
	pubReq := httptest.NewRequest(http.MethodGet, "/api/public/version", nil)
	pubRec := httptest.NewRecorder()
	handler.ServeHTTP(pubRec, pubReq)
	if pubRec.Code != http.StatusOK {
		t.Fatalf("public version code = %d, want 200", pubRec.Code)
	}
	var pubVersion map[string]string
	if err := json.Unmarshal(pubRec.Body.Bytes(), &pubVersion); err != nil {
		t.Fatal(err)
	}
	if pubVersion["server_version"] != version.ServerVersion || pubVersion["latest_agent_version"] != version.AgentVersion {
		t.Fatalf("unexpected public version: %+v", pubVersion)
	}

	// 2. GET /api/agent/v1/update/check
	checkReq := httptest.NewRequest(http.MethodGet, "/api/agent/v1/update/check", nil)
	checkRec := httptest.NewRecorder()
	handler.ServeHTTP(checkRec, checkReq)
	if checkRec.Code != http.StatusOK {
		t.Fatalf("update check code = %d, want 200", checkRec.Code)
	}
	var checkResp UpdateCheckResponse
	if err := json.Unmarshal(checkRec.Body.Bytes(), &checkResp); err != nil {
		t.Fatal(err)
	}
	if checkResp.LatestAgentVersion != version.AgentVersion {
		t.Fatalf("latest agent version = %s, want %s", checkResp.LatestAgentVersion, version.AgentVersion)
	}

	// 3. GET /api/agent/v1/update/download with invalid OS -> 400
	badOSReq := httptest.NewRequest(http.MethodGet, "/api/agent/v1/update/download?os=solaris&arch=amd64", nil)
	badOSRec := httptest.NewRecorder()
	handler.ServeHTTP(badOSRec, badOSReq)
	if badOSRec.Code != http.StatusBadRequest {
		t.Fatalf("bad os code = %d, want 400", badOSRec.Code)
	}

	// 4. Staged binary test: create fake staged binary and download
	tmpDir := t.TempDir()
	stagedPath := filepath.Join(tmpDir, "probewatch-agent-linux-amd64")
	fakeContent := make([]byte, 1024*1024+10) // > 1MB
	copy(fakeContent, []byte("\x7fELFfakebinarycontent"))
	if err := os.WriteFile(stagedPath, fakeContent, 0755); err != nil {
		t.Fatal(err)
	}

	// Verify computeFileSHA256 works
	hash, err := computeFileSHA256(stagedPath)
	if err != nil || len(hash) != 64 {
		t.Fatalf("computeFileSHA256 failed: %v, hash = %s", err, hash)
	}
}
