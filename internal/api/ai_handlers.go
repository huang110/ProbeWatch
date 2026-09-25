package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/probewatch/probewatch/internal/ai"
)

func (s *Server) aiDiagnose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.aiService == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "AI service not initialized"})
		return
	}

	report, err := s.aiService.RunDiagnosis(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to run diagnosis: %v", err)})
		return
	}

	writeJSON(w, http.StatusOK, report)
}

func (s *Server) aiChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.aiService == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "AI service not initialized"})
		return
	}

	var req struct {
		Prompt string `json:"prompt"`
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read request body"})
		return
	}
	if err := json.Unmarshal(body, &req); err != nil || strings.TrimSpace(req.Prompt) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid prompt"})
		return
	}

	resp, err := s.aiService.Chat(r.Context(), strings.TrimSpace(req.Prompt))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to process chat: %v", err)})
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) aiSettingsRoute(w http.ResponseWriter, r *http.Request) {
	if s.aiService == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "AI service not initialized"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		settings, err := s.aiService.GetAISettings(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		// Mask API Key
		if settings.HasAPIKey {
			settings.APIKey = "••••••••••••••••"
		}
		writeJSON(w, http.StatusOK, settings)

	case http.MethodPost:
		NewMiddleware(s.service, s.cfg).RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var settings ai.AISettings
			body, err := io.ReadAll(io.LimitReader(r.Body, 16*1024))
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read body failed"})
				return
			}
			if err := json.Unmarshal(body, &settings); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
				return
			}

			if err := s.aiService.SaveAISettings(r.Context(), settings); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}

			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})).ServeHTTP(w, r)

	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) aiMCPTokenRegenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.aiService == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "AI service not initialized"})
		return
	}

	token, err := s.aiService.RegenerateMCPToken(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

func (s *Server) mcpConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.aiService == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "AI service not initialized"})
		return
	}

	token, err := s.aiService.GetOrGenerateMCPToken(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	baseURL := s.cfg.PublicBaseURL
	if baseURL == "" {
		proto := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			proto = "https"
		}
		baseURL = fmt.Sprintf("%s://%s", proto, r.Host)
	}
	baseURL = strings.TrimRight(baseURL, "/")

	mcpEndpoint := fmt.Sprintf("%s/mcp?token=%s", baseURL, token)

	claudeConfig := map[string]any{
		"mcpServers": map[string]any{
			"probewatch": map[string]any{
				"url": mcpEndpoint,
			},
		},
	}

	cursorConfig := map[string]any{
		"mcpServers": map[string]any{
			"probewatch": map[string]any{
				"type": "sse",
				"url":  mcpEndpoint,
			},
		},
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"token":         token,
		"endpoint":      mcpEndpoint,
		"claude_config": claudeConfig,
		"cursor_config": cursorConfig,
	})
}
