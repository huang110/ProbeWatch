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

func TestPublicNodeDetailWhitelistSecurity(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	handler := NewServer(task4Config(), service).Handler()
	now := time.Now().UTC().Truncate(time.Second)

	// 1. Register a valid node
	token, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	reg := task4Register(t, handler, protocol.RegisterRequest{
		NodeUUID:          task4NodeUUID1,
		Name:              "production-edge-node",
		RegistrationToken: token.Token,
	})
	node := mustNode(t, store, reg.NodeUUID)

	// 2. Persist a rich agent resource payload containing both allowed public fields
	// and highly sensitive fields (credentials, internal IPs, MACs, processes, sockets, port maps).
	richPayload := `{
		"cpu_percent": 42.5,
		"cpu_model": "Intel(R) Xeon(R) CPU E5-2680 v3",
		"cpu_cores": 8,
		"cpu_mhz": 2499.98,
		"os": "linux",
		"kernel": "7.0.0-31-generic",
		"arch": "amd64",
		"memory_total_bytes": 17179869184,
		"memory_used_bytes": 8589934592,
		"filesystem_total_bytes": 107374182400,
		"filesystem_used_bytes": 32212254720,
		"network_rx_bytes": 104857600,
		"network_tx_bytes": 209715200,
		"started_at": 1700000000,
		"hostname": "prod-internal-hypervisor-01.corp",
		"token": "super-secret-node-token-12345",
		"agent_token": "top-secret-agent-token",
		"password": "node-root-password-999",
		"secret": "ultra-secret-key-888",
		"mac": "00:1A:2B:3C:4D:5E",
		"ipv4": "203.0.113.195",
		"ipv6": "2001:db8::cafe:1",
		"interfaces": [
			{"name": "eth0", "ipv4": "192.168.1.100", "ipv6": "fe80::1", "mac": "52:54:00:12:34:56"}
		],
		"listening_ports": [
			{"proto": "tcp", "port": 22, "bind_ip": "0.0.0.0", "process": "sshd", "pid": 1024, "is_public": true}
		],
		"socket_stats": {
			"tcp_established": 10,
			"tcp_listen": 5,
			"tcp_time_wait": 2,
			"tcp_close_wait": 0,
			"tcp_total": 17,
			"udp_total": 4
		},
		"top_processes": [
			{"pid": 1, "name": "systemd", "user": "root"}
		],
		"disks": [
			{"device": "vda", "read_bytes_per_sec": 1024, "write_bytes_per_sec": 2048, "read_iops": 10, "write_iops": 20}
		],
		"ip_quality": {
			"ip_type": "DataCenter",
			"country": "US",
			"region": "California",
			"asn": "AS15169",
			"organization": "Google LLC",
			"risk": "low"
		},
		"health_info": {
			"health_score": 95,
			"health_status": "optimal",
			"reboot_required": false,
			"security_updates": 0,
			"total_updates": 2
		}
	}`
	if err := store.UpsertResourceLatest(context.Background(), node.ID, now, []byte(richPayload)); err != nil {
		t.Fatal(err)
	}

	if err := store.CreateMediaDetector(context.Background(), db.ResultTargetInput{ID: "netflix", Name: "Netflix", Kind: "media", Host: "netflix.com"}, now); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateNetworkTarget(context.Background(), db.ResultTargetInput{ID: "tcp-cf", Name: "Cloudflare", Kind: string(db.TargetKindTCP), Host: "1.1.1.1"}, now); err != nil {
		t.Fatal(err)
	}

	mediaPayload := `{"detector": "netflix", "status": "available", "region": "US", "latency_ms": 120, "reason": ""}`
	if err := store.UpsertMediaLatest(context.Background(), node.ID, "netflix", now, []byte(mediaPayload)); err != nil {
		t.Fatal(err)
	}
	netPayload := `{"status": "success", "latency_ms": 25, "remote_addr": "203.0.113.195:443"}`
	if err := store.UpsertNetworkLatest(context.Background(), node.ID, "tcp-cf", now, []byte(netPayload)); err != nil {
		t.Fatal(err)
	}
	if err := store.PersistAgentResult(context.Background(), node.ID, "req-1", now.Add(time.Hour), now, db.AgentResultInput{
		Kind: db.TargetKindTCP, TargetID: "tcp-cf", CheckedAt: now, Payload: []byte(netPayload),
	}); err != nil {
		t.Fatal(err)
	}

	// 3. Test GET /api/public/nodes/:uuid/detail without authentication
	req := httptest.NewRequest(http.MethodGet, "/api/public/nodes/"+node.UUID+"/detail", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK without authentication, got %d: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()

	// 4. Verify sensitive fields do NOT appear in the JSON
	forbiddenStrings := []string{
		"super-secret-node-token-12345",
		"top-secret-agent-token",
		"node-root-password-999",
		"ultra-secret-key-888",
		"prod-internal-hypervisor-01.corp",
		"00:1A:2B:3C:4D:5E",
		"52:54:00:12:34:56",
		"203.0.113.195",
		"2001:db8::cafe:1",
		"\"listening_ports\"",
		"\"socket_stats\"",
		"\"top_processes\"",
		"\"interfaces\"",
		"\"password\"",
		"\"secret\"",
		"\"vda\"",
	}
	for _, forbidden := range forbiddenStrings {
		if strings.Contains(body, forbidden) {
			t.Errorf("SECURITY LEAK: public response contains forbidden text %q\nResponse body: %s", forbidden, body)
		}
	}

	// 5. Verify allowed fields ARE present and structured correctly
	var parsed publicNodeDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("failed to unmarshal public response: %v", err)
	}
	if parsed.UUID != node.UUID {
		t.Errorf("expected UUID %s, got %s", node.UUID, parsed.UUID)
	}
	if parsed.Resource == nil {
		t.Fatalf("expected resource object, got nil")
	}
	if parsed.Resource.CPUModel != "Intel(R) Xeon(R) CPU E5-2680 v3" {
		t.Errorf("unexpected cpu model: %s", parsed.Resource.CPUModel)
	}
	if parsed.Resource.CPUCores != 8 {
		t.Errorf("expected cpu cores 8, got %d", parsed.Resource.CPUCores)
	}
	if parsed.Resource.OS != "linux" {
		t.Errorf("expected OS linux, got %s", parsed.Resource.OS)
	}
	if parsed.Resource.Kernel != "7.0.0-31-generic" {
		t.Errorf("expected kernel 7.0.0-31-generic, got %s", parsed.Resource.Kernel)
	}
	if parsed.IPQuality == nil || parsed.IPQuality.ASN != "AS15169" {
		t.Errorf("expected ip quality ASN AS15169, got %+v", parsed.IPQuality)
	}
	if parsed.HealthInfo == nil || parsed.HealthInfo.HealthScore != 95 {
		t.Errorf("expected health score 95, got %+v", parsed.HealthInfo)
	}

	// Verify no duplicated fields inside Resource
	var rawDetail map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &rawDetail); err == nil {
		var rawRes map[string]json.RawMessage
		if err := json.Unmarshal(rawDetail["resource"], &rawRes); err == nil {
			if _, exists := rawRes["ip_quality"]; exists {
				t.Errorf("DESIGN DEFECT: duplicate ip_quality found inside resource map")
			}
			if _, exists := rawRes["health_info"]; exists {
				t.Errorf("DESIGN DEFECT: duplicate health_info found inside resource map")
			}
		}
	}

	// 6. Test GET /api/public/nodes/:uuid/resource/history
	hReq := httptest.NewRequest(http.MethodGet, "/api/public/nodes/"+node.UUID+"/resource/history?range=1h&limit=10", nil)
	hRec := httptest.NewRecorder()
	handler.ServeHTTP(hRec, hReq)
	if hRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for public resource history, got %d: %s", hRec.Code, hRec.Body.String())
	}
	hBody := hRec.Body.String()
	for _, forbidden := range forbiddenStrings {
		if strings.Contains(hBody, forbidden) {
			t.Errorf("SECURITY LEAK in history: public history response contains forbidden text %q\nResponse body: %s", forbidden, hBody)
		}
	}
	var historyList []publicHistoryResourceResponse
	if err := json.Unmarshal(hRec.Body.Bytes(), &historyList); err != nil {
		t.Fatalf("failed to unmarshal public history response: %v", err)
	}
	if len(historyList) == 0 {
		t.Fatalf("expected non-empty history list")
	}
	if historyList[0].Resource.CPUModel != "Intel(R) Xeon(R) CPU E5-2680 v3" {
		t.Errorf("unexpected cpu model in history: %s", historyList[0].Resource.CPUModel)
	}
	if len(historyList[0].Resource.Disks) == 0 || historyList[0].Resource.Disks[0].ReadBytesPerSec != 1024 {
		t.Errorf("expected sanitized disk stats with read rate 1024, got %+v", historyList[0].Resource.Disks)
	}

	// 7. Test GET /api/public/nodes/:uuid/media
	mReq := httptest.NewRequest(http.MethodGet, "/api/public/nodes/"+node.UUID+"/media", nil)
	mRec := httptest.NewRecorder()
	handler.ServeHTTP(mRec, mReq)
	if mRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for media, got %d: %s", mRec.Code, mRec.Body.String())
	}
	mBody := mRec.Body.String()
	for _, forbidden := range forbiddenStrings {
		if strings.Contains(mBody, forbidden) {
			t.Errorf("SECURITY LEAK in media: %q found in %s", forbidden, mBody)
		}
	}

	// 8. Test GET /api/public/nodes/:uuid/checks/summary
	cReq := httptest.NewRequest(http.MethodGet, "/api/public/nodes/"+node.UUID+"/checks/summary", nil)
	cRec := httptest.NewRecorder()
	handler.ServeHTTP(cRec, cReq)
	if cRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for checks/summary, got %d: %s", cRec.Code, cRec.Body.String())
	}
	cBody := cRec.Body.String()
	for _, forbidden := range forbiddenStrings {
		if strings.Contains(cBody, forbidden) {
			t.Errorf("SECURITY LEAK in checks/summary: %q found in %s", forbidden, cBody)
		}
	}

	// 9. Test GET /api/public/nodes/:uuid/network/history
	nReq := httptest.NewRequest(http.MethodGet, "/api/public/nodes/"+node.UUID+"/network/history?range=1h&limit=10", nil)
	nRec := httptest.NewRecorder()
	handler.ServeHTTP(nRec, nReq)
	if nRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for network/history, got %d: %s", nRec.Code, nRec.Body.String())
	}
	nBody := nRec.Body.String()
	for _, forbidden := range forbiddenStrings {
		if strings.Contains(nBody, forbidden) {
			t.Errorf("SECURITY LEAK in network/history: %q found in %s", forbidden, nBody)
		}
	}
}

func TestPublicNodeRouteScopeAndBypassRejection(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	handler := NewServer(task4Config(), service).Handler()

	token, err := store.CreateRegistrationToken(context.Background(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	reg := task4Register(t, handler, protocol.RegisterRequest{
		NodeUUID:          task4NodeUUID1,
		Name:              "primary-target",
		RegistrationToken: token.Token,
	})
	node := mustNode(t, store, reg.NodeUUID)

	testCases := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{"Valid UUID detail", "/api/public/nodes/" + node.UUID + "/detail", http.StatusOK},
		{"Valid UUID root path", "/api/public/nodes/" + node.UUID, http.StatusOK},
		{"Non-existent UUID", "/api/public/nodes/00000000-0000-0000-0000-000000000000/detail", http.StatusNotFound},
		{"Name matching bypass forbidden", "/api/public/nodes/primary-target/detail", http.StatusNotFound},
		{"guest- prefix fallback forbidden", "/api/public/nodes/guest-primary-target/detail", http.StatusNotFound},
		{"Path traversal rejected", "/api/public/nodes/..%2f..%2fetc%2fpasswd/detail", http.StatusNotFound},
		{"Non-UUID string rejected", "/api/public/nodes/not-a-valid-uuid/detail", http.StatusNotFound},
		{"Protected containers endpoint hidden from public", "/api/public/nodes/" + node.UUID + "/containers", http.StatusNotFound},
		{"Protected processes endpoint hidden from public", "/api/public/nodes/" + node.UUID + "/processes", http.StatusNotFound},
		{"Protected events endpoint hidden from public", "/api/public/nodes/" + node.UUID + "/events", http.StatusNotFound},
		{"Protected billing endpoint hidden from public", "/api/public/nodes/" + node.UUID + "/billing", http.StatusNotFound},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Errorf("%s: expected status %d, got %d (%s)", tc.name, tc.wantStatus, rec.Code, rec.Body.String())
			}
		})
	}
}
