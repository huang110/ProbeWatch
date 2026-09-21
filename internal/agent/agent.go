package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/probewatch/probewatch/internal/config"
	"github.com/probewatch/probewatch/internal/monitor"
	"github.com/probewatch/probewatch/internal/protocol"
)

const (
	maxReportAttempts = 3
	reportRetryDelay  = 100 * time.Millisecond
	maxOutboxFiles    = 100
	maxOutboxFileSize = 1 << 20
	maxOutboxAttempts = 8
)

// Runner is the outbound-only monitoring agent. It never opens a listener.
type Runner struct {
	cfg       config.Config
	client    *http.Client
	mu        sync.RWMutex
	tasks     []protocol.CheckTask
	next      map[string]time.Time
	now       func() time.Time
	startedAt int64
	cpu       cpuSampler
	probe     networkMonitor
	media     mediaMonitor
	mtr       mtrMonitor
}

type networkMonitor interface {
	Run(context.Context, protocol.CheckTask) protocol.NetworkResult
}
type mediaMonitor interface {
	Run(context.Context, protocol.CheckTask) protocol.MediaResult
}
type mtrMonitor interface {
	Run(context.Context, protocol.CheckTask) protocol.MTRResult
}
type cpuSampler interface{ Sample() (float64, error) }

type queuedReport struct {
	RequestID string          `json:"request_id"`
	Payload   json.RawMessage `json:"payload"`
	Attempts  int             `json:"attempts"`
	NextTry   time.Time       `json:"next_try"`
}

func New(cfg config.Config) (*Runner, error) {
	if strings.TrimSpace(cfg.AgentEndpoint) == "" || strings.TrimSpace(cfg.AgentNodeUUID) == "" || strings.TrimSpace(cfg.AgentNodeToken) == "" {
		return nil, fmt.Errorf("agent endpoint, node UUID, and node token are required")
	}
	return &Runner{cfg: cfg, client: &http.Client{Timeout: 35 * time.Second}, next: make(map[string]time.Time), now: time.Now, startedAt: processStartTime(), cpu: newCPUTracker(), probe: &monitor.Probe{}, media: &monitor.MediaDetector{}, mtr: &monitor.MTRMonitor{}}, nil
}

func (r *Runner) Run(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_ = r.replayOutbox(ctx)
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
	defer r.mu.Unlock()
	updated := append([]protocol.CheckTask(nil), response.Tasks...)
	valid := make(map[string]struct{}, len(updated))
	for _, task := range updated {
		valid[task.ID] = struct{}{}
	}
	for id := range r.next {
		if _, ok := valid[id]; !ok {
			delete(r.next, id)
		}
	}
	r.tasks = updated
	return nil
}

func (r *Runner) report(ctx context.Context) error {
	r.mu.Lock()
	tasks := append([]protocol.CheckTask(nil), r.tasks...)
	now := r.now()
	due := make([]protocol.CheckTask, 0, len(tasks))
	for _, task := range tasks {
		if !task.Enabled {
			continue
		}
		next, ok := r.next[task.ID]
		if !ok || !now.Before(next) {
			due = append(due, task)
			r.next[task.ID] = now.Add(time.Duration(task.IntervalSeconds) * time.Second)
		}
	}
	r.mu.Unlock()
	results := make([]protocol.CheckResult, 0, len(due))
	for _, task := range due {
		var result protocol.CheckResult
		switch task.Kind {
		case "mtr":
			value := r.mtr.Run(ctx, task)
			result = protocol.CheckResult{ID: task.ID, Kind: task.Kind, MTR: &value}
		case "media_http":
			value := r.media.Run(ctx, task)
			result = protocol.CheckResult{ID: task.ID, Kind: task.Kind, Media: &value}
		default:
			value := r.probe.Run(ctx, task)
			result = protocol.CheckResult{ID: task.ID, Kind: task.Kind, Network: &value}
		}
		results = append(results, result)
	}
	request := protocol.ReportRequest{NodeUUID: r.cfg.AgentNodeUUID, ReportedAt: r.now().UTC().Unix(), Resource: collectResourceWith(r.startedAt, r.cpu), Results: results}
	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}
	requestID, err := newRequestID()
	if err != nil {
		return err
	}
	if err = r.sendReport(ctx, requestID, payload); err == nil {
		return nil
	}
	return r.enqueueReport(queuedReport{RequestID: requestID, Payload: payload, Attempts: 1, NextTry: time.Now().UTC().Add(reportRetryDelay)})
}

func newRequestID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (r *Runner) sendReport(ctx context.Context, requestID string, payload []byte) error {
	var last error
	for attempt := 0; attempt < maxReportAttempts; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(reportRetryDelay * time.Duration(1<<(attempt-1)))
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		last = r.doJSONWithRequest(ctx, http.MethodPost, "/report", requestID, payload)
		if last == nil {
			return nil
		}
	}
	return last
}

func (r *Runner) doJSONWithRequest(ctx context.Context, method, path, requestID string, payload []byte) error {
	endpoint := strings.TrimRight(r.cfg.AgentEndpoint, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.cfg.AgentNodeToken)
	req.Header.Set("X-Probe-Timestamp", fmt.Sprintf("%d", time.Now().UTC().Unix()))
	req.Header.Set("X-Probe-Request-ID", requestID)
	req.Header.Set("Content-Type", "application/json")
	response, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("control plane returned HTTP %d", response.StatusCode)
	}
	return nil
}

func (r *Runner) doJSON(ctx context.Context, method, path string, body any, destination any) error {
	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	endpoint := strings.TrimRight(r.cfg.AgentEndpoint, "/") + path
	var reader io.Reader = strings.NewReader(string(payload))
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.cfg.AgentNodeToken)
	req.Header.Set("X-Probe-Timestamp", fmt.Sprintf("%d", time.Now().UTC().Unix()))
	requestID, err := newRequestID()
	if err != nil {
		return err
	}
	req.Header.Set("X-Probe-Request-ID", requestID)
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

func (r *Runner) outboxDir() (string, error) {
	base := strings.TrimSpace(r.cfg.AgentDataDir)
	if base == "" {
		return "", nil
	}
	clean := filepath.Clean(base)
	if clean == "." || clean == string(filepath.Separator) {
		return "", fmt.Errorf("invalid agent data directory")
	}
	return filepath.Join(clean, "outbox"), nil
}

func (r *Runner) enqueueReport(item queuedReport) error {
	dir, err := r.outboxDir()
	if err != nil {
		return err
	}
	if dir == "" {
		return fmt.Errorf("agent data directory is not configured")
	}
	if len(item.Payload) > maxOutboxFileSize {
		return fmt.Errorf("report exceeds outbox file size limit")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	_ = os.Chmod(dir, 0700)
	files, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	count := 0
	for _, entry := range files {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			count++
		}
	}
	if count >= maxOutboxFiles {
		return fmt.Errorf("outbox queue limit reached")
	}
	name := fmt.Sprintf("%020d-%s.json", time.Now().UTC().UnixNano(), item.RequestID)
	data, err := json.Marshal(item)
	if err != nil {
		return err
	}
	if len(data) > maxOutboxFileSize {
		return fmt.Errorf("report exceeds outbox file size limit")
	}
	tmp, err := os.CreateTemp(dir, ".tmp-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(data)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmpName, filepath.Join(dir, name))
}

func (r *Runner) replayOutbox(ctx context.Context) error {
	dir, err := r.outboxDir()
	if err != nil || dir == "" {
		return err
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, statErr := entry.Info()
		if statErr != nil || info.Size() > maxOutboxFileSize {
			_ = os.Remove(path)
			continue
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		var item queuedReport
		if json.Unmarshal(data, &item) != nil || item.RequestID == "" || len(item.Payload) == 0 || len(item.Payload) > maxOutboxFileSize {
			_ = os.Remove(path)
			continue
		}
		if item.Attempts >= maxOutboxAttempts {
			continue
		}
		if err := r.sendReport(ctx, item.RequestID, item.Payload); err == nil {
			_ = os.Remove(path)
		} else {
			item.Attempts++
			item.NextTry = time.Now().UTC().Add(reportRetryDelay * time.Duration(1<<min(item.Attempts, 6)))
			if updated, marshalErr := json.Marshal(item); marshalErr == nil && len(updated) <= maxOutboxFileSize {
				_ = os.WriteFile(path, updated, 0600)
			}
		}
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
