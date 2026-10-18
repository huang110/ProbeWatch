package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/version"
)

func (s *Server) mcpHandler(w http.ResponseWriter, r *http.Request) {
	if s.mcpLimiter != nil && !s.mcpLimiter.Allow(publicLimiterKey(r), time.Now().UTC()) {
		writeJSONError(w, http.StatusTooManyRequests, "too many requests")
		return
	}
	if s.aiService == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"jsonrpc": "2.0",
			"error": map[string]any{
				"code":    -32000,
				"message": "AI & MCP service unavailable",
			},
		})
		return
	}

	// 1. Authenticate Request: Authorization header or Admin session cookie.
	// Query-string tokens are deliberately rejected because URLs are copied to
	// access logs, browser history, proxy logs, and referrer headers.
	if r.URL.Query().Get("token") != "" {
		writeJSONError(w, http.StatusUnauthorized, "query-string MCP tokens are not supported; use Authorization: Bearer")
		return
	}
	token := ""
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		token = strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	}

	authenticated := false
	fullAccess := false
	if token != "" && s.aiService.ValidateMCPToken(r.Context(), token) {
		authenticated = true
		fullAccess = true
	} else if s.service != nil {
		if _, err := s.service.AuthenticateWithError(r, time.Now().UTC()); err == nil {
			if user, userErr := s.service.CurrentUser(r, time.Now().UTC()); userErr == nil && !user.Disabled {
				authenticated = true
				fullAccess = user.IsAdmin() || user.AllowedNodes == "*"
			}
		}
	}
	if !authenticated {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"error": map[string]any{
				"code":    -32000,
				"message": "Unauthorized: invalid or missing MCP token",
			},
		})
		return
	}
	if !fullAccess {
		writeJSONError(w, http.StatusForbidden, "administrator access required")
		return
	}

	// 2. Handle GET (SSE transport or status metadata)
	if r.Method == http.MethodGet {
		isSSE := strings.Contains(r.Header.Get("Accept"), "text/event-stream") || r.URL.Query().Get("format") == "sse"
		if isSSE {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			w.Header().Set("X-Accel-Buffering", "no")

			flusher, ok := w.(http.Flusher)
			_, _ = fmt.Fprint(w, "event: endpoint\ndata: /mcp\n\n")
			if ok {
				flusher.Flush()
			}

			// Keep-alive loop until client disconnects or 45s timeout
			ticker := time.NewTicker(15 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-r.Context().Done():
					return
				case <-ticker.C:
					_, _ = fmt.Fprintf(w, ": ping\n\n")
					if ok {
						flusher.Flush()
					}
				}
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"name":       "probewatch-mcp",
			"version":    version.ServerVersion,
			"status":     "ready",
			"transports": []string{"http-post", "sse"},
			"endpoint":   "/mcp",
		})
		return
	}

	// 3. Handle POST (JSON-RPC 2.0 message)
	if r.Method == http.MethodPost {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"jsonrpc": "2.0",
				"error": map[string]any{
					"code":    -32700,
					"message": "Failed to read request body",
				},
			})
			return
		}

		resp, err := s.aiService.HandleMCPJSONRPC(r.Context(), body)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"jsonrpc": "2.0",
				"error": map[string]any{
					"code":    -32603,
					"message": err.Error(),
				},
			})
			return
		}

		writeJSON(w, http.StatusOK, resp)
		return
	}

	writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
}
