package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"
)

func setupTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite memory db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if err := migrate(ctx, db); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	return &Store{db: db}
}

func TestIPQAArchives_UpsertAndGetLatest(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	// Create test node
	reg, err := s.CreateRegistrationToken(ctx, time.Hour)
	if err != nil {
		t.Fatalf("create registration: %v", err)
	}
	regNode, err := s.RegisterNode(ctx, reg.Token, NodeInput{
		UUID: "550e8400-e29b-41d4-a716-446655440001",
		Name: "Test Node 1",
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("register node: %v", err)
	}
	nodeID := regNode.Node.ID

	now := time.Now().UTC()
	payload1 := []byte(`{"risk_score": 15.5, "level": "low"}`)
	rec1 := IPQAArchiveRecord{
		NodeID:          nodeID,
		ArchiveDate:     "2026-10-05",
		Family:          "IPv4",
		Payload:         payload1,
		HighestSeverity: "INFO",
		AlertCount:      1,
		CheckedAt:       now.Add(-24 * time.Hour),
		CreatedAt:       now.Add(-24 * time.Hour),
	}
	if err := s.UpsertIPQAArchive(ctx, rec1); err != nil {
		t.Fatalf("upsert archive 1: %v", err)
	}

	payload2 := []byte(`{"risk_score": 85.0, "level": "high"}`)
	rec2 := IPQAArchiveRecord{
		NodeID:          nodeID,
		ArchiveDate:     "2026-10-06",
		Family:          "IPv4",
		Payload:         payload2,
		HighestSeverity: "CRITICAL",
		AlertCount:      4,
		CheckedAt:       now,
		CreatedAt:       now,
	}
	if err := s.UpsertIPQAArchive(ctx, rec2); err != nil {
		t.Fatalf("upsert archive 2: %v", err)
	}

	latest, err := s.GetLatestIPQAArchive(ctx, nodeID, "IPv4")
	if err != nil {
		t.Fatalf("get latest archive: %v", err)
	}
	if latest.ArchiveDate != "2026-10-06" || latest.HighestSeverity != "CRITICAL" || latest.AlertCount != 4 {
		t.Fatalf("unexpected latest archive: %+v", latest)
	}
	if string(latest.Payload) != string(payload2) {
		t.Fatalf("unexpected payload: %s", string(latest.Payload))
	}
}

func TestIPQAArchives_RetentionPruning(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	reg, err := s.CreateRegistrationToken(ctx, time.Hour)
	if err != nil {
		t.Fatalf("create registration: %v", err)
	}
	regNode, err := s.RegisterNode(ctx, reg.Token, NodeInput{
		UUID: "550e8400-e29b-41d4-a716-446655440002",
		Name: "Retention Test Node",
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("register node: %v", err)
	}
	nodeID := regNode.Node.ID

	now := time.Now().UTC()

	// 1. Fresh archive (1 day old) - should be fully preserved
	_ = s.UpsertIPQAArchive(ctx, IPQAArchiveRecord{
		NodeID:          nodeID,
		ArchiveDate:     "2026-10-05",
		Family:          "IPv4",
		Payload:         []byte(`{"detailed": true, "sample": 1}`),
		HighestSeverity: "INFO",
		AlertCount:      1,
		CheckedAt:       now.AddDate(0, 0, -1),
	})

	// 2. Middle-aged archive (45 days old) - should have payload summarized
	_ = s.UpsertIPQAArchive(ctx, IPQAArchiveRecord{
		NodeID:          nodeID,
		ArchiveDate:     "2026-08-20",
		Family:          "IPv4",
		Payload:         []byte(`{"detailed": true, "raw_data": [1,2,3]}`),
		HighestSeverity: "WARNING",
		AlertCount:      5,
		CheckedAt:       now.AddDate(0, 0, -45),
	})

	// 3. Ancient archive (100 days old) - should be completely purged
	_ = s.UpsertIPQAArchive(ctx, IPQAArchiveRecord{
		NodeID:          nodeID,
		ArchiveDate:     "2026-06-25",
		Family:          "IPv4",
		Payload:         []byte(`{"detailed": true, "old": true}`),
		HighestSeverity: "CRITICAL",
		AlertCount:      10,
		CheckedAt:       now.AddDate(0, 0, -100),
	})

	// Run retention pruning
	pruned, purged, err := s.PruneIPQAArchives(ctx, now)
	if err != nil {
		t.Fatalf("prune error: %v", err)
	}
	if purged != 1 {
		t.Fatalf("expected 1 purged record (>90 days), got %d", purged)
	}
	if pruned != 1 {
		t.Fatalf("expected 1 pruned record (30-90 days), got %d", pruned)
	}

	// Verify records remaining in db
	archives, err := s.ListIPQAArchives(ctx, nodeID, 10, 0)
	if err != nil {
		t.Fatalf("list archives: %v", err)
	}
	if len(archives) != 2 {
		t.Fatalf("expected 2 archives remaining, got %d", len(archives))
	}

	// First is fresh (1 day old): detailed = true
	var freshMap map[string]any
	_ = json.Unmarshal(archives[0].Payload, &freshMap)
	if freshMap["detailed"] != true {
		t.Fatalf("expected fresh archive payload unmodified: %v", freshMap)
	}

	// Second is middle-aged (45 days old): pruned_summary = true
	var midMap map[string]any
	_ = json.Unmarshal(archives[1].Payload, &midMap)
	if midMap["pruned_summary"] != true {
		t.Fatalf("expected mid-age archive payload pruned to summary: %v", midMap)
	}
}
