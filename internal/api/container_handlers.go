package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/protocol"
)

// workloadAgent handles edge node workload report ingestion (Docker containers & host top processes).
func (s *Server) workloadAgent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	node, requestID, now, ok := s.authenticateAgentRequest(w, r)
	if !ok {
		return
	}
	var report protocol.NodeWorkloadReport
	if err := decodeJSONRequest(w, r, s.requestBodyLimit(), &report); err != nil {
		writeRequestError(w, err)
		return
	}
	if err := report.Validate(); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid workload report: "+err.Error())
		return
	}
	if report.NodeUUID != node.UUID {
		writeJSONError(w, http.StatusBadRequest, "node uuid mismatch")
		return
	}
	if err := s.service.Store().SaveNodeWorkload(r.Context(), node.ID, report); err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "workload save failed")
		return
	}
	if requestID != "" {
		s.agentReplayCache.Add(node.ID, requestID, now.Add(2*s.agentClockSkew()))
	}
	w.WriteHeader(http.StatusNoContent)
}

// getNodeContainers returns current containers running on the specified node.
func (s *Server) getNodeContainers(w http.ResponseWriter, r *http.Request, uuid string) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	node, ok := s.authorizeNodeScope(w, r, uuid)
	if !ok {
		return
	}

	containers, err := s.service.Store().ListNodeContainers(r.Context(), node.ID)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "containers unavailable")
		return
	}
	if containers == nil {
		containers = make([]db.NodeContainerRecord, 0)
	}

	workload, _ := s.service.Store().GetNodeWorkload(r.Context(), node.ID)

	response := map[string]any{
		"node_id":         node.ID,
		"node_uuid":       node.UUID,
		"node_name":       node.Name,
		"docker_available": false,
		"docker_version":   "",
		"containers_total": 0,
		"containers_running": 0,
		"containers_stopped": 0,
		"containers":      containers,
	}

	if workload != nil {
		response["docker_available"] = workload.DockerAvailable
		response["docker_version"] = workload.DockerVersion
		response["containers_total"] = workload.ContainersTotal
		response["containers_running"] = workload.ContainersRunning
		response["containers_stopped"] = workload.ContainersStopped
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// getNodeProcesses returns the host's top processes snapshot.
func (s *Server) getNodeProcesses(w http.ResponseWriter, r *http.Request, uuid string) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	node, ok := s.authorizeNodeScope(w, r, uuid)
	if !ok {
		return
	}

	workload, err := s.service.Store().GetNodeWorkload(r.Context(), node.ID)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "workload unavailable")
		return
	}

	response := map[string]any{
		"node_id":       node.ID,
		"node_uuid":     node.UUID,
		"node_name":     node.Name,
		"top_processes": make([]protocol.ProcessSnapshot, 0),
		"reported_at":   0,
	}

	if workload != nil {
		if workload.TopProcesses != nil {
			response["top_processes"] = workload.TopProcesses
		}
		response["reported_at"] = workload.ReportedAt.Unix()
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// fleetContainerOverviewHandler returns aggregated container and workload stats across all nodes.
func (s *Server) fleetContainerOverviewHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	overview, err := s.service.Store().GetFleetContainerOverview(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "fleet overview unavailable")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(overview)
}

// publicFleetContainerOverviewResponse is the strict allow-listed shape for the guest container overview.
// Container names, IDs, images, ports, and resource consumption details are strictly omitted.
type publicFleetContainerOverviewResponse struct {
	TotalNodes          int `json:"total_nodes"`
	NodesWithDocker     int `json:"nodes_with_docker"`
	TotalContainers     int `json:"total_containers"`
	RunningContainers   int `json:"running_containers"`
	StoppedContainers   int `json:"stopped_containers"`
	UnhealthyContainers int `json:"unhealthy_containers"`
}

// publicFleetContainerOverviewHandler returns sanitized fleet container overview for guest dashboards.
// Sensitive workload details such as container names, IDs, images, ports, and top resource consumers
// are strictly omitted from the public response.
func (s *Server) publicFleetContainerOverviewHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	now := time.Now().UTC()
	if !s.publicLimiter.Allow(publicLimiterKey(r), now) {
		writeJSONError(w, http.StatusTooManyRequests, "too many requests")
		return
	}

	overview, err := s.service.Store().GetFleetContainerOverview(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "fleet overview unavailable")
		return
	}

	res := publicFleetContainerOverviewResponse{
		TotalNodes:          overview.TotalNodes,
		NodesWithDocker:     overview.NodesWithDocker,
		TotalContainers:     overview.TotalContainers,
		RunningContainers:   overview.RunningContainers,
		StoppedContainers:   overview.StoppedContainers,
		UnhealthyContainers: overview.UnhealthyContainers,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}
