//go:build linux

package agent

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/probewatch/probewatch/internal/protocol"
)

type linuxHostHealthCollector struct {
	rebootFilePath  string
	updatesFilePath string
	systemctlCmd    string

	mu                   sync.Mutex
	cachedTotalUp        int
	cachedSecUp          int
	lastUpdateScan       time.Time
	cachedFailedServices []string
	lastFailedScan       time.Time
	cacheTTL             time.Duration
}

func newPlatformHostHealthCollector() HostHealthCollector {
	return &linuxHostHealthCollector{
		rebootFilePath:  "/var/run/reboot-required",
		updatesFilePath: "/var/lib/update-notifier/updates-available",
		systemctlCmd:    "systemctl",
		cacheTTL:        120 * time.Second,
	}
}

func (c *linuxHostHealthCollector) Collect(snapshot *protocol.ResourceSnapshot) *protocol.HostHealthInfo {
	reboot := checkRebootRequired(c.rebootFilePath)

	c.mu.Lock()
	now := time.Now()
	ttl := c.cacheTTL
	if ttl <= 0 {
		ttl = 120 * time.Second
	}

	if c.lastUpdateScan.IsZero() || now.Sub(c.lastUpdateScan) >= ttl {
		c.cachedTotalUp, c.cachedSecUp = checkUpdatesAvailable(c.updatesFilePath)
		c.lastUpdateScan = now
	}
	totalUp, secUp := c.cachedTotalUp, c.cachedSecUp

	if c.lastFailedScan.IsZero() || now.Sub(c.lastFailedScan) >= ttl {
		c.cachedFailedServices = checkFailedServices(c.systemctlCmd)
		c.lastFailedScan = now
	}
	failed := append([]string(nil), c.cachedFailedServices...)
	c.mu.Unlock()

	score, status, deductions := computeHealthScore(snapshot, reboot, secUp, failed)

	return &protocol.HostHealthInfo{
		HealthScore:      score,
		HealthStatus:     status,
		RebootRequired:   reboot,
		SecurityUpdates:  secUp,
		TotalUpdates:     totalUp,
		FailedServices:   failed,
		HealthDeductions: deductions,
	}
}

func checkRebootRequired(path string) bool {
	if path == "" {
		path = "/var/run/reboot-required"
	}
	_, err := os.Stat(path)
	return err == nil
}

var (
	totalUpdatesRegex = regexp.MustCompile(`(\d+)\s+updates?\s+(?:can\s+be|available)`)
	secUpdatesRegex   = regexp.MustCompile(`(\d+)\s+(?:additional\s+)?security\s+updates?`)
)

func checkUpdatesAvailable(path string) (int, int) {
	if path == "" {
		path = "/var/lib/update-notifier/updates-available"
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer f.Close()

	var totalUp, secUp int
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if m := totalUpdatesRegex.FindStringSubmatch(line); len(m) > 1 {
			if val, err := strconv.Atoi(m[1]); err == nil {
				totalUp = val
			}
		}
		if m := secUpdatesRegex.FindStringSubmatch(line); len(m) > 1 {
			if val, err := strconv.Atoi(m[1]); err == nil {
				secUp = val
			}
		}
	}

	return totalUp, secUp
}

func checkFailedServices(systemctlCmd string) []string {
	if systemctlCmd == "" {
		systemctlCmd = "systemctl"
	}
	binPath, err := exec.LookPath(systemctlCmd)
	if err != nil || binPath == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath, "--failed", "--no-legend", "--plain")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	var failed []string
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) > 0 {
			unit := fields[0]
			if strings.HasSuffix(unit, ".service") || strings.HasSuffix(unit, ".timer") || strings.HasSuffix(unit, ".socket") {
				failed = append(failed, unit)
				if len(failed) >= 16 {
					break
				}
			}
		}
	}

	return failed
}
