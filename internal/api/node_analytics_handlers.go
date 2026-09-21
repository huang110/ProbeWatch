package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/security"
)

// ---------------------------------------------------------------------------
// GET /api/nodes/{uuid}/checks/summary
// ---------------------------------------------------------------------------

// checkSummaryResponse reports per-target check reliability for the window.
// Statistics are null when the window contains no data for the target
// (HasWindowData=false in db.CheckSummary); last_checked_at stays populated
// from network_results_latest whenever the target has ever been checked.
type checkSummaryResponse struct {
	TargetID      string     `json:"target_id"`
	Name          string     `json:"name"`
	Kind          string     `json:"kind"`
	Host          string     `json:"host"`
	Total         *int64     `json:"total"`
	Success       *int64     `json:"success"`
	Failure       *int64     `json:"failure"`
	LossRate      *float64   `json:"loss_rate"`
	LatencyAvgMS  *float64   `json:"latency_avg_ms"`
	JitterMS      *float64   `json:"jitter_ms"`
	LastCheckedAt *time.Time `json:"last_checked_at"`
}

func (s *Server) nodeChecksSummary(w http.ResponseWriter, r *http.Request, uuid string) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !security.IsRFC4122UUID(uuid) {
		writeJSONError(w, http.StatusNotFound, "node not found")
		return
	}
	// The summary aggregates the whole window, so the limit accepted by
	// parseHistoryWindow is validated but intentionally not applied: applying
	// it would silently truncate the statistics.
	from, to, _, ok := parseHistoryWindow(r, time.Now().UTC())
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "invalid query parameters")
		return
	}
	node, err := s.service.Store().GetNodeByUUID(r.Context(), uuid)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "node not found")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "node unavailable")
		return
	}
	summaries, err := s.service.Store().GetCheckSummary(r.Context(), node.ID, from, to)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "checks summary unavailable")
		return
	}
	out := make([]checkSummaryResponse, 0, len(summaries))
	for _, summary := range summaries {
		item := checkSummaryResponse{TargetID: summary.TargetID, Name: summary.Name, Kind: summary.Kind, Host: summary.Host}
		if !summary.LastCheckedAt.IsZero() {
			checkedAt := summary.LastCheckedAt
			item.LastCheckedAt = &checkedAt
		}
		if summary.HasWindowData {
			total, success, failure := summary.Total, summary.Success, summary.Failure
			item.Total, item.Success, item.Failure = &total, &success, &failure
			if summary.Total > 0 {
				lossRate := float64(summary.Failure) / float64(summary.Total)
				item.LossRate = &lossRate
			}
			if summary.LatencyCount > 0 {
				latencyAvg := summary.LatencyAvgMS
				item.LatencyAvgMS = &latencyAvg
			}
			if summary.HasJitter {
				jitter := summary.JitterMS
				item.JitterMS = &jitter
			}
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, out)
}

// ---------------------------------------------------------------------------
// GET /api/nodes/{uuid}/traffic?period=day|week|month
// ---------------------------------------------------------------------------

type trafficPeriodShape struct {
	window time.Duration
	step   time.Duration
}

// trafficPeriods maps the accepted period values to window length and series
// resolution: hour buckets for a day, day buckets for a week or a month.
var trafficPeriods = map[string]trafficPeriodShape{
	"day":   {window: 24 * time.Hour, step: time.Hour},
	"week":  {window: 7 * 24 * time.Hour, step: 24 * time.Hour},
	"month": {window: 30 * 24 * time.Hour, step: 24 * time.Hour},
}

type trafficPointResponse struct {
	Time    time.Time `json:"time"`
	RxBytes *int64    `json:"rx_bytes"`
	TxBytes *int64    `json:"tx_bytes"`
}

type trafficReportResponse struct {
	Period      string                 `json:"period"`
	Window      map[string]time.Time   `json:"window"`
	RxBytes     *int64                 `json:"rx_bytes"`
	TxBytes     *int64                 `json:"tx_bytes"`
	RxResets    int                    `json:"rx_resets"`
	TxResets    int                    `json:"tx_resets"`
	IntervalSec int64                  `json:"interval_seconds"`
	Series      []trafficPointResponse `json:"series"`
}

func (s *Server) nodeTraffic(w http.ResponseWriter, r *http.Request, uuid string) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !security.IsRFC4122UUID(uuid) {
		writeJSONError(w, http.StatusNotFound, "node not found")
		return
	}
	period := strings.TrimSpace(r.URL.Query().Get("period"))
	if period == "" {
		period = "day"
	}
	shape, ok := trafficPeriods[period]
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "invalid period")
		return
	}
	node, err := s.service.Store().GetNodeByUUID(r.Context(), uuid)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "node not found")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "node unavailable")
		return
	}
	now := time.Now().UTC()
	report, err := s.service.Store().GetTrafficReport(r.Context(), node.ID, now.Add(-shape.window), now, shape.step)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "traffic report unavailable")
		return
	}
	response := trafficReportResponse{
		Period:      period,
		Window:      map[string]time.Time{"from": report.From, "to": report.To},
		RxResets:    report.RxResets,
		TxResets:    report.TxResets,
		IntervalSec: int64(shape.step / time.Second),
		Series:      make([]trafficPointResponse, 0, len(report.Buckets)),
	}
	if report.HasData {
		rx, tx := report.RxBytes, report.TxBytes
		response.RxBytes, response.TxBytes = &rx, &tx
	}
	for _, bucket := range report.Buckets {
		point := trafficPointResponse{Time: bucket.Start}
		if bucket.HasData {
			rx, tx := bucket.RxBytes, bucket.TxBytes
			point.RxBytes, point.TxBytes = &rx, &tx
		}
		response.Series = append(response.Series, point)
	}
	writeJSON(w, http.StatusOK, response)
}
