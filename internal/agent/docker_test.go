package agent

import (
	"testing"
)

func TestCalculateCPUPercent(t *testing.T) {
	stats := &dockerContainerStats{}
	stats.CPUStats.CPUUsage.TotalUsage = 2000000000
	stats.PreCPUStats.CPUUsage.TotalUsage = 1000000000
	stats.CPUStats.SystemCPUUsage = 10000000000
	stats.PreCPUStats.SystemCPUUsage = 8000000000
	stats.CPUStats.OnlineCPUs = 2

	// cpuDelta = 1000000000
	// systemDelta = 2000000000
	// cpus = 2
	// percent = (1 / 2) * 2 * 100 = 100%
	percent := calculateCPUPercent(stats)
	if percent != 100.0 {
		t.Fatalf("expected 100.0%% CPU, got %f", percent)
	}
}

func TestCalculateMemoryUsage(t *testing.T) {
	stats := &dockerContainerStats{}
	stats.MemoryStats.Usage = 500 * 1024 * 1024
	stats.MemoryStats.Stats.Cache = 100 * 1024 * 1024

	usage := calculateMemoryUsage(stats)
	expected := uint64(400 * 1024 * 1024)
	if usage != expected {
		t.Fatalf("expected memory usage %d, got %d", expected, usage)
	}
}

func TestFindDockerSocket(t *testing.T) {
	// Should not panic even if no docker socket exists
	_ = FindDockerSocket()
}
