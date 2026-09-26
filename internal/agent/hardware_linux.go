//go:build linux

package agent

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/probewatch/probewatch/internal/protocol"
)

type linuxHardwareCollector struct {
	cpuinfoPath string
	sysfsRoot   string
	mountsPath  string
}

func newPlatformHardwareCollector() HardwareCollector {
	return &linuxHardwareCollector{
		cpuinfoPath: "/proc/cpuinfo",
		sysfsRoot:   "/sys",
		mountsPath:  "/proc/mounts",
	}
}

// CollectCPU parses /proc/cpuinfo to extract model name, current MHz, and core count.
func (c *linuxHardwareCollector) CollectCPU() (string, float64, int) {
	return parseLinuxCPUInfo(c.cpuinfoPath)
}

func parseLinuxCPUInfo(path string) (string, float64, int) {
	f, err := os.Open(path)
	if err != nil {
		return fallbackCPUInfo()
	}
	defer f.Close()

	var (
		model string
		mhz   float64
		cores int
	)

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) < 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch key {
		case "model name", "Hardware", "Processor", "cpu model":
			if model == "" && val != "" {
				model = val
			}
		case "cpu MHz":
			if mhz == 0 {
				if parsed, err := strconv.ParseFloat(val, 64); err == nil {
					mhz = roundOneDecimal(parsed)
				}
			}
		case "processor":
			cores++
		}
	}

	if cores < 1 {
		_, _, cores = fallbackCPUInfo()
	}
	if model == "" {
		model, _, _ = fallbackCPUInfo()
	}
	return model, mhz, cores
}

// CollectSensors discovers and reads thermal zones and hwmon temperature sensors.
func (c *linuxHardwareCollector) CollectSensors() ([]protocol.SensorInfo, float64) {
	return parseLinuxSensors(c.sysfsRoot)
}

func parseLinuxSensors(sysfsRoot string) ([]protocol.SensorInfo, float64) {
	var sensors []protocol.SensorInfo
	seen := make(map[string]bool)

	// 1. Scan /sys/class/thermal/thermal_zone*
	zones, _ := filepath.Glob(filepath.Join(sysfsRoot, "class/thermal/thermal_zone*"))
	for _, zone := range zones {
		typeBytes, err := os.ReadFile(filepath.Join(zone, "type"))
		if err != nil {
			continue
		}
		typeName := strings.TrimSpace(string(typeBytes))
		tempBytes, err := os.ReadFile(filepath.Join(zone, "temp"))
		if err != nil {
			continue
		}
		milli, err := strconv.ParseInt(strings.TrimSpace(string(tempBytes)), 10, 64)
		if err != nil || milli <= -50000 || milli > 200000 {
			continue
		}
		tempC := roundOneDecimal(float64(milli) / 1000.0)

		var critC float64
		// Check for critical trip point
		tripFiles, _ := filepath.Glob(filepath.Join(zone, "trip_point_*_type"))
		for _, tf := range tripFiles {
			ttBytes, err := os.ReadFile(tf)
			if err == nil && strings.TrimSpace(string(ttBytes)) == "critical" {
				base := strings.TrimSuffix(tf, "_type")
				if ctBytes, err := os.ReadFile(base + "_temp"); err == nil {
					if cMilli, err := strconv.ParseInt(strings.TrimSpace(string(ctBytes)), 10, 64); err == nil && cMilli > 0 {
						critC = roundOneDecimal(float64(cMilli) / 1000.0)
						break
					}
				}
			}
		}

		sensorName := filepath.Base(zone)
		if typeName != "" {
			sensorName = typeName
		}
		sensorName = sanitizeSensorName(sensorName)
		if seen[sensorName] {
			continue
		}
		seen[sensorName] = true

		sensorType := "thermal_zone"
		lowerType := strings.ToLower(sensorName)
		if strings.Contains(lowerType, "cpu") || strings.Contains(lowerType, "pkg") || strings.Contains(lowerType, "core") || strings.Contains(lowerType, "x86_pkg") {
			sensorType = "cpu"
		}

		sensors = append(sensors, protocol.SensorInfo{
			Name:      sensorName,
			Type:      sensorType,
			TempC:     tempC,
			CriticalC: critC,
		})
	}

	// 2. Scan /sys/class/hwmon/hwmon*
	hwmons, _ := filepath.Glob(filepath.Join(sysfsRoot, "class/hwmon/hwmon*"))
	for _, hwmon := range hwmons {
		hwNameBytes, _ := os.ReadFile(filepath.Join(hwmon, "name"))
		driverName := strings.TrimSpace(string(hwNameBytes))
		if driverName == "" {
			driverName = filepath.Base(hwmon)
		}

		tempInputs, _ := filepath.Glob(filepath.Join(hwmon, "temp*_input"))
		for _, inputPath := range tempInputs {
			tempBytes, err := os.ReadFile(inputPath)
			if err != nil {
				continue
			}
			milli, err := strconv.ParseInt(strings.TrimSpace(string(tempBytes)), 10, 64)
			if err != nil || milli <= -50000 || milli > 200000 {
				continue
			}
			tempC := roundOneDecimal(float64(milli) / 1000.0)

			base := strings.TrimSuffix(inputPath, "_input")
			var label string
			if lBytes, err := os.ReadFile(base + "_label"); err == nil {
				label = strings.TrimSpace(string(lBytes))
			}
			if label == "" {
				label = filepath.Base(base)
			}

			fullName := sanitizeSensorName(driverName + " " + label)
			if seen[fullName] {
				continue
			}
			seen[fullName] = true

			var critC float64
			if cBytes, err := os.ReadFile(base + "_crit"); err == nil {
				if cMilli, err := strconv.ParseInt(strings.TrimSpace(string(cBytes)), 10, 64); err == nil && cMilli > 0 {
					critC = roundOneDecimal(float64(cMilli) / 1000.0)
				}
			}

			sensorType := "hwmon"
			lowerDriver := strings.ToLower(driverName)
			lowerLabel := strings.ToLower(label)
			if strings.Contains(lowerDriver, "coretemp") || strings.Contains(lowerDriver, "k10temp") || strings.Contains(lowerDriver, "zenpower") || strings.Contains(lowerLabel, "cpu") || strings.Contains(lowerLabel, "package") || strings.Contains(lowerLabel, "tctl") {
				sensorType = "cpu"
			}

			sensors = append(sensors, protocol.SensorInfo{
				Name:      fullName,
				Type:      sensorType,
				TempC:     tempC,
				CriticalC: critC,
			})
		}
	}

	// 3. Determine best representative CPU temperature (package/die temp or highest CPU temp)
	var bestCPUTemp float64
	var foundPackage bool
	for _, s := range sensors {
		if s.TempC <= 0 {
			continue
		}
		lower := strings.ToLower(s.Name)
		if !foundPackage && (strings.Contains(lower, "package") || strings.Contains(lower, "pkg") || strings.Contains(lower, "tctl")) {
			bestCPUTemp = s.TempC
			foundPackage = true
			continue
		}
		if !foundPackage && s.Type == "cpu" {
			if s.TempC > bestCPUTemp {
				bestCPUTemp = s.TempC
			}
		}
	}
	if bestCPUTemp == 0 && len(sensors) > 0 {
		for _, s := range sensors {
			if s.TempC > bestCPUTemp {
				bestCPUTemp = s.TempC
			}
		}
	}

	return sensors, bestCPUTemp
}

// CollectMounts parses /proc/mounts and calls Statfs to measure space and inode utilization.
func (c *linuxHardwareCollector) CollectMounts() ([]protocol.MountStat, error) {
	return parseLinuxMounts(c.mountsPath)
}

func parseLinuxMounts(mountsPath string) ([]protocol.MountStat, error) {
	f, err := os.Open(mountsPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	ignoredFSTypes := map[string]bool{
		"proc":        true,
		"sysfs":       true,
		"devpts":      true,
		"cgroup":      true,
		"cgroup2":     true,
		"pstore":      true,
		"bpf":         true,
		"tracefs":     true,
		"securityfs":  true,
		"debugfs":     true,
		"autofs":      true,
		"mqueue":      true,
		"hugetlbfs":   true,
		"fusectl":     true,
		"configfs":    true,
		"ramfs":       true,
		"devtmpfs":    true,
		"efivarfs":    true,
		"none":        true,
		"nsfs":        true,
		"fuse.lxcfs":  true,
		"binfmt_misc": true,
	}

	var results []protocol.MountStat
	seenMounts := make(map[string]bool)

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		device := fields[0]
		mountPoint := fields[1]
		fsType := fields[2]

		if ignoredFSTypes[fsType] {
			continue
		}
		// Skip temporary container/systemd mounts unless they are real roots
		if strings.HasPrefix(mountPoint, "/run/credentials") || strings.HasPrefix(mountPoint, "/sys") || strings.HasPrefix(mountPoint, "/proc") || strings.HasPrefix(mountPoint, "/dev") {
			continue
		}
		if fsType == "tmpfs" && mountPoint != "/" && mountPoint != "/tmp" && mountPoint != "/run" {
			continue
		}
		if seenMounts[mountPoint] {
			continue
		}

		var stat syscall.Statfs_t
		if err := syscall.Statfs(mountPoint, &stat); err != nil {
			continue
		}

		blockSize := uint64(stat.Bsize)
		if blockSize == 0 {
			blockSize = 4096
		}
		totalBytes := stat.Blocks * blockSize
		if totalBytes == 0 {
			continue
		}
		availableBytes := stat.Bavail * blockSize
		usedBytes := uint64(0)
		if totalBytes > availableBytes {
			usedBytes = totalBytes - availableBytes
		}
		usedPercent := roundOneDecimal((float64(usedBytes) / float64(totalBytes)) * 100.0)

		inodesTotal := stat.Files
		inodesFree := stat.Ffree
		inodesUsed := uint64(0)
		if inodesTotal > inodesFree {
			inodesUsed = inodesTotal - inodesFree
		}
		var inodesPercent float64
		if inodesTotal > 0 {
			inodesPercent = roundOneDecimal((float64(inodesUsed) / float64(inodesTotal)) * 100.0)
		}

		seenMounts[mountPoint] = true
		results = append(results, protocol.MountStat{
			MountPoint:    mountPoint,
			Device:        device,
			FSType:        fsType,
			TotalBytes:    totalBytes,
			UsedBytes:     usedBytes,
			FreeBytes:     availableBytes,
			UsedPercent:   usedPercent,
			InodesTotal:   inodesTotal,
			InodesUsed:    inodesUsed,
			InodesFree:    inodesFree,
			InodesPercent: inodesPercent,
		})
	}

	return results, scanner.Err()
}
