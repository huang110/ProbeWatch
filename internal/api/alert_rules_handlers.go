package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/db"
)

type alertRuleRequest struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Metric          string  `json:"metric"`
	Operator        string  `json:"operator"`
	Threshold       float64 `json:"threshold"`
	DurationSeconds int     `json:"duration_seconds"`
	Severity        string  `json:"severity"`
	NodeFilter      string  `json:"node_filter"`
	Enabled         bool    `json:"enabled"`
}

func (s *Server) listAlertRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.service.Store().ListAlertRules(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "failed to list alert rules")
		return
	}
	writeJSON(w, http.StatusOK, rules)
}

func (s *Server) getAlertRule(w http.ResponseWriter, r *http.Request, id string) {
	rule, err := s.service.Store().GetAlertRule(r.Context(), id)
	if errors.Is(err, db.ErrAlertRuleNotFound) {
		writeJSONError(w, http.StatusNotFound, "alert rule not found")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "failed to get alert rule")
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

func (s *Server) createAlertRule(w http.ResponseWriter, r *http.Request) {
	var req alertRuleRequest
	if err := decodeJSONObjectRequest(w, r, s.requestBodyLimit(), &req); err != nil {
		writeRequestError(w, err)
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Metric) == "" {
		writeJSONError(w, http.StatusBadRequest, "name and metric are required")
		return
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		id = "rule-" + randomHex(6)
	}
	op := strings.TrimSpace(req.Operator)
	if op == "" {
		op = ">"
	}
	sev := strings.TrimSpace(strings.ToLower(req.Severity))
	if sev == "" {
		sev = "warning"
	}
	filter := strings.TrimSpace(req.NodeFilter)
	if filter == "" {
		filter = "*"
	}

	rule := db.AlertRule{
		ID:              id,
		Name:            strings.TrimSpace(req.Name),
		Metric:          strings.TrimSpace(strings.ToLower(req.Metric)),
		Operator:        op,
		Threshold:       req.Threshold,
		DurationSeconds: req.DurationSeconds,
		Severity:        sev,
		NodeFilter:      filter,
		Enabled:         req.Enabled,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	if err := s.service.Store().CreateAlertRule(r.Context(), rule); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, rule)
}

func (s *Server) updateAlertRule(w http.ResponseWriter, r *http.Request, id string) {
	var req alertRuleRequest
	if err := decodeJSONObjectRequest(w, r, s.requestBodyLimit(), &req); err != nil {
		writeRequestError(w, err)
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Metric) == "" {
		writeJSONError(w, http.StatusBadRequest, "name and metric are required")
		return
	}
	op := strings.TrimSpace(req.Operator)
	if op == "" {
		op = ">"
	}
	sev := strings.TrimSpace(strings.ToLower(req.Severity))
	if sev == "" {
		sev = "warning"
	}
	filter := strings.TrimSpace(req.NodeFilter)
	if filter == "" {
		filter = "*"
	}

	rule := db.AlertRule{
		ID:              id,
		Name:            strings.TrimSpace(req.Name),
		Metric:          strings.TrimSpace(strings.ToLower(req.Metric)),
		Operator:        op,
		Threshold:       req.Threshold,
		DurationSeconds: req.DurationSeconds,
		Severity:        sev,
		NodeFilter:      filter,
		Enabled:         req.Enabled,
		UpdatedAt:       time.Now().UTC(),
	}
	if err := s.service.Store().UpdateAlertRule(r.Context(), rule); err != nil {
		if errors.Is(err, db.ErrAlertRuleNotFound) {
			writeJSONError(w, http.StatusNotFound, "alert rule not found")
			return
		}
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

func (s *Server) deleteAlertRule(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.service.Store().DeleteAlertRule(r.Context(), id); err != nil {
		if errors.Is(err, db.ErrAlertRuleNotFound) {
			writeJSONError(w, http.StatusNotFound, "alert rule not found")
			return
		}
		writeJSONError(w, http.StatusServiceUnavailable, "failed to delete alert rule")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": id})
}

func (s *Server) toggleAlertRule(w http.ResponseWriter, r *http.Request, id string) {
	newState, err := s.service.Store().ToggleAlertRule(r.Context(), id)
	if errors.Is(err, db.ErrAlertRuleNotFound) {
		writeJSONError(w, http.StatusNotFound, "alert rule not found")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "failed to toggle alert rule")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id, "enabled": newState})
}
