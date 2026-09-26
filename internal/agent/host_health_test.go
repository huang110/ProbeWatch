package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/probewatch/probewatch/internal/protocol"
)

func TestComputeHealthScoreOptimal(t *testing.T) {
	snap := &protocol.ResourceSnapshot{
		CPUPercent:           15.0,
		Load1:                0.5,
		CPUCores:             4,
		MemoryTotalBytes:     10000,
		MemoryUsedBytes:      3000,
		FilesystemTotalBytes: 10000,
		FilesystemUsedBytes:  2000,
		CPUTempC:             45.0,
	}

	score, status, deductions := computeHealthScore(snap, false, 0, nil)
	if score != 100 {
		t.Errorf("expected score 100, got %d", score)
	}
	if status != "optimal" {
		t.Errorf("expected status 'optimal', got %s", status)
	}
	if len(deductions) != 0 {
		t.Errorf("expected 0 deductions, got %v", deductions)
	}
}

func TestComputeHealthScoreDegraded(t *testing.T) {
	snap := &protocol.ResourceSnapshot{
		CPUPercent:           85.0, // -10
		Load1:                1.0,
		CPUCores:             4,
		MemoryTotalBytes:     10000,
		MemoryUsedBytes:      8800, // -10 (88%)
		FilesystemTotalBytes: 10000,
		FilesystemUsedBytes:  2000,
		CPUTempC:             78.0, // -8
	}

	// -10 (cpu) -10 (mem) -8 (temp) -5 (reboot) -5 (secUpdates) = 62
	score, status, deductions := computeHealthScore(snap, true, 2, nil)
	if score != 62 {
		t.Errorf("expected score 62, got %d", score)
	}
	if status != "warning" {
		t.Errorf("expected status 'warning', got %s", status)
	}
	if len(deductions) != 5 {
		t.Errorf("expected 5 deductions, got %d (%v)", len(deductions), deductions)
	}
}

func TestComputeHealthScoreCritical(t *testing.T) {
	snap := &protocol.ResourceSnapshot{
		CPUPercent:           95.0, // -20
		Load1:                20.0, // -20 (loadRatio 5.0)
		CPUCores:             4,
		MemoryTotalBytes:     10000,
		MemoryUsedBytes:      9800, // -25 (98%)
		FilesystemTotalBytes: 10000,
		FilesystemUsedBytes:  9700, // -25 (97%)
	}

	// 100 - 20 - 20 - 25 - 25 = 10 -> -15 (failed service) = 0
	score, status, deductions := computeHealthScore(snap, false, 0, []string{"docker.service"})
	if score != 0 {
		t.Errorf("expected score 0, got %d", score)
	}
	if status != "critical" {
		t.Errorf("expected status 'critical', got %s", status)
	}
	if len(deductions) < 4 {
		t.Errorf("expected at least 4 deductions, got %d", len(deductions))
	}
}

func TestCheckUpdatesAvailableAndReboot(t *testing.T) {
	tmpDir := t.TempDir()

	rebootPath := filepath.Join(tmpDir, "reboot-required")
	if checkRebootRequired(rebootPath) {
		t.Errorf("expected rebootRequired = false before file creation")
	}
	os.WriteFile(rebootPath, []byte(""), 0644)
	if !checkRebootRequired(rebootPath) {
		t.Errorf("expected rebootRequired = true after file creation")
	}

	updatesPath := filepath.Join(tmpDir, "updates-available")
	sampleText := `
16 updates can be applied immediately.
To see these additional updates run: apt list --upgradable

3 additional security updates can be applied with ESM Apps.
`
	os.WriteFile(updatesPath, []byte(sampleText), 0644)
	tot, sec := checkUpdatesAvailable(updatesPath)
	if tot != 16 {
		t.Errorf("expected total updates = 16, got %d", tot)
	}
	if sec != 3 {
		t.Errorf("expected security updates = 3, got %d", sec)
	}
}
