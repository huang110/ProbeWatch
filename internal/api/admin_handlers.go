package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/protocol"
	"github.com/probewatch/probewatch/internal/security"
)

type alertResponse struct {
	ID              string     `json:"id"`
	NodeID          string     `json:"node_id"`
	Category        string     `json:"category"`
	TargetID        string     `json:"target_id"`
	Reason          string     `json:"reason"`
	Severity        string     `json:"severity"`
	Status          string     `json:"status"`
	OccurrenceCount int        `json:"occurrence_count"`
	FirstSeenAt     time.Time  `json:"first_seen_at"`
	LastSeenAt      time.Time  `json:"last_seen_at"`
	ResolvedAt      *time.Time `json:"resolved_at,omitempty"`
}

func alertResponseFrom(a db.AlertEvent) alertResponse {
	return alertResponse{ID: a.ID, NodeID: a.NodeID, Category: a.Category, TargetID: a.TargetID, Reason: a.Reason, Severity: a.Severity, Status: a.Status, OccurrenceCount: a.OccurrenceCount, FirstSeenAt: a.FirstSeenAt, LastSeenAt: a.LastSeenAt, ResolvedAt: a.ResolvedAt}
}

func (s *Server) alertRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == "/api/alerts" {
		s.listAlerts(w, r)
		return
	}
	if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/ack") {
		NewMiddleware(s.service, s.cfg).RequireCSRF(http.HandlerFunc(s.ackAlert)).ServeHTTP(w, r)
		return
	}
	writeJSONError(w, http.StatusNotFound, "not found")
}

func (s *Server) listAlerts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	q := r.URL.Query()
	statuses := q["status"]
	if len(statuses) == 1 && strings.Contains(statuses[0], ",") {
		statuses = strings.Split(statuses[0], ",")
	}
	if len(statuses) == 0 {
		statuses = []string{db.AlertStatusOpen, db.AlertStatusAcked}
	}
	for _, status := range statuses {
		if status != db.AlertStatusOpen && status != db.AlertStatusAcked && status != db.AlertStatusResolved {
			writeJSONError(w, http.StatusBadRequest, "invalid status")
			return
		}
	}
	from, to, limit, ok := parseHistoryWindow(r, time.Now().UTC())
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "invalid query parameters")
		return
	}
	alerts, err := s.service.Store().ListAlerts(r.Context(), db.AlertQuery{Statuses: statuses, From: from, To: to, Limit: limit})
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "service unavailable")
		return
	}
	out := make([]alertResponse, 0, len(alerts))
	for _, a := range alerts {
		out = append(out, alertResponseFrom(a))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) ackAlert(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 4 || parts[0] != "api" || parts[1] != "alerts" || parts[3] != "ack" || parts[2] == "" {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, err := s.service.AuthenticateWithError(r, time.Now().UTC())
	if err != nil {
		writeAuthenticationError(w, err)
		return
	}
	a, err := s.service.Store().AckAlert(r.Context(), parts[2], session.AdminUserID, time.Now().UTC())
	if errors.Is(err, db.ErrAlertNotFound) {
		writeJSONError(w, http.StatusNotFound, "alert not found")
		return
	}
	if errors.Is(err, db.ErrAlertResolved) {
		writeJSONError(w, http.StatusConflict, "alert already resolved")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "service unavailable")
		return
	}
	writeJSON(w, http.StatusOK, alertResponseFrom(a))
}

type registrationTokenResponse struct {
	RegistrationToken string    `json:"registration_token"`
	Endpoint          string    `json:"endpoint"`
	ExpiresAt         time.Time `json:"expires_at"`
}

type nodeResponse struct {
	ID             string          `json:"id"`
	UUID           string          `json:"uuid"`
	Name           string          `json:"name"`
	Status         string          `json:"status"`
	LastReportedAt *time.Time      `json:"last_reported_at,omitempty"`
	Resource       json.RawMessage `json:"resource,omitempty"`
}

type networkLatestResponse struct {
	TargetID  string                 `json:"target_id"`
	CheckedAt time.Time              `json:"checked_at"`
	Result    protocol.NetworkResult `json:"result"`
}

func (s *Server) createRegistrationToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var request struct{}
	if err := decodeJSONObjectRequest(w, r, s.requestBodyLimit(), &request); err != nil {
		writeRequestError(w, err)
		return
	}
	registration, err := s.service.Store().CreateRegistrationToken(r.Context(), s.agentTokenTTL())
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "service unavailable")
		return
	}
	endpoint := strings.TrimRight(s.cfg.PublicBaseURL, "/") + "/api/agent/v1"
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(registrationTokenResponse{RegistrationToken: registration.Token, Endpoint: endpoint, ExpiresAt: registration.ExpiresAt})
}

func (s *Server) listNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	nodes, err := s.service.Store().ListNodes(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "service unavailable")
		return
	}
	response := make([]nodeResponse, 0, len(nodes))
	for _, node := range nodes {
		response = append(response, s.nodeSummary(r.Context(), node))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) nodeSummary(ctx context.Context, node db.Node) nodeResponse {
	response := nodeResponse{ID: node.ID, UUID: node.UUID, Name: node.Name, Status: "offline"}
	reportedAt, payload, err := s.service.Store().GetResourceLatest(ctx, node.ID)
	if err != nil {
		return response
	}
	response.LastReportedAt = &reportedAt
	if time.Since(reportedAt) <= 2*time.Minute {
		response.Status = "online"
	} else if time.Since(reportedAt) <= 10*time.Minute {
		response.Status = "attention"
	}
	if json.Valid(payload) {
		response.Resource = json.RawMessage(payload)
	}
	return response
}

func (s *Server) nodeAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	nodeID, action, ok := parseNodeActionPath(r.URL.Path)
	if !ok {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	var request struct{}
	if err := decodeJSONObjectRequest(w, r, s.requestBodyLimit(), &request); err != nil {
		writeRequestError(w, err)
		return
	}
	session, err := s.service.AuthenticateWithError(r, time.Now().UTC())
	if err != nil {
		writeAuthenticationError(w, err)
		return
	}
	now := time.Now().UTC()
	switch action {
	case "rotate-token":
		token, err := s.service.Store().RotateNodeTokenWithTTL(r.Context(), nodeID, session.AdminUserID, now, s.agentNodeTokenTTL())
		if err != nil {
			writeNodeActionError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"node_token": token})
	case "revoke":
		if err := s.service.Store().RevokeNodeToken(r.Context(), nodeID, session.AdminUserID, now); err != nil {
			writeNodeActionError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type mtrLatestResponse struct {
	TargetID  string             `json:"target_id"`
	CheckedAt time.Time          `json:"checked_at"`
	Result    protocol.MTRResult `json:"result"`
}

type mediaLatestResponse struct {
	DetectorID string               `json:"detector_id"`
	CheckedAt  time.Time            `json:"checked_at"`
	Result     protocol.MediaResult `json:"result"`
}

func parseHistoryWindow(r *http.Request, now time.Time) (time.Time, time.Time, int, bool) {
	q := r.URL.Query()
	limit := 100
	if v := q.Get("limit"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 || n > 1000 {
			return time.Time{}, time.Time{}, 0, false
		}
		limit = n
	}
	from, to := now.Add(-24*time.Hour), now
	var e error
	if v := q.Get("from"); v != "" {
		from, e = time.Parse(time.RFC3339, v)
		if e != nil {
			return time.Time{}, time.Time{}, 0, false
		}
	}
	if v := q.Get("to"); v != "" {
		to, e = time.Parse(time.RFC3339, v)
		if e != nil {
			return time.Time{}, time.Time{}, 0, false
		}
	}
	if from.After(to) {
		return time.Time{}, time.Time{}, 0, false
	}
	return from.UTC(), to.UTC(), limit, true
}

func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, 405, "method not allowed")
		return
	}
	now := time.Now().UTC()
	from, to, limit, ok := parseHistoryWindow(r, now)
	if !ok {
		writeJSONError(w, 400, "invalid query parameters")
		return
	}
	nodes, err := s.service.Store().ListNodes(r.Context())
	if err != nil {
		writeJSONError(w, 503, "service unavailable")
		return
	}
	nc := map[string]int{"total": len(nodes), "online": 0, "attention": 0, "offline": 0, "resource_reporting": 0}
	checksTotal, checksSuccess, latencyTotal, latencyCount := 0, 0, int64(0), 0
	resources := map[string]any{"network_rx_bytes": uint64(0), "network_tx_bytes": uint64(0), "network_rx_bytes_delta": uint64(0), "network_tx_bytes_delta": uint64(0), "network_history": []any{}, "cpu_percent": nil, "memory_used_bytes": uint64(0), "memory_total_bytes": uint64(0), "filesystem_used_bytes": uint64(0), "filesystem_total_bytes": uint64(0)}
	var cpuTotal float64
	var resourceCount int
	seenChecks := make(map[string]struct{})
	var rxFirst, txFirst, rxLast, txLast uint64
	var haveCounters bool

	for _, n := range nodes {
		at, _, e := s.service.Store().GetResourceLatest(r.Context(), n.ID)
		if e != nil {
			nc["offline"]++
			continue
		}
		age := now.Sub(at)
		if age <= 2*time.Minute {
			nc["online"]++
		} else if age <= 10*time.Minute {
			nc["attention"]++
		} else {
			nc["offline"]++
		}
		if !at.Before(from) && !at.After(to) {
			nc["resource_reporting"]++
		}
		if records, e := s.service.Store().GetResourceHistory(r.Context(), n.ID, from, to, limit); e == nil {
			for _, record := range records {
				var value struct {
					CPU             float64 `json:"cpu_percent"`
					Rx              uint64  `json:"network_rx_bytes"`
					Tx              uint64  `json:"network_tx_bytes"`
					MemoryUsed      uint64  `json:"memory_used_bytes"`
					MemoryTotal     uint64  `json:"memory_total_bytes"`
					FilesystemUsed  uint64  `json:"filesystem_used_bytes"`
					FilesystemTotal uint64  `json:"filesystem_total_bytes"`
				}
				if json.Unmarshal(record.Payload, &value) == nil {
					if !haveCounters {
						rxFirst, txFirst, haveCounters = value.Rx, value.Tx, true
					}
					rxLast, txLast = value.Rx, value.Tx
					cpuTotal += value.CPU
					resourceCount++
					resources["memory_used_bytes"] = resources["memory_used_bytes"].(uint64) + value.MemoryUsed
					resources["memory_total_bytes"] = resources["memory_total_bytes"].(uint64) + value.MemoryTotal
					resources["filesystem_used_bytes"] = resources["filesystem_used_bytes"].(uint64) + value.FilesystemUsed
					resources["filesystem_total_bytes"] = resources["filesystem_total_bytes"].(uint64) + value.FilesystemTotal
				}
			}

		}
		for _, kind := range []db.TargetKind{db.TargetKindTCP, db.TargetKindMTR, db.TargetKindMediaHTTP} {
			if records, e := s.service.Store().GetResultHistory(r.Context(), kind, n.ID, from, to, limit); e == nil {
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
	}
	checks := map[string]any{"total": checksTotal, "success": checksSuccess, "failure": checksTotal - checksSuccess, "success_rate": nil, "avg_latency_ms": nil}
	if checksTotal > 0 {
		checks["success_rate"] = float64(checksSuccess) * 100 / float64(checksTotal)
		if latencyCount > 0 {
			checks["avg_latency_ms"] = float64(latencyTotal) / float64(latencyCount)
		}
	}
	if resourceCount > 0 {
		resources["cpu_percent"] = cpuTotal / float64(resourceCount)
	}
	if haveCounters {
		resources["network_rx_bytes"] = rxLast
		resources["network_tx_bytes"] = txLast
		if rxLast >= rxFirst {
			resources["network_rx_bytes_delta"] = rxLast - rxFirst
		}
		if txLast >= txFirst {
			resources["network_tx_bytes_delta"] = txLast - txFirst
		}
	}

	database := map[string]any{
		"file_bytes":  uint64(0),
		"wal_bytes":   uint64(0),
		"shm_bytes":   uint64(0),
		"total_bytes": uint64(0),
	}
	if s.cfg.DatabasePath != "" {
		if fi, err := os.Stat(s.cfg.DatabasePath); err == nil {
			database["file_bytes"] = uint64(fi.Size())
		}
		if fi, err := os.Stat(s.cfg.DatabasePath + "-wal"); err == nil {
			database["wal_bytes"] = uint64(fi.Size())
		}
		if fi, err := os.Stat(s.cfg.DatabasePath + "-shm"); err == nil {
			database["shm_bytes"] = uint64(fi.Size())
		}
		database["total_bytes"] = database["file_bytes"].(uint64) + database["wal_bytes"].(uint64) + database["shm_bytes"].(uint64)
	}

	targetsCount := 0
	for _, kind := range targetKinds() {
		if tgts, err := s.service.Store().ListTargets(r.Context(), kind); err == nil {
			targetsCount += len(tgts)
		}
	}

	writeJSON(w, 200, map[string]any{
		"nodes":         nc,
		"checks":        checks,
		"resources":     resources,
		"database":      database,
		"targets_count": targetsCount,
		"window":        map[string]time.Time{"from": from, "to": to},
		"generated_at":  now,
	})
}

func (s *Server) nodeSummaryRead(w http.ResponseWriter, r *http.Request, uuid string) {
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
		writeJSONError(w, http.StatusServiceUnavailable, "node unavailable")
		return
	}
	writeJSON(w, http.StatusOK, s.nodeSummary(r.Context(), node))
}

type historyResourceResponse struct {
	ReportedAt time.Time       `json:"reported_at"`
	Resource   json.RawMessage `json:"resource"`
}

type historyResultResponse struct {
	TargetID   string          `json:"target_id,omitempty"`
	DetectorID string          `json:"detector_id,omitempty"`
	CheckedAt  time.Time       `json:"checked_at"`
	Result     json.RawMessage `json:"result"`
}

func (s *Server) writeNodeHistory(w http.ResponseWriter, r *http.Request, nodeID, resource string) {
	from, to, limit, ok := parseHistoryWindow(r, time.Now().UTC())
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "invalid query parameters")
		return
	}
	if resource == "resource" {
		records, err := s.service.Store().GetResourceHistory(r.Context(), nodeID, from, to, limit)
		if err != nil {
			writeJSONError(w, http.StatusServiceUnavailable, "node resource history unavailable")
			return
		}
		out := make([]historyResourceResponse, 0, len(records))
		for _, record := range records {
			out = append(out, historyResourceResponse{ReportedAt: record.ReportedAt, Resource: json.RawMessage(record.Payload)})
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	kind := map[string]db.TargetKind{"network": db.TargetKindTCP, "mtr": db.TargetKindMTR, "media": db.TargetKindMediaHTTP}[resource]
	records, err := s.service.Store().GetResultHistory(r.Context(), kind, nodeID, from, to, limit)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "node results history unavailable")
		return
	}
	out := make([]historyResultResponse, 0, len(records))
	for _, record := range records {
		item := historyResultResponse{CheckedAt: record.CheckedAt, Result: json.RawMessage(record.Payload)}
		if resource == "media" {
			item.DetectorID = record.TargetID
		} else {
			item.TargetID = record.TargetID
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) nodeRead(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if (len(parts) != 4 && len(parts) != 5) || parts[0] != "api" || parts[1] != "nodes" || parts[2] == "" || (len(parts) == 4 && parts[3] != "mtr" && parts[3] != "media" && parts[3] != "resource" && parts[3] != "network") || (len(parts) == 5 && parts[4] != "history") {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	if !security.IsRFC4122UUID(parts[2]) {
		writeJSONError(w, http.StatusNotFound, "node not found")
		return
	}
	node, err := s.service.Store().GetNodeByUUID(r.Context(), parts[2])
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "node not found")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "node results unavailable")
		return
	}
	if len(parts) == 5 {
		s.writeNodeHistory(w, r, node.ID, parts[3])
		return
	}

	if parts[3] == "mtr" {
		s.writeMTRLatest(w, r, node.ID)
		return
	}
	if parts[3] == "media" {
		s.writeMediaLatest(w, r, node.ID)
		return
	}
	if parts[3] == "resource" {
		s.writeResourceLatest(w, r, node.ID)
		return
	}
	s.writeNetworkLatest(w, r, node.ID)
}

func (s *Server) writeResourceLatest(w http.ResponseWriter, r *http.Request, nodeID string) {
	reportedAt, payload, err := s.service.Store().GetResourceLatest(r.Context(), nodeID)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{"reported_at": nil, "resource": nil})
		return
	}
	if err != nil || !json.Valid(payload) {
		writeJSONError(w, http.StatusServiceUnavailable, "node resource unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reported_at": reportedAt, "resource": json.RawMessage(payload)})
}

func (s *Server) writeNetworkLatest(w http.ResponseWriter, r *http.Request, nodeID string) {
	results, err := s.service.Store().ListNetworkLatest(r.Context(), nodeID)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "node network unavailable")
		return
	}
	response := make([]networkLatestResponse, 0, len(results))
	for _, latest := range results {
		var result protocol.NetworkResult
		if err := json.Unmarshal(latest.Payload, &result); err != nil {
			writeJSONError(w, http.StatusServiceUnavailable, "node network unavailable")
			return
		}
		response = append(response, networkLatestResponse{TargetID: latest.ID, CheckedAt: latest.CheckedAt, Result: result})
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) writeMTRLatest(w http.ResponseWriter, r *http.Request, nodeID string) {
	results, err := s.service.Store().ListMTRLatest(r.Context(), nodeID)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "node results unavailable")
		return
	}
	response := make([]mtrLatestResponse, 0, len(results))
	for _, latest := range results {
		var result protocol.MTRResult
		if err := json.Unmarshal(latest.Payload, &result); err != nil {
			writeJSONError(w, http.StatusServiceUnavailable, "node results unavailable")
			return
		}
		response = append(response, mtrLatestResponse{TargetID: latest.ID, CheckedAt: latest.CheckedAt, Result: result})
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) writeMediaLatest(w http.ResponseWriter, r *http.Request, nodeID string) {
	results, err := s.service.Store().ListMediaLatest(r.Context(), nodeID)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "node results unavailable")
		return
	}
	response := make([]mediaLatestResponse, 0, len(results))
	for _, latest := range results {
		var result protocol.MediaResult
		if err := json.Unmarshal(latest.Payload, &result); err != nil {
			writeJSONError(w, http.StatusServiceUnavailable, "node results unavailable")
			return
		}
		response = append(response, mediaLatestResponse{DetectorID: latest.ID, CheckedAt: latest.CheckedAt, Result: result})
	}
	writeJSON(w, http.StatusOK, response)
}

func parseNodeActionPath(path string) (string, string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 4 || parts[0] != "api" || parts[1] != "nodes" || parts[2] == "" || (parts[3] != "rotate-token" && parts[3] != "revoke") {
		return "", "", false
	}
	return parts[2], parts[3], true
}

func writeNodeActionError(w http.ResponseWriter, err error) {
	if errors.Is(err, db.ErrNodeDeleted) || errors.Is(err, db.ErrTokenRevoked) {
		writeJSONError(w, http.StatusConflict, "node token unavailable")
		return
	}
	if errors.Is(err, db.ErrTokenInvalid) {
		writeJSONError(w, http.StatusNotFound, "node not found")
		return
	}
	writeJSONError(w, http.StatusServiceUnavailable, "service unavailable")
}
