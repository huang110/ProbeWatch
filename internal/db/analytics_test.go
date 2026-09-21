package db

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"
)

func insertNetworkResultRow(t *testing.T, store *Store, nodeID, targetID string, at time.Time, status string, latency int64) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"status": status, "latency_ms": latency})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`INSERT INTO network_results_history (node_id, target_id, payload, checked_at, recorded_at) VALUES (?, ?, ?, ?, ?)`, nodeID, targetID, payload, unixNano(at), unixNano(at)); err != nil {
		t.Fatal(err)
	}
}

func insertResourceCounterRow(t *testing.T, store *Store, nodeID string, at time.Time, rx, tx uint64) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"cpu_percent": 1.0, "network_rx_bytes": rx, "network_tx_bytes": tx})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`INSERT INTO resource_history (node_id, payload, reported_at, recorded_at) VALUES (?, ?, ?, ?)`, nodeID, payload, unixNano(at), unixNano(at)); err != nil {
		t.Fatal(err)
	}
}

func assertFloatNear(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
}

func countRows(t *testing.T, store *Store, query string) int {
	t.Helper()
	var count int
	if err := store.db.QueryRow(query).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestGetCheckSummaryRawWindow(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	node := registerTestNode(t, store, "summary-node")
	nodeID := node.Node.ID
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	if err := store.CreateNetworkTarget(ctx, ResultTargetInput{ID: "tgt-1", Name: "target-1", Kind: string(TargetKindTCP), Host: "example.com"}, now); err != nil {
		t.Fatal(err)
	}
	insertNetworkResultRow(t, store, nodeID, "tgt-1", now.Add(-150*time.Minute), "success", 999) // outside window
	insertNetworkResultRow(t, store, nodeID, "tgt-1", now.Add(-90*time.Minute), "success", 100)
	insertNetworkResultRow(t, store, nodeID, "tgt-1", now.Add(-60*time.Minute), "success", 300)
	insertNetworkResultRow(t, store, nodeID, "tgt-1", now.Add(-30*time.Minute), "timeout", 0)

	from, to := now.Add(-2*time.Hour), now
	summaries, err := store.GetCheckSummary(ctx, nodeID, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("summaries = %+v, want one target", summaries)
	}
	summary := summaries[0]
	if summary.TargetID != "tgt-1" || summary.Name != "target-1" || summary.Kind != "tcp" || summary.Host != "example.com" {
		t.Fatalf("target metadata = %+v", summary)
	}
	if !summary.HasWindowData || summary.Total != 3 || summary.Success != 2 || summary.Failure != 1 {
		t.Fatalf("counts = %+v", summary)
	}
	assertFloatNear(t, "latency_avg_ms", summary.LatencyAvgMS, 200)
	if summary.LatencyCount != 2 {
		t.Fatalf("latency_count = %d, want 2", summary.LatencyCount)
	}
	// Jitter is max−min over raw latencies: 300−100.
	assertFloatNear(t, "jitter_ms", summary.JitterMS, 200)
	if !summary.HasJitter {
		t.Fatalf("jitter must be present with latency samples")
	}
	if !summary.LastCheckedAt.Equal(now.Add(-30 * time.Minute)) {
		t.Fatalf("last_checked_at = %s, want %s", summary.LastCheckedAt, now.Add(-30*time.Minute))
	}

	// network_results_latest wins for last_checked_at even when its check is
	// outside the window, and must not disturb the window statistics.
	if err := store.UpsertNetworkLatest(ctx, nodeID, "tgt-1", now.Add(time.Second), []byte(`{"status":"success","latency_ms":50}`)); err != nil {
		t.Fatal(err)
	}
	summaries, err = store.GetCheckSummary(ctx, nodeID, from, to)
	if err != nil {
		t.Fatal(err)
	}
	summary = summaries[0]
	if summary.Total != 3 || summary.Success != 2 || summary.Failure != 1 {
		t.Fatalf("counts after latest = %+v", summary)
	}
	if !summary.LastCheckedAt.Equal(now.Add(time.Second)) {
		t.Fatalf("last_checked_at after latest = %s, want %s", summary.LastCheckedAt, now.Add(time.Second))
	}
}

func TestGetCheckSummaryUsesAggregatesAndNeverDoubleCounts(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	node := registerTestNode(t, store, "agg-summary-node")
	nodeID := node.Node.ID
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	boundary := now.Add(-48 * time.Hour)
	if err := store.CreateNetworkTarget(ctx, ResultTargetInput{ID: "tgt-agg", Name: "agg", Kind: string(TargetKindTCP), Host: "example.com"}, now); err != nil {
		t.Fatal(err)
	}
	// Expired raw rows: hour 11:00 (2 success, 1 timeout) and hour 08:00.
	insertNetworkResultRow(t, store, nodeID, "tgt-agg", time.Date(2026, 9, 19, 11, 5, 0, 0, time.UTC), "success", 100)
	insertNetworkResultRow(t, store, nodeID, "tgt-agg", time.Date(2026, 9, 19, 11, 15, 0, 0, time.UTC), "success", 300)
	insertNetworkResultRow(t, store, nodeID, "tgt-agg", time.Date(2026, 9, 19, 11, 25, 0, 0, time.UTC), "timeout", 0)
	insertNetworkResultRow(t, store, nodeID, "tgt-agg", time.Date(2026, 9, 19, 8, 0, 0, 0, time.UTC), "blocked", 50)
	if _, err := store.AggregateHistory(ctx, boundary); err != nil {
		t.Fatal(err)
	}

	// Aggregated-only window: stats come from the hourly table. Jitter is the
	// max−min proxy: hourly 11:00 contributes max 300 / min-stand-in avg 200,
	// hourly 08:00 contributes max 50 / min 50 → 300−50.
	oldFrom := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	oldTo := time.Date(2026, 9, 19, 23, 0, 0, 0, time.UTC)
	summaries, err := store.GetCheckSummary(ctx, nodeID, oldFrom, oldTo)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("summaries = %+v, want one target", summaries)
	}
	summary := summaries[0]
	if !summary.HasWindowData || summary.Total != 4 || summary.Success != 2 || summary.Failure != 2 {
		t.Fatalf("aggregate counts = %+v", summary)
	}
	assertFloatNear(t, "aggregate latency_avg_ms", summary.LatencyAvgMS, 150)
	assertFloatNear(t, "aggregate jitter_ms", summary.JitterMS, 250)
	if !summary.LastCheckedAt.IsZero() {
		t.Fatalf("aggregate-only window must not fabricate last_checked_at, got %s", summary.LastCheckedAt)
	}

	// Mixed window spanning the retention boundary: raw samples and hourly
	// windows sum without double counting.
	insertNetworkResultRow(t, store, nodeID, "tgt-agg", now.Add(-time.Hour), "success", 10)
	insertNetworkResultRow(t, store, nodeID, "tgt-agg", now.Add(-30*time.Minute), "timeout", 0)
	summaries, err = store.GetCheckSummary(ctx, nodeID, oldFrom, now)
	if err != nil {
		t.Fatal(err)
	}
	summary = summaries[0]
	if summary.Total != 6 || summary.Success != 3 || summary.Failure != 3 {
		t.Fatalf("mixed counts = %+v", summary)
	}
	assertFloatNear(t, "mixed latency_avg_ms", summary.LatencyAvgMS, 115)
	// min 10 (raw) to max 300 (aggregate).
	assertFloatNear(t, "mixed jitter_ms", summary.JitterMS, 290)
	if !summary.LastCheckedAt.Equal(now.Add(-30 * time.Minute)) {
		t.Fatalf("mixed last_checked_at = %s", summary.LastCheckedAt)
	}
}

func TestGetCheckSummaryWithoutWindowDataKeepsLatestOnly(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	node := registerTestNode(t, store, "latest-only-node")
	nodeID := node.Node.ID
	now := time.Now().UTC().Truncate(time.Second)
	if err := store.CreateNetworkTarget(ctx, ResultTargetInput{ID: "tgt-old", Name: "old", Kind: string(TargetKindHTTP), Host: "example.org"}, now); err != nil {
		t.Fatal(err)
	}
	checkedAt := now.Add(-72 * time.Hour) // far outside the window
	if err := store.UpsertNetworkLatest(ctx, nodeID, "tgt-old", checkedAt, []byte(`{"status":"success","latency_ms":42}`)); err != nil {
		t.Fatal(err)
	}

	summaries, err := store.GetCheckSummary(ctx, nodeID, now.Add(-24*time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("summaries = %+v, want the latest-only target", summaries)
	}
	summary := summaries[0]
	if summary.HasWindowData {
		t.Fatalf("window has no data, got %+v", summary)
	}
	if summary.Total != 0 || summary.Success != 0 || summary.Failure != 0 || summary.LatencyCount != 0 || summary.HasJitter {
		t.Fatalf("stats must be empty without window data: %+v", summary)
	}
	if !summary.LastCheckedAt.Equal(checkedAt) {
		t.Fatalf("last_checked_at = %s, want %s", summary.LastCheckedAt, checkedAt)
	}

	// A node that has never reported anything yields no entries at all.
	emptyNode := registerTestNode(t, store, "never-reported-node")
	empty, err := store.GetCheckSummary(ctx, emptyNode.Node.ID, now.Add(-time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("window without any data = %+v, want no entries", empty)
	}
}

func TestGetCheckSummaryRejectsInvalidWindow(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	node := registerTestNode(t, store, "invalid-window-node")
	now := time.Now().UTC()
	if _, err := store.GetCheckSummary(ctx, node.Node.ID, now, now.Add(-time.Hour)); err == nil {
		t.Fatalf("inverted window must be rejected")
	}
	if _, err := store.GetCheckSummary(ctx, node.Node.ID, time.Time{}, now); err == nil {
		t.Fatalf("zero from must be rejected")
	}
}

func TestGetTrafficReportRawCountersWithReset(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	node := registerTestNode(t, store, "traffic-node")
	nodeID := node.Node.ID
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	from, to, step := now.Add(-24*time.Hour), now, time.Hour

	insertResourceCounterRow(t, store, nodeID, now.Add(-3*time.Hour), 100, 200)     // baseline
	insertResourceCounterRow(t, store, nodeID, now.Add(-2*time.Hour), 1100, 300)    // rx +1000, tx +100
	insertResourceCounterRow(t, store, nodeID, now.Add(-time.Hour), 60, 1200)       // rx reset (1100→60), tx +900
	insertResourceCounterRow(t, store, nodeID, now.Add(-30*time.Minute), 500, 1300) // rx +440, tx +100

	report, err := store.GetTrafficReport(ctx, nodeID, from, to, step)
	if err != nil {
		t.Fatal(err)
	}
	if !report.HasData {
		t.Fatalf("report must have data: %+v", report)
	}
	if report.RxBytes != 1440 {
		t.Fatalf("rx_bytes = %d, want 1440 (reset segment skipped)", report.RxBytes)
	}
	if report.TxBytes != 1100 {
		t.Fatalf("tx_bytes = %d, want 1100", report.TxBytes)
	}
	if report.RxResets != 1 || report.TxResets != 0 {
		t.Fatalf("resets = rx %d tx %d, want rx 1 tx 0", report.RxResets, report.TxResets)
	}
	if len(report.Buckets) != 25 {
		t.Fatalf("buckets = %d, want 25", len(report.Buckets))
	}
	// Deltas land in the bucket of the later observation: window starts at
	// 12:00 the day before, so 09:00 is bucket 21, 10:00 bucket 22 and
	// 11:00/11:30 bucket 23.
	if bucket := report.Buckets[22]; !bucket.HasData || bucket.RxBytes != 1000 || bucket.TxBytes != 100 {
		t.Fatalf("bucket 10:00 = %+v", report.Buckets[22])
	}
	if bucket := report.Buckets[23]; !bucket.HasData || bucket.RxBytes != 440 || bucket.TxBytes != 1000 {
		t.Fatalf("bucket 11:00 = %+v", report.Buckets[23])
	}
	if bucket := report.Buckets[21]; !bucket.HasData || bucket.RxBytes != 0 || bucket.TxBytes != 0 {
		t.Fatalf("baseline bucket = %+v", report.Buckets[21])
	}
	if bucket := report.Buckets[0]; bucket.HasData {
		t.Fatalf("empty bucket must have no data: %+v", bucket)
	}
}

func TestGetTrafficReportUsesHourlyAggregates(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	node := registerTestNode(t, store, "traffic-agg-node")
	nodeID := node.Node.ID
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	boundary := now.Add(-48 * time.Hour)

	// Three hours, all older than the boundary: hour A rx 100→200 / tx
	// 500→600, hour B rx 200→350 / tx 600→750, hour C rx 50→80 / tx 500→830
	// after a reboot (both counters dropped relative to hour B).
	insertResourceCounterRow(t, store, nodeID, time.Date(2026, 9, 19, 10, 5, 0, 0, time.UTC), 100, 500)
	insertResourceCounterRow(t, store, nodeID, time.Date(2026, 9, 19, 10, 15, 0, 0, time.UTC), 200, 600)
	insertResourceCounterRow(t, store, nodeID, time.Date(2026, 9, 19, 11, 5, 0, 0, time.UTC), 200, 600)
	insertResourceCounterRow(t, store, nodeID, time.Date(2026, 9, 19, 11, 15, 0, 0, time.UTC), 350, 750)
	insertResourceCounterRow(t, store, nodeID, time.Date(2026, 9, 19, 13, 5, 0, 0, time.UTC), 50, 500)
	insertResourceCounterRow(t, store, nodeID, time.Date(2026, 9, 19, 13, 15, 0, 0, time.UTC), 80, 830)
	if _, err := store.AggregateHistory(ctx, boundary); err != nil {
		t.Fatal(err)
	}
	if rows := countRows(t, store, `SELECT count(*) FROM resource_history`); rows != 0 {
		t.Fatalf("raw rows must be aggregated, %d left", rows)
	}

	from := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 19, 14, 0, 0, 0, time.UTC)
	report, err := store.GetTrafficReport(ctx, nodeID, from, to, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// rx: 100 (A) + 0 (bridge) + 150 (B) + reset (350→50 skipped) + 30 (C) = 280.
	if report.RxBytes != 280 || report.RxResets != 1 {
		t.Fatalf("rx = %d resets %d, want 280/1", report.RxBytes, report.RxResets)
	}
	// tx: 100 + 0 + 150 + reset (750→500 skipped) + 330 = 580.
	if report.TxBytes != 580 || report.TxResets != 1 {
		t.Fatalf("tx = %d resets %d, want 580/1", report.TxBytes, report.TxResets)
	}
	if len(report.Buckets) != 5 {
		t.Fatalf("buckets = %d, want 5", len(report.Buckets))
	}
	expectations := []struct {
		index   int
		rx, tx  int64
		hasData bool
	}{
		{0, 0, 0, true},     // baseline at 10:00
		{1, 100, 100, true}, // hour A internal delta, bridge 200→200
		{2, 150, 150, true}, // hour B internal delta at 12:00
		{3, 0, 0, true},     // reset segment at 13:00, bucket marked but no delta
		{4, 30, 330, true},  // hour C internal delta at 14:00 (clamped last bucket)
	}
	for _, expected := range expectations {
		bucket := report.Buckets[expected.index]
		if bucket.HasData != expected.hasData || bucket.RxBytes != expected.rx || bucket.TxBytes != expected.tx {
			t.Fatalf("bucket %d = %+v, want rx %d tx %d hasData %v", expected.index, bucket, expected.rx, expected.tx, expected.hasData)
		}
	}
}

func TestGetTrafficReportWithoutDataAndInvalidArguments(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	node := registerTestNode(t, store, "traffic-empty-node")
	now := time.Now().UTC()

	report, err := store.GetTrafficReport(ctx, node.Node.ID, now.Add(-24*time.Hour), now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if report.HasData || report.RxBytes != 0 || report.TxBytes != 0 || report.RxResets != 0 {
		t.Fatalf("empty report = %+v", report)
	}
	for _, bucket := range report.Buckets {
		if bucket.HasData {
			t.Fatalf("empty report bucket has data: %+v", bucket)
		}
	}

	if _, err := store.GetTrafficReport(ctx, node.Node.ID, now, now.Add(-time.Hour), time.Hour); err == nil {
		t.Fatalf("inverted window must be rejected")
	}
	if _, err := store.GetTrafficReport(ctx, node.Node.ID, now.Add(-time.Hour), now, 0); err == nil {
		t.Fatalf("zero step must be rejected")
	}
	if _, err := store.GetTrafficReport(ctx, node.Node.ID, time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), now, time.Hour); err == nil {
		t.Fatalf("oversized bucket count must be rejected")
	}
}
