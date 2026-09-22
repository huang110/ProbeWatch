package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/probewatch/probewatch/internal/auth"
)

type totpSetupResponse struct {
	Enabled    bool   `json:"enabled"`
	Secret     string `json:"secret,omitempty"`
	OTPAuthURL string `json:"otpauth_url,omitempty"`
}

type totpStateResponse struct {
	Enabled bool `json:"enabled"`
}

type totpCodeRequest struct {
	Code string `json:"code"`
}

// decodeTOTPCodeRequest parses and normalizes the 6-digit code body shared by
// the enable, disable, and login-verification endpoints.
func decodeTOTPCodeRequest(body io.Reader) (string, error) {
	content, err := io.ReadAll(io.LimitReader(body, 4096))
	if err != nil {
		return "", err
	}
	var payload totpCodeRequest
	if err := json.Unmarshal(content, &payload); err != nil {
		return "", err
	}
	return auth.NormalizeTOTPCode(payload.Code), nil
}

// totpSetup returns the enrollment material for the signed-in administrator.
// While 2FA is off each call provisions a fresh pending secret; once it is on
// the secret is never exposed again.
func (s *Server) totpSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	user, err := s.service.CurrentUser(r, time.Now().UTC())
	if err != nil {
		writeAuthenticationError(w, err)
		return
	}
	_, enabled, err := s.service.Store().GetAdminUserTOTP(r.Context(), user.ID)
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "service unavailable")
		return
	}
	if enabled {
		writeJSON(w, http.StatusOK, totpSetupResponse{Enabled: true})
		return
	}
	secret, otpauthURL, err := s.service.SetupTOTP(r.Context(), user.ID, user.Login, time.Now().UTC())
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "service unavailable")
		return
	}
	writeJSON(w, http.StatusOK, totpSetupResponse{Enabled: false, Secret: secret, OTPAuthURL: otpauthURL})
}

func (s *Server) totpEnable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, err := s.service.AuthenticateWithError(r, time.Now().UTC())
	if err != nil {
		writeAuthenticationError(w, err)
		return
	}
	code, err := decodeTOTPCodeRequest(r.Body)
	if err != nil || code == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.service.EnableTOTP(r.Context(), session.AdminUserID, code, time.Now().UTC()); err != nil {
		writeTOTPError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, totpStateResponse{Enabled: true})
}

func (s *Server) totpDisable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, err := s.service.AuthenticateWithError(r, time.Now().UTC())
	if err != nil {
		writeAuthenticationError(w, err)
		return
	}
	code, err := decodeTOTPCodeRequest(r.Body)
	if err != nil || code == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.service.DisableTOTP(r.Context(), session.AdminUserID, code, time.Now().UTC()); err != nil {
		writeTOTPError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, totpStateResponse{Enabled: false})
}

func writeTOTPError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrTOTPCodeInvalid):
		writeJSONError(w, http.StatusForbidden, "two-factor code invalid")
	case errors.Is(err, auth.ErrTOTPSetupRequired):
		writeJSONError(w, http.StatusConflict, "two-factor setup required")
	case errors.Is(err, auth.ErrTOTPAlreadyEnabled), errors.Is(err, auth.ErrTOTPNotEnabled):
		writeJSONError(w, http.StatusConflict, "two-factor state conflict")
	default:
		writeJSONError(w, http.StatusServiceUnavailable, "service unavailable")
	}
}

// totpVerifyLogin is the unauthenticated (pending-cookie) completion of a
// two-factor login; the auth service enforces method, origin, and code.
func (s *Server) totpVerifyLogin(w http.ResponseWriter, r *http.Request) {
	s.service.VerifyTOTPLogin(w, r)
}
