package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/probewatch/probewatch/internal/security"
)

const (
	maxIDLength          = 128
	maxNodeUUIDLength    = 128
	maxNameLength        = 128
	maxHostLength        = 253
	maxPathLength        = 2048
	maxKindLength        = 32
	maxDNSTypeLength     = 16
	maxTokenLength       = 512
	maxEndpointLength    = 2048
	maxVersionLength     = 128
	maxReasonLength      = 512
	maxFingerprintLength = 512

	maxRegionLength         = 16
	maxRegionContainsLength = 128
	maxRegionRules          = 16

	maxConfigMaxAgeSeconds = 86400
)

var allowedTaskKinds = map[string]struct{}{
	"tcp":        {},
	"http":       {},
	"https":      {},
	"dns":        {},
	"mtr":        {},
	"media_http": {},
	"speedtest":  {},
	"grpc":       {},
	"websocket":  {},
	"doh":        {},
	"synthetic":  {},
}

// ReportRequest is the complete resource and check report sent by an agent.
type ReportRequest struct {
	NodeUUID   string           `json:"node_uuid"`
	ReportedAt int64            `json:"reported_at"`
	Resource   ResourceSnapshot `json:"resource"`
	Results    []CheckResult    `json:"results"`
}

// CheckResult contains exactly one typed result for a configured check.
type CheckResult struct {
	ID        string           `json:"id"`
	Kind      string           `json:"kind"`
	Network   *NetworkResult   `json:"network,omitempty"`
	MTR       *MTRResult       `json:"mtr,omitempty"`
	Media     *MediaResult     `json:"media,omitempty"`
	Speedtest *SpeedtestResult `json:"speedtest,omitempty"`
	Synthetic *SyntheticResult `json:"synthetic,omitempty"`
}

// RegisterRequest contains the one-time registration credential and node identity.
type RegisterRequest struct {
	RegistrationToken string              `json:"registration_token"`
	NodeUUID          string              `json:"node_uuid"`
	Name              string              `json:"name"`
	System            *RegistrationSystem `json:"system"`
}

// RegistrationSystem is the bounded identity-only system information sent at registration.
type RegistrationSystem struct {
	OS           string `json:"os,omitempty"`
	Arch         string `json:"arch,omitempty"`
	Hostname     string `json:"hostname,omitempty"`
	AgentVersion string `json:"agent_version,omitempty"`
}

// RegisterResponse contains the node credential returned after registration.
type RegisterResponse struct {
	NodeUUID  string `json:"node_uuid"`
	NodeToken string `json:"node_token"`
	Endpoint  string `json:"endpoint,omitempty"`
}

// NetworkResultEnvelope is the strict wire shape for a standalone network result.
type NetworkResultEnvelope struct {
	TargetID string        `json:"target_id"`
	Result   NetworkResult `json:"result"`
}

// MTRResultEnvelope is the strict wire shape for a standalone MTR result.
type MTRResultEnvelope struct {
	TargetID string    `json:"target_id"`
	Result   MTRResult `json:"result"`
}

// MediaResultEnvelope is the strict wire shape for a standalone media result.
type MediaResultEnvelope struct {
	DetectorID string      `json:"detector_id"`
	Result     MediaResult `json:"result"`
}

// SpeedtestResultEnvelope is the strict wire shape for a standalone speedtest result.
type SpeedtestResultEnvelope struct {
	TaskID string          `json:"task_id"`
	Result SpeedtestResult `json:"result"`
}

// SyntheticResultEnvelope is the strict wire shape for a standalone synthetic check result.
type SyntheticResultEnvelope struct {
	TargetID string          `json:"target_id"`
	Result   SyntheticResult `json:"result"`
}

// InterfaceStat represents one network interface's traffic counters and addresses.
type InterfaceStat struct {
	Name      string `json:"name"`
	RxBytes   uint64 `json:"rx_bytes"`
	TxBytes   uint64 `json:"tx_bytes"`
	RxPackets uint64 `json:"rx_packets,omitempty"`
	TxPackets uint64 `json:"tx_packets,omitempty"`
	RxErrors  uint64 `json:"rx_errors,omitempty"`
	TxErrors  uint64 `json:"tx_errors,omitempty"`
	IPv4      string `json:"ipv4,omitempty"`
	IPv6      string `json:"ipv6,omitempty"`
}

// ResourceSnapshot contains the bounded resource and identity data an agent reports.
type ResourceSnapshot struct {
	CPUPercent           float64         `json:"cpu_percent,omitempty"`
	Load1                float64         `json:"load1,omitempty"`
	Load5                float64         `json:"load5,omitempty"`
	Load15               float64         `json:"load15,omitempty"`
	MemoryTotalBytes     uint64          `json:"memory_total_bytes,omitempty"`
	MemoryUsedBytes      uint64          `json:"memory_used_bytes,omitempty"`
	MemoryAvailableBytes uint64          `json:"memory_available_bytes,omitempty"`
	SwapTotalBytes       uint64          `json:"swap_total_bytes,omitempty"`
	SwapUsedBytes        uint64          `json:"swap_used_bytes,omitempty"`
	FilesystemTotalBytes uint64          `json:"filesystem_total_bytes,omitempty"`
	FilesystemUsedBytes  uint64          `json:"filesystem_used_bytes,omitempty"`
	NetworkRxBytes       uint64          `json:"network_rx_bytes,omitempty"`
	NetworkTxBytes       uint64          `json:"network_tx_bytes,omitempty"`
	TCPConnCount         uint64          `json:"tcp_conn_count,omitempty"`
	UDPConnCount         uint64          `json:"udp_conn_count,omitempty"`
	ProcessCount         uint64          `json:"process_count,omitempty"`
	OS                   string          `json:"os,omitempty"`
	Kernel               string          `json:"kernel,omitempty"`
	Arch                 string          `json:"arch,omitempty"`
	Hostname             string          `json:"hostname,omitempty"`
	AgentVersion         string          `json:"agent_version,omitempty"`
	StartedAt            int64           `json:"started_at,omitempty"`
	Interfaces           []InterfaceStat `json:"interfaces,omitempty"`
	IPv4                 string          `json:"ipv4,omitempty"`
	IPv6                 string          `json:"ipv6,omitempty"`
}

// CheckTask is the only task shape an agent accepts from the control plane.
type CheckTask struct {
	ID              string       `json:"id"`
	Kind            string       `json:"kind"`
	Host            string       `json:"host"`
	Port            int          `json:"port"`
	Path            string       `json:"path,omitempty"`
	ExpectedStatus  int          `json:"expected_status,omitempty"`
	DNSType         string       `json:"dns_type,omitempty"`
	TimeoutMS       int          `json:"timeout_ms"`
	MaxHops         int          `json:"max_hops,omitempty"`
	IntervalSeconds int          `json:"interval_seconds"`
	Enabled         bool         `json:"enabled"`
	RegionRules     []RegionRule `json:"region_rules,omitempty"`
	Keyword         string       `json:"keyword,omitempty"`
	Nameserver      string       `json:"nameserver,omitempty"`
	CheckTLS        bool         `json:"check_tls,omitempty"`
	DownloadBytes   int64                    `json:"download_bytes,omitempty"`
	UploadBytes     int64                    `json:"upload_bytes,omitempty"`
	ServerURL       string                   `json:"server_url,omitempty"`
	Method          string                   `json:"method,omitempty"`
	Headers         map[string]string        `json:"headers,omitempty"`
	BodyPayload     string                   `json:"body_payload,omitempty"`
	Assertions      []SyntheticAssertionRule `json:"assertions,omitempty"`
	GRPCService     string                   `json:"grpc_service,omitempty"`
}

// RegionRule is one bounded body-marker rule for classifying the serving
// region of a media_http endpoint. The agent matches Contains against a
// bounded prefix of the response body it already read; nothing else about
// the body is interpreted, stored, or reported.
type RegionRule struct {
	Region   string `json:"region"`
	Contains string `json:"contains"`
}

// Validate enforces the strict bounds on one region rule: region is at most
// 16 characters from a fixed allowlist, contains is 1..128 bytes with no
// control characters.
func (r RegionRule) Validate() error {
	if err := validateString("region", r.Region, maxRegionLength, true); err != nil {
		return err
	}
	for _, character := range r.Region {
		switch {
		case character >= 'a' && character <= 'z':
		case character >= 'A' && character <= 'Z':
		case character >= '0' && character <= '9':
		case character == '-' || character == '_':
		default:
			return errors.New("region must contain only letters, digits, '-', and '_'")
		}
	}
	if err := validateString("contains", r.Contains, maxRegionContainsLength, true); err != nil {
		return err
	}
	for _, character := range r.Contains {
		if character < 0x20 || character == 0x7f {
			return errors.New("contains must not contain control characters")
		}
	}
	return nil
}

// ValidateRegionRules validates an optional rule list. Rules are only
// meaningful for media_http tasks; any other kind carrying rules is rejected.
func ValidateRegionRules(kind string, rules []RegionRule) error {
	if len(rules) == 0 {
		return nil
	}
	if kind != "media_http" {
		return errors.New("region_rules are only allowed for media_http tasks")
	}
	if len(rules) > maxRegionRules {
		return fmt.Errorf("region_rules exceeds maximum length of %d", maxRegionRules)
	}
	for index, rule := range rules {
		if err := rule.Validate(); err != nil {
			return fmt.Errorf("region_rules[%d]: %w", index, err)
		}
	}
	return nil
}

// ErrorResponse is the stable JSON error envelope used by protocol endpoints.
type ErrorResponse struct {
	Error string `json:"error"`
}

// AgentConfigResponse is the strict wire shape for the agent's pulled check configuration.
// ConfigVersion and ConfigMaxAgeSeconds are optional: a legacy control plane omits both,
// and an agent then keeps its existing behavior without fail-closed staleness checks.
type AgentConfigResponse struct {
	Tasks               []CheckTask `json:"tasks"`
	ConfigVersion       int64       `json:"config_version,omitempty"`
	ConfigMaxAgeSeconds int         `json:"config_max_age_seconds,omitempty"`
}

// Validate checks the decoded configuration before an agent accepts any task.
func (r AgentConfigResponse) Validate() error {
	if r.ConfigVersion < 0 {
		return errors.New("config_version must not be negative")
	}
	if r.ConfigMaxAgeSeconds < 0 || r.ConfigMaxAgeSeconds > maxConfigMaxAgeSeconds {
		return fmt.Errorf("config_max_age_seconds must be between 1 and %d", maxConfigMaxAgeSeconds)
	}
	if len(r.Tasks) > 1000 {
		return errors.New("tasks exceeds maximum length of 1000")
	}
	seen := make(map[string]struct{}, len(r.Tasks))
	for index, task := range r.Tasks {
		if err := task.Validate(); err != nil {
			return fmt.Errorf("tasks[%d]: %w", index, err)
		}
		if _, duplicate := seen[task.ID]; duplicate {
			return fmt.Errorf("tasks[%d]: duplicate task id %q", index, task.ID)
		}
		seen[task.ID] = struct{}{}
	}
	return nil
}

// TLSCertResult contains parsed SSL/TLS certificate metadata and expiration tracking.
type TLSCertResult struct {
	Issuer       string   `json:"issuer,omitempty"`
	Subject      string   `json:"subject,omitempty"`
	DNSNames     []string `json:"dns_names,omitempty"`
	NotBefore    int64    `json:"not_before,omitempty"`
	NotAfter     int64    `json:"not_after,omitempty"`
	DaysLeft     int      `json:"days_left"`
	Protocol     string   `json:"protocol,omitempty"`
	CipherSuite  string   `json:"cipher_suite,omitempty"`
	IsExpired    bool     `json:"is_expired"`
	ExpiringSoon bool     `json:"expiring_soon"`
}

// DNSResult contains resolved record addresses and query timing.
type DNSResult struct {
	Records     []string `json:"records,omitempty"`
	Nameserver  string   `json:"nameserver,omitempty"`
	QueryTimeMS int64    `json:"query_time_ms,omitempty"`
}

// HTTPResult contains HTTP body matching and content details.
type HTTPResult struct {
	KeywordFound  bool   `json:"keyword_found,omitempty"`
	ResponseBytes int    `json:"response_bytes,omitempty"`
	ContentType   string `json:"content_type,omitempty"`
}

// NetworkResult is the normalized result of a TCP or HTTP check.
type NetworkResult struct {
	Host       string         `json:"host"`
	Port       int            `json:"port,omitempty"`
	Status     string         `json:"status,omitempty"`
	StatusCode int            `json:"status_code,omitempty"`
	LatencyMS  int64          `json:"latency_ms,omitempty"`
	Error      string         `json:"error,omitempty"`
	CheckedAt  int64          `json:"checked_at,omitempty"`
	TLSCert    *TLSCertResult `json:"tls_cert,omitempty"`
	DNS        *DNSResult     `json:"dns,omitempty"`
	HTTP       *HTTPResult    `json:"http,omitempty"`
}

// MTRResult is the normalized result of a bounded route trace.
type MTRResult struct {
	Host          string   `json:"host"`
	DestinationIP string   `json:"destination_ip,omitempty"`
	Hops          []MTRHop `json:"hops,omitempty"`
	Reached       bool     `json:"reached"`
	Fingerprint   string   `json:"fingerprint,omitempty"`
	Error         string   `json:"error,omitempty"`
	CheckedAt     int64    `json:"checked_at,omitempty"`
}

// MTRHop is one bounded hop in an MTR result.
type MTRHop struct {
	TTL       int    `json:"ttl"`
	IP        string `json:"ip,omitempty"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
	TimedOut  bool   `json:"timed_out,omitempty"`
}

// MediaResult is the normalized result of a structured media HTTP check.
type MediaResult struct {
	Detector  string `json:"detector"`
	Status    string `json:"status"`
	Region    string `json:"region,omitempty"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
	Reason    string `json:"reason,omitempty"`
	CheckedAt int64  `json:"checked_at,omitempty"`
}

// SpeedtestResult contains bandwidth, latency, and duration metrics from a benchmark probe.
type SpeedtestResult struct {
	ServerName        string  `json:"server_name,omitempty"`
	ServerURL         string  `json:"server_url,omitempty"`
	DownloadSpeedMbps float64 `json:"download_speed_mbps"`
	UploadSpeedMbps   float64 `json:"upload_speed_mbps"`
	LatencyMS         int64   `json:"latency_ms,omitempty"`
	JitterMS          int64   `json:"jitter_ms,omitempty"`
	BytesReceived     int64   `json:"bytes_received,omitempty"`
	BytesSent         int64   `json:"bytes_sent,omitempty"`
	DurationMS        int64   `json:"duration_ms,omitempty"`
	Status            string  `json:"status,omitempty"`
	Error             string  `json:"error,omitempty"`
	TestedAt          int64   `json:"tested_at,omitempty"`
}

// SyntheticAssertionRule defines one declarative SLA / contract check rule.
type SyntheticAssertionRule struct {
	Source   string `json:"source"`             // status_code, header, body_regex, jsonpath, max_latency_ms, cert_days_left
	Property string `json:"property,omitempty"` // header key or jsonpath expression
	Operator string `json:"operator"`           // equals, not_equals, contains, not_contains, regex_match, less_than, greater_than
	Target   string `json:"target"`             // expected value or threshold string
}

func (r SyntheticAssertionRule) Validate() error {
	switch r.Source {
	case "status_code", "header", "body_regex", "jsonpath", "max_latency_ms", "cert_days_left":
	default:
		return fmt.Errorf("invalid assertion source %q", r.Source)
	}
	switch r.Operator {
	case "equals", "not_equals", "contains", "not_contains", "regex_match", "less_than", "greater_than":
	default:
		return fmt.Errorf("invalid assertion operator %q", r.Operator)
	}
	if err := validateString("property", r.Property, 256, false); err != nil {
		return err
	}
	if err := validateString("target", r.Target, 1024, false); err != nil {
		return err
	}
	return nil
}

// SyntheticTiming contains the multi-phase network connection breakdown.
type SyntheticTiming struct {
	DNSLookupMS     int64 `json:"dns_lookup_ms"`
	TCPConnectMS    int64 `json:"tcp_connect_ms"`
	TLSHandshakeMS  int64 `json:"tls_handshake_ms"`
	TTFBMS          int64 `json:"ttfb_ms"`
	TransferMS      int64 `json:"transfer_ms"`
	TotalDurationMS int64 `json:"total_duration_ms"`
}

// SyntheticResult contains outcome, timing waterfall, and assertion status for a synthetic probe.
type SyntheticResult struct {
	Protocol        string          `json:"protocol"`
	TargetURL       string          `json:"target_url"`
	Timing          SyntheticTiming `json:"timing"`
	StatusCode      int             `json:"status_code,omitempty"`
	GRPCStatus      string          `json:"grpc_status,omitempty"`
	WebSocketEcho   bool            `json:"websocket_echo,omitempty"`
	DNSAnswers      []string        `json:"dns_answers,omitempty"`
	Passed          bool            `json:"passed"`
	FailedAssertion string          `json:"failed_assertion,omitempty"`
	Status          string          `json:"status"`
	Error           string          `json:"error,omitempty"`
	CheckedAt       int64           `json:"checked_at"`
}

func (r SyntheticResult) Validate() error {
	if err := validateString("protocol", r.Protocol, maxKindLength, true); err != nil {
		return err
	}
	if err := validateString("target_url", r.TargetURL, maxEndpointLength, false); err != nil {
		return err
	}
	if err := validateString("status", r.Status, maxKindLength, false); err != nil {
		return err
	}
	if err := validateString("error", r.Error, maxReasonLength, false); err != nil {
		return err
	}
	if err := validateString("failed_assertion", r.FailedAssertion, maxReasonLength, false); err != nil {
		return err
	}
	if r.CheckedAt <= 0 {
		return errors.New("checked_at must be greater than zero")
	}
	return nil
}

func (r ReportRequest) Validate() error {
	if err := validateUUID("node_uuid", r.NodeUUID); err != nil {
		return err
	}
	if r.ReportedAt <= 0 {
		return errors.New("reported_at must be greater than zero")
	}
	if err := r.Resource.Validate(); err != nil {
		return fmt.Errorf("resource: %w", err)
	}
	if len(r.Results) > 1000 {
		return errors.New("results exceeds maximum length of 1000")
	}
	for index, result := range r.Results {
		if err := result.Validate(); err != nil {
			return fmt.Errorf("results[%d]: %w", index, err)
		}
	}
	return nil
}

func (r RegisterRequest) Validate() error {
	if err := validateString("registration_token", r.RegistrationToken, maxTokenLength, true); err != nil {
		return err
	}
	if err := validateUUID("node_uuid", r.NodeUUID); err != nil {
		return err
	}
	if err := validateString("name", r.Name, maxNameLength, true); err != nil {
		return err
	}
	if r.System == nil {
		return errors.New("system is required")
	}
	return r.System.Validate()
}

func (r RegistrationSystem) Validate() error {
	for _, field := range []struct {
		name  string
		value string
		limit int
	}{
		{name: "os", value: r.OS, limit: maxVersionLength},
		{name: "arch", value: r.Arch, limit: maxVersionLength},
		{name: "hostname", value: r.Hostname, limit: maxHostLength},
		{name: "agent_version", value: r.AgentVersion, limit: maxVersionLength},
	} {
		if err := validateString(field.name, field.value, field.limit, true); err != nil {
			return err
		}
	}
	return nil
}

func (r RegisterResponse) Validate() error {
	if err := validateUUID("node_uuid", r.NodeUUID); err != nil {
		return err
	}
	if err := validateString("node_token", r.NodeToken, maxTokenLength, true); err != nil {
		return err
	}
	return validateString("endpoint", r.Endpoint, maxEndpointLength, false)
}

func (r NetworkResultEnvelope) Validate() error {
	if err := validateString("target_id", r.TargetID, maxIDLength, true); err != nil {
		return err
	}
	return r.Result.Validate()
}

func (r MTRResultEnvelope) Validate() error {
	if err := validateString("target_id", r.TargetID, maxIDLength, true); err != nil {
		return err
	}
	return r.Result.Validate()
}

func (r MediaResultEnvelope) Validate() error {
	if err := validateString("detector_id", r.DetectorID, maxIDLength, true); err != nil {
		return err
	}
	return r.Result.Validate()
}

func (r SpeedtestResultEnvelope) Validate() error {
	if err := validateString("task_id", r.TaskID, maxIDLength, true); err != nil {
		return err
	}
	return r.Result.Validate()
}

func (r SyntheticResultEnvelope) Validate() error {
	if err := validateString("target_id", r.TargetID, maxIDLength, true); err != nil {
		return err
	}
	return r.Result.Validate()
}

func (r ResourceSnapshot) Validate() error {
	for _, field := range []struct {
		name  string
		value string
		limit int
	}{
		{name: "os", value: r.OS, limit: maxVersionLength},
		{name: "kernel", value: r.Kernel, limit: maxVersionLength},
		{name: "arch", value: r.Arch, limit: maxVersionLength},
		{name: "hostname", value: r.Hostname, limit: maxHostLength},
		{name: "agent_version", value: r.AgentVersion, limit: maxVersionLength},
	} {
		if err := validateString(field.name, field.value, field.limit, false); err != nil {
			return err
		}
	}
	if r.CPUPercent < 0 || r.CPUPercent > 100 {
		return errors.New("cpu_percent must be between 0 and 100")
	}
	if r.Load1 < 0 || r.Load5 < 0 || r.Load15 < 0 {
		return errors.New("load values must not be negative")
	}
	if r.StartedAt < 0 {
		return errors.New("started_at must not be negative")
	}
	if err := validateString("ipv4", r.IPv4, maxHostLength, false); err != nil {
		return err
	}
	if err := validateString("ipv6", r.IPv6, maxHostLength, false); err != nil {
		return err
	}
	if len(r.Interfaces) > 64 {
		return errors.New("interfaces count exceeds 64")
	}
	for i, iface := range r.Interfaces {
		if err := validateString(fmt.Sprintf("interfaces[%d].name", i), iface.Name, 32, true); err != nil {
			return err
		}
		if err := validateString(fmt.Sprintf("interfaces[%d].ipv4", i), iface.IPv4, maxHostLength, false); err != nil {
			return err
		}
		if err := validateString(fmt.Sprintf("interfaces[%d].ipv6", i), iface.IPv6, maxHostLength, false); err != nil {
			return err
		}
	}
	return nil
}

func (t CheckTask) Validate() error {
	if err := validateString("id", t.ID, maxIDLength, true); err != nil {
		return err
	}
	if err := validateString("kind", t.Kind, maxKindLength, true); err != nil {
		return err
	}
	if _, ok := allowedTaskKinds[t.Kind]; !ok {
		return fmt.Errorf("kind %q is not allowed", t.Kind)
	}
	if err := validateString("host", t.Host, maxHostLength, true); err != nil {
		return err
	}
	if t.Port < 1 || t.Port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	if t.TimeoutMS < 100 || t.TimeoutMS > 30000 {
		return errors.New("timeout_ms must be between 100 and 30000")
	}
	if t.IntervalSeconds < 10 || t.IntervalSeconds > 86400 {
		return errors.New("interval_seconds must be between 10 and 86400")
	}
	if t.MaxHops < 1 || t.MaxHops > 30 {
		return errors.New("max_hops must be between 1 and 30")
	}
	if err := validateString("path", t.Path, maxPathLength, false); err != nil {
		return err
	}
	if t.Path != "" && !strings.HasPrefix(t.Path, "/") {
		return errors.New("path must start with /")
	}
	if t.ExpectedStatus != 0 && (t.ExpectedStatus < 100 || t.ExpectedStatus > 599) {
		return errors.New("expected_status must be between 100 and 599")
	}
	if err := validateString("dns_type", t.DNSType, maxDNSTypeLength, false); err != nil {
		return err
	}
	if t.Kind == "dns" && t.DNSType != "" && t.DNSType != "A" && t.DNSType != "AAAA" && t.DNSType != "CNAME" {
		return errors.New("dns_type must be A, AAAA, or CNAME")
	}
	if t.Kind == "http" || t.Kind == "https" || t.Kind == "media_http" {
		if t.Path == "" {
			return errors.New("path is required for HTTP tasks")
		}
	}
	if t.DownloadBytes < 0 || t.DownloadBytes > 500*1024*1024 {
		return errors.New("download_bytes must be between 0 and 500MB")
	}
	if t.UploadBytes < 0 || t.UploadBytes > 200*1024*1024 {
		return errors.New("upload_bytes must be between 0 and 200MB")
	}
	if err := validateString("server_url", t.ServerURL, maxEndpointLength, false); err != nil {
		return err
	}
	if len(t.Assertions) > 16 {
		return errors.New("assertions count exceeds 16")
	}
	for i, a := range t.Assertions {
		if err := a.Validate(); err != nil {
			return fmt.Errorf("assertions[%d]: %w", i, err)
		}
	}
	if len(t.Headers) > 32 {
		return errors.New("headers count exceeds 32")
	}
	for k, v := range t.Headers {
		if err := validateString("header key", k, 128, true); err != nil {
			return err
		}
		if err := validateString("header value", v, 1024, false); err != nil {
			return err
		}
	}
	if len([]byte(t.BodyPayload)) > 65536 {
		return errors.New("body_payload exceeds 64KB")
	}
	if err := validateString("method", t.Method, 16, false); err != nil {
		return err
	}
	if err := validateString("grpc_service", t.GRPCService, 256, false); err != nil {
		return err
	}
	return ValidateRegionRules(t.Kind, t.RegionRules)
}

func (r CheckResult) Validate() error {
	if err := validateString("id", r.ID, maxIDLength, true); err != nil {
		return err
	}
	if err := validateString("kind", r.Kind, maxKindLength, true); err != nil {
		return err
	}
	if _, ok := allowedTaskKinds[r.Kind]; !ok {
		return fmt.Errorf("kind %q is not allowed", r.Kind)
	}
	resultCount := 0
	if r.Network != nil {
		resultCount++
		if err := r.Network.Validate(); err != nil {
			return fmt.Errorf("network: %w", err)
		}
	}
	if r.MTR != nil {
		resultCount++
		if err := r.MTR.Validate(); err != nil {
			return fmt.Errorf("mtr: %w", err)
		}
	}
	if r.Media != nil {
		resultCount++
		if err := r.Media.Validate(); err != nil {
			return fmt.Errorf("media: %w", err)
		}
	}
	if r.Speedtest != nil {
		resultCount++
		if err := r.Speedtest.Validate(); err != nil {
			return fmt.Errorf("speedtest: %w", err)
		}
	}
	if r.Synthetic != nil {
		resultCount++
		if err := r.Synthetic.Validate(); err != nil {
			return fmt.Errorf("synthetic: %w", err)
		}
	}
	if resultCount != 1 {
		return errors.New("check result must contain exactly one typed result")
	}
	switch r.Kind {
	case "tcp", "http", "https", "dns":
		if r.Network == nil {
			return fmt.Errorf("kind %q requires a network result", r.Kind)
		}
	case "mtr":
		if r.MTR == nil {
			return errors.New("kind mtr requires an mtr result")
		}
	case "media_http":
		if r.Media == nil {
			return errors.New("kind media_http requires a media result")
		}
	case "speedtest":
		if r.Speedtest == nil {
			return errors.New("kind speedtest requires a speedtest result")
		}
	case "grpc", "websocket", "doh", "synthetic":
		if r.Synthetic == nil {
			return fmt.Errorf("kind %s requires a synthetic result", r.Kind)
		}
	}
	return nil
}

func (r NetworkResult) Validate() error {
	if err := validateString("host", r.Host, maxHostLength, true); err != nil {
		return err
	}
	if r.Port < 1 || r.Port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	if err := validateString("status", r.Status, maxKindLength, false); err != nil {
		return err
	}
	if err := validateString("error", r.Error, maxReasonLength, false); err != nil {
		return err
	}
	if r.CheckedAt <= 0 {
		return errors.New("checked_at must be greater than zero")
	}
	return nil
}

func (r MTRResult) Validate() error {
	if err := validateString("host", r.Host, maxHostLength, true); err != nil {
		return err
	}
	if err := validateString("destination_ip", r.DestinationIP, maxHostLength, false); err != nil {
		return err
	}
	if err := validateString("fingerprint", r.Fingerprint, maxFingerprintLength, false); err != nil {
		return err
	}
	if err := validateString("error", r.Error, maxReasonLength, false); err != nil {
		return err
	}
	if r.CheckedAt <= 0 {
		return errors.New("checked_at must be greater than zero")
	}
	if len(r.Hops) > 30 {
		return errors.New("hops exceeds maximum length of 30")
	}
	for index, hop := range r.Hops {
		if hop.TTL < 1 || hop.TTL > 30 {
			return fmt.Errorf("hops[%d].ttl must be between 1 and 30", index)
		}
		if err := validateString(fmt.Sprintf("hops[%d].ip", index), hop.IP, maxHostLength, false); err != nil {
			return err
		}
	}
	return nil
}

func (r MediaResult) Validate() error {
	if err := validateString("detector", r.Detector, maxIDLength, true); err != nil {
		return err
	}
	if err := validateString("status", r.Status, maxKindLength, true); err != nil {
		return err
	}
	if err := validateString("region", r.Region, maxKindLength, false); err != nil {
		return err
	}
	if err := validateString("reason", r.Reason, maxReasonLength, false); err != nil {
		return err
	}
	if r.CheckedAt <= 0 {
		return errors.New("checked_at must be greater than zero")
	}
	return nil
}

func (r SpeedtestResult) Validate() error {
	if err := validateString("server_name", r.ServerName, maxNameLength, false); err != nil {
		return err
	}
	if err := validateString("server_url", r.ServerURL, maxEndpointLength, false); err != nil {
		return err
	}
	if r.DownloadSpeedMbps < 0 || r.DownloadSpeedMbps > 1000000 {
		return errors.New("download_speed_mbps must be between 0 and 1000000")
	}
	if r.UploadSpeedMbps < 0 || r.UploadSpeedMbps > 1000000 {
		return errors.New("upload_speed_mbps must be between 0 and 1000000")
	}
	if r.LatencyMS < 0 || r.LatencyMS > 60000 {
		return errors.New("latency_ms must be between 0 and 60000")
	}
	if r.JitterMS < 0 || r.JitterMS > 60000 {
		return errors.New("jitter_ms must be between 0 and 60000")
	}
	if r.BytesReceived < 0 {
		return errors.New("bytes_received must not be negative")
	}
	if r.BytesSent < 0 {
		return errors.New("bytes_sent must not be negative")
	}
	if r.DurationMS < 0 {
		return errors.New("duration_ms must not be negative")
	}
	if err := validateString("status", r.Status, maxKindLength, false); err != nil {
		return err
	}
	if err := validateString("error", r.Error, maxReasonLength, false); err != nil {
		return err
	}
	if r.TestedAt <= 0 {
		return errors.New("tested_at must be greater than zero")
	}
	return nil
}

// DecodeJSON decodes one bounded JSON value with no unknown fields or trailing values.
func DecodeJSON(input io.Reader, maxBytes int64, destination any) error {
	if input == nil {
		return errors.New("JSON input is nil")
	}
	if maxBytes <= 0 {
		return errors.New("maxBytes must be greater than zero")
	}

	limited := &countingReader{reader: io.LimitReader(input, maxBytes+1)}
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("decode JSON: trailing JSON value")
		}
		return fmt.Errorf("decode JSON: trailing data: %w", err)
	}
	if limited.read > maxBytes {
		return fmt.Errorf("decode JSON: body exceeds %d bytes", maxBytes)
	}
	return nil
}

// DecodeJSONObject decodes one bounded JSON object and rejects null, arrays, and scalar values.
func DecodeJSONObject(input io.Reader, maxBytes int64, destination any) error {
	var raw json.RawMessage
	if err := DecodeJSON(input, maxBytes, &raw); err != nil {
		return err
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return errors.New("decode JSON: top-level value must be an object")
	}
	return DecodeJSON(bytes.NewReader(trimmed), maxBytes, destination)
}

func validateString(name, value string, maxLength int, required bool) error {
	if required && strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", name)
	}
	if len([]byte(value)) > maxLength {
		return fmt.Errorf("%s exceeds maximum length of %d bytes", name, maxLength)
	}
	return nil
}

func validateUUID(name, value string) error {
	if !security.IsRFC4122UUID(value) {
		return fmt.Errorf("%s must be an RFC 4122 UUID", name)
	}
	return nil
}

type countingReader struct {
	reader io.Reader
	read   int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.read += int64(n)
	return n, err
}
