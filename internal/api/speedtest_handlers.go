package api

import (
	"errors"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/db"
)

// SpeedtestStats summarizes fleet bandwidth benchmark metrics.
type SpeedtestStats struct {
	MaxDownloadMbps       float64 `json:"max_download_mbps"`
	MaxUploadMbps         float64 `json:"max_upload_mbps"`
	AvgDownloadMbps       float64 `json:"avg_download_mbps"`
	AvgUploadMbps         float64 `json:"avg_upload_mbps"`
	AvgLatencyMS          float64 `json:"avg_latency_ms"`
	ActiveBenchmarkNodes  int     `json:"active_benchmark_nodes"`
	TotalBenchmarksCount  int     `json:"total_benchmarks_count"`
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
	Stats       SpeedtestStats          `json:"stats"`
	Rankings    []SpeedtestRankItem     `json:"rankings"`
	Results     []db.SpeedtestResultRecord `json:"results"`
	GeneratedAt int64                   `json:"generated_at"`
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

func (s *Server) speedtestTasksRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	path := strings.TrimPrefix(r.URL.Path, "/api/speedtest/tasks")
	path = strings.TrimPrefix(path, "/")

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
		s.createSpeedtestTask(w, r)
	case http.MethodPatch:
		if path == "" {
			writeJSONError(w, http.StatusNotFound, "not found")
			return
		}
		s.updateSpeedtestTask(w, r, path)
	case http.MethodDelete:
		if path == "" {
			writeJSONError(w, http.StatusNotFound, "not found")
			return
		}
		s.deleteSpeedtestTask(w, r, path)
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
	u, err := url.Parse(strings.TrimSpace(req.ServerURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		writeJSONError(w, http.StatusBadRequest, "server_url must be a valid http or https URL")
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
	if ulBytes <= 0 {
		ulBytes = 5 * 1024 * 1024
	}
	interval := req.IntervalSeconds
	if interval <= 0 {
		interval = 3600
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
		u, err := url.Parse(strings.TrimSpace(req.ServerURL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			writeJSONError(w, http.StatusBadRequest, "server_url must be a valid http or https URL")
			return
		}
		existing.ServerURL = strings.TrimSpace(req.ServerURL)
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
	s.serveSpeedtestResults(w, r, true)
}

func (s *Server) serveSpeedtestResults(w http.ResponseWriter, r *http.Request, isPublic bool) {
	w.Header().Set("Cache-Control", "no-store")
	results, err := s.service.Store().ListLatestSpeedtestResults(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "results unavailable")
		return
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

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) speedtestHistoryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	nodeID := strings.TrimSpace(r.URL.Query().Get("node_id"))
	taskID := strings.TrimSpace(r.URL.Query().Get("task_id"))

	history, err := s.service.Store().ListSpeedtestHistory(r.Context(), nodeID, taskID, 100)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "history unavailable")
		return
	}
	writeJSON(w, http.StatusOK, history)
}

func (s *Server) speedtestRunHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
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
	_ = decodeJSONRequest(w, r, s.requestBodyLimit(), &req)

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
