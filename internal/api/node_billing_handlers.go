package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/security"
)

func (s *Server) getNodeBilling(w http.ResponseWriter, r *http.Request, uuid string) {
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

	info, err := s.service.Store().GetNodeCycleTraffic(r.Context(), node.ID, time.Now().UTC())
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "billing unavailable: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) putNodeBilling(w http.ResponseWriter, r *http.Request, uuid string) {
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

	var req db.NodeBillingSettings
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.NodeID = node.ID

	if err := s.service.Store().UpsertNodeBillingSettings(r.Context(), req); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to save billing settings: "+err.Error())
		return
	}

	info, err := s.service.Store().GetNodeCycleTraffic(r.Context(), node.ID, time.Now().UTC())
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "billing unavailable")
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) resetNodeBilling(w http.ResponseWriter, r *http.Request, uuid string) {
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

	now := time.Now().UTC()
	if err := s.service.Store().ResetNodeBillingCycle(r.Context(), node.ID, now); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to reset billing cycle: "+err.Error())
		return
	}

	info, err := s.service.Store().GetNodeCycleTraffic(r.Context(), node.ID, now)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "billing unavailable")
		return
	}
	writeJSON(w, http.StatusOK, info)
}

type publicBillingResponse struct {
	ResetDay          int       `json:"reset_day"`
	DaysUntilReset    int       `json:"days_until_reset"`
	PeriodStart       time.Time `json:"period_start"`
	PeriodEnd         time.Time `json:"period_end"`
	CycleUsedBytes    uint64    `json:"cycle_used_bytes"`
	TrafficQuotaBytes uint64    `json:"traffic_quota_bytes"`
	BonusQuotaBytes   uint64    `json:"bonus_quota_bytes"`
	TotalQuotaBytes   uint64    `json:"total_quota_bytes"`
	UsedPercent       float64   `json:"used_percent"`
	AccountingMethod  string    `json:"accounting_method"`
}

func (s *Server) publicNodeBilling(w http.ResponseWriter, r *http.Request, uuid string) {
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

	info, err := s.service.Store().GetNodeCycleTraffic(r.Context(), node.ID, time.Now().UTC())
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "billing unavailable")
		return
	}

	writeJSON(w, http.StatusOK, publicBillingResponse{
		ResetDay:          info.ResetDay,
		DaysUntilReset:    info.DaysUntilReset,
		PeriodStart:       info.PeriodStart,
		PeriodEnd:         info.PeriodEnd,
		CycleUsedBytes:    info.CycleUsedBytes,
		TrafficQuotaBytes: info.TrafficQuotaBytes,
		BonusQuotaBytes:   info.BonusQuotaBytes,
		TotalQuotaBytes:   info.TotalQuotaBytes,
		UsedPercent:       info.UsedPercent,
		AccountingMethod:  info.AccountingMethod,
	})
}
