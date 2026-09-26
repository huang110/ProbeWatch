package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/protocol"
)

// agentConfigMaxAgeSeconds is the default freshness window served to agents.
// An agent that cannot refresh its configuration within this window stops
// probing (fail-closed) while continuing to report resources.
const agentConfigMaxAgeSeconds = 1800

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
				writeJSONError(w, http.StatusServiceUnavailable, "configuration unavailable")
				return
			}
			if !matches {
				continue
			}
			tasks = append(tasks, task)
		}
	}
	speedTasks, err := s.service.Store().ListEnabledSpeedtestTasks(r.Context())
	if err == nil {
		for _, st := range speedTasks {
			if !nodeMatchesSpeedtestTask(node, st) {
				continue
			}
			host := st.Name
			if host == "" {
				host = "speedtest"
			}
			tasks = append(tasks, protocol.CheckTask{
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
	}
	writeJSON(w, http.StatusOK, protocol.AgentConfigResponse{
		Tasks:               tasks,
		ConfigVersion:       time.Now().UTC().Unix(),
		ConfigMaxAgeSeconds: agentConfigMaxAgeSeconds,
	})
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
