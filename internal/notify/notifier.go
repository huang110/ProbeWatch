package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/probewatch/probewatch/internal/config"
	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/protocol"
	"github.com/probewatch/probewatch/internal/security"
)

const maxWebhookResponseBytes = 64 * 1024

type Notifier struct {
	cfg             config.Config
	store           *db.Store
	client          *http.Client
	flappingTracker *FlappingTracker
	mu              sync.Mutex
	dispatched      map[string]int64 // alertID -> occurrenceCount
}

func NewNotifier(cfg config.Config) *Notifier {
	return NewNotifierWithStore(cfg, nil)
}

func NewNotifierWithStore(cfg config.Config, store *db.Store) *Notifier {
	return &Notifier{
		cfg:             cfg,
		store:           store,
		flappingTracker: NewFlappingTracker(),
		client: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				Proxy:                  nil,
				MaxResponseHeaderBytes: 32 << 10,
				DialContext:            safeDialContext,
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return fmt.Errorf("notification redirect limit exceeded")
				}
				return nil
			},
		},
		dispatched: make(map[string]int64),
	}
}

func (n *Notifier) FlappingTracker() *FlappingTracker {
	return n.flappingTracker
}

func (n *Notifier) SetFlappingTracker(t *FlappingTracker) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.flappingTracker = t
}

func (n *Notifier) SetStore(store *db.Store) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.store = store
}

func safeDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if ip, parseErr := netip.ParseAddr(host); parseErr == nil {
		if security.IsBlockedAddress(ip) {
			return nil, fmt.Errorf("notification target address is blocked: %s", ip)
		}
		return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if err := security.ValidateResolvedIPs(addresses); err != nil {
		return nil, fmt.Errorf("notification target address is blocked: %w", err)
	}
	return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(addresses[0].String(), port))
}

func (n *Notifier) Enabled() bool {
	if (strings.TrimSpace(n.cfg.TelegramBotToken) != "" && strings.TrimSpace(n.cfg.TelegramChatID) != "") ||
		strings.TrimSpace(n.cfg.WebhookURL) != "" {
		return true
	}
	if n.store != nil {
		channels, err := n.store.ListNotificationChannels(context.Background())
		if err == nil {
			for _, ch := range channels {
				if ch.Enabled {
					return true
				}
			}
		}
	}
	return false
}

func (n *Notifier) Dispatch(ctx context.Context, alert db.AlertEvent, node db.Node) error {
	// 1. Check if alert is silenced (via maintenance window or snooze)
	if n.store != nil {
		silenced, reason, sErr := n.store.IsAlertSilenced(ctx, alert.NodeID, alert.Category, alert.TargetID, alert.Fingerprint, time.Now().UTC())
		if sErr == nil && silenced {
			slog.Info("alert silenced by maintenance window or snooze", "alert_id", alert.ID, "node_id", alert.NodeID, "reason", reason)
			return nil
		}
	}

	// 2. Check flapping tracker
	if n.flappingTracker != nil {
		isFlapping, justEntered := n.flappingTracker.RecordAlert(alert.NodeID, alert.Fingerprint, alert.Status, time.Now().UTC())
		if justEntered {
			slog.Warn("alert entered flapping state, suppressing repetitive notifications", "node_id", alert.NodeID, "fingerprint", alert.Fingerprint)
			flappingAlert := alert
			flappingAlert.Severity = "warning"
			flappingAlert.Reason = "flapping_suppressed: 状态频繁抖动(5分钟内>=4次切换)，已自动开启静默抑制，指标稳定3分钟后自动恢复"
			_ = n.dispatchToChannels(ctx, flappingAlert, node)
			return nil
		}
		if isFlapping {
			slog.Debug("suppressing notification for flapping alert", "node_id", alert.NodeID, "fingerprint", alert.Fingerprint)
			return nil
		}
	}

	n.mu.Lock()
	lastCount, exists := n.dispatched[alert.ID]
	// If already dispatched this occurrence count and status hasn't resolved, skip
	if exists && lastCount >= int64(alert.OccurrenceCount) && alert.Status != db.AlertStatusResolved {
		n.mu.Unlock()
		return nil
	}
	// If resolved was already dispatched once
	if exists && alert.Status == db.AlertStatusResolved && lastCount == -1 {
		n.mu.Unlock()
		return nil
	}
	n.mu.Unlock()

	err := n.dispatchToChannels(ctx, alert, node)

	n.mu.Lock()
	if alert.Status == db.AlertStatusResolved {
		n.dispatched[alert.ID] = -1 // marked as resolved dispatched
	} else {
		n.dispatched[alert.ID] = int64(alert.OccurrenceCount)
	}
	if len(n.dispatched) > 5000 {
		trimmed := make(map[string]int64)
		for k, v := range n.dispatched {
			trimmed[k] = v
			if len(trimmed) >= 2500 {
				break
			}
		}
		n.dispatched = trimmed
	}
	n.mu.Unlock()

	return err
}

func (n *Notifier) dispatchToChannels(ctx context.Context, alert db.AlertEvent, node db.Node) error {
	var channels []db.NotificationChannel
	if n.store != nil {
		dbChannels, err := n.store.ListNotificationChannels(ctx)
		if err == nil {
			for _, ch := range dbChannels {
				if ch.Enabled {
					channels = append(channels, ch)
				}
			}
		}
	}

	// Fallback to env vars if no active DB channels exist
	if len(channels) == 0 {
		if strings.TrimSpace(n.cfg.TelegramBotToken) != "" && strings.TrimSpace(n.cfg.TelegramChatID) != "" {
			cfgBytes, _ := json.Marshal(map[string]string{
				"bot_token": n.cfg.TelegramBotToken,
				"chat_id":   n.cfg.TelegramChatID,
			})
			channels = append(channels, db.NotificationChannel{
				ID:      "env-telegram",
				Name:    "Telegram (Env)",
				Type:    "telegram",
				Config:  string(cfgBytes),
				Enabled: true,
				Events:  "[]",
			})
		}
		if strings.TrimSpace(n.cfg.WebhookURL) != "" {
			cfgBytes, _ := json.Marshal(map[string]string{
				"webhook_url": n.cfg.WebhookURL,
			})
			channels = append(channels, db.NotificationChannel{
				ID:      "env-webhook",
				Name:    "Webhook (Env)",
				Type:    "webhook",
				Config:  string(cfgBytes),
				Enabled: true,
				Events:  "[]",
			})
		}
	}

	if len(channels) == 0 {
		return nil
	}

	var errs []string
	for _, ch := range channels {
		if !n.isSubscribed(ch, alert.Category) {
			continue
		}
		if err := n.dispatchToChannel(ctx, ch, alert, node); err != nil {
			errs = append(errs, fmt.Sprintf("%s (%s): %v", ch.Name, ch.Type, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("dispatch errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (n *Notifier) isSubscribed(ch db.NotificationChannel, category string) bool {
	if strings.TrimSpace(ch.Events) == "" || ch.Events == "[]" {
		return true
	}
	var events []string
	if err := json.Unmarshal([]byte(ch.Events), &events); err != nil || len(events) == 0 {
		return true
	}
	for _, ev := range events {
		if strings.EqualFold(ev, category) || ev == "*" || ev == "all" {
			return true
		}
	}
	return false
}

func (n *Notifier) dispatchToChannel(ctx context.Context, ch db.NotificationChannel, alert db.AlertEvent, node db.Node) error {
	switch strings.ToLower(ch.Type) {
	case "telegram":
		var conf struct {
			BotToken string `json:"bot_token"`
			ChatID   string `json:"chat_id"`
		}
		if err := json.Unmarshal([]byte(ch.Config), &conf); err != nil {
			return fmt.Errorf("invalid telegram config: %w", err)
		}
		return n.sendTelegram(ctx, conf.BotToken, conf.ChatID, alert, node)
	case "discord":
		var conf struct {
			WebhookURL string `json:"webhook_url"`
		}
		if err := json.Unmarshal([]byte(ch.Config), &conf); err != nil {
			return fmt.Errorf("invalid discord config: %w", err)
		}
		return n.sendDiscord(ctx, conf.WebhookURL, alert, node)
	case "wecom", "wechat":
		var conf struct {
			WebhookURL string `json:"webhook_url"`
		}
		if err := json.Unmarshal([]byte(ch.Config), &conf); err != nil {
			return fmt.Errorf("invalid wecom config: %w", err)
		}
		return n.sendWeCom(ctx, conf.WebhookURL, alert, node)
	case "bark":
		var conf struct {
			ServerURL string `json:"server_url"`
			DeviceKey string `json:"device_key"`
		}
		if err := json.Unmarshal([]byte(ch.Config), &conf); err != nil {
			return fmt.Errorf("invalid bark config: %w", err)
		}
		serverURL := strings.TrimRight(strings.TrimSpace(conf.ServerURL), "/")
		if serverURL == "" {
			serverURL = "https://api.day.app"
		}
		return n.sendBark(ctx, serverURL, conf.DeviceKey, alert, node)
	case "webhook":
		var conf struct {
			WebhookURL string `json:"webhook_url"`
		}
		if err := json.Unmarshal([]byte(ch.Config), &conf); err != nil {
			return fmt.Errorf("invalid webhook config: %w", err)
		}
		return n.sendWebhook(ctx, conf.WebhookURL, alert, node)
	default:
		return fmt.Errorf("unsupported channel type: %s", ch.Type)
	}
}

func (n *Notifier) SendTestNotification(ctx context.Context, channelType, rawConfig string) error {
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
		Name: "ProbeWatch 探针管理节点",
	}
	ch := db.NotificationChannel{
		ID:      "test",
		Name:    "Test Channel",
		Type:    channelType,
		Config:  rawConfig,
		Enabled: true,
	}
	return n.dispatchToChannel(ctx, ch, testAlert, testNode)
}

func formatEmoji(alert db.AlertEvent) string {
	if alert.Status == db.AlertStatusResolved {
		return "✅"
	}
	if alert.Severity == "critical" {
		return "🚨"
	}
	if alert.Severity == "warning" {
		return "⚠️"
	}
	return "ℹ️"
}

func formatCategoryTitle(category, reason string) string {
	if strings.HasPrefix(reason, "flapping_suppressed") {
		return "告警抖动抑制通知"
	}
	switch strings.ToLower(category) {
	case "node":
		if reason == "node_offline" {
			return "探针离线"
		}
		return "节点状态"
	case "resource":
		switch reason {
		case "cpu_high":
			return "CPU负载过高"
		case "memory_high":
			return "内存占用过高"
		case "filesystem_high":
			return "磁盘空间不足"
		default:
			return "系统资源告警"
		}
	case "network":
		switch reason {
		case "timeout":
			return "网络连接超时"
		case "packet_loss":
			return "网络丢包超标"
		case "high_latency":
			return "网络延迟突增"
		default:
			return "网络质量异常"
		}
	case "mtr":
		if reason == "path_changed" {
			return "MTR路由拓扑漂移"
		}
		return "MTR路由异常"
	case "media":
		return "流媒体/AI解锁失效"
	case "traffic":
		return "流量限额预警"
	case "billing":
		return "服务器到期提醒"
	default:
		return strings.ToUpper(category)
	}
}

func formatSeverityName(sev string) string {
	switch strings.ToLower(sev) {
	case "critical":
		return "紧急 (Critical)"
	case "warning":
		return "警告 (Warning)"
	case "info":
		return "提醒 (Info)"
	default:
		return sev
	}
}

func (n *Notifier) sendTelegram(ctx context.Context, token, chatID string, alert db.AlertEvent, node db.Node) error {
	if strings.TrimSpace(token) == "" || strings.TrimSpace(chatID) == "" {
		return fmt.Errorf("telegram token and chat_id are required")
	}

	emoji := formatEmoji(alert)
	catTitle := formatCategoryTitle(alert.Category, alert.Reason)
	nodeName := node.Name
	if nodeName == "" {
		nodeName = alert.NodeID
	}

	headerText := fmt.Sprintf("%s <b>ProbeWatch 告警通知 · %s</b>", emoji, catTitle)
	if alert.Status == db.AlertStatusResolved {
		headerText = fmt.Sprintf("✅ <b>ProbeWatch 恢复通知 · %s 已恢复</b>", catTitle)
	}

	message := fmt.Sprintf(
		"%s\n\n"+
			"<b>节点:</b> <code>%s</code>\n"+
			"<b>状态:</b> %s | <b>等级:</b> %s\n"+
			"<b>原因:</b> %s\n"+
			"<b>目标:</b> %s\n"+
			"<b>发生次数:</b> %d\n"+
			"<b>时间:</b> %s",
		headerText,
		html.EscapeString(nodeName),
		html.EscapeString(alert.Status),
		html.EscapeString(formatSeverityName(alert.Severity)),
		html.EscapeString(alert.Reason),
		html.EscapeString(alert.TargetID),
		alert.OccurrenceCount,
		alert.LastSeenAt.Format("2006-01-02 15:04:05 UTC"),
	)

	payload := map[string]string{
		"chat_id":    chatID,
		"text":       message,
		"parse_mode": "HTML",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxWebhookResponseBytes))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram returned status %d", resp.StatusCode)
	}
	return nil
}

func (n *Notifier) sendDiscord(ctx context.Context, webhookURL string, alert db.AlertEvent, node db.Node) error {
	if strings.TrimSpace(webhookURL) == "" {
		return fmt.Errorf("discord webhook URL is required")
	}

	emoji := formatEmoji(alert)
	catTitle := formatCategoryTitle(alert.Category, alert.Reason)
	nodeName := node.Name
	if nodeName == "" {
		nodeName = alert.NodeID
	}

	// Discord color: 15158332 (Red), 15105570 (Orange/Amber), 3066993 (Green), 3447003 (Blue)
	color := 15158332
	if alert.Status == db.AlertStatusResolved {
		color = 3066993
	} else if alert.Severity == "warning" {
		color = 15105570
	} else if alert.Severity == "info" {
		color = 3447003
	}

	title := fmt.Sprintf("%s [ProbeWatch] %s: %s", emoji, catTitle, nodeName)
	if alert.Status == db.AlertStatusResolved {
		title = fmt.Sprintf("✅ [ProbeWatch 恢复] %s: %s 恢复正常", catTitle, nodeName)
	}

	payload := map[string]any{
		"embeds": []map[string]any{
			{
				"title":       title,
				"description": fmt.Sprintf("**告警原因:** `%s`", alert.Reason),
				"color":       color,
				"fields": []map[string]any{
					{"name": "节点名称", "value": nodeName, "inline": true},
					{"name": "监控状态", "value": alert.Status, "inline": true},
					{"name": "告警等级", "value": formatSeverityName(alert.Severity), "inline": true},
					{"name": "检测目标", "value": alert.TargetID, "inline": true},
					{"name": "发生次数", "value": strconv.Itoa(alert.OccurrenceCount), "inline": true},
					{"name": "触发时间", "value": alert.LastSeenAt.Format("2006-01-02 15:04:05 UTC"), "inline": true},
				},
				"footer": map[string]string{
					"text": "ProbeWatch 实时监控告警",
				},
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxWebhookResponseBytes))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord returned status %d", resp.StatusCode)
	}
	return nil
}

func (n *Notifier) sendWeCom(ctx context.Context, webhookURL string, alert db.AlertEvent, node db.Node) error {
	if strings.TrimSpace(webhookURL) == "" {
		return fmt.Errorf("wecom webhook URL is required")
	}

	emoji := formatEmoji(alert)
	catTitle := formatCategoryTitle(alert.Category, alert.Reason)
	nodeName := node.Name
	if nodeName == "" {
		nodeName = alert.NodeID
	}

	statusColor := "warning"
	if alert.Status == db.AlertStatusResolved {
		statusColor = "info"
	}

	content := fmt.Sprintf(
		"### %s ProbeWatch 监控告警 · %s\n"+
			"> **节点:** <font color=\"info\">%s</font>\n"+
			"> **状态:** <font color=\"%s\">%s</font> | **等级:** %s\n"+
			"> **原因:** %s\n"+
			"> **目标:** %s\n"+
			"> **发生次数:** %d\n"+
			"> **时间:** %s",
		emoji,
		catTitle,
		nodeName,
		statusColor,
		alert.Status,
		formatSeverityName(alert.Severity),
		alert.Reason,
		alert.TargetID,
		alert.OccurrenceCount,
		alert.LastSeenAt.Format("2006-01-02 15:04:05 UTC"),
	)

	payload := map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"content": content,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxWebhookResponseBytes))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("wecom returned status %d", resp.StatusCode)
	}
	return nil
}

func (n *Notifier) sendBark(ctx context.Context, serverURL, deviceKey string, alert db.AlertEvent, node db.Node) error {
	if strings.TrimSpace(deviceKey) == "" {
		return fmt.Errorf("bark device key is required")
	}

	emoji := formatEmoji(alert)
	catTitle := formatCategoryTitle(alert.Category, alert.Reason)
	nodeName := node.Name
	if nodeName == "" {
		nodeName = alert.NodeID
	}

	title := fmt.Sprintf("%s ProbeWatch: %s · %s", emoji, catTitle, nodeName)
	bodyText := fmt.Sprintf("节点: %s\n状态: %s | 等级: %s\n原因: %s\n时间: %s",
		nodeName, alert.Status, formatSeverityName(alert.Severity), alert.Reason, alert.LastSeenAt.Format("15:04:05 UTC"))

	pushURL := fmt.Sprintf("%s/push", strings.TrimRight(serverURL, "/"))
	payload := map[string]any{
		"device_key": deviceKey,
		"title":      title,
		"body":       bodyText,
		"category":   "probewatch",
		"group":      "ProbeWatch",
		"level":      "timeSensitive",
		"badge":      1,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pushURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxWebhookResponseBytes))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("bark returned status %d", resp.StatusCode)
	}
	return nil
}

func (n *Notifier) sendWebhook(ctx context.Context, webhookURL string, alert db.AlertEvent, node db.Node) error {
	if strings.TrimSpace(webhookURL) == "" {
		return fmt.Errorf("webhook URL is required")
	}

	nodeName := node.Name
	if nodeName == "" {
		nodeName = alert.NodeID
	}

	// ServerChan format compatibility
	if strings.Contains(webhookURL, "ftqq.com") {
		catTitle := formatCategoryTitle(alert.Category, alert.Reason)
		title := fmt.Sprintf("%s [ProbeWatch] %s: %s", formatEmoji(alert), catTitle, nodeName)
		desp := fmt.Sprintf("### 节点: %s\n- 状态: %s\n- 等级: %s\n- 原因: %s\n- 时间: %s",
			nodeName, alert.Status, alert.Severity, alert.Reason, alert.LastSeenAt.Format("2006-01-02 15:04:05 UTC"))
		form := url.Values{}
		form.Set("title", title)
		form.Set("desp", desp)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, strings.NewReader(form.Encode()))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := n.client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("serverchan returned status %d", resp.StatusCode)
		}
		return nil
	}

	payload := map[string]any{
		"event":            "alert",
		"id":               alert.ID,
		"node_id":          alert.NodeID,
		"node_name":        nodeName,
		"category":         alert.Category,
		"category_title":   formatCategoryTitle(alert.Category, alert.Reason),
		"target_id":        alert.TargetID,
		"severity":         alert.Severity,
		"status":           alert.Status,
		"reason":           alert.Reason,
		"occurrence_count": alert.OccurrenceCount,
		"first_seen_at":    alert.FirstSeenAt,
		"last_seen_at":     alert.LastSeenAt,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxWebhookResponseBytes))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}

func RunAlertDispatcher(ctx context.Context, store *db.Store, notifier *Notifier) {
	if notifier != nil {
		notifier.SetStore(store)
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	scanAndDispatch := func() {
		scanCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
		defer cancel()

		now := time.Now().UTC()

		// 1. Evaluate Node Offline / Online status for all nodes
		nodes, err := store.ListNodes(scanCtx)
		if err == nil {
			// Read offline grace period setting (default 180s)
			gracePeriodSec := 180
			if val, sErr := store.GetSetting(scanCtx, "offline_grace_seconds", "180"); sErr == nil {
				if n, parseErr := strconv.Atoi(strings.TrimSpace(val)); parseErr == nil && n >= 30 && n <= 3600 {
					gracePeriodSec = n
				}
			}
			gracePeriod := time.Duration(gracePeriodSec) * time.Second

			for _, node := range nodes {
				reportedAt, _, rErr := store.GetResourceLatest(scanCtx, node.ID)
				if rErr == nil && !reportedAt.IsZero() {
					isOffline := time.Since(reportedAt) > gracePeriod
					_ = store.EvaluateAlert(scanCtx, node.ID, db.AlertEvaluation{
						Category: "node",
						TargetID: "heartbeat",
						Reason:   "node_offline",
						Severity: db.AlertSeverityCrit,
						Failing:  isOffline,
					}, now)
				}
			}
		}

		// 1b. Evaluate SSL/TLS Certificate Expiration for monitored targets
		allNetworkLatest, netErr := store.ListAllNetworkLatest(scanCtx)
		if netErr == nil {
			for _, item := range allNetworkLatest {
				var res protocol.NetworkResult
				if json.Unmarshal(item.Payload, &res) == nil && res.TLSCert != nil {
					failing := res.TLSCert.IsExpired || res.TLSCert.ExpiringSoon
					severity := db.AlertSeverityWarn
					reason := "tls_cert_expiring_soon"
					if res.TLSCert.IsExpired || res.TLSCert.DaysLeft <= 3 {
						severity = db.AlertSeverityCrit
						reason = "tls_cert_expired"
					}
					_ = store.EvaluateAlert(scanCtx, item.NodeID, db.AlertEvaluation{
						Category: "security",
						TargetID: item.TargetID,
						Reason:   reason,
						Severity: severity,
						Failing:  failing,
					}, now)
				}
			}
		}

		// 2. Dispatch open alerts
		alerts, err := store.ListAlerts(scanCtx, db.AlertQuery{
			Statuses: []string{db.AlertStatusOpen},
			Limit:    100,
		})
		if err != nil {
			slog.Error("scan alert events failed", "error", err)
			return
		}

		for _, alert := range alerts {
			node, nErr := store.GetNodeByUUID(scanCtx, alert.NodeID)
			if nErr != nil {
				node = db.Node{ID: alert.NodeID, Name: alert.NodeID}
			}
			if err := notifier.Dispatch(scanCtx, alert, node); err != nil {
				slog.Error("dispatch alert failed", "alert_id", alert.ID, "error", err)
			}
		}

		// 3. Dispatch recently resolved alerts (e.g. resolved in last 45 seconds)
		resolvedAlerts, err := store.ListAlerts(scanCtx, db.AlertQuery{
			Statuses: []string{db.AlertStatusResolved},
			From:     now.Add(-45 * time.Second),
			To:       now,
			Limit:    50,
		})
		if err == nil {
			for _, alert := range resolvedAlerts {
				node, nErr := store.GetNodeByUUID(scanCtx, alert.NodeID)
				if nErr != nil {
					node = db.Node{ID: alert.NodeID, Name: alert.NodeID}
				}
				_ = notifier.Dispatch(scanCtx, alert, node)
			}
		}
	}

	scanAndDispatch()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			scanAndDispatch()
		}
	}
}
