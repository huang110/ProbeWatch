package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/auth"
	"github.com/probewatch/probewatch/internal/config"
	"github.com/probewatch/probewatch/internal/db"
)

type Middleware struct {
	service *auth.Service
	cfg     config.Config
}

func NewMiddleware(service *auth.Service, cfg config.Config) *Middleware {
	return &Middleware{service: service, cfg: cfg}
}

type userCtxKey struct{}
type tokenCtxKey struct{}
type isTokenAuthCtxKey struct{}

var (
	currentAdminUserKey = userCtxKey{}
	currentAPITokenKey  = tokenCtxKey{}
	isAPITokenKey       = isTokenAuthCtxKey{}
)

// UserFromContext retrieves the authenticated user from the request context.
func UserFromContext(ctx context.Context) (db.AdminUser, bool) {
	if ctx == nil {
		return db.AdminUser{}, false
	}
	u, ok := ctx.Value(currentAdminUserKey).(db.AdminUser)
	return u, ok
}

// TokenFromContext retrieves the authenticated API token from the request context.
func TokenFromContext(ctx context.Context) (db.APIToken, bool) {
	if ctx == nil {
		return db.APIToken{}, false
	}
	t, ok := ctx.Value(currentAPITokenKey).(db.APIToken)
	return t, ok
}

// IsTokenAuthFromContext returns whether the request was authenticated via an API token.
func IsTokenAuthFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, ok := ctx.Value(isAPITokenKey).(bool)
	return ok && v
}

func (m *Middleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawToken := auth.ExtractAPIToken(r)
		isToken := rawToken != ""

		var user db.AdminUser
		var tokenRecord *db.APIToken
		var err error

		if isToken {
			var caller *db.AdminUser
			tokenRecord, caller, err = m.service.AuthenticateAPIToken(r.Context(), rawToken, time.Now().UTC())
			if err != nil {
				if errors.Is(err, db.ErrAPITokenDisabled) {
					writeJSONError(w, http.StatusForbidden, "api token is disabled")
					return
				}
				if errors.Is(err, db.ErrAPITokenExpired) {
					writeJSONError(w, http.StatusUnauthorized, "api token has expired")
					return
				}
				writeAuthenticationError(w, err)
				return
			}
			user = *caller
		} else {
			user, err = m.service.CurrentUser(r, time.Now().UTC())
			if err != nil {
				writeAuthenticationError(w, err)
				return
			}
		}

		if user.Disabled {
			writeJSONError(w, http.StatusForbidden, "user account is disabled")
			return
		}

		ctx := context.WithValue(r.Context(), currentAdminUserKey, user)
		if isToken {
			ctx = context.WithValue(ctx, isAPITokenKey, true)
			if tokenRecord != nil {
				ctx = context.WithValue(ctx, currentAPITokenKey, *tokenRecord)
			}
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (m *Middleware) RequireRole(roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := UserFromContext(r.Context())
			if !ok {
				var err error
				user, err = m.service.CurrentUser(r, time.Now().UTC())
				if err != nil {
					writeAuthenticationError(w, err)
					return
				}
			}
			if user.Disabled {
				writeJSONError(w, http.StatusForbidden, "user account is disabled")
				return
			}
			for _, role := range roles {
				if user.Role == role {
					ctx := context.WithValue(r.Context(), currentAdminUserKey, user)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}
			writeJSONError(w, http.StatusForbidden, "insufficient role permissions")
		})
	}
}

func (m *Middleware) RequireScope(requiredScope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tok, ok := TokenFromContext(r.Context()); ok {
				if !db.HasScope(tok.Scopes, requiredScope) {
					writeJSONError(w, http.StatusForbidden, "token lacks required scope: "+requiredScope)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (m *Middleware) RequireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isToken := IsTokenAuthFromContext(r.Context()) || auth.ExtractAPIToken(r) != ""

		user, ok := UserFromContext(r.Context())
		if !ok {
			var err error
			user, err = m.service.CurrentUser(r, time.Now().UTC())
			if err != nil {
				writeAuthenticationError(w, err)
				return
			}
		}
		if user.Disabled {
			writeJSONError(w, http.StatusForbidden, "user account is disabled")
			return
		}
		ctx := context.WithValue(r.Context(), currentAdminUserKey, user)
		if !isWriteMethod(r.Method) {
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		if !user.CanWrite() {
			writeJSONError(w, http.StatusForbidden, "viewer role is read-only")
			return
		}
		if m.cfg.MaxRequestBody > 0 {
			body, err := io.ReadAll(io.LimitReader(r.Body, m.cfg.MaxRequestBody+1))
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid request body")
				return
			}
			if int64(len(body)) > m.cfg.MaxRequestBody {
				writeJSONError(w, http.StatusRequestEntityTooLarge, "request body too large")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		if !isJSONContentType(r.Header.Get("Content-Type")) {
			writeJSONError(w, http.StatusBadRequest, "content type must be application/json")
			return
		}

		// API Token authentication skips browser CSRF check and same-origin requirement
		if isToken {
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		if !sameOrigin(r, m.cfg.PublicBaseURL) {
			writeJSONError(w, http.StatusForbidden, "same-origin request required")
			return
		}
		unlock := m.service.LockSessionCSRF(r)
		defer unlock()
		nextToken, err := m.service.ClaimCSRFWithError(r, r.Header.Get("X-CSRF-Token"))
		if err != nil {
			if !errors.Is(err, db.ErrTokenInvalid) && !errors.Is(err, db.ErrSessionNotFound) {
				writeJSONError(w, http.StatusInternalServerError, "csrf unavailable")
				return
			}
			writeJSONError(w, http.StatusForbidden, "csrf validation failed")
			return
		}

		buffered := newBufferedResponse()
		next.ServeHTTP(buffered, r)
		if buffered.status >= http.StatusOK && buffered.status < http.StatusMultipleChoices {
			buffered.Header().Set("X-CSRF-Token", nextToken)
			if m.service.CSRFRefreshPending(r) {
				buffered.Header().Set("X-CSRF-Refresh-Required", "true")
			}
		}
		buffered.commit(w)
	})
}

func writeAuthenticationError(w http.ResponseWriter, err error) {
	if errors.Is(err, db.ErrSessionNotFound) || errors.Is(err, db.ErrSessionExpired) || errors.Is(err, db.ErrSessionPolicyChanged) ||
		errors.Is(err, db.ErrAPITokenNotFound) || errors.Is(err, db.ErrAPITokenInvalid) || errors.Is(err, db.ErrAPITokenExpired) {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if errors.Is(err, db.ErrAPITokenDisabled) {
		writeJSONError(w, http.StatusForbidden, "api token is disabled")
		return
	}
	writeJSONError(w, http.StatusInternalServerError, "authentication unavailable")
}

type bufferedResponse struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func newBufferedResponse() *bufferedResponse {
	return &bufferedResponse{header: make(http.Header)}
}

func (b *bufferedResponse) Header() http.Header {
	return b.header
}

func (b *bufferedResponse) WriteHeader(status int) {
	if b.status == 0 {
		b.status = status
	}
}

func (b *bufferedResponse) Write(body []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}
	return b.body.Write(body)
}

func (b *bufferedResponse) commit(w http.ResponseWriter) {
	for key, values := range b.header {
		w.Header()[key] = append([]string(nil), values...)
	}
	if b.status == 0 {
		b.status = http.StatusOK
	}
	w.WriteHeader(b.status)
	_, _ = w.Write(b.body.Bytes())
}

func isWriteMethod(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete
}

func isJSONContentType(value string) bool {
	mediaType := strings.TrimSpace(strings.SplitN(value, ";", 2)[0])
	return strings.EqualFold(mediaType, "application/json")
}

func sameOrigin(r *http.Request, publicBaseURL string) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	want, err := url.Parse(publicBaseURL)
	if err != nil || want.Scheme == "" || want.Host == "" {
		return false
	}
	have, err := url.Parse(origin)
	if err != nil || have.User != nil || want.User != nil || have.Scheme == "" || have.Host == "" || have.Path != "" || have.RawQuery != "" || have.Fragment != "" {
		return false
	}
	return strings.EqualFold(have.Scheme, want.Scheme) && strings.EqualFold(have.Hostname(), want.Hostname()) && normalizedOriginPort(have) == normalizedOriginPort(want)
}

func normalizedOriginPort(value *url.URL) string {
	if value.Port() != "" {
		return value.Port()
	}
	if strings.EqualFold(value.Scheme, "https") {
		return "443"
	}
	return "80"
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
