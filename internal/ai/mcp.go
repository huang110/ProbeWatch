package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/protocol"
	"github.com/probewatch/probewatch/internal/version"
)

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      any           `json:"id"`
	Result  any           `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type MCPToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type MCPCallResult struct {
	Content []MCPContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

type MCPContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

var exposedMCPTools = []MCPToolDefinition{
	{
		Name:        "probewatch_get_overview",
		Description: "Get real-time operational overview of all monitored servers, health score, and active alerts in ProbeWatch.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	},
	{
		Name:        "probewatch_get_node_metrics",
		Description: "Get real-time system metrics, hardware specs, OS, CPU, RAM, disk, and network throughput for a specific node.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"node_id": map[string]any{
					"type":        "string",
					"description": "The target node ID or UUID",
				},
				"name": map[string]any{
					"type":        "string",
					"description": "Optional node name",
				},
			},
		},
	},
	{
		Name:        "probewatch_get_network_diagnosis",
		Description: "Retrieve network ping results and MTR hop-by-hop traceroute telemetry for network diagnostics.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"node_id": map[string]any{
					"type":        "string",
					"description": "Optional node ID to filter checks",
				},
			},
		},
	},
	{
		Name:        "probewatch_get_fleet_cost_audit",
		Description: "Audit all monitored VPS/servers for idle capacity (low CPU/traffic), monthly bandwidth quota usage, and upcoming billing expiration dates.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	},
	{
		Name:        "probewatch_run_ai_diagnosis",
		Description: "Run comprehensive AI root-cause analysis on network anomalies, server performance bottlenecks, and VPS cost optimization.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"focus": map[string]any{
					"type":        "string",
					"description": "Optional focus area: 'all', 'network', 'cost', 'system'",
				},
			},
		},
	},
}

func (s *AIService) HandleMCPJSONRPC(ctx context.Context, body []byte) (*JSONRPCResponse, error) {
	var req JSONRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      nil,
			Error: &JSONRPCError{
				Code:    -32700,
				Message: "Parse error",
			},
		}, nil
	}

	res := &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
	}

	switch req.Method {
	case "initialize":
		res.Result = map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "probewatch-mcp",
				"version": version.ServerVersion,
			},
		}

	case "notifications/initialized":
		// Notifications don't require responses according to JSON-RPC 2.0, but when sent in request/response mode we return ok
		res.Result = map[string]any{"status": "ok"}

	case "ping":
		res.Result = map[string]any{}

	case "tools/list":
		res.Result = map[string]any{
			"tools": exposedMCPTools,
		}

	case "tools/call":
		var callParams struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &callParams); err != nil {
			res.Error = &JSONRPCError{
				Code:    -32602,
				Message: "Invalid params: failed to parse tool call arguments",
			}
			return res, nil
		}

		callRes, err := s.executeMCPTool(ctx, callParams.Name, callParams.Arguments)
		if err != nil {
			res.Result = MCPCallResult{
				Content: []MCPContent{
					{
						Type: "text",
						Text: fmt.Sprintf("Error executing tool %s: %v", callParams.Name, err),
					},
				},
				IsError: true,
			}
		} else {
			res.Result = callRes
		}

	default:
		res.Error = &JSONRPCError{
			Code:    -32601,
			Message: fmt.Sprintf("Method not found: %s", req.Method),
		}
	}

	return res, nil
}

func (s *AIService) executeMCPTool(ctx context.Context, name string, rawArgs json.RawMessage) (*MCPCallResult, error) {
	switch name {
	case "probewatch_get_overview":
		diag, err := s.RunDiagnosis(ctx)
		if err != nil {
			return nil, err
		}
		summaryJSON, _ := json.MarshalIndent(map[string]any{
			"health_score":  diag.HealthScore,
			"health_status": diag.HealthStatus,
			"summary":       diag.Summary,
			"cost_summary": map[string]any{
				"monthly_cost":       diag.CostAudit.TotalMonthlyCost,
				"potential_savings": diag.CostAudit.PotentialMonthlySavings,
				"currency":          diag.CostAudit.Currency,
			},
		}, "", "  ")

		return &MCPCallResult{
			Content: []MCPContent{
				{
					Type: "text",
					Text: string(summaryJSON),
				},
			},
		}, nil

	case "probewatch_get_node_metrics":
		var args struct {
			NodeID string `json:"node_id"`
			Name   string `json:"name"`
		}
		_ = json.Unmarshal(rawArgs, &args)

		nodes, err := s.store.ListNodes(ctx)
		if err != nil {
			return nil, err
		}

		var targetNodeID string
		var targetNodeName string
		for _, n := range nodes {
			if args.NodeID != "" && (n.ID == args.NodeID || n.UUID == args.NodeID) {
				targetNodeID = n.ID
				targetNodeName = n.Name
				break
			}
			if args.Name != "" && strings.EqualFold(n.Name, args.Name) {
				targetNodeID = n.ID
				targetNodeName = n.Name
				break
			}
		}

		if targetNodeID == "" && len(nodes) > 0 {
			targetNodeID = nodes[0].ID
			targetNodeName = nodes[0].Name
		}

		reportedAt, payload, err := s.store.GetResourceLatest(ctx, targetNodeID)
		if err != nil {
			return nil, fmt.Errorf("no resource metrics found for node %s", targetNodeName)
		}

		var snap protocol.ResourceSnapshot
		_ = json.Unmarshal(payload, &snap)

		billing, _ := s.store.GetNodeCycleTraffic(ctx, targetNodeID, time.Now().UTC())

		data := map[string]any{
			"node_id":     targetNodeID,
			"node_name":   targetNodeName,
			"reported_at": reportedAt.Format(time.RFC3339),
			"system": map[string]any{
				"os":            snap.OS,
				"kernel":        snap.Kernel,
				"arch":          snap.Arch,
				"agent_version": snap.AgentVersion,
				"cpu_percent":   snap.CPUPercent,
				"load1":         snap.Load1,
				"load5":         snap.Load5,
				"load15":        snap.Load15,
			},
			"memory": map[string]any{
				"total_mb": snap.MemoryTotalBytes / (1024 * 1024),
				"used_mb":  snap.MemoryUsedBytes / (1024 * 1024),
				"used_pct": fmt.Sprintf("%.1f%%", float64(snap.MemoryUsedBytes)/float64(snap.MemoryTotalBytes)*100),
			},
			"disk": map[string]any{
				"total_gb": snap.FilesystemTotalBytes / (1024 * 1024 * 1024),
				"used_gb":  snap.FilesystemUsedBytes / (1024 * 1024 * 1024),
				"used_pct": fmt.Sprintf("%.1f%%", float64(snap.FilesystemUsedBytes)/float64(snap.FilesystemTotalBytes)*100),
			},
			"billing": map[string]any{
				"cycle_used_gb": float64(billing.CycleUsedBytes) / (1024 * 1024 * 1024),
				"quota_gb":      float64(billing.TotalQuotaBytes) / (1024 * 1024 * 1024),
				"used_pct":      fmt.Sprintf("%.1f%%", billing.UsedPercent),
				"monthly_price": billing.Price,
				"currency":      billing.Currency,
				"due_date":      billing.DueDate,
			},
		}

		dataJSON, _ := json.MarshalIndent(data, "", "  ")
		return &MCPCallResult{
			Content: []MCPContent{
				{
					Type: "text",
					Text: string(dataJSON),
				},
			},
		}, nil

	case "probewatch_get_network_diagnosis":
		var args struct {
			NodeID string `json:"node_id"`
		}
		_ = json.Unmarshal(rawArgs, &args)

		nodes, err := s.store.ListNodes(ctx)
		if err != nil {
			return nil, err
		}

		type NetworkReportItem struct {
			NodeName   string   `json:"node_name"`
			Host       string   `json:"host"`
			HopsCount  int      `json:"hops_count"`
			Reached    bool     `json:"reached"`
			RootCause  string   `json:"root_cause"`
			Anomaly    bool     `json:"anomaly"`
			LatencyAvg float64  `json:"latency_avg_ms"`
			Hops       []string `json:"hops,omitempty"`
		}

		var items []NetworkReportItem
		for _, node := range nodes {
			if args.NodeID != "" && node.ID != args.NodeID && node.UUID != args.NodeID {
				continue
			}
			mtrList, _ := s.store.ListMTRLatest(ctx, node.ID)
			for _, m := range mtrList {
				var res protocol.MTRResult
				if err := json.Unmarshal(m.Payload, &res); err == nil {
					cause, anomaly, _ := AnalyzeMTRRootCause(res.Host, res.Hops, res.Reached)
					var hopStrs []string
					var totalLat int64
					validHops := 0
					for _, h := range res.Hops {
						if h.TimedOut {
							hopStrs = append(hopStrs, fmt.Sprintf("#%d * * *", h.TTL))
						} else {
							hopStrs = append(hopStrs, fmt.Sprintf("#%d %s (%dms)", h.TTL, h.IP, h.LatencyMS))
							totalLat += h.LatencyMS
							validHops++
						}
					}
					var avgLat float64
					if validHops > 0 {
						avgLat = float64(totalLat) / float64(validHops)
					}
					items = append(items, NetworkReportItem{
						NodeName:   node.Name,
						Host:       res.Host,
						HopsCount:  len(res.Hops),
						Reached:    res.Reached,
						RootCause:  cause,
						Anomaly:    anomaly,
						LatencyAvg: avgLat,
						Hops:       hopStrs,
					})
				}
			}
		}

		resJSON, _ := json.MarshalIndent(items, "", "  ")
		return &MCPCallResult{
			Content: []MCPContent{
				{
					Type: "text",
					Text: string(resJSON),
				},
			},
		}, nil

	case "probewatch_get_fleet_cost_audit":
		diag, err := s.RunDiagnosis(ctx)
		if err != nil {
			return nil, err
		}
		costJSON, _ := json.MarshalIndent(diag.CostAudit, "", "  ")
		return &MCPCallResult{
			Content: []MCPContent{
				{
					Type: "text",
					Text: string(costJSON),
				},
			},
		}, nil

	case "probewatch_run_ai_diagnosis":
		diag, err := s.RunDiagnosis(ctx)
		if err != nil {
			return nil, err
		}
		return &MCPCallResult{
			Content: []MCPContent{
				{
					Type: "text",
					Text: diag.MarkdownReport,
				},
			},
		}, nil

	default:
		return nil, fmt.Errorf("unknown tool name: %s", name)
	}
}
