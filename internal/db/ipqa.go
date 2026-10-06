package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// IPQAArchiveRecord maps to a row in the ipqa_archives table.
type IPQAArchiveRecord struct {
	ID              int64     `json:"id"`
	NodeID          string    `json:"node_id"`
	ArchiveDate     string    `json:"archive_date"`
	Family          string    `json:"family"`
	Payload         []byte    `json:"payload"`
	HighestSeverity string    `json:"highest_severity"`
	AlertCount      int       `json:"alert_count"`
	CheckedAt       time.Time `json:"checked_at"`
	CreatedAt       time.Time `json:"created_at"`
}

// UpsertIPQAArchive inserts or updates an IPQA archive snapshot for a node, date, and family.
func (s *Store) UpsertIPQAArchive(ctx context.Context, rec IPQAArchiveRecord) error {
	if rec.NodeID == "" || rec.ArchiveDate == "" || rec.Family == "" {
		return errors.New("node_id, archive_date, and family are required")
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}
	if rec.CheckedAt.IsZero() {
		rec.CheckedAt = rec.CreatedAt
	}

	query := `
INSERT INTO ipqa_archives (
    node_id, archive_date, family, payload, highest_severity, alert_count, checked_at, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(node_id, archive_date, family) DO UPDATE SET
    payload = excluded.payload,
    highest_severity = excluded.highest_severity,
    alert_count = excluded.alert_count,
    checked_at = excluded.checked_at;
`
	_, err := s.db.ExecContext(ctx, query,
		rec.NodeID,
		rec.ArchiveDate,
		rec.Family,
		rec.Payload,
		rec.HighestSeverity,
		rec.AlertCount,
		rec.CheckedAt.Unix(),
		rec.CreatedAt.Unix(),
	)
	if err != nil {
		return fmt.Errorf("upsert ipqa archive: %w", err)
	}
	return nil
}

// GetLatestIPQAArchive returns the newest archive record for a node and family.
func (s *Store) GetLatestIPQAArchive(ctx context.Context, nodeID string, family string) (IPQAArchiveRecord, error) {
	query := `
SELECT id, node_id, archive_date, family, payload, highest_severity, alert_count, checked_at, created_at
FROM ipqa_archives
WHERE node_id = ? AND family = ?
ORDER BY archive_date DESC, checked_at DESC
LIMIT 1;
`
	row := s.db.QueryRowContext(ctx, query, nodeID, family)
	var rec IPQAArchiveRecord
	var checkedSec, createdSec int64
	err := row.Scan(
		&rec.ID,
		&rec.NodeID,
		&rec.ArchiveDate,
		&rec.Family,
		&rec.Payload,
		&rec.HighestSeverity,
		&rec.AlertCount,
		&checkedSec,
		&createdSec,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return IPQAArchiveRecord{}, sql.ErrNoRows
		}
		return IPQAArchiveRecord{}, fmt.Errorf("get latest ipqa archive: %w", err)
	}
	rec.CheckedAt = time.Unix(checkedSec, 0).UTC()
	rec.CreatedAt = time.Unix(createdSec, 0).UTC()
	return rec, nil
}

// ListIPQAArchives returns historical archives for a node ordered by archive_date descending.
func (s *Store) ListIPQAArchives(ctx context.Context, nodeID string, limit, offset int) ([]IPQAArchiveRecord, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	if offset < 0 {
		offset = 0
	}

	query := `
SELECT id, node_id, archive_date, family, payload, highest_severity, alert_count, checked_at, created_at
FROM ipqa_archives
WHERE node_id = ?
ORDER BY archive_date DESC, checked_at DESC
LIMIT ? OFFSET ?;
`
	rows, err := s.db.QueryContext(ctx, query, nodeID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list ipqa archives: %w", err)
	}
	defer rows.Close()

	var records []IPQAArchiveRecord
	for rows.Next() {
		var rec IPQAArchiveRecord
		var checkedSec, createdSec int64
		if err := rows.Scan(
			&rec.ID,
			&rec.NodeID,
			&rec.ArchiveDate,
			&rec.Family,
			&rec.Payload,
			&rec.HighestSeverity,
			&rec.AlertCount,
			&checkedSec,
			&createdSec,
		); err != nil {
			return nil, fmt.Errorf("scan ipqa archive: %w", err)
		}
		rec.CheckedAt = time.Unix(checkedSec, 0).UTC()
		rec.CreatedAt = time.Unix(createdSec, 0).UTC()
		records = append(records, rec)
	}
	return records, rows.Err()
}

// PruneIPQAArchives enforces the 3-tier retention policy:
// 1. Records <= 30 days keep full archive payloads.
// 2. Records > 30 days and <= 90 days have detailed payload truncated to summary-only.
// 3. Records > 90 days are purged completely.
func (s *Store) PruneIPQAArchives(ctx context.Context, now time.Time) (summaryPruned int64, purged int64, err error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}

	purgeThreshold := now.AddDate(0, 0, -90).Unix()
	pruneThreshold := now.AddDate(0, 0, -30).Unix()

	// 1. Purge archives older than 90 days
	delRes, delErr := s.db.ExecContext(ctx, `DELETE FROM ipqa_archives WHERE checked_at < ?;`, purgeThreshold)
	if delErr != nil {
		return 0, 0, fmt.Errorf("purge old ipqa archives: %w", delErr)
	}
	if rowsAffected, aErr := delRes.RowsAffected(); aErr == nil {
		purged = rowsAffected
	}

	// 2. Truncate payload for archives between 30 and 90 days old if not already summary-only
	// Find candidates
	selectQuery := `
SELECT id, highest_severity, alert_count, payload
FROM ipqa_archives
WHERE checked_at < ? AND checked_at >= ?;
`
	rows, qErr := s.db.QueryContext(ctx, selectQuery, pruneThreshold, purgeThreshold)
	if qErr != nil {
		return 0, purged, fmt.Errorf("select candidates for ipqa archive pruning: %w", qErr)
	}
	defer rows.Close()

	type toUpdate struct {
		id      int64
		payload []byte
	}
	var updates []toUpdate

	for rows.Next() {
		var id int64
		var highestSev string
		var alertCnt int
		var payload []byte
		if err := rows.Scan(&id, &highestSev, &alertCnt, &payload); err != nil {
			continue
		}

		// Check if payload is already summarized
		var probe map[string]any
		if json.Unmarshal(payload, &probe) == nil {
			if isSum, ok := probe["pruned_summary"].(bool); ok && isSum {
				continue
			}
		}

		summaryPayload, _ := json.Marshal(map[string]any{
			"pruned_summary":   true,
			"highest_severity": highestSev,
			"alert_count":      alertCnt,
			"pruned_at":        now.Unix(),
		})
		updates = append(updates, toUpdate{id: id, payload: summaryPayload})
	}

	for _, u := range updates {
		if _, err := s.db.ExecContext(ctx, `UPDATE ipqa_archives SET payload = ? WHERE id = ?;`, u.payload, u.id); err == nil {
			summaryPruned++
		}
	}

	slog.Info("ipqa retention policy executed",
		"pruned_summary_count", summaryPruned,
		"purged_count", purged,
		"now", now.Format(time.RFC3339),
	)

	return summaryPruned, purged, nil
}
