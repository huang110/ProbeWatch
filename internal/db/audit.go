package db

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// AuditLogEntry represents a recorded security or administrative audit event.
type AuditLogEntry struct {
	ID           int64     `json:"id"`
	ActorID      string    `json:"actor_id"`
	ActorName    string    `json:"actor_name"`
	ActorType    string    `json:"actor_type"` // 'user' | 'token'
	Action       string    `json:"action"`     // 'auth.login', 'token.create', 'node.update', etc.
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id"`
	Detail       string    `json:"detail"`
	IPAddress    string    `json:"ip_address"`
	StatusCode   int       `json:"status_code"`
	CreatedAt    time.Time `json:"created_at"`
}

// RecordAuditLog writes an audit event into the audit_logs table.
func (s *Store) RecordAuditLog(ctx context.Context, entry AuditLogEntry) error {
	now := entry.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}

	actorType := entry.ActorType
	if actorType == "" {
		actorType = "user"
	}

	statusCode := entry.StatusCode
	if statusCode == 0 {
		statusCode = 200
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_logs (
			actor_id, actor_name, actor_type, action,
			resource_type, resource_id, detail, ip_address,
			status_code, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, entry.ActorID, entry.ActorName, actorType, entry.Action,
		entry.ResourceType, entry.ResourceID, entry.Detail, entry.IPAddress,
		statusCode, now.Unix())
	if err != nil {
		return fmt.Errorf("record audit log: %w", err)
	}

	return nil
}

// ListAuditLogs returns paginated audit log entries with optional action filter and total count.
func (s *Store) ListAuditLogs(ctx context.Context, limit, offset int, actionFilter string) ([]AuditLogEntry, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var countQuery, query string
	var args []interface{}
	actionFilter = strings.TrimSpace(actionFilter)

	if actionFilter != "" {
		countQuery = `SELECT COUNT(*) FROM audit_logs WHERE action LIKE ?`
		query = `
			SELECT id, actor_id, actor_name, actor_type, action,
			       resource_type, resource_id, detail, ip_address,
			       status_code, created_at
			FROM audit_logs
			WHERE action LIKE ?
			ORDER BY id DESC
			LIMIT ? OFFSET ?
		`
		pattern := "%" + actionFilter + "%"
		args = append(args, pattern)
	} else {
		countQuery = `SELECT COUNT(*) FROM audit_logs`
		query = `
			SELECT id, actor_id, actor_name, actor_type, action,
			       resource_type, resource_id, detail, ip_address,
			       status_code, created_at
			FROM audit_logs
			ORDER BY id DESC
			LIMIT ? OFFSET ?
		`
	}

	var total int
	if actionFilter != "" {
		if err := s.db.QueryRowContext(ctx, countQuery, "%"+actionFilter+"%").Scan(&total); err != nil {
			return nil, 0, fmt.Errorf("count audit logs: %w", err)
		}
	} else {
		if err := s.db.QueryRowContext(ctx, countQuery).Scan(&total); err != nil {
			return nil, 0, fmt.Errorf("count audit logs: %w", err)
		}
	}

	queryArgs := append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("query audit logs: %w", err)
	}
	defer rows.Close()

	var logs []AuditLogEntry
	for rows.Next() {
		var l AuditLogEntry
		var createdSec int64

		if err := rows.Scan(
			&l.ID, &l.ActorID, &l.ActorName, &l.ActorType, &l.Action,
			&l.ResourceType, &l.ResourceID, &l.Detail, &l.IPAddress,
			&l.StatusCode, &createdSec,
		); err != nil {
			return nil, 0, fmt.Errorf("scan audit log: %w", err)
		}

		l.CreatedAt = time.Unix(createdSec, 0).UTC()
		logs = append(logs, l)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate audit logs: %w", err)
	}

	return logs, total, nil
}
