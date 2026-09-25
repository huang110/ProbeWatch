package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/config"
	"github.com/probewatch/probewatch/internal/db"
)

func TestFormatEmoji(t *testing.T) {
	if formatEmoji(db.AlertEvent{Status: "resolved"}) != "✅" {
		t.Fatal("expected resolved emoji to be ✅")
	}
	if formatEmoji(db.AlertEvent{Status: "open", Severity: "critical"}) != "🚨" {
		t.Fatal("expected critical emoji to be 🚨")
	}
	if formatEmoji(db.AlertEvent{Status: "open", Severity: "warning"}) != "⚠️" {
		t.Fatal("expected warning emoji to be ⚠️")
	}
}

func TestFormatCategoryTitle(t *testing.T) {
	if formatCategoryTitle("node", "node_offline") != "探针离线" {
		t.Fatal("expected node_offline to be 探针离线")
	}
	if formatCategoryTitle("resource", "cpu_high") != "CPU负载过高" {
		t.Fatal("expected cpu_high to be CPU负载过高")
	}
	if formatCategoryTitle("network", "timeout") != "网络连接超时" {
		t.Fatal("expected timeout to be 网络连接超时")
	}
	if formatCategoryTitle("mtr", "path_changed") != "MTR路由拓扑漂移" {
		t.Fatal("expected path_changed to be MTR路由拓扑漂移")
	}
}

func TestIsSubscribed(t *testing.T) {
	n := NewNotifier(config.Config{})

	// Empty events = subscribed to everything
	chEmpty := db.NotificationChannel{Events: "[]"}
	if !n.isSubscribed(chEmpty, "node") || !n.isSubscribed(chEmpty, "resource") {
		t.Fatal("expected empty events to subscribe to everything")
	}

	// Specific subscriptions
	chSpecific := db.NotificationChannel{Events: `["node","resource"]`}
	if !n.isSubscribed(chSpecific, "node") {
		t.Fatal("expected node to be subscribed")
	}
	if !n.isSubscribed(chSpecific, "resource") {
		t.Fatal("expected resource to be subscribed")
	}
	if n.isSubscribed(chSpecific, "network") {
		t.Fatal("expected network to NOT be subscribed")
	}
}

func TestDispatchToChannels(t *testing.T) {
	var receivedBody []byte
	var receivedPath string
	var receivedHeader http.Header

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedHeader = r.Header
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok": true}`))
	}))
	defer srv.Close()

	n := NewNotifier(config.Config{})
	// Use standard client for loopback httptest server
	n.client = srv.Client()

	now := time.Now().UTC()
	alert := db.AlertEvent{
		ID:              "alert-1",
		NodeID:          "test-node",
		Category:        "node",
		TargetID:        "heartbeat",
		Reason:          "node_offline",
		Severity:        "critical",
		Status:          "open",
		OccurrenceCount: 1,
		FirstSeenAt:     now,
		LastSeenAt:      now,
	}
	node := db.Node{
		ID:   "test-node",
		Name: "HongKong-01",
	}

	// 1. Test Discord
	chDiscord := db.NotificationChannel{
		Type:   "discord",
		Config: `{"webhook_url":"` + srv.URL + `/webhook"}`,
	}
	if err := n.dispatchToChannel(context.Background(), chDiscord, alert, node); err != nil {
		t.Fatalf("discord dispatch failed: %v", err)
	}
	if !strings.Contains(string(receivedBody), "HongKong-01") || !strings.Contains(string(receivedBody), "embeds") {
		t.Fatalf("discord payload missing fields: %s", string(receivedBody))
	}

	// 2. Test WeCom
	chWeCom := db.NotificationChannel{
		Type:   "wecom",
		Config: `{"webhook_url":"` + srv.URL + `/wecom"}`,
	}
	if err := n.dispatchToChannel(context.Background(), chWeCom, alert, node); err != nil {
		t.Fatalf("wecom dispatch failed: %v", err)
	}
	if !strings.Contains(string(receivedBody), "markdown") || !strings.Contains(string(receivedBody), "HongKong-01") {
		t.Fatalf("wecom payload missing fields: %s", string(receivedBody))
	}

	// 3. Test Bark
	chBark := db.NotificationChannel{
		Type:   "bark",
		Config: `{"server_url":"` + srv.URL + `","device_key":"testkey123"}`,
	}
	if err := n.dispatchToChannel(context.Background(), chBark, alert, node); err != nil {
		t.Fatalf("bark dispatch failed: %v", err)
	}
	if !strings.Contains(string(receivedBody), "testkey123") || !strings.Contains(string(receivedBody), "HongKong-01") {
		t.Fatalf("bark payload missing fields: %s", string(receivedBody))
	}

	// 4. Test Generic Webhook
	chWebhook := db.NotificationChannel{
		Type:   "webhook",
		Config: `{"webhook_url":"` + srv.URL + `/custom"}`,
	}
	if err := n.dispatchToChannel(context.Background(), chWebhook, alert, node); err != nil {
		t.Fatalf("webhook dispatch failed: %v", err)
	}
	var webhookPayload map[string]any
	if err := json.Unmarshal(receivedBody, &webhookPayload); err != nil {
		t.Fatalf("webhook payload not json: %v", err)
	}
	if webhookPayload["node_name"] != "HongKong-01" || webhookPayload["reason"] != "node_offline" {
		t.Fatalf("webhook payload unexpected content: %s", string(receivedBody))
	}
	_ = receivedPath
	_ = receivedHeader
}
