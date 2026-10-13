package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
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
	IP             string `json:"ip"`
	Location       string `json:"location"`
	ISP            string `json:"isp"`
	ASN            string `json:"asn"`
	Country        string `json:"country"`
	Region         string `json:"region"`
	City           string `json:"city"`
	Platform       string `json:"platform"`
	Browser        string `json:"browser"`
	BrowserVersion string `json:"browser_version"`
	Connection     string `json:"connection"`
	VisitedAt      string `json:"visited_at"`
	GeoSource      string `json:"geo_source"`
}

type visitorGeoData struct {
	Country  string
	Region   string
	City     string
	Location string
	ISP      string
	ASN      string
}

type visitorGeoCacheEntry struct {
	data      visitorGeoData
	expiresAt time.Time
	ok        bool
}

var (
	uaEdgeRe    = regexp.MustCompile(`(?i)(?:edg|edga|edgios)/([0-9]+(?:\.[0-9]+){0,3})`)
	uaOperaRe   = regexp.MustCompile(`(?i)(?:opr|opera)/([0-9]+(?:\.[0-9]+){0,3})`)
	uaFirefoxRe = regexp.MustCompile(`(?i)(?:firefox|fxios)/([0-9]+(?:\.[0-9]+){0,3})`)
	uaChromeRe  = regexp.MustCompile(`(?i)(?:chrome|crios)/([0-9]+(?:\.[0-9]+){0,3})`)
	uaVersionRe = regexp.MustCompile(`(?i)version/([0-9]+(?:\.[0-9]+){0,3})`)
	uaCurlRe    = regexp.MustCompile(`(?i)curl/([0-9]+(?:\.[0-9]+){0,3})`)
	asnDigitsRe = regexp.MustCompile(`^[0-9]+$`)
)

// publicClientInfo returns only the visitor's connection address and request metadata
// as seen by the server or trusted reverse proxy. It is intentionally separate from
// node telemetry so the public dashboard can label the current visitor without
// exposing node IPs, UUIDs, or tokens.
func (s *Server) publicClientInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ip := s.publicClientIP(r)
	browser, browserVersion := publicClientBrowserAndVersion(r.UserAgent())
	geoData, geoSource := s.resolveVisitorGeo(r.Context(), r, ip)
	response := publicClientInfoResponse{
		IP:             ip,
		Location:       geoData.Location,
		ISP:            geoData.ISP,
		ASN:            geoData.ASN,
		Country:        geoData.Country,
		Region:         geoData.Region,
		City:           geoData.City,
		Platform:       publicClientPlatform(r.UserAgent()),
		Browser:        browser,
		BrowserVersion: browserVersion,
		Connection:     publicClientConnectionFamily(ip),
		VisitedAt:      time.Now().UTC().Format(time.RFC3339),
		GeoSource:      geoSource,
	}
	writeJSON(w, http.StatusOK, response)
}

func publicClientPlatform(userAgent string) string {
	ua := strings.ToLower(userAgent)
	switch {
	case strings.Contains(ua, "windows"):
		return "Windows"
	case strings.Contains(ua, "iphone") || strings.Contains(ua, "ipad") || strings.Contains(ua, "ipod"):
		return "iOS"
	case strings.Contains(ua, "android"):
		return "Android"
	case strings.Contains(ua, "mac os") || strings.Contains(ua, "macintosh"):
		return "macOS"
	case strings.Contains(ua, "linux"):
		return "Linux"
	default:
		return "未知系统"
	}
}

func publicClientBrowser(userAgent string) string {
	browser, _ := publicClientBrowserAndVersion(userAgent)
	return browser
}

func publicClientBrowserAndVersion(userAgent string) (string, string) {
	ua := strings.TrimSpace(userAgent)
	if ua == "" {
		return "未知浏览器", ""
	}
	lower := strings.ToLower(ua)
	if m := uaEdgeRe.FindStringSubmatch(ua); len(m) == 2 {
		return "Edge", m[1]
	}
	if m := uaOperaRe.FindStringSubmatch(ua); len(m) == 2 {
		return "Opera", m[1]
	}
	if m := uaFirefoxRe.FindStringSubmatch(ua); len(m) == 2 {
		return "Firefox", m[1]
	}
	if m := uaChromeRe.FindStringSubmatch(ua); len(m) == 2 {
		return "Chrome", m[1]
	}
	if strings.Contains(lower, "safari/") {
		if m := uaVersionRe.FindStringSubmatch(ua); len(m) == 2 {
			return "Safari", m[1]
		}
		return "Safari", ""
	}
	if m := uaCurlRe.FindStringSubmatch(ua); len(m) == 2 {
		return "curl", m[1]
	}
	return "未知浏览器", ""
}

func publicClientConnectionFamily(ipStr string) string {
	parsed := net.ParseIP(strings.TrimSpace(ipStr))
	if parsed != nil && parsed.To4() == nil {
		return "IPv6"
	}
	return "IPv4"
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
	IPQuality         *publicIPQualityDTO    `json:"ip_quality,omitempty"`
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
				IPQuality *protocol.IPQualityInfo `json:"ip_quality"`
				IPQA      *protocol.IPQAInfo      `json:"ipqa"`
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
				if resource.IPQuality != nil || resource.IPQA != nil {
					telemetry.IPQuality = populatePublicIPQualityDTO(resource.IPQuality, resource.IPQA)
				}
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

func (s *Server) publicLimiterKey(r *http.Request) string {
	if s != nil {
		if clientIP := s.publicClientIP(r); clientIP != "" {
			return clientIP
		}
	}
	return publicLimiterKey(r)
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

func normalizeCanonicalIPString(raw string) string {
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.String()
}

func extractRemotePeerIP(remoteAddr string) net.IP {
	trimmed := strings.TrimSpace(remoteAddr)
	if trimmed == "" {
		return nil
	}
	if host, _, err := net.SplitHostPort(trimmed); err == nil {
		return net.ParseIP(strings.TrimSpace(host))
	}
	return net.ParseIP(trimmed)
}

func isTrustedReverseProxyPeer(remoteAddr string, trustedNets ...*net.IPNet) bool {
	peerIP := extractRemotePeerIP(remoteAddr)
	if peerIP == nil {
		return false
	}
	if peerIP.IsLoopback() {
		return true
	}
	for _, ipNet := range trustedNets {
		if ipNet != nil && ipNet.Contains(peerIP) {
			return true
		}
	}
	return false
}

func (s *Server) isTrustedPeer(remoteAddr string) bool {
	if s == nil {
		return isTrustedReverseProxyPeer(remoteAddr)
	}
	return isTrustedReverseProxyPeer(remoteAddr, s.trustedProxyNets...)
}

func publicClientIP(r *http.Request, trustedNets ...*net.IPNet) string {
	if r == nil {
		return ""
	}
	if isTrustedReverseProxyPeer(r.RemoteAddr, trustedNets...) {
		for _, header := range []string{"X-Real-IP", "CF-Connecting-IP"} {
			if canonical := normalizeCanonicalIPString(r.Header.Get(header)); canonical != "" {
				return canonical
			}
		}
		if forwarded := strings.Split(r.Header.Get("X-Forwarded-For"), ","); len(forwarded) > 0 {
			for _, part := range forwarded {
				if canonical := normalizeCanonicalIPString(part); canonical != "" {
					return canonical
				}
			}
		}
	}
	if peerIP := extractRemotePeerIP(r.RemoteAddr); peerIP != nil {
		if v4 := peerIP.To4(); v4 != nil {
			return v4.String()
		}
		return peerIP.String()
	}
	return ""
}

func (s *Server) publicClientIP(r *http.Request) string {
	if s == nil {
		return publicClientIP(r)
	}
	return publicClientIP(r, s.trustedProxyNets...)
}

func sanitizeClientHeaderText(raw string, maxRunes int) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	var b strings.Builder
	count := 0
	for _, r := range trimmed {
		if r < 32 || r == 127 {
			continue
		}
		b.WriteRune(r)
		count++
		if maxRunes > 0 && count >= maxRunes {
			break
		}
	}
	return strings.TrimSpace(b.String())
}

func firstTrustedHeader(r *http.Request, maxRunes int, names ...string) string {
	for _, name := range names {
		if val := sanitizeClientHeaderText(r.Header.Get(name), maxRunes); val != "" {
			return val
		}
	}
	return ""
}

func publicClientGeoMeta(r *http.Request, trustedNets ...*net.IPNet) (country, region, city, location, isp, asn string) {
	if r == nil || !isTrustedReverseProxyPeer(r.RemoteAddr, trustedNets...) {
		return "", "", "", "", "", ""
	}
	country = firstTrustedHeader(r, 48, "X-Visitor-Country", "X-Geo-Country", "CF-IPCountry", "X-Country-Code")
	if strings.EqualFold(country, "XX") || strings.EqualFold(country, "T1") || strings.EqualFold(country, "unknown") {
		country = ""
	}
	region = firstTrustedHeader(r, 64, "X-Visitor-Region", "X-Geo-Region", "CF-Region", "X-Region")
	if strings.EqualFold(region, "unknown") {
		region = ""
	}
	city = firstTrustedHeader(r, 64, "X-Visitor-City", "X-Geo-City", "CF-IPCity", "X-City")
	if strings.EqualFold(city, "unknown") {
		city = ""
	}
	isp = firstTrustedHeader(r, 96, "X-Visitor-ISP", "X-Geo-ISP", "CF-ISP", "X-ISP")
	if strings.EqualFold(isp, "unknown") {
		isp = ""
	}
	asn = firstTrustedHeader(r, 32, "X-Visitor-ASN", "X-Geo-ASN", "CF-IPASN", "X-ASN")
	if strings.EqualFold(asn, "unknown") {
		asn = ""
	} else if asnDigitsRe.MatchString(asn) {
		asn = "AS" + asn
	} else if strings.HasPrefix(strings.ToLower(asn), "as") && asnDigitsRe.MatchString(asn[2:]) {
		asn = "AS" + asn[2:]
	}
	location = firstTrustedHeader(r, 128, "X-Visitor-Location", "X-Geo-Location")
	if location == "" {
		parts := make([]string, 0, 3)
		for _, p := range []string{country, region, city} {
			if p == "" {
				continue
			}
			dup := false
			for _, existing := range parts {
				if strings.EqualFold(existing, p) {
					dup = true
					break
				}
			}
			if !dup {
				parts = append(parts, p)
			}
		}
		location = strings.Join(parts, " · ")
	}
	return country, region, city, location, isp, asn
}

func (s *Server) resolveVisitorGeo(ctx context.Context, r *http.Request, clientIP string) (visitorGeoData, string) {
	if s == nil {
		return visitorGeoData{}, "unconfigured"
	}
	// 1. Check reverse proxy Geo headers ONLY if peer is trusted AND TrustProxyGeoHeaders is true
	if s.cfg.TrustProxyGeoHeaders && s.isTrustedPeer(r.RemoteAddr) {
		country, region, city, location, isp, asn := publicClientGeoMeta(r, s.trustedProxyNets...)
		if location != "" || isp != "" || asn != "" || country != "" || region != "" || city != "" {
			return visitorGeoData{
				Country:  country,
				Region:   region,
				City:     city,
				Location: location,
				ISP:      isp,
				ASN:      asn,
			}, "proxy"
		}
	}

	// 2. Check server-side visitor GeoIP lookup if enabled
	if s.cfg.EnableVisitorGeoIP {
		if data, ok := s.lookupVisitorGeoCached(ctx, clientIP); ok {
			return data, "geoip"
		}
		return visitorGeoData{}, "geoip"
	}

	if s.cfg.TrustProxyGeoHeaders {
		return visitorGeoData{}, "proxy"
	}
	return visitorGeoData{}, "unconfigured"
}

func isGeoRoutableVisitorIP(ipStr string) bool {
	ip := net.ParseIP(strings.TrimSpace(ipStr))
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	return true
}

func (s *Server) lookupVisitorGeoCached(ctx context.Context, clientIP string) (visitorGeoData, bool) {
	trimmed := strings.TrimSpace(clientIP)
	if trimmed == "" {
		return visitorGeoData{}, false
	}
	if s.visitorGeoFetch == nil && !isGeoRoutableVisitorIP(trimmed) {
		return visitorGeoData{}, false
	}

	now := time.Now()
	s.visitorGeoMu.RLock()
	entry, found := s.visitorGeoCache[trimmed]
	s.visitorGeoMu.RUnlock()
	if found && now.Before(entry.expiresAt) {
		return entry.data, entry.ok
	}

	var data visitorGeoData
	var err error
	if s.visitorGeoFetch != nil {
		data, err = s.visitorGeoFetch(ctx, trimmed)
	} else {
		data, err = s.fetchVisitorGeoHTTP(ctx, trimmed)
	}

	s.visitorGeoMu.Lock()
	defer s.visitorGeoMu.Unlock()
	if len(s.visitorGeoCache) > 2048 {
		for k, v := range s.visitorGeoCache {
			if now.After(v.expiresAt) {
				delete(s.visitorGeoCache, k)
			}
		}
	}
	if err == nil && (data.Location != "" || data.ISP != "" || data.ASN != "" || data.Country != "") {
		s.visitorGeoCache[trimmed] = visitorGeoCacheEntry{
			data:      data,
			expiresAt: now.Add(30 * time.Minute),
			ok:        true,
		}
		return data, true
	}

	s.visitorGeoCache[trimmed] = visitorGeoCacheEntry{
		data:      visitorGeoData{},
		expiresAt: now.Add(1 * time.Minute),
		ok:        false,
	}
	return visitorGeoData{}, false
}

func (s *Server) fetchVisitorGeoHTTP(ctx context.Context, clientIP string) (visitorGeoData, error) {
	template := strings.TrimSpace(s.cfg.VisitorGeoIPURL)
	if template == "" {
		template = "https://ipwho.is/{ip}?lang=zh-CN"
	}
	targetURL := template
	if strings.Contains(targetURL, "{ip}") {
		targetURL = strings.ReplaceAll(targetURL, "{ip}", url.PathEscape(clientIP))
	} else {
		targetURL = strings.TrimRight(targetURL, "/") + "/" + url.PathEscape(clientIP)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return visitorGeoData{}, err
	}
	req.Header.Set("User-Agent", "ProbeWatch-Server/0.9.3")
	req.Header.Set("Accept", "application/json")

	client := s.visitorGeoClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return visitorGeoData{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return visitorGeoData{}, fmt.Errorf("geoip provider status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 128*1024))
	if err != nil {
		return visitorGeoData{}, err
	}
	return parseVisitorGeoResponse(body)
}

func parseVisitorGeoResponse(body []byte) (visitorGeoData, error) {
	if len(body) == 0 {
		return visitorGeoData{}, errors.New("empty response")
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return visitorGeoData{}, err
	}
	if success, ok := raw["success"].(bool); ok && !success {
		return visitorGeoData{}, errors.New("provider reported failure")
	}
	if isErr, ok := raw["error"].(bool); ok && isErr {
		return visitorGeoData{}, errors.New("provider returned error")
	}

	country := extractStringField(raw, "country", "country_name", "countryCode", "country_code")
	if strings.EqualFold(country, "unknown") || strings.EqualFold(country, "XX") {
		country = ""
	}
	region := extractStringField(raw, "region", "regionName", "region_name", "state")
	if strings.EqualFold(region, "unknown") {
		region = ""
	}
	city := extractStringField(raw, "city")
	if strings.EqualFold(city, "unknown") {
		city = ""
	}

	var asn string
	if rawASN := extractFieldAny(raw, "connection.asn", "asn", "as.number"); rawASN != nil {
		asn = formatASNString(rawASN)
	}

	ispVal := extractStringField(raw, "connection.isp", "isp", "isp.isp")
	orgVal := extractStringField(raw, "connection.org", "org", "organization", "company", "isp.org")
	isp := pickGeoISP(ispVal, orgVal)

	location := extractStringField(raw, "location")
	if location == "" {
		parts := make([]string, 0, 3)
		for _, p := range []string{country, region, city} {
			if p == "" {
				continue
			}
			dup := false
			for _, existing := range parts {
				if strings.EqualFold(existing, p) {
					dup = true
					break
				}
			}
			if !dup {
				parts = append(parts, p)
			}
		}
		location = strings.Join(parts, " · ")
	}

	return visitorGeoData{
		Country:  country,
		Region:   region,
		City:     city,
		Location: location,
		ISP:      isp,
		ASN:      asn,
	}, nil
}

func extractFieldAny(m map[string]any, paths ...string) any {
	for _, p := range paths {
		parts := strings.Split(p, ".")
		var current any = m
		found := true
		for _, part := range parts {
			currMap, ok := current.(map[string]any)
			if !ok {
				found = false
				break
			}
			current, ok = currMap[part]
			if !ok {
				found = false
				break
			}
		}
		if found && current != nil {
			return current
		}
	}
	return nil
}

func extractStringField(m map[string]any, paths ...string) string {
	val := extractFieldAny(m, paths...)
	if val == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(val))
}

func formatASNString(val any) string {
	if val == nil {
		return ""
	}
	str := strings.TrimSpace(fmt.Sprint(val))
	if str == "" || strings.EqualFold(str, "0") {
		return ""
	}
	if asnDigitsRe.MatchString(str) {
		return "AS" + str
	}
	if strings.HasPrefix(strings.ToLower(str), "as") && asnDigitsRe.MatchString(str[2:]) {
		return "AS" + str[2:]
	}
	return str
}

func pickGeoISP(ispVal, orgVal string) string {
	ispVal = strings.TrimSpace(ispVal)
	orgVal = strings.TrimSpace(orgVal)
	lowerISP := strings.ToLower(ispVal)
	if ispVal == "" || strings.HasPrefix(lowerISP, "no.") || strings.Contains(lowerISP, "street") || strings.Contains(lowerISP, "road") {
		if orgVal != "" {
			return orgVal
		}
	}
	if ispVal != "" {
		return ispVal
	}
	return orgVal
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
	IPType              string             `json:"ip_type,omitempty"`
	Country             string             `json:"country,omitempty"`
	Region              string             `json:"region,omitempty"`
	City                string             `json:"city,omitempty"`
	ASN                 string             `json:"asn,omitempty"`
	Organization        string             `json:"organization,omitempty"`
	Risk                string             `json:"risk,omitempty"`
	Proxy               *bool              `json:"proxy,omitempty"`
	VPN                 *bool              `json:"vpn,omitempty"`
	Tor                 *bool              `json:"tor,omitempty"`
	Abuse               *bool              `json:"abuse,omitempty"`
	Scores              map[string]float64 `json:"scores,omitempty"`
	Flags               map[string]*bool   `json:"flags,omitempty"`
	CheckedAt           int64              `json:"checked_at,omitempty"`
	Stale               bool               `json:"stale"`
	Source              string             `json:"source,omitempty"`
	IPQAEnabled         bool               `json:"ipqa_enabled"`
	IPQAInstalled       bool               `json:"ipqa_installed"`
	IPQAState           string             `json:"ipqa_state,omitempty"`
	HighestSeverity     string             `json:"highest_severity,omitempty"`
	AlertCount          int                `json:"alert_count,omitempty"`
	CriticalCount       int                `json:"critical_count,omitempty"`
	WarningCount        int                `json:"warning_count,omitempty"`
	InfoCount           int                `json:"info_count,omitempty"`
	LastCheckedAt       *int64             `json:"last_checked_at,omitempty"`
	HasRecentChanges    bool               `json:"has_recent_changes,omitempty"`
	RecentChangeSummary string             `json:"recent_change_summary,omitempty"`
	LatestArchiveDate   string             `json:"latest_archive_date,omitempty"`
	Sources             []string           `json:"sources,omitempty"`
	RiskScore           *float64           `json:"risk_score,omitempty"`
	RiskLevel           string                   `json:"risk_level,omitempty"`
	HasIPv4             bool                     `json:"has_ipv4,omitempty"`
	HasIPv6             bool                     `json:"has_ipv6,omitempty"`
	IPv4                *protocol.IPQAFamilyInfo `json:"ipv4,omitempty"`
	IPv6                *protocol.IPQAFamilyInfo `json:"ipv6,omitempty"`
	Media               []protocol.IPQAMediaItem `json:"media,omitempty"`
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

	// Collect public media results: prioritize real IPQA archive results
	mediaList := make([]publicMediaResultDTO, 0)
	if snapshot.IPQA != nil && len(snapshot.IPQA.Media) > 0 {
		for _, m := range snapshot.IPQA.Media {
			mediaList = append(mediaList, publicMediaResultDTO{
				DetectorID: "ipqa-" + strings.ToLower(m.Name),
				Detector:   m.Name,
				Status:     m.Status,
				Region:     m.Region,
				Reason:     m.Type,
				CheckedAt:  m.CheckedAt,
			})
		}
	} else {
		mediaResults, err := s.service.Store().ListMediaLatest(r.Context(), node.ID)
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
	now := time.Now().UTC().Unix()
	if q != nil {
		dto.IPType = q.IPType
		dto.Country = q.Country
		dto.Region = q.Region
		dto.City = q.City
		dto.ASN = q.ASN
		dto.Organization = q.Organization
		dto.Risk = q.Risk
		dto.Proxy = q.Proxy
		dto.VPN = q.VPN
		dto.Tor = q.Tor
		dto.Abuse = q.Abuse
		dto.CheckedAt = q.CheckedAt
		if len(q.Sources) > 0 {
			dto.Scores = q.Sources
		}
		dto.Flags = map[string]*bool{
			"proxy": q.Proxy,
			"vpn":   q.VPN,
			"tor":   q.Tor,
			"abuse": q.Abuse,
		}
		if q.CheckedAt > 0 {
			dto.Stale = (now - q.CheckedAt) > int64(12*time.Hour/time.Second)
		}
		dto.Source = "ipwho.is"
	}
	if qa != nil {
		dto.IPQAEnabled = qa.Enabled
		dto.IPQAInstalled = qa.Installed
		dto.IPQAState = qa.State
		dto.HighestSeverity = qa.HighestSeverity
		dto.AlertCount = qa.AlertCount
		dto.CriticalCount = qa.CriticalCount
		dto.WarningCount = qa.WarningCount
		dto.InfoCount = qa.InfoCount
		dto.LatestArchiveDate = qa.LatestArchiveDate
		if len(qa.Sources) > 0 {
			dto.Sources = qa.Sources
		}
		if len(qa.Media) > 0 {
			dto.Media = qa.Media
		}
		dto.HasIPv4 = qa.IPv4 != nil
		dto.HasIPv6 = qa.IPv6 != nil
		dto.IPv4 = qa.IPv4
		dto.IPv6 = qa.IPv6
		primary := qa.IPv4
		secondary := qa.IPv6
		if primary == nil {
			primary = qa.IPv6
			secondary = nil
		}
		if primary != nil {
			if primary.RiskScore != nil {
				dto.RiskScore = primary.RiskScore
			} else if secondary != nil && secondary.RiskScore != nil {
				dto.RiskScore = secondary.RiskScore
			}
			if primary.RiskLevel != "" {
				dto.RiskLevel = primary.RiskLevel
				if dto.Risk == "" || dto.Risk == "unknown" {
					dto.Risk = primary.RiskLevel
				}
			} else if secondary != nil && secondary.RiskLevel != "" {
				dto.RiskLevel = secondary.RiskLevel
				if dto.Risk == "" || dto.Risk == "unknown" {
					dto.Risk = secondary.RiskLevel
				}
			}
			if len(primary.SourceScores) > 0 && len(dto.Scores) == 0 {
				dto.Scores = primary.SourceScores
			} else if secondary != nil && len(secondary.SourceScores) > 0 && len(dto.Scores) == 0 {
				dto.Scores = secondary.SourceScores
			}
		}
		if qa.LastCheckedAt > 0 {
			dto.LastCheckedAt = &qa.LastCheckedAt
			if dto.CheckedAt == 0 {
				dto.CheckedAt = qa.LastCheckedAt
			}
		}
		dto.HasRecentChanges = len(qa.Changes) > 0
		if len(qa.Changes) > 0 {
			dto.RecentChangeSummary = qa.Changes[0].After
			if dto.RecentChangeSummary == "" {
				dto.RecentChangeSummary = qa.Changes[0].Category
			}
		}
		if qa.Enabled && qa.Installed {
			dto.Source = "IPQA"
			if primary != nil && primary.RiskLevel != "" && primary.RiskLevel != "unknown" {
				dto.Risk = primary.RiskLevel
			}
			if primary != nil && primary.IPType != "" && primary.IPType != "unknown" {
				dto.IPType = primary.IPType
			}
			if primary != nil && len(primary.SourceScores) > 0 {
				dto.Scores = primary.SourceScores
			} else if secondary != nil && len(secondary.SourceScores) > 0 {
				dto.Scores = secondary.SourceScores
			}
			if dto.Flags == nil {
				dto.Flags = make(map[string]*bool)
			}
			if primary != nil && primary.Proxy != nil {
				dto.Proxy = primary.Proxy
				dto.Flags["proxy"] = primary.Proxy
			}
			if primary != nil && primary.VPN != nil {
				dto.VPN = primary.VPN
				dto.Flags["vpn"] = primary.VPN
			}
			if primary != nil && primary.Tor != nil {
				dto.Tor = primary.Tor
				dto.Flags["tor"] = primary.Tor
			}
			if primary != nil && primary.Abuse != nil {
				dto.Abuse = primary.Abuse
				dto.Flags["abuse"] = primary.Abuse
			}
		}
		if (dto.IPType == "" || dto.IPType == "unknown") && primary != nil && primary.IPType != "" {
			dto.IPType = primary.IPType
		}
		if dto.Country == "" && primary != nil && primary.Country != "" {
			dto.Country = primary.Country
		}
		if dto.Region == "" && primary != nil && primary.Region != "" {
			dto.Region = primary.Region
		}
		if dto.City == "" && primary != nil && primary.Region != "" {
			dto.City = primary.Region
		}
		if dto.ASN == "" && primary != nil && primary.ASN != "" {
			dto.ASN = primary.ASN
		}
		if dto.Organization == "" && primary != nil && primary.Organization != "" {
			dto.Organization = primary.Organization
		}
		if dto.Flags == nil {
			dto.Flags = make(map[string]*bool)
		}
		if dto.Proxy == nil && primary != nil && primary.Proxy != nil {
			dto.Proxy = primary.Proxy
			dto.Flags["proxy"] = primary.Proxy
		}
		if dto.VPN == nil && primary != nil && primary.VPN != nil {
			dto.VPN = primary.VPN
			dto.Flags["vpn"] = primary.VPN
		}
		if dto.Tor == nil && primary != nil && primary.Tor != nil {
			dto.Tor = primary.Tor
			dto.Flags["tor"] = primary.Tor
		}
		if dto.Abuse == nil && primary != nil && primary.Abuse != nil {
			dto.Abuse = primary.Abuse
			dto.Flags["abuse"] = primary.Abuse
		}
	}
	if dto.IPType == "" {
		dto.IPType = "unknown"
	}
	if dto.Risk == "" {
		dto.Risk = "unknown"
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
	_, payload, err := s.service.Store().GetResourceLatest(r.Context(), nodeID)
	if err == nil {
		var snapshot protocol.ResourceSnapshot
		if json.Unmarshal(payload, &snapshot) == nil && snapshot.IPQA != nil && len(snapshot.IPQA.Media) > 0 {
			mediaList := make([]publicMediaResultDTO, 0, len(snapshot.IPQA.Media))
			for _, m := range snapshot.IPQA.Media {
				mediaList = append(mediaList, publicMediaResultDTO{
					DetectorID: "ipqa-" + strings.ToLower(m.Name),
					Detector:   m.Name,
					Status:     m.Status,
					Region:     m.Region,
					Reason:     m.Type,
					CheckedAt:  m.CheckedAt,
				})
			}
			writeJSON(w, http.StatusOK, mediaList)
			return
		}
	}

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

