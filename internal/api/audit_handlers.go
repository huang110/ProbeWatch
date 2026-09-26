package api

import (
	"net/http"
	"strconv"

	"github.com/probewatch/probewatch/internal/db"
)

func (s *Server) auditLogsRoute(w http.ResponseWriter, r *http.Request) {
	middleware := NewMiddleware(s.service, s.cfg)

	// Audit logs are reserved for the super administrator
	adminCheck := middleware.RequireRole(db.RoleAdmin)

	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	adminCheck(http.HandlerFunc(s.listAuditLogs)).ServeHTTP(w, r)
}

func (s *Server) listAuditLogs(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	limit := 50
	if l := query.Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	offset := 0
	if o := query.Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	actionFilter := query.Get("action")

	logs, total, err := s.service.Store().ListAuditLogs(r.Context(), limit, offset, actionFilter)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to query audit logs: "+err.Error())
		return
	}

	if logs == nil {
		logs = []db.AuditLogEntry{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "ok",
		"total":  total,
		"limit":  limit,
		"offset": offset,
		"logs":   logs,
	})
}
