package api

import (
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

	node, requestID, now, ok := s.authenticateAgentRequest(w, r)
	if !ok {
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

	if requestID != "" {
		s.agentReplayCache.Add(node.ID, requestID, now.Add(2*s.agentClockSkew()))
	}

	w.WriteHeader(http.StatusNoContent)
}

// getNodeEvents retrieves recent system events for a specific node.
func (s *Server) getNodeEvents(w http.ResponseWriter, r *http.Request, uuid string) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	node, ok := s.authorizeNodeScope(w, r, uuid)
	if !ok {
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

	user, ok := s.authenticateUser(w, r)
	if !ok {
		return
	}

	fullAccess, allowedUUIDs, allowedIDs, err := s.getUserNodeScope(r.Context(), user)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "failed to resolve node scope")
		return
	}

	category := r.URL.Query().Get("category")
	severity := r.URL.Query().Get("severity")
	reqNode := r.URL.Query().Get("node_id")
	if reqNode == "" {
		reqNode = r.URL.Query().Get("uuid")
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	var targetNodeIDs []string
	if !fullAccess {
		if reqNode != "" {
			resolvedID := ""
			if _, ok := allowedIDs[reqNode]; ok {
				resolvedID = reqNode
			} else if _, ok := allowedUUIDs[reqNode]; ok {
				nodes, _ := s.service.Store().ListNodes(r.Context())
				for _, n := range nodes {
					if n.UUID == reqNode {
						resolvedID = n.ID
						break
					}
				}
			}
			if resolvedID == "" {
				writeJSON(w, http.StatusOK, map[string]any{"events": make([]db.SystemEventRecord, 0)})
				return
			}
			targetNodeIDs = []string{resolvedID}
		} else {
			if len(allowedIDs) == 0 {
				writeJSON(w, http.StatusOK, map[string]any{"events": make([]db.SystemEventRecord, 0)})
				return
			}
			targetNodeIDs = make([]string, 0, len(allowedIDs))
			for nid := range allowedIDs {
				targetNodeIDs = append(targetNodeIDs, nid)
			}
		}
	} else {
		if reqNode != "" {
			resolvedID := reqNode
			if security.IsRFC4122UUID(reqNode) {
				if node, err := s.service.Store().GetNodeByUUID(r.Context(), reqNode); err == nil {
					resolvedID = node.ID
				}
			}
			targetNodeIDs = []string{resolvedID}
		}
	}

	var events []db.SystemEventRecord
	if fullAccess && reqNode == "" {
		events, err = s.service.Store().ListSystemEvents(r.Context(), "", category, severity, limit)
	} else {
		events, err = s.service.Store().ListSystemEventsForNodes(r.Context(), targetNodeIDs, category, severity, limit)
	}
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

	user, ok := s.authenticateUser(w, r)
	if !ok {
		return
	}

	fullAccess, _, allowedIDs, err := s.getUserNodeScope(r.Context(), user)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "failed to resolve node scope")
		return
	}

	var overview *db.SystemEventsOverview
	if fullAccess {
		overview, err = s.service.Store().GetSystemEventsOverview(r.Context())
	} else {
		if len(allowedIDs) == 0 {
			overview = &db.SystemEventsOverview{
				TotalEvents24h:    0,
				CriticalEvents24h: 0,
				WarningEvents24h:  0,
				CategoryCounts:    make(map[string]int64),
				RecentEvents:      make([]db.SystemEventRecord, 0),
			}
		} else {
			nodeIDs := make([]string, 0, len(allowedIDs))
			for nid := range allowedIDs {
				nodeIDs = append(nodeIDs, nid)
			}
			overview, err = s.service.Store().GetSystemEventsOverviewForNodes(r.Context(), nodeIDs)
		}
	}
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
	now := time.Now().UTC()
	if !s.publicLimiter.Allow(publicLimiterKey(r), now) {
		writeJSONError(w, http.StatusTooManyRequests, "too many requests")
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
		title := "系统状态异常"
		switch ev.Category {
		case "security":
			title = "安全事件拦截"
		case "kernel":
			title = "内核保护事件"
		case "system":
			title = "系统服务状态异常"
		}
		sanitizedRecent = append(sanitizedRecent, map[string]any{
			"id":          ev.ID,
			"node_name":   sanitizeNodeName(ev.NodeName),
			"category":    ev.Category,
			"severity":    ev.Severity,
			"title":       title,
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

	node, ok := s.authorizeNodeScope(w, r, uuid)
	if !ok {
		return
	}

	unit := r.URL.Query().Get("unit")
	priority := r.URL.Query().Get("priority")
	grep := r.URL.Query().Get("grep")
	since := r.URL.Query().Get("since")
	lines, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	if lines <= 0 {
		lines = 100
	} else if lines > 1000 {
		lines = 1000
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

	// 1. If agent is connected via reverse tunnel, query directly through the tunnel with POSIX quoted argv
	if s.terminalManager != nil && s.terminalManager.IsNodeConnected(node.UUID) {
		cmdArgs, err := agent.BuildJournalctlArgs(req)
		if err == nil {
			var cmdParts []string
			cmdParts = append(cmdParts, "journalctl")
			for _, arg := range cmdArgs {
				// POSIX safe single-quoting prevents shell command substitutions
				cmdParts = append(cmdParts, "'"+strings.ReplaceAll(arg, "'", "'\\''")+"'")
			}
			cmdStr := strings.Join(cmdParts, " ")
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
