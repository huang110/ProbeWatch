package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/probewatch/probewatch/internal/agent"
	"github.com/probewatch/probewatch/internal/api"
	"github.com/probewatch/probewatch/internal/auth"
	"github.com/probewatch/probewatch/internal/config"
	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/notify"
	"github.com/probewatch/probewatch/internal/version"
)

// ErrControlPlaneNotConfigured marks the future API/store runtime boundary.
var ErrControlPlaneNotConfigured = errors.New("control-plane runtime is not configured for this task")

// ErrAgentNotConfigured marks the future outbound Agent runtime boundary.
var ErrAgentNotConfigured = errors.New("agent runtime is not configured for this task")

// StartControlPlane is the replacement boundary for the future API and store runtime.
func StartControlPlane(cfg config.Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return StartControlPlaneContext(ctx, cfg)
}

func StartControlPlaneContext(ctx context.Context, cfg config.Config) error {
	if strings.TrimSpace(cfg.ListenAddress) == "" {
		return fmt.Errorf("control-plane listen address is required")
	}
	pepper := cfg.TokenPepper
	if strings.TrimSpace(pepper) == "" && cfg.Environment == "development" {
		pepper = config.DefaultDevelopmentTokenPepper
	}
	if len([]byte(pepper)) < 32 {
		return fmt.Errorf("control-plane token pepper must be at least 32 bytes")
	}
	store, err := db.OpenStore(cfg.DatabasePath, []byte(pepper))
	if err != nil {
		return fmt.Errorf("open control-plane store: %w", err)
	}
	defer store.Close()

	service := auth.NewService(cfg, store, auth.ProviderEndpoints{
		AuthorizeURL:              "https://github.com/login/oauth/authorize",
		TokenURL:                  "https://github.com/login/oauth/access_token",
		UserURL:                   "https://api.github.com/user",
		OrganizationsURL:          "https://api.github.com/user/orgs",
		OrganizationMembershipURL: "https://api.github.com/user/memberships/orgs/{org}",
	})
	cleanupCtx, cleanupCancel := context.WithCancel(ctx)
	defer cleanupCancel()
	go runAuthCleanup(cleanupCtx, service)
	go runLifecycleCleanup(cleanupCtx, store)
	go runHistoryAggregation(cleanupCtx, store)
	go runBillingCycleWatcher(cleanupCtx, store)
	notifier := notify.NewNotifier(cfg)
	go notify.RunAlertDispatcher(cleanupCtx, store, notifier)
	serverInstance := api.NewServer(cfg, service)
	serverInstance.SetNotifier(notifier)
	if serverInstance.BackupScheduler() != nil {
		go serverInstance.BackupScheduler().Start(cleanupCtx)
	}
	handler := serverInstance.Handler()
	listener, err := net.Listen("tcp", cfg.ListenAddress)
	if err != nil {
		return fmt.Errorf("listen for control plane: %w", err)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	shutdownDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_ = server.Shutdown(shutdownCtx)
			cancel()
		case <-shutdownDone:
		}
	}()
	err = server.Serve(listener)
	close(shutdownDone)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func runAuthCleanup(ctx context.Context, service *auth.Service) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	cleanup := func() {
		cleanupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if _, err := service.CleanupExpiredOAuthStates(cleanupCtx, time.Now().UTC()); err != nil {
			slog.Error("auth cleanup failed", "resource", "oauth_states", "error_class", fmt.Sprintf("%T", err))
		}
		if _, err := service.CleanupExpiredTOTPPendingStates(cleanupCtx, time.Now().UTC()); err != nil {
			slog.Error("auth cleanup failed", "resource", "totp_pending_states", "error_class", fmt.Sprintf("%T", err))
		}
		if _, err := service.CleanupExpiredSessions(cleanupCtx, time.Now().UTC()); err != nil {
			slog.Error("auth cleanup failed", "resource", "sessions", "error_class", fmt.Sprintf("%T", err))
		}
	}
	cleanup()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanup()
		}
	}
}

func runLifecycleCleanup(ctx context.Context, store *db.Store) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	cleanup := func() {
		cleanupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if _, err := store.CleanupLifecycle(cleanupCtx, time.Now().UTC()); err != nil {
			slog.Error("lifecycle cleanup failed", "error_class", fmt.Sprintf("%T", err))
		}
	}
	cleanup()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanup()
		}
	}
}

// runHistoryAggregation folds raw history older than the retention window into
// the hourly and daily aggregate tables once per hour. A failed pass is logged
// and retried on the next tick; it never stops the control plane.
func runHistoryAggregation(ctx context.Context, store *db.Store) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	aggregate := func() {
		aggregateCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		boundary := time.Now().UTC().Add(-db.DefaultAggregateRetentionWindow)
		if _, err := store.AggregateHistory(aggregateCtx, boundary); err != nil {
			slog.Error("history aggregation failed", "error_class", fmt.Sprintf("%T", err))
		}
	}
	aggregate()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			aggregate()
		}
	}
}

func runBillingCycleWatcher(ctx context.Context, store *db.Store) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	check := func() {
		checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if _, err := store.CheckAndAutoResetBillingCycles(checkCtx, time.Now().UTC()); err != nil {
			slog.Error("billing cycle auto-reset check failed", "error_class", fmt.Sprintf("%T", err), "error", err)
		}
	}
	check()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check()
		}
	}
}

func StartAgent(cfg config.Config) error {
	runner, err := agent.New(cfg)
	if err != nil {
		return fmt.Errorf("agent runtime: %w: %v", ErrAgentNotConfigured, err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runner.Run(ctx)
}

// CheckAgentUpdate checks if a newer agent binary is available from the configured control plane.
func CheckAgentUpdate() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	fmt.Printf("Current version: %s\n", version.FullAgentVersionString())
	fmt.Printf("Checking for updates from: %s ...\n", cfg.AgentEndpoint)

	info, err := agent.CheckUpdate(ctx, nil, cfg.AgentEndpoint, cfg.AgentNodeToken)
	if err != nil {
		return fmt.Errorf("check update failed: %w", err)
	}

	fmt.Printf("Control plane version: %s\n", info.ServerVersion)
	fmt.Printf("Latest available agent: %s\n", info.LatestAgentVersion)
	if info.UpdateAvailable {
		fmt.Printf("\n[!] A new version is available! Run 'probewatch-agent --self-update' to upgrade.\n")
		if info.ReleaseNotes != "" {
			fmt.Printf("Release Notes: %s\n", info.ReleaseNotes)
		}
	} else {
		fmt.Println("[OK] ProbeWatch Agent is up to date!")
	}
	return nil
}

// SelfUpdateAgent downloads, verifies, and installs the latest agent binary in place.
func SelfUpdateAgent() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	fmt.Printf("Current version: %s\n", version.FullAgentVersionString())
	fmt.Printf("Checking for updates from: %s ...\n", cfg.AgentEndpoint)

	info, err := agent.CheckUpdate(ctx, nil, cfg.AgentEndpoint, cfg.AgentNodeToken)
	if err != nil {
		return fmt.Errorf("check update failed: %w", err)
	}

	if !info.UpdateAvailable {
		fmt.Printf("[OK] Agent is already on the latest version (%s). No upgrade needed.\n", version.AgentVersion)
		return nil
	}

	fmt.Printf("Downloading upgrade to v%s ...\n", info.LatestAgentVersion)
	if err := agent.DownloadAndApplyUpdate(ctx, nil, cfg.AgentEndpoint, cfg.AgentNodeToken, info, cfg.AgentDataDir); err != nil {
		return fmt.Errorf("self-update failed: %w", err)
	}

	fmt.Printf("[SUCCESS] ProbeWatch Agent successfully updated to v%s!\n", info.LatestAgentVersion)
	fmt.Println("Please restart the service: 'systemctl restart probewatch-agent' (or supervisor will automatically reload).")
	return nil
}
