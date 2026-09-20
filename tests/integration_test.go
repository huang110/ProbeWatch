package tests

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/probewatch/probewatch/internal/api"
	"github.com/probewatch/probewatch/internal/auth"
	"github.com/probewatch/probewatch/internal/config"
	"github.com/probewatch/probewatch/internal/db"
)

func TestTask3HTTPIntegrationHealthAndProtectedAPI(t *testing.T) {
	store, err := db.OpenStore(filepath.Join(t.TempDir(), "probe.db"), []byte("integration-test-pepper"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := config.Config{Environment: "development", PublicBaseURL: "http://127.0.0.1:8080", SessionSecret: "integration-session-secret-that-is-long-enough", MaxRequestBody: 1024}
	service := auth.NewService(cfg, store, auth.ProviderEndpoints{})
	handler := api.NewServer(cfg, service).Handler()

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", health.Code)
	}
	protected := httptest.NewRecorder()
	handler.ServeHTTP(protected, httptest.NewRequest(http.MethodGet, "/api/me", nil))
	if protected.Code != http.StatusUnauthorized {
		t.Fatalf("protected status = %d, want 401", protected.Code)
	}
}
