package api

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/security"
)

// SpeedtestStats summarizes fleet bandwidth benchmark metrics.
type SpeedtestStats struct {
	MaxDownloadMbps      float64 `json:"max_download_mbps"`
	MaxUploadMbps        float64 `json:"max_upload_mbps"`
	AvgDownloadMbps      float64 `json:"avg_download_mbps"`
	AvgUploadMbps        float64 `json:"avg_upload_mbps"`
	AvgLatencyMS         float64 `json:"avg_latency_ms"`
	ActiveBenchmarkNodes int     `json:"active_benchmark_nodes"`
	TotalBenchmarksCount int     `json:"total_benchmarks_count"`
}

// SpeedtestRankItem represents one node's placement on the bandwidth leaderboard.
type SpeedtestRankItem struct {
	Rank              int     `json:"rank"`
	NodeID            string  `json:"node_id"`
	NodeName          string  `json:"node_name"`
	DownloadSpeedMbps float64 `json:"download_speed_mbps"`
	UploadSpeedMbps   float64 `json:"upload_speed_mbps"`
	LatencyMS         int64   `json:"latency_ms"`
	JitterMS          int64   `json:"jitter_ms"`
	ServerName        string  `json:"server_name"`
	TestedAt          int64   `json:"tested_at"`
}

// SpeedtestResultsResponse is the full response for the speedtest dashboard.
type SpeedtestResultsResponse struct {
	Stats       SpeedtestStats             `json:"stats"`
	Rankings    []SpeedtestRankItem        `json:"rankings"`
	Results     []db.SpeedtestResultRecord `json:"results"`
	GeneratedAt int64                      `json:"generated_at"`
}

type speedtestTaskRequest struct {
	ID              string   `json:"id,omitempty"`
	Name            string   `json:"name"`
	ServerURL       string   `json:"server_url"`
	DownloadBytes   int64    `json:"download_bytes,omitempty"`
	UploadBytes     int64    `json:"upload_bytes,omitempty"`
	IntervalSeconds int      `json:"interval_seconds,omitempty"`
	NodeTags        []string `json:"node_tags,omitempty"`
	NodeIDs         []string `json:"node_ids,omitempty"`
	Enabled         *bool    `json:"enabled,omitempty"`
}

const (
	// These limits match the production runner's actual transfer caps. The
	// runner clamps larger values, but rejecting them here keeps the saved task
	// honest and prevents an administrator from accidentally creating a very
	// expensive benchmark that runs every few seconds on a small VPS.
	maxSpeedtestDownloadBytes = 50 * 1024 * 1024
	maxSpeedtestUploadBytes   = 20 * 1024 * 1024
	minSpeedtestIntervalSec   = 60
	maxSpeedtestIntervalSec   = 86_400
)

func validateSpeedtestSchedule(downloadBytes, uploadBytes int64, intervalSeconds int, requireAll bool) error {
	if downloadBytes < 0 || downloadBytes > maxSpeedtestDownloadBytes {
		return fmt.Errorf("download_bytes must be between 0 and %d", maxSpeedtestDownloadBytes)
	}
	if uploadBytes < 0 || uploadBytes > maxSpeedtestUploadBytes {
		return fmt.Errorf("upload_bytes must be between 0 and %d", maxSpeedtestUploadBytes)
	}
	if requireAll || intervalSeconds != 0 {
		if intervalSeconds < minSpeedtestIntervalSec || intervalSeconds > maxSpeedtestIntervalSec {
			return fmt.Errorf("interval_seconds must be between %d and %d", minSpeedtestIntervalSec, maxSpeedtestIntervalSec)
		}
	}
	return nil
}

func (s *Server) speedtestTasksRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.requireFleetWideNodeScope(w, r) {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/speedtest/tasks")
	path = strings.TrimPrefix(path, "/")

	mw := NewMiddleware(s.service, s.cfg)
	adminAction := mw.RequireRole(db.RoleAdmin)

	switch r.Method {
	case http.MethodGet:
		if path == "" {
			s.listSpeedtestTasks(w, r)
		} else {
			s.getSpeedtestTask(w, r, path)
		}
	case http.MethodPost:
		if path != "" {
			writeJSONError(w, http.StatusNotFound, "not found")
			return
		}
		adminAction(mw.RequireCSRF(http.HandlerFunc(s.createSpeedtestTask))).ServeHTTP(w, r)
	case http.MethodPatch:
		if path == "" {
			writeJSONError(w, http.StatusNotFound, "not found")
			return
		}
		adminAction(mw.RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s.updateSpeedtestTask(w, r, path)
		}))).ServeHTTP(w, r)
	case http.MethodDelete:
		if path == "" {
			writeJSONError(w, http.StatusNotFound, "not found")
			return
		}
		adminAction(mw.RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s.deleteSpeedtestTask(w, r, path)
		}))).ServeHTTP(w, r)
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) listSpeedtestTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.service.Store().ListSpeedtestTasks(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "speedtest tasks unavailable")
		return
	}
	writeJSON(w, http.StatusOK, tasks)
}

func (s *Server) getSpeedtestTask(w http.ResponseWriter, r *http.Request, id string) {
	task, err := s.service.Store().GetSpeedtestTask(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrTargetNotFound) {
			writeJSONError(w, http.StatusNotFound, "task not found")
			return
		}
		writeJSONError(w, http.StatusServiceUnavailable, "task unavailable")
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) createSpeedtestTask(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if ok && !user.CanWrite() {
		writeJSONError(w, http.StatusForbidden, "write permission required")
		return
	}
	var req speedtestTaskRequest
	if err := decodeJSONRequest(w, r, s.requestBodyLimit(), &req); err != nil {
		writeRequestError(w, err)
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.ServerURL) == "" {
		writeJSONError(w, http.StatusBadRequest, "name and server_url are required")
		return
	}
	if _, err := security.ValidateURLWithOptions(strings.TrimSpace(req.ServerURL), []string{"http", "https"}, true); err != nil {
		writeJSONError(w, http.StatusBadRequest, "server_url must be a valid safe http or https URL")
		return
	}

	taskID := strings.TrimSpace(req.ID)
	if taskID == "" {
		taskID = "speed-" + randomHex(8)
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	dlBytes := req.DownloadBytes
	if dlBytes <= 0 {
		dlBytes = 10 * 1024 * 1024
	}
	ulBytes := req.UploadBytes
	if err := validateSpeedtestSchedule(req.DownloadBytes, req.UploadBytes, 0, false); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if ulBytes <= 0 {
		ulBytes = 5 * 1024 * 1024
	}
	interval := req.IntervalSeconds
	if interval <= 0 {
		interval = 3600
	}
	if err := validateSpeedtestSchedule(dlBytes, ulBytes, interval, true); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	record := db.SpeedtestTaskRecord{
		ID:              taskID,
		Name:            strings.TrimSpace(req.Name),
		ServerURL:       strings.TrimSpace(req.ServerURL),
		DownloadBytes:   dlBytes,
		UploadBytes:     ulBytes,
		IntervalSeconds: interval,
		NodeTags:        strings.Join(req.NodeTags, ","),
		NodeIDs:         strings.Join(req.NodeIDs, ","),
		Enabled:         enabled,
	}

	if err := s.service.Store().CreateSpeedtestTask(r.Context(), record); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to create speedtest task: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, record)
}

func (s *Server) updateSpeedtestTask(w http.ResponseWriter, r *http.Request, id string) {
	user, ok := UserFromContext(r.Context())
	if ok && !user.CanWrite() {
		writeJSONError(w, http.StatusForbidden, "write permission required")
		return
	}
	existing, err := s.service.Store().GetSpeedtestTask(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrTargetNotFound) {
			writeJSONError(w, http.StatusNotFound, "task not found")
			return
		}
		writeJSONError(w, http.StatusServiceUnavailable, "task unavailable")
		return
	}

	var req speedtestTaskRequest
	if err := decodeJSONRequest(w, r, s.requestBodyLimit(), &req); err != nil {
		writeRequestError(w, err)
		return
	}

	if strings.TrimSpace(req.Name) != "" {
		existing.Name = strings.TrimSpace(req.Name)
	}
	if strings.TrimSpace(req.ServerURL) != "" {
		if _, err := security.ValidateURLWithOptions(strings.TrimSpace(req.ServerURL), []string{"http", "https"}, true); err != nil {
			writeJSONError(w, http.StatusBadRequest, "server_url must be a valid safe http or https URL")
			return
		}
		existing.ServerURL = strings.TrimSpace(req.ServerURL)
	}
	if err := validateSpeedtestSchedule(req.DownloadBytes, req.UploadBytes, 0, false); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.IntervalSeconds < 0 {
		writeJSONError(w, http.StatusBadRequest, "interval_seconds must not be negative")
		return
	}
	if req.DownloadBytes > 0 {
		existing.DownloadBytes = req.DownloadBytes
	}
	if req.UploadBytes > 0 {
		existing.UploadBytes = req.UploadBytes
	}
	if req.IntervalSeconds > 0 {
		existing.IntervalSeconds = req.IntervalSeconds
	}
	if err := validateSpeedtestSchedule(existing.DownloadBytes, existing.UploadBytes, existing.IntervalSeconds, true); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.NodeTags != nil {
		existing.NodeTags = strings.Join(req.NodeTags, ",")
	}
	if req.NodeIDs != nil {
		existing.NodeIDs = strings.Join(req.NodeIDs, ",")
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}

	if err := s.service.Store().UpdateSpeedtestTask(r.Context(), existing); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to update task: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, existing)
}

func (s *Server) deleteSpeedtestTask(w http.ResponseWriter, r *http.Request, id string) {
	user, ok := UserFromContext(r.Context())
	if ok && !user.CanWrite() {
		writeJSONError(w, http.StatusForbidden, "write permission required")
		return
	}
	if err := s.service.Store().DeleteSpeedtestTask(r.Context(), id); err != nil {
		if errors.Is(err, db.ErrTargetNotFound) {
			writeJSONError(w, http.StatusNotFound, "task not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "failed to delete task: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) speedtestResultsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.serveSpeedtestResults(w, r, false)
}

func (s *Server) publicSpeedtestResultsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.publicLimiter != nil && !s.publicLimiter.Allow(s.publicLimiterKey(r), time.Now().UTC()) {
		writeJSONError(w, http.StatusTooManyRequests, "too many requests")
		return
	}
	s.serveSpeedtestResults(w, r, true)
}

func (s *Server) serveSpeedtestResults(w http.ResponseWriter, r *http.Request, isPublic bool) {
	w.Header().Set("Cache-Control", "no-store")
	results, err := s.service.Store().ListLatestSpeedtestResults(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "results unavailable")
		return
	}
	adminAccess := false
	preserveIdentifiers := false
	if !isPublic {
		user, ok := s.authenticateUser(w, r)
		if !ok {
			return
		}
		fullAccess, _, allowedIDs, scopeErr := s.getUserNodeScope(r.Context(), user)
		if scopeErr != nil {
			writeJSONError(w, http.StatusServiceUnavailable, "results unavailable")
			return
		}
		// A fleet-wide node scope does not by itself grant access to raw agent
		// payloads. Only a fleet-wide administrator receives the complete private
		// response; a writable operator keeps identifiers needed by the rerun
		// action, while read-only users receive anonymous labels.
		adminAccess = user.IsAdmin() && fullAccess
		// Fleet-wide operators can trigger a rerun from the dashboard. Keep the
		// internal node reference for that action, but still strip the raw agent
		// payload and benchmark URL.
		preserveIdentifiers = user.CanWrite() && fullAccess
		if !fullAccess {
			filtered := make([]db.SpeedtestResultRecord, 0, len(results))
			for _, result := range results {
				if _, allowed := allowedIDs[result.NodeID]; allowed {
					filtered = append(filtered, result)
				}
			}
			results = filtered
		}
	}

	var maxDl, maxUl, sumDl, sumUl, sumLat float64
	uniqueNodes := make(map[string]struct{})
	validCount := 0

	for _, item := range results {
		uniqueNodes[item.NodeID] = struct{}{}
		if item.DownloadSpeedMbps > maxDl {
			maxDl = item.DownloadSpeedMbps
		}
		if item.UploadSpeedMbps > maxUl {
			maxUl = item.UploadSpeedMbps
		}
		if item.DownloadSpeedMbps > 0 || item.UploadSpeedMbps > 0 {
			sumDl += item.DownloadSpeedMbps
			sumUl += item.UploadSpeedMbps
			sumLat += float64(item.LatencyMS)
			validCount++
		}
	}

	stats := SpeedtestStats{
		MaxDownloadMbps:      math.Round(maxDl*100) / 100,
		MaxUploadMbps:        math.Round(maxUl*100) / 100,
		ActiveBenchmarkNodes: len(uniqueNodes),
		TotalBenchmarksCount: len(results),
	}
	if validCount > 0 {
		stats.AvgDownloadMbps = math.Round((sumDl/float64(validCount))*100) / 100
		stats.AvgUploadMbps = math.Round((sumUl/float64(validCount))*100) / 100
		stats.AvgLatencyMS = math.Round((sumLat/float64(validCount))*10) / 10
	}

	// Build rankings leaderboard (sorted by download speed desc)
	rankings := make([]SpeedtestRankItem, 0, len(results))
	for idx, item := range results {
		rankings = append(rankings, SpeedtestRankItem{
			Rank:              idx + 1,
			NodeID:            item.NodeID,
			NodeName:          item.NodeName,
			DownloadSpeedMbps: item.DownloadSpeedMbps,
			UploadSpeedMbps:   item.UploadSpeedMbps,
			LatencyMS:         item.LatencyMS,
			JitterMS:          item.JitterMS,
			ServerName:        item.TaskName,
			TestedAt:          item.TestedAt.Unix(),
		})
	}

	sort.Slice(rankings, func(i, j int) bool {
		return rankings[i].DownloadSpeedMbps > rankings[j].DownloadSpeedMbps
	})
	for i := range rankings {
		rankings[i].Rank = i + 1
	}

	resp := SpeedtestResultsResponse{
		Stats:       stats,
		Rankings:    rankings,
		Results:     results,
		GeneratedAt: time.Now().UTC().Unix(),
	}
	if isPublic {
		resp = sanitizePublicSpeedtestResults(resp)
	} else if !adminAccess {
		resp = sanitizeRestrictedSpeedtestResults(resp, preserveIdentifiers)
	}

	writeJSON(w, http.StatusOK, resp)
}

// sanitizeRestrictedSpeedtestResults removes task/node identifiers and raw
// agent payloads from node-scoped or read-only users. The payload contains the
// configured benchmark URL and may include endpoint-specific error details;
// neither is needed to render the metrics dashboard.
func sanitizeRestrictedSpeedtestResults(response SpeedtestResultsResponse, preserveIdentifiers bool) SpeedtestResultsResponse {
	if preserveIdentifiers {
		for i := range response.Results {
			response.Results[i].Payload = nil
		}
		return response
	}
	nodeLabels := make(map[string]string)
	nodeNames := make(map[string]string)
	taskLabels := make(map[string]string)
	nextNode := 1
	nextTask := 1
	labelNode := func(id string) (string, string) {
		key := id
		if key == "" {
			key = fmt.Sprintf("missing-node-%d", nextNode)
		}
		if label, ok := nodeLabels[key]; ok {
			return label, nodeNames[key]
		}
		label := fmt.Sprintf("node-%d", nextNode)
		name := fmt.Sprintf("Node %d", nextNode)
		nodeLabels[key] = label
		nodeNames[key] = name
		nextNode++
		return label, name
	}
	labelTask := func(id string) string {
		key := id
		if key == "" {
			key = fmt.Sprintf("missing-task-%d", nextTask)
		}
		if label, ok := taskLabels[key]; ok {
			return label
		}
		label := fmt.Sprintf("benchmark-%d", nextTask)
		taskLabels[key] = label
		nextTask++
		return label
	}

	results := make([]db.SpeedtestResultRecord, 0, len(response.Results))
	for _, item := range response.Results {
		nodeID, nodeName := labelNode(item.NodeID)
		taskID := labelTask(item.TaskID)
		item.NodeID = nodeID
		item.NodeName = nodeName
		item.TaskID = taskID
		item.TaskName = fmt.Sprintf("Benchmark %s", strings.TrimPrefix(taskID, "benchmark-"))
		item.Payload = nil
		results = append(results, item)
	}
	response.Results = results

	rankings := make([]SpeedtestRankItem, 0, len(response.Rankings))
	for _, item := range response.Rankings {
		nodeID, nodeName := labelNode(item.NodeID)
		rankings = append(rankings, SpeedtestRankItem{
			Rank:              item.Rank,
			NodeID:            nodeID,
			NodeName:          nodeName,
			DownloadSpeedMbps: item.DownloadSpeedMbps,
			UploadSpeedMbps:   item.UploadSpeedMbps,
			LatencyMS:         item.LatencyMS,
			JitterMS:          item.JitterMS,
			ServerName:        "Benchmark",
			TestedAt:          item.TestedAt,
		})
	}
	response.Rankings = rankings
	return response
}

// sanitizePublicSpeedtestResults keeps only anonymous benchmark rankings and
// fleet aggregates. Node/database IDs, operator-chosen names, task names and
// raw payloads are private infrastructure details and must not be exposed by
// the unauthenticated endpoint.
func sanitizePublicSpeedtestResults(response SpeedtestResultsResponse) SpeedtestResultsResponse {
	labels := make(map[string]string)
	publicRankings := make([]SpeedtestRankItem, 0, len(response.Rankings))
	for i, item := range response.Rankings {
		label, ok := labels[item.NodeID]
		if !ok {
			label = fmt.Sprintf("node-%d", len(labels)+1)
			labels[item.NodeID] = label
		}
		publicRankings = append(publicRankings, SpeedtestRankItem{
			Rank:              i + 1,
			NodeID:            label,
			NodeName:          fmt.Sprintf("Node %s", strings.TrimPrefix(label, "node-")),
			DownloadSpeedMbps: item.DownloadSpeedMbps,
			UploadSpeedMbps:   item.UploadSpeedMbps,
			LatencyMS:         item.LatencyMS,
			JitterMS:          item.JitterMS,
			ServerName:        "Benchmark",
			TestedAt:          item.TestedAt,
		})
	}
	response.Rankings = publicRankings
	// The public UI only needs rankings and aggregate cards. Returning an empty
	// results array avoids leaking task/node IDs or opaque agent payloads.
	response.Results = []db.SpeedtestResultRecord{}
	return response
}

func (s *Server) speedtestHistoryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	nodeID := strings.TrimSpace(r.URL.Query().Get("node_id"))
	taskID := strings.TrimSpace(r.URL.Query().Get("task_id"))
	limit := 100
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		if parsed, err := strconv.Atoi(rawLimit); err == nil && parsed > 0 {
			limit = parsed
			if limit > 500 {
				limit = 500
			}
		}
	}
	user, ok := s.authenticateUser(w, r)
	if !ok {
		return
	}
	fullAccess, _, allowedIDs, scopeErr := s.getUserNodeScope(r.Context(), user)
	if scopeErr != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "history unavailable")
		return
	}
	if !fullAccess && nodeID != "" {
		if _, allowed := allowedIDs[nodeID]; !allowed {
			writeJSON(w, http.StatusOK, []db.SpeedtestHistoryRecord{})
			return
		}
	}

	var history []db.SpeedtestHistoryRecord
	var err error
	if fullAccess {
		history, err = s.service.Store().ListSpeedtestHistory(r.Context(), nodeID, taskID, limit)
	} else if nodeID != "" {
		history, err = s.service.Store().ListSpeedtestHistoryForNodes(r.Context(), []string{nodeID}, taskID, limit)
	} else {
		nodeIDs := make([]string, 0, len(allowedIDs))
		for id := range allowedIDs {
			nodeIDs = append(nodeIDs, id)
		}
		history, err = s.service.Store().ListSpeedtestHistoryForNodes(r.Context(), nodeIDs, taskID, limit)
	}
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "history unavailable")
		return
	}
	if history == nil {
		history = []db.SpeedtestHistoryRecord{}
	}
	if !fullAccess || !user.IsAdmin() {
		writeJSON(w, http.StatusOK, sanitizeSpeedtestHistory(history))
		return
	}
	writeJSON(w, http.StatusOK, history)
}

type restrictedSpeedtestHistoryRecord struct {
	NodeName          string    `json:"node_name"`
	TaskName          string    `json:"task_name"`
	DownloadSpeedMbps float64   `json:"download_speed_mbps"`
	UploadSpeedMbps   float64   `json:"upload_speed_mbps"`
	LatencyMS         int64     `json:"latency_ms"`
	JitterMS          int64     `json:"jitter_ms"`
	Status            string    `json:"status,omitempty"`
	TestedAt          time.Time `json:"tested_at"`
	RecordedAt        time.Time `json:"recorded_at"`
}

func sanitizeSpeedtestHistory(history []db.SpeedtestHistoryRecord) []restrictedSpeedtestHistoryRecord {
	result := make([]restrictedSpeedtestHistoryRecord, 0, len(history))
	nodeLabels := make(map[string]string)
	taskLabels := make(map[string]string)
	for _, item := range history {
		nodeKey := item.NodeID
		if nodeKey == "" {
			nodeKey = fmt.Sprintf("missing-node-%d", len(nodeLabels)+1)
		}
		nodeLabel, ok := nodeLabels[nodeKey]
		if !ok {
			nodeLabel = fmt.Sprintf("Node %d", len(nodeLabels)+1)
			nodeLabels[nodeKey] = nodeLabel
		}
		taskKey := item.TaskID
		if taskKey == "" {
			taskKey = fmt.Sprintf("missing-task-%d", len(taskLabels)+1)
		}
		taskLabel, ok := taskLabels[taskKey]
		if !ok {
			taskLabel = fmt.Sprintf("Benchmark %d", len(taskLabels)+1)
			taskLabels[taskKey] = taskLabel
		}
		result = append(result, restrictedSpeedtestHistoryRecord{
			NodeName:          nodeLabel,
			TaskName:          taskLabel,
			DownloadSpeedMbps: item.DownloadSpeedMbps,
			UploadSpeedMbps:   item.UploadSpeedMbps,
			LatencyMS:         item.LatencyMS,
			JitterMS:          item.JitterMS,
			Status:            item.Status,
			TestedAt:          item.TestedAt,
			RecordedAt:        item.RecordedAt,
		})
	}
	return result
}

func (s *Server) speedtestRunHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.requireFleetWideNodeScope(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	user, ok := UserFromContext(r.Context())
	if ok && !user.CanWrite() {
		writeJSONError(w, http.StatusForbidden, "write permission required")
		return
	}

	var req struct {
		NodeID    string `json:"node_id,omitempty"`
		TaskID    string `json:"task_id,omitempty"`
		ServerURL string `json:"server_url,omitempty"`
	}
	// The trigger body is optional, but malformed or oversized JSON must not be
	// silently accepted as a successful trigger. ContentLength == 0 is the
	// intentional no-body form; chunked requests are decoded and validated.
	if r.ContentLength != 0 {
		if err := decodeJSONRequest(w, r, s.requestBodyLimit(), &req); err != nil {
			writeRequestError(w, err)
			return
		}
	}

	// If no speedtest task exists at all, ensure at least one default preset task exists
	tasks, err := s.service.Store().ListSpeedtestTasks(r.Context())
	if err == nil && len(tasks) == 0 {
		_ = s.service.Store().CreateSpeedtestTask(r.Context(), db.SpeedtestTaskRecord{
			ID:              "speed-default-cloudflare",
			Name:            "Cloudflare Global Edge",
			ServerURL:       "https://speed.cloudflare.com/__down?bytes=10485760",
			DownloadBytes:   10 * 1024 * 1024,
			UploadBytes:     5 * 1024 * 1024,
			IntervalSeconds: 3600,
			Enabled:         true,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"message": "Speedtest benchmark triggered. Edge nodes will execute on next check cycle.",
	})
}
