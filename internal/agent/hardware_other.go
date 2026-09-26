//go:build !linux

package agent

import "github.com/probewatch/probewatch/internal/protocol"

type nonLinuxHardwareCollector struct{}

func newPlatformHardwareCollector() HardwareCollector {
	return &nonLinuxHardwareCollector{}
}

func (c *nonLinuxHardwareCollector) CollectCPU() (string, float64, int) {
	return fallbackCPUInfo()
}

func (c *nonLinuxHardwareCollector) CollectSensors() ([]protocol.SensorInfo, float64) {
	return nil, 0
}

func (c *nonLinuxHardwareCollector) CollectMounts() ([]protocol.MountStat, error) {
	return nil, nil
}
