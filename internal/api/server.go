package api

import (
	"encoding/json"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/probewatch/probewatch/frontend"
	"github.com/probewatch/probewatch/internal/auth"
	"github.com/probewatch/probewatch/internal/config"
	"github.com/probewatch/probewatch/internal/db"
)

type Server struct {
	cfg           config.Config
	service       *auth.Service
	agentLimiter  *rateLimiter
	publicLimiter *rateLimiter
}

func NewServer(cfg config.Config, service *auth.Service) *Server {
	return &Server{cfg: cfg, service: service, agentLimiter: newRateLimiter(120, time.Minute, 10000), publicLimiter: newRateLimiter(60, time.Minute, 10000)}
}

func (s *Server) agentNodeTokenTTL() time.Duration {
	if s.cfg.AgentNodeTokenTTL <= 0 {
		return db.DefaultNodeTokenLifetime
	}
	return s.cfg.AgentNodeTokenTTL
}

func (s *Server) agentTokenTTL() time.Duration {
	if s.cfg.AgentTokenTTL <= 0 {
		return db.DefaultRegistrationTokenLifetime
	}
	return s.cfg.AgentTokenTTL
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.health)
	mux.HandleFunc("/api/public/status", s.publicStatus)
	mux.HandleFunc("/auth/github", s.githubStart)
	mux.HandleFunc("/auth/github/callback", s.githubCallback)
	mux.HandleFunc("/auth/login", s.localLogin)
	mux.HandleFunc("/auth/totp/verify", s.totpVerifyLogin)

	middleware := NewMiddleware(s.service, s.cfg)
	mux.Handle("/auth/logout", middleware.RequireAuth(middleware.RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.service.Logout(w, r)
	}))))
	mux.Handle("/api/csrf", middleware.RequireAuth(http.HandlerFunc(s.service.CSRFHandler)))
	mux.Handle("/api/totp/setup", middleware.RequireAuth(http.HandlerFunc(s.totpSetup)))
	mux.Handle("/api/totp/enable", middleware.RequireAuth(middleware.RequireCSRF(http.HandlerFunc(s.totpEnable))))
	mux.Handle("/api/totp/disable", middleware.RequireAuth(middleware.RequireCSRF(http.HandlerFunc(s.totpDisable))))
	mux.Handle("/api/registration-tokens", middleware.RequireAuth(middleware.RequireCSRF(http.HandlerFunc(s.createRegistrationToken))))
	mux.Handle("/api/nodes", middleware.RequireAuth(http.HandlerFunc(s.listNodes)))
	mux.Handle("/api/overview", middleware.RequireAuth(http.HandlerFunc(s.overview)))
	mux.Handle("/api/alerts", middleware.RequireAuth(http.HandlerFunc(s.alertRoute)))
	mux.Handle("/api/alerts/", middleware.RequireAuth(http.HandlerFunc(s.alertRoute)))
	mux.Handle("/api/nodes/", middleware.RequireAuth(http.HandlerFunc(s.nodeRoute)))
	mux.Handle("/api/targets", middleware.RequireAuth(http.HandlerFunc(s.targetRoute)))
	mux.Handle("/api/targets/", middleware.RequireAuth(http.HandlerFunc(s.targetRoute)))
	mux.HandleFunc("/api/agent/v1/register", s.registerAgent)
	mux.HandleFunc("/api/agent/v1/config", s.agentConfig)
	mux.HandleFunc("/api/agent/v1/report", s.reportAgent)
	mux.HandleFunc("/api/agent/v1/network-result", s.networkResultAgent)
	mux.HandleFunc("/api/agent/v1/mtr-result", s.mtrResultAgent)
	mux.HandleFunc("/api/agent/v1/media-result", s.mediaResultAgent)

	protected := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPatch || r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	protectedRoute := middleware.RequireAuth(middleware.RequireCSRF(protected))
	if s.cfg.Environment == "development" {
		mux.Handle("/api/test/protected", protectedRoute)
		mux.Handle("/api/me/protected", protectedRoute)
	}
	mux.Handle("/api/me", middleware.RequireAuth(http.HandlerFunc(s.me)))

	distFS, err := frontend.DistFS()
	if err == nil {
		fileServer := http.FileServer(http.FS(distFS))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/auth/") || r.URL.Path == "/healthz" {
				http.NotFound(w, r)
				return
			}
			cleanPath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
			if cleanPath == "" || cleanPath == "." {
				fileServer.ServeHTTP(w, r)
				return
			}
			f, err := distFS.Open(cleanPath)
			if err == nil {
				_ = f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
			// SPA fallback: serve index.html for client-side routing
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
		})
	}

	return securityHeaders(mux)
}

func (s *Server) targetRoute(w http.ResponseWriter, r *http.Request) {
	if isWriteMethod(r.Method) {
		NewMiddleware(s.service, s.cfg).RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/targets" {
				s.targetCollection(w, r)
				return
			}
			s.targetAction(w, r)
		})).ServeHTTP(w, r)
		return
	}
	if r.URL.Path == "/api/targets" {
		s.targetCollection(w, r)
		return
	}
	s.targetAction(w, r)
}

func (s *Server) nodeRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		trimmed := strings.Trim(r.URL.Path, "/")
		parts := strings.Split(trimmed, "/")
		if len(parts) >= 3 && parts[0] == "api" && parts[1] == "nodes" {
			if len(parts) == 5 && parts[3] == "checks" && parts[4] == "summary" {
				s.nodeChecksSummary(w, r, parts[2])
				return
			}
			if len(parts) == 4 && parts[3] == "traffic" {
				s.nodeTraffic(w, r, parts[2])
				return
			}
		}
		if strings.HasSuffix(trimmed, "/mtr/history") || strings.HasSuffix(trimmed, "/media/history") || strings.HasSuffix(trimmed, "/resource/history") || strings.HasSuffix(trimmed, "/network/history") || strings.HasSuffix(trimmed, "/mtr") || strings.HasSuffix(trimmed, "/media") || strings.HasSuffix(trimmed, "/resource") || strings.HasSuffix(trimmed, "/network") {
			s.nodeRead(w, r)
			return
		}
		if parts := strings.Split(trimmed, "/"); len(parts) == 3 && parts[0] == "api" && parts[1] == "nodes" {
			s.nodeSummaryRead(w, r, parts[2])
			return
		}
	}
	NewMiddleware(s.service, s.cfg).RequireCSRF(http.HandlerFunc(s.nodeAction)).ServeHTTP(w, r)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	user, err := s.service.CurrentUser(r, time.Now().UTC())
	if err != nil {
		writeAuthenticationError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"id":       user.ProviderUserID,
		"provider": user.Provider,
		"login":    user.Login,
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) githubStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.service.BeginOAuth(w, r)
}

func (s *Server) githubCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.service.Callback(w, r)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/auth/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

type localLoginRequest struct {
	Password string `json:"password"`
}

func (s *Server) localLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req localLoginRequest
	if err := decodeJSONRequest(w, r, s.requestBodyLimit(), &req); err != nil {
		writeRequestError(w, err)
		return
	}
	if err := s.service.AuthenticateLocalPassword(w, r, req.Password); err != nil {
		writeJSONError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"user": map[string]string{
			"id":       "admin",
			"provider": "local",
			"login":    "admin",
		},
	})
}
