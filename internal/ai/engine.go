package ai

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/config"
	"github.com/probewatch/probewatch/internal/db"
	"github.com/probewatch/probewatch/internal/protocol"
)

type AIService struct {
	store  *db.Store
	cfg    *config.Config
	client *http.Client
}

func NewAIService(store *db.Store, cfg *config.Config) *AIService {
	return &AIService{
		store: store,
		cfg:   cfg,
		client: &http.Client{
			Timeout: 45 * time.Second,
		},
	}
}

type AISettings struct {
	Enabled     bool   `json:"enabled"`
	Provider    string `json:"provider"` // "local", "deepseek", "openai", "claude", "ollama", "custom"
	APIKey      string `json:"api_key,omitempty"`
	APIEndpoint string `json:"api_endpoint"`
	Model       string `json:"model"`
	HasAPIKey   bool   `json:"has_api_key"`
}

type AIChatResponse struct {
	Reply          string   `json:"reply"`
	Provider       string   `json:"provider"`
	Model          string   `json:"model"`
	GeneratedAt    int64    `json:"generated_at"`
	RelevantNodes  []string `json:"relevant_nodes,omitempty"`
	HealthScore    int      `json:"health_score"`
	FallbackToRule bool     `json:"fallback_to_rule"`
}

func (s *AIService) GetAISettings(ctx context.Context) (AISettings, error) {
	enabledStr, _ := s.store.GetSetting(ctx, "ai_enabled", "false")
	provider, _ := s.store.GetSetting(ctx, "ai_provider", "local")
	endpoint, _ := s.store.GetSetting(ctx, "ai_api_endpoint", "https://api.deepseek.com/v1")
	model, _ := s.store.GetSetting(ctx, "ai_model", "deepseek-chat")
	apiKey, _ := s.store.GetSetting(ctx, "ai_api_key", "")

	return AISettings{
		Enabled:     enabledStr == "true",
		Provider:    provider,
		APIEndpoint: endpoint,
		Model:       model,
		HasAPIKey:   apiKey != "",
	}, nil
}

func (s *AIService) SaveAISettings(ctx context.Context, settings AISettings) error {
	enabledStr := "false"
	if settings.Enabled {
		enabledStr = "true"
	}
	if err := s.store.SetSetting(ctx, "ai_enabled", enabledStr); err != nil {
		return err
	}
	if err := s.store.SetSetting(ctx, "ai_provider", settings.Provider); err != nil {
		return err
	}
	if err := s.store.SetSetting(ctx, "ai_api_endpoint", settings.APIEndpoint); err != nil {
		return err
	}
	if err := s.store.SetSetting(ctx, "ai_model", settings.Model); err != nil {
		return err
	}
	// Only update API key if non-empty, so frontend doesn't wipe existing key when sending masked value
	if strings.TrimSpace(settings.APIKey) != "" && !strings.Contains(settings.APIKey, "•••") {
		if err := s.store.SetSetting(ctx, "ai_api_key", strings.TrimSpace(settings.APIKey)); err != nil {
			return err
		}
	}
	return nil
}

func (s *AIService) GetOrGenerateMCPToken(ctx context.Context) (string, error) {
	token, err := s.store.GetSetting(ctx, "mcp_auth_token", "")
	if err == nil && token != "" {
		return token, nil
	}
	return s.RegenerateMCPToken(ctx)
}

func (s *AIService) RegenerateMCPToken(ctx context.Context) (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate mcp token: %w", err)
	}
	token := "pw_mcp_" + hex.EncodeToString(buf)
	if err := s.store.SetSetting(ctx, "mcp_auth_token", token); err != nil {
		return "", err
	}
	return token, nil
}

func (s *AIService) ValidateMCPToken(ctx context.Context, token string) bool {
	if token == "" {
		return false
	}
	stored, err := s.store.GetSetting(ctx, "mcp_auth_token", "")
	if err != nil || stored == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(stored)) == 1
}

func (s *AIService) RunDiagnosis(ctx context.Context) (*DiagnosisReport, error) {
	nodes, err := s.store.ListNodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}

	alerts, _ := s.store.ListAlerts(ctx, db.AlertQuery{
		Statuses: []string{"open"},
		Limit:    100,
	})

	now := time.Now().UTC()
	rep := &DiagnosisReport{
		GeneratedAt: now.Unix(),
		Summary: DiagnosisSummary{
			TotalNodes:   len(nodes),
			ActiveAlerts: len(alerts),
		},
		CostAudit: CostAuditSummary{
			Currency: "CNY",
		},
	}

	onlineCount := 0
	offlineCount := 0
	findings := make([]Finding, 0)
	var totalMonthlyCost float64
	var potentialSavings float64

	for _, node := range nodes {
		// 1. Fetch Node Latest Resource
		reportedAt, payload, err := s.store.GetResourceLatest(ctx, node.ID)
		isOnline := false
		var snap protocol.ResourceSnapshot
		if err == nil && len(payload) > 0 {
			if err := json.Unmarshal(payload, &snap); err == nil {
				// Node is considered online if reported within 3 minutes
				if now.Sub(reportedAt) < 3*time.Minute {
					isOnline = true
				}
			}
		}

		if isOnline {
			onlineCount++
		} else {
			offlineCount++
			findings = append(findings, Finding{
				ID:             fmt.Sprintf("offline-%s", node.ID),
				Category:       CategorySystem,
				Severity:       SeverityCritical,
				Title:          fmt.Sprintf("服务器节点失联 (%s)", node.Name),
				NodeID:         node.ID,
				NodeName:       node.Name,
				Summary:        fmt.Sprintf("该节点超过 3 分钟未上报监控心跳（最后上报时间: %s）。", reportedAt.Format("15:04:05")),
				RootCause:      "Agent 进程终止、系统网络中断或服务器关机/宕机。",
				Recommendation: "请通过 SSH 或云服务商控制台检查 probewatch-agent 服务状态及网络路由。",
			})
		}

		// 2. Hardware Resource Bottlenecks
		if isOnline {
			if snap.FilesystemTotalBytes > 0 {
				diskPercent := float64(snap.FilesystemUsedBytes) / float64(snap.FilesystemTotalBytes) * 100
				if diskPercent >= 90.0 {
					rep.Summary.ResourceWarnings++
					findings = append(findings, Finding{
						ID:             fmt.Sprintf("disk-critical-%s", node.ID),
						Category:       CategorySystem,
						Severity:       SeverityCritical,
						Title:          fmt.Sprintf("磁盘空间极度匮乏 (%.1f%%) - %s", diskPercent, node.Name),
						NodeID:         node.ID,
						NodeName:       node.Name,
						Summary:        fmt.Sprintf("根文件系统使用率已达 %.1f%%，可用容量仅剩 %.2f GB。", diskPercent, float64(snap.FilesystemTotalBytes-snap.FilesystemUsedBytes)/(1024*1024*1024)),
						RootCause:      "系统日志、临时缓存或业务数据库膨胀。",
						Recommendation: "执行 `journalctl --vacuum-time=3d` 清理日志，并排查大文件占用避免服务崩溃。",
					})
				} else if diskPercent >= 80.0 {
					rep.Summary.ResourceWarnings++
					findings = append(findings, Finding{
						ID:             fmt.Sprintf("disk-warning-%s", node.ID),
						Category:       CategorySystem,
						Severity:       SeverityWarning,
						Title:          fmt.Sprintf("磁盘容量预警 (%.1f%%) - %s", diskPercent, node.Name),
						NodeID:         node.ID,
						NodeName:       node.Name,
						Summary:        fmt.Sprintf("磁盘空间已使用 %.1f%%，建议提前扩容或清理无用归档。", diskPercent),
						RootCause:      "持久化数据稳步增长。",
						Recommendation: "排查并清理 Docker 废弃镜像、临时包文件或设置日志自动轮转。",
					})
				}
			}

			if snap.MemoryTotalBytes > 0 {
				memPercent := float64(snap.MemoryUsedBytes) / float64(snap.MemoryTotalBytes) * 100
				if memPercent >= 92.0 {
					rep.Summary.ResourceWarnings++
					findings = append(findings, Finding{
						ID:             fmt.Sprintf("mem-warning-%s", node.ID),
						Category:       CategorySystem,
						Severity:       SeverityWarning,
						Title:          fmt.Sprintf("内存资源高度吃紧 (%.1f%%) - %s", memPercent, node.Name),
						NodeID:         node.ID,
						NodeName:       node.Name,
						Summary:        fmt.Sprintf("内存使用率持续高于 92%%（当前使用: %.2f GB / %.2f GB）。", float64(snap.MemoryUsedBytes)/(1024*1024*1024), float64(snap.MemoryTotalBytes)/(1024*1024*1024)),
						RootCause:      "内存密集型进程驻留或突发并发，可能引发系统 OOM Killer 强杀关键服务。",
						Recommendation: "核查 `top/htop` 高内存进程，配置并启用 Swap 交换分区或升级内存规格。",
					})
				}
			}
		}

		// 3. Billing & Cost Audit
		billingInfo, err := s.store.GetNodeCycleTraffic(ctx, node.ID, now)
		if err == nil {
			if billingInfo.Currency != "" {
				rep.CostAudit.Currency = billingInfo.Currency
			}

			// Monthly cost normalization
			monthlyCost := billingInfo.Price
			switch billingInfo.Cycle {
			case "quarter":
				monthlyCost = billingInfo.Price / 3.0
			case "half_year":
				monthlyCost = billingInfo.Price / 6.0
			case "year":
				monthlyCost = billingInfo.Price / 12.0
			}
			totalMonthlyCost += monthlyCost

			// Traffic Quota Warning
			if billingInfo.TotalQuotaBytes > 0 {
				if billingInfo.UsedPercent >= 90.0 {
					findings = append(findings, Finding{
						ID:             fmt.Sprintf("quota-critical-%s", node.ID),
						Category:       CategoryCost,
						Severity:       SeverityCritical,
						Title:          fmt.Sprintf("流量配额即将耗尽 (%.1f%%) - %s", billingInfo.UsedPercent, node.Name),
						NodeID:         node.ID,
						NodeName:       node.Name,
						Summary:        fmt.Sprintf("本周期流量已消耗 %.1f%%，距离下个重置日还有 %d 天。", billingInfo.UsedPercent, billingInfo.DaysUntilReset),
						RootCause:      "节点出入站带宽流量超出预估计划。",
						Recommendation: "注意限制超量带宽限速或购买叠加流量包，防止被服务商暂停或产生昂贵超额计费。",
					})
					rep.CostAudit.QuotaWarnings = append(rep.CostAudit.QuotaWarnings, QuotaWarning{
						NodeID:         node.ID,
						NodeName:       node.Name,
						UsedBytes:      billingInfo.CycleUsedBytes,
						QuotaBytes:     billingInfo.TotalQuotaBytes,
						UsedPercent:    billingInfo.UsedPercent,
						DaysUntilReset: billingInfo.DaysUntilReset,
					})
				}
			}

			// Impending Expiry Check
			if billingInfo.DueDate != "" {
				if dueTime, err := time.Parse("2006-01-02", billingInfo.DueDate); err == nil {
					daysLeft := int(math.Ceil(dueTime.Sub(now).Hours() / 24.0))
					if daysLeft >= 0 && daysLeft <= 7 {
						rep.Summary.ExpiringNodes++
						sev := SeverityWarning
						if daysLeft <= 3 {
							sev = SeverityCritical
						}
						findings = append(findings, Finding{
							ID:             fmt.Sprintf("expiry-%s", node.ID),
							Category:       CategoryCost,
							Severity:       sev,
							Title:          fmt.Sprintf("服务器即将到期 (剩余 %d 天) - %s", daysLeft, node.Name),
							NodeID:         node.ID,
							NodeName:       node.Name,
							Summary:        fmt.Sprintf("该节点将于 %s 到期（续费金额: %.2f %s）。", billingInfo.DueDate, billingInfo.Price, billingInfo.Currency),
							RootCause:      "服务器账期即将截止。",
							Recommendation: "如需继续使用请及时安排续费，若已放弃请提前备份数据并解绑 DNS 解析。",
						})
						rep.CostAudit.ExpiringNodes = append(rep.CostAudit.ExpiringNodes, ExpiringNodeAudit{
							NodeID:    node.ID,
							NodeName:  node.Name,
							DueDate:   billingInfo.DueDate,
							DaysLeft:  daysLeft,
							Price:     billingInfo.Price,
							Currency:  billingInfo.Currency,
							AutoRenew: billingInfo.AutoRenew,
							Severity:  string(sev),
						})
					}
				}
			}

			// Idle VPS Detection:
			// If CPU is consistently low (< 2.5%) and monthly traffic is negligible (< 1GB)
			trafficGB := float64(billingInfo.CycleUsedBytes) / (1024 * 1024 * 1024)
			if isOnline && snap.CPUPercent < 2.5 && trafficGB < 1.0 && monthlyCost > 0 {
				rep.Summary.IdleNodes++
				potentialSavings += monthlyCost
				findings = append(findings, Finding{
					ID:             fmt.Sprintf("idle-%s", node.ID),
					Category:       CategoryCost,
					Severity:       SeverityOptimize,
					Title:          fmt.Sprintf("闲置 VPS 资源浪费 (低负载与低流量) - %s", node.Name),
					NodeID:         node.ID,
					NodeName:       node.Name,
					Summary:        fmt.Sprintf("当前节点 CPU 占用仅 %.1f%%，本周期流量仅消耗 %.2f GB，月租折合 %.2f %s。", snap.CPUPercent, trafficGB, monthlyCost, billingInfo.Currency),
					RootCause:      "实例长期处于非工作空闲状态或测试环境遗留。",
					Recommendation: fmt.Sprintf("建议评估业务整合或退订，可每月节约 %.2f %s 开支。", monthlyCost, billingInfo.Currency),
				})
				rep.CostAudit.IdleNodes = append(rep.CostAudit.IdleNodes, IdleNodeAudit{
					NodeID:              node.ID,
					NodeName:            node.Name,
					CPUPercent:          snap.CPUPercent,
					MonthlyTrafficGB:    trafficGB,
					MonthlyCost:         monthlyCost,
					Currency:            billingInfo.Currency,
					OptimizationSavings: monthlyCost,
					Suggestion:          "建议业务合并或到期不续费以节约预算",
				})
			}
		}

		// 4. MTR & Network Anomaly Inspection
		mtrResults, _ := s.store.ListMTRLatest(ctx, node.ID)
		for _, mtrItem := range mtrResults {
			var mtrRes protocol.MTRResult
			if err := json.Unmarshal(mtrItem.Payload, &mtrRes); err == nil {
				rootCause, isIssue, sev := AnalyzeMTRRootCause(mtrRes.Host, mtrRes.Hops, mtrRes.Reached)
				if isIssue {
					rep.Summary.NetworkIssues++
					findings = append(findings, Finding{
						ID:             fmt.Sprintf("mtr-%s-%s", node.ID, mtrItem.ID),
						Category:       CategoryNetwork,
						Severity:       sev,
						Title:          fmt.Sprintf("回程链路诊断异常: %s -> %s", node.Name, mtrRes.Host),
						NodeID:         node.ID,
						NodeName:       node.Name,
						Summary:        fmt.Sprintf("目标主机 %s 的回程路由探测触发异常。", mtrRes.Host),
						RootCause:      rootCause,
						Recommendation: "核查对端防火墙规则、本地云服务商网络出口或等待运营商骨干网路由收敛。",
					})
				}
			}
		}
	}

	rep.Summary.OnlineNodes = onlineCount
	rep.Summary.OfflineNodes = offlineCount
	rep.CostAudit.TotalMonthlyCost = totalMonthlyCost
	rep.CostAudit.PotentialMonthlySavings = potentialSavings
	rep.Findings = findings

	rep.HealthScore, rep.HealthStatus = CalculateHealthScore(findings, len(nodes), offlineCount)
	rep.MarkdownReport = GenerateMarkdownSummary(rep)

	return rep, nil
}

func (s *AIService) Chat(ctx context.Context, prompt string) (*AIChatResponse, error) {
	diagnosis, err := s.RunDiagnosis(ctx)
	if err != nil {
		return nil, fmt.Errorf("run diagnosis for chat: %w", err)
	}

	settings, _ := s.GetAISettings(ctx)
	storedKey, _ := s.store.GetSetting(ctx, "ai_api_key", "")

	// Check if external LLM call is enabled and key exists
	if settings.Enabled && storedKey != "" && settings.Provider != "local" {
		reply, err := s.callLLM(ctx, settings, storedKey, prompt, diagnosis)
		if err == nil && reply != "" {
			return &AIChatResponse{
				Reply:          reply,
				Provider:       settings.Provider,
				Model:          settings.Model,
				GeneratedAt:    time.Now().UTC().Unix(),
				HealthScore:    diagnosis.HealthScore,
				FallbackToRule: false,
			}, nil
		}
	}

	// Fallback to intelligent rule-based heuristic synthesizer
	ruleReply := s.generateRuleBasedAnswer(prompt, diagnosis)
	return &AIChatResponse{
		Reply:          ruleReply,
		Provider:       "ProbeWatch Built-in Heuristics Engine",
		Model:          "heuristics-v0.5.8",
		GeneratedAt:    time.Now().UTC().Unix(),
		HealthScore:    diagnosis.HealthScore,
		FallbackToRule: true,
	}, nil
}

func (s *AIService) callLLM(ctx context.Context, settings AISettings, apiKey, userPrompt string, diagnosis *DiagnosisReport) (string, error) {
	endpoint := strings.TrimRight(settings.APIEndpoint, "/")
	if !strings.HasSuffix(endpoint, "/chat/completions") {
		endpoint += "/chat/completions"
	}

	systemPrompt := fmt.Sprintf(`你是 ProbeWatch 全球基础设施监控与智能运维 Copilot 专家。
你拥有当前被监控集群的完整实时事实与深度遥测数据：
- 节点健康评分: %d/100 (%s)
- 节点总数: %d 台 (在线: %d, 离线: %d)
- 活跃告警: %d 项, 网络异常: %d 项
- 月度总服务器开销: %.2f %s, 潜在可节约: %.2f %s
- 闲置机器数量: %d 台, 近期即将到期: %d 台

已检测出的核心异常列表：
%s

请以专业、客观、逻辑严密的网络架构师与 SRE 专家口吻回答用户的提问，遵循：
1. 引用上述具体节点名称、指标数值与诊断事实；
2. 明确给出根因判断（区分出向拥堵、跨国骨干网抖动、中继路由限速、主机宕机或配置瓶颈）；
3. 给出切实可落地的排查命令与优化建议；
4. 语言使用干练清晰的 Markdown 格式输出。`,
		diagnosis.HealthScore, diagnosis.HealthStatus,
		diagnosis.Summary.TotalNodes, diagnosis.Summary.OnlineNodes, diagnosis.Summary.OfflineNodes,
		diagnosis.Summary.ActiveAlerts, diagnosis.Summary.NetworkIssues,
		diagnosis.CostAudit.TotalMonthlyCost, diagnosis.CostAudit.Currency,
		diagnosis.CostAudit.PotentialMonthlySavings, diagnosis.CostAudit.Currency,
		diagnosis.Summary.IdleNodes, diagnosis.Summary.ExpiringNodes,
		diagnosis.MarkdownReport)

	reqBody := map[string]any{
		"model": settings.Model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"temperature": 0.3,
	}

	rawJSON, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(rawJSON))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("llm request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("llm response code %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(bodyBytes, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("empty choices from llm")
	}

	return parsed.Choices[0].Message.Content, nil
}

func (s *AIService) generateRuleBasedAnswer(prompt string, diagnosis *DiagnosisReport) string {
	lower := strings.ToLower(prompt)
	var sb strings.Builder

	// Keyword routing
	if strings.Contains(lower, "网络") || strings.Contains(lower, "丢包") || strings.Contains(lower, "延迟") || strings.Contains(lower, "mtr") || strings.Contains(lower, "路由") {
		sb.WriteString("### 🌐 网络拓扑与链路瓶颈根因分析\n\n")
		networkFindings := 0
		for _, f := range diagnosis.Findings {
			if f.Category == CategoryNetwork {
				networkFindings++
				sb.WriteString(fmt.Sprintf("- **%s** (`%s`)\n", f.Title, f.NodeName))
				sb.WriteString(fmt.Sprintf("  - **根因分析**: %s\n", f.RootCause))
				sb.WriteString(fmt.Sprintf("  - **建议行动**: %s\n\n", f.Recommendation))
			}
		}
		if networkFindings == 0 {
			sb.WriteString("✅ **当前全网链路状态良好**：所有已监控节点的探测目标与回程路由均未检测到持续性丢包或骨干网拥塞。\n\n")
		}
		sb.WriteString("> **网络专家提示**：部分中继路由节点显示 ICMP 超时属正常骨干网限速策略，只要终点节点 RTT 稳定且丢包率为 0%，即不影响实际 TCP/UDP 业务。")
		return sb.String()
	}

	if strings.Contains(lower, "成本") || strings.Contains(lower, "闲置") || strings.Contains(lower, "省钱") || strings.Contains(lower, "续费") || strings.Contains(lower, "到期") || strings.Contains(lower, "账单") {
		sb.WriteString("### 💰 云服务器成本与闲置资产优化建议\n\n")
		sb.WriteString(fmt.Sprintf("- 💳 **月度估算总开销**: `%.2f %s`\n", diagnosis.CostAudit.TotalMonthlyCost, diagnosis.CostAudit.Currency))
		sb.WriteString(fmt.Sprintf("- 💡 **建议可优化节省**: `%.2f %s` / 月\n\n", diagnosis.CostAudit.PotentialMonthlySavings, diagnosis.CostAudit.Currency))

		if len(diagnosis.CostAudit.IdleNodes) > 0 {
			sb.WriteString("#### 💤 检测到的闲置/低负载服务器：\n")
			for _, idle := range diagnosis.CostAudit.IdleNodes {
				sb.WriteString(fmt.Sprintf("- **%s**: CPU 持续 %.1f%%，月流量仅 %.2f GB，月费折合 %.2f %s。建议业务合并或到期停机。\n",
					idle.NodeName, idle.CPUPercent, idle.MonthlyTrafficGB, idle.MonthlyCost, idle.Currency))
			}
			sb.WriteString("\n")
		}

		if len(diagnosis.CostAudit.ExpiringNodes) > 0 {
			sb.WriteString("#### 📅 近期即将到期的服务器：\n")
			for _, exp := range diagnosis.CostAudit.ExpiringNodes {
				sb.WriteString(fmt.Sprintf("- **%s**: 于 %s 到期（剩余 **%d 天**，续费价格 %.2f %s）。\n",
					exp.NodeName, exp.DueDate, exp.DaysLeft, exp.Price, exp.Currency))
			}
			sb.WriteString("\n")
		}

		if len(diagnosis.CostAudit.IdleNodes) == 0 && len(diagnosis.CostAudit.ExpiringNodes) == 0 {
			sb.WriteString("✅ 各节点资源利用率均衡，未发现显著闲置浪费或近 7 日内紧急到期的服务器。")
		}
		return sb.String()
	}

	// General executive answer
	sb.WriteString(fmt.Sprintf("### 🛡️ ProbeWatch 智能诊断概览 (健康得分: %d/100 · %s)\n\n", diagnosis.HealthScore, diagnosis.HealthStatus))
	sb.WriteString(fmt.Sprintf("当前纳入监控的服务器共计 **%d 台**（在线: `%d`，离线: `%d`），当前开放告警 **%d 项**。\n\n",
		diagnosis.Summary.TotalNodes, diagnosis.Summary.OnlineNodes, diagnosis.Summary.OfflineNodes, diagnosis.Summary.ActiveAlerts))

	if len(diagnosis.Findings) > 0 {
		sb.WriteString("#### 需重点关注的事项：\n")
		for i, f := range diagnosis.Findings {
			if i >= 5 {
				sb.WriteString(fmt.Sprintf("- *...另有 %d 项次要体检发现*\n", len(diagnosis.Findings)-5))
				break
			}
			sb.WriteString(fmt.Sprintf("- **[%s] %s**: %s（建议：%s）\n", f.Severity, f.Title, f.Summary, f.Recommendation))
		}
	} else {
		sb.WriteString("✅ 全站所有节点、硬件指标及网络链路均运转平稳，未发现需要人工干预的异常。")
	}

	return sb.String()
}
