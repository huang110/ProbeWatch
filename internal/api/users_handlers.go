package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/db"
)

type userSummaryResponse struct {
	ID           string    `json:"id"`
	Provider     string    `json:"provider"`
	Login        string    `json:"login"`
	DisplayName  string    `json:"display_name"`
	Role         string    `json:"role"`
	AllowedNodes string    `json:"allowed_nodes"`
	Disabled     bool      `json:"disabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func userSummaryFrom(u db.AdminUser) userSummaryResponse {
	return userSummaryResponse{
		ID:           u.ID,
		Provider:     u.Provider,
		Login:        u.Login,
		DisplayName:  u.DisplayName,
		Role:         u.Role,
		AllowedNodes: u.AllowedNodes,
		Disabled:     u.Disabled,
		CreatedAt:    u.CreatedAt,
		UpdatedAt:    u.UpdatedAt,
	}
}

func (s *Server) usersRoute(w http.ResponseWriter, r *http.Request) {
	middleware := NewMiddleware(s.service, s.cfg)

	// User management is strictly reserved for the admin role
	adminCheck := middleware.RequireRole(db.RoleAdmin)

	cleanPath := strings.TrimRight(r.URL.Path, "/")
	parts := strings.Split(cleanPath, "/")
	// Expected parts: ["api", "users"] or ["api", "users", "<id>"]

	if len(parts) == 3 && parts[1] == "api" && parts[2] == "users" {
		switch r.Method {
		case http.MethodGet:
			adminCheck(http.HandlerFunc(s.listUsers)).ServeHTTP(w, r)
		case http.MethodPost:
			adminCheck(middleware.RequireCSRF(http.HandlerFunc(s.createUser))).ServeHTTP(w, r)
		default:
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	if len(parts) == 4 && parts[1] == "api" && parts[2] == "users" {
		userID := parts[3]
		switch r.Method {
		case http.MethodPut:
			adminCheck(middleware.RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				s.updateUser(w, r, userID)
			}))).ServeHTTP(w, r)
		case http.MethodDelete:
			adminCheck(middleware.RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				s.deleteUser(w, r, userID)
			}))).ServeHTTP(w, r)
		default:
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	writeJSONError(w, http.StatusNotFound, "not found")
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.service.Store().ListAdminUsers(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "failed to list users: "+err.Error())
		return
	}

	res := make([]userSummaryResponse, 0, len(users))
	for _, u := range users {
		res = append(res, userSummaryFrom(u))
	}
	writeJSON(w, http.StatusOK, res)
}

type createUserRequest struct {
	Login        string `json:"login"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	DisplayName  string `json:"display_name"`
	Role         string `json:"role"`
	AllowedNodes string `json:"allowed_nodes"`
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := decodeJSONRequest(w, r, s.requestBodyLimit(), &req); err != nil {
		writeRequestError(w, err)
		return
	}

	if req.Login == "" {
		req.Login = req.Username
	}
	req.Login = strings.TrimSpace(req.Login)
	if req.Login == "" {
		writeJSONError(w, http.StatusBadRequest, "login username is required")
		return
	}
	if len(req.Password) < 6 {
		writeJSONError(w, http.StatusBadRequest, "password must be at least 6 characters")
		return
	}

	created, err := s.service.Store().CreateLocalUser(r.Context(), db.CreateUserInput{
		Login:        req.Login,
		Password:     req.Password,
		DisplayName:  req.DisplayName,
		Role:         req.Role,
		AllowedNodes: req.AllowedNodes,
	}, time.Now().UTC())
	if err != nil {
		if errors.Is(err, db.ErrUserAlreadyExists) {
			writeJSONError(w, http.StatusConflict, "user with this username already exists")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "failed to create user: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, userSummaryFrom(created))
}

type updateUserRequest struct {
	DisplayName  *string `json:"display_name"`
	Role         *string `json:"role"`
	AllowedNodes *string `json:"allowed_nodes"`
	Disabled     *bool   `json:"disabled"`
	Password     *string `json:"password"`
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request, userID string) {
	var req updateUserRequest
	if err := decodeJSONRequest(w, r, s.requestBodyLimit(), &req); err != nil {
		writeRequestError(w, err)
		return
	}

	updated, err := s.service.Store().UpdateAdminUser(r.Context(), userID, db.UpdateUserInput{
		DisplayName:  req.DisplayName,
		Role:         req.Role,
		AllowedNodes: req.AllowedNodes,
		Disabled:     req.Disabled,
		NewPassword:  req.Password,
	}, time.Now().UTC())
	if err != nil {
		if errors.Is(err, db.ErrLastAdminProtection) {
			writeJSONError(w, http.StatusConflict, "cannot demote or disable the last active administrator")
			return
		}
		if errors.Is(err, db.ErrUserNotFound) {
			writeJSONError(w, http.StatusNotFound, "user not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "failed to update user: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, userSummaryFrom(updated))
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request, userID string) {
	// Cannot delete own active user account
	currentUser, ok := UserFromContext(r.Context())
	if ok && currentUser.ID == userID {
		writeJSONError(w, http.StatusBadRequest, "cannot delete your own currently logged-in account")
		return
	}

	if err := s.service.Store().DeleteAdminUser(r.Context(), userID); err != nil {
		if errors.Is(err, db.ErrLastAdminProtection) {
			writeJSONError(w, http.StatusConflict, "cannot delete the last active administrator")
			return
		}
		if errors.Is(err, db.ErrUserNotFound) {
			writeJSONError(w, http.StatusNotFound, "user not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "failed to delete user: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"message": "user deleted successfully",
	})
}
