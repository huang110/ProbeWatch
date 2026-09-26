package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/protocol"
	"github.com/probewatch/probewatch/internal/security"
)

// workloadAgent handles edge node workload report ingestion (Docker containers & host top processes).
func (s *Server) workloadAgent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	token, ok := parseBearer(r.Header.Get("Authorization"))
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "authentication failed")
		return
	}
	now := time.Now().UTC()
	node, err := s.service.Store().AuthenticateNodeToken(r.Context(), token, now)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication failed")
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
	w.WriteHeader(http.StatusNoContent)
}

// getNodeContainers returns current containers running on the specified node.
func (s *Server) getNodeContainers(w http.ResponseWriter, r *http.Request, uuid string) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !security.IsRFC4122UUID(uuid) {
		writeJSONError(w, http.StatusNotFound, "node not found")
		return
	}
	node, err := s.service.Store().GetNodeByUUID(r.Context(), uuid)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "node not found")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "node unavailable")
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
	if !security.IsRFC4122UUID(uuid) {
		writeJSONError(w, http.StatusNotFound, "node not found")
		return
	}
	node, err := s.service.Store().GetNodeByUUID(r.Context(), uuid)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "node not found")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "node unavailable")
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

// publicFleetContainerOverviewHandler returns sanitized fleet container overview for guest dashboards.
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

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(overview)
}
