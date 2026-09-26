package agent

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/probewatch/probewatch/internal/protocol"
)

var (
	oomRegex    = regexp.MustCompile(`(?i)(?:Out of memory:\s*Kill(?:ed)?\s*process\s*(\d+)\s*\(([^)]+)\)|Killed process\s*(\d+)\s*\(([^)]+)\)|invoked oom-killer)`)
	kernelRegex = regexp.MustCompile(`(?i)(?:segfault at [0-9a-f]+|general protection fault|kernel BUG at|Kernel panic - not syncing)`)
	sshFailRegex = regexp.MustCompile(`(?i)Failed password for (?:invalid user )?([^\s]+) from ([0-9a-f\.\:]+) port (\d+)`)
)

type EventCollector struct {
	mu           sync.Mutex
	seenSignatures map[string]int64 // signature -> timestamp (epoch seconds)
}

var defaultCollector = &EventCollector{
	seenSignatures: make(map[string]int64),
}

// GetDefaultEventCollector returns the global singleton EventCollector.
func GetDefaultEventCollector() *EventCollector {
	return defaultCollector
}

// CollectSystemEvents scans system logs, kernel buffers, and systemd status for critical security and health events.
func (c *EventCollector) CollectSystemEvents(ctx context.Context) ([]protocol.SystemEvent, error) {
	if runtime.GOOS != "linux" {
		return nil, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Prune seen signatures older than 24 hours
	nowSec := time.Now().Unix()
	cutoff := nowSec - 86400
	for sig, ts := range c.seenSignatures {
		if ts < cutoff {
			delete(c.seenSignatures, sig)
		}
	}

	events := make([]protocol.SystemEvent, 0)

	// 1. Scan kernel messages (dmesg / kmsg) for OOM killer and kernel panics
	kernelEvents := c.scanKernelEvents(ctx, nowSec)
	events = append(events, kernelEvents...)

	// 2. Scan auth log for SSH brute force failures
	authEvents := c.scanAuthLogs(ctx, nowSec)
	events = append(events, authEvents...)

	// 3. Scan systemd failed units
	systemdEvents := c.scanSystemdFailures(ctx, nowSec)
	events = append(events, systemdEvents...)

	return events, nil
}

func (c *EventCollector) isNewEvent(sig string, nowSec int64) bool {
	if _, seen := c.seenSignatures[sig]; seen {
		return false
	}
	c.seenSignatures[sig] = nowSec
	return true
}

func eventSig(category, title, source string) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s|%s|%s", category, title, source)
	return hex.EncodeToString(h.Sum(nil))[:24]
}

func (c *EventCollector) scanKernelEvents(ctx context.Context, nowSec int64) []protocol.SystemEvent {
	var events []protocol.SystemEvent

	// Try reading dmesg with timeout
	cmdCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "dmesg", "-T")
	output, err := cmd.Output()
	var lines []string
	if err == nil {
		lines = strings.Split(string(output), "\n")
		// Only inspect the last 200 lines of dmesg
		if len(lines) > 200 {
			lines = lines[len(lines)-200:]
		}
	} else {
		// Fallback: check /var/log/kern.log or /var/log/messages
		lines = readLogTail("/var/log/kern.log", 150)
		if len(lines) == 0 {
			lines = readLogTail("/var/log/messages", 150)
		}
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Check for OOM
		if oomRegex.MatchString(line) {
			matches := oomRegex.FindStringSubmatch(line)
			title := "Kernel OOM Killer invoked"
			if len(matches) >= 3 && matches[1] != "" && matches[2] != "" {
				title = fmt.Sprintf("OOM Killer: killed process %s (%s)", matches[1], matches[2])
			} else if len(matches) >= 5 && matches[3] != "" && matches[4] != "" {
				title = fmt.Sprintf("OOM Killer: killed process %s (%s)", matches[3], matches[4])
			}

			sig := eventSig("oom", title+line, "kernel")
			if c.isNewEvent(sig, nowSec) {
				events = append(events, protocol.SystemEvent{
					ID:         "evt-" + sig[:12],
					Category:   "oom",
					Severity:   "critical",
					Title:      title,
					Message:    line,
					Source:     "kernel",
					OccurredAt: nowSec,
				})
			}
			continue
		}

		// Check for Segfault / Kernel Panic
		if kernelRegex.MatchString(line) {
			title := "Kernel segfault / protection fault detected"
			if strings.Contains(strings.ToLower(line), "panic") {
				title = "Kernel panic detected"
			} else if strings.Contains(strings.ToLower(line), "kernel bug") {
				title = "Kernel BUG assertion failure"
			}

			sig := eventSig("kernel", title+line, "kernel")
			if c.isNewEvent(sig, nowSec) {
				events = append(events, protocol.SystemEvent{
					ID:         "evt-" + sig[:12],
					Category:   "kernel",
					Severity:   "critical",
					Title:      title,
					Message:    line,
					Source:     "kernel",
					OccurredAt: nowSec,
				})
			}
		}
	}

	return events
}

func (c *EventCollector) scanAuthLogs(ctx context.Context, nowSec int64) []protocol.SystemEvent {
	var events []protocol.SystemEvent

	// Check /var/log/auth.log (Debian/Ubuntu) or /var/log/secure (RHEL/CentOS)
	lines := readLogTail("/var/log/auth.log", 100)
	if len(lines) == 0 {
		lines = readLogTail("/var/log/secure", 100)
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if matches := sshFailRegex.FindStringSubmatch(line); len(matches) >= 4 {
			user := matches[1]
			ip := matches[2]
			port := matches[3]
			title := fmt.Sprintf("SSH authentication failed for user '%s' from %s:%s", user, ip, port)
			sig := eventSig("ssh_auth", title, "sshd")
			if c.isNewEvent(sig, nowSec) {
				events = append(events, protocol.SystemEvent{
					ID:         "evt-" + sig[:12],
					Category:   "ssh_auth",
					Severity:   "warning",
					Title:      title,
					Message:    line,
					Source:     "sshd",
					OccurredAt: nowSec,
				})
			}
		}
	}

	return events
}

func (c *EventCollector) scanSystemdFailures(ctx context.Context, nowSec int64) []protocol.SystemEvent {
	var events []protocol.SystemEvent

	cmdCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "systemctl", "--failed", "--no-legend", "--no-pager")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 4 && strings.HasSuffix(fields[0], ".service") {
			unitName := fields[0]
			title := fmt.Sprintf("Systemd service failed: %s", unitName)
			sig := eventSig("service", title, "systemd")
			if c.isNewEvent(sig, nowSec) {
				events = append(events, protocol.SystemEvent{
					ID:         "evt-" + sig[:12],
					Category:   "service",
					Severity:   "warning",
					Title:      title,
					Message:    line,
					Source:     "systemd",
					OccurredAt: nowSec,
				})
			}
		}
	}

	return events
}

func readLogTail(path string, maxLines int) []string {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > maxLines*2 {
			lines = lines[len(lines)-maxLines:]
		}
	}

	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return lines
}
