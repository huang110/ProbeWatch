package db

import (
	"context"
	"testing"
	"time"
)

func TestStatusPageConfig(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	// 1. Get default config
	cfg, err := store.GetStatusPageConfig(ctx, "default")
	if err != nil {
		t.Fatalf("failed to get status page config: %v", err)
	}
	if cfg.ID != "default" {
		t.Errorf("expected id default, got %s", cfg.ID)
	}
	if cfg.ShowUptimeDays != 90 {
		t.Errorf("expected 90 days, got %d", cfg.ShowUptimeDays)
	}

	// 2. Update config
	cfg.Title = "Custom Service Status"
	cfg.Announcement = "Scheduled maintenance tonight at 23:00"
	cfg.Components = []StatusPageComponent{
		{
			ID:          "comp_api",
			Name:        "API Gateway",
			Group:       "Core Services",
			ShowLatency: true,
			Order:       1,
		},
	}
	if err := store.UpdateStatusPageConfig(ctx, cfg); err != nil {
		t.Fatalf("failed to update status page config: %v", err)
	}

	// 3. Reload config
	reloaded, err := store.GetStatusPageConfig(ctx, "default")
	if err != nil {
		t.Fatalf("failed to reload config: %v", err)
	}
	if reloaded.Title != "Custom Service Status" {
		t.Errorf("title mismatch: %s", reloaded.Title)
	}
	if reloaded.Announcement != "Scheduled maintenance tonight at 23:00" {
		t.Errorf("announcement mismatch: %s", reloaded.Announcement)
	}
	if len(reloaded.Components) != 1 || reloaded.Components[0].Name != "API Gateway" {
		t.Errorf("components mismatch: %+v", reloaded.Components)
	}
}

func TestIncidentsWorkflow(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	// 1. Create incident
	inc := &Incident{
		Title:  "API Gateway High Latency",
		Status: IncidentStatusInvestigating,
		Impact: IncidentImpactMajor,
	}
	created, err := store.CreateIncident(ctx, inc, "We are investigating elevated response times in HK region.")
	if err != nil {
		t.Fatalf("failed to create incident: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected generated incident ID")
	}
	if len(created.Updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(created.Updates))
	}

	// 2. Add update (identified)
	upd, err := store.AddIncidentUpdate(ctx, created.ID, IncidentStatusIdentified, "Root cause identified as upstream transit congestion. Rerouting traffic.")
	if err != nil {
		t.Fatalf("failed to add update: %v", err)
	}
	if upd.Status != IncidentStatusIdentified {
		t.Errorf("expected status %s, got %s", IncidentStatusIdentified, upd.Status)
	}

	// 3. Add update (resolved)
	_, err = store.AddIncidentUpdate(ctx, created.ID, IncidentStatusResolved, "Traffic rerouted successfully, latency back to normal.")
	if err != nil {
		t.Fatalf("failed to add resolve update: %v", err)
	}

	// 4. Verify incident status
	loaded, err := store.GetIncident(ctx, created.ID)
	if err != nil {
		t.Fatalf("failed to get incident: %v", err)
	}
	if loaded.Status != IncidentStatusResolved {
		t.Errorf("expected status resolved, got %s", loaded.Status)
	}
	if loaded.ResolvedAt == nil {
		t.Error("expected non-nil ResolvedAt")
	}
	if len(loaded.Updates) != 3 {
		t.Errorf("expected 3 updates, got %d", len(loaded.Updates))
	}

	// 5. List incidents
	list, total, err := store.ListIncidents(ctx, true, 10, 0)
	if err != nil {
		t.Fatalf("failed to list incidents: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Errorf("expected 1 incident, got total=%d len=%d", total, len(list))
	}

	// Filter out resolved
	activeList, activeTotal, err := store.ListIncidents(ctx, false, 10, 0)
	if err != nil {
		t.Fatalf("failed to list active incidents: %v", err)
	}
	if activeTotal != 0 || len(activeList) != 0 {
		t.Errorf("expected 0 active incidents, got %d", activeTotal)
	}

	// 6. Delete incident
	if err := store.DeleteIncident(ctx, created.ID); err != nil {
		t.Fatalf("failed to delete incident: %v", err)
	}
	_, err = store.GetIncident(ctx, created.ID)
	if err != ErrIncidentNotFound {
		t.Errorf("expected ErrIncidentNotFound, got %v", err)
	}
}

func TestCalculateComponentSLA(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	// SLA on abstract component (empty nodeID)
	buckets, u24h, u7d, u30d, u90d, err := store.CalculateComponentSLA(ctx, "", 90)
	if err != nil {
		t.Fatalf("calculate SLA failed: %v", err)
	}
	if len(buckets) != 90 {
		t.Errorf("expected 90 daily buckets, got %d", len(buckets))
	}
	if u90d != 100.0 || u30d != 100.0 || u7d != 100.0 || u24h != 100.0 {
		t.Errorf("expected 100%% uptime, got u24h=%.2f u7d=%.2f u30d=%.2f u90d=%.2f", u24h, u7d, u30d, u90d)
	}

	// Today's bucket should be present
	todayStr := time.Now().UTC().Format("2006-01-02")
	if buckets[89].Date != todayStr {
		t.Errorf("last bucket date expected %s, got %s", todayStr, buckets[89].Date)
	}
}
