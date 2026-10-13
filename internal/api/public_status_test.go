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
			Online    int      `json:"online"`
			Total     int      `json:"total"`
			Names     []string `json:"names"`
			Telemetry []struct {
				UUID string `json:"uuid"`
				Name string `json:"name"`
			} `json:"telemetry"`
		} `json:"nodes"`
		Checks struct {
			SuccessRate  *float64 `json:"success_rate"`
			AvgLatencyMs *float64 `json:"avg_latency_ms"`
			Total        int      `json:"total"`
			Success      int      `json:"success"`
			Failure      int      `json:"failure"`
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
	if len(payload.Nodes.Telemetry) != 2 || payload.Nodes.Telemetry[0].UUID != onlineNode.UUID || payload.Nodes.Telemetry[1].UUID != quietNode.UUID {
		t.Fatalf("public status telemetry uuids mismatch: %#v", payload.Nodes.Telemetry)
	}
	if payload.Checks.SuccessRate == nil || *payload.Checks.SuccessRate != 100 {
		t.Fatalf("public status success rate = %#v, want 100", payload.Checks.SuccessRate)
	}
	if payload.Checks.AvgLatencyMs == nil || *payload.Checks.AvgLatencyMs != 120 {
		t.Fatalf("public status avg latency = %#v, want 120", payload.Checks.AvgLatencyMs)
	}
	if payload.Checks.Total != 1 || payload.Checks.Success != 1 || payload.Checks.Failure != 0 {
		t.Fatalf("public status counts = %#v/%#v/%#v, want 1/1/0", payload.Checks.Total, payload.Checks.Success, payload.Checks.Failure)
	}
	if payload.LastUpdatedAt == nil {
		t.Fatal("public status last_updated_at is missing")
	}
	body := response.Body.String()
	for _, leaked := range []string{
		onlineNode.ID, quietNode.ID,
		"secret-host", "public-target",
		"192.0.2.10", "node_id", "target_id", "detector_id", "token", "resource", "alert",
	} {
		if strings.Contains(body, leaked) {
			t.Fatalf("public status body leaked %q: %s", leaked, body)
		}
	}
	if !strings.Contains(body, "\"cpu_percent\":12.5") || !strings.Contains(body, "\"network_rx_bytes\"") {
		t.Fatalf("public status body omitted allow-listed live telemetry: %s", body)
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

func TestPublicClientInfoEndpoint(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()

	cfgDefault := task4Config()
	serverDefault := NewServer(cfgDefault, service)
	handlerDefault := serverDefault.Handler()

	// 1. Direct untrusted public request with forged X-Forwarded-For / X-Real-IP / CF-Connecting-IP and geo headers
	reqUntrusted := httptest.NewRequest(http.MethodGet, "/api/public/client-info", nil)
	reqUntrusted.RemoteAddr = "198.51.100.42:54321"
	reqUntrusted.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:131.0) Gecko/20100101 Firefox/131.0")
	reqUntrusted.Header.Set("X-Forwarded-For", "203.0.113.99")
	reqUntrusted.Header.Set("X-Real-IP", "203.0.113.99")
	reqUntrusted.Header.Set("CF-Connecting-IP", "203.0.113.99")
	reqUntrusted.Header.Set("CF-IPCountry", "SG")
	reqUntrusted.Header.Set("X-Visitor-ISP", "Forged ISP")
	resUntrusted := httptest.NewRecorder()
	handlerDefault.ServeHTTP(resUntrusted, reqUntrusted)

	if resUntrusted.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", resUntrusted.Code)
	}
	if cc := resUntrusted.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cc)
	}
	var infoUntrusted publicClientInfoResponse
	if err := json.Unmarshal(resUntrusted.Body.Bytes(), &infoUntrusted); err != nil {
		t.Fatal(err)
	}
	if infoUntrusted.IP != "198.51.100.42" {
		t.Fatalf("untrusted request IP = %q, want 198.51.100.42 (must ignore forged X-Forwarded-For)", infoUntrusted.IP)
	}
	if infoUntrusted.Connection != "IPv4" {
		t.Fatalf("connection = %q, want IPv4", infoUntrusted.Connection)
	}
	if infoUntrusted.Platform != "Windows" || infoUntrusted.Browser != "Firefox" || infoUntrusted.BrowserVersion != "131.0" {
		t.Fatalf("UA parsed = %+v, want Windows / Firefox / 131.0", infoUntrusted)
	}
	if infoUntrusted.Location != "" || infoUntrusted.ISP != "" || infoUntrusted.ASN != "" {
		t.Fatalf("untrusted geo headers must be ignored, got location=%q isp=%q asn=%q", infoUntrusted.Location, infoUntrusted.ISP, infoUntrusted.ASN)
	}
	if infoUntrusted.GeoSource != "unconfigured" {
		t.Fatalf("untrusted geo_source = %q, want unconfigured", infoUntrusted.GeoSource)
	}
	if _, err := time.Parse(time.RFC3339, infoUntrusted.VisitedAt); err != nil {
		t.Fatalf("visited_at %q is not RFC3339: %v", infoUntrusted.VisitedAt, err)
	}

	// 2. Untrusted RFC1918 private address (e.g. 10.20.30.40) when PROBEWATCH_TRUSTED_PROXY_CIDRS is unset/empty
	reqRFC1918 := httptest.NewRequest(http.MethodGet, "/api/public/client-info", nil)
	reqRFC1918.RemoteAddr = "10.20.30.40:43210"
	reqRFC1918.Header.Set("X-Real-IP", "203.0.113.55")
	reqRFC1918.Header.Set("X-Forwarded-For", "203.0.113.55")
	reqRFC1918.Header.Set("X-Visitor-Country", "SG")
	resRFC1918 := httptest.NewRecorder()
	handlerDefault.ServeHTTP(resRFC1918, reqRFC1918)
	var infoRFC1918 publicClientInfoResponse
	if err := json.Unmarshal(resRFC1918.Body.Bytes(), &infoRFC1918); err != nil {
		t.Fatal(err)
	}
	if infoRFC1918.IP != "10.20.30.40" {
		t.Fatalf("arbitrary RFC1918 must NOT be trusted by default, got IP = %q, want 10.20.30.40", infoRFC1918.IP)
	}

	// 3. Trusted loopback proxy with TrustProxyGeoHeaders enabled and IPv6 visitor
	cfgProxyHeaders := task4Config()
	cfgProxyHeaders.TrustProxyGeoHeaders = true
	handlerProxyHeaders := NewServer(cfgProxyHeaders, service).Handler()

	reqTrustedV6 := httptest.NewRequest(http.MethodGet, "/api/public/client-info", nil)
	reqTrustedV6.RemoteAddr = "127.0.0.1:43210"
	reqTrustedV6.Header.Set("X-Real-IP", "2001:db8::1234")
	reqTrustedV6.Header.Set("X-Visitor-Country", "HK")
	reqTrustedV6.Header.Set("X-Visitor-City", "Hong Kong")
	reqTrustedV6.Header.Set("X-Visitor-ISP", "Cloudflare")
	reqTrustedV6.Header.Set("X-Visitor-ASN", "13335")
	reqTrustedV6.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.6723.58 Safari/537.36")
	resTrustedV6 := httptest.NewRecorder()
	handlerProxyHeaders.ServeHTTP(resTrustedV6, reqTrustedV6)

	if resTrustedV6.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", resTrustedV6.Code)
	}
	var infoV6 publicClientInfoResponse
	if err := json.Unmarshal(resTrustedV6.Body.Bytes(), &infoV6); err != nil {
		t.Fatal(err)
	}
	if infoV6.IP != "2001:db8::1234" || infoV6.Connection != "IPv6" {
		t.Fatalf("trusted IPv6 = %q (%q), want 2001:db8::1234 (IPv6)", infoV6.IP, infoV6.Connection)
	}
	if infoV6.Platform != "macOS" || infoV6.Browser != "Chrome" || infoV6.BrowserVersion != "130.0.6723.58" {
		t.Fatalf("UA parsed = %+v, want macOS / Chrome / 130.0.6723.58", infoV6)
	}
	if infoV6.Location != "HK · Hong Kong" || infoV6.ISP != "Cloudflare" || infoV6.ASN != "AS13335" || infoV6.GeoSource != "proxy" {
		t.Fatalf("trusted geo meta = %+v, want HK · Hong Kong, Cloudflare, AS13335, proxy", infoV6)
	}

	// 4. Configured Docker Bridge CIDR (172.18.0.0/16) with EnableVisitorGeoIP enabled
	cfgDockerGeoIP := task4Config()
	cfgDockerGeoIP.TrustedProxyCIDRs = []string{"172.18.0.0/16"}
	cfgDockerGeoIP.EnableVisitorGeoIP = true
	serverDockerGeoIP := NewServer(cfgDockerGeoIP, service)
	serverDockerGeoIP.visitorGeoFetch = func(ctx context.Context, ip string) (visitorGeoData, error) {
		if ip == "125.80.81.51" {
			return visitorGeoData{
				Country:  "中国",
				Region:   "重庆市",
				City:     "渝中區",
				Location: "中国 · 重庆市 · 渝中區",
				ISP:      "CHINANET Chongqing Province Network",
				ASN:      "AS4134",
			}, nil
		}
		return visitorGeoData{}, errors.New("not found")
	}
	handlerDockerGeoIP := serverDockerGeoIP.Handler()

	reqDockerBridge := httptest.NewRequest(http.MethodGet, "/api/public/client-info", nil)
	reqDockerBridge.RemoteAddr = "172.18.0.1:54321" // Docker bridge gateway peer
	reqDockerBridge.Header.Set("X-Real-IP", "125.80.81.51")
	resDockerBridge := httptest.NewRecorder()
	handlerDockerGeoIP.ServeHTTP(resDockerBridge, reqDockerBridge)

	if resDockerBridge.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", resDockerBridge.Code)
	}
	var infoDocker publicClientInfoResponse
	if err := json.Unmarshal(resDockerBridge.Body.Bytes(), &infoDocker); err != nil {
		t.Fatal(err)
	}
	if infoDocker.IP != "125.80.81.51" {
		t.Fatalf("docker bridge IP = %q, want 125.80.81.51", infoDocker.IP)
	}
	if infoDocker.Location != "中国 · 重庆市 · 渝中區" || infoDocker.ISP != "CHINANET Chongqing Province Network" || infoDocker.ASN != "AS4134" {
		t.Fatalf("docker bridge geo = %+v, want Chinese location, ISP, and AS4134", infoDocker)
	}
	if infoDocker.GeoSource != "geoip" {
		t.Fatalf("docker bridge geo_source = %q, want geoip", infoDocker.GeoSource)
	}

	// 5. Lookup failure returns empty fields without fake data
	reqFailedGeo := httptest.NewRequest(http.MethodGet, "/api/public/client-info", nil)
	reqFailedGeo.RemoteAddr = "172.18.0.1:54322"
	reqFailedGeo.Header.Set("X-Real-IP", "192.0.2.99")
	resFailedGeo := httptest.NewRecorder()
	handlerDockerGeoIP.ServeHTTP(resFailedGeo, reqFailedGeo)
	var infoFailed publicClientInfoResponse
	if err := json.Unmarshal(resFailedGeo.Body.Bytes(), &infoFailed); err != nil {
		t.Fatal(err)
	}
	if infoFailed.Location != "" || infoFailed.ISP != "" || infoFailed.ASN != "" {
		t.Fatalf("failed geo lookup must return empty fields, got %+v", infoFailed)
	}
}
