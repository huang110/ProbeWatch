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
	"github.com/probewatch/probewatch/internal/terminal"
)

const (
	maxReportAttempts            = 3
	reportRetryDelay             = 100 * time.Millisecond
	maxOutboxFiles               = 100
	maxOutboxFileSize            = 1 << 20
	maxOutboxAttempts            = 8
	maxOutboxAge                 = 7 * 24 * time.Hour
	maxConfigResponseBytes       = 2 << 20
	initialResourceFollowupDelay = 2 * time.Second
	maxConcurrentFastProbes      = 8
	maxConcurrentHeavyProbes     = 2
	maxBufferedProbeResults      = 100
	defaultProbeTimeout          = 15 * time.Second
	probeFastWaitTimeout         = 40 * time.Millisecond
	maxOutboxReplayBatch         = 10
	outboxReplayTimeout          = 10 * time.Second
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
	hardware  HardwareCollector
	disk      DiskIOTracker
	sockets   SocketCollector
	health    HostHealthCollector
	ipQuality IPQualityCollector
	ipqa      IPQACollector
	probe     networkMonitor
	media     mediaMonitor
	mtr       mtrMonitor
	speedtest speedtestMonitor
	synthetic syntheticMonitor

	// Concurrency & buffer management for network probes
	fastProbeWorkerSem  chan struct{}
	heavyProbeWorkerSem chan struct{}
	probeMu             sync.Mutex
	runningTasks        map[string]struct{}
	bufferedResults     []protocol.CheckResult
	outboxReplayMu      sync.Mutex
	workloadMu          sync.Mutex
	eventsMu            sync.Mutex

	// configMaxAgeSeconds is the freshness window the control plane asked for
	// (0 = no fail-closed enforcement, legacy behavior). lastRefreshSuccessAt
	// is the time of the last successful config refresh. Once the configured
	// window elapses without a successful refresh the runner stops executing
	// probes (fail-closed) and keeps reporting resources only until a refresh
	// succeeds again. lastConfigVersion records the version of the last
	// accepted configuration. All three are guarded by mu.
	configMaxAgeSeconds  int
	lastRefreshSuccessAt time.Time
	lastConfigVersion    int64
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
type speedtestMonitor interface {
	Run(context.Context, protocol.CheckTask) protocol.SpeedtestResult
}
type syntheticMonitor interface {
	Run(context.Context, protocol.CheckTask) protocol.SyntheticResult
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
	return &Runner{
		cfg: cfg,
		client: &http.Client{Timeout: 35 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return fmt.Errorf("agent endpoint redirects are not allowed")
		}},
		next: make(map[string]time.Time), now: time.Now, startedAt: processStartTime(),
		cpu: newCPUTracker(), hardware: defaultHardwareCollector(), disk: newPlatformDiskTracker(), sockets: defaultSocketCollector(), health: defaultHostHealthCollector(), ipQuality: NewIPQualityCollector(), ipqa: NewIPQACollector(),
		probe:               &monitor.Probe{},
		media:               &monitor.MediaDetector{},
		mtr:                 &monitor.MTRMonitor{},
		speedtest:           &monitor.SpeedtestRunner{},
		synthetic:           monitor.NewSyntheticMonitor(),
		fastProbeWorkerSem:  make(chan struct{}, maxConcurrentFastProbes),
		heavyProbeWorkerSem: make(chan struct{}, maxConcurrentHeavyProbes),
		runningTasks:        make(map[string]struct{}),
		bufferedResults:     make([]protocol.CheckResult, 0, 16),
	}, nil
}

func (r *Runner) Run(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_ = r.replayOutbox(ctx)
	if err := r.refresh(ctx); err != nil {
		return fmt.Errorf("initial config refresh: %w", err)
	}
	VerifyAndClearPendingUpgrade(r.cfg.AgentDataDir)

	if r.cfg.EnableTerminal {
		termClient := terminal.NewClient(r.cfg.AgentEndpoint, r.cfg.AgentNodeUUID, r.cfg.AgentNodeToken)
		go termClient.Run(ctx)
	}
	// Publish a resource snapshot before running the first probe batch. Probe
	// targets may have network timeouts, while the console should show the node
	// as soon as the agent is reachable.
	if err := r.reportResources(ctx); err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	// Keep resource and probe freshness aligned with the live console.
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	workloadTicker := time.NewTicker(30 * time.Second)
	defer workloadTicker.Stop()
	eventsTicker := time.NewTicker(10 * time.Second)
	defer eventsTicker.Stop()
	configTicker := time.NewTicker(5 * time.Minute)
	defer configTicker.Stop()
	updateTicker := time.NewTicker(6 * time.Hour)
	defer updateTicker.Stop()
	outboxTicker := time.NewTicker(30 * time.Second)
	defer outboxTicker.Stop()

	// Start the first probe report after the initial resource snapshot has had
	// a moment to settle. This avoids competing disk samples at startup.
	go func() {
		timer := time.NewTimer(initialResourceFollowupDelay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
		case <-timer.C:
			_ = r.report(ctx)
			_ = r.reportWorkload(ctx)
			_ = r.reportEvents(ctx)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := r.report(ctx); err != nil && ctx.Err() != nil {
				return ctx.Err()
			}
		case <-workloadTicker.C:
			go func() { _ = r.reportWorkload(ctx) }()
		case <-eventsTicker.C:
			go func() { _ = r.reportEvents(ctx) }()
		case <-configTicker.C:
			_ = r.refresh(ctx)
		case <-updateTicker.C:
			r.autoCheckUpdate(ctx)
		case <-outboxTicker.C:
			_ = r.replayOutboxLimited(ctx)
		}
	}
}

func (r *Runner) reportEvents(ctx context.Context) error {
	if !r.eventsMu.TryLock() {
		return nil
	}
	defer r.eventsMu.Unlock()
	collector := GetDefaultEventCollector()
	events, err := collector.CollectSystemEvents(ctx)
	if err != nil || len(events) == 0 {
		return nil
	}

	report := protocol.SystemEventBatchReport{
		NodeUUID:   r.cfg.AgentNodeUUID,
		ReportedAt: r.now().UTC().Unix(),
		Events:     events,
	}

	return r.doJSON(ctx, http.MethodPost, "/events", report, nil)
}

func (r *Runner) reportWorkload(ctx context.Context) error {
	if !r.workloadMu.TryLock() {
		return nil
	}
	defer r.workloadMu.Unlock()
	dockerAvail, dockerVer, containers, _ := CollectDockerWorkload()
	procs, _ := CollectTopProcesses(15)

	running := 0
	stopped := 0
	for _, c := range containers {
		if c.State == "running" {
			running++
		} else {
			stopped++
		}
	}

	report := protocol.NodeWorkloadReport{
		NodeUUID:          r.cfg.AgentNodeUUID,
		ReportedAt:        r.now().UTC().Unix(),
		DockerAvailable:   dockerAvail,
		DockerVersion:     dockerVer,
		ContainersTotal:   len(containers),
		ContainersRunning: running,
		ContainersStopped: stopped,
		Containers:        containers,
		TopProcesses:      procs,
	}

	return r.doJSON(ctx, http.MethodPost, "/workload", report, nil)
}

func (r *Runner) autoCheckUpdate(ctx context.Context) {
	result, err := CheckUpdate(ctx, r.client, r.cfg.AgentEndpoint, r.cfg.AgentNodeToken)
	if err == nil && result != nil && result.UpdateAvailable {
		if err := DownloadAndApplyUpdate(ctx, nil, r.cfg.AgentEndpoint, r.cfg.AgentNodeToken, result, r.cfg.AgentDataDir); err == nil {
			_ = ApplyRuntimeStrategy()
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
	if r.lastConfigVersion > 0 && response.ConfigVersion <= 0 {
		return fmt.Errorf("configuration version regressed from %d to legacy", r.lastConfigVersion)
	}
	if response.ConfigVersion > 0 && r.lastConfigVersion > 0 && response.ConfigVersion < r.lastConfigVersion {
		// Tolerate minor backward clock skew (e.g. NTP sync adjustment <= 900s) without fail-closed halt
		if (r.lastConfigVersion - response.ConfigVersion) <= 900 {
			response.ConfigVersion = r.lastConfigVersion
		} else {
			return fmt.Errorf("configuration version regressed from %d to %d", r.lastConfigVersion, response.ConfigVersion)
		}
	}
	if response.SyncIPQA {
		if r.ipqa != nil {
			if inv, ok := r.ipqa.(interface{ Invalidate() }); ok {
				inv.Invalidate()
				_ = r.ipqa.Get()
			}
		}
	}
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
	// A new config version replaces the task set without disturbing running
	// schedules: surviving tasks keep their next-due entries, removed ones are
	// pruned above. Version and freshness tracking only affect fail-closed
	// staleness checks, never which tasks are currently scheduled.
	r.tasks = updated
	r.lastConfigVersion = response.ConfigVersion
	r.configMaxAgeSeconds = response.ConfigMaxAgeSeconds
	r.lastRefreshSuccessAt = r.now()
	return nil
}

func isFastProbeKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "tcp", "http", "https", "dns", "ping", "icmp":
		return true
	default:
		return false
	}
}

func (r *Runner) selectProbeSem(kind string) chan struct{} {
	if isFastProbeKind(kind) {
		return r.fastProbeWorkerSem
	}
	return r.heavyProbeWorkerSem
}

func (r *Runner) runProbeTask(ctx context.Context, task protocol.CheckTask) {
	sem := r.selectProbeSem(task.Kind)
	select {
	case sem <- struct{}{}:
	case <-ctx.Done():
		r.probeMu.Lock()
		delete(r.runningTasks, task.ID)
		r.probeMu.Unlock()
		return
	}
	defer func() { <-sem }()

	taskTimeout := defaultProbeTimeout
	if task.TimeoutMS > 0 {
		taskTimeout = time.Duration(task.TimeoutMS) * time.Millisecond
		if taskTimeout > 30*time.Second {
			taskTimeout = 30 * time.Second
		}
	}
	taskCtx, cancel := context.WithTimeout(ctx, taskTimeout)
	defer cancel()

	var result protocol.CheckResult
	switch task.Kind {
	case "mtr":
		value := r.mtr.Run(taskCtx, task)
		result = protocol.CheckResult{ID: task.ID, Kind: task.Kind, MTR: &value}
	case "media_http":
		value := r.media.Run(taskCtx, task)
		result = protocol.CheckResult{ID: task.ID, Kind: task.Kind, Media: &value}
	case "speedtest":
		value := r.speedtest.Run(taskCtx, task)
		result = protocol.CheckResult{ID: task.ID, Kind: task.Kind, Speedtest: &value}
	case "grpc", "websocket", "doh", "synthetic":
		value := r.synthetic.Run(taskCtx, task)
		result = protocol.CheckResult{ID: task.ID, Kind: task.Kind, Synthetic: &value}
	default:
		value := r.probe.Run(taskCtx, task)
		result = protocol.CheckResult{ID: task.ID, Kind: task.Kind, Network: &value}
	}

	r.probeMu.Lock()
	delete(r.runningTasks, task.ID)
	if len(r.bufferedResults) < maxBufferedProbeResults {
		r.bufferedResults = append(r.bufferedResults, result)
	}
	r.probeMu.Unlock()
}

func (r *Runner) report(ctx context.Context) error {
	r.mu.Lock()
	tasks := append([]protocol.CheckTask(nil), r.tasks...)
	now := r.now()
	// Fail-closed: when the control plane declared a config freshness window
	// and it expired without a successful refresh, stop executing new probes
	// with the stale configuration. Resource reporting continues with empty
	// results; probing resumes automatically after a successful refresh.
	stale := r.configMaxAgeSeconds > 0 && now.Sub(r.lastRefreshSuccessAt) > time.Duration(r.configMaxAgeSeconds)*time.Second
	due := make([]protocol.CheckTask, 0, len(tasks))
	if !stale {
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
	}
	r.mu.Unlock()

	// Dispatch due tasks to bounded worker pool without blocking the heartbeat loop
	toLaunch := make([]protocol.CheckTask, 0, len(due))
	r.probeMu.Lock()
	if r.runningTasks == nil {
		r.runningTasks = make(map[string]struct{})
	}
	if r.fastProbeWorkerSem == nil {
		r.fastProbeWorkerSem = make(chan struct{}, maxConcurrentFastProbes)
	}
	if r.heavyProbeWorkerSem == nil {
		r.heavyProbeWorkerSem = make(chan struct{}, maxConcurrentHeavyProbes)
	}
	for _, task := range due {
		if _, running := r.runningTasks[task.ID]; !running {
			r.runningTasks[task.ID] = struct{}{}
			toLaunch = append(toLaunch, task)
		}
	}
	r.probeMu.Unlock()

	if len(toLaunch) > 0 {
		var wg sync.WaitGroup
		for _, t := range toLaunch {
			wg.Add(1)
			go func(task protocol.CheckTask) {
				defer wg.Done()
				r.runProbeTask(ctx, task)
			}(t)
		}

		// Wait briefly for fast tasks so immediate results can be bundled,
		// but never block the heartbeat on slow tasks.
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(probeFastWaitTimeout):
		case <-ctx.Done():
		}
	}

	// Drain any completed probe results from buffer
	r.probeMu.Lock()
	results := r.bufferedResults
	r.bufferedResults = make([]protocol.CheckResult, 0, 16)
	r.probeMu.Unlock()

	request := protocol.ReportRequest{
		NodeUUID:   r.cfg.AgentNodeUUID,
		ReportedAt: r.now().UTC().Unix(),
		Resource:   collectResourceWith(r.startedAt, r.cpu, r.hardware, r.disk, r.sockets, r.health, r.ipQuality, r.ipqa),
		Results:    results,
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}
	requestID, err := newRequestID()
	if err != nil {
		return err
	}
	if err = r.sendReport(ctx, requestID, payload); err == nil {
		go func() { _ = r.replayOutboxLimited(context.Background()) }()
		return nil
	}
	return r.enqueueReport(queuedReport{RequestID: requestID, Payload: payload, Attempts: 1, NextTry: time.Now().UTC().Add(reportRetryDelay)})
}

// reportResources sends an immediate resource-only heartbeat. It keeps first
// paint independent from the latency of configured network probes.
func (r *Runner) reportResources(ctx context.Context) error {
	payload, err := json.Marshal(protocol.ReportRequest{
		NodeUUID:   r.cfg.AgentNodeUUID,
		ReportedAt: r.now().UTC().Unix(),
		Resource:   collectResourceWith(r.startedAt, r.cpu, r.hardware, r.disk, r.sockets, r.health, r.ipQuality, r.ipqa),
		Results:    []protocol.CheckResult{},
	})
	if err != nil {
		return err
	}
	requestID, err := newRequestID()
	if err != nil {
		return err
	}
	if err := r.sendReport(ctx, requestID, payload); err == nil {
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
	return r.sendReportRequest(ctx, requestID, payload, false)
}

func (r *Runner) sendReplayReport(ctx context.Context, requestID string, payload []byte) error {
	return r.sendReportRequest(ctx, requestID, payload, true)
}

func (r *Runner) sendReportRequest(ctx context.Context, requestID string, payload []byte, isReplay bool) error {
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
		last = r.doJSONWithRequestAndReplay(ctx, http.MethodPost, "/report", requestID, payload, isReplay)
		if last == nil {
			return nil
		}
	}
	return last
}

func (r *Runner) doJSONWithRequest(ctx context.Context, method, path, requestID string, payload []byte) error {
	return r.doJSONWithRequestAndReplay(ctx, method, path, requestID, payload, false)
}

func (r *Runner) doJSONWithRequestAndReplay(ctx context.Context, method, path, requestID string, payload []byte, isReplay bool) error {
	endpoint := strings.TrimRight(r.cfg.AgentEndpoint, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.cfg.AgentNodeToken)
	req.Header.Set("X-Probe-Timestamp", fmt.Sprintf("%d", time.Now().UTC().Unix()))
	req.Header.Set("X-Probe-Request-ID", requestID)
	req.Header.Set("Content-Type", "application/json")
	if isReplay {
		req.Header.Set("X-Outbox-Replay", "true")
	}
	response, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("control plane returned HTTP %d", response.StatusCode)
	}
	if response.Header.Get("X-Probe-Sync-IPQA") == "true" {
		if r.ipqa != nil {
			if inv, ok := r.ipqa.(interface{ Invalidate() }); ok {
				inv.Invalidate()
				_ = r.ipqa.Get()
			}
		}
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
	if response.Header.Get("X-Probe-Sync-IPQA") == "true" {
		if r.ipqa != nil {
			if inv, ok := r.ipqa.(interface{ Invalidate() }); ok {
				inv.Invalidate()
				_ = r.ipqa.Get()
			}
		}
	}
	if destination != nil {
		limited := io.LimitReader(response.Body, maxConfigResponseBytes+1)
		body, readErr := io.ReadAll(limited)
		if readErr != nil {
			return readErr
		}
		if len(body) > maxConfigResponseBytes {
			return fmt.Errorf("control plane response too large")
		}
		if err := json.Unmarshal(body, destination); err != nil {
			return err
		}
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

func (r *Runner) replayOutboxLimited(ctx context.Context) error {
	if !r.outboxReplayMu.TryLock() {
		return nil
	}
	defer r.outboxReplayMu.Unlock()

	replayCtx, cancel := context.WithTimeout(ctx, outboxReplayTimeout)
	defer cancel()

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
	processed := 0
	for _, entry := range entries {
		if processed >= maxOutboxReplayBatch || replayCtx.Err() != nil {
			break
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, statErr := entry.Info()
		if statErr != nil || info.Size() > maxOutboxFileSize || r.now().Sub(info.ModTime()) > maxOutboxAge {
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
			_ = os.Remove(path)
			continue
		}
		if !item.NextTry.IsZero() && item.NextTry.After(r.now().UTC()) {
			continue
		}
		processed++
		if err := r.sendReplayReport(replayCtx, item.RequestID, item.Payload); err == nil {
			_ = os.Remove(path)
			// Slight pacing delay between replays to prevent burst traffic storm
			select {
			case <-replayCtx.Done():
				break
			case <-time.After(20 * time.Millisecond):
			}
		} else {
			item.Attempts++
			attempts := item.Attempts
			if attempts <= 0 {
				attempts = 1
			}
			backoffFactor := time.Duration(1 << min(attempts-1, 6))
			item.NextTry = r.now().UTC().Add(reportRetryDelay * backoffFactor)
			if updated, marshalErr := json.Marshal(item); marshalErr == nil && len(updated) <= maxOutboxFileSize {
				_ = os.WriteFile(path, updated, 0600)
			}
			// Stop further attempts in this batch to avoid hammering recovering server
			break
		}
	}
	return nil
}

func (r *Runner) replayOutbox(ctx context.Context) error {
	return r.replayOutboxLimited(ctx)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
