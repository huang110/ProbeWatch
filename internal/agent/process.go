package agent

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/probewatch/probewatch/internal/protocol"
)

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

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}

		snap, err := readProcessInfo(pid, totalMemBytes)
		if err != nil {
			// Process may have exited between listing and reading
			continue
		}
		procs = append(procs, snap)
	}

	// Sort by RSS memory descending
	sort.Slice(procs, func(i, j int) bool {
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

	statStr := string(statData)
	// Process comm is enclosed in parentheses, e.g. "123 (kworker/0:0) S 2 ..."
	leftParen := strings.Index(statStr, "(")
	rightParen := strings.LastIndex(statStr, ")")
	if leftParen != -1 && rightParen != -1 && rightParen > leftParen {
		snap.Name = statStr[leftParen+1 : rightParen]
		after := strings.TrimSpace(statStr[rightParen+1:])
		fields := strings.Fields(after)
		// after fields:
		// 0: state (e.g. S, R, Z)
		// 1: ppid
		// 2: pgrp
		// ...
		// 11: utime (field 14 of original)
		// 12: stime (field 15 of original)
		// ...
		// 17: num_threads (field 20 of original)
		if len(fields) > 0 {
			snap.State = fields[0]
		}
		if len(fields) > 1 {
			if ppid, err := strconv.Atoi(fields[1]); err == nil {
				snap.PPID = int32(ppid)
			}
		}
		if len(fields) > 17 {
			if threads, err := strconv.Atoi(fields[17]); err == nil {
				snap.Threads = int32(threads)
			}
		}
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
