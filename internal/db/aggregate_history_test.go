package db

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"
)

type resourceAggregateRow struct {
	sampleCount      int64
	cpuAvg, cpuMax   float64
	memAvg, memMax   float64
	diskAvg, diskMax float64
	rxMin, rxMax     int64
	txMin, txMax     int64
}

type networkAggregateRow struct {
	total        int64
	success      int64
	failure      int64
	latencyAvg   float64
	latencyMax   int64
	latencyCount int64
}

func queryResourceAggregateRow(t *testing.T, store *Store, table string, windowStart time.Time, nodeID string) resourceAggregateRow {
	t.Helper()
	var row resourceAggregateRow
	err := store.db.QueryRow(`SELECT sample_count, cpu_avg, cpu_max, mem_used_ratio_avg, mem_used_ratio_max, disk_used_ratio_avg, disk_used_ratio_max, rx_min, rx_max, tx_min, tx_max FROM `+table+` WHERE window_start = ? AND node_id = ?`, unixNano(windowStart), nodeID).Scan(
		&row.sampleCount, &row.cpuAvg, &row.cpuMax, &row.memAvg, &row.memMax, &row.diskAvg, &row.diskMax, &row.rxMin, &row.rxMax, &row.txMin, &row.txMax)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func queryNetworkAggregateRow(t *testing.T, store *Store, table string, windowStart time.Time, nodeID, targetID string) networkAggregateRow {
	t.Helper()
	var row networkAggregateRow
	err := store.db.QueryRow(`SELECT total, success, failure, latency_avg, latency_max, latency_count FROM `+table+` WHERE window_start = ? AND node_id = ? AND target_id = ?`, unixNano(windowStart), nodeID, targetID).Scan(
		&row.total, &row.success, &row.failure, &row.latencyAvg, &row.latencyMax, &row.latencyCount)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func assertResourceAggregateRow(t *testing.T, name string, got, want resourceAggregateRow) {
	t.Helper()
	if got.sampleCount != want.sampleCount || got.rxMin != want.rxMin || got.rxMax != want.rxMax || got.txMin != want.txMin || got.txMax != want.txMax {
		t.Fatalf("%s = %+v, want %+v (integer fields)", name, got, want)
	}
	for _, metric := range []struct {
		label     string
		got, want float64
	}{
		{"cpu_avg", got.cpuAvg, want.cpuAvg},
		{"cpu_max", got.cpuMax, want.cpuMax},
		{"mem_used_ratio_avg", got.memAvg, want.memAvg},
		{"mem_used_ratio_max", got.memMax, want.memMax},
		{"disk_used_ratio_avg", got.diskAvg, want.diskAvg},
		{"disk_used_ratio_max", got.diskMax, want.diskMax},
	} {
		if math.Abs(metric.got-metric.want) > 1e-9 {
			t.Fatalf("%s.%s = %v, want %v", name, metric.label, metric.got, metric.want)
		}
	}
}

func assertNetworkAggregateRow(t *testing.T, name string, got, want networkAggregateRow) {
	t.Helper()
	if got.total != want.total || got.success != want.success || got.failure != want.failure || got.latencyMax != want.latencyMax || got.latencyCount != want.latencyCount {
		t.Fatalf("%s = %+v, want %+v (integer fields)", name, got, want)
	}
	if math.Abs(got.latencyAvg-want.latencyAvg) > 1e-9 {
		t.Fatalf("%s.latency_avg = %v, want %v", name, got.latencyAvg, want.latencyAvg)
	}
}

func TestAggregateHistoryFoldsExpiredRowsIntoHourlyAndDaily(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	node := registerTestNode(t, store, "agg-node")
	nodeID := node.Node.ID
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	boundary := now.Add(-48 * time.Hour)

	if err := store.CreateNetworkTarget(ctx, ResultTargetInput{ID: "tgt-1", Name: "target-1", Kind: string(TargetKindTCP), Host: "example.com"}, now); err != nil {
		t.Fatal(err)
	}

	// Latest snapshots must be untouched by aggregation.
	latestResource := []byte(`{"cpu_percent":7}`)
	latestNetwork := []byte(`{"status":"success","latency_ms":11}`)
	if err := store.UpsertResourceLatest(ctx, nodeID, now, latestResource); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertNetworkLatest(ctx, nodeID, "tgt-1", now, latestNetwork); err != nil {
		t.Fatal(err)
	}

	type resourceRow struct {
		at                                     time.Time
		cpu                                    float64
		memUsed, memTotal, diskUsed, diskTotal uint64
		rx, tx                                 uint64
	}
	resourceRows := []resourceRow{
		// Expired hour window 2026-09-19T10:00Z.
		{time.Date(2026, 9, 19, 10, 5, 0, 0, time.UTC), 10, 500, 1000, 200, 1000, 1000, 2000},
		{time.Date(2026, 9, 19, 10, 15, 0, 0, time.UTC), 30, 700, 1000, 400, 1000, 3000, 4000},
		{time.Date(2026, 9, 19, 10, 25, 0, 0, time.UTC), 50, 600, 1000, 600, 1000, 2000, 3000},
		// Same expired day, different hour: daily only.
		{time.Date(2026, 9, 19, 8, 0, 0, 0, time.UTC), 90, 900, 1000, 100, 1000, 5000, 6000},
		// Within the retention window: must stay raw.
		{now.Add(-47 * time.Hour), 5, 100, 1000, 100, 1000, 7000, 8000},
		{now.Add(-time.Hour), 1, 50, 1000, 50, 1000, 9000, 10000},
	}
	for _, row := range resourceRows {
		payload, err := json.Marshal(map[string]any{
			"cpu_percent": row.cpu, "memory_used_bytes": row.memUsed, "memory_total_bytes": row.memTotal,
			"filesystem_used_bytes": row.diskUsed, "filesystem_total_bytes": row.diskTotal,
			"network_rx_bytes": row.rx, "network_tx_bytes": row.tx,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.db.Exec(`INSERT INTO resource_history (node_id, payload, reported_at, recorded_at) VALUES (?, ?, ?, ?)`, nodeID, payload, unixNano(row.at), unixNano(now)); err != nil {
			t.Fatal(err)
		}
	}

	type networkRow struct {
		at      time.Time
		status  string
		latency int64
	}
	networkRows := []networkRow{
		// Expired hour window 2026-09-19T11:00Z.
		{time.Date(2026, 9, 19, 11, 5, 0, 0, time.UTC), "success", 100},
		{time.Date(2026, 9, 19, 11, 15, 0, 0, time.UTC), "success", 300},
		{time.Date(2026, 9, 19, 11, 25, 0, 0, time.UTC), "timeout", 0},
		// Same expired day, different hour: daily only.
		{time.Date(2026, 9, 19, 8, 0, 0, 0, time.UTC), "blocked", 50},
		// Within the retention window: must stay raw.
		{now.Add(-47 * time.Hour), "success", 10},
		{now.Add(-time.Hour), "timeout", 0},
	}
	for _, row := range networkRows {
		payload, err := json.Marshal(map[string]any{"status": row.status, "latency_ms": row.latency})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.db.Exec(`INSERT INTO network_results_history (node_id, target_id, payload, checked_at, recorded_at) VALUES (?, ?, ?, ?, ?)`, nodeID, "tgt-1", payload, unixNano(row.at), unixNano(now)); err != nil {
			t.Fatal(err)
		}
	}

	result, err := store.AggregateHistory(ctx, boundary)
	if err != nil {
		t.Fatal(err)
	}
	if result.ResourceRows != 4 || result.NetworkRows != 4 {
		t.Fatalf("aggregate result = %+v, want 4 expired rows per table", result)
	}

	// Raw rows: only the ones inside the retention window remain. The latest
	// upserts added one fresh row of their own to each raw history table.
	assertRowCount(t, store, `SELECT count(*) FROM resource_history`, 3)
	assertRowCount(t, store, `SELECT count(*) FROM network_results_history`, 3)
	assertRowCount(t, store, `SELECT count(*) FROM resource_history WHERE reported_at < ?`, 0, unixNano(boundary))
	assertRowCount(t, store, `SELECT count(*) FROM network_results_history WHERE checked_at < ?`, 0, unixNano(boundary))

	// Aggregate row counts: two hourly windows (10:00 and 08:00) and one daily
	// window per table.
	assertRowCount(t, store, `SELECT count(*) FROM resource_history_hourly`, 2)
	assertRowCount(t, store, `SELECT count(*) FROM resource_history_daily`, 1)
	assertRowCount(t, store, `SELECT count(*) FROM network_results_history_hourly`, 2)
	assertRowCount(t, store, `SELECT count(*) FROM network_results_history_daily`, 1)

	hourWindow := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	earlyHourWindow := time.Date(2026, 9, 19, 8, 0, 0, 0, time.UTC)
	dayWindow := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	assertResourceAggregateRow(t, "resource hourly", queryResourceAggregateRow(t, store, "resource_history_hourly", hourWindow, nodeID), resourceAggregateRow{
		sampleCount: 3, cpuAvg: 30, cpuMax: 50, memAvg: 0.6, memMax: 0.7, diskAvg: 0.4, diskMax: 0.6, rxMin: 1000, rxMax: 3000, txMin: 2000, txMax: 4000,
	})
	assertResourceAggregateRow(t, "resource early hour", queryResourceAggregateRow(t, store, "resource_history_hourly", earlyHourWindow, nodeID), resourceAggregateRow{
		sampleCount: 1, cpuAvg: 90, cpuMax: 90, memAvg: 0.9, memMax: 0.9, diskAvg: 0.1, diskMax: 0.1, rxMin: 5000, rxMax: 5000, txMin: 6000, txMax: 6000,
	})
	assertResourceAggregateRow(t, "resource daily", queryResourceAggregateRow(t, store, "resource_history_daily", dayWindow, nodeID), resourceAggregateRow{
		sampleCount: 4, cpuAvg: 45, cpuMax: 90, memAvg: 0.675, memMax: 0.9, diskAvg: 0.325, diskMax: 0.6, rxMin: 1000, rxMax: 5000, txMin: 2000, txMax: 6000,
	})
	networkHourWindow := time.Date(2026, 9, 19, 11, 0, 0, 0, time.UTC)
	assertNetworkAggregateRow(t, "network hourly", queryNetworkAggregateRow(t, store, "network_results_history_hourly", networkHourWindow, nodeID, "tgt-1"), networkAggregateRow{
		total: 3, success: 2, failure: 1, latencyAvg: 200, latencyMax: 300, latencyCount: 2,
	})
	assertNetworkAggregateRow(t, "network early hour", queryNetworkAggregateRow(t, store, "network_results_history_hourly", earlyHourWindow, nodeID, "tgt-1"), networkAggregateRow{
		total: 1, success: 0, failure: 1, latencyAvg: 50, latencyMax: 50, latencyCount: 1,
	})
	assertNetworkAggregateRow(t, "network daily", queryNetworkAggregateRow(t, store, "network_results_history_daily", dayWindow, nodeID, "tgt-1"), networkAggregateRow{
		total: 4, success: 2, failure: 2, latencyAvg: 150, latencyMax: 300, latencyCount: 3,
	})

	// Latest snapshots are untouched.
	gotAt, gotPayload, err := store.GetResourceLatest(ctx, nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if !gotAt.Equal(now) || string(gotPayload) != string(latestResource) {
		t.Fatalf("resource latest changed: at=%s payload=%s", gotAt, gotPayload)
	}
	gotAt, gotPayload, err = store.GetNetworkLatest(ctx, nodeID, "tgt-1")
	if err != nil {
		t.Fatal(err)
	}
	if !gotAt.Equal(now) || string(gotPayload) != string(latestNetwork) {
		t.Fatalf("network latest changed: at=%s payload=%s", gotAt, gotPayload)
	}

	// Repeated aggregation must not double count anything.
	second, err := store.AggregateHistory(ctx, boundary)
	if err != nil {
		t.Fatal(err)
	}
	if second.ResourceRows != 0 || second.NetworkRows != 0 {
		t.Fatalf("repeat aggregate result = %+v, want no rows folded", second)
	}
	assertRowCount(t, store, `SELECT count(*) FROM resource_history`, 3)
	assertRowCount(t, store, `SELECT count(*) FROM network_results_history`, 3)
	assertRowCount(t, store, `SELECT count(*) FROM resource_history_hourly`, 2)
	assertRowCount(t, store, `SELECT count(*) FROM resource_history_daily`, 1)
	assertRowCount(t, store, `SELECT count(*) FROM network_results_history_hourly`, 2)
	assertRowCount(t, store, `SELECT count(*) FROM network_results_history_daily`, 1)
	assertResourceAggregateRow(t, "resource hourly after repeat", queryResourceAggregateRow(t, store, "resource_history_hourly", hourWindow, nodeID), resourceAggregateRow{
		sampleCount: 3, cpuAvg: 30, cpuMax: 50, memAvg: 0.6, memMax: 0.7, diskAvg: 0.4, diskMax: 0.6, rxMin: 1000, rxMax: 3000, txMin: 2000, txMax: 4000,
	})
	assertNetworkAggregateRow(t, "network daily after repeat", queryNetworkAggregateRow(t, store, "network_results_history_daily", dayWindow, nodeID, "tgt-1"), networkAggregateRow{
		total: 4, success: 2, failure: 2, latencyAvg: 150, latencyMax: 300, latencyCount: 3,
	})
}

func TestAggregateHistoryMergesWindowsSplitAcrossBatches(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	node := registerTestNode(t, store, "batch-node")
	nodeID := node.Node.ID
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	boundary := now.Add(-48 * time.Hour)

	if err := store.CreateNetworkTarget(ctx, ResultTargetInput{ID: "tgt-1", Name: "target-1", Kind: string(TargetKindTCP), Host: "example.com"}, now); err != nil {
		t.Fatal(err)
	}

	// Two hourly windows of 600 samples each: one bounded pass takes the first
	// 1000 raw rows, splitting the second window across two passes.
	hour0 := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	hour1 := hour0.Add(time.Hour)
	rowsPerWindow := 600
	if tx, err := store.db.Begin(); err != nil {
		t.Fatal(err)
	} else {
		for i := 0; i < 2*rowsPerWindow; i++ {
			at := hour0.Add(time.Duration(i) * time.Second)
			if i >= rowsPerWindow {
				at = hour1.Add(time.Duration(i-rowsPerWindow) * time.Second)
			}
			cpu := 10.0
			latency := int64(100)
			if i >= rowsPerWindow {
				cpu = 30
				latency = 200
			} else if i%2 == 1 {
				cpu = 20
			}
			resourcePayload, err := json.Marshal(map[string]any{"cpu_percent": cpu, "memory_used_bytes": 100, "memory_total_bytes": 1000, "filesystem_used_bytes": 100, "filesystem_total_bytes": 1000, "network_rx_bytes": i, "network_tx_bytes": i + 1})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(`INSERT INTO resource_history (node_id, payload, reported_at, recorded_at) VALUES (?, ?, ?, ?)`, nodeID, resourcePayload, unixNano(at), unixNano(now)); err != nil {
				t.Fatal(err)
			}
			networkPayload, err := json.Marshal(map[string]any{"status": "success", "latency_ms": latency})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(`INSERT INTO network_results_history (node_id, target_id, payload, checked_at, recorded_at) VALUES (?, ?, ?, ?, ?)`, nodeID, "tgt-1", networkPayload, unixNano(at), unixNano(now)); err != nil {
				t.Fatal(err)
			}
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}

	first, err := store.AggregateHistory(ctx, boundary)
	if err != nil {
		t.Fatal(err)
	}
	if first.ResourceRows != int64(maxAggregateHistoryRows) || first.NetworkRows != int64(maxAggregateHistoryRows) {
		t.Fatalf("first pass result = %+v, want %d rows per table", first, maxAggregateHistoryRows)
	}
	second, err := store.AggregateHistory(ctx, boundary)
	if err != nil {
		t.Fatal(err)
	}
	if second.ResourceRows != int64(2*rowsPerWindow-maxAggregateHistoryRows) || second.NetworkRows != int64(2*rowsPerWindow-maxAggregateHistoryRows) {
		t.Fatalf("second pass result = %+v, want the remaining raw rows", second)
	}
	third, err := store.AggregateHistory(ctx, boundary)
	if err != nil {
		t.Fatal(err)
	}
	if third.ResourceRows != 0 || third.NetworkRows != 0 {
		t.Fatalf("third pass result = %+v, want no rows folded", third)
	}

	assertRowCount(t, store, `SELECT count(*) FROM resource_history`, 0)
	assertRowCount(t, store, `SELECT count(*) FROM network_results_history`, 0)

	dayWindow := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	assertResourceAggregateRow(t, "resource hourly h0", queryResourceAggregateRow(t, store, "resource_history_hourly", hour0, nodeID), resourceAggregateRow{
		sampleCount: 600, cpuAvg: 15, cpuMax: 20, memAvg: 0.1, memMax: 0.1, diskAvg: 0.1, diskMax: 0.1, rxMin: 0, rxMax: 599, txMin: 1, txMax: 600,
	})
	assertResourceAggregateRow(t, "resource hourly h1 (split across passes)", queryResourceAggregateRow(t, store, "resource_history_hourly", hour1, nodeID), resourceAggregateRow{
		sampleCount: 600, cpuAvg: 30, cpuMax: 30, memAvg: 0.1, memMax: 0.1, diskAvg: 0.1, diskMax: 0.1, rxMin: 600, rxMax: 1199, txMin: 601, txMax: 1200,
	})
	assertResourceAggregateRow(t, "resource daily", queryResourceAggregateRow(t, store, "resource_history_daily", dayWindow, nodeID), resourceAggregateRow{
		sampleCount: 1200, cpuAvg: 22.5, cpuMax: 30, memAvg: 0.1, memMax: 0.1, diskAvg: 0.1, diskMax: 0.1, rxMin: 0, rxMax: 1199, txMin: 1, txMax: 1200,
	})
	assertNetworkAggregateRow(t, "network hourly h0", queryNetworkAggregateRow(t, store, "network_results_history_hourly", hour0, nodeID, "tgt-1"), networkAggregateRow{
		total: 600, success: 600, failure: 0, latencyAvg: 100, latencyMax: 100, latencyCount: 600,
	})
	assertNetworkAggregateRow(t, "network hourly h1 (split across passes)", queryNetworkAggregateRow(t, store, "network_results_history_hourly", hour1, nodeID, "tgt-1"), networkAggregateRow{
		total: 600, success: 600, failure: 0, latencyAvg: 200, latencyMax: 200, latencyCount: 600,
	})
	assertNetworkAggregateRow(t, "network daily", queryNetworkAggregateRow(t, store, "network_results_history_daily", dayWindow, nodeID, "tgt-1"), networkAggregateRow{
		total: 1200, success: 1200, failure: 0, latencyAvg: 150, latencyMax: 200, latencyCount: 1200,
	})

	// One more pass changes nothing.
	fourth, err := store.AggregateHistory(ctx, boundary)
	if err != nil {
		t.Fatal(err)
	}
	if fourth.ResourceRows != 0 || fourth.NetworkRows != 0 {
		t.Fatalf("fourth pass result = %+v, want no rows folded", fourth)
	}
	assertResourceAggregateRow(t, "resource daily after repeat", queryResourceAggregateRow(t, store, "resource_history_daily", dayWindow, nodeID), resourceAggregateRow{
		sampleCount: 1200, cpuAvg: 22.5, cpuMax: 30, memAvg: 0.1, memMax: 0.1, diskAvg: 0.1, diskMax: 0.1, rxMin: 0, rxMax: 1199, txMin: 1, txMax: 1200,
	})
}
