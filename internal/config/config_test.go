package config

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestLoadDevelopmentDefaults(t *testing.T) {
	clearConfigEnvironment(t)
	t.Setenv("PROBEWATCH_ENV", "development")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Environment != "development" {
		t.Fatalf("Environment = %q, want development", cfg.Environment)
	}
	if cfg.ListenAddress != "127.0.0.1:8080" {
		t.Fatalf("ListenAddress = %q", cfg.ListenAddress)
	}
	if cfg.DatabasePath != "./data/probewatch.db" {
		t.Fatalf("DatabasePath = %q", cfg.DatabasePath)
	}
	if cfg.PublicBaseURL != "http://127.0.0.1:8080" {
		t.Fatalf("PublicBaseURL = %q", cfg.PublicBaseURL)
	}
	if cfg.AgentTokenTTL != 15*time.Minute {
		t.Fatalf("AgentTokenTTL = %s", cfg.AgentTokenTTL)
	}
	if cfg.AgentNodeTokenTTL != 365*24*time.Hour {
		t.Fatalf("AgentNodeTokenTTL = %s", cfg.AgentNodeTokenTTL)
	}
	if cfg.AgentClockSkew != 5*time.Minute {
		t.Fatalf("AgentClockSkew = %s", cfg.AgentClockSkew)
	}
	if cfg.MaxRequestBody != 1<<20 {
		t.Fatalf("MaxRequestBody = %d", cfg.MaxRequestBody)
	}
	if cfg.AgentIgnoreUnsafeCert {
		t.Fatal("development default enabled unsafe certificate bypass")
	}
	if cfg.TokenPepper == "" {
		t.Fatal("development token pepper is empty")
	}
}

func TestLoadParsesEnvironmentValues(t *testing.T) {
	clearConfigEnvironment(t)
	t.Setenv("PROBEWATCH_ENV", "development")
	t.Setenv("PROBEWATCH_LISTEN", "127.0.0.1:9090")
	t.Setenv("PROBEWATCH_ALLOW_NON_LOOPBACK_LISTEN", "false")
	t.Setenv("PROBEWATCH_DATABASE", "./tmp/test.db")
	t.Setenv("PROBEWATCH_PUBLIC_BASE_URL", "http://127.0.0.1:9090")
	t.Setenv("GITHUB_CLIENT_ID", "client-id")
	t.Setenv("GITHUB_CLIENT_SECRET", "client-secret")
	t.Setenv("GITHUB_REDIRECT_URL", "http://127.0.0.1:9090/callback")
	t.Setenv("GITHUB_ALLOWED_USERS", " alice, bob ,, ")
	t.Setenv("GITHUB_ALLOWED_ORG", "example")
	t.Setenv("SESSION_SECRET", "development-secret")
	t.Setenv("PROBEWATCH_TOKEN_PEPPER", "development-token-pepper")
	t.Setenv("AGENT_TOKEN_TTL", "30m")
	t.Setenv("AGENT_NODE_TOKEN_TTL", "720h")
	t.Setenv("AGENT_CLOCK_SKEW", "45s")
	t.Setenv("MAX_REQUEST_BODY", "2MiB")
	t.Setenv("AGENT_IGNORE_UNSAFE_CERT", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.ListenAddress != "127.0.0.1:9090" || cfg.DatabasePath != "./tmp/test.db" {
		t.Fatalf("core values were not parsed: %+v", cfg)
	}
	if cfg.PublicBaseURL != "http://127.0.0.1:9090" {
		t.Fatalf("PublicBaseURL = %q", cfg.PublicBaseURL)
	}
	if strings.Join(cfg.GitHubAllowedUsers, "|") != "alice|bob" {
		t.Fatalf("GitHubAllowedUsers = %#v", cfg.GitHubAllowedUsers)
	}
	if cfg.AgentTokenTTL != 30*time.Minute || cfg.AgentNodeTokenTTL != 720*time.Hour || cfg.AgentClockSkew != 45*time.Second {
		t.Fatalf("durations were not parsed: ttl=%s node ttl=%s skew=%s", cfg.AgentTokenTTL, cfg.AgentNodeTokenTTL, cfg.AgentClockSkew)
	}
	if cfg.MaxRequestBody != 2<<20 || !cfg.AgentIgnoreUnsafeCert {
		t.Fatalf("limits or boolean were not parsed: size=%d unsafe=%t", cfg.MaxRequestBody, cfg.AgentIgnoreUnsafeCert)
	}
	if cfg.TokenPepper != "development-token-pepper" {
		t.Fatalf("TokenPepper = %q", cfg.TokenPepper)
	}
}

func TestLoadRejectsMissingProductionSecrets(t *testing.T) {
	clearConfigEnvironment(t)
	t.Setenv("PROBEWATCH_ENV", "production")

	if _, err := Load(); err == nil {
		t.Fatal("Load accepted missing production secrets")
	}
}

func TestLoadRejectsUnsafeCertificateBypassInProduction(t *testing.T) {
	clearConfigEnvironment(t)
	setValidProductionEnvironment(t)
	t.Setenv("AGENT_IGNORE_UNSAFE_CERT", "true")

	if _, err := Load(); err == nil {
		t.Fatal("Load accepted unsafe certificate bypass in production")
	}
}

func TestLoadRejectsShortProductionSessionSecret(t *testing.T) {
	clearConfigEnvironment(t)
	setValidProductionEnvironment(t)
	t.Setenv("SESSION_SECRET", "too-short")

	if _, err := Load(); err == nil {
		t.Fatal("Load accepted a short production session secret")
	}
}

func TestLoadRejectsMissingProductionTokenPepper(t *testing.T) {
	clearConfigEnvironment(t)
	setValidProductionEnvironment(t)
	t.Setenv("PROBEWATCH_TOKEN_PEPPER", "")

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "PROBEWATCH_TOKEN_PEPPER") {
		t.Fatalf("error = %v, want a PROBEWATCH_TOKEN_PEPPER error", err)
	}
}

func TestLoadRejectsMissingProductionNodeTokenTTL(t *testing.T) {
	clearConfigEnvironment(t)
	setValidProductionEnvironment(t)
	t.Setenv("AGENT_NODE_TOKEN_TTL", "")

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "AGENT_NODE_TOKEN_TTL") {
		t.Fatalf("error = %v, want an AGENT_NODE_TOKEN_TTL error", err)
	}
}

func TestLoadRejectsMissingProductionPublicBaseURL(t *testing.T) {
	clearConfigEnvironment(t)
	setValidProductionEnvironment(t)
	t.Setenv("PROBEWATCH_PUBLIC_BASE_URL", "")

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "PROBEWATCH_PUBLIC_BASE_URL") {
		t.Fatalf("error = %v, want a PROBEWATCH_PUBLIC_BASE_URL error", err)
	}
}

func TestLoadRejectsInvalidProductionPublicBaseURL(t *testing.T) {
	for _, value := range []string{
		":/malformed",
		"http://example.test",
		"https://:443",
		"https://example.test:65536",
		"https://user:pass@example.test",
		"https://example.test/path?query=1",
		"https://example.test/path#fragment",
		"https://example.test?",
		"https://example.test#",
		"https://monitor.example.test:",
	} {
		t.Run(value, func(t *testing.T) {
			clearConfigEnvironment(t)
			setValidProductionEnvironment(t)
			t.Setenv("PROBEWATCH_PUBLIC_BASE_URL", value)

			if _, err := Load(); err == nil || !strings.Contains(err.Error(), "PROBEWATCH_PUBLIC_BASE_URL") {
				t.Fatalf("error = %v, want a PROBEWATCH_PUBLIC_BASE_URL error", err)
			}
		})
	}
}

func TestLoadRejectsInvalidProductionGitHubRedirectURL(t *testing.T) {
	for _, value := range []string{
		"http://monitor.example.test/callback",
		":/malformed",
		"https://:443/callback",
		"https://user:pass@monitor.example.test/callback",
		"https://monitor.example.test/callback?code=1",
		"https://monitor.example.test/callback#fragment",
		"https://monitor.example.test:/callback",
		"https://monitor.example.test:8443/callback",
		"https://other.example.test/callback",
	} {
		t.Run(value, func(t *testing.T) {
			clearConfigEnvironment(t)
			setValidProductionEnvironment(t)
			t.Setenv("GITHUB_REDIRECT_URL", value)

			if _, err := Load(); err == nil || !strings.Contains(err.Error(), "GITHUB_REDIRECT_URL") {
				t.Fatalf("error = %v, want a GITHUB_REDIRECT_URL error", err)
			}
		})
	}
}

func TestLoadRejectsInvalidListenPorts(t *testing.T) {
	for _, value := range []string{
		"127.0.0.1:",
		"127.0.0.1:not-a-port",
		"127.0.0.1:0",
		"127.0.0.1:65536",
		"0.0.0.0:",
		"0.0.0.0:not-a-port",
		"0.0.0.0:0",
		"0.0.0.0:65536",
	} {
		t.Run(value, func(t *testing.T) {
			clearConfigEnvironment(t)
			t.Setenv("PROBEWATCH_ENV", "development")
			t.Setenv("PROBEWATCH_LISTEN", value)
			t.Setenv("PROBEWATCH_ALLOW_NON_LOOPBACK_LISTEN", "true")
			t.Setenv("PROBEWATCH_DEPLOYMENT_MODE", "container")

			if _, err := Load(); err == nil || !strings.Contains(err.Error(), "PROBEWATCH_LISTEN") {
				t.Fatalf("error = %v, want a PROBEWATCH_LISTEN port error", err)
			}
		})
	}
}

func TestLoadRejectsNonLoopbackListenByDefault(t *testing.T) {
	clearConfigEnvironment(t)
	t.Setenv("PROBEWATCH_ENV", "development")
	t.Setenv("PROBEWATCH_LISTEN", "0.0.0.0:8080")

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "PROBEWATCH_ALLOW_NON_LOOPBACK_LISTEN") {
		t.Fatalf("error = %v, want a non-loopback listen opt-in error", err)
	}
}

func TestLoadRejectsNonLoopbackListenWithoutDeploymentMode(t *testing.T) {
	clearConfigEnvironment(t)
	t.Setenv("PROBEWATCH_ENV", "development")
	t.Setenv("PROBEWATCH_LISTEN", "0.0.0.0:8080")
	t.Setenv("PROBEWATCH_ALLOW_NON_LOOPBACK_LISTEN", "true")

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "PROBEWATCH_DEPLOYMENT_MODE") {
		t.Fatalf("error = %v, want a deployment-mode error", err)
	}
}

func TestLoadRejectsListenOptInWithoutDeploymentModeEvenOnLoopback(t *testing.T) {
	clearConfigEnvironment(t)
	t.Setenv("PROBEWATCH_ENV", "development")
	t.Setenv("PROBEWATCH_ALLOW_NON_LOOPBACK_LISTEN", "true")

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "PROBEWATCH_DEPLOYMENT_MODE") {
		t.Fatalf("error = %v, want a deployment-mode error", err)
	}
}

func TestLoadAcceptsDevelopmentNonLoopbackContainerMode(t *testing.T) {
	clearConfigEnvironment(t)
	t.Setenv("PROBEWATCH_ENV", "development")
	t.Setenv("PROBEWATCH_LISTEN", "0.0.0.0:8080")
	t.Setenv("PROBEWATCH_ALLOW_NON_LOOPBACK_LISTEN", "true")
	t.Setenv("PROBEWATCH_DEPLOYMENT_MODE", "container")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AllowNonLoopbackListen || cfg.DeploymentMode != "container" {
		t.Fatalf("listen opt-in state = %#v", cfg)
	}
}

func TestLoadRejectsProductionNonLoopbackWithoutExplicitOptIn(t *testing.T) {
	clearConfigEnvironment(t)
	setValidProductionEnvironment(t)
	t.Setenv("PROBEWATCH_LISTEN", "0.0.0.0:8080")

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "PROBEWATCH_ALLOW_NON_LOOPBACK_LISTEN") {
		t.Fatalf("error = %v, want a production non-loopback opt-in error", err)
	}
}

func TestLoadRejectsProductionNonLoopbackWithoutDeploymentMode(t *testing.T) {
	clearConfigEnvironment(t)
	setValidProductionEnvironment(t)
	t.Setenv("PROBEWATCH_LISTEN", "0.0.0.0:8080")
	t.Setenv("PROBEWATCH_ALLOW_NON_LOOPBACK_LISTEN", "true")

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "PROBEWATCH_DEPLOYMENT_MODE") {
		t.Fatalf("error = %v, want a missing production deployment-mode error", err)
	}
}

func TestLoadAcceptsProductionNonLoopbackContainerMode(t *testing.T) {
	clearConfigEnvironment(t)
	setValidProductionEnvironment(t)
	t.Setenv("PROBEWATCH_LISTEN", "0.0.0.0:8080")
	t.Setenv("PROBEWATCH_ALLOW_NON_LOOPBACK_LISTEN", "true")
	t.Setenv("PROBEWATCH_DEPLOYMENT_MODE", "container")

	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadAcceptsMatchingProductionGitHubRedirectURL(t *testing.T) {
	clearConfigEnvironment(t)
	setValidProductionEnvironment(t)
	t.Setenv("GITHUB_REDIRECT_URL", "https://monitor.example.test:443/auth/github/callback")

	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRejectsProductionLimitsAboveBounds(t *testing.T) {
	tests := []struct {
		name  string
		env   string
		value string
	}{
		{name: "AgentTokenTTL", env: "AGENT_TOKEN_TTL", value: "24h1s"},
		{name: "AgentNodeTokenTTL", env: "AGENT_NODE_TOKEN_TTL", value: "17520h1s"},
		{name: "AgentClockSkew", env: "AGENT_CLOCK_SKEW", value: "15m1s"},
		{name: "MaxRequestBody", env: "MAX_REQUEST_BODY", value: "16777217"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearConfigEnvironment(t)
			setValidProductionEnvironment(t)
			t.Setenv(test.env, test.value)

			if _, err := Load(); err == nil || !strings.Contains(err.Error(), test.env) {
				t.Fatalf("error = %v, want a %s bound error", err, test.env)
			}
		})
	}
}

func TestLoadRejectsDevelopmentLimitsAboveBounds(t *testing.T) {
	tests := []struct {
		name  string
		env   string
		value string
	}{
		{name: "AgentTokenTTL", env: "AGENT_TOKEN_TTL", value: "24h1s"},
		{name: "AgentNodeTokenTTL", env: "AGENT_NODE_TOKEN_TTL", value: "17520h1s"},
		{name: "AgentClockSkew", env: "AGENT_CLOCK_SKEW", value: "15m1s"},
		{name: "MaxRequestBody", env: "MAX_REQUEST_BODY", value: "16777217"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearConfigEnvironment(t)
			t.Setenv("PROBEWATCH_ENV", "development")
			t.Setenv(test.env, test.value)

			if _, err := Load(); err == nil || !strings.Contains(err.Error(), test.env) {
				t.Fatalf("error = %v, want a %s bound error", err, test.env)
			}
		})
	}
}

func TestLoadAcceptsValidProductionPublicBaseURL(t *testing.T) {
	clearConfigEnvironment(t)
	setValidProductionEnvironment(t)
	t.Setenv("PROBEWATCH_PUBLIC_BASE_URL", "https://monitor.example.test/base")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PublicBaseURL != "https://monitor.example.test/base" {
		t.Fatalf("PublicBaseURL = %q", cfg.PublicBaseURL)
	}
}

func TestLoadRejectsProductionWithoutAuthorizationPolicy(t *testing.T) {
	clearConfigEnvironment(t)
	setValidProductionEnvironment(t)
	t.Setenv("GITHUB_ALLOWED_USERS", "")
	t.Setenv("GITHUB_ALLOWED_ORG", "")

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "GITHUB_ALLOWED_USERS") {
		t.Fatalf("error = %v, want a production authorization policy error", err)
	}
}

func TestParseByteSizeAcceptsMaxInt64(t *testing.T) {
	got, err := parseByteSize("9223372036854775807")
	if err != nil {
		t.Fatal(err)
	}
	if got != math.MaxInt64 {
		t.Fatalf("parseByteSize(MaxInt64) = %d", got)
	}
}

func TestParseByteSizeRejectsMaxInt64PlusOne(t *testing.T) {
	if _, err := parseByteSize("9223372036854775808"); err == nil {
		t.Fatal("parseByteSize accepted MaxInt64+1")
	}
}

func TestParseByteSizeRejectsUnitOverflow(t *testing.T) {
	if _, err := parseByteSize("9223372036854775808B"); err == nil {
		t.Fatal("parseByteSize accepted an overflowing byte-size value")
	}
}

func TestLoadReportsInvalidDuration(t *testing.T) {
	clearConfigEnvironment(t)
	t.Setenv("AGENT_TOKEN_TTL", "not-a-duration")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "AGENT_TOKEN_TTL") {
		t.Fatalf("error = %v, want an AGENT_TOKEN_TTL parsing error", err)
	}
}

func TestLoadReportsInvalidNodeTokenTTL(t *testing.T) {
	clearConfigEnvironment(t)
	t.Setenv("AGENT_NODE_TOKEN_TTL", "not-a-duration")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "AGENT_NODE_TOKEN_TTL") {
		t.Fatalf("error = %v, want an AGENT_NODE_TOKEN_TTL parsing error", err)
	}
}

func TestLoadRejectsNonPositiveNodeTokenTTL(t *testing.T) {
	clearConfigEnvironment(t)
	for _, value := range []string{"0s", "-1h"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("AGENT_NODE_TOKEN_TTL", value)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), "AGENT_NODE_TOKEN_TTL") {
				t.Fatalf("error = %v, want an AGENT_NODE_TOKEN_TTL validation error", err)
			}
		})
	}
}

func TestLoadReportsInvalidByteSize(t *testing.T) {
	clearConfigEnvironment(t)
	t.Setenv("MAX_REQUEST_BODY", "not-a-size")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "MAX_REQUEST_BODY") {
		t.Fatalf("error = %v, want a MAX_REQUEST_BODY parsing error", err)
	}
}

func TestLoadReportsInvalidBoolean(t *testing.T) {
	clearConfigEnvironment(t)
	t.Setenv("AGENT_IGNORE_UNSAFE_CERT", "sometimes")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "AGENT_IGNORE_UNSAFE_CERT") {
		t.Fatalf("error = %v, want an AGENT_IGNORE_UNSAFE_CERT parsing error", err)
	}
}

func clearConfigEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"PROBEWATCH_ENV",
		"PROBEWATCH_LISTEN",
		"PROBEWATCH_ALLOW_NON_LOOPBACK_LISTEN",
		"PROBEWATCH_DEPLOYMENT_MODE",
		"PROBEWATCH_DATABASE",
		"PROBEWATCH_PUBLIC_BASE_URL",
		"GITHUB_CLIENT_ID",
		"GITHUB_CLIENT_SECRET",
		"GITHUB_REDIRECT_URL",
		"GITHUB_ALLOWED_USERS",
		"GITHUB_ALLOWED_ORG",
		"SESSION_SECRET",
		"PROBEWATCH_TOKEN_PEPPER",
		"AGENT_TOKEN_TTL",
		"AGENT_NODE_TOKEN_TTL",
		"AGENT_CLOCK_SKEW",
		"MAX_REQUEST_BODY",
		"AGENT_IGNORE_UNSAFE_CERT",
	} {
		t.Setenv(name, "")
	}
}

func setValidProductionEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("PROBEWATCH_ENV", "production")
	t.Setenv("PROBEWATCH_PUBLIC_BASE_URL", "https://monitor.example.test")
	t.Setenv("GITHUB_CLIENT_ID", "client-id")
	t.Setenv("GITHUB_CLIENT_SECRET", "client-secret")
	t.Setenv("GITHUB_REDIRECT_URL", "https://monitor.example.test/callback")
	t.Setenv("GITHUB_ALLOWED_USERS", "alice")
	t.Setenv("SESSION_SECRET", "12345678901234567890123456789012")
	t.Setenv("PROBEWATCH_TOKEN_PEPPER", "12345678901234567890123456789012")
	t.Setenv("AGENT_NODE_TOKEN_TTL", "8760h")
}
