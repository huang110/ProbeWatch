package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrSilenceNotFound = errors.New("alert silence not found")
	ErrInvalidSilence  = errors.New("invalid alert silence")
)

// AlertSilence represents an active or planned alert maintenance/snooze window.
type AlertSilence struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	NodeFilter  string    `json:"node_filter"` // * or comma-separated node IDs
	Category    string    `json:"category"`    // * or specific category
	RuleID      string    `json:"rule_id"`     // empty or specific rule id
	Fingerprint string    `json:"fingerprint"` // empty or specific alert fingerprint
	StartsAt    time.Time `json:"starts_at"`
	EndsAt      time.Time `json:"ends_at"`
	Reason      string    `json:"reason"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
}

func (s *Store) CreateAlertSilence(ctx context.Context, silence AlertSilence) error {
	silence.ID = strings.TrimSpace(silence.ID)
	if silence.ID == "" {
		id, err := randomID()
		if err != nil {
			return err
		}
		silence.ID = "silence-" + id
	}
	silence.Name = strings.TrimSpace(silence.Name)
	if silence.Name == "" {
		silence.Name = "Snooze " + silence.ID
	}
	silence.NodeFilter = strings.TrimSpace(silence.NodeFilter)
	if silence.NodeFilter == "" {
		silence.NodeFilter = "*"
	}
	silence.Category = strings.TrimSpace(silence.Category)
	if silence.Category == "" {
		silence.Category = "*"
	}
	silence.RuleID = strings.TrimSpace(silence.RuleID)
	silence.Fingerprint = strings.TrimSpace(silence.Fingerprint)
	silence.Reason = strings.TrimSpace(silence.Reason)
	silence.CreatedBy = strings.TrimSpace(silence.CreatedBy)

	if silence.StartsAt.IsZero() {
		silence.StartsAt = time.Now().UTC()
	}
	if silence.EndsAt.IsZero() || !silence.EndsAt.After(silence.StartsAt) {
		silence.EndsAt = silence.StartsAt.Add(1 * time.Hour)
	}
	if silence.CreatedAt.IsZero() {
		silence.CreatedAt = time.Now().UTC()
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO alert_silences (
			id, name, node_filter, category, rule_id, fingerprint, starts_at, ends_at, reason, created_by, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, silence.ID, silence.Name, silence.NodeFilter, silence.Category, silence.RuleID, silence.Fingerprint,
		unixNano(silence.StartsAt), unixNano(silence.EndsAt), silence.Reason, silence.CreatedBy, unixNano(silence.CreatedAt))
	if err != nil {
		return fmt.Errorf("create alert silence: %w", err)
	}
	return nil
}

func (s *Store) ListAlertSilences(ctx context.Context) ([]AlertSilence, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, node_filter, category, rule_id, fingerprint, starts_at, ends_at, reason, created_by, created_at
		FROM alert_silences
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list alert silences: %w", err)
	}
	defer rows.Close()

	var silences []AlertSilence
	for rows.Next() {
		var (
			item   AlertSilence
			starts int64
			ends   int64
			create int64
		)
		if err := rows.Scan(
			&item.ID, &item.Name, &item.NodeFilter, &item.Category, &item.RuleID, &item.Fingerprint,
			&starts, &ends, &item.Reason, &item.CreatedBy, &create,
		); err != nil {
			return nil, fmt.Errorf("scan alert silence: %w", err)
		}
		item.StartsAt = time.Unix(0, starts).UTC()
		item.EndsAt = time.Unix(0, ends).UTC()
		item.CreatedAt = time.Unix(0, create).UTC()
		silences = append(silences, item)
	}
	if silences == nil {
		silences = []AlertSilence{}
	}
	return silences, rows.Err()
}

func (s *Store) DeleteAlertSilence(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM alert_silences WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete alert silence: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrSilenceNotFound
	}
	return nil
}

// IsAlertSilenced checks if a given alert event is silenced at the specified timestamp.
func (s *Store) IsAlertSilenced(ctx context.Context, nodeID, category, targetID, fingerprint string, at time.Time) (bool, string, error) {
	atNano := unixNano(at)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, node_filter, category, rule_id, fingerprint, reason
		FROM alert_silences
		WHERE starts_at <= ? AND ends_at >= ?
	`, atNano, atNano)
	if err != nil {
		return false, "", fmt.Errorf("query active silences: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			silenceID, name, nodeFilter, cat, ruleID, fp, reason string
		)
		if err := rows.Scan(&silenceID, &name, &nodeFilter, &cat, &ruleID, &fp, &reason); err != nil {
			continue
		}

		label := strings.TrimSpace(reason)
		if label == "" {
			label = name
		}

		// 1. Direct fingerprint match
		if fp != "" && fp == fingerprint {
			return true, label, nil
		}

		// 2. RuleID match
		if ruleID != "" && (ruleID == targetID || strings.Contains(targetID, ruleID)) {
			return true, label, nil
		}

		// 3. Node & Category wildcard/filter match
		if isRuleApplicableToNode(nodeFilter, nodeID) {
			if cat == "*" || cat == "" || strings.EqualFold(cat, category) {
				if fp == "" && ruleID == "" {
					return true, label, nil
				}
			}
		}
	}

	return false, "", rows.Err()
}
