package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/terminal"
)

// agentTerminalTunnel handles the reverse tunnel WebSocket initiated by an agent.
func (s *Server) agentTerminalTunnel(w http.ResponseWriter, r *http.Request) {
	node, _, _, ok := s.authenticateAgentRequest(w, r)
	if !ok {
		return
	}

	conn, err := terminal.Upgrade(w, r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("upgrade error: %v", err))
		return
	}
	defer conn.Close()

	s.terminalManager.RegisterTunnel(node.UUID, conn)
	defer s.terminalManager.UnregisterTunnel(node.UUID)

	for {
		var env terminal.Envelope
		if err := conn.ReadJSON(&env); err != nil {
			return
		}
		s.terminalManager.HandleTunnelMessage(node.UUID, env)
	}
}

// terminalStatus returns the connection status of terminal tunnels for all nodes.
func (s *Server) terminalStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	nodes, err := s.service.Store().ListNodes(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}

	type statusItem struct {
		ID             string `json:"id"`
		UUID           string `json:"uuid"`
		Name           string `json:"name"`
		TerminalOnline bool   `json:"terminal_online"`
	}

	results := make([]statusItem, 0, len(nodes))
	for _, n := range nodes {
		results = append(results, statusItem{
			ID:             n.ID,
			UUID:           n.UUID,
			Name:           n.Name,
			TerminalOnline: s.terminalManager.IsNodeConnected(n.UUID),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": s.cfg.EnableRemoteTerminal,
		"nodes":   results,
	})
}

// terminalExec handles controlled one-shot remote command execution.
func (s *Server) terminalExec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	session, err := s.service.AuthenticateWithError(r, time.Now().UTC())
	if err != nil {
		writeAuthenticationError(w, err)
		return
	}

	if !s.cfg.EnableRemoteTerminal {
		writeJSONError(w, http.StatusServiceUnavailable, "remote terminal is disabled by server configuration")
		return
	}

	var req struct {
		NodeID     string `json:"node_id"`
		Command    string `json:"command"`
		TimeoutSec int    `json:"timeout_sec"`
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json payload")
		return
	}

	req.Command = strings.TrimSpace(req.Command)
	if req.Command == "" {
		writeJSONError(w, http.StatusBadRequest, "command cannot be empty")
		return
	}
	if len(req.Command) > 4096 {
		writeJSONError(w, http.StatusBadRequest, "command exceeds maximum length (4096 bytes)")
		return
	}

	node, err := s.resolveNode(r, req.NodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "node not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "database error")
		return
	}

	if !s.terminalManager.IsNodeConnected(node.UUID) {
		writeJSONError(w, http.StatusServiceUnavailable, "agent terminal is offline or not enabled on this node")
		return
	}

	res, err := s.terminalManager.ExecuteCommand(r.Context(), node.UUID, req.Command, req.TimeoutSec)
	if err != nil {
		if errors.Is(err, terminal.ErrNodeOffline) {
			writeJSONError(w, http.StatusServiceUnavailable, "agent terminal disconnected")
			return
		}
		if errors.Is(err, terminal.ErrExecutionTimeout) {
			writeJSONError(w, http.StatusGatewayTimeout, "command execution timed out")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("execution failed: %v", err))
		return
	}

	// Record audit event
	meta, _ := json.Marshal(map[string]any{
		"command":     req.Command,
		"exit_code":   res.ExitCode,
		"duration_ms": res.DurationMS,
		"client_ip":   publicLimiterKey(r),
	})
	_ = s.service.Store().RecordAudit(r.Context(), "terminal_exec", node.ID, session.AdminUserID, meta)

	writeJSON(w, http.StatusOK, map[string]any{
		"exit_code":   res.ExitCode,
		"stdout":      res.Stdout,
		"stderr":      res.Stderr,
		"duration_ms": res.DurationMS,
	})
}

// terminalWS establishes a full interactive terminal session with the agent node.
func (s *Server) terminalWS(w http.ResponseWriter, r *http.Request) {
	sessionUser, err := s.service.AuthenticateWithError(r, time.Now().UTC())
	if err != nil {
		writeAuthenticationError(w, err)
		return
	}

	if !s.cfg.EnableRemoteTerminal {
		writeJSONError(w, http.StatusServiceUnavailable, "remote terminal is disabled by server configuration")
		return
	}

	nodeID := strings.TrimSpace(r.URL.Query().Get("node_id"))
	if nodeID == "" {
		writeJSONError(w, http.StatusBadRequest, "missing node_id query parameter")
		return
	}

	cols, _ := strconv.Atoi(r.URL.Query().Get("cols"))
	rows, _ := strconv.Atoi(r.URL.Query().Get("rows"))
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}

	node, err := s.resolveNode(r, nodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "node not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "database error")
		return
	}

	if !s.terminalManager.IsNodeConnected(node.UUID) {
		writeJSONError(w, http.StatusServiceUnavailable, "agent terminal is offline or unreachable")
		return
	}

	browserConn, err := terminal.Upgrade(w, r)
	if err != nil {
		return
	}
	defer browserConn.Close()

	termSession, err := s.terminalManager.CreateSession("", node.UUID, cols, rows, browserConn, func(durationSeconds int64) {
		meta, _ := json.Marshal(map[string]any{
			"duration_seconds": durationSeconds,
			"client_ip":        publicLimiterKey(r),
		})
		_ = s.service.Store().RecordAudit(r.Context(), "terminal_session", node.ID, sessionUser.AdminUserID, meta)
	})
	if err != nil {
		_ = browserConn.WriteJSON(terminal.Envelope{
			Type:  terminal.TypeError,
			Error: err.Error(),
		})
		return
	}

	// Keep handler alive while session is running
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			termSession.Close()
			return
		case <-ticker.C:
			if !s.terminalManager.IsNodeConnected(node.UUID) {
				termSession.Close()
				return
			}
		}
	}
}

func (s *Server) resolveNode(r *http.Request, idOrUUID string) (db.Node, error) {
	node, err := s.service.Store().GetNodeByID(r.Context(), idOrUUID)
	if err == nil {
		return node, nil
	}
	return s.service.Store().GetNodeByUUID(r.Context(), idOrUUID)
}
