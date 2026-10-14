package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/protocol"
)

func TestContainerAndWorkloadHandlers(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	server := NewServer(cfg, service)
	handler := server.Handler()
	session, csrf := task4AdminSession(t, service, store)

	// 1. Register Edge Node
	reg, _ := task4CreateRegistration(t, handler, session, csrf)
	regResp := task4Register(t, handler, protocol.RegisterRequest{
		RegistrationToken: reg.Token,
		NodeUUID:          task4NodeUUID1,
		Name:              "Edge-Docker-01",
	})

	now := time.Now().UTC()

	// 2. Report Workload from Edge Node
	workloadReport := protocol.NodeWorkloadReport{
		NodeUUID:          task4NodeUUID1,
		ReportedAt:        now.Unix(),
		DockerAvailable:   true,
		DockerVersion:     "24.0.7",
		ContainersTotal:   2,
		ContainersRunning: 1,
		ContainersStopped: 1,
		Containers: []protocol.ContainerSnapshot{
			{
				ID:               "c1a2b3c4d5e6",
				Names:            []string{"/redis-server"},
				Image:            "redis:7-alpine",
				State:            "running",
				Status:           "Up 5 hours",
				Health:           "healthy",
				CPUPercent:       2.5,
				MemoryUsageBytes: 64000000,
				MemoryLimitBytes: 512000000,
				MemoryPercent:    12.5,
				NetworkRxBytes:   1024000,
				NetworkTxBytes:   2048000,
				BlockReadBytes:   4096,
				BlockWriteBytes:  8192,
				PIDs:             5,
				Ports:            []string{"6379:6379/tcp"},
			},
			{
				ID:               "f7e8d9c0b1a2",
				Names:            []string{"/old-worker"},
				Image:            "worker:legacy",
				State:            "exited",
				Status:           "Exited (0) 2 hours ago",
				CPUPercent:       0.0,
				MemoryUsageBytes: 0,
				MemoryLimitBytes: 0,
				MemoryPercent:    0.0,
			},
		},
		TopProcesses: []protocol.ProcessSnapshot{
			{
				PID:            501,
				PPID:           1,
				Name:           "dockerd",
				User:           "root",
				State:          "S",
				CPUPercent:     1.1,
				MemoryRSSBytes: 85000000,
				MemoryPercent:  4.2,
				Threads:        14,
			},
			{
				PID:            1234,
				PPID:           501,
				Name:           "redis-server",
				User:           "redis",
				State:          "S",
				CPUPercent:     2.5,
				MemoryRSSBytes: 64000000,
				MemoryPercent:  3.1,
				Threads:        5,
			},
		},
	}

	reportBytes, _ := json.Marshal(workloadReport)
	reqReport := httptest.NewRequest(http.MethodPost, "/api/agent/v1/workload", bytes.NewReader(reportBytes))
	reqReport.Header.Set("Authorization", "Bearer "+regResp.NodeToken)
	reqReport.Header.Set("Content-Type", "application/json")
	recReport := httptest.NewRecorder()
	handler.ServeHTTP(recReport, reqReport)

	if recReport.Code != http.StatusNoContent {
		t.Fatalf("expected 204 No Content for workload report, got %d: %s", recReport.Code, recReport.Body.String())
	}

	// 3. Test Unauthorized Workload Report
	reqUnauthorized := httptest.NewRequest(http.MethodPost, "/api/agent/v1/workload", bytes.NewReader(reportBytes))
	reqUnauthorized.Header.Set("Authorization", "Bearer invalid-token")
	reqUnauthorized.Header.Set("Content-Type", "application/json")
	recUnauthorized := httptest.NewRecorder()
	handler.ServeHTTP(recUnauthorized, reqUnauthorized)
	if recUnauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", recUnauthorized.Code)
	}

	// 4. Query GET /api/nodes/{uuid}/containers
	reqContainers := httptest.NewRequest(http.MethodGet, "/api/nodes/"+task4NodeUUID1+"/containers", nil)
	reqContainers.AddCookie(task4SessionCookie(session))
	recContainers := httptest.NewRecorder()
	handler.ServeHTTP(recContainers, reqContainers)

	if recContainers.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for containers, got %d", recContainers.Code)
	}
	var contResp struct {
		NodeName        string `json:"node_name"`
		DockerAvailable bool   `json:"docker_available"`
		ContainersTotal int    `json:"containers_total"`
		Containers      []struct {
			ID         string  `json:"container_id"`
			Name       string  `json:"name"`
			Image      string  `json:"image"`
			State      string  `json:"state"`
			Health     string  `json:"health"`
			CPUPercent float64 `json:"cpu_percent"`
		} `json:"containers"`
	}
	if err := json.Unmarshal(recContainers.Body.Bytes(), &contResp); err != nil {
		t.Fatalf("unmarshal containers response: %v", err)
	}
	if !contResp.DockerAvailable || contResp.ContainersTotal != 2 {
		t.Fatalf("unexpected containers summary: %#v", contResp)
	}
	if len(contResp.Containers) != 2 || contResp.Containers[0].Name != "redis-server" {
		t.Fatalf("unexpected container list: %#v", contResp.Containers)
	}

	// 5. Query GET /api/nodes/{uuid}/processes
	reqProcs := httptest.NewRequest(http.MethodGet, "/api/nodes/"+task4NodeUUID1+"/processes", nil)
	reqProcs.AddCookie(task4SessionCookie(session))
	recProcs := httptest.NewRecorder()
	handler.ServeHTTP(recProcs, reqProcs)

	if recProcs.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for processes, got %d", recProcs.Code)
	}
	var procResp struct {
		NodeName     string                     `json:"node_name"`
		TopProcesses []protocol.ProcessSnapshot `json:"top_processes"`
	}
	if err := json.Unmarshal(recProcs.Body.Bytes(), &procResp); err != nil {
		t.Fatalf("unmarshal processes response: %v", err)
	}
	if len(procResp.TopProcesses) != 2 || procResp.TopProcesses[0].Name != "dockerd" {
		t.Fatalf("unexpected processes response: %#v", procResp)
	}

	// 6. Query Fleet Containers Overview
	reqOverview := httptest.NewRequest(http.MethodGet, "/api/containers/overview", nil)
	reqOverview.AddCookie(task4SessionCookie(session))
	recOverview := httptest.NewRecorder()
	handler.ServeHTTP(recOverview, reqOverview)

	if recOverview.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for fleet overview, got %d", recOverview.Code)
	}
	var fleetOverview struct {
		TotalNodes        int `json:"total_nodes"`
		NodesWithDocker   int `json:"nodes_with_docker"`
		TotalContainers   int `json:"total_containers"`
		RunningContainers int `json:"running_containers"`
		TopCPUContainers  []struct {
			Name       string  `json:"name"`
			CPUPercent float64 `json:"cpu_percent"`
		} `json:"top_cpu_containers"`
	}
	if err := json.Unmarshal(recOverview.Body.Bytes(), &fleetOverview); err != nil {
		t.Fatalf("unmarshal fleet overview: %v", err)
	}
	if fleetOverview.TotalNodes != 1 || fleetOverview.RunningContainers != 1 {
		t.Fatalf("unexpected fleet overview: %#v", fleetOverview)
	}
	if len(fleetOverview.TopCPUContainers) != 1 || fleetOverview.TopCPUContainers[0].Name != "redis-server" {
		t.Fatalf("unexpected top cpu containers: %#v", fleetOverview.TopCPUContainers)
	}

	// 7. Query Public Endpoints
	reqPubCont := httptest.NewRequest(http.MethodGet, "/api/public/nodes/"+task4NodeUUID1+"/containers", nil)
	recPubCont := httptest.NewRecorder()
	handler.ServeHTTP(recPubCont, reqPubCont)
	if recPubCont.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found for public containers, got %d", recPubCont.Code)
	}

	reqPubOver := httptest.NewRequest(http.MethodGet, "/api/public/containers/overview", nil)
	recPubOver := httptest.NewRecorder()
	handler.ServeHTTP(recPubOver, reqPubOver)
	if recPubOver.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for public containers overview, got %d", recPubOver.Code)
	}
}

func TestPublicFleetContainerOverviewWhitelistSecurity(t *testing.T) {
	service, store := newTask4Auth(t)
	defer store.Close()
	cfg := task4Config()
	server := NewServer(cfg, service)
	handler := server.Handler()
	session, csrf := task4AdminSession(t, service, store)

	// 1. Register Edge Node
	reg, _ := task4CreateRegistration(t, handler, session, csrf)
	regResp := task4Register(t, handler, protocol.RegisterRequest{
		RegistrationToken: reg.Token,
		NodeUUID:          task4NodeUUID1,
		Name:              "Edge-Docker-Security-Test",
	})

	now := time.Now().UTC()

	// 2. Report Workload with highly sensitive container fields
	sensitiveWorkload := protocol.NodeWorkloadReport{
		NodeUUID:          task4NodeUUID1,
		ReportedAt:        now.Unix(),
		DockerAvailable:   true,
		DockerVersion:     "26.1.0",
		ContainersTotal:   3,
		ContainersRunning: 2,
		ContainersStopped: 1,
		Containers: []protocol.ContainerSnapshot{
			{
				ID:               "container-secret-id-9988",
				Names:            []string{"/internal-billing-db"},
				Image:            "private-registry.corp/internal-billing:v2.1",
				State:            "running",
				Status:           "Up 12 hours",
				Health:           "healthy",
				CPUPercent:       35.5,
				MemoryUsageBytes: 256000000,
				MemoryLimitBytes: 1024000000,
				MemoryPercent:    25.0,
				NetworkRxBytes:   5000000,
				NetworkTxBytes:   10000000,
				BlockReadBytes:   16384,
				BlockWriteBytes:  32768,
				PIDs:             18,
				Ports:            []string{"127.0.0.1:5432:5432/tcp"},
			},
			{
				ID:               "container-api-id-1122",
				Names:            []string{"/production-auth-gateway"},
				Image:            "auth-gateway:latest",
				State:            "running",
				Status:           "Up 10 hours",
				Health:           "unhealthy",
				CPUPercent:       12.0,
				MemoryUsageBytes: 128000000,
				MemoryLimitBytes: 512000000,
				MemoryPercent:    25.0,
				PIDs:             8,
				Ports:            []string{"0.0.0.0:8443:8443/tcp"},
			},
			{
				ID:               "container-stopped-id-3344",
				Names:            []string{"/backup-worker"},
				Image:            "backup-job:v1.0",
				State:            "exited",
				Status:           "Exited (0) 1 hour ago",
			},
		},
		TopProcesses: []protocol.ProcessSnapshot{
			{
				PID:            888,
				PPID:           1,
				Name:           "super-secret-daemon",
				User:           "root",
				State:          "S",
				CPUPercent:     4.5,
				MemoryRSSBytes: 120000000,
				MemoryPercent:  5.0,
				Threads:        10,
			},
		},
	}

	reportBytes, _ := json.Marshal(sensitiveWorkload)
	reqReport := httptest.NewRequest(http.MethodPost, "/api/agent/v1/workload", bytes.NewReader(reportBytes))
	reqReport.Header.Set("Authorization", "Bearer "+regResp.NodeToken)
	reqReport.Header.Set("Content-Type", "application/json")
	recReport := httptest.NewRecorder()
	handler.ServeHTTP(recReport, reqReport)
	if recReport.Code != http.StatusNoContent {
		t.Fatalf("expected 204 No Content for workload report, got %d: %s", recReport.Code, recReport.Body.String())
	}

	// 3. Test Unauthenticated Public Endpoint: GET /api/public/containers/overview
	pubReq := httptest.NewRequest(http.MethodGet, "/api/public/containers/overview", nil)
	pubRec := httptest.NewRecorder()
	handler.ServeHTTP(pubRec, pubReq)
	if pubRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for public containers overview, got %d: %s", pubRec.Code, pubRec.Body.String())
	}

	var pubMap map[string]any
	if err := json.Unmarshal(pubRec.Body.Bytes(), &pubMap); err != nil {
		t.Fatalf("failed to unmarshal public containers overview JSON: %v", err)
	}

	// 4. Assert allowed aggregate statistical fields are present with correct values
	expectedNumericFields := []string{
		"total_nodes",
		"nodes_with_docker",
		"total_containers",
		"running_containers",
		"stopped_containers",
		"unhealthy_containers",
	}
	for _, field := range expectedNumericFields {
		val, exists := pubMap[field]
		if !exists {
			t.Errorf("missing expected aggregate statistical field %q in public overview", field)
		} else if _, ok := val.(float64); !ok {
			t.Errorf("expected numeric value for field %q, got %T (%v)", field, val, val)
		}
	}
	if totalContainers, _ := pubMap["total_containers"].(float64); totalContainers != 3 {
		t.Errorf("expected total_containers = 3, got %v", totalContainers)
	}
	if runningContainers, _ := pubMap["running_containers"].(float64); runningContainers != 2 {
		t.Errorf("expected running_containers = 2, got %v", runningContainers)
	}
	if stoppedContainers, _ := pubMap["stopped_containers"].(float64); stoppedContainers != 1 {
		t.Errorf("expected stopped_containers = 1, got %v", stoppedContainers)
	}

	// 5. Assert forbidden keys and sensitive workload details do NOT exist in the JSON map
	forbiddenKeys := []string{
		"top_cpu_containers",
		"top_memory_containers",
		"container_id",
		"container_name",
		"name",
		"image",
		"ports",
		"node_uuid",
		"hostname",
		"command",
		"mount",
		"environment",
		"token",
		"ip",
	}
	for _, key := range forbiddenKeys {
		if _, exists := pubMap[key]; exists {
			t.Errorf("SECURITY LEAK: forbidden key %q found in public containers overview response: %#v", key, pubMap)
		}
	}

	// Also verify raw body text does not leak container strings
	pubBody := pubRec.Body.String()
	sensitiveSubstrings := []string{
		"container-secret-id-9988",
		"internal-billing",
		"production-auth-gateway",
		"backup-worker",
		"auth-gateway",
		"5432",
		"8443",
		"super-secret-daemon",
		"top_cpu_containers",
		"top_memory_containers",
	}
	for _, sub := range sensitiveSubstrings {
		if strings.Contains(pubBody, sub) {
			t.Errorf("SECURITY LEAK: sensitive string %q found in public response body:\n%s", sub, pubBody)
		}
	}

	// 6. Test Admin Endpoint /api/containers/overview REMAINS FULL AND COMPLETE
	adminReq := httptest.NewRequest(http.MethodGet, "/api/containers/overview", nil)
	adminReq.AddCookie(task4SessionCookie(session))
	adminRec := httptest.NewRecorder()
	handler.ServeHTTP(adminRec, adminReq)
	if adminRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for admin containers overview, got %d: %s", adminRec.Code, adminRec.Body.String())
	}

	var adminMap map[string]any
	if err := json.Unmarshal(adminRec.Body.Bytes(), &adminMap); err != nil {
		t.Fatalf("failed to unmarshal admin containers overview JSON: %v", err)
	}
	// Admin MUST still have top_cpu_containers and top_memory_containers
	if _, exists := adminMap["top_cpu_containers"]; !exists {
		t.Errorf("admin containers overview must retain top_cpu_containers")
	}
	if _, exists := adminMap["top_memory_containers"]; !exists {
		t.Errorf("admin containers overview must retain top_memory_containers")
	}
	adminBody := adminRec.Body.String()
	if !strings.Contains(adminBody, "internal-billing") {
		t.Errorf("admin containers overview must contain internal-billing container details")
	}

	// 7. Test Non-GET Methods on /api/public/containers/overview Return 405 Method Not Allowed
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		mReq := httptest.NewRequest(method, "/api/public/containers/overview", nil)
		mRec := httptest.NewRecorder()
		handler.ServeHTTP(mRec, mReq)
		if mRec.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405 Method Not Allowed for %s /api/public/containers/overview, got %d", method, mRec.Code)
		}
	}

	// 8. Test Rate Limiter on /api/public/containers/overview (Returns 429 Too Many Requests)
	limitServer := NewServer(task4Config(), service)
	limitServer.publicLimiter = newRateLimiter(1, time.Minute, 32)
	limitHandler := limitServer.Handler()

	rlReq1 := httptest.NewRequest(http.MethodGet, "/api/public/containers/overview", nil)
	rlReq1.RemoteAddr = "198.51.100.55:12345"
	rlRec1 := httptest.NewRecorder()
	limitHandler.ServeHTTP(rlRec1, rlReq1)
	if rlRec1.Code != http.StatusOK {
		t.Fatalf("first request must succeed with 200, got %d", rlRec1.Code)
	}

	rlReq2 := httptest.NewRequest(http.MethodGet, "/api/public/containers/overview", nil)
	rlReq2.RemoteAddr = "198.51.100.55:12345"
	rlRec2 := httptest.NewRecorder()
	limitHandler.ServeHTTP(rlRec2, rlReq2)
	if rlRec2.Code != http.StatusTooManyRequests {
		t.Fatalf("second rate-limited request must return 429 Too Many Requests, got %d", rlRec2.Code)
	}

	// 9. Test Store Error on /api/public/containers/overview (Returns 503 Service Unavailable)
	errorService, errorStore := newTask4Auth(t)
	errorStore.Close() // Close store immediately to force database error
	errorServer := NewServer(task4Config(), errorService)
	errorHandler := errorServer.Handler()

	errReq := httptest.NewRequest(http.MethodGet, "/api/public/containers/overview", nil)
	errRec := httptest.NewRecorder()
	errorHandler.ServeHTTP(errRec, errReq)
	if errRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 Service Unavailable on store error, got %d: %s", errRec.Code, errRec.Body.String())
	}
}
