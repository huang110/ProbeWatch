package tests

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/probewatch/probewatch/internal/api"
	"github.com/probewatch/probewatch/internal/auth"
	"github.com/probewatch/probewatch/internal/config"
	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/deploy"
)

func TestInstallScriptParityAndContent(t *testing.T) {
	rootDeployScript, err := os.ReadFile(filepath.Join("..", "deploy", "install.sh"))
	if err != nil {
		t.Fatalf("failed to read deploy/install.sh: %v", err)
	}
	internalDeployScript, err := os.ReadFile(filepath.Join("..", "internal", "deploy", "install.sh"))
	if err != nil {
		t.Fatalf("failed to read internal/deploy/install.sh: %v", err)
	}

	rootNorm := bytes.ReplaceAll(rootDeployScript, []byte("\r\n"), []byte("\n"))
	internalNorm := bytes.ReplaceAll(internalDeployScript, []byte("\r\n"), []byte("\n"))

	if !bytes.Equal(rootNorm, internalNorm) {
		t.Fatal("deploy/install.sh and internal/deploy/install.sh must be identical")
	}

	embeddedNorm := bytes.ReplaceAll(deploy.InstallScript, []byte("\r\n"), []byte("\n"))
	if !bytes.Equal(internalNorm, embeddedNorm) {
		t.Fatal("internal/deploy/install.sh and embedded deploy.InstallScript must be identical")
	}

	content := string(rootNorm)
	requiredPhrases := []string{
		"--enable-remote-control=true|false",
		"--remote-control=true|false",
		"REMOTE_CONTROL_ENABLED=\"true\"",
		"REMOTE_CONTROL_ENABLED=\"false\"",
		"PROBEWATCH_AGENT_ENABLE_TERMINAL=${REMOTE_CONTROL_ENABLED}",
		"远程管理参数值无效，请使用 true/false、1/0、yes/no 或 on/off",
		"--dry-run",
	}

	for _, phrase := range requiredPhrases {
		if !strings.Contains(content, phrase) {
			t.Errorf("deploy/install.sh missing required phrase: %q", phrase)
		}
	}
}

func TestInstallScriptEndpointServesUpdatedScript(t *testing.T) {
	store, err := db.OpenStore(filepath.Join(t.TempDir(), "probe.db"), []byte("install-test-pepper"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := config.Config{Environment: "development", PublicBaseURL: "http://127.0.0.1:8080", SessionSecret: "integration-session-secret-that-is-long-enough"}
	service := auth.NewService(cfg, store, auth.ProviderEndpoints{})
	handler := api.NewServer(cfg, service).Handler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/install.sh", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/install.sh returned code %d, want 200", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "--enable-remote-control=true|false") {
		t.Errorf("/install.sh served content missing updated help text")
	}
	if !strings.Contains(body, "PROBEWATCH_AGENT_ENABLE_TERMINAL=${REMOTE_CONTROL_ENABLED}") {
		t.Errorf("/install.sh served content missing REMOTE_CONTROL_ENABLED propagation")
	}
}
