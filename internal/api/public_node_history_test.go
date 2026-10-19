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
	"github.com/probewatch/probewatch/internal/protocol"
)

func TestPublicNodeNetworkHistoryReturnsReadOnlyProbeSamples(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	handler := NewServer(task4Config(), service).Handler()
	now := time.Now().UTC().Truncate(time.Second)
	registrationToken, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	registered := task4Register(t, handler, protocol.RegisterRequest{NodeUUID: task4NodeUUID1, Name: "public history node", RegistrationToken: registrationToken.Token})
	node := mustNode(t, store, registered.NodeUUID)
	if err := store.CreateNetworkTarget(context.Background(), db.ResultTargetInput{ID: "public-history-target", Name: "public target", Kind: string(db.TargetKindTCP), Host: "example.com"}, now); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateNetworkTarget(context.Background(), db.ResultTargetInput{ID: "public-history-http", Name: "public HTTP target", Kind: string(db.TargetKindHTTP), Host: "example.com"}, now); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateNetworkTarget(context.Background(), db.ResultTargetInput{ID: "public-history-dns", Name: "public DNS target", Kind: string(db.TargetKindDNS), Host: "example.com"}, now); err != nil {
		t.Fatal(err)
	}
	if err := store.PersistAgentResult(context.Background(), node.ID, "public-history-request", now.Add(time.Hour), now, db.AgentResultInput{Kind: db.TargetKindTCP, TargetID: "public-history-target", CheckedAt: now, Payload: []byte(`{"status":"success","latency_ms":123}`)}); err != nil {
		t.Fatal(err)
	}
	if err := store.PersistAgentResult(context.Background(), node.ID, "public-history-http-request", now.Add(-time.Minute), now.Add(-time.Minute), db.AgentResultInput{Kind: db.TargetKindHTTP, TargetID: "public-history-http", CheckedAt: now.Add(-time.Minute), Payload: []byte(`{"status":"success","latency_ms":231}`)}); err != nil {
		t.Fatal(err)
	}
	if err := store.PersistAgentResult(context.Background(), node.ID, "public-history-dns-request", now.Add(-2*time.Minute), now.Add(-2*time.Minute), db.AgentResultInput{Kind: db.TargetKindDNS, TargetID: "public-history-dns", CheckedAt: now.Add(-2 * time.Minute), Payload: []byte(`{"status":"success","latency_ms":312}`)}); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/public/nodes/"+registered.NodeUUID+"/network/history?range=1h&limit=10", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("public network history status = %d, body = %q", response.Code, response.Body.String())
	}
	var payload []historyResultResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	var parsedRes struct {
		Status    string `json:"status"`
		LatencyMS int64  `json:"latency_ms"`
	}
	if len(payload) != 3 {
		t.Fatalf("public network history payload count = %d, want 3: %+v", len(payload), payload)
	}
	for i, wantLatency := range []int64{123, 231, 312} {
		parsedRes = struct {
			Status    string `json:"status"`
			LatencyMS int64  `json:"latency_ms"`
		}{}
		if json.Unmarshal(payload[i].Result, &parsedRes) != nil || payload[i].TargetID == "" || parsedRes.Status != "success" || parsedRes.LatencyMS != wantLatency {
			t.Fatalf("public network history payload[%d] = %+v, want latency %d", i, payload[i], wantLatency)
		}
	}
}

func TestPublicTargetAliasFallbackIsStable(t *testing.T) {
	aliases := map[string]string{"target-a": "check-1"}
	first := publicTargetAliasFor(aliases, "target-b")
	second := publicTargetAliasFor(aliases, " target-b ")
	if first == "" || first != second {
		t.Fatalf("fallback target aliases = %q and %q, want stable equal values", first, second)
	}
	if aliases["target-b"] != first {
		t.Fatalf("fallback alias was not cached: %+v", aliases)
	}
	if got := publicTargetAliasFor(aliases, ""); got != "unknown" {
		t.Fatalf("empty target alias = %q, want unknown", got)
	}
}

func TestPublicNodeNetworkHistoryRejectsInvalidRequests(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	handler := NewServer(task4Config(), service).Handler()
	for _, test := range []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodPost, "/api/public/nodes/550e8400-e29b-41d4-a716-446655440000/network/history", http.StatusMethodNotAllowed},
		{http.MethodGet, "/api/public/nodes/not-a-uuid/network/history", http.StatusNotFound},
	} {
		request := httptest.NewRequest(test.method, test.path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.want {
			t.Errorf("%s %s status = %d, want %d", test.method, test.path, response.Code, test.want)
		}
	}
}

func TestPublicNodeTrafficDoesNotRequireAuthentication(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	handler := NewServer(task4Config(), service).Handler()
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Second)
	registrationToken, err := store.CreateRegistrationToken(ctx, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	registered := task4Register(t, handler, protocol.RegisterRequest{
		NodeUUID:          task4NodeUUID1,
		Name:              "public traffic node",
		RegistrationToken: registrationToken.Token,
	})
	node := mustNode(t, store, registered.NodeUUID)
	for i, sample := range []struct{ rx, tx uint64 }{{100, 200}, {1100, 1200}} {
		at := base.Add(time.Duration(i) * time.Minute)
		payload, err := json.Marshal(map[string]any{
			"network_rx_bytes": sample.rx,
			"network_tx_bytes": sample.tx,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.PersistAgentReport(ctx, db.AgentReportInput{
			NodeID:          node.ID,
			RequestID:       fmt.Sprintf("public-traffic-%d", i),
			ReportedAt:      at,
			Now:             at,
			ResourcePayload: payload,
		}); err != nil {
			t.Fatal(err)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/api/public/nodes/"+registered.NodeUUID+"/traffic?period=day", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("public traffic status = %d, body = %q", response.Code, response.Body.String())
	}
	var report trafficReportResponse
	if err := json.Unmarshal(response.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Period != "day" || report.RxBytes == nil || report.TxBytes == nil {
		t.Fatalf("public traffic report = %+v, want populated day report", report)
	}
}
