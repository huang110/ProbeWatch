package api

import (
	"encoding/json"
	"net/http"
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
}

// agentConfig serves the agent's read-only check configuration. It exposes only
// enabled targets, carries no command channel, and accepts no request body.
func (s *Server) agentConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, _, _, ok := s.authenticateAgentRequest(w, r); !ok {
		return
	}
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
			task, err := targetToCheckTask(target)
			if err != nil {
				writeJSONError(w, http.StatusServiceUnavailable, "configuration unavailable")
				return
			}
			tasks = append(tasks, task)
		}
	}
	writeJSON(w, http.StatusOK, protocol.AgentConfigResponse{
		Tasks:               tasks,
		ConfigVersion:       time.Now().UTC().Unix(),
		ConfigMaxAgeSeconds: agentConfigMaxAgeSeconds,
	})
}

func targetToCheckTask(target db.TargetRecord) (protocol.CheckTask, error) {
	var config agentTargetPayload
	if err := json.Unmarshal(target.Payload, &config); err != nil {
		return protocol.CheckTask{}, err
	}
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
