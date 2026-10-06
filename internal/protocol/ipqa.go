package protocol

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// IPQAInfo holds sanitized, bounded IP-Quality-Archive telemetry from an agent.
// Raw alerts.log content, node filesystem paths, and plain public IPs are strictly excluded.
type IPQAInfo struct {
	Enabled           bool            `json:"enabled,omitempty"`
	Installed         bool            `json:"installed,omitempty"`
	LastCheckedAt     int64           `json:"last_checked_at,omitempty"`
	LatestArchiveDate string          `json:"latest_archive_date,omitempty"`
	HighestSeverity   string          `json:"highest_severity,omitempty"`
	AlertCount        int             `json:"alert_count,omitempty"`
	CriticalCount     int             `json:"critical_count,omitempty"`
	WarningCount      int             `json:"warning_count,omitempty"`
	InfoCount         int             `json:"info_count,omitempty"`
	IPv4              *IPQAFamilyInfo `json:"ipv4,omitempty"`
	IPv6              *IPQAFamilyInfo `json:"ipv6,omitempty"`
	Changes           []IPQAChange    `json:"changes,omitempty"`
	Sources           []string        `json:"sources,omitempty"`
	CollectionError   string          `json:"collection_error,omitempty"`
}

// IPQAFamilyInfo contains provider intelligence for a specific IP family (IPv4 / IPv6).
type IPQAFamilyInfo struct {
	RiskScore    *float64           `json:"risk_score,omitempty"`
	RiskLevel    string             `json:"risk_level,omitempty"`
	IPType       string             `json:"ip_type,omitempty"`
	Country      string             `json:"country,omitempty"`
	Region       string             `json:"region,omitempty"`
	ASN          string             `json:"asn,omitempty"`
	Organization string             `json:"organization,omitempty"`
	Proxy        *bool              `json:"proxy,omitempty"`
	VPN          *bool              `json:"vpn,omitempty"`
	Tor          *bool              `json:"tor,omitempty"`
	Abuse        *bool              `json:"abuse,omitempty"`
	SourceScores map[string]float64 `json:"source_scores,omitempty"`
}

// IPQAChange describes a semantic difference detected between consecutive archives.
type IPQAChange struct {
	Category  string `json:"category,omitempty"`
	Severity  string `json:"severity,omitempty"`
	Before    string `json:"before,omitempty"`
	After     string `json:"after,omitempty"`
	ChangedAt int64  `json:"changed_at,omitempty"`
}

// Validate checks boundaries, lengths, and acceptable values for IPQAInfo.
func (q IPQAInfo) Validate() error {
	if q.LastCheckedAt < 0 {
		return errors.New("last_checked_at must not be negative")
	}
	if q.LastCheckedAt > 0 {
		maxFuture := time.Now().UTC().Add(1 * time.Hour).Unix()
		if q.LastCheckedAt > maxFuture {
			return errors.New("last_checked_at is in the future")
		}
	}
	if err := validateString("latest_archive_date", q.LatestArchiveDate, 32, false); err != nil {
		return err
	}
	if q.HighestSeverity != "" {
		switch strings.ToUpper(q.HighestSeverity) {
		case "INFO", "WARNING", "CRITICAL", "NONE":
		default:
			return fmt.Errorf("highest_severity %q is invalid", q.HighestSeverity)
		}
	}
	if q.AlertCount < 0 || q.CriticalCount < 0 || q.WarningCount < 0 || q.InfoCount < 0 {
		return errors.New("alert counts must not be negative")
	}
	if q.AlertCount > 100000 {
		return errors.New("alert_count exceeds maximum allowed limit")
	}
	if err := validateString("collection_error", q.CollectionError, 256, false); err != nil {
		return err
	}
	if len(q.Sources) > 32 {
		return errors.New("sources count exceeds 32")
	}
	for _, s := range q.Sources {
		if err := validateString("source name", s, 64, true); err != nil {
			return err
		}
	}
	if len(q.Changes) > 50 {
		return errors.New("changes count exceeds 50")
	}
	for _, c := range q.Changes {
		if err := c.Validate(); err != nil {
			return err
		}
	}
	if q.IPv4 != nil {
		if err := q.IPv4.Validate(); err != nil {
			return fmt.Errorf("ipv4: %w", err)
		}
	}
	if q.IPv6 != nil {
		if err := q.IPv6.Validate(); err != nil {
			return fmt.Errorf("ipv6: %w", err)
		}
	}
	return nil
}

// Validate checks boundaries for an IPQAFamilyInfo struct.
func (f IPQAFamilyInfo) Validate() error {
	if f.RiskScore != nil {
		score := *f.RiskScore
		if math.IsNaN(score) || math.IsInf(score, 0) || score < 0 || score > 100 {
			return errors.New("risk_score must be a finite number between 0 and 100")
		}
	}
	if f.RiskLevel != "" {
		switch strings.ToLower(f.RiskLevel) {
		case "low", "medium", "high", "unknown", "clean", "critical":
		default:
			return fmt.Errorf("risk_level %q is invalid", f.RiskLevel)
		}
	}
	if err := validateString("ip_type", f.IPType, 32, false); err != nil {
		return err
	}
	if err := validateString("country", f.Country, 64, false); err != nil {
		return err
	}
	if err := validateString("region", f.Region, 128, false); err != nil {
		return err
	}
	if err := validateString("asn", f.ASN, 64, false); err != nil {
		return err
	}
	if err := validateString("organization", f.Organization, 256, false); err != nil {
		return err
	}
	if len(f.SourceScores) > 32 {
		return errors.New("source_scores count exceeds 32")
	}
	for k, v := range f.SourceScores {
		if err := validateString("source score name", k, 64, true); err != nil {
			return err
		}
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 100 {
			return fmt.Errorf("source score %q must be a finite number between 0 and 100", k)
		}
	}
	return nil
}

// Validate checks boundaries for an IPQAChange struct.
func (c IPQAChange) Validate() error {
	if err := validateString("category", c.Category, 64, false); err != nil {
		return err
	}
	if c.Severity != "" {
		switch strings.ToUpper(c.Severity) {
		case "INFO", "WARNING", "CRITICAL", "NONE":
		default:
			return fmt.Errorf("change severity %q is invalid", c.Severity)
		}
	}
	if err := validateString("before", c.Before, 256, false); err != nil {
		return err
	}
	if err := validateString("after", c.After, 256, false); err != nil {
		return err
	}
	if c.ChangedAt < 0 {
		return errors.New("changed_at must not be negative")
	}
	return nil
}
