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

func publicStatusRegisterNode(t *testing.T, handler http.Handler, store *db.Store, nodeUUID, name string) db.Node {
	t.Helper()
	token, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	registered := task4Register(t, handler, protocol.RegisterRequest{NodeUUID: nodeUUID, Name: name, RegistrationToken: token.Token})
	return mustNode(t, store, registered.NodeUUID)
}

func TestPublicStatusReturnsSanitizedAggregatesWithoutAuthentication(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	handler := NewServer(task4Config(), service).Handler()
	now := time.Now().UTC().Truncate(time.Second)

	onlineNode := publicStatusRegisterNode(t, handler, store, task4NodeUUID1, "edge-01 192.0.2.10")
	quietNode := publicStatusRegisterNode(t, handler, store, task4NodeUUID2, "edge-02")
	if err := store.UpsertResourceLatest(context.Background(), onlineNode.ID, now.Add(-30*time.Second), []byte(`{"hostname":"secret-host","cpu_percent":12.5,"memory_used_bytes":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateNetworkTarget(context.Background(), db.ResultTargetInput{ID: "public-target", Name: "public-target", Kind: "tcp", Host: "example.com"}, now); err != nil {
		t.Fatal(err)
	}
	if err := store.PersistAgentResult(context.Background(), onlineNode.ID, "public-status-request-1", now.Add(time.Hour), now, db.AgentResultInput{Kind: db.TargetKindTCP, TargetID: "public-target", CheckedAt: now, Payload: []byte(`{"status":"success","latency_ms":120}`)}); err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/public/status", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("public status status = %d, body = %q", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("public status content type = %q", response.Header().Get("Content-Type"))
	}
	var payload struct {
		Nodes struct {
			Online int      `json:"online"`
			Total  int      `json:"total"`
			Names  []string `json:"names"`
		} `json:"nodes"`
		Checks struct {
			SuccessRate  *float64 `json:"success_rate"`
			AvgLatencyMs *float64 `json:"avg_latency_ms"`
		} `json:"checks"`
		LastUpdatedAt *time.Time `json:"last_updated_at"`
		GeneratedAt   time.Time  `json:"generated_at"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Nodes.Total != 2 || payload.Nodes.Online != 1 {
		t.Fatalf("public status node counts = %d/%d, want 1/2", payload.Nodes.Online, payload.Nodes.Total)
	}
	if len(payload.Nodes.Names) != 2 || payload.Nodes.Names[0] != "edge-01 [已脱敏]" || payload.Nodes.Names[1] != "edge-02" {
		t.Fatalf("public status node names = %#v", payload.Nodes.Names)
	}
	if payload.Checks.SuccessRate == nil || *payload.Checks.SuccessRate != 100 {
		t.Fatalf("public status success rate = %#v, want 100", payload.Checks.SuccessRate)
	}
	if payload.Checks.AvgLatencyMs == nil || *payload.Checks.AvgLatencyMs != 120 {
		t.Fatalf("public status avg latency = %#v, want 120", payload.Checks.AvgLatencyMs)
	}
	if payload.LastUpdatedAt == nil {
		t.Fatal("public status last_updated_at is missing")
	}
	body := response.Body.String()
	for _, leaked := range []string{
		onlineNode.ID, onlineNode.UUID, quietNode.ID, quietNode.UUID,
		"secret-host", "cpu_percent", "memory_used_bytes", "public-target",
		"192.0.2.10", "uuid", "node_id", "target_id", "detector_id", "token", "resource", "alert",
	} {
		if strings.Contains(body, leaked) {
			t.Fatalf("public status body leaked %q: %s", leaked, body)
		}
	}
}

func TestPublicStatusRejectsNonGetMethods(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	handler := NewServer(task4Config(), service).Handler()
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(method, "/api/public/status", nil))
		if response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s /api/public/status status = %d, want 405", method, response.Code)
		}
	}
}

func TestPublicStatusRateLimitsUnauthenticatedClients(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	server := NewServer(task4Config(), service)
	server.publicLimiter = newRateLimiter(1, time.Minute, 32)
	handler := server.Handler()
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/api/public/status", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first public status status = %d, want 200", first.Code)
	}
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/api/public/status", nil))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second public status status = %d, want 429", second.Code)
	}
}

func TestPublicStatusEmptyStoreReturnsZeroedAggregates(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	handler := NewServer(task4Config(), service).Handler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/public/status", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("empty public status status = %d, body = %q", response.Code, response.Body.String())
	}
	var payload struct {
		Nodes struct {
			Online int      `json:"online"`
			Total  int      `json:"total"`
			Names  []string `json:"names"`
		} `json:"nodes"`
		Checks struct {
			SuccessRate  *float64 `json:"success_rate"`
			AvgLatencyMs *float64 `json:"avg_latency_ms"`
		} `json:"checks"`
		LastUpdatedAt *time.Time `json:"last_updated_at"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Nodes.Total != 0 || payload.Nodes.Online != 0 || len(payload.Nodes.Names) != 0 {
		t.Fatalf("empty public status nodes = %#v", payload.Nodes)
	}
	if payload.Checks.SuccessRate != nil || payload.Checks.AvgLatencyMs != nil || payload.LastUpdatedAt != nil {
		t.Fatalf("empty public status checks = %#v", payload.Checks)
	}
}

func TestSanitizeNodeNameMasksIdentifiers(t *testing.T) {
	cases := map[string]string{
		"edge-01":         "edge-01",
		"edge 192.0.2.10": "edge [已脱敏]",
		"node 550e8400-e29b-41d4-a716-446655440000":                   "node [已脱敏]",
		"550E8400-E29B-41D4-A716-446655440000":                        "[已脱敏]",
		"  relay-9 198.51.100.7 550e8400-e29b-41d4-a716-446655440000": "relay-9 [已脱敏] [已脱敏]",
	}
	for input, want := range cases {
		if got := sanitizeNodeName(input); got != want {
			t.Errorf("sanitizeNodeName(%q) = %q, want %q", input, got, want)
		}
	}
	long := strings.Repeat("名", publicNodeNameLimit+4)
	if got := sanitizeNodeName(long); len([]rune(got)) != publicNodeNameLimit+1 || !strings.HasSuffix(got, "…") {
		t.Errorf("sanitizeNodeName did not truncate long name: %d runes", len([]rune(got)))
	}
}
