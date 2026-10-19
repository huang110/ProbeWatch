package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/protocol"
	"github.com/probewatch/probewatch/internal/security"
)

// agentConfigMaxAgeSeconds is the default freshness window served to agents.
// An agent that cannot refresh its configuration within this window stops
// probing (fail-closed) while continuing to report resources.
const agentConfigMaxAgeSeconds = 1800

// maxAgentConfigTasks mirrors the task limit enforced by
// protocol.AgentConfigResponse.Validate. agentConfig truncates instead of
// failing the whole feed so one oversized legacy dataset cannot push every
// agent into the fail-closed window.
const maxAgentConfigTasks = 1000

var (
	configVersionMu   sync.Mutex
	lastConfigVersion int64
)

func getNextConfigVersion() int64 {
	configVersionMu.Lock()
	defer configVersionMu.Unlock()
	now := time.Now().UTC().Unix()
	if now <= lastConfigVersion {
		now = lastConfigVersion + 1
	}
	lastConfigVersion = now
	return now
}

type agentTargetPayload struct {
	Host            string                `json:"host,omitempty"`
	Port            int                   `json:"port,omitempty"`
	Path            string                `json:"path,omitempty"`
	ExpectedStatus  int                   `json:"expected_status,omitempty"`
	DNSType         string                `json:"dns_type,omitempty"`
	TimeoutMS       int                   `json:"timeout_ms,omitempty"`
	MaxHops         int                   `json:"max_hops,omitempty"`
	IntervalSeconds int                   `json:"interval_seconds,omitempty"`
	RegionRules     []protocol.RegionRule `json:"region_rules,omitempty"`
	Keyword         string                `json:"keyword,omitempty"`
	Nameserver      string                `json:"nameserver,omitempty"`
	CheckTLS        bool                  `json:"check_tls,omitempty"`
	NodeTags        []string              `json:"node_tags,omitempty"`
	NodeIDs         []string              `json:"node_ids,omitempty"`
}

// agentConfig serves the agent's read-only check configuration. It exposes only
// enabled targets, carries no command channel, and accepts no request body.
func (s *Server) agentConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	node, _, _, ok := s.authenticateAgentRequest(w, r)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	tasks := make([]protocol.CheckTask, 0)
	taskIDs := make(map[string]struct{})
	appendTask := func(task protocol.CheckTask) {
		if task.ID == "" {
			slog.WarnContext(r.Context(), "skipping agent target without task ID", "target_kind", "network")
			return
		}
		if _, exists := taskIDs[task.ID]; exists {
			slog.WarnContext(r.Context(), "skipping duplicate agent task ID", "task_id", task.ID, "target_kind", "network")
			return
		}
		taskIDs[task.ID] = struct{}{}
		tasks = append(tasks, task)
	}
	for _, kind := range targetKinds() {
		targets, err := s.service.Store().ListTargets(r.Context(), kind)
		if err != nil {
			writeJSONError(w, http.StatusServiceUnavailable, "configuration unavailable")
			return
		}
		for _, target := range targets {
			if !target.Enabled {
				continue
			}
			task, matches, err := targetToCheckTaskForNode(target, node)
			if err != nil {
				slog.WarnContext(r.Context(), "skipping invalid legacy target in agent config", "target_id", target.ID, "target_kind", kind, "error", err)
				continue
			}
			if !matches {
				continue
			}
			appendTask(task)
		}
	}
	speedTasks, err := s.service.Store().ListEnabledSpeedtestTasks(r.Context())
	if err != nil {
		// Do not return a successful empty configuration when the database is
		// unavailable.  Agents would cache that response and silently stop all
		// configured checks until the next refresh window.
		writeJSONError(w, http.StatusServiceUnavailable, "configuration unavailable")
		return
	}
	for _, st := range speedTasks {
		if !nodeMatchesSpeedtestTask(node, st) {
			continue
		}
		host := st.Name
		if host == "" {
			host = "speedtest"
		}
		appendTask(protocol.CheckTask{
			ID:              st.ID,
			Kind:            "speedtest",
			Host:            host,
			Port:            443,
			ServerURL:       st.ServerURL,
			DownloadBytes:   st.DownloadBytes,
			UploadBytes:     st.UploadBytes,
			IntervalSeconds: st.IntervalSeconds,
			MaxHops:         20,
			TimeoutMS:       25000,
			Enabled:         true,
		})
	}
	syntheticTargets, err := s.service.Store().ListEnabledSyntheticTargets(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "configuration unavailable")
		return
	}
	for _, st := range syntheticTargets {
		if !nodeMatchesSyntheticTarget(node, st) {
			continue
		}
		task, err := syntheticTargetToCheckTask(st, s.cfg.Environment == "development")
		if err != nil {
			slog.WarnContext(r.Context(), "skipping invalid synthetic target in agent config", "target_id", st.ID, "error", err)
			continue
		}
		appendTask(task)
	}

	syncIPQA := checkAndClearNodeIPQASync(node.ID, node.UUID)

	// Validate each task individually so a single malformed legacy row
	// (for example a speedtest task with a stale server URL) cannot fail
	// the whole /config feed. Agents fail closed after their max-age
	// window, so a fleet-wide 503 would silently stop every check.
	validTasks := make([]protocol.CheckTask, 0, len(tasks))
	for _, task := range tasks {
		if err := task.Validate(); err != nil {
			slog.WarnContext(r.Context(), "skipping invalid task in agent config", "task_id", task.ID, "error", err)
			continue
		}
		validTasks = append(validTasks, task)
	}
	if len(validTasks) > maxAgentConfigTasks {
		slog.WarnContext(r.Context(), "agent config exceeds task limit, truncating", "task_count", len(validTasks), "limit", maxAgentConfigTasks)
		validTasks = validTasks[:maxAgentConfigTasks]
	}

	response := protocol.AgentConfigResponse{
		Tasks:               validTasks,
		ConfigVersion:       getNextConfigVersion(),
		ConfigMaxAgeSeconds: agentConfigMaxAgeSeconds,
		SyncIPQA:            syncIPQA,
	}
	// Defense in depth: the per-task loop above already filtered invalid
	// tasks and appendTask deduplicates IDs, so a failure here means the
	// merged set itself is unusable and must not be served to agents.
	if err := response.Validate(); err != nil {
		slog.ErrorContext(r.Context(), "agent config response failed validation", "error", err)
		writeJSONError(w, http.StatusServiceUnavailable, "configuration unavailable")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func targetToCheckTaskForNode(target db.TargetRecord, node db.Node) (protocol.CheckTask, bool, error) {
	var config agentTargetPayload
	if err := json.Unmarshal(target.Payload, &config); err != nil {
		return protocol.CheckTask{}, false, err
	}
	if !nodeMatchesTarget(node, config) {
		return protocol.CheckTask{}, false, nil
	}
	task, err := payloadToCheckTask(target, config)
	return task, true, err
}

func nodeMatchesTarget(node db.Node, config agentTargetPayload) bool {
	hasTags := len(config.NodeTags) > 0
	hasIDs := len(config.NodeIDs) > 0
	if !hasTags && !hasIDs {
		return true
	}
	if hasIDs {
		for _, id := range config.NodeIDs {
			id = strings.TrimSpace(id)
			if id != "" && (strings.EqualFold(id, node.ID) || strings.EqualFold(id, node.UUID)) {
				return true
			}
		}
	}
	if hasTags {
		nodeTags := parseNodeTags(node.Tags)
		for _, reqTag := range config.NodeTags {
			reqTag = strings.TrimSpace(reqTag)
			if reqTag == "" {
				continue
			}
			for _, nt := range nodeTags {
				if strings.EqualFold(nt, reqTag) {
					return true
				}
			}
		}
	}
	return false
}

func nodeMatchesSpeedtestTask(node db.Node, st db.SpeedtestTaskRecord) bool {
	tags := parseNodeTags(st.NodeTags)
	ids := parseNodeTags(st.NodeIDs)
	if len(tags) == 0 && len(ids) == 0 {
		return true
	}
	if len(ids) > 0 {
		for _, id := range ids {
			if strings.EqualFold(id, node.ID) || strings.EqualFold(id, node.UUID) {
				return true
			}
		}
	}
	if len(tags) > 0 {
		nodeTags := parseNodeTags(node.Tags)
		for _, t := range tags {
			for _, nt := range nodeTags {
				if strings.EqualFold(nt, t) {
					return true
				}
			}
		}
	}
	return false
}

func parseNodeTags(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' '
	})
	tags := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			tags = append(tags, p)
		}
	}
	return tags
}

func targetToCheckTask(target db.TargetRecord) (protocol.CheckTask, error) {
	var config agentTargetPayload
	if err := json.Unmarshal(target.Payload, &config); err != nil {
		return protocol.CheckTask{}, err
	}
	return payloadToCheckTask(target, config)
}

func payloadToCheckTask(target db.TargetRecord, config agentTargetPayload) (protocol.CheckTask, error) {
	host := target.Host
	if host == "" {
		host = config.Host
	}
	task := protocol.CheckTask{
		ID:              target.ID,
		Kind:            string(target.Kind),
		Host:            host,
		Port:            config.Port,
		Path:            config.Path,
		ExpectedStatus:  config.ExpectedStatus,
		DNSType:         config.DNSType,
		TimeoutMS:       config.TimeoutMS,
		MaxHops:         config.MaxHops,
		IntervalSeconds: config.IntervalSeconds,
		Enabled:         target.Enabled,
		RegionRules:     config.RegionRules,
		Keyword:         config.Keyword,
		Nameserver:      config.Nameserver,
		CheckTLS:        config.CheckTLS,
	}
	if task.MaxHops == 0 {
		task.MaxHops = 20
	}
	if task.TimeoutMS == 0 {
		task.TimeoutMS = 3000
	}
	if task.IntervalSeconds == 0 {
		task.IntervalSeconds = 60
	}
	if task.Port == 0 {
		switch task.Kind {
		case "https":
			task.Port = 443
		case "dns":
			task.Port = 53
		default:
			task.Port = 80
		}
	}
	if err := task.Validate(); err != nil {
		return protocol.CheckTask{}, err
	}
	return task, nil
}

func nodeMatchesSyntheticTarget(node db.Node, st db.SyntheticTargetRecord) bool {
	tags := parseNodeTags(st.NodeTags)
	ids := parseNodeTags(st.NodeIDs)
	if len(tags) == 0 && len(ids) == 0 {
		return true
	}
	if len(ids) > 0 {
		for _, id := range ids {
			if strings.EqualFold(id, node.ID) || strings.EqualFold(id, node.UUID) {
				return true
			}
		}
	}
	if len(tags) > 0 {
		nodeTags := parseNodeTags(node.Tags)
		for _, t := range tags {
			for _, nt := range nodeTags {
				if strings.EqualFold(nt, t) {
					return true
				}
			}
		}
	}
	return false
}

func syntheticTargetToCheckTask(st db.SyntheticTargetRecord, allowPrivate ...bool) (protocol.CheckTask, error) {
	protocolKind := strings.ToLower(strings.TrimSpace(st.Protocol))
	switch protocolKind {
	case "http", "https", "grpc", "websocket", "doh":
	default:
		return protocol.CheckTask{}, fmt.Errorf("synthetic target %q has invalid protocol", st.ID)
	}
	targetURL := strings.TrimSpace(st.TargetURL)
	u, err := url.Parse(targetURL)
	if err != nil || !u.IsAbs() || u.Hostname() == "" {
		return protocol.CheckTask{}, fmt.Errorf("synthetic target %q has invalid target_url", st.ID)
	}
	if u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return protocol.CheckTask{}, fmt.Errorf("synthetic target %q has unsafe target_url", st.ID)
	}
	allowedPrivate := len(allowPrivate) > 0 && allowPrivate[0]
	if _, err := security.ValidateURLWithOptions(targetURL, syntheticURLSchemes(protocolKind), true, allowedPrivate); err != nil {
		return protocol.CheckTask{}, fmt.Errorf("synthetic target %q has invalid target_url: %w", st.ID, err)
	}

	port := 0
	if rawPort := u.Port(); rawPort != "" {
		port, err = strconv.Atoi(rawPort)
		if err != nil || port < 1 || port > 65535 {
			return protocol.CheckTask{}, fmt.Errorf("synthetic target %q has invalid port", st.ID)
		}
	} else {
		switch strings.ToLower(u.Scheme) {
		case "https", "wss", "grpcs":
			port = 443
		default:
			port = 80
		}
	}

	var headers map[string]string
	if st.Headers != "" {
		if err := json.Unmarshal([]byte(st.Headers), &headers); err != nil {
			return protocol.CheckTask{}, fmt.Errorf("synthetic target %q has invalid headers", st.ID)
		}
	}
	var assertions []protocol.SyntheticAssertionRule
	if st.Assertions != "" {
		if err := json.Unmarshal([]byte(st.Assertions), &assertions); err != nil {
			return protocol.CheckTask{}, fmt.Errorf("synthetic target %q has invalid assertions", st.ID)
		}
	}
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	method := strings.ToUpper(strings.TrimSpace(st.Method))
	if method == "" {
		method = http.MethodGet
	}

	// HTTP/HTTPS synthetic targets must remain on the synthetic execution
	// path. Reusing the ordinary network kinds would make the agent emit a
	// NetworkResult, which the control plane then tries to bind to the legacy
	// HTTP/HTTPS target tables instead of the synthetic target table.
	taskKind := protocolKind
	if protocolKind == "http" || protocolKind == "https" {
		taskKind = "synthetic"
	}

	task := protocol.CheckTask{
		ID:              st.ID,
		Kind:            taskKind,
		Host:            u.Hostname(),
		Port:            port,
		Path:            path,
		MaxHops:         1, // synthetic protocols do not trace routes
		Method:          method,
		ServerURL:       targetURL,
		Headers:         headers,
		BodyPayload:     st.BodyPayload,
		Assertions:      assertions,
		GRPCService:     st.GRPCService,
		TimeoutMS:       st.TimeoutMS,
		IntervalSeconds: st.IntervalSeconds,
		Enabled:         st.Enabled,
	}
	if task.TimeoutMS <= 0 {
		task.TimeoutMS = 5000
	}
	if task.IntervalSeconds <= 0 {
		task.IntervalSeconds = 60
	}
	if err := task.Validate(); err != nil {
		return protocol.CheckTask{}, fmt.Errorf("synthetic target %q is invalid: %w", st.ID, err)
	}
	return task, nil
}
