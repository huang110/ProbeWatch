package db

import (
	"context"
	"testing"
	"time"
)

func TestAuditLogRecordingAndQuery(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	ctx := context.Background()
	now := time.Now().UTC()

	// 1. Record several audit events
	events := []AuditLogEntry{
		{
			ActorID:      "user-admin",
			ActorName:    "admin",
			ActorType:    "user",
			Action:       "auth.login",
			ResourceType: "session",
			ResourceID:   "sess-1",
			Detail:       "User logged in from web console",
			IPAddress:    "127.0.0.1",
			StatusCode:   200,
			CreatedAt:    now.Add(-2 * time.Minute),
		},
		{
			ActorID:      "user-admin",
			ActorName:    "admin",
			ActorType:    "user",
			Action:       "token.create",
			ResourceType: "token",
			ResourceID:   "tok-1",
			Detail:       "Created API token CI-Token",
			IPAddress:    "127.0.0.1",
			StatusCode:   201,
			CreatedAt:    now.Add(-1 * time.Minute),
		},
		{
			ActorID:      "tok-1",
			ActorName:    "CI-Token",
			ActorType:    "token",
			Action:       "node.update",
			ResourceType: "node",
			ResourceID:   "node-uuid-1",
			Detail:       "Node name updated to Prod-Tokyo",
			IPAddress:    "1.2.3.4",
			StatusCode:   200,
			CreatedAt:    now,
		},
	}

	for _, e := range events {
		if err := store.RecordAuditLog(ctx, e); err != nil {
			t.Fatalf("record audit log: %v", err)
		}
	}

	// 2. Query all logs
	logs, total, err := store.ListAuditLogs(ctx, 10, 0, "")
	if err != nil {
		t.Fatalf("list audit logs: %v", err)
	}
	if total != 3 || len(logs) != 3 {
		t.Fatalf("expected 3 logs, got total=%d, len=%d", total, len(logs))
	}

	// Should be ordered by ID desc (newest first)
	if logs[0].Action != "node.update" {
		t.Fatalf("expected first log to be newest (node.update), got %s", logs[0].Action)
	}

	// 3. Query with filter
	tokenLogs, tokenTotal, err := store.ListAuditLogs(ctx, 10, 0, "token")
	if err != nil {
		t.Fatalf("list filtered audit logs: %v", err)
	}
	if tokenTotal != 1 || len(tokenLogs) != 1 {
		t.Fatalf("expected 1 token log, got total=%d, len=%d", tokenTotal, len(tokenLogs))
	}
	if tokenLogs[0].Action != "token.create" {
		t.Fatalf("expected action token.create, got %s", tokenLogs[0].Action)
	}
}
