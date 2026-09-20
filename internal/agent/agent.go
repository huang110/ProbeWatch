package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/probewatch/probewatch/internal/config"
	"github.com/probewatch/probewatch/internal/monitor"
	"github.com/probewatch/probewatch/internal/protocol"
)

// Runner is the outbound-only monitoring agent. It never opens a listener.
type Runner struct {
	cfg    config.Config
	client *http.Client
	mu     sync.RWMutex
	tasks  []protocol.CheckTask
}

func New(cfg config.Config) (*Runner, error) {
	if strings.TrimSpace(cfg.AgentEndpoint) == "" || strings.TrimSpace(cfg.AgentNodeUUID) == "" || strings.TrimSpace(cfg.AgentNodeToken) == "" {
		return nil, fmt.Errorf("agent endpoint, node UUID, and node token are required")
	}
	return &Runner{cfg: cfg, client: &http.Client{Timeout: 35 * time.Second}}, nil
}

func (r *Runner) Run(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.refresh(ctx); err != nil {
		return fmt.Errorf("initial config refresh: %w", err)
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	configTicker := time.NewTicker(5 * time.Minute)
	defer configTicker.Stop()
	for {
		if err := r.report(ctx); err != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		case <-configTicker.C:
			_ = r.refresh(ctx)
		}
	}
}

func (r *Runner) refresh(ctx context.Context) error {
	var response protocol.AgentConfigResponse
	if err := r.doJSON(ctx, http.MethodGet, "/config", nil, &response); err != nil {
		return err
	}
	if err := response.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	r.tasks = append([]protocol.CheckTask(nil), response.Tasks...)
	r.mu.Unlock()
	return nil
}

func (r *Runner) report(ctx context.Context) error {
	r.mu.RLock()
	tasks := append([]protocol.CheckTask(nil), r.tasks...)
	r.mu.RUnlock()
	results := make([]protocol.CheckResult, 0, len(tasks))
	for _, task := range tasks {
		if !task.Enabled || task.Kind == "mtr" || task.Kind == "media_http" {
			continue
		}
		result := (&monitor.Probe{}).Run(ctx, task)
		results = append(results, protocol.CheckResult{ID: task.ID, Kind: task.Kind, Network: &result})
	}
	now := time.Now().UTC()
	request := protocol.ReportRequest{NodeUUID: r.cfg.AgentNodeUUID, ReportedAt: now.Unix(), Resource: collectResource(), Results: results}
	return r.doJSON(ctx, http.MethodPost, "/report", request, nil)
}

func collectResource() protocol.ResourceSnapshot {
	return protocol.ResourceSnapshot{OS: runtime.GOOS, Arch: runtime.GOARCH, AgentVersion: "0.1.0", StartedAt: time.Now().UTC().Unix()}
}

func (r *Runner) doJSON(ctx context.Context, method, path string, body any, destination any) error {
	var reader *strings.Reader
	if body == nil {
		reader = strings.NewReader("")
	} else {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = strings.NewReader(string(encoded))
	}
	endpoint := strings.TrimRight(r.cfg.AgentEndpoint, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.cfg.AgentNodeToken)
	req.Header.Set("X-Probe-Timestamp", fmt.Sprintf("%d", time.Now().UTC().Unix()))
	requestID := make([]byte, 16)
	if _, err := rand.Read(requestID); err != nil {
		return err
	}
	req.Header.Set("X-Probe-Request-ID", hex.EncodeToString(requestID))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("control plane returned HTTP %d", response.StatusCode)
	}
	if destination != nil {
		return json.NewDecoder(response.Body).Decode(destination)
	}
	return nil
}
