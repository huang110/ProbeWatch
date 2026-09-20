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
)

var allowedTaskKinds = map[string]struct{}{
	"tcp":        {},
	"http":       {},
	"https":      {},
	"dns":        {},
	"mtr":        {},
	"media_http": {},
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
	ID      string         `json:"id"`
	Kind    string         `json:"kind"`
	Network *NetworkResult `json:"network,omitempty"`
	MTR     *MTRResult     `json:"mtr,omitempty"`
	Media   *MediaResult   `json:"media,omitempty"`
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

// ResourceSnapshot contains the bounded resource and identity data an agent reports.
type ResourceSnapshot struct {
	CPUPercent           float64 `json:"cpu_percent,omitempty"`
	Load1                float64 `json:"load1,omitempty"`
	Load5                float64 `json:"load5,omitempty"`
	Load15               float64 `json:"load15,omitempty"`
	MemoryTotalBytes     uint64  `json:"memory_total_bytes,omitempty"`
	MemoryUsedBytes      uint64  `json:"memory_used_bytes,omitempty"`
	MemoryAvailableBytes uint64  `json:"memory_available_bytes,omitempty"`
	SwapTotalBytes       uint64  `json:"swap_total_bytes,omitempty"`
	SwapUsedBytes        uint64  `json:"swap_used_bytes,omitempty"`
	FilesystemTotalBytes uint64  `json:"filesystem_total_bytes,omitempty"`
	FilesystemUsedBytes  uint64  `json:"filesystem_used_bytes,omitempty"`
	NetworkRxBytes       uint64  `json:"network_rx_bytes,omitempty"`
	NetworkTxBytes       uint64  `json:"network_tx_bytes,omitempty"`
	OS                   string  `json:"os,omitempty"`
	Kernel               string  `json:"kernel,omitempty"`
	Arch                 string  `json:"arch,omitempty"`
	Hostname             string  `json:"hostname,omitempty"`
	AgentVersion         string  `json:"agent_version,omitempty"`
	StartedAt            int64   `json:"started_at,omitempty"`
}

// CheckTask is the only task shape an agent accepts from the control plane.
type CheckTask struct {
	ID              string `json:"id"`
	Kind            string `json:"kind"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	Path            string `json:"path,omitempty"`
	ExpectedStatus  int    `json:"expected_status,omitempty"`
	DNSType         string `json:"dns_type,omitempty"`
	TimeoutMS       int    `json:"timeout_ms"`
	MaxHops         int    `json:"max_hops,omitempty"`
	IntervalSeconds int    `json:"interval_seconds"`
	Enabled         bool   `json:"enabled"`
}

// ErrorResponse is the stable JSON error envelope used by protocol endpoints.
type ErrorResponse struct {
	Error string `json:"error"`
}

// AgentConfigResponse is the strict wire shape for the agent's pulled check configuration.
type AgentConfigResponse struct {
	Tasks []CheckTask `json:"tasks"`
}

// Validate checks the decoded configuration before an agent accepts any task.
func (r AgentConfigResponse) Validate() error {
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

// NetworkResult is the normalized result of a TCP or HTTP check.
type NetworkResult struct {
	Host       string `json:"host"`
	Port       int    `json:"port,omitempty"`
	Status     string `json:"status,omitempty"`
	StatusCode int    `json:"status_code,omitempty"`
	LatencyMS  int64  `json:"latency_ms,omitempty"`
	Error      string `json:"error,omitempty"`
	CheckedAt  int64  `json:"checked_at,omitempty"`
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
	return nil
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
