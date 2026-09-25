package ai

import (
	"strings"
	"testing"

	"github.com/probewatch/probewatch/internal/protocol"
)

func TestCalculateHealthScore(t *testing.T) {
	// 1. Perfect score
	score, status := CalculateHealthScore(nil, 5, 0)
	if score != 100 || status != "HEALTHY" {
		t.Fatalf("expected 100 HEALTHY, got %d %s", score, status)
	}

	// 2. Offline node penalties
	scoreOffline, statusOffline := CalculateHealthScore(nil, 4, 1)
	if scoreOffline >= 100 || statusOffline != "WARNING" && statusOffline != "HEALTHY" {
		t.Fatalf("expected score drop with 1/4 offline, got %d %s", scoreOffline, statusOffline)
	}

	// 3. Critical findings
	findings := []Finding{
		{Severity: SeverityCritical},
		{Severity: SeverityCritical},
		{Severity: SeverityCritical},
		{Severity: SeverityWarning},
	}
	scoreCrit, statusCrit := CalculateHealthScore(findings, 2, 0)
	if scoreCrit >= 60 || (statusCrit != "DEGRADED" && statusCrit != "CRITICAL") {
		t.Fatalf("expected low score with multiple critical findings, got %d %s", scoreCrit, statusCrit)
	}
}

func TestAnalyzeMTRRootCause(t *testing.T) {
	// Case 1: All healthy
	hopsHealthy := []protocol.MTRHop{
		{TTL: 1, IP: "192.168.1.1", LatencyMS: 2},
		{TTL: 2, IP: "10.0.0.1", LatencyMS: 5},
		{TTL: 3, IP: "1.1.1.1", LatencyMS: 15},
	}
	cause, anomaly, sev := AnalyzeMTRRootCause("1.1.1.1", hopsHealthy, true)
	if anomaly || sev != SeverityInfo || !strings.Contains(cause, "健康稳定") {
		t.Fatalf("expected healthy MTR, got anomaly=%v, cause=%s", anomaly, cause)
	}

	// Case 2: Local egress drop
	hopsLocalDrop := []protocol.MTRHop{
		{TTL: 1, TimedOut: true},
		{TTL: 2, TimedOut: true},
		{TTL: 3, IP: "1.1.1.1", LatencyMS: 20},
	}
	causeLocal, anomalyLocal, sevLocal := AnalyzeMTRRootCause("1.1.1.1", hopsLocalDrop, true)
	if !anomalyLocal || sevLocal != SeverityCritical || !strings.Contains(causeLocal, "局域网/网关丢包") {
		t.Fatalf("expected local egress anomaly, got anomaly=%v, cause=%s", anomalyLocal, causeLocal)
	}

	// Case 3: Intermediate ICMP rate limiting
	hopsICMP := []protocol.MTRHop{
		{TTL: 1, IP: "192.168.1.1", LatencyMS: 2},
		{TTL: 2, TimedOut: true},
		{TTL: 3, IP: "1.1.1.1", LatencyMS: 15},
	}
	causeICMP, anomalyICMP, sevICMP := AnalyzeMTRRootCause("1.1.1.1", hopsICMP, true)
	if anomalyICMP || sevICMP != SeverityInfo || !strings.Contains(causeICMP, "ICMP 限速策略") {
		t.Fatalf("expected harmless ICMP rate limiting, got anomaly=%v, cause=%s", anomalyICMP, causeICMP)
	}

	// Case 4: Destination unreached
	hopsUnreached := []protocol.MTRHop{
		{TTL: 1, IP: "192.168.1.1", LatencyMS: 2},
		{TTL: 2, IP: "10.0.0.1", LatencyMS: 10},
		{TTL: 3, TimedOut: true},
	}
	causeDest, anomalyDest, sevDest := AnalyzeMTRRootCause("1.1.1.1", hopsUnreached, false)
	if !anomalyDest || sevDest != SeverityCritical || !strings.Contains(causeDest, "无法直接连通") {
		t.Fatalf("expected destination unreached anomaly, got anomaly=%v, cause=%s", anomalyDest, causeDest)
	}

	// Case 5: Latency jump
	hopsJump := []protocol.MTRHop{
		{TTL: 1, IP: "192.168.1.1", LatencyMS: 2},
		{TTL: 2, IP: "202.97.1.1", LatencyMS: 20},
		{TTL: 3, IP: "59.43.1.1", LatencyMS: 180},
		{TTL: 4, IP: "1.1.1.1", LatencyMS: 185},
	}
	causeJump, anomalyJump, sevJump := AnalyzeMTRRootCause("1.1.1.1", hopsJump, true)
	if !anomalyJump || sevJump != SeverityWarning || !strings.Contains(causeJump, "延迟陡增") {
		t.Fatalf("expected latency jump anomaly, got anomaly=%v, cause=%s", anomalyJump, causeJump)
	}
}

func TestGenerateMarkdownSummary(t *testing.T) {
	rep := &DiagnosisReport{
		GeneratedAt:  1700000000,
		HealthScore:  92,
		HealthStatus: "HEALTHY",
		Summary: DiagnosisSummary{
			TotalNodes:    2,
			OnlineNodes:   2,
			OfflineNodes:  0,
			ActiveAlerts:  0,
			NetworkIssues: 0,
		},
		CostAudit: CostAuditSummary{
			TotalMonthlyCost: 45.0,
			Currency:         "USD",
		},
	}
	md := GenerateMarkdownSummary(rep)
	if !strings.Contains(md, "ProbeWatch 智能诊断与云上资产审计报告") {
		t.Fatalf("expected title in markdown report, got: %s", md)
	}
	if !strings.Contains(md, "健康评分") {
		t.Fatalf("expected health score in markdown report, got: %s", md)
	}
}
