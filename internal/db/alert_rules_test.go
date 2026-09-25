package db

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func newTestAlertsStore(t *testing.T) *Store {
	dir := t.TempDir()
	store, err := OpenStore(dir+"/test.db", []byte("test-pepper-12345678901234567890"))
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestAlertRulesCRUD(t *testing.T) {
	ctx := context.Background()
	store := newTestAlertsStore(t)
	defer store.Close()

	// 1. Initial list should contain seeded defaults
	rules, err := store.ListAlertRules(ctx)
	if err != nil {
		t.Fatalf("ListAlertRules: %v", err)
	}
	if len(rules) < 3 {
		t.Fatalf("expected at least 3 seeded rules, got %d", len(rules))
	}

	// 2. Create custom rule
	customRule := AlertRule{
		ID:              "rule-custom-cpu-70",
		Name:            "自定义 CPU 告警 70%",
		Metric:          "cpu",
		Operator:        ">",
		Threshold:       70.0,
		DurationSeconds: 60,
		Severity:        "warning",
		NodeFilter:      "*",
		Enabled:         true,
	}
	if err := store.CreateAlertRule(ctx, customRule); err != nil {
		t.Fatalf("CreateAlertRule: %v", err)
	}

	// 3. Get rule
	got, err := store.GetAlertRule(ctx, "rule-custom-cpu-70")
	if err != nil {
		t.Fatalf("GetAlertRule: %v", err)
	}
	if got.Threshold != 70.0 || got.Metric != "cpu" || !got.Enabled {
		t.Fatalf("unexpected rule details: %+v", got)
	}

	// 4. Update rule
	got.Threshold = 75.0
	got.Severity = "critical"
	if err := store.UpdateAlertRule(ctx, got); err != nil {
		t.Fatalf("UpdateAlertRule: %v", err)
	}
	updated, err := store.GetAlertRule(ctx, "rule-custom-cpu-70")
	if err != nil {
		t.Fatalf("GetAlertRule after update: %v", err)
	}
	if updated.Threshold != 75.0 || updated.Severity != "critical" {
		t.Fatalf("unexpected updated rule: %+v", updated)
	}

	// 5. Toggle rule
	newState, err := store.ToggleAlertRule(ctx, "rule-custom-cpu-70")
	if err != nil {
		t.Fatalf("ToggleAlertRule: %v", err)
	}
	if newState != false {
		t.Fatalf("expected toggled state false, got %v", newState)
	}

	// 6. Delete rule
	if err := store.DeleteAlertRule(ctx, "rule-custom-cpu-70"); err != nil {
		t.Fatalf("DeleteAlertRule: %v", err)
	}
	_, err = store.GetAlertRule(ctx, "rule-custom-cpu-70")
	if err != ErrAlertRuleNotFound {
		t.Fatalf("expected ErrAlertRuleNotFound, got %v", err)
	}
}

func TestEvaluateResourceMetricsWithCustomRules(t *testing.T) {
	ctx := context.Background()
	store := newTestAlertsStore(t)
	defer store.Close()

	// Insert test node
	nodeID := "test-node-rules-1"
	if _, err := store.db.ExecContext(ctx, `INSERT INTO nodes (id, uuid, name, status, created_at, updated_at) VALUES (?, '550e8400-e29b-41d4-a716-446655440077', 'Rule Evaluation Node', 'online', 1, 1)`, nodeID); err != nil {
		t.Fatalf("insert test node: %v", err)
	}

	// Clear default rules to have deterministic test
	if _, err := store.db.ExecContext(ctx, "DELETE FROM alert_rules"); err != nil {
		t.Fatal(err)
	}

	// Add rule: CPU > 75.0% -> warning
	err := store.CreateAlertRule(ctx, AlertRule{
		ID:         "rule-cpu-75",
		Name:       "CPU 75%",
		Metric:     "cpu",
		Operator:   ">",
		Threshold:  75.0,
		Severity:   "warning",
		NodeFilter: "*",
		Enabled:    true,
	})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()

	// 1. Report with CPU = 60.0% -> No alert
	report1Payload, _ := json.Marshal(map[string]any{
		"cpu_percent":            60.0,
		"memory_total_bytes":     1000,
		"memory_used_bytes":      400,
		"filesystem_total_bytes": 1000,
		"filesystem_used_bytes":  400,
	})
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.evaluateResourceAlertTx(ctx, tx, nodeID, report1Payload, now); err != nil {
		t.Fatal(err)
	}
	_ = tx.Commit()

	openAlerts, err := store.ListAlerts(ctx, AlertQuery{Statuses: []string{AlertStatusOpen}})
	if err != nil {
		t.Fatal(err)
	}
	if len(openAlerts) != 0 {
		t.Fatalf("expected 0 open alerts at 60%% CPU, got %d", len(openAlerts))
	}

	// 2. Report with CPU = 80.0% -> Triggers alert
	now = now.Add(time.Minute)
	report2Payload, _ := json.Marshal(map[string]any{
		"cpu_percent":            80.0,
		"memory_total_bytes":     1000,
		"memory_used_bytes":      400,
		"filesystem_total_bytes": 1000,
		"filesystem_used_bytes":  400,
	})
	tx, err = store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.evaluateResourceAlertTx(ctx, tx, nodeID, report2Payload, now); err != nil {
		t.Fatal(err)
	}
	_ = tx.Commit()

	openAlerts, err = store.ListAlerts(ctx, AlertQuery{Statuses: []string{AlertStatusOpen}})
	if err != nil {
		t.Fatal(err)
	}
	if len(openAlerts) != 1 {
		t.Fatalf("expected 1 open alert at 80%% CPU, got %d", len(openAlerts))
	}
	if openAlerts[0].Severity != "warning" || openAlerts[0].Category != "resource" {
		t.Fatalf("unexpected alert: %+v", openAlerts[0])
	}

	// 3. Report with CPU = 50.0% -> Alert automatically resolves
	now = now.Add(time.Minute)
	report3Payload, _ := json.Marshal(map[string]any{
		"cpu_percent":            50.0,
		"memory_total_bytes":     1000,
		"memory_used_bytes":      400,
		"filesystem_total_bytes": 1000,
		"filesystem_used_bytes":  400,
	})
	tx, err = store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.evaluateResourceAlertTx(ctx, tx, nodeID, report3Payload, now); err != nil {
		t.Fatal(err)
	}
	_ = tx.Commit()

	openAlerts, err = store.ListAlerts(ctx, AlertQuery{Statuses: []string{AlertStatusOpen}})
	if err != nil {
		t.Fatal(err)
	}
	if len(openAlerts) != 0 {
		t.Fatalf("expected alert to resolve at 50%% CPU, still got %d open alerts", len(openAlerts))
	}

	resolvedAlerts, err := store.ListAlerts(ctx, AlertQuery{Statuses: []string{AlertStatusResolved}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolvedAlerts) != 1 {
		t.Fatalf("expected 1 resolved alert, got %d", len(resolvedAlerts))
	}
}
