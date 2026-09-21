package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Check reliability statistics
// ---------------------------------------------------------------------------

// CheckSummary is the per-target reliability statistic for one node over a
// time window. Statistics come from network_results_history (raw samples
// inside the retention window) and network_results_history_hourly (windows
// already folded into aggregates); network_results_latest supplies
// LastCheckedAt. The daily aggregate table covers exactly the same folded
// rows as the hourly table, so only the hourly table is read — reading both
// would double count, and hourly is the finer of the two.
type CheckSummary struct {
	TargetID string
	Name     string
	Kind     string
	Host     string
	// HasWindowData is false when the window itself contains no check data
	// for the target; API responses report all statistics as null in that
	// case and keep only LastCheckedAt.
	HasWindowData bool
	Total         int64
	Success       int64
	Failure       int64
	// LatencyAvgMS is the mean latency_ms over samples with a positive
	// latency; it is only meaningful when LatencyCount > 0.
	LatencyAvgMS float64
	LatencyCount int64
	// JitterMS is the max−min spread, in milliseconds, of the latency
	// observations inside the window. Max−min is chosen over standard
	// deviation deliberately: the hourly/daily aggregates retain per-window
	// latency_avg and latency_max but neither a sum of squares nor a minimum,
	// so a stddev could not be computed consistently across raw and
	// aggregated history. Raw samples contribute their exact latency_ms;
	// each aggregated window contributes its latency_max as the upper bound
	// and its latency_avg as a lower-bound stand-in for the unstored window
	// minimum. JitterMS is therefore a conservative lower bound of the true
	// max−min spread whenever the window reaches into aggregated history.
	//
	// It is only meaningful when HasJitter is true (at least one latency
	// observation inside the window).
	JitterMS  float64
	HasJitter bool
	// LastCheckedAt is the time of the most recent check for the target:
	// network_results_latest.checked_at when present, otherwise the newest
	// raw checked_at inside the window, otherwise the zero time (unknown).
	LastCheckedAt time.Time
}

// checkAccumulator collects per-target statistics across raw samples and
// aggregate rows without double counting: every check lands either in a raw
// row or in exactly one hourly window, never both.
type checkAccumulator struct {
	hasWindowData bool
	total         int64
	success       int64
	failure       int64
	latencySumMS  float64
	latencyCount  int64
	latencyMinMS  float64
	latencyMaxMS  float64
	jitterSeen    bool
	rawCheckedAt  time.Time
}

func (a *checkAccumulator) addRawSample(sample networkSample) {
	a.hasWindowData = true
	a.total++
	if sample.status == "success" {
		a.success++
	} else {
		a.failure++
	}
	if sample.latency > 0 {
		a.latencySumMS += float64(sample.latency)
		a.latencyCount++
		a.observeLatency(float64(sample.latency), float64(sample.latency))
	}
}

func (a *checkAccumulator) addHourlyRow(total, success, failure, latencyCount int64, latencyAvg float64, latencyMax int64) {
	a.hasWindowData = true
	a.total += total
	a.success += success
	a.failure += failure
	if latencyCount > 0 {
		a.latencySumMS += latencyAvg * float64(latencyCount)
		a.latencyCount += latencyCount
		// The aggregate retains latency_max but not a minimum; latency_avg
		// stands in as the lower bound (see CheckSummary.JitterMS).
		if latencyMax > 0 {
			a.observeLatency(latencyAvg, float64(latencyMax))
		}
	}
}

func (a *checkAccumulator) observeLatency(minMS, maxMS float64) {
	a.jitterSeen = true
	// Latency observations are always positive, so a zero minimum means
	// "unset".
	if a.latencyMinMS == 0 || minMS < a.latencyMinMS {
		a.latencyMinMS = minMS
	}
	if maxMS > a.latencyMaxMS {
		a.latencyMaxMS = maxMS
	}
}

// GetCheckSummary returns per-target check reliability statistics for the
// node over [from, to]. Targets that have a network_results_latest row but
// no data inside the window are included with HasWindowData=false so callers
// can still show last_checked_at.
func (s *Store) GetCheckSummary(ctx context.Context, nodeID string, from, to time.Time) ([]CheckSummary, error) {
	if from.IsZero() || to.IsZero() || from.After(to) {
		return nil, errors.New("check summary window is invalid")
	}
	accumulators := make(map[string]*checkAccumulator)

	// Raw samples. AggregateHistory keeps the raw table bounded (rows older
	// than the retention boundary are folded into the aggregates), so this
	// scan is bounded in practice by the retention window.
	rows, err := s.db.QueryContext(ctx, `SELECT target_id, checked_at, payload FROM network_results_history WHERE node_id = ? AND checked_at >= ? AND checked_at <= ? ORDER BY target_id, checked_at, id`, nodeID, unixNano(from), unixNano(to))
	if err != nil {
		return nil, fmt.Errorf("query check summary history: %w", err)
	}
	for rows.Next() {
		var targetID string
		var checkedAt int64
		var payload []byte
		if err := rows.Scan(&targetID, &checkedAt, &payload); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan check summary history: %w", err)
		}
		sample, err := decodeNetworkSample(payload)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("decode check summary history row: %w", err)
		}
		accumulator := accumulators[targetID]
		if accumulator == nil {
			accumulator = &checkAccumulator{}
			accumulators[targetID] = accumulator
		}
		accumulator.addRawSample(sample)
		if checked := time.Unix(0, checkedAt).UTC(); checked.After(accumulator.rawCheckedAt) {
			accumulator.rawCheckedAt = checked
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("read check summary history: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close check summary history: %w", err)
	}

	// Hourly aggregate windows whose full hour lies inside [from, to]. A
	// partially folded hour contributes its folded half here and its raw
	// half above; the two halves never overlap, so sums stay exact.
	hourRows, err := s.db.QueryContext(ctx, `SELECT target_id, total, success, failure, latency_avg, latency_max, latency_count FROM network_results_history_hourly WHERE node_id = ? AND window_start >= ? AND window_start <= ? ORDER BY target_id, window_start`, nodeID, unixNano(from), unixNano(to.Add(-time.Hour)))
	if err != nil {
		return nil, fmt.Errorf("query check summary aggregates: %w", err)
	}
	for hourRows.Next() {
		var targetID string
		var total, success, failure, latencyCount int64
		var latencyAvg float64
		var latencyMax int64
		if err := hourRows.Scan(&targetID, &total, &success, &failure, &latencyAvg, &latencyMax, &latencyCount); err != nil {
			hourRows.Close()
			return nil, fmt.Errorf("scan check summary aggregates: %w", err)
		}
		accumulator := accumulators[targetID]
		if accumulator == nil {
			accumulator = &checkAccumulator{}
			accumulators[targetID] = accumulator
		}
		accumulator.addHourlyRow(total, success, failure, latencyCount, latencyAvg, latencyMax)
	}
	if err := hourRows.Err(); err != nil {
		hourRows.Close()
		return nil, fmt.Errorf("read check summary aggregates: %w", err)
	}
	if err := hourRows.Close(); err != nil {
		return nil, fmt.Errorf("close check summary aggregates: %w", err)
	}

	// network_results_latest: last_checked_at, plus targets whose latest
	// check predates the window entirely.
	latestRows, err := s.db.QueryContext(ctx, `SELECT target_id, checked_at FROM network_results_latest WHERE node_id = ?`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("query check summary latest: %w", err)
	}
	latestChecked := make(map[string]time.Time)
	for latestRows.Next() {
		var targetID string
		var checkedAt int64
		if err := latestRows.Scan(&targetID, &checkedAt); err != nil {
			latestRows.Close()
			return nil, fmt.Errorf("scan check summary latest: %w", err)
		}
		latestChecked[targetID] = time.Unix(0, checkedAt).UTC()
	}
	if err := latestRows.Err(); err != nil {
		latestRows.Close()
		return nil, fmt.Errorf("read check summary latest: %w", err)
	}
	if err := latestRows.Close(); err != nil {
		return nil, fmt.Errorf("close check summary latest: %w", err)
	}
	for targetID := range latestChecked {
		if _, exists := accumulators[targetID]; !exists {
			accumulators[targetID] = &checkAccumulator{}
		}
	}

	targetIDs := make([]string, 0, len(accumulators))
	for targetID := range accumulators {
		targetIDs = append(targetIDs, targetID)
	}
	sort.Strings(targetIDs)
	metadata, err := s.lookupNetworkTargetMetadata(ctx, targetIDs)
	if err != nil {
		return nil, err
	}

	summaries := make([]CheckSummary, 0, len(accumulators))
	for _, targetID := range targetIDs {
		accumulator := accumulators[targetID]
		summary := CheckSummary{
			TargetID:      targetID,
			Name:          metadata[targetID].name,
			Kind:          metadata[targetID].kind,
			Host:          metadata[targetID].host,
			HasWindowData: accumulator.hasWindowData,
			Total:         accumulator.total,
			Success:       accumulator.success,
			Failure:       accumulator.failure,
			LastCheckedAt: accumulator.rawCheckedAt,
		}
		if checked, exists := latestChecked[targetID]; exists && checked.After(summary.LastCheckedAt) {
			summary.LastCheckedAt = checked
		}
		if accumulator.latencyCount > 0 {
			summary.LatencyAvgMS = accumulator.latencySumMS / float64(accumulator.latencyCount)
			summary.LatencyCount = accumulator.latencyCount
		}
		if accumulator.jitterSeen {
			summary.JitterMS = accumulator.latencyMaxMS - accumulator.latencyMinMS
			if summary.JitterMS < 0 {
				summary.JitterMS = 0
			}
			summary.HasJitter = true
		}
		summaries = append(summaries, summary)
	}
	return summaries, nil
}

type targetMetadata struct {
	name string
	kind string
	host string
}

// lookupNetworkTargetMetadata resolves display metadata for target IDs. IDs
// come from the database itself and are bound as parameters, never
// interpolated into the statement.
func (s *Store) lookupNetworkTargetMetadata(ctx context.Context, targetIDs []string) (map[string]targetMetadata, error) {
	metadata := make(map[string]targetMetadata, len(targetIDs))
	if len(targetIDs) == 0 {
		return metadata, nil
	}
	marks := make([]string, len(targetIDs))
	args := make([]any, 0, len(targetIDs))
	for i, id := range targetIDs {
		marks[i] = "?"
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, kind, host FROM network_targets WHERE id IN (`+strings.Join(marks, ",")+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("query check summary targets: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, name, kind, host string
		if err := rows.Scan(&id, &name, &kind, &host); err != nil {
			return nil, fmt.Errorf("scan check summary targets: %w", err)
		}
		metadata[id] = targetMetadata{name: name, kind: kind, host: host}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read check summary targets: %w", err)
	}
	return metadata, nil
}

// ---------------------------------------------------------------------------
// Traffic report
// ---------------------------------------------------------------------------

// maxTrafficBuckets bounds the number of series buckets a single report may
// produce, keeping the computation proportional to the requested resolution.
const maxTrafficBuckets = 1024

// TrafficBucket is one series slice of a traffic report. HasData is false
// when no counter observation falls into the bucket; API responses report
// the bucket values as null in that case.
type TrafficBucket struct {
	Start   time.Time
	RxBytes int64
	TxBytes int64
	HasData bool
}

// TrafficReport is the rx/tx traffic increment for one node over a window.
type TrafficReport struct {
	From time.Time
	To   time.Time
	Step time.Duration
	// RxBytes/TxBytes are the summed positive counter deltas inside the
	// window; only meaningful when HasData is true.
	RxBytes int64
	TxBytes int64
	// RxResets/TxResets count counter drops (host reboot or agent restart)
	// between consecutive observations; the affected segment is skipped.
	RxResets int
	TxResets int
	// HasData is false when the window contains no counter observations.
	HasData bool
	Buckets []TrafficBucket
}

// GetTrafficReport computes rx/tx byte increments for [from, to] bucketed by
// step, from resource_history counter samples plus resource_history_hourly
// rx_min/rx_max for hours already folded into aggregates.
// resource_history_daily covers exactly the same folded rows as the hourly
// table, so only the hourly table is read (reading both would double count,
// and hourly is the finer of the two).
//
// Counter semantics: network_rx_bytes/network_tx_bytes are host boot-time
// counters. Each hourly aggregate is treated as a linear counter from
// (window_start, rx_min) to (window_start+1h, rx_max) — exact while the
// counter is monotonic inside the hour, which is the common case. A drop
// between two consecutive observations is a reset: that segment contributes
// no delta (a negative delta is never added) and is counted in RxResets or
// TxResets. The first observation of the window only establishes the
// baseline, matching "last counter minus first counter".
func (s *Store) GetTrafficReport(ctx context.Context, nodeID string, from, to time.Time, step time.Duration) (TrafficReport, error) {
	report := TrafficReport{From: from.UTC(), To: to.UTC(), Step: step}
	if from.IsZero() || to.IsZero() || from.After(to) {
		return report, errors.New("traffic window is invalid")
	}
	if step <= 0 {
		return report, errors.New("traffic step must be positive")
	}
	bucketCount := int(to.Sub(from)/step) + 1
	if bucketCount > maxTrafficBuckets {
		return report, fmt.Errorf("traffic window produces %d buckets, exceeding %d", bucketCount, maxTrafficBuckets)
	}
	report.Buckets = make([]TrafficBucket, bucketCount)
	for i := range report.Buckets {
		report.Buckets[i].Start = from.Add(time.Duration(i) * step).UTC()
	}
	bucketIndex := func(at time.Time) int {
		index := int(at.Sub(report.From) / step)
		if index >= bucketCount {
			index = bucketCount - 1
		}
		if index < 0 {
			index = 0
		}
		return index
	}

	type counterPoint struct {
		at     time.Time
		rx, tx int64
	}
	points := make([]counterPoint, 0, 256)

	rows, err := s.db.QueryContext(ctx, `SELECT reported_at, payload FROM resource_history WHERE node_id = ? AND reported_at >= ? AND reported_at <= ? ORDER BY reported_at, id`, nodeID, unixNano(from), unixNano(to))
	if err != nil {
		return report, fmt.Errorf("query traffic history: %w", err)
	}
	for rows.Next() {
		var reportedAt int64
		var payload []byte
		if err := rows.Scan(&reportedAt, &payload); err != nil {
			rows.Close()
			return report, fmt.Errorf("scan traffic history: %w", err)
		}
		var snapshot struct {
			NetworkRxBytes uint64 `json:"network_rx_bytes"`
			NetworkTxBytes uint64 `json:"network_tx_bytes"`
		}
		if err := json.Unmarshal(payload, &snapshot); err != nil {
			rows.Close()
			return report, fmt.Errorf("decode traffic history row: %w", err)
		}
		points = append(points, counterPoint{at: time.Unix(0, reportedAt).UTC(), rx: int64(snapshot.NetworkRxBytes), tx: int64(snapshot.NetworkTxBytes)})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return report, fmt.Errorf("read traffic history: %w", err)
	}
	if err := rows.Close(); err != nil {
		return report, fmt.Errorf("close traffic history: %w", err)
	}

	// Hourly aggregate windows whose full hour lies inside [from, to],
	// expanded into (start, min) and (end, max) counter points. A partially
	// folded hour contributes its folded half here and its raw half above;
	// the halves never overlap, so deltas stay exact.
	hourRows, err := s.db.QueryContext(ctx, `SELECT window_start, rx_min, rx_max, tx_min, tx_max FROM resource_history_hourly WHERE node_id = ? AND window_start >= ? AND window_start <= ? ORDER BY window_start`, nodeID, unixNano(from), unixNano(to.Add(-time.Hour)))
	if err != nil {
		return report, fmt.Errorf("query traffic aggregates: %w", err)
	}
	for hourRows.Next() {
		var windowStart int64
		var rxMin, rxMax, txMin, txMax int64
		if err := hourRows.Scan(&windowStart, &rxMin, &rxMax, &txMin, &txMax); err != nil {
			hourRows.Close()
			return report, fmt.Errorf("scan traffic aggregates: %w", err)
		}
		start := time.Unix(0, windowStart).UTC()
		end := start.Add(time.Hour)
		points = append(points, counterPoint{at: start, rx: rxMin, tx: txMin}, counterPoint{at: end, rx: rxMax, tx: txMax})
	}
	if err := hourRows.Err(); err != nil {
		hourRows.Close()
		return report, fmt.Errorf("read traffic aggregates: %w", err)
	}
	if err := hourRows.Close(); err != nil {
		return report, fmt.Errorf("close traffic aggregates: %w", err)
	}

	if len(points) == 0 {
		return report, nil
	}
	report.HasData = true
	sort.SliceStable(points, func(i, j int) bool { return points[i].at.Before(points[j].at) })
	for _, point := range points {
		report.Buckets[bucketIndex(point.at)].HasData = true
	}

	previous := points[0]
	for _, point := range points[1:] {
		index := bucketIndex(point.at)
		if point.rx >= previous.rx {
			delta := point.rx - previous.rx
			report.RxBytes += delta
			report.Buckets[index].RxBytes += delta
		} else {
			report.RxResets++
		}
		if point.tx >= previous.tx {
			delta := point.tx - previous.tx
			report.TxBytes += delta
			report.Buckets[index].TxBytes += delta
		} else {
			report.TxResets++
		}
		previous = point
	}
	return report, nil
}
