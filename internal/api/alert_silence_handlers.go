package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/notify"
)

type createSilenceRequest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	NodeFilter  string `json:"node_filter"`
	Category    string `json:"category"`
	RuleID      string `json:"rule_id"`
	Fingerprint string `json:"fingerprint"`
	StartsAt    int64  `json:"starts_at"` // Unix timestamp in seconds or milliseconds
	EndsAt      int64  `json:"ends_at"`   // Unix timestamp in seconds or milliseconds
	DurationMinutes int `json:"duration_minutes"` // Shortcut if ends_at not specified
	Reason      string `json:"reason"`
}

func (s *Server) listAlertSilences(w http.ResponseWriter, r *http.Request) {
	silences, err := s.service.Store().ListAlertSilences(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "failed to list alert silences")
		return
	}
	writeJSON(w, http.StatusOK, silences)
}

func (s *Server) createAlertSilence(w http.ResponseWriter, r *http.Request) {
	var req createSilenceRequest
	if err := decodeJSONObjectRequest(w, r, s.requestBodyLimit(), &req); err != nil {
		writeRequestError(w, err)
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "静默规则"
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		id = "silence-" + randomHex(6)
	}

	now := time.Now().UTC()
	var startsAt time.Time
	if req.StartsAt > 0 {
		if req.StartsAt > 1e12 {
			startsAt = time.UnixMilli(req.StartsAt).UTC()
		} else {
			startsAt = time.Unix(req.StartsAt, 0).UTC()
		}
	} else {
		startsAt = now
	}

	var endsAt time.Time
	if req.EndsAt > 0 {
		if req.EndsAt > 1e12 {
			endsAt = time.UnixMilli(req.EndsAt).UTC()
		} else {
			endsAt = time.Unix(req.EndsAt, 0).UTC()
		}
	} else if req.DurationMinutes > 0 {
		endsAt = startsAt.Add(time.Duration(req.DurationMinutes) * time.Minute)
	} else {
		endsAt = startsAt.Add(1 * time.Hour) // Default 1 hour snooze
	}

	actor := "admin"
	if session, err := s.service.AuthenticateWithError(r, time.Now().UTC()); err == nil && session.AdminUserID != "" {
		actor = session.AdminUserID
	}

	silence := db.AlertSilence{
		ID:          id,
		Name:        name,
		NodeFilter:  strings.TrimSpace(req.NodeFilter),
		Category:    strings.TrimSpace(req.Category),
		RuleID:      strings.TrimSpace(req.RuleID),
		Fingerprint: strings.TrimSpace(req.Fingerprint),
		StartsAt:    startsAt,
		EndsAt:      endsAt,
		Reason:      strings.TrimSpace(req.Reason),
		CreatedBy:   actor,
		CreatedAt:   now,
	}

	if err := s.service.Store().CreateAlertSilence(r.Context(), silence); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, silence)
}

func (s *Server) deleteAlertSilence(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.service.Store().DeleteAlertSilence(r.Context(), id); err != nil {
		if errors.Is(err, db.ErrSilenceNotFound) {
			writeJSONError(w, http.StatusNotFound, "alert silence not found")
			return
		}
		writeJSONError(w, http.StatusServiceUnavailable, "failed to delete alert silence")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": id})
}

func (s *Server) listFlappingAlerts(w http.ResponseWriter, r *http.Request) {
	if s.notifier == nil || s.notifier.FlappingTracker() == nil {
		writeJSON(w, http.StatusOK, []notify.FlappingTarget{})
		return
	}
	targets := s.notifier.FlappingTracker().GetActiveFlapping()
	writeJSON(w, http.StatusOK, targets)
}
