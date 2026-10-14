package agent

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/probewatch/probewatch/internal/protocol"
)

// ProcCPUSample stores previous ticks and timestamp for a specific process.
type ProcCPUSample struct {
	TotalTicks uint64
	Timestamp  time.Time
	StartTime  uint64
}

// ProcessSampleTracker tracks CPU tick samples per PID with bounded capacity and thread-safety.
type ProcessSampleTracker struct {
	mu      sync.Mutex
	samples map[int32]ProcCPUSample
	maxSize int
}

// defaultProcessTracker is the package-level process tracker bounded to 512 entries.
var defaultProcessTracker = NewProcessSampleTracker(512)

// NewProcessSampleTracker creates a new ProcessSampleTracker.
func NewProcessSampleTracker(maxSize int) *ProcessSampleTracker {
	if maxSize <= 0 {
		maxSize = 512
	}
	return &ProcessSampleTracker{
		samples: make(map[int32]ProcCPUSample),
		maxSize: maxSize,
	}
}

// CalculateAndRecord computes the process CPU percentage over the elapsed duration.
// Returns 0.0 on first sample, PID reuse, short intervals (<10ms), or errors.
func (t *ProcessSampleTracker) CalculateAndRecord(pid int32, utime, stime, startTime uint64, now time.Time, numCPU int) float64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	currentTicks := utime + stime
	prev, exists := t.samples[pid]

	// Enforce capacity bounds if a new PID is being added
	if !exists && len(t.samples) >= t.maxSize {
		for k := range t.samples {
			delete(t.samples, k)
			break
		}
	}

	t.samples[pid] = ProcCPUSample{
		TotalTicks: currentTicks,
		Timestamp:  now,
		StartTime:  startTime,
	}

	// First sample for this PID or PID was reused
	if !exists || prev.StartTime != startTime {
		return 0.0
	}

	deltaSec := now.Sub(prev.Timestamp).Seconds()
	// Guard against short delta < 10ms or backward time
	if deltaSec < 0.01 {
		return 0.0
	}

	// Guard against counter rollover or clock reset
	if currentTicks < prev.TotalTicks {
		return 0.0
	}

	deltaTicks := float64(currentTicks - prev.TotalTicks)
	const clkTck = 100.0 // Standard Linux sysconf(_SC_CLK_TCK)
	cpuPercent := (deltaTicks / deltaSec / clkTck) * 100.0

	if cpuPercent < 0.0 {
		cpuPercent = 0.0
	}

	if numCPU <= 0 {
		numCPU = 1
	}
	maxPercent := float64(numCPU) * 100.0
	if cpuPercent > maxPercent {
		cpuPercent = maxPercent
	}

	return math.Round(cpuPercent*100) / 100
}

// PruneExited removes any tracked PIDs that are no longer active.
func (t *ProcessSampleTracker) PruneExited(activePIDs map[int32]struct{}) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for pid := range t.samples {
		if _, ok := activePIDs[pid]; !ok {
			delete(t.samples, pid)
		}
	}
}

// ProcStat contains parsed fields from /proc/[pid]/stat.
type ProcStat struct {
	PID       int32
	Comm      string
	State     string
	PPID      int32
	Threads   int32
	UTime     uint64
	STime     uint64
	StartTime uint64
}

// parseProcessStat safely extracts fields from /proc/[pid]/stat string.
func parseProcessStat(statStr string) (ProcStat, error) {
	leftParen := strings.Index(statStr, "(")
	rightParen := strings.LastIndex(statStr, ")")
	if leftParen == -1 || rightParen == -1 || rightParen <= leftParen {
		return ProcStat{}, fmt.Errorf("malformed proc stat: parentheses not found")
	}

	res := ProcStat{
		Comm: statStr[leftParen+1 : rightParen],
	}

	before := strings.TrimSpace(statStr[:leftParen])
	if pid, err := strconv.Atoi(before); err == nil {
		res.PID = int32(pid)
	}

	after := strings.TrimSpace(statStr[rightParen+1:])
	fields := strings.Fields(after)
	if len(fields) < 1 {
		return res, fmt.Errorf("malformed proc stat: empty fields after comm")
	}

	// fields[0]: state
	res.State = fields[0]

	// fields[1]: ppid
	if len(fields) > 1 {
		if ppid, err := strconv.Atoi(fields[1]); err == nil {
			res.PPID = int32(ppid)
		}
	}

	// fields[11]: utime (14th field in /proc/[pid]/stat)
	if len(fields) > 11 {
		if utime, err := strconv.ParseUint(fields[11], 10, 64); err == nil {
			res.UTime = utime
		}
	}

	// fields[12]: stime (15th field in /proc/[pid]/stat)
	if len(fields) > 12 {
		if stime, err := strconv.ParseUint(fields[12], 10, 64); err == nil {
			res.STime = stime
		}
	}

	// fields[17]: num_threads (20th field in /proc/[pid]/stat)
	if len(fields) > 17 {
		if threads, err := strconv.Atoi(fields[17]); err == nil {
			res.Threads = int32(threads)
		}
	}

	// fields[19]: starttime (22nd field in /proc/[pid]/stat)
	if len(fields) > 19 {
		if starttime, err := strconv.ParseUint(fields[19], 10, 64); err == nil {
			res.StartTime = starttime
		}
	}

	return res, nil
}

// CollectTopProcesses collects the top resource-consuming processes from the Linux /proc filesystem.
func CollectTopProcesses(limit int) ([]protocol.ProcessSnapshot, error) {
	if runtime.GOOS != "linux" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	totalMemBytes := uint64(1)
	if mem, err := readMemInfo(); err == nil && mem.total > 0 {
		totalMemBytes = mem.total
	}

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("read /proc: %w", err)
	}

	var procs []protocol.ProcessSnapshot
	activePIDs := make(map[int32]struct{}, len(entries))

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		activePIDs[int32(pid)] = struct{}{}

		snap, err := readProcessInfo(pid, totalMemBytes)
		if err != nil {
			// Process may have exited between listing and reading
			continue
		}
		procs = append(procs, snap)
	}

	defaultProcessTracker.PruneExited(activePIDs)

	// Sort by CPU percent descending, then RSS memory descending
	sort.Slice(procs, func(i, j int) bool {
		if procs[i].CPUPercent != procs[j].CPUPercent {
			return procs[i].CPUPercent > procs[j].CPUPercent
		}
		return procs[i].MemoryRSSBytes > procs[j].MemoryRSSBytes
	})

	if len(procs) > limit {
		procs = procs[:limit]
	}

	return procs, nil
}

func readProcessInfo(pid int, totalMemBytes uint64) (protocol.ProcessSnapshot, error) {
	snap := protocol.ProcessSnapshot{
		PID: int32(pid),
	}

	// 1. Read /proc/[pid]/stat
	statPath := fmt.Sprintf("/proc/%d/stat", pid)
	statData, err := os.ReadFile(statPath)
	if err != nil {
		return snap, err
	}

	stat, err := parseProcessStat(string(statData))
	if err == nil {
		snap.Name = stat.Comm
		snap.State = stat.State
		snap.PPID = stat.PPID
		snap.Threads = stat.Threads
		snap.CPUPercent = defaultProcessTracker.CalculateAndRecord(
			int32(pid),
			stat.UTime,
			stat.STime,
			stat.StartTime,
			time.Now(),
			runtime.NumCPU(),
		)
	}

	// 2. Read /proc/[pid]/status for VmRSS and Uid
	statusPath := fmt.Sprintf("/proc/%d/status", pid)
	statusFile, err := os.Open(statusPath)
	if err == nil {
		defer statusFile.Close()
		scanner := bufio.NewScanner(statusFile)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "VmRSS:") {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					if kb, err := strconv.ParseUint(parts[1], 10, 64); err == nil {
						snap.MemoryRSSBytes = kb * 1024
					}
				}
			} else if strings.HasPrefix(line, "Uid:") {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					snap.User = parts[1]
					if parts[1] == "0" {
						snap.User = "root"
					}
				}
			}
		}
	}

	if totalMemBytes > 0 && snap.MemoryRSSBytes > 0 {
		percent := (float64(snap.MemoryRSSBytes) / float64(totalMemBytes)) * 100.0
		snap.MemoryPercent = float64(int(percent*100)) / 100.0
	}

	// 3. Read /proc/[pid]/cmdline (bounded to 256 bytes)
	cmdlinePath := fmt.Sprintf("/proc/%d/cmdline", pid)
	if cmdData, err := os.ReadFile(cmdlinePath); err == nil && len(cmdData) > 0 {
		cmdStr := strings.ReplaceAll(string(cmdData), "\x00", " ")
		cmdStr = strings.TrimSpace(cmdStr)
		if len(cmdStr) > 256 {
			cmdStr = cmdStr[:256] + "..."
		}
		snap.CommandLine = cmdStr
	}

	return snap, nil
}
