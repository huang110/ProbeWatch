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

type publicStatusNodes struct {
	Online int      `json:"online"`
	Total  int      `json:"total"`
	Names  []string `json:"names"`
}

type publicStatusChecks struct {
	SuccessRate  *float64 `json:"success_rate"`
	AvgLatencyMs *float64 `json:"avg_latency_ms"`
}

var (
	publicNameIPv4Pattern = regexp.MustCompile(`(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])(\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])){3}`)
	publicNameUUIDPattern = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
)

const publicNodeNameLimit = 48

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
	if !s.publicCacheAt.IsZero() && now.Sub(s.publicCacheAt) < 5*time.Second {
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
		Nodes:  publicStatusNodes{Names: make([]string, 0, len(nodes))},
		Checks: publicStatusChecks{},
	}
	response.Nodes.Total = len(nodes)
	from := now.Add(-24 * time.Hour)
	limit := 100
	checksTotal, checksSuccess, latencyTotal, latencyCount := 0, 0, int64(0), 0
	seenChecks := make(map[string]struct{})
	var lastUpdated time.Time
	for _, node := range nodes {
		// The resource payload is intentionally discarded here:
		// only the report timestamp is used for the online count.
		if reportedAt, _, e := s.service.Store().GetResourceLatest(r.Context(), node.ID); e == nil {
			if lastUpdated.IsZero() || reportedAt.After(lastUpdated) {
				lastUpdated = reportedAt
			}
			if now.Sub(reportedAt) <= 2*time.Minute {
				response.Nodes.Online++
			}
		}
		response.Nodes.Names = append(response.Nodes.Names, sanitizeNodeName(node.Name))
		for _, kind := range []db.TargetKind{db.TargetKindTCP, db.TargetKindMTR, db.TargetKindMediaHTTP} {
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
					if ok := result.Status == "success" || result.Status == "available" || (kind == db.TargetKindMTR && result.Reached && result.Error == ""); ok {
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
		rate := float64(checksSuccess) * 100 / float64(checksTotal)
		response.Checks.SuccessRate = &rate
		if latencyCount > 0 {
			avg := float64(latencyTotal) / float64(latencyCount)
			response.Checks.AvgLatencyMs = &avg
		}
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
	if !security.IsRFC4122UUID(uuid) {
		writeJSONError(w, http.StatusNotFound, "node not found")
		return
	}
	node, err := s.service.Store().GetNodeByUUID(r.Context(), uuid)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "node not found")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "service unavailable")
		return
	}
	if len(parts) == 6 && parts[4] == "resource" && parts[5] == "history" {
		s.writeNodeHistory(w, r, node.ID, "resource")
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
