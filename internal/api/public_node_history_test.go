package api

import (
    "context"
    "encoding/json"
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
    if err != nil { t.Fatal(err) }
    registered := task4Register(t, handler, protocol.RegisterRequest{NodeUUID: task4NodeUUID1, Name: "public history node", RegistrationToken: registrationToken.Token})
    node := mustNode(t, store, registered.NodeUUID)
    if err := store.CreateNetworkTarget(context.Background(), db.ResultTargetInput{ID: "public-history-target", Name: "public target", Kind: string(db.TargetKindTCP), Host: "example.com"}, now); err != nil { t.Fatal(err) }
    if err := store.PersistAgentResult(context.Background(), node.ID, "public-history-request", now.Add(time.Hour), now, db.AgentResultInput{Kind: db.TargetKindTCP, TargetID: "public-history-target", CheckedAt: now, Payload: []byte(`{"status":"success","latency_ms":123}`)}); err != nil { t.Fatal(err) }
    request := httptest.NewRequest(http.MethodGet, "/api/public/nodes/"+registered.NodeUUID+"/network/history?range=1h&limit=10", nil)
    response := httptest.NewRecorder()
    handler.ServeHTTP(response, request)
    if response.Code != http.StatusOK { t.Fatalf("public network history status = %d, body = %q", response.Code, response.Body.String()) }
    var payload []historyResultResponse
    if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil { t.Fatal(err) }
    if len(payload) != 1 || payload[0].TargetID != "public-history-target" || string(payload[0].Result) != `{"status":"success","latency_ms":123}` { t.Fatalf("public network history payload = %+v", payload) }
}

func TestPublicNodeNetworkHistoryRejectsInvalidRequests(t *testing.T) {
    service, store := newTask4Auth(t)
    defer store.Close()
    handler := NewServer(task4Config(), service).Handler()
    for _, test := range []struct { method string; path string; want int }{
        {http.MethodPost, "/api/public/nodes/550e8400-e29b-41d4-a716-446655440000/network/history", http.StatusMethodNotAllowed},
        {http.MethodGet, "/api/public/nodes/not-a-uuid/network/history", http.StatusNotFound},
    } {
        request := httptest.NewRequest(test.method, test.path, nil)
        response := httptest.NewRecorder()
        handler.ServeHTTP(response, request)
        if response.Code != test.want { t.Errorf("%s %s status = %d, want %d", test.method, test.path, response.Code, test.want) }
    }
}
