package db

import (
	"bytes"
	"context"
	"encoding/json"
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

func mtrPathAlertTestSetup(t *testing.T) (*Store, RegisteredNode, string) {
	t.Helper()
	store := openTestStore(t)
	node := registerTestNode(t, store, "mtr-path-node")
	const targetID = "mtr-route"
	if _, err := store.CreateTarget(context.Background(), TargetDefinition{ID: targetID, Name: "Route", Kind: TargetKindMTR, Host: "example.com", Enabled: true, Payload: []byte(`{"max_hops":20}`)}, time.Now()); err != nil {
		t.Fatal(err)
	}
	return store, node, targetID
}

func mtrRoutePayload(t *testing.T, destination string, reached bool, hopIPs ...string) []byte {
	t.Helper()
	hops := make([]map[string]any, 0, len(hopIPs))
	for index, ip := range hopIPs {
		hop := map[string]any{"ttl": index + 1}
		if ip != "" {
			hop["ip"] = ip
		}
		hops = append(hops, hop)
	}
	payload, err := json.Marshal(map[string]any{"destination_ip": destination, "hops": hops, "reached": reached})
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func persistMTRPathResult(t *testing.T, store *Store, nodeID, targetID, requestID string, checkedAt, now time.Time, payload []byte) {
	t.Helper()
	input := AgentResultInput{Kind: TargetKindMTR, TargetID: targetID, CheckedAt: checkedAt, Payload: payload}
	if err := store.PersistAgentResult(context.Background(), nodeID, requestID, now.Add(time.Hour), now, input); err != nil {
		t.Fatal(err)
	}
}

func findMTRPathChangedAlert(t *testing.T, store *Store, nodeID, targetID string) (AlertEvent, bool) {
	t.Helper()
	alerts, err := store.ListAlerts(context.Background(), AlertQuery{})
	if err != nil {
		t.Fatal(err)
	}
	for _, alert := range alerts {
		if alert.NodeID == nodeID && alert.TargetID == targetID && alert.Category == "mtr" && alert.Reason == alertReasonPathChanged {
			return alert, true
		}
	}
	return AlertEvent{}, false
}

func TestMTRPathChangeAlertTriggersOncePerChangeAndIsNotAutoResolved(t *testing.T) {
	store, node, targetID := mtrPathAlertTestSetup(t)
	base := time.Unix(1_000_000, 0).UTC()
	routeA := mtrRoutePayload(t, "93.184.216.34", true, "10.0.0.1", "93.184.216.34")
	routeB := mtrRoutePayload(t, "93.184.216.34", true, "10.0.0.1", "10.0.0.2", "93.184.216.34")

	// First report establishes the baseline without alerting.
	persistMTRPathResult(t, store, node.Node.ID, targetID, "mtr-req-1", base, base, routeA)
	if _, found := findMTRPathChangedAlert(t, store, node.Node.ID, targetID); found {
		t.Fatal("first report must not raise a path change alert")
	}

	// Route change raises exactly one open warning event.
	persistMTRPathResult(t, store, node.Node.ID, targetID, "mtr-req-2", base.Add(time.Minute), base.Add(time.Minute), routeB)
	alert, found := findMTRPathChangedAlert(t, store, node.Node.ID, targetID)
	if !found {
		t.Fatal("route change did not raise a path change alert")
	}
	if alert.Status != AlertStatusOpen || alert.Severity != AlertSeverityWarn || alert.OccurrenceCount != 1 {
		t.Fatalf("path change alert = status %q severity %q occurrence %d", alert.Status, alert.Severity, alert.OccurrenceCount)
	}

	// The same route again must not raise or increment.
	persistMTRPathResult(t, store, node.Node.ID, targetID, "mtr-req-3", base.Add(2*time.Minute), base.Add(2*time.Minute), routeB)
	alert, _ = findMTRPathChangedAlert(t, store, node.Node.ID, targetID)
	if alert.OccurrenceCount != 1 {
		t.Fatalf("same route re-report occurrence_count = %d, want 1", alert.OccurrenceCount)
	}

	// A path change alert is an event, not a state: later successful probes
	// must leave it open.
	persistMTRPathResult(t, store, node.Node.ID, targetID, "mtr-req-4", base.Add(3*time.Minute), base.Add(3*time.Minute), routeB)
	alert, _ = findMTRPathChangedAlert(t, store, node.Node.ID, targetID)
	if alert.Status != AlertStatusOpen {
		t.Fatalf("successful probe changed path change alert status to %q", alert.Status)
	}
}

func TestMTRPathChangeAlertIncrementsOccurrenceOnEachChange(t *testing.T) {
	store, node, targetID := mtrPathAlertTestSetup(t)
	base := time.Unix(2_000_000, 0).UTC()
	routeA := mtrRoutePayload(t, "93.184.216.34", true, "10.0.0.1", "93.184.216.34")
	routeB := mtrRoutePayload(t, "93.184.216.34", true, "10.0.0.1", "10.0.0.2", "93.184.216.34")
	routeC := mtrRoutePayload(t, "93.184.216.34", true, "10.0.0.9", "93.184.216.34")

	persistMTRPathResult(t, store, node.Node.ID, targetID, "mtr-req-1", base, base, routeA)
	persistMTRPathResult(t, store, node.Node.ID, targetID, "mtr-req-2", base.Add(time.Minute), base.Add(time.Minute), routeB)
	alert, found := findMTRPathChangedAlert(t, store, node.Node.ID, targetID)
	if !found || alert.OccurrenceCount != 1 {
		t.Fatalf("first change occurrence = %d found=%v, want 1 true", alert.OccurrenceCount, found)
	}
	firstSeen := alert.FirstSeenAt

	persistMTRPathResult(t, store, node.Node.ID, targetID, "mtr-req-3", base.Add(2*time.Minute), base.Add(2*time.Minute), routeC)
	alert, _ = findMTRPathChangedAlert(t, store, node.Node.ID, targetID)
	if alert.OccurrenceCount != 2 {
		t.Fatalf("second change occurrence_count = %d, want 2", alert.OccurrenceCount)
	}
	if !alert.FirstSeenAt.Equal(firstSeen) {
		t.Fatalf("first_seen_at moved from %v to %v", firstSeen, alert.FirstSeenAt)
	}
	if !alert.LastSeenAt.After(firstSeen) {
		t.Fatalf("last_seen_at = %v, want after %v", alert.LastSeenAt, firstSeen)
	}
}

func TestMTRPathChangeAlertSkipsUnreachedResults(t *testing.T) {
	store, node, targetID := mtrPathAlertTestSetup(t)
	base := time.Unix(3_000_000, 0).UTC()
	routeA := mtrRoutePayload(t, "93.184.216.34", true, "10.0.0.1", "93.184.216.34")
	unreachedA := mtrRoutePayload(t, "93.184.216.34", false, "10.0.0.1")
	unreachedB := mtrRoutePayload(t, "93.184.216.34", false, "10.0.0.9")

	persistMTRPathResult(t, store, node.Node.ID, targetID, "mtr-req-1", base, base, routeA)
	persistMTRPathResult(t, store, node.Node.ID, targetID, "mtr-req-2", base.Add(time.Minute), base.Add(time.Minute), unreachedA)
	persistMTRPathResult(t, store, node.Node.ID, targetID, "mtr-req-3", base.Add(2*time.Minute), base.Add(2*time.Minute), unreachedB)
	persistMTRPathResult(t, store, node.Node.ID, targetID, "mtr-req-4", base.Add(3*time.Minute), base.Add(3*time.Minute), routeA)
	if _, found := findMTRPathChangedAlert(t, store, node.Node.ID, targetID); found {
		t.Fatal("unreached results must not raise path change alerts")
	}
}

func TestMTRPathChangeAlertIgnoresStaleTimestamps(t *testing.T) {
	store, node, targetID := mtrPathAlertTestSetup(t)
	base := time.Unix(4_000_000, 0).UTC()
	routeA := mtrRoutePayload(t, "93.184.216.34", true, "10.0.0.1", "93.184.216.34")
	routeB := mtrRoutePayload(t, "93.184.216.34", true, "10.0.0.1", "10.0.0.2", "93.184.216.34")

	persistMTRPathResult(t, store, node.Node.ID, targetID, "mtr-req-1", base, base, routeA)
	// A result older than the stored latest would not replace it, so it must
	// not be treated as a new route observation.
	persistMTRPathResult(t, store, node.Node.ID, targetID, "mtr-req-2", base.Add(-time.Minute), base.Add(time.Minute), routeB)
	if _, found := findMTRPathChangedAlert(t, store, node.Node.ID, targetID); found {
		t.Fatal("stale result raised a path change alert")
	}
	latestCheckedAt, latestPayload, err := store.GetMTRLatest(context.Background(), node.Node.ID, targetID)
	if err != nil {
		t.Fatal(err)
	}
	if !latestCheckedAt.Equal(base) || !bytes.Equal(latestPayload, routeA) {
		t.Fatalf("stale result replaced the stored latest: checked_at=%v payload=%s", latestCheckedAt, latestPayload)
	}

	// The same route reported fresh does trigger, proving the stale report was
	// not counted as the baseline.
	persistMTRPathResult(t, store, node.Node.ID, targetID, "mtr-req-3", base.Add(2*time.Minute), base.Add(2*time.Minute), routeB)
	alert, found := findMTRPathChangedAlert(t, store, node.Node.ID, targetID)
	if !found || alert.OccurrenceCount != 1 {
		t.Fatalf("fresh route change occurrence = %d found=%v, want 1 true", alert.OccurrenceCount, found)
	}
}

func TestMTRPathChangeAlertCoexistsWithReachabilityAlert(t *testing.T) {
	store, node, targetID := mtrPathAlertTestSetup(t)
	base := time.Unix(5_000_000, 0).UTC()
	routeA := mtrRoutePayload(t, "93.184.216.34", true, "10.0.0.1", "93.184.216.34")
	routeB := mtrRoutePayload(t, "93.184.216.34", true, "10.0.0.1", "10.0.0.2", "93.184.216.34")
	routeError, err := json.Marshal(map[string]any{"destination_ip": "93.184.216.34", "hops": []map[string]any{{"ttl": 1, "ip": "10.0.0.1"}}, "reached": false, "error": "timeout: i/o timeout"})
	if err != nil {
		t.Fatal(err)
	}

	persistMTRPathResult(t, store, node.Node.ID, targetID, "mtr-req-1", base, base, routeA)
	persistMTRPathResult(t, store, node.Node.ID, targetID, "mtr-req-2", base.Add(time.Minute), base.Add(time.Minute), routeB)
	persistMTRPathResult(t, store, node.Node.ID, targetID, "mtr-req-3", base.Add(2*time.Minute), base.Add(2*time.Minute), routeError)
	// The failing probe opens a reachability alert on the legacy fingerprint.
	openAlerts, err := store.ListAlerts(context.Background(), AlertQuery{Statuses: []string{AlertStatusOpen}})
	if err != nil {
		t.Fatal(err)
	}
	if len(openAlerts) != 2 {
		t.Fatalf("open alerts = %d, want path change and reachability alerts", len(openAlerts))
	}

	// A successful probe resolves the reachability alert but must leave the
	// path change event untouched.
	persistMTRPathResult(t, store, node.Node.ID, targetID, "mtr-req-4", base.Add(3*time.Minute), base.Add(3*time.Minute), routeB)
	openAlerts, err = store.ListAlerts(context.Background(), AlertQuery{Statuses: []string{AlertStatusOpen}})
	if err != nil {
		t.Fatal(err)
	}
	if len(openAlerts) != 1 || openAlerts[0].Reason != alertReasonPathChanged || openAlerts[0].OccurrenceCount != 1 {
		t.Fatalf("open alerts after success = %+v, want only the path change alert", openAlerts)
	}
}
