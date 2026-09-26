package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/probewatch/probewatch/internal/db"
)

var (
	statusPageCacheMu sync.RWMutex
	statusPageCacheAt time.Time
	statusPageCache   *db.PublicStatusPageResponse
)

func (s *Server) publicStatusPageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	now := time.Now().UTC()
	if !s.publicLimiter.Allow(publicLimiterKey(r), now) {
		writeJSONError(w, http.StatusTooManyRequests, "too many requests")
		return
	}

	// 5-second public cache to absorb traffic spikes
	statusPageCacheMu.RLock()
	if statusPageCache != nil && !statusPageCacheAt.IsZero() && now.Sub(statusPageCacheAt) < 5*time.Second {
		cached := statusPageCache
		statusPageCacheMu.RUnlock()
		writeJSON(w, http.StatusOK, cached)
		return
	}
	statusPageCacheMu.RUnlock()

	ctx := r.Context()
	cfg, err := s.service.Store().GetStatusPageConfig(ctx, "default")
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to get status page configuration")
		return
	}

	// Fetch active incidents
	incidents, _, err := s.service.Store().ListIncidents(ctx, false, 50, 0)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to query incidents")
		return
	}

	activeIncidents := make([]db.Incident, 0)
	maintenances := make([]db.Incident, 0)
	for _, inc := range incidents {
		if inc.IsMaintenance {
			maintenances = append(maintenances, inc)
		} else {
			activeIncidents = append(activeIncidents, inc)
		}
	}

	days := cfg.ShowUptimeDays
	if days <= 0 {
		days = 90
	}

	// Nodes cache to check current node statuses
	nodes, _ := s.service.Store().ListNodes(ctx)
	nodesMap := make(map[string]db.Node)
	for _, n := range nodes {
		nodesMap[n.ID] = n
	}

	componentStatuses := make([]db.ComponentStatus, 0, len(cfg.Components))
	hasOutage := false
	hasDegraded := false
	hasMajorOutage := false

	for _, comp := range cfg.Components {
		dailyUptimes, u24h, u7d, u30d, u90d, _ := s.service.Store().CalculateComponentSLA(ctx, comp.NodeID, days)

		curStatus := "operational"
		var latencyVal *float64

		if comp.NodeID != "" {
			node, exists := nodesMap[comp.NodeID]
			if exists {
				// Check node online status
				reportedAt, _, errLatest := s.service.Store().GetResourceLatest(ctx, node.ID)
				if errLatest != nil || reportedAt.IsZero() || time.Since(reportedAt) > 90*time.Second {
					curStatus = "outage"
					hasOutage = true
				} else {
					curStatus = "operational"
				}

				if comp.ShowLatency {
					records, _ := s.service.Store().GetResultHistory(ctx, db.TargetKindTCP, node.ID, now.Add(-1*time.Hour), now, 5)
					if len(records) > 0 {
						var res struct {
							Latency int64 `json:"latency_ms"`
						}
						if errU := json.Unmarshal(records[0].Payload, &res); errU == nil && res.Latency > 0 {
							val := float64(res.Latency)
							latencyVal = &val
						}
					}
				}
			} else {
				curStatus = "degraded"
				hasDegraded = true
			}
		}

		if curStatus == "outage" {
			hasOutage = true
		} else if curStatus == "degraded" {
			hasDegraded = true
		}

		componentStatuses = append(componentStatuses, db.ComponentStatus{
			Component:     comp,
			CurrentStatus: curStatus,
			LatencyMs:     latencyVal,
			Uptime24h:     u24h,
			Uptime7d:      u7d,
			Uptime30d:     u30d,
			Uptime90d:     u90d,
			DailyUptimes:  dailyUptimes,
		})
	}

	// Overall status evaluation
	overallStatus := "operational"
	overallMessage := "所有系统均正常运行"

	for _, inc := range activeIncidents {
		if inc.Impact == db.IncidentImpactCritical {
			hasMajorOutage = true
		} else if inc.Impact == db.IncidentImpactMajor {
			hasOutage = true
		} else if inc.Impact == db.IncidentImpactMinor {
			hasDegraded = true
		}
	}

	if hasMajorOutage {
		overallStatus = "major_outage"
		overallMessage = "系统发生重大服务故障"
	} else if hasOutage {
		overallStatus = "partial_outage"
		overallMessage = "部分服务发生中断"
	} else if hasDegraded {
		overallStatus = "degraded"
		overallMessage = "部分服务性能有所降级"
	} else if len(maintenances) > 0 {
		overallStatus = "under_maintenance"
		overallMessage = "系统正在进行计划维护"
	}

	resp := &db.PublicStatusPageResponse{
		Config:          *cfg,
		OverallStatus:   overallStatus,
		OverallMessage:  overallMessage,
		ActiveIncidents: activeIncidents,
		Maintenance:     maintenances,
		Components:      componentStatuses,
		LastUpdatedAt:   now,
	}

	statusPageCacheMu.Lock()
	statusPageCache = resp
	statusPageCacheAt = now
	statusPageCacheMu.Unlock()

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) publicIncidentsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if val, err := strconv.Atoi(l); err == nil && val > 0 && val <= 100 {
			limit = val
		}
	}
	offset := 0
	if off := r.URL.Query().Get("offset"); off != "" {
		if val, err := strconv.Atoi(off); err == nil && val >= 0 {
			offset = val
		}
	}

	incidents, total, err := s.service.Store().ListIncidents(r.Context(), true, limit, offset)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to query incidents")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"incidents": incidents,
		"total":     total,
		"limit":     limit,
		"offset":    offset,
	})
}

// adminStatusPageRoute handles GET and PUT /api/admin/status-page
func (s *Server) adminStatusPageRoute(w http.ResponseWriter, r *http.Request) {
	middleware := NewMiddleware(s.service, s.cfg)

	switch r.Method {
	case http.MethodGet:
		middleware.RequireAuth(http.HandlerFunc(s.adminGetStatusPageConfig)).ServeHTTP(w, r)
	case http.MethodPut:
		middleware.RequireAuth(middleware.RequireCSRF(http.HandlerFunc(s.adminUpdateStatusPageConfig))).ServeHTTP(w, r)
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) adminGetStatusPageConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.service.Store().GetStatusPageConfig(r.Context(), "default")
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to get status page configuration")
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) adminUpdateStatusPageConfig(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok || !user.IsAdmin() {
		writeJSONError(w, http.StatusForbidden, "forbidden: admin role required")
		return
	}

	var req db.StatusPageConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.ID = "default"

	if err := s.service.Store().UpdateStatusPageConfig(r.Context(), &req); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to save status page configuration")
		return
	}

	// Invalidate public cache
	statusPageCacheMu.Lock()
	statusPageCache = nil
	statusPageCacheMu.Unlock()

	// Audit log
	_ = s.service.Store().RecordAuditLog(r.Context(), db.AuditLogEntry{
		ActorID:      user.ID,
		ActorName:    user.DisplayNameOrLogin(),
		ActorType:    "user",
		Action:       "status_page.update",
		ResourceType: "status_page_config",
		ResourceID:   req.ID,
		Detail:       fmt.Sprintf("Updated status page '%s' with %d components", req.Title, len(req.Components)),
		IPAddress:    r.RemoteAddr,
		StatusCode:   http.StatusOK,
	})

	writeJSON(w, http.StatusOK, req)
}

// adminIncidentsRoute dispatches /api/admin/incidents and /api/admin/incidents/{id}
func (s *Server) adminIncidentsRoute(w http.ResponseWriter, r *http.Request) {
	middleware := NewMiddleware(s.service, s.cfg)

	cleanPath := strings.TrimRight(r.URL.Path, "/")
	parts := strings.Split(cleanPath, "/")
	// Expected parts:
	// ["api", "admin", "incidents"] -> collection
	// ["api", "admin", "incidents", "<id>"] -> item
	// ["api", "admin", "incidents", "<id>", "updates"] -> add update

	if len(parts) == 4 && parts[1] == "api" && parts[2] == "admin" && parts[3] == "incidents" {
		switch r.Method {
		case http.MethodGet:
			middleware.RequireAuth(http.HandlerFunc(s.adminListIncidents)).ServeHTTP(w, r)
		case http.MethodPost:
			middleware.RequireAuth(middleware.RequireCSRF(http.HandlerFunc(s.adminCreateIncident))).ServeHTTP(w, r)
		default:
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	if len(parts) == 5 && parts[1] == "api" && parts[2] == "admin" && parts[3] == "incidents" {
		incidentID := parts[4]
		switch r.Method {
		case http.MethodGet:
			middleware.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				s.adminGetIncident(w, r, incidentID)
			})).ServeHTTP(w, r)
		case http.MethodPut:
			middleware.RequireAuth(middleware.RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				s.adminUpdateIncident(w, r, incidentID)
			}))).ServeHTTP(w, r)
		case http.MethodDelete:
			middleware.RequireAuth(middleware.RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				s.adminDeleteIncident(w, r, incidentID)
			}))).ServeHTTP(w, r)
		default:
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	if len(parts) == 6 && parts[1] == "api" && parts[2] == "admin" && parts[3] == "incidents" && parts[5] == "updates" {
		incidentID := parts[4]
		if r.Method == http.MethodPost {
			middleware.RequireAuth(middleware.RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				s.adminAddIncidentUpdate(w, r, incidentID)
			}))).ServeHTTP(w, r)
			return
		}
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	writeJSONError(w, http.StatusNotFound, "not found")
}

func (s *Server) adminListIncidents(w http.ResponseWriter, r *http.Request) {
	includeAll := r.URL.Query().Get("all") == "true"
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if val, err := strconv.Atoi(l); err == nil && val > 0 && val <= 200 {
			limit = val
		}
	}
	offset := 0
	if off := r.URL.Query().Get("offset"); off != "" {
		if val, err := strconv.Atoi(off); err == nil && val >= 0 {
			offset = val
		}
	}

	incidents, total, err := s.service.Store().ListIncidents(r.Context(), includeAll, limit, offset)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to query incidents")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"incidents": incidents,
		"total":     total,
		"limit":     limit,
		"offset":    offset,
	})
}

type createIncidentRequest struct {
	Title            string `json:"title"`
	Status           string `json:"status"`
	Impact           string `json:"impact"`
	IsMaintenance    bool   `json:"is_maintenance"`
	ScheduledStartAt *int64 `json:"scheduled_start_at"`
	ScheduledEndAt   *int64 `json:"scheduled_end_at"`
	Message          string `json:"message"`
}

func (s *Server) adminCreateIncident(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok || !user.CanWrite() {
		writeJSONError(w, http.StatusForbidden, "forbidden: write permission required")
		return
	}

	var req createIncidentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if strings.TrimSpace(req.Title) == "" {
		writeJSONError(w, http.StatusBadRequest, "incident title is required")
		return
	}

	inc := &db.Incident{
		Title:            req.Title,
		Status:           req.Status,
		Impact:           req.Impact,
		IsMaintenance:    req.IsMaintenance,
		ScheduledStartAt: req.ScheduledStartAt,
		ScheduledEndAt:   req.ScheduledEndAt,
	}

	created, err := s.service.Store().CreateIncident(r.Context(), inc, req.Message)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create incident: %v", err))
		return
	}

	// Invalidate public cache
	statusPageCacheMu.Lock()
	statusPageCache = nil
	statusPageCacheMu.Unlock()

	// Audit log
	_ = s.service.Store().RecordAuditLog(r.Context(), db.AuditLogEntry{
		ActorID:      user.ID,
		ActorName:    user.DisplayNameOrLogin(),
		ActorType:    "user",
		Action:       "incident.create",
		ResourceType: "incident",
		ResourceID:   created.ID,
		Detail:       fmt.Sprintf("Created incident '%s' (impact: %s, maintenance: %t)", created.Title, created.Impact, created.IsMaintenance),
		IPAddress:    r.RemoteAddr,
		StatusCode:   http.StatusCreated,
	})

	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) adminGetIncident(w http.ResponseWriter, r *http.Request, incidentID string) {
	inc, err := s.service.Store().GetIncident(r.Context(), incidentID)
	if errors.Is(err, db.ErrIncidentNotFound) {
		writeJSONError(w, http.StatusNotFound, "incident not found")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to get incident")
		return
	}
	writeJSON(w, http.StatusOK, inc)
}

func (s *Server) adminUpdateIncident(w http.ResponseWriter, r *http.Request, incidentID string) {
	user, ok := UserFromContext(r.Context())
	if !ok || !user.CanWrite() {
		writeJSONError(w, http.StatusForbidden, "forbidden: write permission required")
		return
	}

	var req db.Incident
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.ID = incidentID

	if err := s.service.Store().UpdateIncident(r.Context(), &req); err != nil {
		if errors.Is(err, db.ErrIncidentNotFound) {
			writeJSONError(w, http.StatusNotFound, "incident not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "failed to update incident")
		return
	}

	// Invalidate public cache
	statusPageCacheMu.Lock()
	statusPageCache = nil
	statusPageCacheMu.Unlock()

	// Audit log
	_ = s.service.Store().RecordAuditLog(r.Context(), db.AuditLogEntry{
		ActorID:      user.ID,
		ActorName:    user.DisplayNameOrLogin(),
		ActorType:    "user",
		Action:       "incident.update",
		ResourceType: "incident",
		ResourceID:   incidentID,
		Detail:       fmt.Sprintf("Updated incident '%s' status to %s", req.Title, req.Status),
		IPAddress:    r.RemoteAddr,
		StatusCode:   http.StatusOK,
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type addUpdateRequest struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

func (s *Server) adminAddIncidentUpdate(w http.ResponseWriter, r *http.Request, incidentID string) {
	user, ok := UserFromContext(r.Context())
	if !ok || !user.CanWrite() {
		writeJSONError(w, http.StatusForbidden, "forbidden: write permission required")
		return
	}

	var req addUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if strings.TrimSpace(req.Message) == "" {
		writeJSONError(w, http.StatusBadRequest, "update message cannot be empty")
		return
	}

	upd, err := s.service.Store().AddIncidentUpdate(r.Context(), incidentID, req.Status, req.Message)
	if errors.Is(err, db.ErrIncidentNotFound) {
		writeJSONError(w, http.StatusNotFound, "incident not found")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to add update: %v", err))
		return
	}

	// Invalidate public cache
	statusPageCacheMu.Lock()
	statusPageCache = nil
	statusPageCacheMu.Unlock()

	// Audit log
	_ = s.service.Store().RecordAuditLog(r.Context(), db.AuditLogEntry{
		ActorID:      user.ID,
		ActorName:    user.DisplayNameOrLogin(),
		ActorType:    "user",
		Action:       "incident.update_post",
		ResourceType: "incident",
		ResourceID:   incidentID,
		Detail:       fmt.Sprintf("Posted update on incident: status=%s, msg=%s", upd.Status, upd.Message),
		IPAddress:    r.RemoteAddr,
		StatusCode:   http.StatusOK,
	})

	writeJSON(w, http.StatusOK, upd)
}

func (s *Server) adminDeleteIncident(w http.ResponseWriter, r *http.Request, incidentID string) {
	user, ok := UserFromContext(r.Context())
	if !ok || !user.IsAdmin() {
		writeJSONError(w, http.StatusForbidden, "forbidden: admin role required")
		return
	}

	err := s.service.Store().DeleteIncident(r.Context(), incidentID)
	if errors.Is(err, db.ErrIncidentNotFound) {
		writeJSONError(w, http.StatusNotFound, "incident not found")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to delete incident")
		return
	}

	// Invalidate public cache
	statusPageCacheMu.Lock()
	statusPageCache = nil
	statusPageCacheMu.Unlock()

	// Audit log
	_ = s.service.Store().RecordAuditLog(r.Context(), db.AuditLogEntry{
		ActorID:      user.ID,
		ActorName:    user.DisplayNameOrLogin(),
		ActorType:    "user",
		Action:       "incident.delete",
		ResourceType: "incident",
		ResourceID:   incidentID,
		Detail:       fmt.Sprintf("Deleted incident %s", incidentID),
		IPAddress:    r.RemoteAddr,
		StatusCode:   http.StatusOK,
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
