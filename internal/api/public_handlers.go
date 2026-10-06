package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/protocol"
	"github.com/probewatch/probewatch/internal/security"
)

// publicStatusResponse is the complete, allow-listed shape of the
// unauthenticated public status payload. Every field is a sanitized
// aggregate: node IPs, UUIDs, internal node IDs, tokens, resource
// details, detection targets and alert details are deliberately absent
// from this struct so they can never leak through the endpoint.
type publicStatusResponse struct {
	Nodes         publicStatusNodes  `json:"nodes"`
	Checks        publicStatusChecks `json:"checks"`
	LastUpdatedAt *time.Time         `json:"last_updated_at"`
	GeneratedAt   time.Time          `json:"generated_at"`
}

type publicClientInfoResponse struct {
	IP       string `json:"ip"`
	Location string `json:"location,omitempty"`
	ISP      string `json:"isp,omitempty"`
	Platform string `json:"platform,omitempty"`
	Browser  string `json:"browser,omitempty"`
}

// publicClientInfo returns only the visitor's connection address as seen by
// the reverse proxy. It is intentionally separate from node telemetry so the
// public dashboard can label the current visitor without exposing node IPs.
func (s *Server) publicClientInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ip := publicClientIP(r)
	response := publicClientInfoResponse{IP: ip, Platform: publicClientPlatform(r.UserAgent()), Browser: publicClientBrowser(r.UserAgent())}
	writeJSON(w, http.StatusOK, response)
}

func publicClientPlatform(userAgent string) string {
	ua := strings.ToLower(userAgent)
	switch {
	case strings.Contains(ua, "windows"):
		return "Windows"
	case strings.Contains(ua, "mac os") || strings.Contains(ua, "macintosh"):
		return "macOS"
	case strings.Contains(ua, "android"):
		return "Android"
	case strings.Contains(ua, "iphone") || strings.Contains(ua, "ipad"):
		return "iOS"
	case strings.Contains(ua, "linux"):
		return "Linux"
	default:
		return "未知系统"
	}
}

func publicClientBrowser(userAgent string) string {
	ua := strings.ToLower(userAgent)
	switch {
	case strings.Contains(ua, "edg/"):
		return "Edge Browser"
	case strings.Contains(ua, "firefox/"):
		return "Firefox Browser"
	case strings.Contains(ua, "chrome/") || strings.Contains(ua, "crios/"):
		return "Chrome Browser"
	case strings.Contains(ua, "safari/"):
		return "Safari Browser"
	default:
		return "浏览器"
	}
}

type publicStatusNodes struct {
	Online    int                    `json:"online"`
	Total     int                    `json:"total"`
	Names     []string               `json:"names"`
	Telemetry []publicNodeTelemetry  `json:"telemetry"`
}

type publicStatusChecks struct {
	SuccessRate  *float64 `json:"success_rate"`
	AvgLatencyMs *float64 `json:"avg_latency_ms"`
	Total        int      `json:"total"`
	Success      int      `json:"success"`
	Failure      int      `json:"failure"`
	CoverageTotal   int      `json:"coverage_total"`
	CoverageSampled int      `json:"coverage_sampled"`
	CoverageMissing []string `json:"coverage_missing"`
	LatencyThresholdMs int `json:"latency_threshold_ms"`
	RecentFailureStreak int `json:"recent_failure_streak"`
	WindowHours int `json:"window_hours"`
}

// publicNodeTelemetry is the allow-listed, read-only data used by the public
// dashboard cards. It includes the public UUID for detail navigation while
// deliberately omitting internal database IDs, secret tokens, passwords, hostnames,
// listening ports, and raw network payloads.
type publicNodeTelemetry struct {
	UUID              string                 `json:"uuid"`
	Name              string                 `json:"name"`
	Status            string                 `json:"status"`
	LastReportedAt   *time.Time             `json:"last_reported_at"`
	CPUPercent        *float64               `json:"cpu_percent"`
	Load1             *float64               `json:"load1"`
	MemoryUsedBytes   *uint64                `json:"memory_used_bytes"`
	MemoryTotalBytes  *uint64                `json:"memory_total_bytes"`
	FilesystemUsed    *uint64                `json:"filesystem_used_bytes"`
	FilesystemTotal   *uint64                `json:"filesystem_total_bytes"`
	NetworkRxBytes    *uint64                `json:"network_rx_bytes"`
	NetworkTxBytes    *uint64                `json:"network_tx_bytes"`
	StartedAt         *int64                 `json:"started_at"`
	LatencyMS         *float64               `json:"latency_ms"`
	LossRate          *float64               `json:"loss_rate"`
	LastCheckedAt     *time.Time             `json:"last_checked_at"`
	Checks            []publicNodeCheck      `json:"checks"`
}

type publicNodeCheck struct {
	Kind          string     `json:"kind"`
	Label         string     `json:"label,omitempty"`
	LatencyMS     *float64   `json:"latency_ms"`
	LatencyP95MS  *float64   `json:"latency_p95_ms,omitempty"`
	SampleCount   int        `json:"sample_count,omitempty"`
	LossRate      *float64   `json:"loss_rate"`
	LastCheckedAt *time.Time `json:"last_checked_at"`
}

var (
	publicNameIPv4Pattern = regexp.MustCompile(`(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])(\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])){3}`)
	publicNameUUIDPattern = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
)

const publicNodeNameLimit = 48

func publicCheckLabel(name, kind string) string {
	text := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.Contains(text, "电信") || strings.Contains(text, "telecom") || strings.Contains(text, "china tel"):
		return "电信"
	case strings.Contains(text, "联通") || strings.Contains(text, "unicom") || strings.Contains(text, "china unicom"):
		return "联通"
	case strings.Contains(text, "移动") || strings.Contains(text, "mobile") || strings.Contains(text, "china mobile"):
		return "移动"
	default:
		return strings.ToUpper(strings.TrimSpace(kind))
	}
}

// sanitizeNodeName masks anything that could identify infrastructure
// (IPv4 literals and UUIDs) inside an operator-chosen node name before
// the name is published on the public status endpoint.
func sanitizeNodeName(name string) string {
	masked := publicNameUUIDPattern.ReplaceAllString(name, "[已脱敏]")
	masked = publicNameIPv4Pattern.ReplaceAllString(masked, "[已脱敏]")
	masked = strings.TrimSpace(masked)
	if runes := []rune(masked); len(runes) > publicNodeNameLimit {
		masked = string(runes[:publicNodeNameLimit]) + "…"
	}
	return masked
}

// publicStatus serves GET /api/public/status: an unauthenticated,
// read-only view of sanitized aggregates for the guest status page.
// It reuses the same store data as the authenticated /api/overview
// handler but only emits the allow-listed fields above.
func (s *Server) publicStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	now := time.Now().UTC()
	if !s.publicLimiter.Allow(publicLimiterKey(r), now) {
		writeJSONError(w, http.StatusTooManyRequests, "too many requests")
		return
	}
	s.publicCacheMu.RLock()
	if !s.publicCacheAt.IsZero() && now.Sub(s.publicCacheAt) < 2*time.Second {
		cached := s.publicCache
		s.publicCacheMu.RUnlock()
		writeJSON(w, http.StatusOK, cached)
		return
	}
	s.publicCacheMu.RUnlock()
	nodes, err := s.service.Store().ListNodes(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "service unavailable")
		return
	}
	response := publicStatusResponse{
		Nodes:  publicStatusNodes{Names: make([]string, 0, len(nodes)), Telemetry: make([]publicNodeTelemetry, 0, len(nodes))},
		Checks: publicStatusChecks{},
	}
	response.Nodes.Total = len(nodes)
	from := now.Add(-24 * time.Hour)
	limit := 100
	checksTotal, checksSuccess, latencyTotal, latencyCount := 0, 0, int64(0), 0
	telemetryLatencyTotal, telemetryLatencyCount := 0.0, 0
	const latencyAlertThresholdMs = 200
	const windowHours = 24
	coverageLabels := map[string]bool{"联通": false, "电信": false, "移动": false}
	type checkOutcome struct { at time.Time; ok bool }
	var outcomes []checkOutcome
	seenChecks := make(map[string]struct{})
	var lastUpdated time.Time
	for _, node := range nodes {
		// The resource payload is intentionally discarded here:
		// only the report timestamp is used for the online count.
		telemetry := publicNodeTelemetry{
			UUID:   node.UUID,
			Name:   sanitizeNodeName(node.Name),
			Status: "offline",
			Checks: make([]publicNodeCheck, 0, 3),
		}
		if reportedAt, payload, e := s.service.Store().GetResourceLatest(r.Context(), node.ID); e == nil {
			telemetry.LastReportedAt = &reportedAt
			if time.Since(reportedAt) <= 2*time.Minute {
				telemetry.Status = "online"
			} else if time.Since(reportedAt) <= 10*time.Minute {
				telemetry.Status = "attention"
			}
			var resource struct {
				CPUPercent float64 `json:"cpu_percent"`
				Load1 float64 `json:"load1"`
				MemoryUsedBytes uint64 `json:"memory_used_bytes"`
				MemoryTotalBytes uint64 `json:"memory_total_bytes"`
				FilesystemUsedBytes uint64 `json:"filesystem_used_bytes"`
				FilesystemTotalBytes uint64 `json:"filesystem_total_bytes"`
				NetworkRxBytes uint64 `json:"network_rx_bytes"`
				NetworkTxBytes uint64 `json:"network_tx_bytes"`
				StartedAt int64 `json:"started_at"`
			}
			if json.Unmarshal(payload, &resource) == nil {
				telemetry.CPUPercent = &resource.CPUPercent
				telemetry.Load1 = &resource.Load1
				telemetry.MemoryUsedBytes = &resource.MemoryUsedBytes
				telemetry.MemoryTotalBytes = &resource.MemoryTotalBytes
				telemetry.FilesystemUsed = &resource.FilesystemUsedBytes
				telemetry.FilesystemTotal = &resource.FilesystemTotalBytes
				telemetry.NetworkRxBytes = &resource.NetworkRxBytes
				telemetry.NetworkTxBytes = &resource.NetworkTxBytes
				telemetry.StartedAt = &resource.StartedAt
			}
			if lastUpdated.IsZero() || reportedAt.After(lastUpdated) {
				lastUpdated = reportedAt
			}
			if now.Sub(reportedAt) <= 2*time.Minute {
				response.Nodes.Online++
			}
		}
		// Use the latest result per target to expose a safe aggregate for the card.
		latestResults, _ := s.service.Store().ListNetworkLatest(r.Context(), node.ID)
		var latencyTotal int64
		var latencyCount, latestTotal, latestFailure int
		var latestChecked time.Time
		for _, latest := range latestResults {
			var result struct { Status string `json:"status"`; Latency int64 `json:"latency_ms"` }
			if json.Unmarshal(latest.Payload, &result) != nil { continue }
			latestTotal++
			if result.Status != "success" && result.Status != "available" { latestFailure++ }
			if result.Latency > 0 { latencyTotal += result.Latency; latencyCount++ }
			if latestChecked.IsZero() || latest.CheckedAt.After(latestChecked) { latestChecked = latest.CheckedAt }
		}
		if latestTotal > 0 {
			loss := float64(latestFailure) / float64(latestTotal)
			telemetry.LossRate = &loss
		}
		if latencyCount > 0 { avg := float64(latencyTotal) / float64(latencyCount); telemetry.LatencyMS = &avg }
		if telemetry.LatencyMS != nil { telemetryLatencyTotal += *telemetry.LatencyMS; telemetryLatencyCount++ }
		if !latestChecked.IsZero() { telemetry.LastCheckedAt = &latestChecked }
		if summaries, e := s.service.Store().GetCheckSummary(r.Context(), node.ID, now.Add(-24*time.Hour), now); e == nil {
			for _, summary := range summaries {
				check := publicNodeCheck{Kind: summary.Kind, Label: publicCheckLabel(summary.Name, summary.Kind)}
				if _, known := coverageLabels[check.Label]; known && summary.HasWindowData { coverageLabels[check.Label] = true }
				if summary.HasWindowData && summary.LatencyCount > 0 { value := summary.LatencyAvgMS; check.LatencyMS = &value }
				if summary.HasWindowData && summary.LatencyP95MS != nil { p95 := *summary.LatencyP95MS; check.LatencyP95MS = &p95 }
				check.SampleCount = summary.SampleCount
				if summary.HasWindowData && summary.Total > 0 { value := float64(summary.Failure) / float64(summary.Total); check.LossRate = &value }
				if !summary.LastCheckedAt.IsZero() { value := summary.LastCheckedAt; check.LastCheckedAt = &value }
				if check.LatencyMS != nil || check.LossRate != nil || check.LastCheckedAt != nil { telemetry.Checks = append(telemetry.Checks, check) }
			}
		}
		response.Nodes.Names = append(response.Nodes.Names, telemetry.Name)
		response.Nodes.Telemetry = append(response.Nodes.Telemetry, telemetry)
		for _, kind := range []db.TargetKind{db.TargetKindTCP, db.TargetKindHTTP, db.TargetKindHTTPS, db.TargetKindDNS, db.TargetKindMTR} {
			records, e := s.service.Store().GetResultHistory(r.Context(), kind, node.ID, from, now, limit)
			if e != nil {
				continue
			}
			for _, record := range records {
				key := record.TargetID + "\x00" + record.CheckedAt.UTC().Format(time.RFC3339Nano)
				if _, exists := seenChecks[key]; exists {
					continue
				}
				seenChecks[key] = struct{}{}
				var result struct {
					Status  string `json:"status"`
					Reached bool   `json:"reached"`
					Error   string `json:"error"`
					Latency int64  `json:"latency_ms"`
				}
				if json.Unmarshal(record.Payload, &result) == nil {
					checksTotal++
					ok := result.Status == "success" || result.Status == "available" || (kind == db.TargetKindMTR && result.Reached && result.Error == "")
					outcomes = append(outcomes, checkOutcome{at: record.CheckedAt, ok: ok})
					if ok {
						checksSuccess++
					}
					if result.Latency > 0 {
						latencyTotal += result.Latency
						latencyCount++
					}
				}
			}
		}
	}
	if checksTotal > 0 {
		response.Checks.Total = checksTotal
		response.Checks.Success = checksSuccess
		response.Checks.Failure = checksTotal - checksSuccess
		rate := float64(checksSuccess) * 100 / float64(checksTotal)
		response.Checks.SuccessRate = &rate
		if latencyCount > 0 {
			avg := float64(latencyTotal) / float64(latencyCount)
			response.Checks.AvgLatencyMs = &avg
		}
	}
	if response.Checks.AvgLatencyMs == nil && telemetryLatencyCount > 0 {
		avg := telemetryLatencyTotal / float64(telemetryLatencyCount)
		response.Checks.AvgLatencyMs = &avg
	}
	response.Checks.CoverageTotal = len(coverageLabels)
	for label, sampled := range coverageLabels {
		if sampled {
			response.Checks.CoverageSampled++
		} else {
			response.Checks.CoverageMissing = append(response.Checks.CoverageMissing, label)
		}
	}
	sort.Strings(response.Checks.CoverageMissing)
	response.Checks.LatencyThresholdMs = latencyAlertThresholdMs
	response.Checks.WindowHours = windowHours
	sort.Slice(outcomes, func(i, j int) bool { return outcomes[i].at.After(outcomes[j].at) })
	for _, outcome := range outcomes {
		if outcome.ok { break }
		response.Checks.RecentFailureStreak++
	}
	if !lastUpdated.IsZero() {
		response.LastUpdatedAt = &lastUpdated
	}
	response.GeneratedAt = now
	sort.Strings(response.Nodes.Names)
	s.publicCacheMu.Lock()
	s.publicCache = response
	s.publicCacheAt = now
	s.publicCacheMu.Unlock()
	writeJSON(w, http.StatusOK, response)
}

func publicLimiterKey(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil && isLoopbackHost(host) {
		if forwarded := strings.TrimSpace(r.Header.Get("X-Real-IP")); forwarded != "" && net.ParseIP(forwarded) != nil {
			return forwarded
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

func publicClientIP(r *http.Request) string {
	for _, header := range []string{"CF-Connecting-IP", "X-Real-IP"} {
		if value := strings.TrimSpace(r.Header.Get(header)); net.ParseIP(value) != nil {
			return value
		}
	}
	if forwarded := strings.Split(r.Header.Get("X-Forwarded-For"), ","); len(forwarded) > 0 {
		if value := strings.TrimSpace(forwarded[0]); net.ParseIP(value) != nil {
			return value
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil && net.ParseIP(host) != nil {
		return host
	}
	if net.ParseIP(strings.TrimSpace(r.RemoteAddr)) != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return ""
}

func isLoopbackHost(host string) bool {
	ip := net.ParseIP(strings.TrimSpace(host))
	return ip != nil && ip.IsLoopback()
}


// publicNodeResource is the strict, safe public whitelist representation of node hardware and utilization.
// Sensitive network configuration, physical MAC addresses, local processes, listening sockets,
// internal identifiers, and database internals are strictly excluded.
type publicNodeResource struct {
	OS                  string               `json:"os,omitempty"`
	Kernel              string               `json:"kernel,omitempty"`
	Architecture        string               `json:"architecture,omitempty"`
	Arch                string               `json:"arch,omitempty"`
	CPUModel            string               `json:"cpu_model,omitempty"`
	CPUCores            int                  `json:"cpu_cores,omitempty"`
	CPUMHz              float64              `json:"cpu_mhz,omitempty"`
	CPUPercent          *float64             `json:"cpu_percent,omitempty"`
	MemoryUsedBytes     uint64               `json:"memory_used_bytes,omitempty"`
	MemoryTotalBytes    uint64               `json:"memory_total_bytes,omitempty"`
	FilesystemUsedBytes uint64               `json:"filesystem_used_bytes,omitempty"`
	FilesystemTotalBytes uint64              `json:"filesystem_total_bytes,omitempty"`
	NetworkRxBytes      uint64               `json:"network_rx_bytes,omitempty"`
	NetworkTxBytes      uint64               `json:"network_tx_bytes,omitempty"`
	UptimeSeconds       int64                `json:"uptime_seconds,omitempty"`
	Virtualization      string               `json:"virtualization,omitempty"`
	StartedAt           int64                `json:"started_at,omitempty"`
	HasIPv4             bool                 `json:"has_ipv4,omitempty"`
	HasIPv6             bool                 `json:"has_ipv6,omitempty"`
	DualStack           bool                 `json:"dual_stack,omitempty"`
}

type publicIPQualityDTO struct {
	IPType              string `json:"ip_type,omitempty"`
	Country             string `json:"country,omitempty"`
	Region              string `json:"region,omitempty"`
	ASN                 string `json:"asn,omitempty"`
	Organization        string `json:"organization,omitempty"`
	Risk                string `json:"risk,omitempty"`
	Proxy               *bool  `json:"proxy,omitempty"`
	VPN                 *bool  `json:"vpn,omitempty"`
	Tor                 *bool  `json:"tor,omitempty"`
	Abuse               *bool  `json:"abuse,omitempty"`
	CheckedAt           int64  `json:"checked_at,omitempty"`
	IPQAEnabled         bool   `json:"ipqa_enabled"`
	IPQAInstalled       bool   `json:"ipqa_installed"`
	HighestSeverity     string `json:"highest_severity,omitempty"`
	AlertCount          int    `json:"alert_count,omitempty"`
	CriticalCount       int    `json:"critical_count,omitempty"`
	WarningCount        int    `json:"warning_count,omitempty"`
	InfoCount           int    `json:"info_count,omitempty"`
	LastCheckedAt       *int64 `json:"last_checked_at,omitempty"`
	HasRecentChanges    bool   `json:"has_recent_changes,omitempty"`
	RecentChangeSummary string `json:"recent_change_summary,omitempty"`
}

type publicHealthInfoDTO struct {
	HealthScore      int      `json:"health_score"`
	HealthStatus     string   `json:"health_status,omitempty"`
	RebootRequired   bool     `json:"reboot_required"`
	SecurityUpdates  int      `json:"security_updates"`
	TotalUpdates     int      `json:"total_updates"`
	FailedServices   []string `json:"failed_services,omitempty"`
	HealthDeductions []string `json:"health_deductions,omitempty"`
}

type publicMediaResultDTO struct {
	DetectorID string `json:"detector_id"`
	Detector   string `json:"detector,omitempty"`
	Status     string `json:"status"`
	Region     string `json:"region,omitempty"`
	LatencyMS  int64  `json:"latency_ms,omitempty"`
	Reason     string `json:"reason,omitempty"`
	CheckedAt  int64  `json:"checked_at"`
}

func sanitizePublicTargetHost(host string) string {
	trimmed := strings.TrimSpace(host)
	if h, _, err := net.SplitHostPort(trimmed); err == nil {
		trimmed = h
	}
	if net.ParseIP(trimmed) != nil || publicNameIPv4Pattern.MatchString(trimmed) {
		return ""
	}
	return trimmed
}

type publicCheckSummaryDTO struct {
	TargetID      string     `json:"target_id"`
	Name          string     `json:"name"`
	Kind          string     `json:"kind"`
	Host          string     `json:"host,omitempty"`
	LastCheckedAt *time.Time `json:"last_checked_at,omitempty"`
	Total        *int64     `json:"total,omitempty"`
	Success      *int64     `json:"success,omitempty"`
	Failure      *int64     `json:"failure,omitempty"`
	LossRate     *float64   `json:"loss_rate,omitempty"`
	LatencyAvgMS *float64   `json:"latency_avg_ms,omitempty"`
	LatencyP95MS *float64   `json:"latency_p95_ms,omitempty"`
	JitterMS     *float64   `json:"jitter_ms,omitempty"`
	SampleCount  int        `json:"sample_count,omitempty"`
	WindowHours  int        `json:"window_hours,omitempty"`
}

type publicNodeDetailResponse struct {
	UUID           string                  `json:"uuid"`
	ID             string                  `json:"id"`
	Name           string                  `json:"name"`
	Status         string                  `json:"status"`
	LastReportedAt *time.Time              `json:"last_reported_at"`
	Resource       *publicNodeResource     `json:"resource"`
	IPQuality      *publicIPQualityDTO     `json:"ip_quality,omitempty"`
	HealthInfo     *publicHealthInfoDTO    `json:"health_info,omitempty"`
	Media          []publicMediaResultDTO  `json:"media"`
	Checks         []publicCheckSummaryDTO `json:"checks"`
}

func (s *Server) publicNodeRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	now := time.Now().UTC()
	if !s.publicLimiter.Allow(publicLimiterKey(r), now) {
		writeJSONError(w, http.StatusTooManyRequests, "too many requests")
		return
	}
	trimmed := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(trimmed, "/")
	// Expected parts: ["api", "public", "nodes", "{uuid}", ...]
	if len(parts) < 4 || parts[0] != "api" || parts[1] != "public" || parts[2] != "nodes" {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	uuid := parts[3]
	if !security.IsRFC4122UUID(uuid) {
		writeJSONError(w, http.StatusNotFound, "node not found")
		return
	}
	node, err := s.service.Store().GetNodeByUUID(r.Context(), uuid)
	if errors.Is(err, sql.ErrNoRows) || node.ID == "" {
		writeJSONError(w, http.StatusNotFound, "node not found")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "service unavailable")
		return
	}
	if len(parts) == 4 || (len(parts) == 5 && parts[4] == "detail") {
		s.writePublicNodeDetail(w, r, node)
		return
	}
	if len(parts) == 6 && parts[4] == "resource" && parts[5] == "history" {
		s.writePublicNodeResourceHistory(w, r, node.ID)
		return
	}
	if len(parts) == 6 && parts[4] == "network" && parts[5] == "history" {
		s.writePublicNodeNetworkHistory(w, r, node.ID)
		return
	}
	if len(parts) == 6 && parts[4] == "checks" && parts[5] == "summary" {
		s.writePublicNodeChecksSummary(w, r, node.ID)
		return
	}
	if len(parts) == 5 && parts[4] == "traffic" {
		s.nodeTraffic(w, r, node.UUID)
		return
	}
	if len(parts) == 5 && parts[4] == "media" {
		s.writePublicMediaLatest(w, r, node.ID)
		return
	}
	if len(parts) == 5 && parts[4] == "ip-quality" {
		s.writePublicIPQuality(w, r, node.ID)
		return
	}
	writeJSONError(w, http.StatusNotFound, "not found")
}

// writePublicNodeDetail serves safe, read-only telemetry for the guest node detail page.
// Admin tokens, private keys, passwords, physical MACs, interface lists, processes,
// socket internals, and listening port maps are strictly omitted.
func (s *Server) writePublicNodeDetail(w http.ResponseWriter, r *http.Request, node db.Node) {
	reportedAt, payload, err := s.service.Store().GetResourceLatest(r.Context(), node.ID)
	status := "offline"
	var lastReportedAt *time.Time
	if err == nil && !reportedAt.IsZero() {
		lastReportedAt = &reportedAt
		if time.Since(reportedAt) <= 2*time.Minute {
			status = "online"
		} else if time.Since(reportedAt) <= 10*time.Minute {
			status = "attention"
		}
	}

	var snapshot protocol.ResourceSnapshot
	if len(payload) > 0 && json.Valid(payload) {
		_ = json.Unmarshal(payload, &snapshot)
	}

	ipQualityDTO := populatePublicIPQualityDTO(snapshot.IPQuality, snapshot.IPQA)

	var healthInfoDTO *publicHealthInfoDTO
	if snapshot.HealthInfo != nil {
		healthInfoDTO = &publicHealthInfoDTO{
			HealthScore:      snapshot.HealthInfo.HealthScore,
			HealthStatus:     snapshot.HealthInfo.HealthStatus,
			RebootRequired:   snapshot.HealthInfo.RebootRequired,
			SecurityUpdates:  snapshot.HealthInfo.SecurityUpdates,
			TotalUpdates:     snapshot.HealthInfo.TotalUpdates,
			FailedServices:   snapshot.HealthInfo.FailedServices,
			HealthDeductions: snapshot.HealthInfo.HealthDeductions,
		}
	}

	uptimeSeconds := int64(0)
	if snapshot.StartedAt > 0 {
		nowSec := time.Now().Unix()
		if nowSec >= snapshot.StartedAt {
			uptimeSeconds = nowSec - snapshot.StartedAt
		}
	}

	cpuP := snapshot.CPUPercent
	arch := snapshot.Arch

	publicResource := publicNodeResource{
		OS:                  snapshot.OS,
		Kernel:              snapshot.Kernel,
		Architecture:        arch,
		Arch:                arch,
		CPUModel:            snapshot.CPUModel,
		CPUCores:            snapshot.CPUCores,
		CPUMHz:              snapshot.CPUMHz,
		CPUPercent:          &cpuP,
		MemoryUsedBytes:     snapshot.MemoryUsedBytes,
		MemoryTotalBytes:    snapshot.MemoryTotalBytes,
		FilesystemUsedBytes: snapshot.FilesystemUsedBytes,
		FilesystemTotalBytes: snapshot.FilesystemTotalBytes,
		NetworkRxBytes:      snapshot.NetworkRxBytes,
		NetworkTxBytes:      snapshot.NetworkTxBytes,
		UptimeSeconds:       uptimeSeconds,
		Virtualization:      "",
		StartedAt:           snapshot.StartedAt,
		HasIPv4:             snapshot.IPv4 != "",
		HasIPv6:             snapshot.IPv6 != "",
		DualStack:           snapshot.IPv4 != "" && snapshot.IPv6 != "",
	}

	// Collect public media results
	mediaResults, err := s.service.Store().ListMediaLatest(r.Context(), node.ID)
	mediaList := make([]publicMediaResultDTO, 0, len(mediaResults))
	if err == nil {
		for _, latest := range mediaResults {
			var result protocol.MediaResult
			if json.Unmarshal(latest.Payload, &result) == nil {
				mediaList = append(mediaList, publicMediaResultDTO{
					DetectorID: latest.ID,
					Detector:   result.Detector,
					Status:     result.Status,
					Region:     result.Region,
					LatencyMS:  result.LatencyMS,
					Reason:     result.Reason,
					CheckedAt:  result.CheckedAt,
				})
			}
		}
	}

	// Collect 24h checks summary
	now := time.Now().UTC()
	summaries, err := s.service.Store().GetCheckSummary(r.Context(), node.ID, now.Add(-24*time.Hour), now)
	checksList := make([]publicCheckSummaryDTO, 0, len(summaries))
	if err == nil {
		for _, summary := range summaries {
			item := publicCheckSummaryDTO{
				TargetID: summary.TargetID,
				Name:     summary.Name,
				Kind:     summary.Kind,
				Host:     sanitizePublicTargetHost(summary.Host),
			}
			if !summary.LastCheckedAt.IsZero() {
				checkedAt := summary.LastCheckedAt
				item.LastCheckedAt = &checkedAt
			}
			if summary.HasWindowData {
				total, success, failure := summary.Total, summary.Success, summary.Failure
				item.Total, item.Success, item.Failure = &total, &success, &failure
				if summary.Total > 0 {
					lossRate := float64(summary.Failure) / float64(summary.Total)
					item.LossRate = &lossRate
				}
				if summary.LatencyCount > 0 {
					latencyAvg := summary.LatencyAvgMS
					item.LatencyAvgMS = &latencyAvg
				}
				if summary.HasJitter {
					jitter := summary.JitterMS
					item.JitterMS = &jitter
				}
				if summary.LatencyP95MS != nil {
					p95 := *summary.LatencyP95MS
					item.LatencyP95MS = &p95
				}
				item.SampleCount = summary.SampleCount
				item.WindowHours = 24
			}
			checksList = append(checksList, item)
		}
	}

	response := publicNodeDetailResponse{
		UUID:           node.UUID,
		ID:             node.UUID,
		Name:           sanitizeNodeName(node.Name),
		Status:         status,
		LastReportedAt: lastReportedAt,
		Resource:       &publicResource,
		IPQuality:      ipQualityDTO,
		HealthInfo:     healthInfoDTO,
		Media:          mediaList,
		Checks:         checksList,
	}
	writeJSON(w, http.StatusOK, response)
}

// writePublicIPQuality exposes only a deliberately small, non-identifying
// quality summary. Provider scores and addresses stay private to the
// authenticated node detail response.
func (s *Server) writePublicIPQuality(w http.ResponseWriter, r *http.Request, nodeID string) {
	_, payload, err := s.service.Store().GetResourceLatest(r.Context(), nodeID)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{"ip_quality": nil})
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "node resource unavailable")
		return
	}
	var snapshot protocol.ResourceSnapshot
	if json.Unmarshal(payload, &snapshot) != nil || (snapshot.IPQuality == nil && snapshot.IPQA == nil) {
		writeJSON(w, http.StatusOK, map[string]any{"ip_quality": nil})
		return
	}
	dto := populatePublicIPQualityDTO(snapshot.IPQuality, snapshot.IPQA)
	writeJSON(w, http.StatusOK, map[string]any{"ip_quality": dto})
}

func populatePublicIPQualityDTO(q *protocol.IPQualityInfo, qa *protocol.IPQAInfo) *publicIPQualityDTO {
	if q == nil && qa == nil {
		return nil
	}
	dto := &publicIPQualityDTO{}
	if q != nil {
		dto.IPType = q.IPType
		dto.Country = q.Country
		dto.Region = q.Region
		dto.ASN = q.ASN
		dto.Organization = q.Organization
		dto.Risk = q.Risk
		dto.Proxy = q.Proxy
		dto.VPN = q.VPN
		dto.Tor = q.Tor
		dto.Abuse = q.Abuse
		dto.CheckedAt = q.CheckedAt
	}
	if qa != nil {
		dto.IPQAEnabled = qa.Enabled
		dto.IPQAInstalled = qa.Installed
		dto.HighestSeverity = qa.HighestSeverity
		dto.AlertCount = qa.AlertCount
		dto.CriticalCount = qa.CriticalCount
		dto.WarningCount = qa.WarningCount
		dto.InfoCount = qa.InfoCount
		if qa.LastCheckedAt > 0 {
			dto.LastCheckedAt = &qa.LastCheckedAt
		}
		dto.HasRecentChanges = len(qa.Changes) > 0
		if len(qa.Changes) > 0 {
			dto.RecentChangeSummary = qa.Changes[0].After
			if dto.RecentChangeSummary == "" {
				dto.RecentChangeSummary = qa.Changes[0].Category
			}
		}
		if dto.IPType == "" && qa.IPv4 != nil && qa.IPv4.IPType != "" {
			dto.IPType = qa.IPv4.IPType
		}
		if dto.Country == "" && qa.IPv4 != nil && qa.IPv4.Country != "" {
			dto.Country = qa.IPv4.Country
		}
		if dto.Region == "" && qa.IPv4 != nil && qa.IPv4.Region != "" {
			dto.Region = qa.IPv4.Region
		}
		if dto.ASN == "" && qa.IPv4 != nil && qa.IPv4.ASN != "" {
			dto.ASN = qa.IPv4.ASN
		}
		if dto.Organization == "" && qa.IPv4 != nil && qa.IPv4.Organization != "" {
			dto.Organization = qa.IPv4.Organization
		}
		if dto.Proxy == nil && qa.IPv4 != nil && qa.IPv4.Proxy != nil {
			dto.Proxy = qa.IPv4.Proxy
		}
		if dto.VPN == nil && qa.IPv4 != nil && qa.IPv4.VPN != nil {
			dto.VPN = qa.IPv4.VPN
		}
		if dto.Tor == nil && qa.IPv4 != nil && qa.IPv4.Tor != nil {
			dto.Tor = qa.IPv4.Tor
		}
		if dto.Abuse == nil && qa.IPv4 != nil && qa.IPv4.Abuse != nil {
			dto.Abuse = qa.IPv4.Abuse
		}
	}
	return dto
}

type publicDiskStat struct {
	UsedBytes        uint64  `json:"used_bytes,omitempty"`
	TotalBytes       uint64  `json:"total_bytes,omitempty"`
	ReadBytesPerSec  uint64  `json:"read_bytes_per_sec,omitempty"`
	WriteBytesPerSec uint64  `json:"write_bytes_per_sec,omitempty"`
	ReadIOPS         float64 `json:"read_iops,omitempty"`
	WriteIOPS        float64 `json:"write_iops,omitempty"`
}

// publicHistoryResource is the strict whitelist DTO for historical time-series telemetry in guest mode.
// Sensitive interface maps, MAC addresses, IPv4/IPv6 literals, open ports, socket lists, processes,
// hostnames and tokens are strictly excluded.
type publicHistoryResource struct {
	OS                   string           `json:"os,omitempty"`
	Kernel               string           `json:"kernel,omitempty"`
	Architecture         string           `json:"architecture,omitempty"`
	Arch                 string           `json:"arch,omitempty"`
	CPUModel             string           `json:"cpu_model,omitempty"`
	CPUCores             int              `json:"cpu_cores,omitempty"`
	CPUMHz               float64          `json:"cpu_mhz,omitempty"`
	CPUPercent           *float64         `json:"cpu_percent,omitempty"`
	CPUTempC             *float64         `json:"cpu_temp_c,omitempty"`
	Load1                *float64         `json:"load1,omitempty"`
	Load5                *float64         `json:"load5,omitempty"`
	Load15               *float64         `json:"load15,omitempty"`
	MemoryUsedBytes      uint64           `json:"memory_used_bytes,omitempty"`
	MemoryTotalBytes     uint64           `json:"memory_total_bytes,omitempty"`
	SwapUsedBytes        uint64           `json:"swap_used_bytes,omitempty"`
	SwapTotalBytes       uint64           `json:"swap_total_bytes,omitempty"`
	FilesystemUsedBytes  uint64           `json:"filesystem_used_bytes,omitempty"`
	FilesystemTotalBytes uint64           `json:"filesystem_total_bytes,omitempty"`
	NetworkRxBytes       uint64           `json:"network_rx_bytes,omitempty"`
	NetworkTxBytes       uint64           `json:"network_tx_bytes,omitempty"`
	NetworkRxBytesDelta  *uint64          `json:"network_rx_bytes_delta,omitempty"`
	NetworkTxBytesDelta  *uint64          `json:"network_tx_bytes_delta,omitempty"`
	TCPConnCount         *uint64          `json:"tcp_conn_count,omitempty"`
	UDPConnCount         *uint64          `json:"udp_conn_count,omitempty"`
	ProcessCount         *uint64          `json:"process_count,omitempty"`
	Disks                []publicDiskStat `json:"disks,omitempty"`
}

type publicHistoryResourceResponse struct {
	ReportedAt time.Time             `json:"reported_at"`
	Resource   publicHistoryResource `json:"resource"`
}

func (s *Server) writePublicNodeResourceHistory(w http.ResponseWriter, r *http.Request, nodeID string) {
	from, to, limit, ok := parseHistoryWindow(r, time.Now().UTC())
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "invalid query parameters")
		return
	}
	records, err := s.service.Store().GetResourceHistory(r.Context(), nodeID, from, to, limit)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "node resource history unavailable")
		return
	}
	out := make([]publicHistoryResourceResponse, 0, len(records))
	for _, record := range records {
		var snapshot protocol.ResourceSnapshot
		if err := json.Unmarshal(record.Payload, &snapshot); err == nil {
			var cpuP *float64
			cpuVal := snapshot.CPUPercent
			cpuP = &cpuVal

			var tempC *float64
			if snapshot.CPUTempC > 0 {
				tempVal := snapshot.CPUTempC
				tempC = &tempVal
			}
			var load1, load5, load15 *float64
			if snapshot.Load1 > 0 || snapshot.Load5 > 0 || snapshot.Load15 > 0 {
				l1, l5, l15 := snapshot.Load1, snapshot.Load5, snapshot.Load15
				load1, load5, load15 = &l1, &l5, &l15
			}
			var tcp, udp, proc *uint64
			if snapshot.TCPConnCount > 0 || snapshot.UDPConnCount > 0 {
				tVal, uVal := snapshot.TCPConnCount, snapshot.UDPConnCount
				tcp, udp = &tVal, &uVal
			}
			if snapshot.ProcessCount > 0 {
				pVal := snapshot.ProcessCount
				proc = &pVal
			}
			var publicDisks []publicDiskStat
			if len(snapshot.Disks) > 0 {
				publicDisks = make([]publicDiskStat, 0, len(snapshot.Disks))
				for _, d := range snapshot.Disks {
					publicDisks = append(publicDisks, publicDiskStat{
						ReadBytesPerSec:  d.ReadBytesPerSec,
						WriteBytesPerSec: d.WriteBytesPerSec,
						ReadIOPS:         d.ReadIOPS,
						WriteIOPS:        d.WriteIOPS,
					})
				}
			}
			item := publicHistoryResourceResponse{
				ReportedAt: record.ReportedAt,
				Resource: publicHistoryResource{
					OS:                   snapshot.OS,
					Kernel:               snapshot.Kernel,
					Architecture:         snapshot.Arch,
					Arch:                 snapshot.Arch,
					CPUModel:             snapshot.CPUModel,
					CPUCores:             snapshot.CPUCores,
					CPUMHz:               snapshot.CPUMHz,
					CPUPercent:           cpuP,
					CPUTempC:             tempC,
					Load1:                load1,
					Load5:                load5,
					Load15:               load15,
					MemoryUsedBytes:      snapshot.MemoryUsedBytes,
					MemoryTotalBytes:     snapshot.MemoryTotalBytes,
					SwapUsedBytes:        snapshot.SwapUsedBytes,
					SwapTotalBytes:       snapshot.SwapTotalBytes,
					FilesystemUsedBytes:  snapshot.FilesystemUsedBytes,
					FilesystemTotalBytes: snapshot.FilesystemTotalBytes,
					NetworkRxBytes:       snapshot.NetworkRxBytes,
					NetworkTxBytes:       snapshot.NetworkTxBytes,
					TCPConnCount:         tcp,
					UDPConnCount:         udp,
					ProcessCount:         proc,
					Disks:                publicDisks,
				},
			}
			out = append(out, item)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// writePublicNodeChecksSummary serves sanitized 24h/window checks reliability metrics.
func (s *Server) writePublicNodeChecksSummary(w http.ResponseWriter, r *http.Request, nodeID string) {
	from, to, _, ok := parseHistoryWindow(r, time.Now().UTC())
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "invalid query parameters")
		return
	}
	summaries, err := s.service.Store().GetCheckSummary(r.Context(), nodeID, from, to)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "checks summary unavailable")
		return
	}
	checksList := make([]publicCheckSummaryDTO, 0, len(summaries))
	for _, summary := range summaries {
		item := publicCheckSummaryDTO{
			TargetID: summary.TargetID,
			Name:     summary.Name,
			Kind:     summary.Kind,
			Host:     sanitizePublicTargetHost(summary.Host),
		}
		if !summary.LastCheckedAt.IsZero() {
			checkedAt := summary.LastCheckedAt
			item.LastCheckedAt = &checkedAt
		}
		if summary.HasWindowData {
			total, success, failure := summary.Total, summary.Success, summary.Failure
			item.Total, item.Success, item.Failure = &total, &success, &failure
			if summary.Total > 0 {
				lossRate := float64(summary.Failure) / float64(summary.Total)
				item.LossRate = &lossRate
			}
			if summary.LatencyCount > 0 {
				latencyAvg := summary.LatencyAvgMS
				item.LatencyAvgMS = &latencyAvg
			}
			if summary.HasJitter {
				jitter := summary.JitterMS
				item.JitterMS = &jitter
			}
			if summary.LatencyP95MS != nil {
				p95 := *summary.LatencyP95MS
				item.LatencyP95MS = &p95
			}
			item.SampleCount = summary.SampleCount
			item.WindowHours = int(to.Sub(from).Hours())
		}
		checksList = append(checksList, item)
	}
	writeJSON(w, http.StatusOK, checksList)
}

// writePublicMediaLatest returns unauthenticated streaming media unlock status using publicMediaResultDTO.
func (s *Server) writePublicMediaLatest(w http.ResponseWriter, r *http.Request, nodeID string) {
	results, err := s.service.Store().ListMediaLatest(r.Context(), nodeID)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "node results unavailable")
		return
	}
	mediaList := make([]publicMediaResultDTO, 0, len(results))
	for _, latest := range results {
		var result protocol.MediaResult
		if json.Unmarshal(latest.Payload, &result) == nil {
			mediaList = append(mediaList, publicMediaResultDTO{
				DetectorID: latest.ID,
				Detector:   result.Detector,
				Status:     result.Status,
				Region:     result.Region,
				LatencyMS:  result.LatencyMS,
				Reason:     result.Reason,
				CheckedAt:  result.CheckedAt,
			})
		}
	}
	writeJSON(w, http.StatusOK, mediaList)
}

type publicNetworkHistoryResultDTO struct {
	LatencyMS *int64 `json:"latency_ms,omitempty"`
	Status    string `json:"status,omitempty"`
}

type publicNetworkHistoryItemDTO struct {
	TargetID  string                        `json:"target_id"`
	CheckedAt time.Time                     `json:"checked_at"`
	LatencyMS *int64                        `json:"latency_ms,omitempty"`
	Status    string                        `json:"status,omitempty"`
	Result    publicNetworkHistoryResultDTO `json:"result"`
}

// writePublicNodeNetworkHistory serves safe latency history time series without leaking network topologies, DNS records, or IP literals.
func (s *Server) writePublicNodeNetworkHistory(w http.ResponseWriter, r *http.Request, nodeID string) {
	from, to, limit, ok := parseHistoryWindow(r, time.Now().UTC())
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "invalid query parameters")
		return
	}
	records, err := s.service.Store().GetResultHistory(r.Context(), db.TargetKindTCP, nodeID, from, to, limit)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "node results history unavailable")
		return
	}
	out := make([]publicNetworkHistoryItemDTO, 0, len(records))
	for _, record := range records {
		var res struct {
			Status    string `json:"status"`
			LatencyMS int64  `json:"latency_ms"`
			Latency   int64  `json:"latency"`
		}
		_ = json.Unmarshal(record.Payload, &res)
		lat := res.LatencyMS
		if lat == 0 && res.Latency > 0 {
			lat = res.Latency
		}
		var pLat *int64
		if lat > 0 {
			pLat = &lat
		}
		status := res.Status
		if status == "" {
			status = "success"
		}
		out = append(out, publicNetworkHistoryItemDTO{
			TargetID:  record.TargetID,
			CheckedAt: record.CheckedAt,
			LatencyMS: pLat,
			Status:    status,
			Result: publicNetworkHistoryResultDTO{
				LatencyMS: pLat,
				Status:    status,
			},
		})
	}
	writeJSON(w, http.StatusOK, out)
}

