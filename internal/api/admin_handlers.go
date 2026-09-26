package api

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	cleanPath := strings.TrimRight(r.URL.Path, "/")
	if r.Method == http.MethodGet && cleanPath == "/api/alerts" {
		s.listAlerts(w, r)
		return
	}
	if r.Method == http.MethodPost && strings.HasSuffix(cleanPath, "/ack") {
		NewMiddleware(s.service, s.cfg).RequireCSRF(http.HandlerFunc(s.ackAlert)).ServeHTTP(w, r)
		return
	}

	// Alert Rules endpoints
	if cleanPath == "/api/alerts/rules" {
		switch r.Method {
		case http.MethodGet:
			s.listAlertRules(w, r)
		case http.MethodPost:
			NewMiddleware(s.service, s.cfg).RequireCSRF(http.HandlerFunc(s.createAlertRule)).ServeHTTP(w, r)
		default:
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	if strings.HasPrefix(cleanPath, "/api/alerts/rules/") {
		parts := strings.Split(cleanPath, "/")
		if len(parts) == 5 {
			ruleID := parts[4]
			switch r.Method {
			case http.MethodGet:
				s.getAlertRule(w, r, ruleID)
			case http.MethodPut:
				NewMiddleware(s.service, s.cfg).RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					s.updateAlertRule(w, r, ruleID)
				})).ServeHTTP(w, r)
			case http.MethodDelete:
				NewMiddleware(s.service, s.cfg).RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					s.deleteAlertRule(w, r, ruleID)
				})).ServeHTTP(w, r)
			default:
				writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			}
			return
		}
		if len(parts) == 6 && parts[5] == "toggle" {
			ruleID := parts[4]
			if r.Method != http.MethodPost {
				writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			NewMiddleware(s.service, s.cfg).RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				s.toggleAlertRule(w, r, ruleID)
			})).ServeHTTP(w, r)
			return
		}
	}

	// Alert Silences endpoints
	if cleanPath == "/api/alerts/silences" {
		switch r.Method {
		case http.MethodGet:
			s.listAlertSilences(w, r)
		case http.MethodPost:
			NewMiddleware(s.service, s.cfg).RequireCSRF(http.HandlerFunc(s.createAlertSilence)).ServeHTTP(w, r)
		default:
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	if strings.HasPrefix(cleanPath, "/api/alerts/silences/") {
		parts := strings.Split(cleanPath, "/")
		if len(parts) == 5 {
			silenceID := parts[4]
			if r.Method == http.MethodDelete {
				NewMiddleware(s.service, s.cfg).RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					s.deleteAlertSilence(w, r, silenceID)
				})).ServeHTTP(w, r)
				return
			}
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
	}

	// Flapping targets endpoint
	if cleanPath == "/api/alerts/flapping" {
		if r.Method != http.MethodGet {
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.listFlappingAlerts(w, r)
		return
	}

	// Channel endpoints
	if cleanPath == "/api/alerts/channels" {
		switch r.Method {
		case http.MethodGet:
			s.listNotificationChannels(w, r)
		case http.MethodPost:
			NewMiddleware(s.service, s.cfg).RequireCSRF(http.HandlerFunc(s.createNotificationChannel)).ServeHTTP(w, r)
		default:
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	if strings.HasPrefix(cleanPath, "/api/alerts/channels/") {
		parts := strings.Split(cleanPath, "/")
		if len(parts) == 5 {
			channelID := parts[4]
			switch r.Method {
			case http.MethodGet:
				s.getNotificationChannel(w, r, channelID)
			case http.MethodPut:
				NewMiddleware(s.service, s.cfg).RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					s.updateNotificationChannel(w, r, channelID)
				})).ServeHTTP(w, r)
			case http.MethodDelete:
				NewMiddleware(s.service, s.cfg).RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					s.deleteNotificationChannel(w, r, channelID)
				})).ServeHTTP(w, r)
			default:
				writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			}
			return
		}
		if len(parts) == 6 && parts[5] == "test" {
			channelID := parts[4]
			if r.Method != http.MethodPost {
				writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			NewMiddleware(s.service, s.cfg).RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				s.testNotificationChannel(w, r, channelID)
			})).ServeHTTP(w, r)
			return
		}
	}

	// Direct test endpoint
	if cleanPath == "/api/alerts/test" {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		NewMiddleware(s.service, s.cfg).RequireCSRF(http.HandlerFunc(s.testNotificationDirect)).ServeHTTP(w, r)
		return
	}

	// Settings endpoint
	if cleanPath == "/api/alerts/settings" {
		switch r.Method {
		case http.MethodGet:
			s.getAlertSettings(w, r)
		case http.MethodPost, http.MethodPut:
			NewMiddleware(s.service, s.cfg).RequireCSRF(http.HandlerFunc(s.saveAlertSettings)).ServeHTTP(w, r)
		default:
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	writeJSONError(w, http.StatusNotFound, "not found")
}

func (s *Server) settingsRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.getAlertSettings(w, r)
	case http.MethodPost, http.MethodPut:
		NewMiddleware(s.service, s.cfg).RequireCSRF(http.HandlerFunc(s.saveAlertSettings)).ServeHTTP(w, r)
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

type channelRequest struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Config  string `json:"config"`
	Enabled bool   `json:"enabled"`
	Events  string `json:"events"`
}

type testNotificationRequest struct {
	ChannelID string `json:"channel_id,omitempty"`
	Type      string `json:"type,omitempty"`
	Config    any    `json:"config,omitempty"`
}

func maskToken(token string) string {
	token = strings.TrimSpace(token)
	if len(token) <= 8 {
		return "******"
	}
	return token[:4] + "******" + token[len(token)-4:]
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("ch-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func (s *Server) listNotificationChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := s.service.Store().ListNotificationChannels(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "service unavailable")
		return
	}
	// Synthesize fallback channels from env if DB has none
	if len(channels) == 0 {
		if strings.TrimSpace(s.cfg.TelegramBotToken) != "" && strings.TrimSpace(s.cfg.TelegramChatID) != "" {
			conf, _ := json.Marshal(map[string]string{
				"bot_token": maskToken(s.cfg.TelegramBotToken),
				"chat_id":   s.cfg.TelegramChatID,
			})
			channels = append(channels, db.NotificationChannel{
				ID:        "env-telegram",
				Name:      "Telegram Bot (环境变量)",
				Type:      "telegram",
				Config:    string(conf),
				Enabled:   true,
				Events:    "[]",
				CreatedAt: time.Now().UTC(),
				UpdatedAt: time.Now().UTC(),
			})
		}
		if strings.TrimSpace(s.cfg.WebhookURL) != "" {
			conf, _ := json.Marshal(map[string]string{
				"webhook_url": s.cfg.WebhookURL,
			})
			channels = append(channels, db.NotificationChannel{
				ID:        "env-webhook",
				Name:      "Webhook (环境变量)",
				Type:      "webhook",
				Config:    string(conf),
				Enabled:   true,
				Events:    "[]",
				CreatedAt: time.Now().UTC(),
				UpdatedAt: time.Now().UTC(),
			})
		}
	}
	writeJSON(w, http.StatusOK, channels)
}

func (s *Server) getNotificationChannel(w http.ResponseWriter, r *http.Request, id string) {
	ch, err := s.service.Store().GetNotificationChannel(r.Context(), id)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "channel not found")
		return
	}
	writeJSON(w, http.StatusOK, ch)
}

func (s *Server) createNotificationChannel(w http.ResponseWriter, r *http.Request) {
	var req channelRequest
	if err := decodeJSONObjectRequest(w, r, s.requestBodyLimit(), &req); err != nil {
		writeRequestError(w, err)
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Type) == "" {
		writeJSONError(w, http.StatusBadRequest, "name and type are required")
		return
	}
	ch := db.NotificationChannel{
		ID:        randomHex(8),
		Name:      strings.TrimSpace(req.Name),
		Type:      strings.TrimSpace(strings.ToLower(req.Type)),
		Config:    req.Config,
		Enabled:   req.Enabled,
		Events:    req.Events,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := s.service.Store().UpsertNotificationChannel(r.Context(), ch); err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "failed to save channel")
		return
	}
	writeJSON(w, http.StatusCreated, ch)
}

func (s *Server) updateNotificationChannel(w http.ResponseWriter, r *http.Request, id string) {
	var req channelRequest
	if err := decodeJSONObjectRequest(w, r, s.requestBodyLimit(), &req); err != nil {
		writeRequestError(w, err)
		return
	}
	ch := db.NotificationChannel{
		ID:        id,
		Name:      strings.TrimSpace(req.Name),
		Type:      strings.TrimSpace(strings.ToLower(req.Type)),
		Config:    req.Config,
		Enabled:   req.Enabled,
		Events:    req.Events,
		UpdatedAt: time.Now().UTC(),
	}
	if err := s.service.Store().UpsertNotificationChannel(r.Context(), ch); err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "failed to update channel")
		return
	}
	writeJSON(w, http.StatusOK, ch)
}

func (s *Server) deleteNotificationChannel(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.service.Store().DeleteNotificationChannel(r.Context(), id); err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "failed to delete channel")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) testNotificationChannel(w http.ResponseWriter, r *http.Request, id string) {
	ch, err := s.service.Store().GetNotificationChannel(r.Context(), id)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "channel not found")
		return
	}
	if s.notifier == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "notifier service unavailable")
		return
	}
	if err := s.notifier.SendTestNotification(r.Context(), ch.Type, ch.Config); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("测试消息发送失败: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "message": "测试通知已成功推送到 " + ch.Name})
}

func (s *Server) testNotificationDirect(w http.ResponseWriter, r *http.Request) {
	var req testNotificationRequest
	_ = decodeJSONObjectRequest(w, r, s.requestBodyLimit(), &req)

	if s.notifier == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "notifier service unavailable")
		return
	}

	if req.ChannelID != "" {
		ch, err := s.service.Store().GetNotificationChannel(r.Context(), req.ChannelID)
		if err != nil {
			writeJSONError(w, http.StatusNotFound, "channel not found")
			return
		}
		if err := s.notifier.SendTestNotification(r.Context(), ch.Type, ch.Config); err != nil {
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("测试消息发送失败: %v", err))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "message": "测试通知已成功推送到 " + ch.Name})
		return
	}

	if req.Type != "" && req.Config != nil {
		var cfgStr string
		switch v := req.Config.(type) {
		case string:
			cfgStr = v
		default:
			b, _ := json.Marshal(v)
			cfgStr = string(b)
		}
		if err := s.notifier.SendTestNotification(r.Context(), req.Type, cfgStr); err != nil {
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("测试消息发送失败: %v", err))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "message": "测试通知已成功推送到 " + req.Type})
		return
	}

	// Default: send test alert to all active channels
	testAlert := db.AlertEvent{
		ID:              "test-" + strconv.FormatInt(time.Now().Unix(), 10),
		NodeID:          "probewatch-control-plane",
		Category:        "node",
		TargetID:        "heartbeat",
		Reason:          "测试通知：监控通道连接成功，ProbeWatch 守护就绪！",
		Severity:        "info",
		Status:          "open",
		OccurrenceCount: 1,
		FirstSeenAt:     time.Now().UTC(),
		LastSeenAt:      time.Now().UTC(),
	}
	testNode := db.Node{
		ID:   "probewatch-control-plane",
		Name: "ProbeWatch 监控中心",
	}
	if err := s.notifier.Dispatch(r.Context(), testAlert, testNode); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("测试消息发送失败: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "message": "测试通知已成功发送至所有可用渠道"})
}

func (s *Server) getAlertSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.service.Store().GetAllSettings(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "service unavailable")
		return
	}
	if _, ok := settings["offline_grace_seconds"]; !ok {
		settings["offline_grace_seconds"] = "180"
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) saveAlertSettings(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	if err := decodeJSONObjectRequest(w, r, s.requestBodyLimit(), &body); err != nil {
		writeRequestError(w, err)
		return
	}
	for k, v := range body {
		if err := s.service.Store().SetSetting(r.Context(), k, v); err != nil {
			writeJSONError(w, http.StatusServiceUnavailable, "failed to save settings")
			return
		}
	}
	s.getAlertSettings(w, r)
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
	Tags           string          `json:"tags"`
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
	user, hasUser := UserFromContext(r.Context())
	response := make([]nodeResponse, 0, len(nodes))
	for _, node := range nodes {
		if hasUser && !user.CanAccessNode(node.UUID) {
			continue
		}
		response = append(response, s.nodeSummary(r.Context(), node))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) nodeSummary(ctx context.Context, node db.Node) nodeResponse {
	response := nodeResponse{ID: node.ID, UUID: node.UUID, Name: node.Name, Tags: node.Tags, Status: "offline"}
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
	if v := q.Get("range"); v != "" {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "realtime", "live", "15m", "15min":
			from = now.Add(-15 * time.Minute)
			if q.Get("limit") == "" {
				limit = 60
			}
		case "1h":
			from = now.Add(-1 * time.Hour)
			if q.Get("limit") == "" {
				limit = 60
			}
		case "4h":
			from = now.Add(-4 * time.Hour)
			if q.Get("limit") == "" {
				limit = 80
			}
		case "6h":
			from = now.Add(-6 * time.Hour)
			if q.Get("limit") == "" {
				limit = 80
			}
		case "12h":
			from = now.Add(-12 * time.Hour)
			if q.Get("limit") == "" {
				limit = 90
			}
		case "1d", "24h":
			from = now.Add(-24 * time.Hour)
			if q.Get("limit") == "" {
				limit = 100
			}
		case "7d":
			from = now.Add(-7 * 24 * time.Hour)
			if q.Get("limit") == "" {
				limit = 120
			}
		case "30d":
			from = now.Add(-30 * 24 * time.Hour)
			if q.Get("limit") == "" {
				limit = 150
			}
		}
	}
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
	user, hasUser := UserFromContext(r.Context())
	if hasUser && !user.IsAdmin() && user.AllowedNodes != "*" && user.AllowedNodes != "" {
		filtered := make([]db.Node, 0, len(nodes))
		for _, n := range nodes {
			if user.CanAccessNode(n.UUID) {
				filtered = append(filtered, n)
			}
		}
		nodes = filtered
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
	if user, hasUser := UserFromContext(r.Context()); hasUser {
		if !user.CanAccessNode(uuid) {
			writeJSONError(w, http.StatusForbidden, "access to node denied by scope")
			return
		}
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
	if user, hasUser := UserFromContext(r.Context()); hasUser {
		if !user.CanAccessNode(parts[2]) {
			writeJSONError(w, http.StatusForbidden, "access to node denied by scope")
			return
		}
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
