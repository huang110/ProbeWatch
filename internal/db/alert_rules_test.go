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

func TestCompositeAlertRulesAndConsecutiveBreaches(t *testing.T) {
	ctx := context.Background()
	store := newTestAlertsStore(t)
	defer store.Close()
	ResetConsecutiveTracker()

	nodeID := "test-node-comp-1"
	if _, err := store.db.ExecContext(ctx, `INSERT INTO nodes (id, uuid, name, status, created_at, updated_at) VALUES (?, '550e8400-e29b-41d4-a716-446655440088', 'Composite Eval Node', 'online', 1, 1)`, nodeID); err != nil {
		t.Fatalf("insert test node: %v", err)
	}

	// Clear default rules
	if _, err := store.db.ExecContext(ctx, "DELETE FROM alert_rules"); err != nil {
		t.Fatal(err)
	}

	// 1. Create a composite rule: CPU > 80 AND memory > 80, consecutive_count = 2
	compRule := AlertRule{
		ID:             "rule-comp-cpu-mem",
		Name:           "High CPU and Memory (Consecutive 2)",
		ExpressionType: "composite",
		Logic:          "AND",
		Conditions: []AlertCondition{
			{Metric: "cpu", Operator: ">", Threshold: 80.0},
			{Metric: "memory", Operator: ">", Threshold: 80.0},
		},
		ConsecutiveCount: 2,
		Severity:         "critical",
		NodeFilter:       "*",
		Enabled:          true,
	}
	if err := store.CreateAlertRule(ctx, compRule); err != nil {
		t.Fatalf("CreateAlertRule: %v", err)
	}

	now := time.Now().UTC()

	// Evaluation 1: CPU = 85 (breached), Memory = 50 (not breached) -> AND condition fails, 0 open alerts
	tx1, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	metrics1 := map[string]float64{"cpu": 85.0, "memory": 50.0}
	if err := store.EvaluateResourceMetricsWithRules(ctx, tx1, nodeID, metrics1, now); err != nil {
		t.Fatal(err)
	}
	_ = tx1.Commit()

	openAlerts, _ := store.ListAlerts(ctx, AlertQuery{Statuses: []string{AlertStatusOpen}})
	if len(openAlerts) != 0 {
		t.Fatalf("expected 0 open alerts when memory < 80, got %d", len(openAlerts))
	}

	// Evaluation 2: CPU = 85, Memory = 85 -> Breach #1, but consecutive_count = 2, so should NOT fire yet!
	now = now.Add(time.Minute)
	tx2, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	metrics2 := map[string]float64{"cpu": 85.0, "memory": 85.0}
	if err := store.EvaluateResourceMetricsWithRules(ctx, tx2, nodeID, metrics2, now); err != nil {
		t.Fatal(err)
	}
	_ = tx2.Commit()

	openAlerts, _ = store.ListAlerts(ctx, AlertQuery{Statuses: []string{AlertStatusOpen}})
	if len(openAlerts) != 0 {
		t.Fatalf("expected 0 open alerts on 1st consecutive breach (requires 2), got %d", len(openAlerts))
	}

	// Evaluation 3: CPU = 85, Memory = 85 -> Breach #2 -> Reached consecutive_count = 2 -> FIRES!
	now = now.Add(time.Minute)
	tx3, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	metrics3 := map[string]float64{"cpu": 85.0, "memory": 85.0}
	if err := store.EvaluateResourceMetricsWithRules(ctx, tx3, nodeID, metrics3, now); err != nil {
		t.Fatal(err)
	}
	_ = tx3.Commit()

	openAlerts, _ = store.ListAlerts(ctx, AlertQuery{Statuses: []string{AlertStatusOpen}})
	if len(openAlerts) != 1 {
		t.Fatalf("expected 1 open alert on 2nd consecutive breach, got %d", len(openAlerts))
	}
	if openAlerts[0].Severity != "critical" {
		t.Fatalf("expected critical severity, got %s", openAlerts[0].Severity)
	}

	// Evaluation 4: CPU drops to 40 -> Recovered -> Alert resolves!
	now = now.Add(time.Minute)
	tx4, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	metrics4 := map[string]float64{"cpu": 40.0, "memory": 85.0}
	if err := store.EvaluateResourceMetricsWithRules(ctx, tx4, nodeID, metrics4, now); err != nil {
		t.Fatal(err)
	}
	_ = tx4.Commit()

	openAlerts, _ = store.ListAlerts(ctx, AlertQuery{Statuses: []string{AlertStatusOpen}})
	if len(openAlerts) != 0 {
		t.Fatalf("expected alert to resolve after recovery, got %d", len(openAlerts))
	}
}

func TestEvaluateHardwareAlertRules(t *testing.T) {
	ctx := context.Background()
	store := newTestAlertsStore(t)
	defer store.Close()

	nodeID := "node-hw-1"
	if _, err := store.db.ExecContext(ctx, `INSERT INTO nodes (id, uuid, name, status, created_at, updated_at) VALUES (?, '550e8400-e29b-41d4-a716-446655440099', 'Hardware Host', 'online', 1, 1)`, nodeID); err != nil {
		t.Fatalf("insert test node: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, "DELETE FROM alert_rules"); err != nil {
		t.Fatal(err)
	}

	// Create rule for CPU temperature > 80.0
	tempRule := AlertRule{
		ID:               "rule-temp-overheat",
		Name:             "CPU Overheat Warning",
		Metric:           "cpu_temp",
		Operator:         ">",
		Threshold:        80.0,
		Severity:         "critical",
		NodeFilter:       "*",
		Enabled:          true,
		ConsecutiveCount: 1,
	}
	if err := store.CreateAlertRule(ctx, tempRule); err != nil {
		t.Fatalf("create temp rule: %v", err)
	}

	// Create rule for Disk IO wait > 40ms
	ioRule := AlertRule{
		ID:               "rule-disk-latency",
		Name:             "High Disk IO Latency",
		Metric:           "disk_io_wait",
		Operator:         ">",
		Threshold:        40.0,
		Severity:         "warning",
		NodeFilter:       "*",
		Enabled:          true,
		ConsecutiveCount: 1,
	}
	if err := store.CreateAlertRule(ctx, ioRule); err != nil {
		t.Fatalf("create io rule: %v", err)
	}

	now := time.Now().UTC()

	// 1. Report normal hardware metrics
	normalPayload := []byte(`{
		"cpu_percent": 25.0,
		"cpu_temp_c": 52.5,
		"disks": [{"device": "vda", "io_wait_ms": 5.2, "util_percent": 12.0}],
		"mounts": [{"mount_point": "/", "inodes_percent": 20.0}]
	}`)
	tx1, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.evaluateResourceAlertTx(ctx, tx1, nodeID, normalPayload, now); err != nil {
		t.Fatalf("eval normal: %v", err)
	}
	_ = tx1.Commit()

	openAlerts, _ := store.ListAlerts(ctx, AlertQuery{Statuses: []string{AlertStatusOpen}})
	if len(openAlerts) != 0 {
		t.Fatalf("expected 0 open alerts on normal hardware, got %d", len(openAlerts))
	}

	// 2. Report overheating and high disk IO wait
	now = now.Add(time.Minute)
	breachPayload := []byte(`{
		"cpu_percent": 88.0,
		"cpu_temp_c": 86.4,
		"disks": [{"device": "vda", "io_wait_ms": 68.5, "util_percent": 95.0}],
		"mounts": [{"mount_point": "/", "inodes_percent": 30.0}]
	}`)
	tx2, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.evaluateResourceAlertTx(ctx, tx2, nodeID, breachPayload, now); err != nil {
		t.Fatalf("eval breach: %v", err)
	}
	_ = tx2.Commit()

	openAlerts, _ = store.ListAlerts(ctx, AlertQuery{Statuses: []string{AlertStatusOpen}})
	if len(openAlerts) != 2 {
		t.Fatalf("expected 2 open alerts (cpu_temp + disk_io_wait), got %d", len(openAlerts))
	}
}


