package db

import (
	"context"
	"testing"
	"time"
)

func TestAlertSilences(t *testing.T) {
	ctx := context.Background()
	store := newTestAlertsStore(t)
	defer store.Close()

	now := time.Now().UTC()

	// 1. Initial list empty
	list, err := store.ListAlertSilences(ctx)
	if err != nil {
		t.Fatalf("ListAlertSilences: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 silences, got %d", len(list))
	}

	// 2. Create silence for a specific node and category
	s1 := AlertSilence{
		ID:         "silence-node-1",
		Name:       "Node 1 Maintenance",
		NodeFilter: "node-1,node-2",
		Category:   "resource",
		StartsAt:   now.Add(-10 * time.Minute),
		EndsAt:     now.Add(50 * time.Minute),
		Reason:     "Kernel upgrade in progress",
		CreatedBy:  "admin",
	}
	if err := store.CreateAlertSilence(ctx, s1); err != nil {
		t.Fatalf("CreateAlertSilence: %v", err)
	}

	// 3. Create silence for a specific fingerprint
	s2 := AlertSilence{
		ID:          "silence-fp-1",
		Name:        "Snooze High Memory",
		NodeFilter:  "*",
		Category:    "*",
		Fingerprint: "fp-memory-high-1234",
		StartsAt:    now.Add(-5 * time.Minute),
		EndsAt:      now.Add(25 * time.Minute),
		Reason:      "Investigating memory leak",
		CreatedBy:   "operator",
	}
	if err := store.CreateAlertSilence(ctx, s2); err != nil {
		t.Fatalf("CreateAlertSilence: %v", err)
	}

	// 4. Verify list
	list, err = store.ListAlertSilences(ctx)
	if err != nil {
		t.Fatalf("ListAlertSilences: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 silences, got %d", len(list))
	}

	// 5. Test IsAlertSilenced
	// 5a. node-1 resource alert -> silenced by s1
	silenced, reason, err := store.IsAlertSilenced(ctx, "node-1", "resource", "cpu", "some-fp", now)
	if err != nil {
		t.Fatalf("IsAlertSilenced: %v", err)
	}
	if !silenced || reason != "Kernel upgrade in progress" {
		t.Fatalf("expected node-1 resource to be silenced, got silenced=%v, reason=%q", silenced, reason)
	}

	// 5b. node-1 network alert -> not silenced because category is resource
	silenced, _, _ = store.IsAlertSilenced(ctx, "node-1", "network", "ping", "some-fp", now)
	if silenced {
		t.Fatalf("expected node-1 network to NOT be silenced")
	}

	// 5c. node-3 resource alert -> not silenced because node-3 not in node-1,node-2
	silenced, _, _ = store.IsAlertSilenced(ctx, "node-3", "resource", "cpu", "some-fp", now)
	if silenced {
		t.Fatalf("expected node-3 resource to NOT be silenced")
	}

	// 5d. Any node with fingerprint "fp-memory-high-1234" -> silenced by s2
	silenced, reason, _ = store.IsAlertSilenced(ctx, "node-99", "resource", "memory", "fp-memory-high-1234", now)
	if !silenced || reason != "Investigating memory leak" {
		t.Fatalf("expected fingerprint match to be silenced, got silenced=%v, reason=%q", silenced, reason)
	}

	// 5e. Expired time check -> not silenced
	future := now.Add(2 * time.Hour)
	silenced, _, _ = store.IsAlertSilenced(ctx, "node-1", "resource", "cpu", "some-fp", future)
	if silenced {
		t.Fatalf("expected expired silence to not match")
	}

	// 6. Delete silence
	if err := store.DeleteAlertSilence(ctx, "silence-node-1"); err != nil {
		t.Fatalf("DeleteAlertSilence: %v", err)
	}
	silenced, _, _ = store.IsAlertSilenced(ctx, "node-1", "resource", "cpu", "some-fp", now)
	if silenced {
		t.Fatalf("expected silence to be removed")
	}
}
