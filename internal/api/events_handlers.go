package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/agent"
	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/protocol"
	"github.com/probewatch/probewatch/internal/security"
)

// eventsAgent handles batch system event reporting from edge nodes.
func (s *Server) eventsAgent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	token, ok := parseBearer(r.Header.Get("Authorization"))
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "authentication failed")
		return
	}

	now := time.Now().UTC()
	node, err := s.service.Store().AuthenticateNodeToken(r.Context(), token, now)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication failed")
		return
	}

	var batch protocol.SystemEventBatchReport
	if err := decodeJSONRequest(w, r, s.requestBodyLimit(), &batch); err != nil {
		writeRequestError(w, err)
		return
	}

	if err := batch.Validate(); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid events report: "+err.Error())
		return
	}

	if batch.NodeUUID != node.UUID {
		writeJSONError(w, http.StatusBadRequest, "node uuid mismatch")
		return
	}

	if err := s.service.Store().SaveSystemEvents(r.Context(), node.ID, batch.Events); err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "events save failed")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// getNodeEvents retrieves recent system events for a specific node.
func (s *Server) getNodeEvents(w http.ResponseWriter, r *http.Request, uuid string) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

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

	category := r.URL.Query().Get("category")
	severity := r.URL.Query().Get("severity")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	events, err := s.service.Store().ListSystemEvents(r.Context(), node.ID, category, severity, limit)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "events query failed")
		return
	}
	if events == nil {
		events = make([]db.SystemEventRecord, 0)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"node_id":   node.ID,
		"node_uuid": node.UUID,
		"node_name": node.Name,
		"events":    events,
	})
}

// fleetEventsHandler lists recent system events fleet-wide with optional filters.
func (s *Server) fleetEventsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	category := r.URL.Query().Get("category")
	severity := r.URL.Query().Get("severity")
	nodeID := r.URL.Query().Get("node_id")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	events, err := s.service.Store().ListSystemEvents(r.Context(), nodeID, category, severity, limit)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "events query failed")
		return
	}
	if events == nil {
		events = make([]db.SystemEventRecord, 0)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"events": events,
	})
}

// eventsOverviewHandler returns fleet-wide security and system event metrics.
func (s *Server) eventsOverviewHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	overview, err := s.service.Store().GetSystemEventsOverview(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "overview unavailable")
		return
	}

	writeJSON(w, http.StatusOK, overview)
}

// publicEventsOverviewHandler provides public sanitized event metrics for status dashboards.
func (s *Server) publicEventsOverviewHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	overview, err := s.service.Store().GetSystemEventsOverview(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "overview unavailable")
		return
	}

	// Sanitize recent events to hide private IPs or sensitive tokens in messages
	sanitizedRecent := make([]map[string]any, 0, len(overview.RecentEvents))
	for _, ev := range overview.RecentEvents {
		sanitizedRecent = append(sanitizedRecent, map[string]any{
			"id":          ev.ID,
			"node_name":   ev.NodeName,
			"category":    ev.Category,
			"severity":    ev.Severity,
			"title":       ev.Title,
			"occurred_at": ev.OccurredAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"total_events_24h":    overview.TotalEvents24h,
		"critical_events_24h": overview.CriticalEvents24h,
		"warning_events_24h":  overview.WarningEvents24h,
		"category_counts":     overview.CategoryCounts,
		"recent_events":       sanitizedRecent,
	})
}

// queryNodeLogs handles secure, parameterized system log retrieval for a node.
func (s *Server) queryNodeLogs(w http.ResponseWriter, r *http.Request, uuid string) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

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

	unit := r.URL.Query().Get("unit")
	priority := r.URL.Query().Get("priority")
	grep := r.URL.Query().Get("grep")
	since := r.URL.Query().Get("since")
	lines, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	if lines <= 0 || lines > 500 {
		lines = 100
	}

	req := protocol.LogQueryRequest{
		NodeUUID: node.UUID,
		Unit:     unit,
		Priority: priority,
		Grep:     grep,
		Lines:    lines,
		Since:    since,
	}

	if err := req.Validate(); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid query parameter: "+err.Error())
		return
	}

	// 1. If agent is connected via reverse tunnel, query directly through the tunnel
	if s.terminalManager != nil && s.terminalManager.IsNodeConnected(node.UUID) {
		cmdStr := fmt.Sprintf("journalctl --no-pager -o short-iso -n %d", req.Lines)
		if req.Unit != "" {
			cmdStr += " -u " + req.Unit
		}
		if req.Priority != "" {
			cmdStr += " -p " + req.Priority
		}
		if req.Since != "" {
			cmdStr += " --since -" + req.Since
		}
		if req.Grep != "" {
			cmdStr += fmt.Sprintf(" -g %q", req.Grep)
		}

		res, err := s.terminalManager.ExecuteCommand(r.Context(), node.UUID, cmdStr, 8)
		if err == nil {
			var logLines []string
			if res.Stdout != "" {
				raw := strings.Split(res.Stdout, "\n")
				for _, l := range raw {
					l = strings.TrimRight(l, "\r\n")
					if l != "" {
						logLines = append(logLines, l)
					}
				}
			} else if res.Stderr != "" {
				logLines = strings.Split(res.Stderr, "\n")
			}

			writeJSON(w, http.StatusOK, protocol.LogQueryResponse{
				NodeUUID:   node.UUID,
				NodeName:   node.Name,
				Unit:       req.Unit,
				LinesCount: len(logLines),
				Lines:      logLines,
				QueriedAt:  time.Now().Unix(),
			})
			return
		}
	}

	// 2. Fallback to local system log query if running on the target node or server
	resp, err := agent.QuerySystemLogs(r.Context(), req)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "log query failed: "+err.Error())
		return
	}
	resp.NodeName = node.Name

	writeJSON(w, http.StatusOK, resp)
}
