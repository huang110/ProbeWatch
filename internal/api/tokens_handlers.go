package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/db"
)

func (s *Server) tokensRoute(w http.ResponseWriter, r *http.Request) {
	middleware := NewMiddleware(s.service, s.cfg)

	cleanPath := strings.TrimRight(r.URL.Path, "/")
	parts := strings.Split(cleanPath, "/")
	// Expected parts: ["api", "tokens"] or ["api", "tokens", "<id>"]

	if len(parts) == 3 && parts[1] == "api" && parts[2] == "tokens" {
		switch r.Method {
		case http.MethodGet:
			middleware.RequireAuth(http.HandlerFunc(s.listTokens)).ServeHTTP(w, r)
		case http.MethodPost:
			middleware.RequireAuth(middleware.RequireCSRF(http.HandlerFunc(s.createToken))).ServeHTTP(w, r)
		default:
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	if len(parts) == 4 && parts[1] == "api" && parts[2] == "tokens" {
		tokenID := parts[3]
		switch r.Method {
		case http.MethodPut:
			middleware.RequireAuth(middleware.RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				s.updateToken(w, r, tokenID)
			}))).ServeHTTP(w, r)
		case http.MethodDelete:
			middleware.RequireAuth(middleware.RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				s.deleteToken(w, r, tokenID)
			}))).ServeHTTP(w, r)
		default:
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	writeJSONError(w, http.StatusNotFound, "not found")
}

func (s *Server) listTokens(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	// Admin can see all tokens or pass ?mine=true, regular users see only their own
	mineOnly := r.URL.Query().Get("mine") == "true"
	queryUserID := user.ID
	if user.IsAdmin() && !mineOnly {
		queryUserID = ""
	}

	tokens, err := s.service.Store().ListAPITokens(r.Context(), queryUserID, user.IsAdmin() && !mineOnly)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to list tokens: "+err.Error())
		return
	}

	if tokens == nil {
		tokens = []db.APIToken{}
	}
	writeJSON(w, http.StatusOK, tokens)
}

type createTokenRequest struct {
	Name          string `json:"name"`
	Role          string `json:"role"`
	Scopes        string `json:"scopes"`
	AllowedNodes  string `json:"allowed_nodes"`
	ExpiresInDays int    `json:"expires_in_days"` // 0 = never, or 7, 30, 90, 365
}

func (s *Server) createToken(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	if !user.CanWrite() {
		writeJSONError(w, http.StatusForbidden, "viewer role cannot create tokens")
		return
	}

	var req createTokenRequest
	if err := decodeJSONRequest(w, r, s.requestBodyLimit(), &req); err != nil {
		writeRequestError(w, err)
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeJSONError(w, http.StatusBadRequest, "token name is required")
		return
	}

	role := strings.ToLower(strings.TrimSpace(req.Role))
	if role == "" {
		role = user.Role
	}
	// Non-admin cannot create a token with a higher role than their own
	if !user.IsAdmin() {
		if role == db.RoleAdmin {
			writeJSONError(w, http.StatusForbidden, "cannot grant admin role to token")
			return
		}
		if user.Role == db.RoleViewer && role != db.RoleViewer {
			writeJSONError(w, http.StatusForbidden, "viewer cannot grant higher roles")
			return
		}
	}

	allowedNodes := strings.TrimSpace(req.AllowedNodes)
	if allowedNodes == "" {
		allowedNodes = user.AllowedNodes
	} else if !user.IsAdmin() && user.AllowedNodes != "*" {
		// Non-admin can only grant subset of their allowed nodes
		requested := strings.Split(allowedNodes, ",")
		for _, node := range requested {
			node = strings.TrimSpace(node)
			if !user.CanAccessNode(node) {
				writeJSONError(w, http.StatusForbidden, fmt.Sprintf("cannot grant access to unpermitted node %s", node))
				return
			}
		}
	}

	var expiresIn *time.Duration
	if req.ExpiresInDays > 0 {
		d := time.Duration(req.ExpiresInDays) * 24 * time.Hour
		expiresIn = &d
	}

	now := time.Now().UTC()
	token, rawToken, err := s.service.Store().CreateAPIToken(r.Context(), db.CreateAPITokenInput{
		Name:         req.Name,
		UserID:       user.ID,
		Role:         role,
		Scopes:       req.Scopes,
		AllowedNodes: allowedNodes,
		ExpiresIn:    expiresIn,
	}, now)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to create token: "+err.Error())
		return
	}

	// Audit log
	_ = s.service.Store().RecordAuditLog(r.Context(), db.AuditLogEntry{
		ActorID:      user.ID,
		ActorName:    user.DisplayNameOrLogin(),
		ActorType:    "user",
		Action:       "token.create",
		ResourceType: "token",
		ResourceID:   token.ID,
		Detail:       fmt.Sprintf("Created API token %q with role %q", token.Name, token.Role),
		IPAddress:    clientIP(r),
		StatusCode:   http.StatusCreated,
		CreatedAt:    now,
	})

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"status":    "ok",
		"token":     token,
		"raw_token": rawToken, // Returned ONLY once upon creation!
	})
}

type updateTokenRequest struct {
	Disabled bool `json:"disabled"`
}

func (s *Server) updateToken(w http.ResponseWriter, r *http.Request, tokenID string) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var req updateTokenRequest
	if err := decodeJSONRequest(w, r, s.requestBodyLimit(), &req); err != nil {
		writeRequestError(w, err)
		return
	}

	err := s.service.Store().ToggleAPITokenDisabled(r.Context(), tokenID, req.Disabled, user.ID, user.IsAdmin())
	if err != nil {
		if errors.Is(err, db.ErrAPITokenNotFound) {
			writeJSONError(w, http.StatusNotFound, "token not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "failed to update token: "+err.Error())
		return
	}

	// Audit log
	actionDesc := "enabled"
	if req.Disabled {
		actionDesc = "disabled"
	}
	_ = s.service.Store().RecordAuditLog(r.Context(), db.AuditLogEntry{
		ActorID:      user.ID,
		ActorName:    user.DisplayNameOrLogin(),
		ActorType:    "user",
		Action:       "token.update",
		ResourceType: "token",
		ResourceID:   tokenID,
		Detail:       fmt.Sprintf("Token %s was %s", tokenID, actionDesc),
		IPAddress:    clientIP(r),
		StatusCode:   http.StatusOK,
		CreatedAt:    time.Now().UTC(),
	})

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"message": "token updated successfully",
	})
}

func (s *Server) deleteToken(w http.ResponseWriter, r *http.Request, tokenID string) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	err := s.service.Store().DeleteAPIToken(r.Context(), tokenID, user.ID, user.IsAdmin())
	if err != nil {
		if errors.Is(err, db.ErrAPITokenNotFound) {
			writeJSONError(w, http.StatusNotFound, "token not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "failed to delete token: "+err.Error())
		return
	}

	// Audit log
	_ = s.service.Store().RecordAuditLog(r.Context(), db.AuditLogEntry{
		ActorID:      user.ID,
		ActorName:    user.DisplayNameOrLogin(),
		ActorType:    "user",
		Action:       "token.delete",
		ResourceType: "token",
		ResourceID:   tokenID,
		Detail:       fmt.Sprintf("Token %s was deleted", tokenID),
		IPAddress:    clientIP(r),
		StatusCode:   http.StatusOK,
		CreatedAt:    time.Now().UTC(),
	})

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"message": "token deleted successfully",
	})
}

func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		parts := strings.Split(fwd, ",")
		return strings.TrimSpace(parts[0])
	}
	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		return strings.TrimSpace(realIP)
	}
	remote := r.RemoteAddr
	if idx := strings.LastIndex(remote, ":"); idx != -1 {
		return remote[:idx]
	}
	return remote
}
