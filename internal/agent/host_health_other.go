//go:build !linux

package agent

import (
	"github.com/probewatch/probewatch/internal/protocol"
)

type otherHostHealthCollector struct{}

func newPlatformHostHealthCollector() HostHealthCollector {
	return &otherHostHealthCollector{}
}

func (c *otherHostHealthCollector) Collect(snapshot *protocol.ResourceSnapshot) *protocol.HostHealthInfo {
	score, status, deductions := computeHealthScore(snapshot, false, 0, nil)
	return &protocol.HostHealthInfo{
		HealthScore:      score,
		HealthStatus:     status,
		HealthDeductions: deductions,
	}
}
