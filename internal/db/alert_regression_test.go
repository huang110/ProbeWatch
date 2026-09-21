package db

import (
	"context"
	"testing"
	"time"
)

func TestAlertAckedRecurrenceReopensAndOldTimestampIsIgnored(t *testing.T) {
	store := openTestStore(t)
	node := registerTestNode(t, store, "alert-reopen")
	ctx := context.Background()
	first := time.Unix(100, 0).UTC()
	eval := AlertEvaluation{Category: "network", TargetID: "target-1", Reason: "timeout", Severity: AlertSeverityWarn, Failing: true}
	if err := store.EvaluateAlert(ctx, node.Node.ID, eval, first); err != nil {
		t.Fatal(err)
	}
	alerts, err := store.ListAlerts(ctx, AlertQuery{Statuses: []string{AlertStatusOpen}})
	if err != nil || len(alerts) != 1 {
		t.Fatalf("initial alerts = %d, err=%v", len(alerts), err)
	}
	if _, err := store.AckAlert(ctx, alerts[0].ID, "admin", first.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := store.EvaluateAlert(ctx, node.Node.ID, eval, first.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	alerts, err = store.ListAlerts(ctx, AlertQuery{Statuses: []string{AlertStatusAcked}})
	if err != nil || len(alerts) != 1 {
		t.Fatalf("old recurrence changed alert state: count=%d err=%v", len(alerts), err)
	}
	if err := store.EvaluateAlert(ctx, node.Node.ID, eval, first.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	alerts, err = store.ListAlerts(ctx, AlertQuery{Statuses: []string{AlertStatusOpen}})
	if err != nil || len(alerts) != 1 {
		t.Fatalf("new recurrence did not reopen alert: count=%d err=%v", len(alerts), err)
	}
	if alerts[0].OccurrenceCount != 2 {
		t.Fatalf("occurrence_count = %d, want 2", alerts[0].OccurrenceCount)
	}
}
