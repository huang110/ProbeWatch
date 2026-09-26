package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/probewatch/probewatch/frontend"
	"github.com/probewatch/probewatch/internal/ai"
	"github.com/probewatch/probewatch/internal/auth"
	"github.com/probewatch/probewatch/internal/backup"
	"github.com/probewatch/probewatch/internal/config"
	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/deploy"
	"github.com/probewatch/probewatch/internal/notify"
	"github.com/probewatch/probewatch/internal/terminal"
)

type Server struct {
	cfg           config.Config
	service       *auth.Service
	aiService     *ai.AIService
	notifier      *notify.Notifier
	agentLimiter  *rateLimiter
	agentIPLimiter *rateLimiter
	registrationLimiter *rateLimiter
	publicLimiter *rateLimiter
	loginLimiter  *rateLimiter
	totpLimiter   *rateLimiter
	terminalManager *terminal.Manager
	backupScheduler *backup.Scheduler
	publicCacheMu sync.RWMutex
	publicCacheAt time.Time
	publicCache    publicStatusResponse
}

func NewServer(cfg config.Config, service *auth.Service) *Server {
	n := notify.NewNotifier(cfg)
	var aiSvc *ai.AIService
	var backupSched *backup.Scheduler
	if service != nil && service.Store() != nil {
		n.SetStore(service.Store())
		aiSvc = ai.NewAIService(service.Store(), &cfg)
		bDir := filepath.Join(filepath.Dir(cfg.DatabasePath), "backups")
		if cfg.DatabasePath == "" {
			bDir = filepath.Join("data", "backups")
		}
		backupSched = backup.NewScheduler(service.Store(), bDir)
	}
	return &Server{
		cfg:           cfg,
		service:       service,
		aiService:     aiSvc,
		notifier:      n,
		agentLimiter:  newRateLimiter(120, time.Minute, 10000),
		agentIPLimiter: newRateLimiter(240, time.Minute, 10000),
		registrationLimiter: newRateLimiter(10, time.Minute, 10000),
		publicLimiter: newRateLimiter(60, time.Minute, 10000),
		loginLimiter:  newRateLimiter(10, time.Minute, 10000),
		totpLimiter:   newRateLimiter(6, time.Minute, 10000),
		terminalManager: terminal.NewManager(),
		backupScheduler: backupSched,
	}
}

func (s *Server) SetNotifier(n *notify.Notifier) {
	s.notifier = n
}

func (s *Server) SetAIService(svc *ai.AIService) {
	s.aiService = svc
}

func (s *Server) BackupScheduler() *backup.Scheduler {
	return s.backupScheduler
}

func (s *Server) SetBackupScheduler(sched *backup.Scheduler) {
	s.backupScheduler = sched
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
	mux.HandleFunc("/api/public/status-page", s.publicStatusPageHandler)
	mux.HandleFunc("/api/public/incidents", s.publicIncidentsHandler)
	mux.HandleFunc("/api/public/nodes/", s.publicNodeRoute)
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
	mux.Handle("/api/settings", middleware.RequireAuth(http.HandlerFunc(s.settingsRoute)))
	mux.Handle("/api/settings/", middleware.RequireAuth(http.HandlerFunc(s.settingsRoute)))
	mux.Handle("/api/nodes/", middleware.RequireAuth(http.HandlerFunc(s.nodeRoute)))
	mux.Handle("/api/targets", middleware.RequireAuth(http.HandlerFunc(s.targetRoute)))
	mux.Handle("/api/targets/", middleware.RequireAuth(http.HandlerFunc(s.targetRoute)))
	mux.Handle("/api/system/backups", middleware.RequireAuth(http.HandlerFunc(s.backupRoute)))
	mux.Handle("/api/system/backups/", middleware.RequireAuth(http.HandlerFunc(s.backupRoute)))
	mux.Handle("/api/users", middleware.RequireAuth(http.HandlerFunc(s.usersRoute)))
	mux.Handle("/api/users/", middleware.RequireAuth(http.HandlerFunc(s.usersRoute)))
	mux.Handle("/api/tokens", middleware.RequireAuth(http.HandlerFunc(s.tokensRoute)))
	mux.Handle("/api/tokens/", middleware.RequireAuth(http.HandlerFunc(s.tokensRoute)))
	mux.Handle("/api/audit-logs", middleware.RequireAuth(http.HandlerFunc(s.auditLogsRoute)))
	mux.Handle("/api/certificates", middleware.RequireAuth(http.HandlerFunc(s.certificatesHandler)))
	mux.HandleFunc("/api/public/certificates", s.certificatesHandler)
	mux.Handle("/api/dns-matrix", middleware.RequireAuth(http.HandlerFunc(s.dnsMatrixHandler)))
	mux.HandleFunc("/api/public/dns-matrix", s.dnsMatrixHandler)
	mux.HandleFunc("/api/admin/status-page", s.adminStatusPageRoute)
	mux.HandleFunc("/api/admin/incidents", s.adminIncidentsRoute)
	mux.HandleFunc("/api/admin/incidents/", s.adminIncidentsRoute)
	mux.HandleFunc("/api/openapi.json", s.openAPIJSONHandler)
	mux.HandleFunc("/docs", s.swaggerUIHandler)
	mux.HandleFunc("/api/docs", s.swaggerUIHandler)
	mux.HandleFunc("/api/agent/v1/register", s.registerAgent)
	mux.HandleFunc("/api/agent/v1/config", s.agentConfig)
	mux.HandleFunc("/api/agent/v1/report", s.reportAgent)
	mux.HandleFunc("/api/agent/v1/network-result", s.networkResultAgent)
	mux.HandleFunc("/api/agent/v1/mtr-result", s.mtrResultAgent)
	mux.HandleFunc("/api/agent/v1/media-result", s.mediaResultAgent)
	mux.HandleFunc("/api/agent/v1/update/check", s.agentUpdateCheck)
	mux.HandleFunc("/api/agent/v1/update/download", s.agentUpdateDownload)

	// WebAuthn / Passkey endpoints
	mux.HandleFunc("/api/webauthn/login/begin", s.webauthnLoginBegin)
	mux.HandleFunc("/api/webauthn/login/finish", s.webauthnLoginFinish)
	mux.Handle("/api/webauthn/register/begin", middleware.RequireAuth(middleware.RequireCSRF(http.HandlerFunc(s.webauthnRegisterBegin))))
	mux.Handle("/api/webauthn/register/finish", middleware.RequireAuth(middleware.RequireCSRF(http.HandlerFunc(s.webauthnRegisterFinish))))
	mux.Handle("/api/webauthn/credentials", middleware.RequireCSRF(http.HandlerFunc(s.webauthnCredentialsRoute)))
	mux.Handle("/api/webauthn/credentials/", middleware.RequireCSRF(http.HandlerFunc(s.webauthnCredentialsRoute)))
	mux.HandleFunc("/api/public/version", s.publicVersion)

	// AI Copilot & Diagnostics endpoints
	mux.Handle("/api/ai/diagnose", middleware.RequireAuth(http.HandlerFunc(s.aiDiagnose)))
	mux.Handle("/api/ai/chat", middleware.RequireAuth(http.HandlerFunc(s.aiChat)))
	mux.Handle("/api/ai/settings", middleware.RequireAuth(http.HandlerFunc(s.aiSettingsRoute)))
	mux.Handle("/api/ai/mcp/token", middleware.RequireAuth(middleware.RequireCSRF(http.HandlerFunc(s.aiMCPTokenRegenerate))))
	mux.Handle("/api/mcp/config", middleware.RequireAuth(http.HandlerFunc(s.mcpConfig)))

	// MCP (Model Context Protocol) endpoints
	mux.HandleFunc("/api/mcp", s.mcpHandler)
	mux.HandleFunc("/mcp", s.mcpHandler)

	// Deployment script endpoints
	
	// Terminal & Remote Execution endpoints
	mux.HandleFunc("/api/agent/v1/terminal/tunnel", s.agentTerminalTunnel)
	mux.Handle("/api/admin/terminal/status", middleware.RequireAuth(http.HandlerFunc(s.terminalStatus)))
	mux.Handle("/api/admin/terminal/exec", middleware.RequireAuth(middleware.RequireCSRF(http.HandlerFunc(s.terminalExec))))
	mux.HandleFunc("/api/admin/terminal/ws", s.terminalWS)
	mux.HandleFunc("/deploy/install.sh", s.installScriptHandler)
	mux.HandleFunc("/install.sh", s.installScriptHandler)

	protected := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch || r.Method == http.MethodDelete {
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
			if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/auth/") || strings.HasPrefix(r.URL.Path, "/mcp") || strings.HasPrefix(r.URL.Path, "/deploy/") || r.URL.Path == "/install.sh" || r.URL.Path == "/healthz" {
				http.NotFound(w, r)
				return
			}
			cleanPath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
			if cleanPath == "" || cleanPath == "." || cleanPath == "index.html" {
				w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
				w.Header().Set("Pragma", "no-cache")
				w.Header().Set("Expires", "0")
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
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
		})
	}

	return securityHeaders(mux, s.cfg)
}

func (s *Server) targetRoute(w http.ResponseWriter, r *http.Request) {
	if isWriteMethod(r.Method) {
		NewMiddleware(s.service, s.cfg).RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/targets/seed-media" {
				s.seedMediaTargets(w, r)
				return
			}
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
	trimmed := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) >= 3 && parts[0] == "api" && parts[1] == "nodes" {
		uuid := parts[2]
		if len(parts) == 4 && parts[3] == "billing" {
			if r.Method == http.MethodGet {
				s.getNodeBilling(w, r, uuid)
				return
			}
			if r.Method == http.MethodPut || r.Method == http.MethodPost {
				NewMiddleware(s.service, s.cfg).RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					s.putNodeBilling(w, r, uuid)
				})).ServeHTTP(w, r)
				return
			}
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if len(parts) == 5 && parts[3] == "billing" && parts[4] == "reset" {
			if r.Method == http.MethodPost {
				NewMiddleware(s.service, s.cfg).RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					s.resetNodeBilling(w, r, uuid)
				})).ServeHTTP(w, r)
				return
			}
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if r.Method == http.MethodGet {
			if len(parts) == 5 && parts[3] == "checks" && parts[4] == "summary" {
				s.nodeChecksSummary(w, r, uuid)
				return
			}
			if len(parts) == 4 && parts[3] == "traffic" {
				s.nodeTraffic(w, r, uuid)
				return
			}
		}
	}
	if r.Method == http.MethodGet {
		if strings.HasSuffix(trimmed, "/mtr/history") || strings.HasSuffix(trimmed, "/media/history") || strings.HasSuffix(trimmed, "/resource/history") || strings.HasSuffix(trimmed, "/network/history") || strings.HasSuffix(trimmed, "/mtr") || strings.HasSuffix(trimmed, "/media") || strings.HasSuffix(trimmed, "/resource") || strings.HasSuffix(trimmed, "/network") {
			s.nodeRead(w, r)
			return
		}
		if len(parts) == 3 && parts[0] == "api" && parts[1] == "nodes" {
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
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":            user.ProviderUserID,
		"user_id":       user.ID,
		"provider":      user.Provider,
		"login":         user.Login,
		"display_name":  user.DisplayName,
		"role":          user.Role,
		"allowed_nodes": user.AllowedNodes,
		"can_write":     user.CanWrite(),
		"is_admin":      user.IsAdmin(),
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

func securityHeaders(next http.Handler, cfg config.Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; font-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if cfg.Environment == "production" {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		if strings.HasPrefix(r.URL.Path, "/auth/") {
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
		}
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/auth/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

type localLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) localLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.loginLimiter.Allow(publicLimiterKey(r), time.Now().UTC()) {
		writeRateLimitError(w)
		return
	}
	var req localLoginRequest
	if err := decodeJSONRequest(w, r, s.requestBodyLimit(), &req); err != nil {
		writeRequestError(w, err)
		return
	}
	user, err := s.service.AuthenticateUserCredentials(w, r, req.Username, req.Password)
	if err != nil {
		if errors.Is(err, db.ErrAccountDisabled) {
			writeJSONError(w, http.StatusForbidden, "account is disabled")
			return
		}
		writeJSONError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"user": map[string]any{
			"id":           user.ID,
			"provider":     user.Provider,
			"login":        user.Login,
			"display_name": user.DisplayName,
			"role":         user.Role,
			"can_write":    user.CanWrite(),
			"is_admin":     user.IsAdmin(),
		},
	})
}

func writeRateLimitError(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "60")
	writeJSONError(w, http.StatusTooManyRequests, "too many attempts")
}

func (s *Server) installScriptHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	_, _ = w.Write(deploy.InstallScript)
}

