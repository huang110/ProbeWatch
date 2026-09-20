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

func TestNodeReadEndpointsRequireAuthentication(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	handler := NewServer(task4Config(), service).Handler()
	for _, path := range []string{
		"/api/nodes/550e8400-e29b-41d4-a716-446655440000",
		"/api/nodes/550e8400-e29b-41d4-a716-446655440000/resource",
		"/api/nodes/550e8400-e29b-41d4-a716-446655440000/network",
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusUnauthorized {
			t.Errorf("GET %s status = %d, want 401", path, response.Code)
		}
	}
}

func TestAuthenticatedNodeReadEndpointsReturnEmptyAndLatestPayloads(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	handler := NewServer(task4Config(), service).Handler()
	session, _ := task4AdminSession(t, service, store)
	registered := task4Register(t, handler, protocol.RegisterRequest{
		NodeUUID: task4NodeUUID1,
		Name:     "read node",
		RegistrationToken: func() string {
			token, err := store.CreateRegistrationToken(context.Background(), time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			return token.Token
		}(),
	})
	node := mustNode(t, store, registered.NodeUUID)
	cookie := task4SessionCookie(session)

	for _, path := range []string{"/api/nodes/" + registered.NodeUUID + "/resource", "/api/nodes/" + registered.NodeUUID + "/network"} {
		response := task4AuthenticatedGET(t, handler, path, cookie)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, body = %q", path, response.Code, response.Body.String())
		}
		if path[strings.LastIndex(path, "/")+1:] == "network" {
			var payload []any
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload == nil || len(payload) != 0 {
				t.Fatalf("empty network payload = %s", response.Body.String())
			}
		} else if response.Body.String() != `{"reported_at":null,"resource":null}`+"\n" {
			t.Fatalf("empty resource payload = %q", response.Body.String())
		}
	}

	reportedAt := time.Now().UTC().Truncate(time.Second)
	resource := []byte(`{"hostname":"read-host","cpu_percent":12.5}`)
	if err := store.UpsertResourceLatest(context.Background(), node.ID, reportedAt, resource); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateNetworkTarget(context.Background(), db.ResultTargetInput{ID: "read-target", Name: "read-target", Kind: "tcp", Host: "example.com"}, reportedAt); err != nil {
		t.Fatal(err)
	}
	networkAt := reportedAt.Add(time.Second)
	network := []byte(`{"host":"example.com","port":443,"status":"ok"}`)
	if err := store.UpsertNetworkLatest(context.Background(), node.ID, "read-target", networkAt, network); err != nil {
		t.Fatal(err)
	}

	summary := task4AuthenticatedGET(t, handler, "/api/nodes/"+registered.NodeUUID, cookie)
	if summary.Code != http.StatusOK || !strings.Contains(summary.Body.String(), `"uuid":"`+registered.NodeUUID+`"`) || !strings.Contains(summary.Body.String(), `"hostname":"read-host"`) {
		t.Fatalf("summary = %d %q", summary.Code, summary.Body.String())
	}
	resourceResponse := task4AuthenticatedGET(t, handler, "/api/nodes/"+registered.NodeUUID+"/resource", cookie)
	if resourceResponse.Code != http.StatusOK || !strings.Contains(resourceResponse.Body.String(), `"cpu_percent":12.5`) {
		t.Fatalf("resource = %d %q", resourceResponse.Code, resourceResponse.Body.String())
	}
	networkResponse := task4AuthenticatedGET(t, handler, "/api/nodes/"+registered.NodeUUID+"/network", cookie)
	var networkPayload []networkLatestResponse
	if networkResponse.Code != http.StatusOK || json.Unmarshal(networkResponse.Body.Bytes(), &networkPayload) != nil || len(networkPayload) != 1 || networkPayload[0].TargetID != "read-target" || networkPayload[0].Result.Status != "ok" {
		t.Fatalf("network = %d %q %#v", networkResponse.Code, networkResponse.Body.String(), networkPayload)
	}
}

func task4AuthenticatedGET(t *testing.T, handler http.Handler, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
