package db

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/protocol"
)

func TestComputeBillingCycle(t *testing.T) {
	// Mid-month: day 15, resetDay 20 -> cycle started previous month day 20, ends this month day 20
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	start, end, next, daysUntil := ComputeBillingCycle(now, 20)
	if start.Year() != 2026 || start.Month() != 8 || start.Day() != 20 {
		t.Fatalf("start = %v, want 2026-08-20", start)
	}
	if end.Year() != 2026 || end.Month() != 9 || end.Day() != 20 {
		t.Fatalf("end = %v, want 2026-09-20", end)
	}
	if next != end {
		t.Fatalf("next = %v, want %v", next, end)
	}
	if daysUntil != 5 {
		t.Fatalf("daysUntil = %d, want 5", daysUntil)
	}

	// On reset day: day 20, resetDay 20 -> cycle starts today, ends next month day 20
	now2 := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	start2, end2, _, _ := ComputeBillingCycle(now2, 20)
	if start2.Year() != 2026 || start2.Month() != 9 || start2.Day() != 20 {
		t.Fatalf("start2 = %v, want 2026-09-20", start2)
	}
	if end2.Year() != 2026 || end2.Month() != 10 || end2.Day() != 20 {
		t.Fatalf("end2 = %v, want 2026-10-20", end2)
	}
}

func TestNodeBillingCRUDAndCycleTraffic(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir+"/test.db", []byte("test-pepper-12345678901234567890"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	nodeID := "traffic-node-1"
	if _, err := store.db.Exec(`INSERT INTO nodes (id, uuid, name, status, created_at, updated_at) VALUES (?, '550e8400-e29b-41d4-a716-446655440099', 'Traffic Node', 'online', 1, 1)`, nodeID); err != nil {
		t.Fatal(err)
	}

	// 1. Default settings for unconfigured node
	settings, err := store.GetNodeBillingSettings(ctx, nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if settings.ResetDay != 1 || settings.AccountingMethod != "total" {
		t.Fatalf("unexpected default settings: %+v", settings)
	}

	// 2. Upsert custom settings
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	custom := NodeBillingSettings{
		NodeID:             nodeID,
		ResetDay:           20,
		ResetTime:          "00:00:00",
		TrafficQuotaBytes:  500 * 1024 * 1024 * 1024,
		AccountingMethod:   "max",
		IncludedInterfaces: "eth0",
		Merchant:           "BandwagonHost",
		Price:              49.99,
		Currency:           "USD",
		Cycle:              "annual",
		AutoRenew:          true,
	}
	if err := store.UpsertNodeBillingSettings(ctx, custom); err != nil {
		t.Fatalf("upsert settings: %v", err)
	}

	saved, err := store.GetNodeBillingSettings(ctx, nodeID)
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	if saved.AccountingMethod != "max" || saved.Merchant != "BandwagonHost" || saved.ResetDay != 20 {
		t.Fatalf("saved settings mismatch: %+v", saved)
	}

	// 3. Populate resource latest with multi-interface snapshot
	snapshot := protocol.ResourceSnapshot{
		NetworkRxBytes: 100 * 1024 * 1024,
		NetworkTxBytes: 250 * 1024 * 1024,
		Interfaces: []protocol.InterfaceStat{
			{Name: "eth0", RxBytes: 80 * 1024 * 1024, TxBytes: 200 * 1024 * 1024, IPv4: "1.2.3.4"},
			{Name: "eth1", RxBytes: 20 * 1024 * 1024, TxBytes: 50 * 1024 * 1024, IPv4: "10.0.0.1"},
		},
	}
	payload, _ := json.Marshal(snapshot)
	if err := store.UpsertResourceLatest(ctx, nodeID, now, payload); err != nil {
		t.Fatalf("upsert resource latest: %v", err)
	}

	// 4. Reset billing cycle at start of current cycle (2026-09-20)
	cycleStart := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	if err := store.ResetNodeBillingCycle(ctx, nodeID, cycleStart); err != nil {
		t.Fatalf("reset cycle: %v", err)
	}

	// 5. Update resource latest with new traffic
	newSnapshot := protocol.ResourceSnapshot{
		NetworkRxBytes: 150 * 1024 * 1024,
		NetworkTxBytes: 350 * 1024 * 1024,
		Interfaces: []protocol.InterfaceStat{
			{Name: "eth0", RxBytes: 120 * 1024 * 1024, TxBytes: 280 * 1024 * 1024, IPv4: "1.2.3.4"},
			{Name: "eth1", RxBytes: 30 * 1024 * 1024, TxBytes: 70 * 1024 * 1024, IPv4: "10.0.0.1"},
		},
	}
	newPayload, _ := json.Marshal(newSnapshot)
	if err := store.UpsertResourceLatest(ctx, nodeID, now.Add(time.Hour), newPayload); err != nil {
		t.Fatalf("upsert resource latest 2: %v", err)
	}

	// 6. Query cycle traffic info
	info, err := store.GetNodeCycleTraffic(ctx, nodeID, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("get cycle traffic: %v", err)
	}

	// Interface eth0: Rx went from 80MB to 120MB (delta 40MB), Tx went from 200MB to 280MB (delta 80MB)
	// Method is "max", so cycleUsed should be max(40MB, 80MB) = 80MB = 83886080 bytes
	expectedTx := uint64(80 * 1024 * 1024)
	if info.CycleTxBytes != expectedTx {
		t.Fatalf("CycleTxBytes = %d, want %d", info.CycleTxBytes, expectedTx)
	}
	if info.CycleUsedBytes != expectedTx {
		t.Fatalf("CycleUsedBytes = %d, want %d", info.CycleUsedBytes, expectedTx)
	}
	if info.TotalQuotaBytes != 500*1024*1024*1024 {
		t.Fatalf("TotalQuotaBytes = %d", info.TotalQuotaBytes)
	}
}
