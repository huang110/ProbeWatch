package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// DefaultAggregateRetentionWindow is how long raw history rows are kept in
// full resolution. Older raw rows are folded into the hourly and daily
// aggregate tables by AggregateHistory.
const DefaultAggregateRetentionWindow = 48 * time.Hour

// maxAggregateHistoryRows bounds how many raw history rows a single
// AggregateHistory pass folds in per table, keeping every pass short and
// transactional. Backlogs drain across repeated calls.
const maxAggregateHistoryRows = 1000

// AggregateHistoryResult reports how many raw history rows one bounded pass
// folded into the aggregate tables and deleted.
type AggregateHistoryResult struct {
	// ResourceRows is the number of resource_history rows aggregated and removed.
	ResourceRows int64
	// NetworkRows is the number of network_results_history rows aggregated and removed.
	NetworkRows int64
}

// AggregateHistory folds raw history rows reported before the retention
// boundary into resource_history_hourly/daily and
// network_results_history_hourly/daily, then deletes the folded raw rows. The
// aggregation and the delete share one transaction per table, so a successful
// call never double counts: rows that were already folded are gone and cannot
// be aggregated again. Each call processes at most maxAggregateHistoryRows raw
// rows per table; a window split across passes is merged into the existing
// aggregate row, so repeated calls stay correct and idempotent.
func (s *Store) AggregateHistory(ctx context.Context, boundary time.Time) (AggregateHistoryResult, error) {
	resourceRows, err := s.aggregateResourceHistory(ctx, boundary)
	if err != nil {
		return AggregateHistoryResult{}, err
	}
	networkRows, err := s.aggregateNetworkHistory(ctx, boundary)
	if err != nil {
		return AggregateHistoryResult{ResourceRows: resourceRows}, err
	}
	return AggregateHistoryResult{ResourceRows: resourceRows, NetworkRows: networkRows}, nil
}

type resourceSample struct {
	cpu       float64
	memRatio  float64
	diskRatio float64
	rx, tx    int64
}

// resourceAggregate accumulates sums and extrema; averages are derived from
// the sums so a window split across passes merges without precision loss.
type resourceAggregate struct {
	sampleCount                int64
	cpuSum, cpuMax             float64
	memRatioSum, memRatioMax   float64
	diskRatioSum, diskRatioMax float64
	rxMin, rxMax, txMin, txMax int64
}

func (a *resourceAggregate) add(sample resourceSample) {
	if a.sampleCount == 0 {
		a.cpuMax = sample.cpu
		a.memRatioMax = sample.memRatio
		a.diskRatioMax = sample.diskRatio
		a.rxMin, a.rxMax = sample.rx, sample.rx
		a.txMin, a.txMax = sample.tx, sample.tx
	} else {
		if sample.cpu > a.cpuMax {
			a.cpuMax = sample.cpu
		}
		if sample.memRatio > a.memRatioMax {
			a.memRatioMax = sample.memRatio
		}
		if sample.diskRatio > a.diskRatioMax {
			a.diskRatioMax = sample.diskRatio
		}
		if sample.rx < a.rxMin {
			a.rxMin = sample.rx
		}
		if sample.rx > a.rxMax {
			a.rxMax = sample.rx
		}
		if sample.tx < a.txMin {
			a.txMin = sample.tx
		}
		if sample.tx > a.txMax {
			a.txMax = sample.tx
		}
	}
	a.cpuSum += sample.cpu
	a.memRatioSum += sample.memRatio
	a.diskRatioSum += sample.diskRatio
	a.sampleCount++
}

func (a *resourceAggregate) merge(b resourceAggregate) {
	if b.sampleCount == 0 {
		return
	}
	if a.sampleCount == 0 {
		*a = b
		return
	}
	a.cpuSum += b.cpuSum
	if b.cpuMax > a.cpuMax {
		a.cpuMax = b.cpuMax
	}
	a.memRatioSum += b.memRatioSum
	if b.memRatioMax > a.memRatioMax {
		a.memRatioMax = b.memRatioMax
	}
	a.diskRatioSum += b.diskRatioSum
	if b.diskRatioMax > a.diskRatioMax {
		a.diskRatioMax = b.diskRatioMax
	}
	if b.rxMin < a.rxMin {
		a.rxMin = b.rxMin
	}
	if b.rxMax > a.rxMax {
		a.rxMax = b.rxMax
	}
	if b.txMin < a.txMin {
		a.txMin = b.txMin
	}
	if b.txMax > a.txMax {
		a.txMax = b.txMax
	}
	a.sampleCount += b.sampleCount
}

func (a resourceAggregate) cpuAvg() float64 { return a.cpuSum / float64(a.sampleCount) }

func (a resourceAggregate) memUsedRatioAvg() float64 { return a.memRatioSum / float64(a.sampleCount) }

func (a resourceAggregate) diskUsedRatioAvg() float64 { return a.diskRatioSum / float64(a.sampleCount) }

type networkSample struct {
	status  string
	latency int64
}

// networkAggregate keeps the latency sum so averages stay exact when a window
// is split across bounded passes. Latency statistics cover samples with a
// positive latency_ms, matching the summary endpoints.
type networkAggregate struct {
	total        int64
	success      int64
	failure      int64
	latencySum   int64
	latencyCount int64
	latencyMax   int64
}

func (a *networkAggregate) add(sample networkSample) {
	a.total++
	if sample.status == "success" {
		a.success++
	} else {
		a.failure++
	}
	if sample.latency > 0 {
		a.latencySum += sample.latency
		a.latencyCount++
		if sample.latency > a.latencyMax {
			a.latencyMax = sample.latency
		}
	}
}

func (a *networkAggregate) merge(b networkAggregate) {
	a.total += b.total
	a.success += b.success
	a.failure += b.failure
	a.latencySum += b.latencySum
	a.latencyCount += b.latencyCount
	if b.latencyMax > a.latencyMax {
		a.latencyMax = b.latencyMax
	}
}

func (a networkAggregate) latencyAvg() float64 {
	if a.latencyCount == 0 {
		return 0
	}
	return float64(a.latencySum) / float64(a.latencyCount)
}

func (s *Store) aggregateResourceHistory(ctx context.Context, boundary time.Time) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin resource history aggregation: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `SELECT id, node_id, reported_at, payload FROM resource_history WHERE reported_at < ? ORDER BY reported_at, id LIMIT ?`, unixNano(boundary), maxAggregateHistoryRows)
	if err != nil {
		return 0, fmt.Errorf("select resource history: %w", err)
	}
	type resourceKey struct {
		table       string
		windowStart int64
		nodeID      string
	}
	groups := make(map[resourceKey]*resourceAggregate)
	var ids []int64
	for rows.Next() {
		var id int64
		var nodeID string
		var reportedAt int64
		var payload []byte
		if err := rows.Scan(&id, &nodeID, &reportedAt, &payload); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan resource history: %w", err)
		}
		sample, err := decodeResourceSample(payload)
		if err != nil {
			rows.Close()
			return 0, fmt.Errorf("decode resource history row %d: %w", id, err)
		}
		reported := time.Unix(0, reportedAt).UTC()
		for _, window := range []struct {
			table string
			start time.Time
		}{{"resource_history_hourly", hourWindowStart(reported)}, {"resource_history_daily", dayWindowStart(reported)}} {
			key := resourceKey{table: window.table, windowStart: unixNano(window.start), nodeID: nodeID}
			aggregate := groups[key]
			if aggregate == nil {
				aggregate = &resourceAggregate{}
				groups[key] = aggregate
			}
			aggregate.add(sample)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("read resource history: %w", err)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("close resource history: %w", err)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	for key, aggregate := range groups {
		existing, err := loadResourceAggregateTx(ctx, tx, key.table, key.windowStart, key.nodeID)
		if err != nil {
			return 0, err
		}
		if existing != nil {
			aggregate.merge(*existing)
		}
		if err := upsertResourceAggregateTx(ctx, tx, key.table, key.windowStart, key.nodeID, *aggregate); err != nil {
			return 0, err
		}
	}
	if err := deleteRowsByIDTx(ctx, tx, "resource_history", ids); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit resource history aggregation: %w", err)
	}
	return int64(len(ids)), nil
}

func (s *Store) aggregateNetworkHistory(ctx context.Context, boundary time.Time) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin network history aggregation: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `SELECT id, node_id, target_id, checked_at, payload FROM network_results_history WHERE checked_at < ? ORDER BY checked_at, id LIMIT ?`, unixNano(boundary), maxAggregateHistoryRows)
	if err != nil {
		return 0, fmt.Errorf("select network results history: %w", err)
	}
	type networkKey struct {
		table       string
		windowStart int64
		nodeID      string
		targetID    string
	}
	groups := make(map[networkKey]*networkAggregate)
	var ids []int64
	for rows.Next() {
		var id int64
		var nodeID, targetID string
		var checkedAt int64
		var payload []byte
		if err := rows.Scan(&id, &nodeID, &targetID, &checkedAt, &payload); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan network results history: %w", err)
		}
		sample, err := decodeNetworkSample(payload)
		if err != nil {
			rows.Close()
			return 0, fmt.Errorf("decode network results history row %d: %w", id, err)
		}
		checked := time.Unix(0, checkedAt).UTC()
		for _, window := range []struct {
			table string
			start time.Time
		}{{"network_results_history_hourly", hourWindowStart(checked)}, {"network_results_history_daily", dayWindowStart(checked)}} {
			key := networkKey{table: window.table, windowStart: unixNano(window.start), nodeID: nodeID, targetID: targetID}
			aggregate := groups[key]
			if aggregate == nil {
				aggregate = &networkAggregate{}
				groups[key] = aggregate
			}
			aggregate.add(sample)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("read network results history: %w", err)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("close network results history: %w", err)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	for key, aggregate := range groups {
		existing, err := loadNetworkAggregateTx(ctx, tx, key.table, key.windowStart, key.nodeID, key.targetID)
		if err != nil {
			return 0, err
		}
		if existing != nil {
			aggregate.merge(*existing)
		}
		if err := upsertNetworkAggregateTx(ctx, tx, key.table, key.windowStart, key.nodeID, key.targetID, *aggregate); err != nil {
			return 0, err
		}
	}
	if err := deleteRowsByIDTx(ctx, tx, "network_results_history", ids); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit network history aggregation: %w", err)
	}
	return int64(len(ids)), nil
}

// decodeResourceSample extracts the bounded resource metrics the aggregates
// need. Used/total ratios fall back to zero when the total is unknown.
func decodeResourceSample(payload []byte) (resourceSample, error) {
	var snapshot struct {
		CPUPercent           float64 `json:"cpu_percent"`
		MemoryTotalBytes     uint64  `json:"memory_total_bytes"`
		MemoryUsedBytes      uint64  `json:"memory_used_bytes"`
		FilesystemTotalBytes uint64  `json:"filesystem_total_bytes"`
		FilesystemUsedBytes  uint64  `json:"filesystem_used_bytes"`
		NetworkRxBytes       uint64  `json:"network_rx_bytes"`
		NetworkTxBytes       uint64  `json:"network_tx_bytes"`
	}
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return resourceSample{}, err
	}
	return resourceSample{
		cpu:       snapshot.CPUPercent,
		memRatio:  usedRatio(snapshot.MemoryUsedBytes, snapshot.MemoryTotalBytes),
		diskRatio: usedRatio(snapshot.FilesystemUsedBytes, snapshot.FilesystemTotalBytes),
		rx:        int64(snapshot.NetworkRxBytes),
		tx:        int64(snapshot.NetworkTxBytes),
	}, nil
}

// decodeNetworkSample extracts the check outcome and latency. Only the literal
// "success" status counts as a success, mirroring the summary endpoints.
func decodeNetworkSample(payload []byte) (networkSample, error) {
	var result struct {
		Status  string `json:"status"`
		Latency int64  `json:"latency_ms"`
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return networkSample{}, err
	}
	return networkSample{status: result.Status, latency: result.Latency}, nil
}

func usedRatio(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(used) / float64(total)
}

func hourWindowStart(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), value.Day(), value.Hour(), 0, 0, 0, time.UTC)
}

func dayWindowStart(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func loadResourceAggregateTx(ctx context.Context, tx *sql.Tx, table string, windowStart int64, nodeID string) (*resourceAggregate, error) {
	var aggregate resourceAggregate
	var cpuAvg, memAvg, diskAvg float64
	err := tx.QueryRowContext(ctx, `SELECT sample_count, cpu_avg, cpu_max, mem_used_ratio_avg, mem_used_ratio_max, disk_used_ratio_avg, disk_used_ratio_max, rx_min, rx_max, tx_min, tx_max FROM `+table+` WHERE window_start = ? AND node_id = ?`, windowStart, nodeID).Scan(
		&aggregate.sampleCount, &cpuAvg, &aggregate.cpuMax, &memAvg, &aggregate.memRatioMax, &diskAvg, &aggregate.diskRatioMax, &aggregate.rxMin, &aggregate.rxMax, &aggregate.txMin, &aggregate.txMax)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load %s aggregate: %w", table, err)
	}
	aggregate.cpuSum = cpuAvg * float64(aggregate.sampleCount)
	aggregate.memRatioSum = memAvg * float64(aggregate.sampleCount)
	aggregate.diskRatioSum = diskAvg * float64(aggregate.sampleCount)
	return &aggregate, nil
}

func loadNetworkAggregateTx(ctx context.Context, tx *sql.Tx, table string, windowStart int64, nodeID, targetID string) (*networkAggregate, error) {
	var aggregate networkAggregate
	var latencyAvg float64
	err := tx.QueryRowContext(ctx, `SELECT total, success, failure, latency_avg, latency_max, latency_count FROM `+table+` WHERE window_start = ? AND node_id = ? AND target_id = ?`, windowStart, nodeID, targetID).Scan(
		&aggregate.total, &aggregate.success, &aggregate.failure, &latencyAvg, &aggregate.latencyMax, &aggregate.latencyCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load %s aggregate: %w", table, err)
	}
	aggregate.latencySum = int64(latencyAvg * float64(aggregate.latencyCount))
	return &aggregate, nil
}

func upsertResourceAggregateTx(ctx context.Context, tx *sql.Tx, table string, windowStart int64, nodeID string, aggregate resourceAggregate) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO `+table+` (window_start, node_id, sample_count, cpu_avg, cpu_max, mem_used_ratio_avg, mem_used_ratio_max, disk_used_ratio_avg, disk_used_ratio_max, rx_min, rx_max, tx_min, tx_max)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(window_start, node_id) DO UPDATE SET
			sample_count = excluded.sample_count,
			cpu_avg = excluded.cpu_avg,
			cpu_max = excluded.cpu_max,
			mem_used_ratio_avg = excluded.mem_used_ratio_avg,
			mem_used_ratio_max = excluded.mem_used_ratio_max,
			disk_used_ratio_avg = excluded.disk_used_ratio_avg,
			disk_used_ratio_max = excluded.disk_used_ratio_max,
			rx_min = excluded.rx_min,
			rx_max = excluded.rx_max,
			tx_min = excluded.tx_min,
			tx_max = excluded.tx_max`,
		windowStart, nodeID, aggregate.sampleCount, aggregate.cpuAvg(), aggregate.cpuMax, aggregate.memUsedRatioAvg(), aggregate.memRatioMax, aggregate.diskUsedRatioAvg(), aggregate.diskRatioMax, aggregate.rxMin, aggregate.rxMax, aggregate.txMin, aggregate.txMax)
	if err != nil {
		return fmt.Errorf("upsert %s aggregate: %w", table, err)
	}
	return nil
}

func upsertNetworkAggregateTx(ctx context.Context, tx *sql.Tx, table string, windowStart int64, nodeID, targetID string, aggregate networkAggregate) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO `+table+` (window_start, node_id, target_id, total, success, failure, latency_avg, latency_max, latency_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(window_start, node_id, target_id) DO UPDATE SET
			total = excluded.total,
			success = excluded.success,
			failure = excluded.failure,
			latency_avg = excluded.latency_avg,
			latency_max = excluded.latency_max,
			latency_count = excluded.latency_count`,
		windowStart, nodeID, targetID, aggregate.total, aggregate.success, aggregate.failure, aggregate.latencyAvg(), aggregate.latencyMax, aggregate.latencyCount)
	if err != nil {
		return fmt.Errorf("upsert %s aggregate: %w", table, err)
	}
	return nil
}

func deleteRowsByIDTx(ctx context.Context, tx *sql.Tx, table string, ids []int64) error {
	marks := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		marks[i] = "?"
		args[i] = id
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE id IN (`+strings.Join(marks, ",")+`)`, args...); err != nil {
		return fmt.Errorf("delete aggregated %s rows: %w", table, err)
	}
	return nil
}
