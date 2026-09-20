package api

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/protocol"
)

const (
	defaultAgentClockSkew = 5 * time.Minute
	maxRequestIDLength    = 128
	defaultRequestBody    = 1 << 20
	maxBearerTokenLength  = 512
)

func (s *Server) requestBodyLimit() int64 {
	if s.cfg.MaxRequestBody > 0 {
		return s.cfg.MaxRequestBody
	}
	return defaultRequestBody
}

func (s *Server) agentClockSkew() time.Duration {
	if s.cfg.AgentClockSkew > 0 {
		return s.cfg.AgentClockSkew
	}
	return defaultAgentClockSkew
}

func decodeJSONRequest(w http.ResponseWriter, r *http.Request, limit int64, destination any) error {
	return decodeJSONRequestWithDecoder(w, r, limit, destination, protocol.DecodeJSON)
}

func decodeJSONObjectRequest(w http.ResponseWriter, r *http.Request, limit int64, destination any) error {
	return decodeJSONRequestWithDecoder(w, r, limit, destination, protocol.DecodeJSONObject)
}

func decodeJSONRequestWithDecoder(w http.ResponseWriter, r *http.Request, limit int64, destination any, decode func(io.Reader, int64, any) error) error {
	if r.Method != http.MethodGet && !isJSONContentType(r.Header.Get("Content-Type")) {
		return errInvalidRequest
	}
	if r.ContentLength > limit {
		return errRequestBodyTooLarge
	}
	err := decode(http.MaxBytesReader(w, r.Body, limit), limit, destination)
	if err == nil {
		return nil
	}
	var maxErr *http.MaxBytesError
	if strings.Contains(err.Error(), "body exceeds") || errors.As(err, &maxErr) {
		return errRequestBodyTooLarge
	}
	return errInvalidRequest
}

var (
	errInvalidRequest      = errors.New("invalid request")
	errRequestBodyTooLarge = errors.New("request body too large")
)

type rateLimiter struct {
	mu         sync.Mutex
	limit      int
	window     time.Duration
	maxEntries int
	entries    map[string]rateLimitEntry
}

type rateLimitEntry struct {
	windowStart time.Time
	count       int
}

func newRateLimiter(limit int, window time.Duration, maxEntries int) *rateLimiter {
	if limit < 1 {
		limit = 1
	}
	if window <= 0 {
		window = time.Minute
	}
	if maxEntries < 1 {
		maxEntries = 1
	}
	return &rateLimiter{limit: limit, window: window, maxEntries: maxEntries, entries: make(map[string]rateLimitEntry)}
}

func (l *rateLimiter) Allow(key string, now time.Time) bool {
	now = now.UTC()
	l.mu.Lock()
	defer l.mu.Unlock()
	for existingKey, entry := range l.entries {
		if !now.Before(entry.windowStart.Add(l.window)) {
			delete(l.entries, existingKey)
		}
	}
	entry, exists := l.entries[key]
	if !exists {
		if len(l.entries) >= l.maxEntries {
			return false
		}
		l.entries[key] = rateLimitEntry{windowStart: now, count: 1}
		return true
	}
	if !now.Before(entry.windowStart.Add(l.window)) {
		l.entries[key] = rateLimitEntry{windowStart: now, count: 1}
		return true
	}
	if entry.count >= l.limit {
		return false
	}
	entry.count++
	l.entries[key] = entry
	return true
}

func writeRequestError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errRequestBodyTooLarge):
		writeJSONError(w, http.StatusRequestEntityTooLarge, "request body too large")
	default:
		writeJSONError(w, http.StatusBadRequest, "invalid request")
	}
}

func parseBearer(value string) (string, bool) {
	parts := strings.Fields(value)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" || len([]byte(parts[1])) > maxBearerTokenLength {
		return "", false
	}
	return parts[1], true
}

func parseAgentMetadata(r *http.Request, now time.Time, skew time.Duration) (string, error) {
	rawTimestamp := strings.TrimSpace(r.Header.Get("X-Probe-Timestamp"))
	timestamp, err := strconv.ParseInt(rawTimestamp, 10, 64)
	if err != nil || timestamp <= 0 {
		return "", errInvalidRequest
	}
	if difference := now.Sub(time.Unix(timestamp, 0).UTC()); difference > skew || difference < -skew {
		return "", errInvalidRequest
	}
	requestID := strings.TrimSpace(r.Header.Get("X-Probe-Request-ID"))
	if requestID == "" || len([]byte(requestID)) > maxRequestIDLength {
		return "", errInvalidRequest
	}
	return requestID, nil
}

func validateAgentPayloadTimestamp(unixSeconds int64, now time.Time, skew time.Duration) error {
	if unixSeconds <= 0 {
		return errInvalidRequest
	}
	payloadTime := time.Unix(unixSeconds, 0).UTC()
	if payloadTime.After(now.Add(skew)) || payloadTime.Before(now.Add(-skew)) {
		return errInvalidRequest
	}
	return nil
}

func classifyStoreError(err error) (int, string) {
	switch {
	case errors.Is(err, db.ErrTokenInvalid), errors.Is(err, db.ErrTokenExpired), errors.Is(err, db.ErrTokenAlreadyConsumed):
		return http.StatusUnauthorized, "registration failed"
	case errors.Is(err, db.ErrNodeUUIDConflict):
		return http.StatusConflict, "node identity conflict"
	case errors.Is(err, db.ErrTokenRevoked), errors.Is(err, db.ErrNodeDeleted):
		return http.StatusUnauthorized, "authentication failed"
	case errors.Is(err, db.ErrReplay):
		return http.StatusConflict, "request already processed"
	case errors.Is(err, db.ErrTargetNotFound), errors.Is(err, db.ErrTargetDisabled):
		return http.StatusNotFound, "target not found"
	case errors.Is(err, db.ErrTargetKindMismatch):
		return http.StatusBadRequest, "target kind mismatch"
	default:
		return http.StatusServiceUnavailable, "service unavailable"
	}
}

func checkedAt(unixSeconds int64, fallback time.Time) time.Time {
	if unixSeconds <= 0 {
		return fallback.UTC()
	}
	return time.Unix(unixSeconds, 0).UTC()
}
