package ai

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/protocol"
)

type FindingCategory string

const (
	CategoryNetwork  FindingCategory = "network"
	CategoryCost     FindingCategory = "cost"
	CategorySystem   FindingCategory = "system"
	CategorySecurity FindingCategory = "security"
)

type FindingSeverity string

const (
	SeverityCritical FindingSeverity = "critical"
	SeverityWarning  FindingSeverity = "warning"
	SeverityOptimize FindingSeverity = "optimize"
	SeverityInfo     FindingSeverity = "info"
)

type Finding struct {
	ID             string          `json:"id"`
	Category       FindingCategory `json:"category"`
	Severity       FindingSeverity `json:"severity"`
	Title          string          `json:"title"`
	NodeID         string          `json:"node_id,omitempty"`
	NodeName       string          `json:"node_name,omitempty"`
	Summary        string          `json:"summary"`
	RootCause      string          `json:"root_cause"`
	Recommendation string          `json:"recommendation"`
}

type IdleNodeAudit struct {
	NodeID               string  `json:"node_id"`
	NodeName             string  `json:"node_name"`
	CPUPercent           float64 `json:"cpu_percent"`
	MonthlyTrafficGB     float64 `json:"monthly_traffic_gb"`
	MonthlyCost          float64 `json:"monthly_cost"`
	Currency             string  `json:"currency"`
	OptimizationSavings float64 `json:"optimization_savings"`
	Suggestion           string  `json:"suggestion"`
}

type ExpiringNodeAudit struct {
	NodeID    string  `json:"node_id"`
	NodeName  string  `json:"node_name"`
	DueDate   string  `json:"due_date"`
	DaysLeft  int     `json:"days_left"`
	Price     float64 `json:"price"`
	Currency  string  `json:"currency"`
	AutoRenew bool    `json:"auto_renew"`
	Severity  string  `json:"severity"`
}

type QuotaWarning struct {
	NodeID         string  `json:"node_id"`
	NodeName       string  `json:"node_name"`
	UsedBytes      uint64  `json:"used_bytes"`
	QuotaBytes     uint64  `json:"quota_bytes"`
	UsedPercent    float64 `json:"used_percent"`
	DaysUntilReset int     `json:"days_until_reset"`
}

type CostAuditSummary struct {
	TotalMonthlyCost        float64             `json:"total_monthly_cost"`
	Currency                string              `json:"currency"`
	PotentialMonthlySavings float64             `json:"potential_monthly_savings"`
	IdleNodes               []IdleNodeAudit     `json:"idle_nodes"`
	ExpiringNodes           []ExpiringNodeAudit `json:"expiring_nodes"`
	QuotaWarnings           []QuotaWarning      `json:"quota_warnings"`
}

type DiagnosisSummary struct {
	TotalNodes       int `json:"total_nodes"`
	OnlineNodes      int `json:"online_nodes"`
	OfflineNodes     int `json:"offline_nodes"`
	ActiveAlerts     int `json:"active_alerts"`
	NetworkIssues    int `json:"network_issues"`
	IdleNodes        int `json:"idle_nodes"`
	ExpiringNodes    int `json:"expiring_nodes"`
	ResourceWarnings int `json:"resource_warnings"`
}

type DiagnosisReport struct {
	GeneratedAt    int64            `json:"generated_at"`
	HealthScore    int              `json:"health_score"`
	HealthStatus   string           `json:"health_status"` // "HEALTHY", "WARNING", "DEGRADED", "CRITICAL"
	Summary        DiagnosisSummary `json:"summary"`
	Findings       []Finding        `json:"findings"`
	CostAudit      CostAuditSummary `json:"cost_audit"`
	MarkdownReport string           `json:"markdown_report"`
}

// CalculateHealthScore computes the fleet health score from 0 to 100 based on detected findings.
func CalculateHealthScore(findings []Finding, totalNodes int, offlineNodes int) (int, string) {
	score := 100

	// Offline node penalties
	if totalNodes > 0 && offlineNodes > 0 {
		offlineRatio := float64(offlineNodes) / float64(totalNodes)
		score -= int(math.Round(offlineRatio * 40.0))
	}

	for _, f := range findings {
		switch f.Severity {
		case SeverityCritical:
			score -= 15
		case SeverityWarning:
			score -= 6
		case SeverityOptimize:
			score -= 2
		}
	}

	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	var status string
	switch {
	case score >= 90:
		status = "HEALTHY"
	case score >= 75:
		status = "WARNING"
	case score >= 50:
		status = "DEGRADED"
	default:
		status = "CRITICAL"
	}

	return score, status
}

// AnalyzeMTRRootCause inspects an MTR result and determines root causes of latency/loss.
func AnalyzeMTRRootCause(targetHost string, hops []protocol.MTRHop, reached bool) (rootCause string, isAnomaly bool, severity FindingSeverity) {
	if len(hops) == 0 {
		if !reached {
			return "路由追踪无数据且目标无法连通，可能是节点本地网络故障或 DNS 解析异常", true, SeverityCritical
		}
		return "链路正常", false, SeverityInfo
	}

	// 1. Check local egress (hop 1 and hop 2)
	localLossCount := 0
	for i := 0; i < len(hops) && i < 2; i++ {
		if hops[i].TimedOut {
			localLossCount++
		}
	}
	if localLossCount >= 2 {
		return "节点本地出向局域网/网关丢包，疑似虚拟化交换机限速、物理网线松动或上级路由器丢包", true, SeverityCritical
	}

	// 2. Check destination reachability
	lastHop := hops[len(hops)-1]
	if !reached || lastHop.TimedOut {
		return fmt.Sprintf("目标端主机 (%s) 无法直接连通，可能目标端服务停止、被云厂商安全组屏蔽或遭遇封锁", targetHost), true, SeverityCritical
	}

	// 3. Check for intermediate router ICMP rate limiting
	intermediateTimeouts := 0
	for i := 1; i < len(hops)-1; i++ {
		if hops[i].TimedOut {
			intermediateTimeouts++
		}
	}
	if intermediateTimeouts > 0 && reached && !lastHop.TimedOut {
		return "中继路由跳步存在超时，但终点目标响应正常，属于运营商骨干网路由器的常规 ICMP 限速策略，无实际业务影响", false, SeverityInfo
	}

	// 4. Check for high latency / transatlantic cross-carrier jump
	var maxJump int64
	var jumpHopIndex int
	for i := 1; i < len(hops); i++ {
		if !hops[i].TimedOut && !hops[i-1].TimedOut {
			diff := hops[i].LatencyMS - hops[i-1].LatencyMS
			if diff > maxJump {
				maxJump = diff
				jumpHopIndex = i
			}
		}
	}

	if maxJump > 120 {
		return fmt.Sprintf("在第 %d 跳 (%s) 出现显著延迟陡增 (+%dms)，疑似跨境海缆传输、运营商省际骨干网拥塞或 BGP 跨网互联绕路",
			jumpHopIndex+1, hops[jumpHopIndex].IP, maxJump), true, SeverityWarning
	}

	return "回程路由与链路状态健康稳定", false, SeverityInfo
}

// GenerateMarkdownSummary constructs a human-readable diagnostic report.
func GenerateMarkdownSummary(rep *DiagnosisReport) string {
	var sb strings.Builder

	sb.WriteString("# 🛡️ ProbeWatch 智能诊断与云上资产审计报告\n\n")
	sb.WriteString(fmt.Sprintf("**生成时间**: %s | **健康评分**: `%d/100` (%s)\n\n",
		time.Unix(rep.GeneratedAt, 0).UTC().Format("2006-01-02 15:04:05 UTC"),
		rep.HealthScore, rep.HealthStatus))

	sb.WriteString("## 📊 基础设施运行概览\n\n")
	sb.WriteString(fmt.Sprintf("- 🖥️ **服务器总数**: %d 台 (在线: `%d`, 离线: `%d`)\n",
		rep.Summary.TotalNodes, rep.Summary.OnlineNodes, rep.Summary.OfflineNodes))
	sb.WriteString(fmt.Sprintf("- 🔔 **活跃告警**: %d 项 | 🌐 **网络异常**: %d 项\n",
		rep.Summary.ActiveAlerts, rep.Summary.NetworkIssues))
	sb.WriteString(fmt.Sprintf("- 💰 **月度总开销**: `%.2f %s` | 💡 **潜在可节约**: `%.2f %s`\n",
		rep.CostAudit.TotalMonthlyCost, rep.CostAudit.Currency,
		rep.CostAudit.PotentialMonthlySavings, rep.CostAudit.Currency))
	sb.WriteString(fmt.Sprintf("- 💤 **闲置 VPS**: %d 台 | 📅 **近期到期**: %d 台\n\n",
		rep.Summary.IdleNodes, rep.Summary.ExpiringNodes))

	if len(rep.Findings) > 0 {
		sb.WriteString("## 🔍 核心体检发现与根因分析\n\n")
		for i, f := range rep.Findings {
			badge := "⚠️ [需注意]"
			if f.Severity == SeverityCritical {
				badge = "🚨 [紧急]"
			} else if f.Severity == SeverityOptimize {
				badge = "💡 [优化建议]"
			}
			sb.WriteString(fmt.Sprintf("### %d. %s %s\n", i+1, badge, f.Title))
			if f.NodeName != "" {
				sb.WriteString(fmt.Sprintf("- **关联节点**: `%s`\n", f.NodeName))
			}
			sb.WriteString(fmt.Sprintf("- **问题概述**: %s\n", f.Summary))
			sb.WriteString(fmt.Sprintf("- **根因诊断**: %s\n", f.RootCause))
			sb.WriteString(fmt.Sprintf("- **处置建议**: %s\n\n", f.Recommendation))
		}
	} else {
		sb.WriteString("## 🔍 核心体检发现\n\n✅ 全站所有节点、网络目标及系统资源均处于良好健康状态，未检测到异常瓶颈。\n\n")
	}

	if len(rep.CostAudit.IdleNodes) > 0 {
		sb.WriteString("## 💤 闲置 VPS 降本审计\n\n")
		sb.WriteString("| 节点名称 | 持续 CPU | 月流量估算 | 月单价 | 优化建议 |\n")
		sb.WriteString("| :--- | :--- | :--- | :--- | :--- |\n")
		for _, idle := range rep.CostAudit.IdleNodes {
			sb.WriteString(fmt.Sprintf("| %s | %.1f%% | %.2f GB | %.2f %s | %s |\n",
				idle.NodeName, idle.CPUPercent, idle.MonthlyTrafficGB, idle.MonthlyCost, idle.Currency, idle.Suggestion))
		}
		sb.WriteString("\n")
	}

	if len(rep.CostAudit.ExpiringNodes) > 0 {
		sb.WriteString("## 📅 服务器到期与续费预警\n\n")
		sb.WriteString("| 节点名称 | 到期日期 | 剩余天数 | 续费金额 | 自动续费 |\n")
		sb.WriteString("| :--- | :--- | :--- | :--- | :--- |\n")
		for _, exp := range rep.CostAudit.ExpiringNodes {
			autoStr := "手动续费"
			if exp.AutoRenew {
				autoStr = "自动续费"
			}
			sb.WriteString(fmt.Sprintf("| %s | %s | **%d 天** | %.2f %s | %s |\n",
				exp.NodeName, exp.DueDate, exp.DaysLeft, exp.Price, exp.Currency, autoStr))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
