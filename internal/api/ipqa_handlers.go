package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/protocol"
)

var (
	ipqaCooldownMu   sync.Mutex
	ipqaLastSync     = make(map[string]time.Time)
	ipqaLastNotify   = make(map[string]time.Time)
	ipqaNotifiedKeys = make(map[string]struct{})
)

const (
	ipqaSyncCooldown   = 5 * time.Second
	ipqaNotifyCooldown = 5 * time.Minute
)

// AdminIPQAResponse is the full, authenticated administrator telemetry response for IPQA.
type AdminIPQAResponse struct {
	NodeUUID          string                   `json:"node_uuid"`
	NodeName          string                   `json:"node_name"`
	Enabled           bool                     `json:"enabled"`
	Installed         bool                     `json:"installed"`
	LastCheckedAt     *int64                   `json:"last_checked_at,omitempty"`
	LatestArchiveDate string                   `json:"latest_archive_date,omitempty"`
	HighestSeverity   string                   `json:"highest_severity,omitempty"`
	AlertCount        int                      `json:"alert_count"`
	CriticalCount     int                      `json:"critical_count"`
	WarningCount      int                      `json:"warning_count"`
	InfoCount         int                      `json:"info_count"`
	IPv4              *protocol.IPQAFamilyInfo `json:"ipv4,omitempty"`
	IPv6              *protocol.IPQAFamilyInfo `json:"ipv6,omitempty"`
	Changes           []protocol.IPQAChange    `json:"changes,omitempty"`
	Sources           []string                 `json:"sources,omitempty"`
	CollectionError   string                   `json:"collection_error,omitempty"`
}

// getNodeIPQA handles GET /api/nodes/:uuid/ipqa (Admin authenticated).
func (s *Server) getNodeIPQA(w http.ResponseWriter, r *http.Request, uuid string) {
	if !s.checkIPQARateLimit(w, r) {
		return
	}
	node, err := s.service.Store().GetNodeByUUID(r.Context(), uuid)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "node not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "failed to query node")
		return
	}

	_, payload, err := s.service.Store().GetResourceLatest(r.Context(), node.ID)
	var snapshot protocol.ResourceSnapshot
	if err == nil && len(payload) > 0 {
		_ = json.Unmarshal(payload, &snapshot)
	}

	resp := AdminIPQAResponse{
		NodeUUID: node.UUID,
		NodeName: node.Name,
	}

	if snapshot.IPQA != nil {
		resp.Enabled = snapshot.IPQA.Enabled
		resp.Installed = snapshot.IPQA.Installed
		if snapshot.IPQA.LastCheckedAt > 0 {
			resp.LastCheckedAt = &snapshot.IPQA.LastCheckedAt
		}
		resp.LatestArchiveDate = snapshot.IPQA.LatestArchiveDate
		resp.HighestSeverity = snapshot.IPQA.HighestSeverity
		resp.AlertCount = snapshot.IPQA.AlertCount
		resp.CriticalCount = snapshot.IPQA.CriticalCount
		resp.WarningCount = snapshot.IPQA.WarningCount
		resp.InfoCount = snapshot.IPQA.InfoCount
		resp.IPv4 = snapshot.IPQA.IPv4
		resp.IPv6 = snapshot.IPQA.IPv6
		resp.Changes = snapshot.IPQA.Changes
		resp.Sources = snapshot.IPQA.Sources
		resp.CollectionError = snapshot.IPQA.CollectionError
	}

	writeJSON(w, http.StatusOK, resp)
}

// getNodeIPQAHistory handles GET /api/nodes/:uuid/ipqa/history (Admin authenticated).
func (s *Server) getNodeIPQAHistory(w http.ResponseWriter, r *http.Request, uuid string) {
	if !s.checkIPQARateLimit(w, r) {
		return
	}
	node, err := s.service.Store().GetNodeByUUID(r.Context(), uuid)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "node not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "failed to query node")
		return
	}

	archives, err := s.service.Store().ListIPQAArchives(r.Context(), node.ID, 30, 0)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to list ipqa archives")
		return
	}

	type historyItem struct {
		ID              int64     `json:"id"`
		ArchiveDate     string    `json:"archive_date"`
		Family          string    `json:"family"`
		HighestSeverity string    `json:"highest_severity"`
		AlertCount      int       `json:"alert_count"`
		CheckedAt       time.Time `json:"checked_at"`
	}

	var items []historyItem
	for _, a := range archives {
		items = append(items, historyItem{
			ID:              a.ID,
			ArchiveDate:     a.ArchiveDate,
			Family:          a.Family,
			HighestSeverity: a.HighestSeverity,
			AlertCount:      a.AlertCount,
			CheckedAt:       a.CheckedAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"node_uuid": node.UUID,
		"archives":  items,
	})
}

// getNodeIPQAChanges handles GET /api/nodes/:uuid/ipqa/changes (Admin authenticated).
func (s *Server) getNodeIPQAChanges(w http.ResponseWriter, r *http.Request, uuid string) {
	if !s.checkIPQARateLimit(w, r) {
		return
	}
	node, err := s.service.Store().GetNodeByUUID(r.Context(), uuid)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "node not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "failed to query node")
		return
	}

	_, payload, err := s.service.Store().GetResourceLatest(r.Context(), node.ID)
	var snapshot protocol.ResourceSnapshot
	if err == nil && len(payload) > 0 {
		_ = json.Unmarshal(payload, &snapshot)
	}

	var changes []protocol.IPQAChange
	if snapshot.IPQA != nil {
		changes = snapshot.IPQA.Changes
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"node_uuid": node.UUID,
		"changes":   changes,
	})
}

// postNodeIPQASync handles POST /api/nodes/:uuid/ipqa/sync (Admin authenticated with CSRF).
func (s *Server) postNodeIPQASync(w http.ResponseWriter, r *http.Request, uuid string) {
	if !s.checkIPQARateLimit(w, r) {
		return
	}
	user, err := s.service.CurrentUser(r, time.Now().UTC())
	if err != nil {
		writeAuthenticationError(w, err)
		return
	}

	node, err := s.service.Store().GetNodeByUUID(r.Context(), uuid)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "node not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "failed to query node")
		return
	}

	// Cooldown check to prevent sync abuse
	ipqaCooldownMu.Lock()
	lastSync, exists := ipqaLastSync[node.UUID]
	now := time.Now()
	if exists && now.Sub(lastSync) < ipqaSyncCooldown {
		ipqaCooldownMu.Unlock()
		writeJSONError(w, http.StatusTooManyRequests, "sync cooldown active, please wait a few seconds")
		return
	}
	ipqaLastSync[node.UUID] = now
	ipqaCooldownMu.Unlock()

	// Record security audit log
	_ = s.service.Store().RecordAuditLog(r.Context(), db.AuditLogEntry{
		ActorID:      user.ID,
		ActorName:    user.Login,
		ActorType:    "user",
		Action:       "node.ipqa_sync",
		ResourceType: "node",
		ResourceID:   node.UUID,
		Detail:       fmt.Sprintf("Manual IPQA sync requested for node %s", node.Name),
		IPAddress:    r.RemoteAddr,
		StatusCode:   http.StatusOK,
		CreatedAt:    now.UTC(),
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "IPQA manual sync requested successfully",
	})
}

type testIPQARequest struct {
	Window    string `json:"window"`
	ForceSend bool   `json:"force_send"`
}

// postNodeIPQATest handles POST /api/nodes/:uuid/ipqa/test (Admin authenticated with CSRF).
func (s *Server) postNodeIPQATest(w http.ResponseWriter, r *http.Request, uuid string) {
	if !s.checkIPQARateLimit(w, r) {
		return
	}
	user, err := s.service.CurrentUser(r, time.Now().UTC())
	if err != nil {
		writeAuthenticationError(w, err)
		return
	}

	node, err := s.service.Store().GetNodeByUUID(r.Context(), uuid)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "node not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "failed to query node")
		return
	}

	var req testIPQARequest
	_ = json.NewDecoder(io.LimitReader(r.Body, 1024*32)).Decode(&req)

	_, payload, err := s.service.Store().GetResourceLatest(r.Context(), node.ID)
	var snapshot protocol.ResourceSnapshot
	if err == nil && len(payload) > 0 {
		_ = json.Unmarshal(payload, &snapshot)
	}

	now := time.Now().UTC()
	summaryText := "IPQA 正常运行，无严重风险异常"
	if snapshot.IPQA == nil || !snapshot.IPQA.Installed {
		summaryText = "节点未安装 IP-Quality-Archive 或未上报 IPQA 数据"
	} else if snapshot.IPQA.AlertCount > 0 {
		summaryText = fmt.Sprintf("检出 %d 项告警 (最高严重度: %s)", snapshot.IPQA.AlertCount, snapshot.IPQA.HighestSeverity)
	}

	notificationSent := false
	if req.ForceSend {
		// Enforce notification cooldown
		ipqaCooldownMu.Lock()
		lastNotified, exists := ipqaLastNotify[node.UUID]
		if exists && now.Sub(lastNotified) < ipqaNotifyCooldown {
			ipqaCooldownMu.Unlock()
			writeJSONError(w, http.StatusTooManyRequests, "notification cooldown active, please wait")
			return
		}
		ipqaLastNotify[node.UUID] = now
		ipqaCooldownMu.Unlock()

		if s.notifier != nil {
			testAlert := db.AlertEvent{
				ID:              fmt.Sprintf("ipqa-test-%s-%d", node.ID, now.Unix()),
				NodeID:          node.ID,
				Fingerprint:     fmt.Sprintf("ipqa:%s", node.ID),
				Category:        "ipqa",
				Severity:        "info",
				Reason:          fmt.Sprintf("[IPQA 手动测试] %s: %s", node.Name, summaryText),
				Status:          "firing",
				OccurrenceCount: 1,
				FirstSeenAt:     now,
			}
			_ = s.notifier.Dispatch(r.Context(), testAlert, node)
			notificationSent = true
		}

		_ = s.service.Store().RecordAuditLog(r.Context(), db.AuditLogEntry{
			ActorID:      user.ID,
			ActorName:    user.Login,
			ActorType:    "user",
			Action:       "node.ipqa_test_notify",
			ResourceType: "node",
			ResourceID:   node.UUID,
			Detail:       fmt.Sprintf("Admin triggered IPQA test notification for node %s", node.Name),
			IPAddress:    r.RemoteAddr,
			StatusCode:   http.StatusOK,
			CreatedAt:    now,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":           true,
		"dry_run":           !req.ForceSend,
		"notification_sent": notificationSent,
		"message":           summaryText,
		"timestamp":         now.Format(time.RFC3339),
	})
}

// handleIPQAReport updates archives and dispatches notifications if required.
func (s *Server) handleIPQAReport(ctx context.Context, node db.Node, ipqa *protocol.IPQAInfo, now time.Time) {
	if ipqa == nil || !ipqa.Enabled || !ipqa.Installed {
		return
	}

	// 1. Persist IPv4 archive
	if ipqa.LatestArchiveDate != "" && ipqa.IPv4 != nil {
		b, err := json.Marshal(ipqa.IPv4)
		if err == nil {
			_ = s.service.Store().UpsertIPQAArchive(ctx, db.IPQAArchiveRecord{
				NodeID:          node.ID,
				ArchiveDate:     ipqa.LatestArchiveDate,
				Family:          "IPv4",
				Payload:         b,
				HighestSeverity: ipqa.HighestSeverity,
				AlertCount:      ipqa.AlertCount,
				CheckedAt:       time.Unix(ipqa.LastCheckedAt, 0).UTC(),
				CreatedAt:       now,
			})
		}
	}

	// 2. Persist IPv6 archive
	if ipqa.LatestArchiveDate != "" && ipqa.IPv6 != nil {
		b, err := json.Marshal(ipqa.IPv6)
		if err == nil {
			_ = s.service.Store().UpsertIPQAArchive(ctx, db.IPQAArchiveRecord{
				NodeID:          node.ID,
				ArchiveDate:     ipqa.LatestArchiveDate,
				Family:          "IPv6",
				Payload:         b,
				HighestSeverity: ipqa.HighestSeverity,
				AlertCount:      ipqa.AlertCount,
				CheckedAt:       time.Unix(ipqa.LastCheckedAt, 0).UTC(),
				CreatedAt:       now,
			})
		}
	}

	// 3. Evaluate notification events (with deduplication & cooldown)
	if s.notifier == nil || !s.notifier.Enabled() {
		return
	}

	// Only notify on CRITICAL or WARNING severities, or collection failure
	shouldAlert := false
	sev := strings.ToLower(ipqa.HighestSeverity)
	if sev == "critical" || sev == "warning" {
		shouldAlert = true
	}

	if shouldAlert {
		dedupeKey := fmt.Sprintf("%s:%s:%s", node.ID, ipqa.LatestArchiveDate, sev)
		ipqaCooldownMu.Lock()
		_, alreadySent := ipqaNotifiedKeys[dedupeKey]
		lastSent, hasCooldown := ipqaLastNotify[node.ID]
		nowUTC := time.Now().UTC()
		if alreadySent || (hasCooldown && nowUTC.Sub(lastSent) < ipqaNotifyCooldown) {
			ipqaCooldownMu.Unlock()
			return
		}
		ipqaNotifiedKeys[dedupeKey] = struct{}{}
		ipqaLastNotify[node.ID] = nowUTC
		if len(ipqaNotifiedKeys) > 2000 {
			ipqaNotifiedKeys = make(map[string]struct{})
		}
		ipqaCooldownMu.Unlock()

		reason := fmt.Sprintf("IPQA 风险变动: 检出 %d 项告警，最高等级 %s", ipqa.AlertCount, ipqa.HighestSeverity)
		if len(ipqa.Changes) > 0 {
			reason += fmt.Sprintf(" (%s)", ipqa.Changes[0].After)
		}

		alert := db.AlertEvent{
			ID:              fmt.Sprintf("ipqa-%s-%d", node.ID, now.Unix()),
			NodeID:          node.ID,
			Fingerprint:     fmt.Sprintf("ipqa:%s:%s", node.ID, ipqa.LatestArchiveDate),
			Category:        "ipqa",
			Severity:        sev,
			Reason:          reason,
			Status:          "firing",
			OccurrenceCount: 1,
			FirstSeenAt:     now,
		}
		_ = s.notifier.Dispatch(ctx, alert, node)
		slog.Info("dispatched ipqa alert notification", "node_id", node.ID, "severity", sev)
	}
}

func (s *Server) checkIPQARateLimit(w http.ResponseWriter, r *http.Request) bool {
	if s.ipqaLimiter != nil && !s.ipqaLimiter.Allow(r.RemoteAddr) {
		writeJSONError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return false
	}
	return true
}
