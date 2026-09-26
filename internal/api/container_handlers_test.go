package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	if recPubCont.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for public containers, got %d", recPubCont.Code)
	}

	reqPubOver := httptest.NewRequest(http.MethodGet, "/api/public/containers/overview", nil)
	recPubOver := httptest.NewRecorder()
	handler.ServeHTTP(recPubOver, reqPubOver)
	if recPubOver.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for public containers overview, got %d", recPubOver.Code)
	}
}
