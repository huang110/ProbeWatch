package agent

import (
	"testing"
	"time"
)

func TestProcessCPUCalculationNormal(t *testing.T) {
	tracker := NewProcessSampleTracker(64)
	pid := int32(1001)
	startTime := uint64(50000)
	t0 := time.Now()

	// 1. First sample returns 0.0
	cpu := tracker.CalculateAndRecord(pid, 100, 50, startTime, t0, 2)
	if cpu != 0.0 {
		t.Fatalf("expected first sample to return 0.0, got %f", cpu)
	}

	// 2. Second sample 1.0 second later
	// Delta ticks = (150 + 75) - (100 + 50) = 225 - 150 = 75 ticks
	// deltaSec = 1.0s, CLK_TCK = 100
	// cpuPercent = (75 / 1.0 / 100) * 100 = 75.0%
	t1 := t0.Add(1 * time.Second)
	cpu = tracker.CalculateAndRecord(pid, 150, 75, startTime, t1, 2)
	if cpu != 75.0 {
		t.Fatalf("expected 75.0%%, got %f", cpu)
	}
}

func TestProcessPIDReuse(t *testing.T) {
	tracker := NewProcessSampleTracker(64)
	pid := int32(2002)
	t0 := time.Now()

	// Initial process with StartTime 1000
	_ = tracker.CalculateAndRecord(pid, 100, 50, 1000, t0, 4)

	// PID is reused by a new process with StartTime 2000
	t1 := t0.Add(1 * time.Second)
	cpu := tracker.CalculateAndRecord(pid, 20, 10, 2000, t1, 4)
	if cpu != 0.0 {
		t.Fatalf("expected PID reuse with new start_time to return 0.0, got %f", cpu)
	}

	// Next sample of the new process
	t2 := t1.Add(1 * time.Second)
	// Delta ticks = (40 + 20) - (20 + 10) = 60 - 30 = 30 ticks = 30.0%
	cpu = tracker.CalculateAndRecord(pid, 40, 20, 2000, t2, 4)
	if cpu != 30.0 {
		t.Fatalf("expected 30.0%% after baseline reset on reused PID, got %f", cpu)
	}
}

func TestProcessFirstSampleReturnsZero(t *testing.T) {
	tracker := NewProcessSampleTracker(64)
	cpu := tracker.CalculateAndRecord(3003, 500, 500, 10000, time.Now(), 1)
	if cpu != 0.0 {
		t.Fatalf("expected 0.0 for initial sample, got %f", cpu)
	}
}

func TestProcessShortIntervalProtection(t *testing.T) {
	tracker := NewProcessSampleTracker(64)
	pid := int32(4004)
	startTime := uint64(12345)
	t0 := time.Now()

	_ = tracker.CalculateAndRecord(pid, 100, 100, startTime, t0, 2)

	// Sample 5ms later (< 10ms threshold)
	t1 := t0.Add(5 * time.Millisecond)
	cpu := tracker.CalculateAndRecord(pid, 120, 120, startTime, t1, 2)
	if cpu != 0.0 {
		t.Fatalf("expected short interval <10ms to return 0.0, got %f", cpu)
	}
}

func TestProcessMultiCoreCap(t *testing.T) {
	tracker := NewProcessSampleTracker(64)
	pid := int32(5005)
	startTime := uint64(54321)
	t0 := time.Now()

	_ = tracker.CalculateAndRecord(pid, 0, 0, startTime, t0, 4) // 4 cores, max = 400.0%

	// Delta ticks = 1000 in 1 second -> raw 1000.0%
	t1 := t0.Add(1 * time.Second)
	cpu := tracker.CalculateAndRecord(pid, 600, 400, startTime, t1, 4)
	if cpu != 400.0 {
		t.Fatalf("expected capped at 400.0%% for 4 cores, got %f", cpu)
	}
}

func TestParseProcessStatMalformed(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
		wantPID int32
		wantCmd string
	}{
		{
			name:    "Standard stat line",
			raw:     "1234 (nginx) S 1 1234 1234 0 -1 4194304 100 0 0 0 150 50 0 0 20 0 2 0 12345 10000 0",
			wantErr: false,
			wantPID: 1234,
			wantCmd: "nginx",
		},
		{
			name:    "Process name with spaces and nested parentheses",
			raw:     "5678 (Worker (child: 1)) R 1 5678 5678 0 -1 4194304 0 0 0 0 300 100 0 0 20 0 4 0 99999 50000 0",
			wantErr: false,
			wantPID: 5678,
			wantCmd: "Worker (child: 1)",
		},
		{
			name:    "Missing closing parenthesis",
			raw:     "1234 (nginx S 1 2 3",
			wantErr: true,
		},
		{
			name:    "Missing opening parenthesis",
			raw:     "1234 nginx) S 1 2 3",
			wantErr: true,
		},
		{
			name:    "Empty string",
			raw:     "",
			wantErr: true,
		},
		{
			name:    "Only parentheses",
			raw:     "()",
			wantErr: true,
		},
		{
			name:    "Too few fields after comm",
			raw:     "123 (test)",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stat, err := parseProcessStat(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for malformed line, got nil: %+v", stat)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if stat.PID != tc.wantPID {
				t.Errorf("expected PID %d, got %d", tc.wantPID, stat.PID)
			}
			if stat.Comm != tc.wantCmd {
				t.Errorf("expected Comm %q, got %q", tc.wantCmd, stat.Comm)
			}
		})
	}
}

func TestProcessTrackerPruningAndCapacity(t *testing.T) {
	maxSize := 10
	tracker := NewProcessSampleTracker(maxSize)
	now := time.Now()

	// Fill tracker beyond maxSize
	for i := 1; i <= 20; i++ {
		tracker.CalculateAndRecord(int32(i), 10, 10, 1000, now, 1)
	}

	tracker.mu.Lock()
	if len(tracker.samples) > maxSize {
		tracker.mu.Unlock()
		t.Fatalf("tracker exceeded max capacity: %d > %d", len(tracker.samples), maxSize)
	}
	tracker.mu.Unlock()

	// Prune inactive PIDs
	active := map[int32]struct{}{
		19: {},
		20: {},
	}
	tracker.PruneExited(active)

	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	for pid := range tracker.samples {
		if _, ok := active[pid]; !ok {
			t.Errorf("PID %d should have been pruned", pid)
		}
	}
}

func TestProcessTrackerDefaultCapacity(t *testing.T) {
	if defaultProcessTracker.maxSize != 4096 {
		t.Fatalf("expected defaultProcessTracker maxSize to be 4096, got %d", defaultProcessTracker.maxSize)
	}
	tr0 := NewProcessSampleTracker(0)
	if tr0.maxSize != 4096 {
		t.Fatalf("expected NewProcessSampleTracker(0) to default to 4096, got %d", tr0.maxSize)
	}
}

func TestProcessTrackerDeterministicOldestEviction(t *testing.T) {
	// Bounded capacity of 4
	tracker := NewProcessSampleTracker(4)
	t0 := time.Now()

	// Initial samples for PIDs 1..4 at t0
	for pid := int32(1); pid <= 4; pid++ {
		tracker.CalculateAndRecord(pid, 10, 10, 1000, t0, 1)
	}

	// In the next tick at t1 (+1s), PIDs 2, 3, 4 are updated (they are active)
	// PID 1 is NOT updated (representing an inactive/stale process)
	t1 := t0.Add(1 * time.Second)
	for pid := int32(2); pid <= 4; pid++ {
		tracker.CalculateAndRecord(pid, 20, 20, 1000, t1, 1)
	}

	// Now a new PID 5 appears at t1
	tracker.CalculateAndRecord(5, 5, 5, 2000, t1, 1)

	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	// Capacity must be bounded to 4
	if len(tracker.samples) != 4 {
		t.Fatalf("expected tracker size 4, got %d", len(tracker.samples))
	}

	// PID 1 must be evicted because its sample was oldest (t0)
	if _, ok := tracker.samples[1]; ok {
		t.Errorf("PID 1 should have been evicted as the oldest sample")
	}

	// Active PIDs 2, 3, 4 and new PID 5 must all be present
	for _, pid := range []int32{2, 3, 4, 5} {
		if _, ok := tracker.samples[pid]; !ok {
			t.Errorf("active PID %d should have been retained", pid)
		}
	}
}

func TestProcessTrackerExitedProcessPriorityEviction(t *testing.T) {
	// Tracker capacity of 5
	tracker := NewProcessSampleTracker(5)
	t0 := time.Now()

	// 5 processes active in round 1
	for pid := int32(101); pid <= 105; pid++ {
		tracker.CalculateAndRecord(pid, 100, 50, 1000, t0, 1)
	}

	// In round 2 (+10s), PIDs 102..105 are sampled again. PID 101 has exited.
	t1 := t0.Add(10 * time.Second)
	for pid := int32(102); pid <= 105; pid++ {
		tracker.CalculateAndRecord(pid, 150, 75, 1000, t1, 1)
	}

	// A new process 106 arrives in round 2
	tracker.CalculateAndRecord(106, 10, 10, 2000, t1, 1)

	tracker.mu.Lock()
	// PID 101 (exited process with old timestamp) must be the one evicted
	if _, ok := tracker.samples[101]; ok {
		t.Errorf("exited PID 101 should have been prioritized for eviction")
	}
	// Active PIDs 102-105 and 106 must remain
	for _, pid := range []int32{102, 103, 104, 105, 106} {
		if _, ok := tracker.samples[pid]; !ok {
			t.Errorf("active PID %d should not have been evicted", pid)
		}
	}
	tracker.mu.Unlock()

	// In round 3 (+10s), active PIDs continue running smoothly without baseline loss
	t2 := t1.Add(10 * time.Second)
	// Delta ticks = (200 + 100) - (150 + 75) = 300 - 225 = 75 ticks over 10s -> 7.5%
	cpu := tracker.CalculateAndRecord(102, 200, 100, 1000, t2, 1)
	if cpu != 7.5 {
		t.Errorf("expected smooth 7.5%% CPU for active PID 102, got %f", cpu)
	}
}

func TestProcessCPUSmoothRunningState(t *testing.T) {
	tracker := NewProcessSampleTracker(64)
	pid := int32(8888)
	startTime := uint64(5000)
	t0 := time.Now()

	// Initial sample returns 0.0
	cpu := tracker.CalculateAndRecord(pid, 100, 100, startTime, t0, 1)
	if cpu != 0.0 {
		t.Fatalf("first sample must return 0.0, got %f", cpu)
	}

	// 10 consecutive ticks of 50 ticks/second (50% CPU on 1 core)
	currentTicks := uint64(200)
	currTime := t0
	for step := 1; step <= 10; step++ {
		currTime = currTime.Add(1 * time.Second)
		currentTicks += 50
		cpu = tracker.CalculateAndRecord(pid, currentTicks/2, currentTicks/2, startTime, currTime, 1)
		if cpu != 50.0 {
			t.Fatalf("step %d: expected smooth 50.0%% CPU, got %f", step, cpu)
		}
	}
}
