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
	"net/netip"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/probewatch/probewatch/internal/config"
	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/security"
)

const maxWebhookResponseBytes = 64 * 1024

type Notifier struct {
	telegramBotToken string
	telegramChatID   string
	webhookURL       string
	client           *http.Client
	mu               sync.Mutex
	dispatched       map[string]int64 // alertID -> occurrenceCount
}

func NewNotifier(cfg config.Config) *Notifier {
	webhookURL := strings.TrimSpace(cfg.WebhookURL)
	allowedOrigin := ""
	if parsed, err := url.Parse(webhookURL); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		allowedOrigin = strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host)
	}
	return &Notifier{
		telegramBotToken: strings.TrimSpace(cfg.TelegramBotToken),
		telegramChatID:   strings.TrimSpace(cfg.TelegramChatID),
		webhookURL:       webhookURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				Proxy: nil,
				MaxResponseHeaderBytes: 32 << 10,
				DialContext: safeDialContext,
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 3 {
					return fmt.Errorf("webhook redirect limit exceeded")
				}
				origin := strings.ToLower(req.URL.Scheme) + "://" + strings.ToLower(req.URL.Host)
				if allowedOrigin != "" && origin != allowedOrigin {
					return fmt.Errorf("webhook redirect changed origin")
				}
				return nil
			},
		},
		dispatched:       make(map[string]int64),
	}
}

func safeDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if ip, parseErr := netip.ParseAddr(host); parseErr == nil {
		if security.IsBlockedAddress(ip) {
			return nil, fmt.Errorf("notification target address is blocked")
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
	return (n.telegramBotToken != "" && n.telegramChatID != "") || n.webhookURL != ""
}

func (n *Notifier) Dispatch(ctx context.Context, alert db.AlertEvent, node db.Node) error {
	if !n.Enabled() {
		return nil
	}
	n.mu.Lock()
	lastCount, exists := n.dispatched[alert.ID]
	n.mu.Unlock()
	if exists && lastCount >= int64(alert.OccurrenceCount) {
		return nil
	}

	var errs []string
	if n.telegramBotToken != "" && n.telegramChatID != "" {
		if err := n.sendTelegram(ctx, alert, node); err != nil {
			errs = append(errs, fmt.Sprintf("telegram: %v", err))
		}
	}
	if n.webhookURL != "" {
		if err := n.sendWebhook(ctx, alert, node); err != nil {
			errs = append(errs, fmt.Sprintf("webhook: %v", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("dispatch errors: %s", strings.Join(errs, "; "))
	}
	n.mu.Lock()
	n.dispatched[alert.ID] = int64(alert.OccurrenceCount)
	if len(n.dispatched) > 5000 {
		trimmed := make(map[string]int64)
		trimmed[alert.ID] = int64(alert.OccurrenceCount)
		n.dispatched = trimmed
	}
	n.mu.Unlock()
	return nil
}

func (n *Notifier) sendTelegram(ctx context.Context, alert db.AlertEvent, node db.Node) error {
	statusEmoji := "⚠️"
	if alert.Severity == "critical" {
		statusEmoji = "🚨"
	} else if alert.Status == "resolved" {
		statusEmoji = "✅"
	}

	nodeName := node.Name
	if nodeName == "" {
		nodeName = alert.NodeID
	}

	message := fmt.Sprintf(
		"<b>%s ProbeWatch 告警事件</b>\n\n"+
			"<b>节点:</b> <code>%s</code>\n"+
			"<b>状态:</b> %s | <b>等级:</b> %s\n"+
			"<b>类型:</b> %s\n"+
			"<b>目标:</b> %s\n"+
			"<b>原因:</b> %s\n"+
			"<b>发生次数:</b> %d\n"+
			"<b>时间:</b> %s",
		statusEmoji,
		html.EscapeString(nodeName),
		html.EscapeString(alert.Status),
		html.EscapeString(alert.Severity),
		html.EscapeString(alert.Category),
		html.EscapeString(alert.TargetID),
		html.EscapeString(alert.Reason),
		alert.OccurrenceCount,
		alert.LastSeenAt.Format("2006-01-02 15:04:05 UTC"),
	)

	payload := map[string]string{
		"chat_id":    n.telegramChatID,
		"text":       message,
		"parse_mode": "HTML",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", n.telegramBotToken)
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
	if resp.ContentLength > maxWebhookResponseBytes {
		return fmt.Errorf("telegram response too large")
	}
	read, readErr := io.Copy(io.Discard, io.LimitReader(resp.Body, maxWebhookResponseBytes+1))
	if readErr != nil {
		return fmt.Errorf("read telegram response: %w", readErr)
	}
	if read > maxWebhookResponseBytes {
		return fmt.Errorf("telegram response too large")
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram API returned status %d", resp.StatusCode)
	}
	return nil
}

func (n *Notifier) sendWebhook(ctx context.Context, alert db.AlertEvent, node db.Node) error {
	payload := map[string]any{
		"event":            "alert",
		"id":               alert.ID,
		"node_id":          alert.NodeID,
		"node_name":        node.Name,
		"category":         alert.Category,
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.ContentLength > maxWebhookResponseBytes {
		return fmt.Errorf("webhook response too large")
	}
	read, err := io.Copy(io.Discard, io.LimitReader(resp.Body, maxWebhookResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read webhook response: %w", err)
	}
	if read > maxWebhookResponseBytes {
		return fmt.Errorf("webhook response too large")
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}

func RunAlertDispatcher(ctx context.Context, store *db.Store, notifier *Notifier) {
	if !notifier.Enabled() {
		return
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	scanAndDispatch := func() {
		scanCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		alerts, err := store.ListAlerts(scanCtx, db.AlertQuery{
			Statuses: []string{"open"},
			Limit:    100,
		})
		if err != nil {
			slog.Error("scan alert events failed", "error_class", fmt.Sprintf("%T", err))
			return
		}

		for _, alert := range alerts {
			node, err := store.GetNodeByUUID(scanCtx, alert.NodeID)
			if err != nil {
				node = db.Node{ID: alert.NodeID, Name: alert.NodeID}
			}
			if err := notifier.Dispatch(scanCtx, alert, node); err != nil {
				slog.Error("dispatch alert failed", "alert_id", alert.ID, "error_class", fmt.Sprintf("%T", err))
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
