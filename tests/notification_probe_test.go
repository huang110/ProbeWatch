package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/config"
	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/notify"
)

func maskWebhookTarget(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return "webhook://***"
	}
	return fmt.Sprintf("%s://%s/***", u.Scheme, u.Host)
}

func TestDedicatedNotificationProbe(t *testing.T) {
	webhookURL := strings.TrimSpace(os.Getenv("PROBEWATCH_TEST_WEBHOOK_URL"))
	if webhookURL == "" {
		fmt.Println("SKIP: 未配置专用通知测试通道")
		t.Skip("SKIP: 未配置专用通知测试通道")
		return
	}

	channelName := strings.TrimSpace(os.Getenv("PROBEWATCH_TEST_NOTIFICATION_CHANNEL"))
	if channelName == "" {
		channelName = "ci-test-webhook"
	}

	runID := strings.TrimSpace(os.Getenv("PROBEWATCH_TEST_RUN_ID"))
	if runID == "" {
		runID = fmt.Sprintf("run-%d", time.Now().UnixNano())
	}

	maskedTarget := maskWebhookTarget(webhookURL)
	fmt.Printf("[*] Executing dedicated notification probe (channel: %s, target: %s, run_id: %s)\n", channelName, maskedTarget, runID)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// 0. Ensure production database is strictly isolated if on server
	prodDBPath := "/opt/probewatch/data/probewatch.db"
	prodHashBefore, prodSizeBefore, prodExists := hashFileIfExists(prodDBPath)

	// 1. Set up an isolated test store
	tempDir := t.TempDir()
	isolatedDBPath := filepath.Join(tempDir, "isolated_notif.db")
	pepper := []byte("isolated-notif-pepper-32-bytes!!")
	store, err := db.OpenStore(isolatedDBPath, pepper)
	if err != nil {
		t.Fatalf("OpenStore failed: %v", err)
	}
	defer store.Close()

	// 2. Set up dedicated test channel, test rule, and test silence
	chID := fmt.Sprintf("ch-probe-%s", runID)
	cfgJSON, err := json.Marshal(map[string]string{
		"url": webhookURL,
	})
	if err != nil {
		t.Fatalf("marshal webhook config: %v", err)
	}

	ch := db.NotificationChannel{
		ID:      chID,
		Name:    channelName,
		Type:    "webhook",
		Config:  string(cfgJSON),
		Enabled: true,
	}

	if err := store.CreateNotificationChannel(ctx, ch); err != nil {
		t.Fatalf("CreateNotificationChannel failed: %v", err)
	}

	ruleID := fmt.Sprintf("rule-probe-%s", runID)
	rule := db.AlertRule{
		ID:              ruleID,
		Name:            fmt.Sprintf("CI-Probe-Rule-%s", runID),
		Category:        "node",
		Condition:       "cpu_high",
		Threshold:       99.9,
		DurationSeconds: 300,
		Severity:        "warning",
		Enabled:         false, // Disabled to prevent any unsolicited alert generation
	}
	if err := store.CreateAlertRule(ctx, rule); err != nil {
		t.Fatalf("CreateAlertRule failed: %v", err)
	}

	silenceID := fmt.Sprintf("sil-probe-%s", runID)
	silence := db.AlertSilence{
		ID:        silenceID,
		Category:  "node",
		StartsAt:  time.Now().UTC(),
		EndsAt:    time.Now().UTC().Add(10 * time.Minute),
		Comment:   fmt.Sprintf("probe silence %s", runID),
		CreatedBy: "ci-probe",
	}
	if err := store.CreateAlertSilence(ctx, silence); err != nil {
		t.Fatalf("CreateAlertSilence failed: %v", err)
	}

	// 3. Ensure strict cleanup in defer block with residual assertion
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()

		_ = store.DeleteNotificationChannel(cleanupCtx, chID)
		_ = store.DeleteAlertRule(cleanupCtx, ruleID)
		_ = store.DeleteAlertSilence(cleanupCtx, silenceID)

		// Assert zero residuals in store
		channels, _ := store.ListNotificationChannels(cleanupCtx)
		for _, c := range channels {
			if c.ID == chID {
				t.Fatalf("CRITICAL: Test notification channel %s still exists after cleanup", chID)
			}
		}

		rules, _ := store.ListAlertRules(cleanupCtx)
		for _, r := range rules {
			if r.ID == ruleID {
				t.Fatalf("CRITICAL: Test alert rule %s still exists after cleanup", ruleID)
			}
		}

		silences, _ := store.ListAlertSilences(cleanupCtx)
		for _, s := range silences {
			if s.ID == silenceID {
				t.Fatalf("CRITICAL: Test alert silence %s still exists after cleanup", silenceID)
			}
		}

		if prodExists {
			prodHashAfter, prodSizeAfter, _ := hashFileIfExists(prodDBPath)
			if prodHashAfter != prodHashBefore || prodSizeAfter != prodSizeBefore {
				t.Fatalf("CRITICAL: Production database was modified during notification test! Before: %s (%d), After: %s (%d)",
					prodHashBefore, prodSizeBefore, prodHashAfter, prodSizeAfter)
			}
		}
	}()

	// 4. Dispatch the test notification with strict required format:
	// Message must contain: ProbeWatch CI notification test <run-id>
	testMessage := fmt.Sprintf("ProbeWatch CI notification test %s", runID)
	cfg := config.Config{}
	notifier := notify.NewNotifierWithStore(cfg, store)

	if err := notifier.SendTestNotificationWithMessage(ctx, ch.Type, ch.Config, testMessage); err != nil {
		t.Fatalf("Notification dispatch to %s failed: %v", maskedTarget, err)
	}

	fmt.Printf("[+] Notification successfully delivered to %s (message contained %q)\n", maskedTarget, testMessage)
	fmt.Println("[PASS] dedicated_notification_probe")
}
