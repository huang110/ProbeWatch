package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/probewatch/probewatch/internal/runtime"
)

func TestRunEntrypointBoundary(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*testing.T)
		check     func(*testing.T, error)
	}{
		{
			name: "runtime error is returned",
			configure: func(t *testing.T) {
				t.Setenv("PROBEWATCH_ENV", "development")
			},
			check: func(t *testing.T, err error) {
				if !errors.Is(err, runtime.ErrAgentNotConfigured) || errors.Is(err, runtime.ErrControlPlaneNotConfigured) {
					t.Fatalf("run() error = %v, want agent sentinel only", err)
				}
			},
		},
		{
			name: "config error is returned",
			configure: func(t *testing.T) {
				t.Setenv("PROBEWATCH_ENV", "invalid")
			},
			check: func(t *testing.T, err error) {
				if err == nil || !strings.Contains(err.Error(), "PROBEWATCH_ENV") {
					t.Fatalf("run() error = %v, want PROBEWATCH_ENV error", err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearConfigEnvironment(t)
			test.configure(t)
			test.check(t, run())
		})
	}
}

func clearConfigEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"PROBEWATCH_ENV", "PROBEWATCH_LISTEN", "PROBEWATCH_ALLOW_NON_LOOPBACK_LISTEN", "PROBEWATCH_DEPLOYMENT_MODE", "PROBEWATCH_DATABASE", "PROBEWATCH_PUBLIC_BASE_URL",
		"GITHUB_CLIENT_ID", "GITHUB_CLIENT_SECRET", "GITHUB_REDIRECT_URL", "GITHUB_ALLOWED_USERS",
		"GITHUB_ALLOWED_ORG", "SESSION_SECRET", "AGENT_TOKEN_TTL", "AGENT_CLOCK_SKEW",
		"MAX_REQUEST_BODY", "AGENT_IGNORE_UNSAFE_CERT",
	} {
		t.Setenv(name, "")
	}
}
