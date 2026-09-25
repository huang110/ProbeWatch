package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Config contains all process configuration loaded at startup.
type Config struct {
	Environment            string
	ListenAddress          string
	AllowNonLoopbackListen bool
	DeploymentMode         string
	DatabasePath           string
	PublicBaseURL          string
	GitHubClientID         string
	GitHubClientSecret     string
	GitHubRedirectURL      string
	GitHubAllowedUsers     []string
	GitHubAllowedOrg       string
	SessionSecret          string
	TokenPepper            string
	AgentTokenTTL          time.Duration
	AgentNodeTokenTTL      time.Duration
	AgentClockSkew         time.Duration
	MaxRequestBody         int64
	AgentIgnoreUnsafeCert  bool
	AgentEndpoint          string
	AgentNodeUUID          string
	AgentNodeToken         string
	AgentRegistrationToken string
	AgentName              string
	AgentDataDir           string
	AdminPassword          string
	TelegramBotToken       string
	TelegramChatID         string
	WebhookURL             string
}

const DefaultDevelopmentTokenPepper = "development-only-probewatch-token-pepper"

const (
	defaultAgentNodeTokenTTL = 365 * 24 * time.Hour
	maxAgentNodeTokenTTL     = 2 * 365 * 24 * time.Hour
)

// Load reads and validates the process environment once at startup.
func Load() (Config, error) {
	lookup := os.LookupEnv
	environment := valueOrDefault(lookup, "PROBEWATCH_ENV", "development")
	publicBaseURL := value(lookup, "PROBEWATCH_PUBLIC_BASE_URL")
	if publicBaseURL == "" && environment == "development" {
		publicBaseURL = "http://127.0.0.1:8080"
	}

	cfg := Config{
		Environment:            environment,
		ListenAddress:          valueOrDefault(lookup, "PROBEWATCH_LISTEN", "127.0.0.1:8080"),
		DeploymentMode:         value(lookup, "PROBEWATCH_DEPLOYMENT_MODE"),
		DatabasePath:           valueOrDefault(lookup, "PROBEWATCH_DATABASE", "./data/probewatch.db"),
		PublicBaseURL:          publicBaseURL,
		GitHubClientID:         value(lookup, "GITHUB_CLIENT_ID"),
		GitHubClientSecret:     value(lookup, "GITHUB_CLIENT_SECRET"),
		GitHubRedirectURL:      value(lookup, "GITHUB_REDIRECT_URL"),
		GitHubAllowedUsers:     parseList(value(lookup, "GITHUB_ALLOWED_USERS")),
		GitHubAllowedOrg:       value(lookup, "GITHUB_ALLOWED_ORG"),
		SessionSecret:          value(lookup, "SESSION_SECRET"),
		TokenPepper:            value(lookup, "PROBEWATCH_TOKEN_PEPPER"),
		AgentTokenTTL:          15 * time.Minute,
		AgentNodeTokenTTL:      defaultAgentNodeTokenTTL,
		AgentClockSkew:         5 * time.Minute,
		MaxRequestBody:         1 << 20,
		AgentEndpoint:          value(lookup, "PROBEWATCH_AGENT_ENDPOINT"),
		AgentNodeUUID:          value(lookup, "PROBEWATCH_AGENT_NODE_UUID"),
		AgentNodeToken:         value(lookup, "PROBEWATCH_AGENT_NODE_TOKEN"),
		AgentRegistrationToken: value(lookup, "PROBEWATCH_AGENT_REGISTRATION_TOKEN"),
		AgentName:              valueOrDefault(lookup, "PROBEWATCH_AGENT_NAME", "ProbeWatch Agent"),
		AgentDataDir:           valueOrDefault(lookup, "PROBEWATCH_AGENT_DATA", "./data/agent"),
		AdminPassword:          value(lookup, "PROBEWATCH_ADMIN_PASSWORD"),
		TelegramBotToken:       value(lookup, "PROBEWATCH_TELEGRAM_BOT_TOKEN"),
		TelegramChatID:         value(lookup, "PROBEWATCH_TELEGRAM_CHAT_ID"),
		WebhookURL:             value(lookup, "PROBEWATCH_WEBHOOK_URL"),
	}

	var err error
	if cfg.TokenPepper == "" && cfg.Environment == "development" {
		cfg.TokenPepper = DefaultDevelopmentTokenPepper
	}
	cfg.AgentTokenTTL, err = durationValue(lookup, "AGENT_TOKEN_TTL", cfg.AgentTokenTTL)
	if err != nil {
		return Config{}, err
	}
	if environment == "production" && value(lookup, "AGENT_NODE_TOKEN_TTL") == "" {
		return Config{}, fmt.Errorf("AGENT_NODE_TOKEN_TTL is required in production")
	}
	cfg.AgentNodeTokenTTL, err = durationValue(lookup, "AGENT_NODE_TOKEN_TTL", cfg.AgentNodeTokenTTL)
	if err != nil {
		return Config{}, err
	}
	cfg.AgentClockSkew, err = durationValue(lookup, "AGENT_CLOCK_SKEW", cfg.AgentClockSkew)
	if err != nil {
		return Config{}, err
	}
	cfg.MaxRequestBody, err = byteSizeValue(lookup, "MAX_REQUEST_BODY", cfg.MaxRequestBody)
	if err != nil {
		return Config{}, err
	}
	cfg.AgentIgnoreUnsafeCert, err = boolValue(lookup, "AGENT_IGNORE_UNSAFE_CERT", false)
	if err != nil {
		return Config{}, err
	}
	cfg.AllowNonLoopbackListen, err = boolValue(lookup, "PROBEWATCH_ALLOW_NON_LOOPBACK_LISTEN", false)
	if err != nil {
		return Config{}, err
	}

	if cfg.Environment != "development" && cfg.Environment != "production" {
		return Config{}, fmt.Errorf("PROBEWATCH_ENV must be development or production, got %q", cfg.Environment)
	}
	if err := validateListenAddress(cfg.ListenAddress); err != nil {
		return Config{}, err
	}
	if cfg.AllowNonLoopbackListen && cfg.DeploymentMode != "container" && cfg.DeploymentMode != "trusted-proxy" {
		return Config{}, fmt.Errorf("PROBEWATCH_DEPLOYMENT_MODE must be container or trusted-proxy when PROBEWATCH_ALLOW_NON_LOOPBACK_LISTEN=true")
	}
	if !isLoopbackListen(cfg.ListenAddress) {
		if !cfg.AllowNonLoopbackListen {
			return Config{}, fmt.Errorf("PROBEWATCH_LISTEN must use a loopback address unless PROBEWATCH_ALLOW_NON_LOOPBACK_LISTEN=true")
		}
	}
	if cfg.AgentTokenTTL > 24*time.Hour {
		return Config{}, fmt.Errorf("AGENT_TOKEN_TTL must be no more than 24h")
	}
	if cfg.AgentNodeTokenTTL > maxAgentNodeTokenTTL {
		return Config{}, fmt.Errorf("AGENT_NODE_TOKEN_TTL must be no more than 2 years")
	}
	if cfg.AgentClockSkew > 15*time.Minute {
		return Config{}, fmt.Errorf("AGENT_CLOCK_SKEW must be no more than 15m")
	}
	if cfg.MaxRequestBody > 16<<20 {
		return Config{}, fmt.Errorf("MAX_REQUEST_BODY must be no more than 16MiB")
	}
	if cfg.Environment == "production" {
		if err := validateAgentEndpoint(cfg.AgentEndpoint, true); err != nil {
			return Config{}, err
		}
		if err := validateWebhookURL(cfg.WebhookURL, true); err != nil {
			return Config{}, err
		}
		publicURL, err := validateProductionHTTPSURL("PROBEWATCH_PUBLIC_BASE_URL", cfg.PublicBaseURL)
		if err != nil {
			return Config{}, err
		}
		hasGitHub := strings.TrimSpace(cfg.GitHubClientID) != "" && strings.TrimSpace(cfg.GitHubClientSecret) != ""
		hasPassword := strings.TrimSpace(cfg.AdminPassword) != ""
		if !hasGitHub && !hasPassword {
			return Config{}, fmt.Errorf("either PROBEWATCH_ADMIN_PASSWORD or (GITHUB_CLIENT_ID and GITHUB_CLIENT_SECRET) is required in production")
		}
		if hasPassword && utf8.RuneCountInString(cfg.AdminPassword) < 14 {
			return Config{}, fmt.Errorf("PROBEWATCH_ADMIN_PASSWORD must be at least 14 characters in production")
		}
		if hasGitHub {
			if strings.TrimSpace(cfg.GitHubRedirectURL) == "" {
				return Config{}, fmt.Errorf("GITHUB_REDIRECT_URL is required in production when using GitHub OAuth")
			}
			redirectURL, err := validateProductionHTTPSURL("GITHUB_REDIRECT_URL", cfg.GitHubRedirectURL)
			if err != nil {
				return Config{}, err
			}
			if urlOrigin(publicURL) != urlOrigin(redirectURL) {
				return Config{}, fmt.Errorf("GITHUB_REDIRECT_URL origin must match PROBEWATCH_PUBLIC_BASE_URL origin")
			}
			if len(cfg.GitHubAllowedUsers) == 0 && strings.TrimSpace(cfg.GitHubAllowedOrg) == "" {
				return Config{}, fmt.Errorf("GITHUB_ALLOWED_USERS or GITHUB_ALLOWED_ORG is required in production when using GitHub OAuth")
			}
		}
		if len([]byte(cfg.SessionSecret)) < 32 {
			return Config{}, fmt.Errorf("SESSION_SECRET must be at least 32 bytes in production")
		}
		if len([]byte(cfg.TokenPepper)) < 32 {
			return Config{}, fmt.Errorf("PROBEWATCH_TOKEN_PEPPER must be at least 32 bytes in production")
		}
		if cfg.AgentIgnoreUnsafeCert {
			return Config{}, fmt.Errorf("AGENT_IGNORE_UNSAFE_CERT must be false in production")
		}
	}

	return cfg, nil
}

func validateAgentEndpoint(raw string, production bool) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return fmt.Errorf("PROBEWATCH_AGENT_ENDPOINT must be an absolute URL without credentials, query, or fragment")
	}
	if production && !strings.EqualFold(parsed.Scheme, "https") {
		return fmt.Errorf("PROBEWATCH_AGENT_ENDPOINT must use HTTPS in production")
	}
	if !production && !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return fmt.Errorf("PROBEWATCH_AGENT_ENDPOINT must use HTTP or HTTPS")
	}
	return nil
}

func validateWebhookURL(raw string, production bool) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return fmt.Errorf("PROBEWATCH_WEBHOOK_URL must be an absolute URL without credentials, query, or fragment")
	}
	if production && !strings.EqualFold(parsed.Scheme, "https") {
		return fmt.Errorf("PROBEWATCH_WEBHOOK_URL must use HTTPS in production")
	}
	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return fmt.Errorf("PROBEWATCH_WEBHOOK_URL must use HTTP or HTTPS")
	}
	if production && isLocalWebhookHost(parsed.Hostname()) {
		return fmt.Errorf("PROBEWATCH_WEBHOOK_URL must not target a local or private address in production")
	}
	return nil
}

func isLocalWebhookHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "localhost" || host == "localhost.localdomain" || host == "ip6-localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast() {
			return true
		}
		if ip.Equal(net.ParseIP("169.254.169.254")) || ip.Equal(net.ParseIP("100.100.100.200")) {
			return true
		}
	}
	return false
}

func isLoopbackListen(raw string) bool {
	host, _, err := net.SplitHostPort(raw)
	if err != nil || host == "" {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validateListenAddress(raw string) error {
	host, port, err := net.SplitHostPort(raw)
	if err != nil || host == "" || port == "" {
		return fmt.Errorf("PROBEWATCH_LISTEN must be host:port with a numeric port from 1 to 65535")
	}
	for _, character := range port {
		if character < '0' || character > '9' {
			return fmt.Errorf("PROBEWATCH_LISTEN must use a numeric port from 1 to 65535")
		}
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("PROBEWATCH_LISTEN must use a port from 1 to 65535")
	}
	return nil
}

func validateProductionHTTPSURL(name, raw string) (*url.URL, error) {
	if raw == "" {
		return nil, fmt.Errorf("%s is required in production", name)
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.HasSuffix(raw, "?") || strings.HasSuffix(raw, "#") || strings.HasSuffix(parsed.Host, ":") {
		return nil, fmt.Errorf("%s must be an absolute HTTPS URL with a hostname and valid port, without userinfo, query, or fragment", name)
	}
	if port := parsed.Port(); port != "" {
		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber < 1 || portNumber > 65535 {
			return nil, fmt.Errorf("%s must use a port between 1 and 65535", name)
		}
	}
	return parsed, nil
}

func urlOrigin(parsed *url.URL) string {
	port := parsed.Port()
	if port == "" {
		port = "443"
	}
	return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Hostname()) + ":" + port
}

func value(lookup func(string) (string, bool), name string) string {
	value, _ := lookup(name)
	return strings.TrimSpace(value)
}

func valueOrDefault(lookup func(string) (string, bool), name, fallback string) string {
	value := value(lookup, name)
	if value == "" {
		return fallback
	}
	return value
}

func parseList(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			values = append(values, part)
		}
	}
	return values
}

func durationValue(lookup func(string) (string, bool), name string, fallback time.Duration) (time.Duration, error) {
	raw := value(lookup, name)
	if raw == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: must be a duration such as 15m: %w", name, raw, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("invalid %s %q: duration must be greater than zero", name, raw)
	}
	return parsed, nil
}

func byteSizeValue(lookup func(string) (string, bool), name string, fallback int64) (int64, error) {
	raw := value(lookup, name)
	if raw == "" {
		return fallback, nil
	}
	parsed, err := parseByteSize(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: must be a positive byte size such as 1MiB: %w", name, raw, err)
	}
	return parsed, nil
}

func parseByteSize(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	upper := strings.ToUpper(raw)
	units := []struct {
		suffix     string
		multiplier uint64
	}{
		{"GIB", 1 << 30},
		{"GB", 1e9},
		{"MIB", 1 << 20},
		{"MB", 1e6},
		{"KIB", 1 << 10},
		{"KB", 1e3},
		{"B", 1},
	}

	number := upper
	multiplier := uint64(1)
	for _, unit := range units {
		if strings.HasSuffix(upper, unit.suffix) {
			number = strings.TrimSpace(upper[:len(upper)-len(unit.suffix)])
			multiplier = unit.multiplier
			break
		}
	}
	if number == "" {
		return 0, fmt.Errorf("missing numeric value")
	}
	parsed, err := strconv.ParseUint(number, 10, 63)
	if err != nil {
		return 0, fmt.Errorf("invalid numeric value")
	}
	maxInt64 := uint64(^uint64(0) >> 1)
	if parsed == 0 || parsed > maxInt64/multiplier {
		return 0, fmt.Errorf("size must be a positive whole number of bytes")
	}
	return int64(parsed * multiplier), nil
}

func boolValue(lookup func(string) (string, bool), name string, fallback bool) (bool, error) {
	raw := value(lookup, name)
	if raw == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("invalid %s %q: must be true or false", name, raw)
	}
	return parsed, nil
}
