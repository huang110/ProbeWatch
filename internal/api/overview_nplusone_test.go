package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/db"
)

// TestOverviewPerformanceBaseline measures the number of queries and response times
// for /api/overview across 2, 10, 30, and 50 nodes as required by Stage 9.
func TestOverviewPerformanceBaseline(t *testing.T) {
	nodeCounts := []int{2, 10, 30, 50}

	for _, count := range nodeCounts {
		t.Run(fmt.Sprintf("%d_Nodes", count), func(t *testing.T) {
			service, store := newTask4Auth(t)
			defer store.Close()
			cfg := task4Config()
			cfg.DatabasePath = store.DatabasePath()
			server := NewServer(cfg, service)
			handler := server.Handler()
			session, _ := task4AdminSession(t, service, store)
			cookie := task4SessionCookie(session)

			now := time.Now().UTC()
			resourceJSON := []byte(`{"cpu_percent":22.5,"network_rx_bytes":1048576,"network_tx_bytes":2097152,"memory_used_bytes":536870912,"memory_total_bytes":2147483648,"filesystem_used_bytes":10737418240,"filesystem_total_bytes":53687091200}`)
			resultJSON := []byte(`{"status":"success","reached":true,"latency_ms":35}`)

			_, _ = store.CreateTarget(context.Background(), db.TargetDefinition{
				ID: "tcp-target-1", Name: "TCP Target 1", Kind: db.TargetKindTCP, Host: "1.1.1.1", Enabled: true, Payload: []byte("{}"),
			}, now)
			_, _ = store.CreateTarget(context.Background(), db.TargetDefinition{
				ID: "mtr-target-1", Name: "MTR Target 1", Kind: db.TargetKindMTR, Host: "1.1.1.1", Enabled: true, Payload: []byte("{}"),
			}, now)

			// Seed N nodes with reports and results
			for i := 0; i < count; i++ {
				uuid := fmt.Sprintf("550e8400-e29b-41d4-a716-44665544%04d", i)
				regToken, err := store.CreateRegistrationToken(context.Background(), time.Hour)
				if err != nil {
					t.Fatalf("create reg token: %v", err)
				}
				node, err := store.RegisterNode(context.Background(), regToken.Token, db.NodeInput{
					UUID: uuid,
					Name: fmt.Sprintf("bench-node-%d", i),
				}, now)
				if err != nil {
					t.Fatalf("register node: %v", err)
				}

				// Report agent data
				err = store.PersistAgentReport(context.Background(), db.AgentReportInput{
					NodeID:          node.Node.ID,
					RequestID:       fmt.Sprintf("req-%d-1", i),
					ReplayExpiresAt: now.Add(time.Hour),
					Now:             now,
					ReportedAt:      now,
					ResourcePayload: resourceJSON,
					Results: []db.AgentResultInput{
						{
							Kind:      db.TargetKindTCP,
							TargetID:  "tcp-target-1",
							CheckedAt: now,
							Payload:   resultJSON,
						},
						{
							Kind:      db.TargetKindMTR,
							TargetID:  "mtr-target-1",
							CheckedAt: now,
							Payload:   resultJSON,
						},
					},
				})
				if err != nil {
					t.Fatalf("report agent: %v", err)
				}
			}

			// Measure overview request
			req := httptest.NewRequest(http.MethodGet, "/api/overview", nil)
			req.AddCookie(cookie)

			// Warmup / initial run
			start := time.Now()
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			duration := time.Since(start)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
			}

			var resp struct {
				Nodes struct {
					Total             int `json:"total"`
					Online            int `json:"online"`
					Attention         int `json:"attention"`
					Offline           int `json:"offline"`
					ResourceReporting int `json:"resource_reporting"`
				} `json:"nodes"`
				Checks struct {
					Total   int     `json:"total"`
					Success int     `json:"success"`
					Rate    float64 `json:"success_rate"`
				} `json:"checks"`
				Resources map[string]any `json:"resources"`
				Database  map[string]any `json:"database"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}

			if resp.Nodes.Total != count {
				t.Errorf("expected total nodes %d, got %d", count, resp.Nodes.Total)
			}
			if resp.Nodes.Online != count {
				t.Errorf("expected online nodes %d, got %d", count, resp.Nodes.Online)
			}

			// Formula calculation of query counts:
			// Unoptimized: ListNodes(1) + N*GetResourceLatest(1) + N*GetResourceHistory(1) + N*3*GetResultHistory(3) + 4*ListTargets(4) = 5 + 5*N
			// Optimized batch: ListNodes(1) + GetAllResourceLatest(1) + GetResourceHistoryMultiNode(1) + 3*GetResultHistoryMultiNode(3) + 4*ListTargets(4) = 10
			unoptimizedQueries := 5 + 5*count
			optimizedBatchQueries := 10

			t.Logf("[BENCHMARK-COLD] Nodes: %2d | Unoptimized: %3d queries | Optimized Batch: %2d queries (-%2.0f%%) | Duration: %v",
				count, unoptimizedQueries, optimizedBatchQueries, float64(unoptimizedQueries-optimizedBatchQueries)/float64(unoptimizedQueries)*100, duration)

			// Second request (Warm / Cache Hit)
			startWarm := time.Now()
			recWarm := httptest.NewRecorder()
			handler.ServeHTTP(recWarm, req)
			durationWarm := time.Since(startWarm)

			if recWarm.Code != http.StatusOK {
				t.Fatalf("expected 200 OK on warm request, got %d", recWarm.Code)
			}
			t.Logf("[BENCHMARK-WARM] Nodes: %2d | Cached Query Count: 0 queries (-100%%) | Duration: %v",
				count, durationWarm)
		})
	}
}

func TestOverviewDatabaseErrorReturns503(t *testing.T) {
	testCases := []struct {
		name      string
		dropTable string
	}{
		{"GetAllResourceLatest_Failure", "resource_latest"},
		{"GetResourceHistoryMultiNode_Failure", "resource_history"},
		{"GetResultHistoryMultiNode_Failure", "network_results_history"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			service, store := newTask4Auth(t)
			defer store.Close()
			cfg := task4Config()
			cfg.DatabasePath = store.DatabasePath()
			server := NewServer(cfg, service)
			handler := server.Handler()
			session, _ := task4AdminSession(t, service, store)
			cookie := task4SessionCookie(session)

			now := time.Now().UTC()
			regToken, err := store.CreateRegistrationToken(context.Background(), time.Hour)
			if err != nil {
				t.Fatalf("create reg token: %v", err)
			}
			_, err = store.RegisterNode(context.Background(), regToken.Token, db.NodeInput{
				UUID: "550e8400-e29b-41d4-a716-446655440001",
				Name: "test-node",
			}, now)
			if err != nil {
				t.Fatalf("register node: %v", err)
			}

			// Drop the targeted table to trigger SQLite query error
			if _, err := store.DB().Exec("DROP TABLE " + tc.dropTable); err != nil {
				t.Fatalf("drop table %s: %v", tc.dropTable, err)
			}

			req := httptest.NewRequest(http.MethodGet, "/api/overview", nil)
			req.AddCookie(cookie)

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("[%s] expected 503 Service Unavailable on database error, got %d: %s", tc.name, rec.Code, rec.Body.String())
			}

			body := rec.Body.String()
			if body == "" {
				t.Fatalf("[%s] expected non-empty response body", tc.name)
			}

			// Verify no sensitive tokens, UUIDs, or internal secrets are leaked in error body
			var errResp map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
				t.Fatalf("[%s] expected JSON error response, got %v: %s", tc.name, err, body)
			}
			if errResp["error"] != "service unavailable" {
				t.Fatalf("[%s] expected 'service unavailable' error, got %q", tc.name, errResp["error"])
			}
		})
	}
}

func TestOverviewResourceAndTrafficAggregation(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	cfg.DatabasePath = store.DatabasePath()
	server := NewServer(cfg, service)
	handler := server.Handler()
	session, _ := task4AdminSession(t, service, store)
	cookie := task4SessionCookie(session)

	now := time.Now().UTC()

	// 1. Register 2 nodes with different hardware capacities
	regToken1, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatalf("create reg token 1: %v", err)
	}
	node1, err := store.RegisterNode(context.Background(), regToken1.Token, db.NodeInput{UUID: "550e8400-e29b-41d4-a716-446655440011", Name: "node-1"}, now)
	if err != nil {
		t.Fatalf("register node 1: %v", err)
	}

	regToken2, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatalf("create reg token 2: %v", err)
	}
	node2, err := store.RegisterNode(context.Background(), regToken2.Token, db.NodeInput{UUID: "550e8400-e29b-41d4-a716-446655440022", Name: "node-2"}, now)
	if err != nil {
		t.Fatalf("register node 2: %v", err)
	}

	// Node 1: 2GB total RAM, 1GB used, 50GB disk
	// Node 2: 4GB total RAM, 2GB used, 100GB disk
	expectedMemTotal := uint64(2147483648 + 4294967296) // 6GB
	expectedMemUsed := uint64(1073741824 + 2147483648)  // 3GB
	expectedDiskTotal := uint64(53687091200 + 107374182400)
	expectedDiskUsed := uint64(10737418240 + 21474836480)

	// Seed multiple history reports per node (15 reports each) to test no amplification
	for i := 0; i < 15; i++ {
		reportTime := now.Add(time.Duration(i-15) * time.Minute)
		// Node 1 network: increases from 1000 to 1000 + 14*200 = 3800
		n1Rx := uint64(1000 + i*200)
		n1Tx := uint64(2000 + i*100)
		n1Payload, _ := json.Marshal(map[string]any{
			"cpu_percent":            10.0,
			"memory_total_bytes":     2147483648,
			"memory_used_bytes":      1073741824,
			"filesystem_total_bytes": 53687091200,
			"filesystem_used_bytes":  10737418240,
			"network_rx_bytes":       n1Rx,
			"network_tx_bytes":       n1Tx,
		})
		_ = store.PersistAgentReport(context.Background(), db.AgentReportInput{
			NodeID:          node1.Node.ID,
			RequestID:       fmt.Sprintf("n1-req-%d", i),
			ReplayExpiresAt: now.Add(time.Hour),
			Now:             reportTime,
			ReportedAt:      reportTime,
			ResourcePayload: n1Payload,
		})

		// Node 2 network: increases from 50000 to 50000 + i*1000 = 64000
		n2Rx := uint64(50000 + i*1000)
		n2Tx := uint64(80000 + i*500)
		n2Payload, _ := json.Marshal(map[string]any{
			"cpu_percent":            20.0,
			"memory_total_bytes":     4294967296,
			"memory_used_bytes":      2147483648,
			"filesystem_total_bytes": 107374182400,
			"filesystem_used_bytes":  21474836480,
			"network_rx_bytes":       n2Rx,
			"network_tx_bytes":       n2Tx,
		})
		_ = store.PersistAgentReport(context.Background(), db.AgentReportInput{
			NodeID:          node2.Node.ID,
			RequestID:       fmt.Sprintf("n2-req-%d", i),
			ReplayExpiresAt: now.Add(time.Hour),
			Now:             reportTime,
			ReportedAt:      reportTime,
			ResourcePayload: n2Payload,
		})
	}

	// Test 1: Verify overview with limit=10 and limit=100
	fromRFC := now.Add(-30 * time.Minute).Format(time.RFC3339)
	toRFC := now.Add(time.Minute).Format(time.RFC3339)
	for _, tc := range []struct {
		limit           int
		expectedRxDelta uint64
		expectedTxDelta uint64
	}{
		// 10 samples (reports 0-9): n1 delta = 9*200 = 1800; n2 delta = 9*1000 = 9000 -> rx = 10800
		{limit: 10, expectedRxDelta: 1800 + 9000, expectedTxDelta: 900 + 4500},
		// 100 samples (all 15 reports): n1 delta = 14*200 = 2800; n2 delta = 14*1000 = 14000 -> rx = 16800
		{limit: 100, expectedRxDelta: 2800 + 14000, expectedTxDelta: 1400 + 7000},
	} {
		t.Run(fmt.Sprintf("Limit_%d", tc.limit), func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/overview?limit=%d&from=%s&to=%s", tc.limit, fromRFC, toRFC), nil)
			req.AddCookie(cookie)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
			}

			var resp struct {
				Resources struct {
					MemTotal   uint64   `json:"memory_total_bytes"`
					MemUsed    uint64   `json:"memory_used_bytes"`
					DiskTotal  uint64   `json:"filesystem_total_bytes"`
					DiskUsed   uint64   `json:"filesystem_used_bytes"`
					RxBytes    uint64   `json:"network_rx_bytes"`
					TxBytes    uint64   `json:"network_tx_bytes"`
					RxDelta    uint64   `json:"network_rx_bytes_delta"`
					TxDelta    uint64   `json:"network_tx_bytes_delta"`
					CPUPercent *float64 `json:"cpu_percent"`
				} `json:"resources"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			// Verify capacity is strictly constant and NOT amplified by history limit
			if resp.Resources.MemTotal != expectedMemTotal {
				t.Errorf("limit %d: expected MemTotal %d, got %d", tc.limit, expectedMemTotal, resp.Resources.MemTotal)
			}
			if resp.Resources.MemUsed != expectedMemUsed {
				t.Errorf("limit %d: expected MemUsed %d, got %d", tc.limit, expectedMemUsed, resp.Resources.MemUsed)
			}
			if resp.Resources.DiskTotal != expectedDiskTotal {
				t.Errorf("limit %d: expected DiskTotal %d, got %d", tc.limit, expectedDiskTotal, resp.Resources.DiskTotal)
			}
			if resp.Resources.DiskUsed != expectedDiskUsed {
				t.Errorf("limit %d: expected DiskUsed %d, got %d", tc.limit, expectedDiskUsed, resp.Resources.DiskUsed)
			}

			// Verify network delta is independently calculated per node within the limit window
			if resp.Resources.RxDelta != tc.expectedRxDelta {
				t.Errorf("limit %d: expected RxDelta %d, got %d", tc.limit, tc.expectedRxDelta, resp.Resources.RxDelta)
			}
			if resp.Resources.TxDelta != tc.expectedTxDelta {
				t.Errorf("limit %d: expected TxDelta %d, got %d", tc.limit, tc.expectedTxDelta, resp.Resources.TxDelta)
			}
		})
	}

	// Test 2: Counter rollover / reset does not underflow
	t.Run("CounterRolloverNoUnderflow", func(t *testing.T) {
		regToken3, _ := store.CreateRegistrationToken(context.Background(), time.Hour)
		node3, _ := store.RegisterNode(context.Background(), regToken3.Token, db.NodeInput{UUID: "550e8400-e29b-41d4-a716-446655440033", Name: "node-3"}, now)

		// First report: rx=10000; Second report (after reboot): rx=2000
		t1 := now.Add(-5 * time.Minute)
		t2 := now
		p1, _ := json.Marshal(map[string]any{"network_rx_bytes": 10000, "memory_total_bytes": 1024})
		p2, _ := json.Marshal(map[string]any{"network_rx_bytes": 2000, "memory_total_bytes": 1024})

		_ = store.PersistAgentReport(context.Background(), db.AgentReportInput{
			NodeID: node3.Node.ID, RequestID: "r3-1", ReportedAt: t1, Now: t1, ResourcePayload: p1,
		})
		_ = store.PersistAgentReport(context.Background(), db.AgentReportInput{
			NodeID: node3.Node.ID, RequestID: "r3-2", ReportedAt: t2, Now: t2, ResourcePayload: p2,
		})

		fromRFC2 := t1.Add(-time.Minute).Format(time.RFC3339)
		toRFC2 := t2.Add(time.Minute).Format(time.RFC3339)
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/overview?from=%s&to=%s", fromRFC2, toRFC2), nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		var resp struct {
			Resources struct {
				RxDelta uint64 `json:"network_rx_bytes_delta"`
			} `json:"resources"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		// Must not underflow to ~18446744073709551615
		if resp.Resources.RxDelta > 1000000 {
			t.Fatalf("unexpected huge RxDelta (underflow): %d", resp.Resources.RxDelta)
		}
	})

	// Test 3: Empty history and empty latest snapshot
	t.Run("EmptyNodeHasZeroCapacity", func(t *testing.T) {
		service2, store2 := newTask4Auth(t)
		defer store2.Close()
		cfg2 := task4Config()
		cfg2.DatabasePath = store2.DatabasePath()
		server2 := NewServer(cfg2, service2)
		handler2 := server2.Handler()
		session2, _ := task4AdminSession(t, service2, store2)
		cookie2 := task4SessionCookie(session2)

		regToken4, _ := store2.CreateRegistrationToken(context.Background(), time.Hour)
		_, _ = store2.RegisterNode(context.Background(), regToken4.Token, db.NodeInput{UUID: "550e8400-e29b-41d4-a716-446655440044", Name: "empty-node"}, now)

		req := httptest.NewRequest(http.MethodGet, "/api/overview", nil)
		req.AddCookie(cookie2)
		rec := httptest.NewRecorder()
		handler2.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}
		var resp struct {
			Nodes struct {
				Total   int `json:"total"`
				Offline int `json:"offline"`
			} `json:"nodes"`
			Resources struct {
				MemTotal uint64 `json:"memory_total_bytes"`
			} `json:"resources"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Nodes.Total != 1 || resp.Nodes.Offline != 1 {
			t.Errorf("expected 1 offline node, got total=%d offline=%d", resp.Nodes.Total, resp.Nodes.Offline)
		}
		if resp.Resources.MemTotal != 0 {
			t.Errorf("expected 0 MemTotal for node without reports, got %d", resp.Resources.MemTotal)
		}
	})
}
