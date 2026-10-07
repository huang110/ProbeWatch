package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/protocol"
)

func TestIPQACollector_NotInstalledGraceful(t *testing.T) {
	tmpDir := t.TempDir()
	c := &DefaultIPQACollector{
		enabled: true,
		baseDir: filepath.Join(tmpDir, "non_existent"),
	}
	info, err := c.collect()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	if info.Installed {
		t.Fatal("expected Installed = false")
	}
	if !info.Enabled {
		t.Fatal("expected Enabled = true")
	}
}

func TestIPQACollector_Disabled(t *testing.T) {
	tmpDir := t.TempDir()
	c := &DefaultIPQACollector{
		enabled: false,
		baseDir: filepath.Join(tmpDir, "non_existent"),
	}
	info := c.Get()
	if info == nil {
		t.Fatal("expected non-nil info for disabled collector")
	}
	if info.Enabled || info.Installed {
		t.Fatalf("expected Enabled=false and Installed=false, got %+v", info)
	}
	if info.State != "not_installed" {
		t.Fatalf("expected State=not_installed, got %q", info.State)
	}
}

func TestIPQACollector_FourStateMatrix(t *testing.T) {
	// Case 1: PROBEWATCH_IPQA_ENABLED=false, dir not exist -> enabled=false, installed=false, state="not_installed"
	tmp1 := t.TempDir()
	c1 := &DefaultIPQACollector{
		enabled: false,
		baseDir: filepath.Join(tmp1, "non_existent"),
	}
	info1 := c1.Get()
	if info1 == nil || info1.Enabled || info1.Installed || info1.State != "not_installed" {
		t.Fatalf("case 1 failed: %+v", info1)
	}

	// Case 2: PROBEWATCH_IPQA_ENABLED=false, data dir exists -> enabled=false, installed=true, state="installed_disabled"
	tmp2 := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp2, "data"), 0755); err != nil {
		t.Fatal(err)
	}
	c2 := &DefaultIPQACollector{
		enabled: false,
		baseDir: tmp2,
	}
	info2 := c2.Get()
	if info2 == nil || info2.Enabled || !info2.Installed || info2.State != "installed_disabled" {
		t.Fatalf("case 2 failed: %+v", info2)
	}

	// Case 3: PROBEWATCH_IPQA_ENABLED=true, data dir exists -> enabled=true, installed=true
	tmp3 := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp3, "data"), 0755); err != nil {
		t.Fatal(err)
	}
	c3 := &DefaultIPQACollector{
		enabled: true,
		baseDir: tmp3,
	}
	info3 := c3.Get()
	if info3 == nil || !info3.Enabled || !info3.Installed || info3.State != "enabled_waiting_archive" {
		t.Fatalf("case 3 failed: %+v", info3)
	}

	// Case 4: PROBEWATCH_IPQA_ENABLED=true, dir not exist -> enabled=true, installed=false, state="not_installed"
	tmp4 := t.TempDir()
	c4 := &DefaultIPQACollector{
		enabled: true,
		baseDir: filepath.Join(tmp4, "non_existent"),
	}
	info4 := c4.Get()
	if info4 == nil || !info4.Enabled || info4.Installed || info4.State != "not_installed" {
		t.Fatalf("case 4 failed: %+v", info4)
	}
}

func TestIPQACollector_AlertsLogParsing(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}

	todayBJ := time.Now().In(beijingLocation).Format("2006-01-02")
	logContent := fmt.Sprintf(`%s 02:00:00|INFO|首次完成数据存档监测|v4
%s 04:00:10|INFO|IP数据归档成功|v4
%s 04:05:22|WARNING|新增风险标记: Scamalytics 检出 Proxy 因子|v4
%s 04:10:00|CRITICAL|风险等级变更为高风险|v6
2020-01-01 01:00:00|CRITICAL|远古历史告警不应计入今日|v4
`, todayBJ, todayBJ, todayBJ, todayBJ)

	logPath := filepath.Join(dataDir, "alerts.log")
	if err := os.WriteFile(logPath, []byte(logContent), 0644); err != nil {
		t.Fatal(err)
	}

	c := &DefaultIPQACollector{
		enabled:              true,
		baseDir:              tmpDir,
		maxLogBytes:          1024 * 1024,
		ignoreInitialArchive: true,
	}

	info, err := c.collect()
	if err != nil {
		t.Fatalf("unexpected collect error: %v", err)
	}

	if !info.Installed {
		t.Fatal("expected Installed = true")
	}
	// Initial archive was ignored, older date was ignored.
	// Remaining today alerts: 1 INFO, 1 WARNING, 1 CRITICAL = 3
	if info.AlertCount != 3 {
		t.Fatalf("expected AlertCount = 3, got %d", info.AlertCount)
	}
	if info.CriticalCount != 1 {
		t.Fatalf("expected CriticalCount = 1, got %d", info.CriticalCount)
	}
	if info.WarningCount != 1 {
		t.Fatalf("expected WarningCount = 1, got %d", info.WarningCount)
	}
	if info.InfoCount != 1 {
		t.Fatalf("expected InfoCount = 1, got %d", info.InfoCount)
	}
	if info.HighestSeverity != "CRITICAL" {
		t.Fatalf("expected HighestSeverity = CRITICAL, got %s", info.HighestSeverity)
	}
}

func TestIPQACollector_EmptyAndCorruptedLog(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}

	// 1. Empty log
	logPath := filepath.Join(dataDir, "alerts.log")
	if err := os.WriteFile(logPath, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}
	c := &DefaultIPQACollector{enabled: true, baseDir: tmpDir, maxLogBytes: 1024 * 1024}
	info, err := c.collect()
	if err != nil {
		t.Fatalf("unexpected error on empty log: %v", err)
	}
	if info.AlertCount != 0 || info.HighestSeverity != "NONE" {
		t.Fatalf("expected 0 alerts, got %d with %s", info.AlertCount, info.HighestSeverity)
	}

	// 2. Corrupted log lines
	corrupted := "invalid line without pipes\n\n|incomplete||\n   \n"
	if err := os.WriteFile(logPath, []byte(corrupted), 0644); err != nil {
		t.Fatal(err)
	}
	info, err = c.collect()
	if err != nil {
		t.Fatalf("unexpected error on corrupted log: %v", err)
	}
	if info.AlertCount != 0 {
		t.Fatalf("expected 0 alerts from corrupted lines, got %d", info.AlertCount)
	}
}

func TestIPQACollector_OversizeLogRejected(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}

	logPath := filepath.Join(dataDir, "alerts.log")
	// Write 2KB with maxLogBytes set to 1KB
	data := make([]byte, 2048)
	for i := range data {
		data[i] = 'A'
	}
	if err := os.WriteFile(logPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	c := &DefaultIPQACollector{
		enabled:     true,
		baseDir:     tmpDir,
		maxLogBytes: 1024,
	}

	info, err := c.collect()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.CollectionError != "log_too_large" {
		t.Fatalf("expected collection_error=log_too_large, got %s", info.CollectionError)
	}
}

func TestIPQACollector_OverlongSingleLineTruncated(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}

	todayBJ := time.Now().In(beijingLocation).Format("2006-01-02")
	longMsg := make([]byte, 1000)
	for i := range longMsg {
		longMsg[i] = 'X'
	}
	line := fmt.Sprintf("%s 03:00:00|WARNING|%s|v4\n", todayBJ, string(longMsg))

	logPath := filepath.Join(dataDir, "alerts.log")
	if err := os.WriteFile(logPath, []byte(line), 0644); err != nil {
		t.Fatal(err)
	}

	c := &DefaultIPQACollector{
		enabled:     true,
		baseDir:     tmpDir,
		maxLogBytes: 1024 * 1024,
	}

	info, err := c.collect()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.WarningCount != 1 || info.AlertCount != 1 {
		t.Fatalf("expected 1 warning alert even with overlong line, got %d", info.AlertCount)
	}
}

func TestIPQACollector_ArchiveParsingAndDiff(t *testing.T) {
	tmpDir := t.TempDir()
	v4Dir := filepath.Join(tmpDir, "data", "v4")
	if err := os.MkdirAll(v4Dir, 0755); err != nil {
		t.Fatal(err)
	}

	// Archive 1: yesterday
	arch1 := rawIPQAArchive{
		Info: map[string]any{
			"Country":      "US",
			"Region":       "California",
			"ASN":          float64(15169),
			"Organization": "Google LLC",
			"Type":         "hosting",
		},
		Score: map[string]any{
			"Scamalytics": float64(10),
			"IPQS":        float64(15),
		},
		Factor: map[string]map[string]any{
			"Proxy": {"engineA": false},
			"Abuse": {"engineA": false},
		},
	}
	b1, _ := json.Marshal(arch1)
	if err := os.WriteFile(filepath.Join(v4Dir, "2026-10-05_040000.json"), b1, 0644); err != nil {
		t.Fatal(err)
	}

	// Archive 2: today, proxy detected and risk level increased
	arch2 := rawIPQAArchive{
		Info: map[string]any{
			"Country":      "US",
			"Region":       "California",
			"ASN":          float64(15169),
			"Organization": "Google LLC",
			"Type":         "hosting",
		},
		Score: map[string]any{
			"Scamalytics": float64(80),
			"IPQS":        float64(85),
		},
		Factor: map[string]map[string]any{
			"Proxy": {"engineA": true},
			"Abuse": {"engineA": false},
		},
	}
	b2, _ := json.Marshal(arch2)
	if err := os.WriteFile(filepath.Join(v4Dir, "2026-10-06_040000.json"), b2, 0644); err != nil {
		t.Fatal(err)
	}

	c := &DefaultIPQACollector{
		enabled:     true,
		baseDir:     tmpDir,
		maxLogBytes: 1024 * 1024,
	}

	info, err := c.collect()
	if err != nil {
		t.Fatalf("unexpected collect error: %v", err)
	}

	if info.IPv4 == nil {
		t.Fatal("expected non-nil IPv4 info")
	}
	if info.IPv4.Country != "US" || info.IPv4.ASN != "AS15169" {
		t.Fatalf("unexpected IPv4 info: %+v", info.IPv4)
	}
	if info.IPv4.RiskLevel != "high" {
		t.Fatalf("expected RiskLevel = high, got %s", info.IPv4.RiskLevel)
	}
	if info.IPv4.Proxy == nil || !*info.IPv4.Proxy {
		t.Fatal("expected IPv4 Proxy = true")
	}

	// Verify semantic difference detected
	if len(info.Changes) == 0 {
		t.Fatal("expected semantic changes between consecutive archives")
	}

	hasFactorChange := false
	hasRiskChange := false
	for _, ch := range info.Changes {
		if ch.Category == "factor" {
			hasFactorChange = true
		}
		if ch.Category == "risk_level" {
			hasRiskChange = true
		}
	}
	if !hasFactorChange || !hasRiskChange {
		t.Fatalf("expected both factor and risk_level changes, got %+v", info.Changes)
	}
	if info.LatestArchiveDate != "2026-10-06" {
		t.Fatalf("expected LatestArchiveDate = 2026-10-06, got %s", info.LatestArchiveDate)
	}
}

func TestIPQACollector_RetainsPreviousCacheOnError(t *testing.T) {
	c := &DefaultIPQACollector{
		enabled:  true,
		cacheTTL: 10 * time.Millisecond,
		cached: &protocol.IPQAInfo{
			Enabled:         true,
			Installed:       true,
			HighestSeverity: "INFO",
			AlertCount:      2,
		},
		lastFetch: time.Now().Add(-1 * time.Hour),
		customReader: func(ctx context.Context, dir string) (*protocol.IPQAInfo, error) {
			return nil, fmt.Errorf("simulated read failure")
		},
	}

	// Get triggers background fetch that fails
	first := c.Get()
	if first == nil || first.AlertCount != 2 {
		t.Fatalf("expected previous cache returned, got %+v", first)
	}

	time.Sleep(50 * time.Millisecond)

	second := c.Get()
	if second == nil || second.AlertCount != 2 {
		t.Fatalf("expected retained cache on failure, got %+v", second)
	}
	if second.CollectionError != "collect_failed" {
		t.Fatalf("expected CollectionError = collect_failed, got %s", second.CollectionError)
	}
}
