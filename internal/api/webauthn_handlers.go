package api

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/auth/webauthn"
	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/security"
)

func (s *Server) webauthnOrigin(r *http.Request) string {
	scheme := "https"
	if r.TLS == nil {
		if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
			scheme = proto
		} else if s.cfg.Environment == "development" {
			scheme = "http"
		}
	}
	if r.Host != "" {
		return scheme + "://" + r.Host
	}
	if s.cfg.PublicBaseURL != "" {
		return strings.TrimRight(s.cfg.PublicBaseURL, "/")
	}
	return scheme + "://localhost"
}

func (s *Server) webauthnRPID(r *http.Request) string {
	if r.Host != "" {
		return webauthn.ExtractRPID(r.Host)
	}
	if s.cfg.PublicBaseURL != "" {
		return webauthn.ExtractRPID(s.cfg.PublicBaseURL)
	}
	return "localhost"
}

// POST /api/webauthn/login/begin
func (s *Server) webauthnLoginBegin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	now := time.Now().UTC()
	challengeBytes, challengeB64, err := webauthn.GenerateChallenge()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to generate challenge")
		return
	}

	challengeID, err := security.GenerateToken()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to generate challenge id")
		return
	}

	if err := s.service.Store().SaveWebAuthnChallenge(r.Context(), challengeID, challengeBytes, "login", "", now.Add(5*time.Minute)); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to save challenge")
		return
	}

	rpID := s.webauthnRPID(r)

	writeJSON(w, http.StatusOK, map[string]any{
		"challenge_id": challengeID,
		"publicKey": map[string]any{
			"challenge":        challengeB64,
			"rpId":             rpID,
			"timeout":          60000,
			"userVerification": "preferred",
		},
	})
}

type webauthnLoginFinishRequest struct {
	ChallengeID string `json:"challenge_id"`
	ID          string `json:"id"`
	RawID       string `json:"rawId"`
	Type        string `json:"type"`
	Response    struct {
		ClientDataJSON    string `json:"clientDataJSON"`
		AuthenticatorData string `json:"authenticatorData"`
		Signature         string `json:"signature"`
		UserHandle        string `json:"userHandle,omitempty"`
	} `json:"response"`
}

// POST /api/webauthn/login/finish
func (s *Server) webauthnLoginFinish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req webauthnLoginFinishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	now := time.Now().UTC()
	expectedChallenge, _, err := s.service.Store().GetAndConsumeWebAuthnChallenge(r.Context(), req.ChallengeID, "login", now)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "challenge expired or invalid")
		return
	}

	rawID, err := webauthn.Base64URLDecode(req.RawID)
	if err != nil || len(rawID) == 0 {
		writeJSONError(w, http.StatusBadRequest, "invalid rawId")
		return
	}

	clientDataJSON, err := webauthn.Base64URLDecode(req.Response.ClientDataJSON)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid clientDataJSON")
		return
	}

	authenticatorData, err := webauthn.Base64URLDecode(req.Response.AuthenticatorData)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid authenticatorData")
		return
	}

	signature, err := webauthn.Base64URLDecode(req.Response.Signature)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid signature")
		return
	}

	// Lookup stored credential by raw ID
	cred, err := s.service.Store().GetWebAuthnCredentialByCredID(r.Context(), rawID)
	if errors.Is(err, db.ErrWebAuthnCredentialNotFound) {
		writeJSONError(w, http.StatusUnauthorized, "passkey not recognized")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "database error")
		return
	}

	expectedOrigin := s.webauthnOrigin(r)
	expectedRPID := s.webauthnRPID(r)

	newSignCount, err := webauthn.VerifyAssertion(
		rawID,
		clientDataJSON,
		authenticatorData,
		signature,
		expectedChallenge,
		expectedOrigin,
		expectedRPID,
		cred.PublicKey,
		cred.SignCount,
	)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, fmt.Sprintf("verification failed: %v", err))
		return
	}

	// Update credential usage & count
	_ = s.service.Store().UpdateWebAuthnCredentialUsage(r.Context(), cred.ID, newSignCount, now)

	// Issue admin session cookie
	if err := s.service.IssueSessionCookie(w, r, cred.AdminID); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"user": map[string]any{
			"id":   cred.AdminID,
			"name": cred.Name,
		},
	})
}

// POST /api/webauthn/register/begin
func (s *Server) webauthnRegisterBegin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	now := time.Now().UTC()
	user, err := s.service.CurrentUser(r, now)
	if err != nil {
		writeAuthenticationError(w, err)
		return
	}

	challengeBytes, challengeB64, err := webauthn.GenerateChallenge()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to generate challenge")
		return
	}

	challengeID, err := security.GenerateToken()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to generate challenge id")
		return
	}

	if err := s.service.Store().SaveWebAuthnChallenge(r.Context(), challengeID, challengeBytes, "register", user.ID, now.Add(5*time.Minute)); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to save challenge")
		return
	}

	rpID := s.webauthnRPID(r)

	// Exclude already registered credentials for this admin
	existingCreds, _ := s.service.Store().ListWebAuthnCredentials(r.Context(), user.ID)
	excludeList := make([]map[string]any, 0, len(existingCreds))
	for _, c := range existingCreds {
		excludeList = append(excludeList, map[string]any{
			"type": "public-key",
			"id":   webauthn.Base64URLEncode(c.CredentialID),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"challenge_id": challengeID,
		"publicKey": map[string]any{
			"challenge": challengeB64,
			"rp": map[string]any{
				"name": "ProbeWatch",
				"id":   rpID,
			},
			"user": map[string]any{
				"id":          webauthn.Base64URLEncode([]byte(user.ID)),
				"name":        user.Login,
				"displayName": "ProbeWatch Admin (" + user.Login + ")",
			},
			"pubKeyCredParams": []map[string]any{
				{"type": "public-key", "alg": -7},   // ES256
				{"type": "public-key", "alg": -257}, // RS256
				{"type": "public-key", "alg": -8},   // EdDSA
			},
			"authenticatorSelection": map[string]any{
				"residentKey":      "preferred",
				"userVerification": "preferred",
			},
			"timeout":            60000,
			"attestation":        "none",
			"excludeCredentials": excludeList,
		},
	})
}

type webauthnRegisterFinishRequest struct {
	ChallengeID string `json:"challenge_id"`
	Name        string `json:"name"`
	ID          string `json:"id"`
	RawID       string `json:"rawId"`
	Type        string `json:"type"`
	Response    struct {
		ClientDataJSON    string `json:"clientDataJSON"`
		AttestationObject string `json:"attestationObject"`
	} `json:"response"`
}

// POST /api/webauthn/register/finish
func (s *Server) webauthnRegisterFinish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	now := time.Now().UTC()
	user, err := s.service.CurrentUser(r, now)
	if err != nil {
		writeAuthenticationError(w, err)
		return
	}

	var req webauthnRegisterFinishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	expectedChallenge, challengeAdminID, err := s.service.Store().GetAndConsumeWebAuthnChallenge(r.Context(), req.ChallengeID, "register", now)
	if err != nil || (challengeAdminID != "" && challengeAdminID != user.ID) {
		writeJSONError(w, http.StatusBadRequest, "challenge expired or invalid")
		return
	}

	rawID, err := webauthn.Base64URLDecode(req.RawID)
	if err != nil || len(rawID) == 0 {
		writeJSONError(w, http.StatusBadRequest, "invalid rawId")
		return
	}

	clientDataJSON, err := webauthn.Base64URLDecode(req.Response.ClientDataJSON)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid clientDataJSON")
		return
	}

	attestationObject, err := webauthn.Base64URLDecode(req.Response.AttestationObject)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid attestationObject")
		return
	}

	expectedOrigin := s.webauthnOrigin(r)
	expectedRPID := s.webauthnRPID(r)

	parsed, err := webauthn.VerifyRegistration(
		rawID,
		clientDataJSON,
		attestationObject,
		expectedChallenge,
		expectedOrigin,
		expectedRPID,
	)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("registration verification failed: %v", err))
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = fmt.Sprintf("Passkey %s", now.Format("01-02 15:04"))
	}

	credID, err := security.GenerateToken()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to generate id")
		return
	}

	cred := db.WebAuthnCredential{
		ID:           credID,
		AdminID:      user.ID,
		Name:         name,
		CredentialID: parsed.CredentialID,
		PublicKey:    parsed.PublicKey,
		Algorithm:    parsed.Algorithm,
		SignCount:    parsed.SignCount,
		AAGUID:       parsed.AAGUID,
		CreatedAt:    now,
	}

	if err := s.service.Store().CreateWebAuthnCredential(r.Context(), cred); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to save credential")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"credential": map[string]any{
			"id":         cred.ID,
			"name":       cred.Name,
			"created_at": cred.CreatedAt,
		},
	})
}

// GET /api/webauthn/credentials
func (s *Server) webauthnCredentialsRoute(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	user, err := s.service.CurrentUser(r, now)
	if err != nil {
		writeAuthenticationError(w, err)
		return
	}

	trimmed := strings.TrimPrefix(r.URL.Path, "/api/webauthn/credentials")
	trimmed = strings.Trim(trimmed, "/")

	// Collection handlers
	if trimmed == "" {
		if r.Method == http.MethodGet {
			list, err := s.service.Store().ListWebAuthnCredentials(r.Context(), user.ID)
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "failed to list credentials")
				return
			}
			out := make([]map[string]any, 0, len(list))
			for _, c := range list {
				out = append(out, map[string]any{
					"id":           c.ID,
					"name":         c.Name,
					"algorithm":    c.Algorithm,
					"sign_count":   c.SignCount,
					"aaguid_hex":   hex.EncodeToString(c.AAGUID),
					"created_at":   c.CreatedAt,
					"last_used_at": c.LastUsedAt,
				})
			}
			writeJSON(w, http.StatusOK, out)
			return
		}
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Single credential action (id = trimmed)
	credID := trimmed
	if r.Method == http.MethodDelete {
		if err := s.service.Store().DeleteWebAuthnCredential(r.Context(), credID, user.ID); err != nil {
			if errors.Is(err, db.ErrWebAuthnCredentialNotFound) {
				writeJSONError(w, http.StatusNotFound, "credential not found")
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "failed to delete credential")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	if r.Method == http.MethodPatch || r.Method == http.MethodPut {
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
			writeJSONError(w, http.StatusBadRequest, "valid name required")
			return
		}
		if err := s.service.Store().RenameWebAuthnCredential(r.Context(), credID, user.ID, strings.TrimSpace(req.Name)); err != nil {
			if errors.Is(err, db.ErrWebAuthnCredentialNotFound) {
				writeJSONError(w, http.StatusNotFound, "credential not found")
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "failed to rename credential")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
}
