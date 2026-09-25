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

func TestTargetCRUDUsesStrictSafeDTOsAndCSRF(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	handler := NewServer(task4Config(), service).Handler()
	session, csrf := task4AdminSession(t, service, store)

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/targets", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated target list status = %d, want 401", unauthenticated.Code)
	}

	body := `{"id":"api-https","name":"API HTTPS","kind":"https","host":"example.com","port":443,"path":"/health","expected_status":200,"timeout_ms":3000,"max_hops":20,"interval_seconds":60,"enabled":true}`
	withoutCSRF := task4AdminWrite(t, handler, http.MethodPost, session, "", "/api/targets", body)
	if withoutCSRF.Code != http.StatusForbidden {
		t.Fatalf("target create without CSRF status = %d, want 403", withoutCSRF.Code)
	}

	created, csrf := task4AdminWriteWithCSRF(t, handler, http.MethodPost, session, csrf, "/api/targets", body)
	if created.Code != http.StatusCreated {
		t.Fatalf("target create status = %d, body = %q", created.Code, created.Body.String())
	}
	var target map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &target); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"payload", "headers", "cookies", "scripts", "commands", "proxy", "credentials", "created_at", "updated_at"} {
		if _, ok := target[forbidden]; ok || strings.Contains(created.Body.String(), forbidden) {
			t.Fatalf("target response exposed forbidden field %q: %s", forbidden, created.Body.String())
		}
	}
	if target["id"] != "api-https" || target["kind"] != "https" || target["enabled"] != true {
		t.Fatalf("target response = %#v", target)
	}

	listed := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "/api/targets", nil)
	listRequest.AddCookie(task4SessionCookie(session))
	handler.ServeHTTP(listed, listRequest)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"id":"api-https"`) {
		t.Fatalf("target list = %d %q", listed.Code, listed.Body.String())
	}

	patched, csrf := task4AdminWriteWithCSRF(t, handler, http.MethodPatch, session, csrf, "/api/targets/api-https", `{"name":"Updated HTTPS","enabled":false}`)
	if patched.Code != http.StatusOK || !strings.Contains(patched.Body.String(), `"name":"Updated HTTPS"`) || !strings.Contains(patched.Body.String(), `"enabled":false`) {
		t.Fatalf("target patch = %d %q", patched.Code, patched.Body.String())
	}

	unknown, _ := task4AdminWriteWithCSRF(t, handler, http.MethodPost, session, csrf, "/api/targets", `{"id":"unknown-field","name":"bad","kind":"tcp","host":"example.com","port":80,"timeout_ms":1000,"max_hops":1,"interval_seconds":60,"enabled":true,"headers":{"Authorization":"secret"}}`)
	if unknown.Code != http.StatusBadRequest || strings.Contains(unknown.Body.String(), "Authorization") {
		t.Fatalf("unknown target field response = %d %q", unknown.Code, unknown.Body.String())
	}
	csrf = unknown.Header().Get("X-CSRF-Token")
	if csrf != "" {
		t.Fatal("rejected target request unexpectedly rotated CSRF")
	}

	null, _ := task4AdminWriteWithCSRF(t, handler, http.MethodPatch, session, task4CSRF(t, handler, session), "/api/targets/api-https", "null")
	if null.Code != http.StatusBadRequest {
		t.Fatalf("null target body status = %d, want 400", null.Code)
	}

	csrf = task4CSRF(t, handler, session)
	deleted, _ := task4AdminWriteWithCSRF(t, handler, http.MethodDelete, session, csrf, "/api/targets/api-https", "")
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("target delete status = %d, body = %q", deleted.Code, deleted.Body.String())
	}
}

func TestMediaTargetHostRoundTripsThroughCreateListGetAndPatch(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	handler := NewServer(task4Config(), service).Handler()
	session, csrf := task4AdminSession(t, service, store)
	body := `{"id":"media-round-trip","name":"Media detector","kind":"media_http","host":"media.example.com","path":"/manifest","timeout_ms":3000,"interval_seconds":60,"max_hops":20,"enabled":true}`
	created, csrf := task4AdminWriteWithCSRF(t, handler, http.MethodPost, session, csrf, "/api/targets", body)
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), `"host":"media.example.com"`) {
		t.Fatalf("media target create = %d %q", created.Code, created.Body.String())
	}

	listed := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "/api/targets", nil)
	listRequest.AddCookie(task4SessionCookie(session))
	handler.ServeHTTP(listed, listRequest)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"id":"media-round-trip"`) || !strings.Contains(listed.Body.String(), `"host":"media.example.com"`) {
		t.Fatalf("media target list = %d %q", listed.Code, listed.Body.String())
	}

	got, err := store.GetTarget(context.Background(), db.TargetKindMediaHTTP, "media-round-trip")
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "media.example.com" || !strings.Contains(string(got.Payload), `"host":"media.example.com"`) {
		t.Fatalf("media target get = %#v, payload = %s", got, got.Payload)
	}

	patched, _ := task4AdminWriteWithCSRF(t, handler, http.MethodPatch, session, csrf, "/api/targets/media-round-trip", `{"host":"cdn.example.com"}`)
	if patched.Code != http.StatusOK || !strings.Contains(patched.Body.String(), `"host":"cdn.example.com"`) {
		t.Fatalf("media target patch = %d %q", patched.Code, patched.Body.String())
	}
	got, err = store.GetTarget(context.Background(), db.TargetKindMediaHTTP, "media-round-trip")
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "cdn.example.com" || !strings.Contains(string(got.Payload), `"host":"cdn.example.com"`) {
		t.Fatalf("patched media target get = %#v, payload = %s", got, got.Payload)
	}
}

func TestTargetCRUDValidatesHostPathPortsAndLimits(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	handler := NewServer(task4Config(), service).Handler()
	session, csrf := task4AdminSession(t, service, store)

	cases := map[string]string{
		"bad host":    `{"id":"bad-host","name":"bad","kind":"tcp","host":"https://example.com","port":80,"timeout_ms":1000,"max_hops":1,"interval_seconds":60,"enabled":true}`,
		"bad path":    `{"id":"bad-path","name":"bad","kind":"http","host":"example.com","port":80,"path":"health","timeout_ms":1000,"max_hops":1,"interval_seconds":60,"enabled":true}`,
		"bad port":    `{"id":"bad-port","name":"bad","kind":"tcp","host":"example.com","port":65536,"timeout_ms":1000,"max_hops":1,"interval_seconds":60,"enabled":true}`,
		"bad timeout": `{"id":"bad-timeout","name":"bad","kind":"tcp","host":"example.com","port":80,"timeout_ms":99,"max_hops":1,"interval_seconds":60,"enabled":true}`,
		"bad hops":    `{"id":"bad-hops","name":"bad","kind":"mtr","host":"example.com","port":80,"timeout_ms":1000,"max_hops":31,"interval_seconds":60,"enabled":true}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			response, next := task4AdminWriteWithCSRF(t, handler, http.MethodPost, session, csrf, "/api/targets", body)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %q", response.Code, response.Body.String())
			}
			if response.Code == http.StatusBadRequest {
				csrf = task4CSRF(t, handler, session)
			} else if next != "" {
				csrf = next
			}
		})
	}
}

func TestNodeMTRAndMediaReadsRequireNodeUUIDAndReturnSafeLatestResults(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	handler := NewServer(task4Config(), service).Handler()
	session, _ := task4AdminSession(t, service, store)

	registration, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	registered := task4Register(t, handler, protocol.RegisterRequest{RegistrationToken: registration.Token, NodeUUID: task4NodeUUID1, Name: "result node"})
	node := mustNode(t, store, registered.NodeUUID)
	if _, err := store.CreateTarget(context.Background(), db.TargetDefinition{ID: "mtr-read", Name: "MTR", Kind: db.TargetKindMTR, Host: "example.com", Enabled: true, Payload: []byte(`{"max_hops":5}`)}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTarget(context.Background(), db.TargetDefinition{ID: "media-read", Name: "Media", Kind: db.TargetKindMediaHTTP, Host: "media.example.com", Enabled: true, Payload: []byte(`{"host":"media.example.com","path":"/manifest"}`)}, time.Now()); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	mtrPayload, _ := json.Marshal(protocol.MTRResult{Host: "example.com", Reached: true, CheckedAt: now})
	mediaPayload, _ := json.Marshal(protocol.MediaResult{Detector: "media-read", Status: "available", CheckedAt: now})
	if err := store.UpsertMTRLatest(context.Background(), node.ID, "mtr-read", time.Unix(now, 0), mtrPayload); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertMediaLatest(context.Background(), node.ID, "media-read", time.Unix(now, 0), mediaPayload); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/api/nodes/" + registered.NodeUUID + "/mtr", "/api/nodes/" + registered.NodeUUID + "/media"} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.AddCookie(task4SessionCookie(session))
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "payload") || strings.Contains(response.Body.String(), "secret") {
			t.Fatalf("safe result read %q = %d %q", path, response.Code, response.Body.String())
		}
	}

	internalID := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/nodes/"+node.ID+"/mtr", nil)
	request.AddCookie(task4SessionCookie(session))
	handler.ServeHTTP(internalID, request)
	if internalID.Code != http.StatusNotFound {
		t.Fatalf("internal node ID status = %d, want 404", internalID.Code)
	}
}

func TestTargetBindingHappensBeforeReplayForStandaloneAndReportResults(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	handler := NewServer(task4Config(), service).Handler()
	registration, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	registered := task4Register(t, handler, protocol.RegisterRequest{RegistrationToken: registration.Token, NodeUUID: task4NodeUUID2, Name: "binding node"})

	network := protocol.NetworkResultEnvelope{TargetID: "late-target", Result: protocol.NetworkResult{Host: "example.com", Port: 443, CheckedAt: time.Now().Unix()}}
	missing := task4AgentPost(t, handler, "/api/agent/v1/network-result", registered.NodeToken, registered.NodeUUID, "binding-late", network)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("unknown target status = %d, want 404", missing.Code)
	}
	if _, err := store.CreateTarget(context.Background(), db.TargetDefinition{ID: "late-target", Name: "Late", Kind: db.TargetKindHTTPS, Host: "example.com", Enabled: true, Payload: []byte(`{"port":443,"path":"/"}`)}, time.Now()); err != nil {
		t.Fatal(err)
	}
	accepted := task4AgentPost(t, handler, "/api/agent/v1/network-result", registered.NodeToken, registered.NodeUUID, "binding-late", network)
	if accepted.Code != http.StatusNoContent {
		t.Fatalf("same request ID after rejected binding status = %d, body = %q", accepted.Code, accepted.Body.String())
	}

	if _, err := store.CreateTarget(context.Background(), db.TargetDefinition{ID: "disabled-target", Name: "Disabled", Kind: db.TargetKindHTTPS, Host: "example.com", Enabled: false, Payload: []byte(`{"port":443,"path":"/"}`)}, time.Now()); err != nil {
		t.Fatal(err)
	}
	disabled := network
	disabled.TargetID = "disabled-target"
	if response := task4AgentPost(t, handler, "/api/agent/v1/network-result", registered.NodeToken, registered.NodeUUID, "binding-disabled", disabled); response.Code != http.StatusNotFound {
		t.Fatalf("disabled target status = %d, want 404", response.Code)
	}

	mismatch := protocol.MTRResultEnvelope{TargetID: "late-target", Result: protocol.MTRResult{Host: "example.com", Reached: true, CheckedAt: time.Now().Unix()}}
	if response := task4AgentPost(t, handler, "/api/agent/v1/mtr-result", registered.NodeToken, registered.NodeUUID, "binding-mismatch", mismatch); response.Code != http.StatusBadRequest {
		t.Fatalf("standalone type mismatch status = %d, want 400", response.Code)
	}

	report := protocol.ReportRequest{NodeUUID: registered.NodeUUID, ReportedAt: time.Now().Unix(), Results: []protocol.CheckResult{{ID: "late-target", Kind: "tcp", Network: &protocol.NetworkResult{Host: "example.com", Port: 443, CheckedAt: time.Now().Unix()}}}}
	if response := task4AgentPost(t, handler, "/api/agent/v1/report", registered.NodeToken, registered.NodeUUID, "report-mismatch", report); response.Code != http.StatusBadRequest {
		t.Fatalf("report type mismatch status = %d, want 400", response.Code)
	}
	report.Results[0].Kind = "https"
	if response := task4AgentPost(t, handler, "/api/agent/v1/report", registered.NodeToken, registered.NodeUUID, "report-mismatch", report); response.Code != http.StatusNoContent {
		t.Fatalf("same report request ID after rejected binding status = %d, body = %q", response.Code, response.Body.String())
	}
}

func task4AdminWrite(t *testing.T, handler http.Handler, method, session, csrf, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	response, _ := task4AdminWriteWithCSRF(t, handler, method, session, csrf, path, body)
	return response
}

func task4AdminWriteWithCSRF(t *testing.T, handler http.Handler, method, session, csrf, path, body string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	request := httptest.NewRequest(method, "http://127.0.0.1:8080"+path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://127.0.0.1:8080")
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	request.AddCookie(task4SessionCookie(session))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response, response.Header().Get("X-CSRF-Token")
}

func TestMediaTargetRegionRulesRoundTripThroughCreateListAndPatch(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	handler := NewServer(task4Config(), service).Handler()
	session, csrf := task4AdminSession(t, service, store)

	body := `{"id":"media-region","name":"Region detector","kind":"media_http","host":"media.example.com","path":"/manifest","timeout_ms":3000,"interval_seconds":60,"max_hops":20,"enabled":true,"region_rules":[{"region":"SG","contains":"geo-SG"},{"region":"US-West_2","contains":"edge=us-west"}]}`
	created, csrf := task4AdminWriteWithCSRF(t, handler, http.MethodPost, session, csrf, "/api/targets", body)
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), `"region_rules":[{"region":"SG","contains":"geo-SG"},{"region":"US-West_2","contains":"edge=us-west"}]`) {
		t.Fatalf("media target create with rules = %d %q", created.Code, created.Body.String())
	}

	got, err := store.GetTarget(context.Background(), db.TargetKindMediaHTTP, "media-region")
	if err != nil {
		t.Fatal(err)
	}
	var payload agentTargetPayload
	if err := json.Unmarshal(got.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.RegionRules) != 2 || payload.RegionRules[0].Region != "SG" || payload.RegionRules[1].Contains != "edge=us-west" {
		t.Fatalf("stored payload rules = %#v", payload.RegionRules)
	}

	listed, _ := task4AdminWriteWithCSRF(t, handler, http.MethodGet, session, csrf, "/api/targets", "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"region_rules"`) {
		t.Fatalf("target list = %d %q, want region_rules echoed", listed.Code, listed.Body.String())
	}

	patched, csrf := task4AdminWriteWithCSRF(t, handler, http.MethodPatch, session, csrf, "/api/targets/media-region", `{"region_rules":[{"region":"JP","contains":"geo-JP"}]}`)
	if patched.Code != http.StatusOK || !strings.Contains(patched.Body.String(), `"region":"JP"`) {
		t.Fatalf("media target patch rules = %d %q", patched.Code, patched.Body.String())
	}
	got, err = store.GetTarget(context.Background(), db.TargetKindMediaHTTP, "media-region")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got.Payload), `"region":"JP"`) || strings.Contains(string(got.Payload), `"region":"SG"`) {
		t.Fatalf("patched payload = %s, want rules replaced", got.Payload)
	}

	cleared, _ := task4AdminWriteWithCSRF(t, handler, http.MethodPatch, session, csrf, "/api/targets/media-region", `{"region_rules":[]}`)
	if cleared.Code != http.StatusOK || strings.Contains(cleared.Body.String(), "region_rules") {
		t.Fatalf("cleared rules response = %d %q, want empty rules omitted", cleared.Code, cleared.Body.String())
	}
}

func TestMediaTargetRejectsInvalidRegionRules(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	handler := NewServer(task4Config(), service).Handler()
	session, _ := task4AdminSession(t, service, store)

	base := `"id":"media-bad-rules","name":"Bad rules","kind":"media_http","host":"media.example.com","path":"/manifest","timeout_ms":3000,"interval_seconds":60,"max_hops":20,"enabled":true`
	for _, test := range []struct {
		name  string
		rules string
	}{
		{name: "region too long", rules: `[{"region":"aaaaaaaaaaaaaaaaX","contains":"geo"}]`},
		{name: "region illegal characters", rules: `[{"region":"SG US","contains":"geo"}]`},
		{name: "contains too long", rules: `[{"region":"SG","contains":"` + strings.Repeat("a", 129) + `"}]`},
		{name: "contains control characters", rules: `[{"region":"SG","contains":"geo\nSG"}]`},
		{name: "too many rules", rules: `[{"region":"SG","contains":"a"},{"region":"SG","contains":"b"},{"region":"SG","contains":"c"},{"region":"SG","contains":"d"},{"region":"SG","contains":"e"},{"region":"SG","contains":"f"},{"region":"SG","contains":"g"},{"region":"SG","contains":"h"},{"region":"SG","contains":"i"},{"region":"SG","contains":"j"},{"region":"SG","contains":"k"},{"region":"SG","contains":"l"},{"region":"SG","contains":"m"},{"region":"SG","contains":"n"},{"region":"SG","contains":"o"},{"region":"SG","contains":"p"},{"region":"SG","contains":"q"}]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			csrf := task4CSRF(t, handler, session)
			response, _ := task4AdminWriteWithCSRF(t, handler, http.MethodPost, session, csrf, "/api/targets", `{"`+base+`","region_rules":`+test.rules+`}`)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("create with %s = %d %q, want 400", test.name, response.Code, response.Body.String())
			}
		})
	}

	rulesOnTCP, _ := task4AdminWriteWithCSRF(t, handler, http.MethodPost, session, task4CSRF(t, handler, session), "/api/targets", `{"id":"tcp-bad-rules","name":"TCP rules","kind":"tcp","host":"example.com","port":443,"timeout_ms":3000,"max_hops":20,"interval_seconds":60,"enabled":true,"region_rules":[{"region":"SG","contains":"geo-SG"}]}`)
	if rulesOnTCP.Code != http.StatusBadRequest {
		t.Fatalf("tcp target with rules = %d %q, want 400", rulesOnTCP.Code, rulesOnTCP.Code)
	}
	if _, err := store.GetTarget(context.Background(), db.TargetKindTCP, "tcp-bad-rules"); err == nil {
		t.Fatal("target with invalid rules was persisted")
	}
}

func TestSeedMediaTargetsIdempotent(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	handler := NewServer(task4Config(), service).Handler()
	session, csrf := task4AdminSession(t, service, store)

	// Without CSRF should be rejected
	noCSRF := task4AdminWrite(t, handler, http.MethodPost, session, "", "/api/targets/seed-media", "{}")
	if noCSRF.Code != http.StatusForbidden {
		t.Fatalf("seed without CSRF status = %d, want 403", noCSRF.Code)
	}

	// First call seeds all 8 targets
	res1, csrf := task4AdminWriteWithCSRF(t, handler, http.MethodPost, session, csrf, "/api/targets/seed-media", "{}")
	if res1.Code != http.StatusOK {
		t.Fatalf("seed status = %d %q, want 200", res1.Code, res1.Body.String())
	}
	var data1 map[string]any
	if err := json.Unmarshal(res1.Body.Bytes(), &data1); err != nil {
		t.Fatal(err)
	}
	if data1["ok"] != true || int(data1["created"].(float64)) != 8 {
		t.Fatalf("unexpected seed response 1: %#v", data1)
	}

	// Verify ChatGPT and Claude targets exist
	chatgptTarget, err := store.GetTarget(context.Background(), db.TargetKindMediaHTTP, "media-chatgpt")
	if err != nil || !chatgptTarget.Enabled {
		t.Fatalf("media-chatgpt target missing or disabled: %v", err)
	}
	claudeTarget, err := store.GetTarget(context.Background(), db.TargetKindMediaHTTP, "media-claude")
	if err != nil || !claudeTarget.Enabled {
		t.Fatalf("media-claude target missing or disabled: %v", err)
	}

	// Second call should be idempotent (created: 0)
	res2, _ := task4AdminWriteWithCSRF(t, handler, http.MethodPost, session, csrf, "/api/targets/seed-media", "{}")
	if res2.Code != http.StatusOK {
		t.Fatalf("seed status 2 = %d %q, want 200", res2.Code, res2.Body.String())
	}
	var data2 map[string]any
	if err := json.Unmarshal(res2.Body.Bytes(), &data2); err != nil {
		t.Fatal(err)
	}
	if data2["ok"] != true || int(data2["created"].(float64)) != 0 {
		t.Fatalf("unexpected seed response 2: %#v", data2)
	}
}

