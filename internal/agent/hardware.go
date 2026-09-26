package agent

import (
	"math"
	"runtime"
	"strings"

	"github.com/probewatch/probewatch/internal/protocol"
)

// HardwareCollector gathers host CPU specs, thermal sensors, and multi-mount storage statistics.
type HardwareCollector interface {
	CollectCPU() (model string, mhz float64, cores int)
	CollectSensors() ([]protocol.SensorInfo, float64)
	CollectMounts() ([]protocol.MountStat, error)
}

// defaultHardwareCollector returns the platform-specific hardware collector.
func defaultHardwareCollector() HardwareCollector {
	return newPlatformHardwareCollector()
}

// sanitizeSensorName ensures thermal sensor names are clean and bounded.
func sanitizeSensorName(name string) string {
	name = strings.TrimSpace(name)
	if len(name) > 64 {
		name = name[:64]
	}
	return name
}

// roundOneDecimal rounds a float64 to one decimal point.
func roundOneDecimal(val float64) float64 {
	return math.Round(val*10) / 10
}

// roundTwoDecimals rounds a float64 to two decimal points.
func roundTwoDecimals(val float64) float64 {
	return math.Round(val*100) / 100
}

// fallbackCPUInfo provides cross-platform base CPU information.
func fallbackCPUInfo() (string, float64, int) {
	cores := runtime.NumCPU()
	if cores < 1 {
		cores = 1
	}
	return runtime.GOARCH, 0, cores
}
