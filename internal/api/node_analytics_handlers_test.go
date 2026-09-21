package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/protocol"
)

func TestNodeAnalyticsEndpointsRequireAuthentication(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	handler := NewServer(task4Config(), service).Handler()
	uuid := task4NodeUUID1
	for _, path := range []string{
		"/api/nodes/" + uuid + "/checks/summary",
		"/api/nodes/" + uuid + "/traffic",
		"/api/nodes/" + uuid + "/traffic?period=week",
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusUnauthorized {
			t.Errorf("GET %s status = %d, want 401", path, response.Code)
		}
	}
}

func TestNodeChecksSummaryEndpoint(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	handler := NewServer(task4Config(), service).Handler()
	session, _ := task4AdminSession(t, service, store)
	cookie := task4SessionCookie(session)
	registered := task4Register(t, handler, protocol.RegisterRequest{
		NodeUUID: task4NodeUUID1,
		Name:     "analytics node",
		RegistrationToken: func() string {
			token, err := store.CreateRegistrationToken(context.Background(), time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			return token.Token
		}(),
	})
	node := mustNode(t, store, registered.NodeUUID)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	if err := store.CreateNetworkTarget(ctx, db.ResultTargetInput{ID: "tgt-1", Name: "target-1", Kind: string(db.TargetKindTCP), Host: "example.com"}, now); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateNetworkTarget(ctx, db.ResultTargetInput{ID: "tgt-2", Name: "target-2", Kind: string(db.TargetKindHTTPS), Host: "example.org"}, now); err != nil {
		t.Fatal(err)
	}
	// Two in-window checks for tgt-1: one success with latency, one timeout.
	if err := store.UpsertNetworkLatest(ctx, node.ID, "tgt-1", now.Add(-time.Hour), []byte(`{"status":"success","latency_ms":100}`)); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertNetworkLatest(ctx, node.ID, "tgt-1", now.Add(-30*time.Minute), []byte(`{"status":"timeout","latency_ms":0}`)); err != nil {
		t.Fatal(err)
	}
	// tgt-2 was last checked long before the window: stats stay null.
	if err := store.UpsertNetworkLatest(ctx, node.ID, "tgt-2", now.Add(-72*time.Hour), []byte(`{"status":"success","latency_ms":42}`)); err != nil {
		t.Fatal(err)
	}

	response := task4AuthenticatedGET(t, handler, "/api/nodes/"+registered.NodeUUID+"/checks/summary", cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("checks summary status = %d, body = %q", response.Code, response.Body.String())
	}
	var payload []checkSummaryResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 2 {
		t.Fatalf("payload = %+v, want two targets", payload)
	}
	byTarget := map[string]checkSummaryResponse{}
	for _, item := range payload {
		byTarget[item.TargetID] = item
	}
	first := byTarget["tgt-1"]
	if first.Total == nil || *first.Total != 2 || first.Success == nil || *first.Success != 1 || first.Failure == nil || *first.Failure != 1 {
		t.Fatalf("tgt-1 counts = %+v", first)
	}
	if first.LossRate == nil || *first.LossRate != 0.5 {
		t.Fatalf("tgt-1 loss_rate = %+v, want 0.5", first.LossRate)
	}
	if first.LatencyAvgMS == nil || *first.LatencyAvgMS != 100 {
		t.Fatalf("tgt-1 latency_avg_ms = %+v, want 100", first.LatencyAvgMS)
	}
	if first.JitterMS == nil || *first.JitterMS != 0 {
		t.Fatalf("tgt-1 jitter_ms = %+v, want 0 (single latency observation)", first.JitterMS)
	}
	if first.LastCheckedAt == nil || !first.LastCheckedAt.Equal(now.Add(-30*time.Minute)) {
		t.Fatalf("tgt-1 last_checked_at = %+v", first.LastCheckedAt)
	}
	if first.Name != "target-1" || first.Kind != "tcp" || first.Host != "example.com" {
		t.Fatalf("tgt-1 metadata = %+v", first)
	}
	second := byTarget["tgt-2"]
	if second.Total != nil || second.Success != nil || second.Failure != nil || second.LossRate != nil || second.LatencyAvgMS != nil || second.JitterMS != nil {
		t.Fatalf("tgt-2 stats must be null without window data: %+v", second)
	}
	if second.LastCheckedAt == nil || !second.LastCheckedAt.Equal(now.Add(-72*time.Hour)) {
		t.Fatalf("tgt-2 last_checked_at = %+v", second.LastCheckedAt)
	}

	// A node that never reported anything yields an empty summary.
	empty := task4Register(t, handler, protocol.RegisterRequest{
		NodeUUID: task4NodeUUID2,
		Name:     "empty node",
		RegistrationToken: func() string {
			token, err := store.CreateRegistrationToken(context.Background(), time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			return token.Token
		}(),
	})
	emptyResponse := task4AuthenticatedGET(t, handler, "/api/nodes/"+empty.NodeUUID+"/checks/summary", cookie)
	if emptyResponse.Code != http.StatusOK || strings.TrimSpace(emptyResponse.Body.String()) != "[]" {
		t.Fatalf("empty summary = %d %q, want 200 []", emptyResponse.Code, emptyResponse.Body.String())
	}
}

func TestNodeChecksSummaryValidation(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	handler := NewServer(task4Config(), service).Handler()
	session, _ := task4AdminSession(t, service, store)
	cookie := task4SessionCookie(session)
	base := "/api/nodes/" + task4NodeUUID1 + "/checks/summary"
	for _, path := range []string{
		base + "?from=not-a-time",
		base + "?to=2026-13-99T00:00:00Z",
		base + "?limit=0",
		base + "?limit=1001",
		base + "?from=2026-01-01T00:00:00Z&to=2025-01-01T00:00:00Z",
	} {
		response := task4AuthenticatedGET(t, handler, path, cookie)
		if response.Code != http.StatusBadRequest {
			t.Errorf("GET %s status = %d, want 400", path, response.Code)
		}
	}
	// Unknown but well-formed UUID and malformed UUID both 404.
	for _, path := range []string{
		"/api/nodes/550e8400-e29b-41d4-a716-446655440000/checks/summary",
		"/api/nodes/not-a-uuid/checks/summary",
	} {
		response := task4AuthenticatedGET(t, handler, path, cookie)
		if response.Code != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404", path, response.Code)
		}
	}
}

func TestNodeTrafficEndpoint(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	handler := NewServer(task4Config(), service).Handler()
	session, _ := task4AdminSession(t, service, store)
	cookie := task4SessionCookie(session)
	registered := task4Register(t, handler, protocol.RegisterRequest{
		NodeUUID: task4NodeUUID1,
		Name:     "traffic node",
		RegistrationToken: func() string {
			token, err := store.CreateRegistrationToken(context.Background(), time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			return token.Token
		}(),
	})
	node := mustNode(t, store, registered.NodeUUID)
	ctx := context.Background()
	now := time.Now().UTC()

	resource := func(rx, tx uint64, at time.Time) {
		payload, err := json.Marshal(map[string]any{"cpu_percent": 1.0, "network_rx_bytes": rx, "network_tx_bytes": tx})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.UpsertResourceLatest(ctx, node.ID, at, payload); err != nil {
			t.Fatal(err)
		}
	}
	resource(100, 200, now.Add(-2*time.Hour)) // baseline
	resource(1100, 300, now.Add(-time.Hour))  // rx +1000, tx +100
	resource(60, 1200, now.Add(-time.Minute)) // rx reset (1100→60), tx +900

	response := task4AuthenticatedGET(t, handler, "/api/nodes/"+registered.NodeUUID+"/traffic?period=day", cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("traffic status = %d, body = %q", response.Code, response.Body.String())
	}
	var payload trafficReportResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Period != "day" || payload.IntervalSec != 3600 {
		t.Fatalf("period/interval = %s/%d", payload.Period, payload.IntervalSec)
	}
	if payload.Window["from"].IsZero() || payload.Window["to"].IsZero() {
		t.Fatalf("window = %+v", payload.Window)
	}
	if payload.RxBytes == nil || *payload.RxBytes != 1000 {
		t.Fatalf("rx_bytes = %+v, want 1000 (reset skipped)", payload.RxBytes)
	}
	if payload.TxBytes == nil || *payload.TxBytes != 1000 {
		t.Fatalf("tx_bytes = %+v, want 1000", payload.TxBytes)
	}
	if payload.RxResets != 1 || payload.TxResets != 0 {
		t.Fatalf("resets = rx %d tx %d, want rx 1 tx 0", payload.RxResets, payload.TxResets)
	}
	if len(payload.Series) != 25 {
		t.Fatalf("series = %d points, want 25", len(payload.Series))
	}
	if payload.Series[0].RxBytes != nil || payload.Series[0].TxBytes != nil {
		t.Fatalf("first bucket must be null without data: %+v", payload.Series[0])
	}
	// Three buckets carry data: the baseline bucket (0/0), the bucket holding
	// the +1000/+100 delta and the bucket holding the post-reset +900 tx.
	var dataPoints, rxThousand int
	for _, point := range payload.Series {
		if point.RxBytes != nil {
			dataPoints++
			if *point.RxBytes == 1000 {
				rxThousand++
			}
		}
	}
	if dataPoints != 3 || rxThousand != 1 {
		t.Fatalf("data points = %d (rx==1000: %d), want 3 (rx==1000: 1)", dataPoints, rxThousand)
	}

	// Week and month windows and the default period.
	for _, query := range []string{"?period=week", "?period=month", ""} {
		periodResponse := task4AuthenticatedGET(t, handler, "/api/nodes/"+registered.NodeUUID+"/traffic"+query, cookie)
		if periodResponse.Code != http.StatusOK {
			t.Fatalf("traffic%s status = %d", query, periodResponse.Code)
		}
		var periodPayload trafficReportResponse
		if err := json.Unmarshal(periodResponse.Body.Bytes(), &periodPayload); err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"?period=week": "week", "?period=month": "month", "": "day"}[query]
		if periodPayload.Period != want {
			t.Fatalf("traffic%s period = %s, want %s", query, periodPayload.Period, want)
		}
	}

	if invalid := task4AuthenticatedGET(t, handler, "/api/nodes/"+registered.NodeUUID+"/traffic?period=year", cookie); invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid period status = %d, want 400", invalid.Code)
	}
	if missing := task4AuthenticatedGET(t, handler, "/api/nodes/"+task4NodeUUID3+"/traffic", cookie); missing.Code != http.StatusNotFound {
		t.Fatalf("unknown node status = %d, want 404", missing.Code)
	}
	if malformed := task4AuthenticatedGET(t, handler, "/api/nodes/not-a-uuid/traffic", cookie); malformed.Code != http.StatusNotFound {
		t.Fatalf("malformed uuid status = %d, want 404", malformed.Code)
	}
}
