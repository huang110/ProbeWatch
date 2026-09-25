package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/protocol"
)

func (s *Server) registerAgent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.registrationLimiter.Allow(publicLimiterKey(r), time.Now().UTC()) {
		writeRateLimitError(w)
		return
	}
	var request protocol.RegisterRequest
	if err := decodeJSONRequest(w, r, s.requestBodyLimit(), &request); err != nil {
		writeRequestError(w, err)
		return
	}
	if err := request.Validate(); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid registration request")
		return
	}
	registered, err := s.service.Store().RegisterNodeWithTTL(r.Context(), request.RegistrationToken, db.NodeInput{UUID: request.NodeUUID, Name: request.Name}, time.Now().UTC(), s.agentNodeTokenTTL())
	if err != nil {
		status, message := classifyStoreError(err)
		if status == http.StatusServiceUnavailable {
			message = "registration unavailable"
		}
		writeJSONError(w, status, message)
		return
	}
	response := protocol.RegisterResponse{NodeUUID: registered.Node.UUID, NodeToken: registered.Token, Endpoint: strings.TrimRight(s.cfg.PublicBaseURL, "/") + "/api/agent/v1"}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (s *Server) reportAgent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	node, requestID, now, ok := s.authenticateAgentRequest(w, r)
	if !ok {
		return
	}
	var request protocol.ReportRequest
	if err := decodeJSONRequest(w, r, s.requestBodyLimit(), &request); err != nil {
		writeRequestError(w, err)
		return
	}
	if err := request.Validate(); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid report")
		return
	}
	if err := validateAgentPayloadTimestamp(request.ReportedAt, now, s.agentClockSkew()); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid report")
		return
	}
	if request.NodeUUID != node.UUID {
		writeJSONError(w, http.StatusConflict, "node identity conflict")
		return
	}
	for _, result := range request.Results {
		if err := checkResultTimestamp(result, now, s.agentClockSkew()); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid report")
			return
		}
		if err := s.validateTargetBinding(r.Context(), db.TargetKind(result.Kind), result.ID); err != nil {
			writeTargetBindingError(w, err)
			return
		}
	}
	resourcePayload, err := json.Marshal(request.Resource)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid report")
		return
	}
	results := make([]db.AgentResultInput, 0, len(request.Results))
	for _, result := range request.Results {
		input, err := agentResultInput(result, now)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid report")
			return
		}
		results = append(results, input)
	}
	if err := s.service.Store().PersistAgentReport(r.Context(), db.AgentReportInput{
		NodeID:          node.ID,
		RequestID:       requestID,
		ReplayExpiresAt: now.Add(2 * s.agentClockSkew()),
		Now:             now,
		ReportedAt:      time.Unix(request.ReportedAt, 0).UTC(),
		ResourcePayload: resourcePayload,
		Results:         results,
	}); err != nil {
		if errors.Is(err, db.ErrReplay) {
			writeAgentRequestError(w, err)
			return
		}
		writeJSONError(w, http.StatusServiceUnavailable, "report unavailable")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) networkResultAgent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	node, requestID, now, ok := s.authenticateAgentRequest(w, r)
	if !ok {
		return
	}
	var request protocol.NetworkResultEnvelope
	if err := decodeJSONRequest(w, r, s.requestBodyLimit(), &request); err != nil {
		writeRequestError(w, err)
		return
	}
	if err := request.Validate(); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid network result")
		return
	}
	if err := validateResultTimestamp(request.Result.CheckedAt, now, s.agentClockSkew()); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid network result")
		return
	}
	if err := s.validateNetworkTargetBinding(r.Context(), request.TargetID); err != nil {
		writeTargetBindingError(w, err)
		return
	}
	payload, err := json.Marshal(request.Result)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid network result")
		return
	}
	if err := s.service.Store().PersistAgentResult(r.Context(), node.ID, requestID, now.Add(2*s.agentClockSkew()), now, db.AgentResultInput{Kind: db.TargetKindTCP, TargetID: request.TargetID, CheckedAt: checkedAt(request.Result.CheckedAt, now), Payload: payload}); err != nil {
		if errors.Is(err, db.ErrReplay) {
			writeAgentRequestError(w, err)
			return
		}
		writeJSONError(w, http.StatusServiceUnavailable, "result unavailable")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) mtrResultAgent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	node, requestID, now, ok := s.authenticateAgentRequest(w, r)
	if !ok {
		return
	}
	var request protocol.MTRResultEnvelope
	if err := decodeJSONRequest(w, r, s.requestBodyLimit(), &request); err != nil {
		writeRequestError(w, err)
		return
	}
	if err := request.Validate(); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid mtr result")
		return
	}
	if err := validateResultTimestamp(request.Result.CheckedAt, now, s.agentClockSkew()); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid mtr result")
		return
	}
	if err := s.validateTargetBinding(r.Context(), db.TargetKindMTR, request.TargetID); err != nil {
		writeTargetBindingError(w, err)
		return
	}
	payload, err := json.Marshal(request.Result)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid mtr result")
		return
	}
	if err := s.service.Store().PersistAgentResult(r.Context(), node.ID, requestID, now.Add(2*s.agentClockSkew()), now, db.AgentResultInput{Kind: db.TargetKindMTR, TargetID: request.TargetID, CheckedAt: checkedAt(request.Result.CheckedAt, now), Payload: payload}); err != nil {
		if errors.Is(err, db.ErrReplay) {
			writeAgentRequestError(w, err)
			return
		}
		writeJSONError(w, http.StatusServiceUnavailable, "result unavailable")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) mediaResultAgent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	node, requestID, now, ok := s.authenticateAgentRequest(w, r)
	if !ok {
		return
	}
	var request protocol.MediaResultEnvelope
	if err := decodeJSONRequest(w, r, s.requestBodyLimit(), &request); err != nil {
		writeRequestError(w, err)
		return
	}
	if err := request.Validate(); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid media result")
		return
	}
	if err := validateResultTimestamp(request.Result.CheckedAt, now, s.agentClockSkew()); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid media result")
		return
	}
	if err := s.validateTargetBinding(r.Context(), db.TargetKindMediaHTTP, request.DetectorID); err != nil {
		writeTargetBindingError(w, err)
		return
	}
	payload, err := json.Marshal(request.Result)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid media result")
		return
	}
	if err := s.service.Store().PersistAgentResult(r.Context(), node.ID, requestID, now.Add(2*s.agentClockSkew()), now, db.AgentResultInput{Kind: db.TargetKindMediaHTTP, TargetID: request.DetectorID, CheckedAt: checkedAt(request.Result.CheckedAt, now), Payload: payload}); err != nil {
		if errors.Is(err, db.ErrReplay) {
			writeAgentRequestError(w, err)
			return
		}
		writeJSONError(w, http.StatusServiceUnavailable, "result unavailable")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) authenticateAgentRequest(w http.ResponseWriter, r *http.Request) (db.Node, string, time.Time, bool) {
	if !s.agentIPLimiter.Allow(publicLimiterKey(r), time.Now().UTC()) {
		writeRateLimitError(w)
		return db.Node{}, "", time.Time{}, false
	}
	token, ok := parseBearer(r.Header.Get("Authorization"))
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "authentication failed")
		return db.Node{}, "", time.Time{}, false
	}
	now := time.Now().UTC()
	node, err := s.service.Store().AuthenticateNodeToken(r.Context(), token, now)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication failed")
		return db.Node{}, "", time.Time{}, false
	}
	if !s.agentLimiter.Allow(node.ID, now) {
		writeJSONError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return db.Node{}, "", time.Time{}, false
	}
	requestID, err := parseAgentMetadata(r, now, s.agentClockSkew())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "request authentication failed")
		return db.Node{}, "", time.Time{}, false
	}
	return node, requestID, now, true
}

func (s *Server) validateTargetBinding(ctx context.Context, kind db.TargetKind, id string) error {
	_, err := s.service.Store().LookupEnabledTarget(ctx, kind, id)
	return err
}

func (s *Server) validateNetworkTargetBinding(ctx context.Context, id string) error {
	for _, kind := range []db.TargetKind{db.TargetKindTCP, db.TargetKindHTTP, db.TargetKindHTTPS, db.TargetKindDNS} {
		if target, err := s.service.Store().GetTarget(ctx, kind, id); err == nil {
			if !target.Enabled {
				return db.ErrTargetDisabled
			}
			return nil
		} else if !errors.Is(err, db.ErrTargetNotFound) && !errors.Is(err, db.ErrTargetKindMismatch) {
			return err
		}
	}
	for _, kind := range []db.TargetKind{db.TargetKindMTR, db.TargetKindMediaHTTP} {
		if _, err := s.service.Store().GetTarget(ctx, kind, id); err == nil {
			return db.ErrTargetKindMismatch
		} else if !errors.Is(err, db.ErrTargetNotFound) && !errors.Is(err, db.ErrTargetKindMismatch) {
			return err
		}
	}
	return db.ErrTargetNotFound
}

func writeTargetBindingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, db.ErrTargetNotFound), errors.Is(err, db.ErrTargetDisabled):
		writeJSONError(w, http.StatusNotFound, "target not found")
	case errors.Is(err, db.ErrTargetKindMismatch):
		writeJSONError(w, http.StatusBadRequest, "target kind mismatch")
	default:
		writeJSONError(w, http.StatusServiceUnavailable, "target unavailable")
	}
}

func agentResultInput(result protocol.CheckResult, now time.Time) (db.AgentResultInput, error) {
	switch {
	case result.Network != nil:
		payload, err := json.Marshal(result.Network)
		if err != nil {
			return db.AgentResultInput{}, err
		}
		return db.AgentResultInput{Kind: db.TargetKind(result.Kind), TargetID: result.ID, CheckedAt: checkedAt(result.Network.CheckedAt, now), Payload: payload}, nil
	case result.MTR != nil:
		payload, err := json.Marshal(result.MTR)
		if err != nil {
			return db.AgentResultInput{}, err
		}
		return db.AgentResultInput{Kind: db.TargetKindMTR, TargetID: result.ID, CheckedAt: checkedAt(result.MTR.CheckedAt, now), Payload: payload}, nil
	case result.Media != nil:
		payload, err := json.Marshal(result.Media)
		if err != nil {
			return db.AgentResultInput{}, err
		}
		return db.AgentResultInput{Kind: db.TargetKindMediaHTTP, TargetID: result.ID, CheckedAt: checkedAt(result.Media.CheckedAt, now), Payload: payload}, nil
	default:
		return db.AgentResultInput{}, errors.New("invalid check result shape")
	}
}

func validateResultTimestamp(unixSeconds int64, now time.Time, skew time.Duration) error {
	if unixSeconds == 0 {
		return nil
	}
	return validateAgentPayloadTimestamp(unixSeconds, now, skew)
}

func checkResultTimestamp(result protocol.CheckResult, now time.Time, skew time.Duration) error {
	switch {
	case result.Network != nil:
		return validateResultTimestamp(result.Network.CheckedAt, now, skew)
	case result.MTR != nil:
		return validateResultTimestamp(result.MTR.CheckedAt, now, skew)
	case result.Media != nil:
		return validateResultTimestamp(result.Media.CheckedAt, now, skew)
	default:
		return errors.New("invalid check result shape")
	}
}

func writeAgentRequestError(w http.ResponseWriter, err error) {
	if errors.Is(err, db.ErrReplay) {
		writeJSONError(w, http.StatusConflict, "request already processed")
		return
	}
	writeJSONError(w, http.StatusServiceUnavailable, "request unavailable")
}
