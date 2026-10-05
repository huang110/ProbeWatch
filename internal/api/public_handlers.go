package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/db"
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
// dashboard cards. It deliberately omits UUIDs, internal IDs, addresses and
// raw resource payloads while keeping the latest values genuinely live.
type publicNodeTelemetry struct {
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
		telemetry := publicNodeTelemetry{Name: sanitizeNodeName(node.Name), Status: "offline", Checks: make([]publicNodeCheck, 0, 3)}
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

// publicNodeRoute serves unauthenticated, read-only GET requests for node telemetry
// on the guest dashboard (e.g. /api/public/nodes/{uuid}/resource/history,
// /api/public/nodes/{uuid}/checks/summary, /api/public/nodes/{uuid}/traffic).
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
	var node db.Node
	var err error
	if security.IsRFC4122UUID(uuid) {
		node, err = s.service.Store().GetNodeByUUID(r.Context(), uuid)
	} else if strings.HasPrefix(uuid, "guest-") {
		// Guest cards do not expose UUIDs. Resolve their stable, URL-safe name
		// marker server-side so clicking a public card still loads detail data.
		name, decodeErr := url.PathUnescape(strings.TrimPrefix(uuid, "guest-"))
		if decodeErr == nil {
			nodes, listErr := s.service.Store().ListNodes(r.Context())
			if listErr == nil {
				for _, candidate := range nodes {
					if candidate.Name == name {
						node, err = candidate, nil
						break
					}
				}
			}
		}
	}
	if err != nil || node.ID == "" {
		if errors.Is(err, sql.ErrNoRows) || err == nil {
			writeJSONError(w, http.StatusNotFound, "node not found")
			return
		}
		writeJSONError(w, http.StatusServiceUnavailable, "service unavailable")
		return
	}
	if len(parts) == 6 && parts[4] == "resource" && parts[5] == "history" {
		s.writeNodeHistory(w, r, node.ID, "resource")
		return
	}
	if len(parts) == 6 && parts[4] == "network" && parts[5] == "history" {
		// Network history is read-only telemetry used by the public node detail
		// page. Keep the same window and limit validation as the authenticated
		// endpoint, while exposing only the already-available probe results.
		s.writeNodeHistory(w, r, node.ID, "network")
		return
	}
	if len(parts) == 6 && parts[4] == "checks" && parts[5] == "summary" {
		s.nodeChecksSummary(w, r, uuid)
		return
	}
	if len(parts) == 5 && parts[4] == "traffic" {
		s.nodeTraffic(w, r, uuid)
		return
	}
	if len(parts) == 5 && parts[4] == "media" {
		s.writeMediaLatest(w, r, node.ID)
		return
	}
	if len(parts) == 5 && parts[4] == "ip-quality" {
		s.writePublicIPQuality(w, r, node.ID)
		return
	}
	if len(parts) == 5 && parts[4] == "billing" {
		s.publicNodeBilling(w, r, uuid)
		return
	}
	if len(parts) == 5 && parts[4] == "containers" {
		s.getNodeContainers(w, r, uuid)
		return
	}
	if len(parts) == 5 && parts[4] == "processes" {
		s.getNodeProcesses(w, r, uuid)
		return
	}
	if len(parts) == 5 && parts[4] == "events" {
		s.getNodeEvents(w, r, uuid)
		return
	}
	writeJSONError(w, http.StatusNotFound, "not found")
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
	var resource struct {
		IPQuality *struct {
			IPType string `json:"ip_type,omitempty"`
			Country string `json:"country,omitempty"`
			Region string `json:"region,omitempty"`
			ASN string `json:"asn,omitempty"`
			Organization string `json:"organization,omitempty"`
			Proxy *bool `json:"proxy,omitempty"`
			VPN *bool `json:"vpn,omitempty"`
			Tor *bool `json:"tor,omitempty"`
			Abuse *bool `json:"abuse,omitempty"`
			Risk string `json:"risk,omitempty"`
			CheckedAt int64 `json:"checked_at,omitempty"`
		} `json:"ip_quality,omitempty"`
	}
	if json.Unmarshal(payload, &resource) != nil || resource.IPQuality == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ip_quality": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ip_quality": resource.IPQuality})
}
