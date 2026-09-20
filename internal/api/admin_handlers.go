package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/protocol"
	"github.com/probewatch/probewatch/internal/security"
)

type registrationTokenResponse struct {
	RegistrationToken string    `json:"registration_token"`
	Endpoint          string    `json:"endpoint"`
	ExpiresAt         time.Time `json:"expires_at"`
}

type nodeResponse struct {
	ID   string `json:"id"`
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

func (s *Server) createRegistrationToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var request struct{}
	if err := decodeJSONObjectRequest(w, r, s.requestBodyLimit(), &request); err != nil {
		writeRequestError(w, err)
		return
	}
	registration, err := s.service.Store().CreateRegistrationToken(r.Context(), s.agentTokenTTL())
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "service unavailable")
		return
	}
	endpoint := strings.TrimRight(s.cfg.PublicBaseURL, "/") + "/api/agent/v1"
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(registrationTokenResponse{RegistrationToken: registration.Token, Endpoint: endpoint, ExpiresAt: registration.ExpiresAt})
}

func (s *Server) listNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	nodes, err := s.service.Store().ListNodes(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "service unavailable")
		return
	}
	response := make([]nodeResponse, 0, len(nodes))
	for _, node := range nodes {
		response = append(response, nodeResponse{ID: node.ID, UUID: node.UUID, Name: node.Name})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (s *Server) nodeAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	nodeID, action, ok := parseNodeActionPath(r.URL.Path)
	if !ok {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	var request struct{}
	if err := decodeJSONObjectRequest(w, r, s.requestBodyLimit(), &request); err != nil {
		writeRequestError(w, err)
		return
	}
	session, err := s.service.AuthenticateWithError(r, time.Now().UTC())
	if err != nil {
		writeAuthenticationError(w, err)
		return
	}
	now := time.Now().UTC()
	switch action {
	case "rotate-token":
		token, err := s.service.Store().RotateNodeTokenWithTTL(r.Context(), nodeID, session.AdminUserID, now, s.agentNodeTokenTTL())
		if err != nil {
			writeNodeActionError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"node_token": token})
	case "revoke":
		if err := s.service.Store().RevokeNodeToken(r.Context(), nodeID, session.AdminUserID, now); err != nil {
			writeNodeActionError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type mtrLatestResponse struct {
	TargetID  string             `json:"target_id"`
	CheckedAt time.Time          `json:"checked_at"`
	Result    protocol.MTRResult `json:"result"`
}

type mediaLatestResponse struct {
	DetectorID string               `json:"detector_id"`
	CheckedAt  time.Time            `json:"checked_at"`
	Result     protocol.MediaResult `json:"result"`
}

func (s *Server) nodeRead(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 4 || parts[0] != "api" || parts[1] != "nodes" || parts[2] == "" || (parts[3] != "mtr" && parts[3] != "media") {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	if !security.IsRFC4122UUID(parts[2]) {
		writeJSONError(w, http.StatusNotFound, "node not found")
		return
	}
	node, err := s.service.Store().GetNodeByUUID(r.Context(), parts[2])
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "node not found")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "node results unavailable")
		return
	}
	if parts[3] == "mtr" {
		s.writeMTRLatest(w, r, node.ID)
		return
	}
	s.writeMediaLatest(w, r, node.ID)
}

func (s *Server) writeMTRLatest(w http.ResponseWriter, r *http.Request, nodeID string) {
	results, err := s.service.Store().ListMTRLatest(r.Context(), nodeID)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "node results unavailable")
		return
	}
	response := make([]mtrLatestResponse, 0, len(results))
	for _, latest := range results {
		var result protocol.MTRResult
		if err := json.Unmarshal(latest.Payload, &result); err != nil {
			writeJSONError(w, http.StatusServiceUnavailable, "node results unavailable")
			return
		}
		response = append(response, mtrLatestResponse{TargetID: latest.ID, CheckedAt: latest.CheckedAt, Result: result})
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) writeMediaLatest(w http.ResponseWriter, r *http.Request, nodeID string) {
	results, err := s.service.Store().ListMediaLatest(r.Context(), nodeID)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "node results unavailable")
		return
	}
	response := make([]mediaLatestResponse, 0, len(results))
	for _, latest := range results {
		var result protocol.MediaResult
		if err := json.Unmarshal(latest.Payload, &result); err != nil {
			writeJSONError(w, http.StatusServiceUnavailable, "node results unavailable")
			return
		}
		response = append(response, mediaLatestResponse{DetectorID: latest.ID, CheckedAt: latest.CheckedAt, Result: result})
	}
	writeJSON(w, http.StatusOK, response)
}

func parseNodeActionPath(path string) (string, string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 4 || parts[0] != "api" || parts[1] != "nodes" || parts[2] == "" || (parts[3] != "rotate-token" && parts[3] != "revoke") {
		return "", "", false
	}
	return parts[2], parts[3], true
}

func writeNodeActionError(w http.ResponseWriter, err error) {
	if errors.Is(err, db.ErrNodeDeleted) || errors.Is(err, db.ErrTokenRevoked) {
		writeJSONError(w, http.StatusConflict, "node token unavailable")
		return
	}
	if errors.Is(err, db.ErrTokenInvalid) {
		writeJSONError(w, http.StatusNotFound, "node not found")
		return
	}
	writeJSONError(w, http.StatusServiceUnavailable, "service unavailable")
}
