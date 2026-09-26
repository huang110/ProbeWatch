//go:build linux

package agent

import (
	"bufio"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var (
	nvmePartitionRe  = regexp.MustCompile(`^(nvme\d+n\d+)p\d+$`)
	trailingDigitsRe = regexp.MustCompile(`^(.*[a-zA-Z])\d+$`)
)

func newPlatformDiskTracker() DiskIOTracker {
	return newDiskIOTracker(readLinuxDiskstats)
}

func readLinuxDiskstats() (map[string]rawDiskMetrics, error) {
	return parseLinuxDiskstats("/proc/diskstats")
}

func parseLinuxDiskstats(path string) (map[string]rawDiskMetrics, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	all := make(map[string]rawDiskMetrics)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 14 {
			continue
		}

		dev := fields[2]
		if strings.HasPrefix(dev, "loop") ||
			strings.HasPrefix(dev, "ram") ||
			strings.HasPrefix(dev, "sr") ||
			strings.HasPrefix(dev, "fd") ||
			strings.HasPrefix(dev, "dm-") {
			continue
		}

		reads, _ := strconv.ParseUint(fields[3], 10, 64)
		readSectors, _ := strconv.ParseUint(fields[5], 10, 64)
		writes, _ := strconv.ParseUint(fields[7], 10, 64)
		writeSectors, _ := strconv.ParseUint(fields[9], 10, 64)
		ioMs, _ := strconv.ParseUint(fields[12], 10, 64)

		all[dev] = rawDiskMetrics{
			readsCompleted:  reads,
			sectorsRead:     readSectors,
			writesCompleted: writes,
			sectorsWritten:  writeSectors,
			ioTimeMS:        ioMs,
		}
	}

	// Filter out partitions if parent whole disk is monitored
	filtered := make(map[string]rawDiskMetrics)
	for dev, metrics := range all {
		if sub := nvmePartitionRe.FindStringSubmatch(dev); len(sub) == 2 {
			parent := sub[1]
			if _, hasParent := all[parent]; hasParent {
				continue
			}
		} else if sub := trailingDigitsRe.FindStringSubmatch(dev); len(sub) == 2 {
			parent := sub[1]
			if _, hasParent := all[parent]; hasParent {
				continue
			}
		}
		filtered[dev] = metrics
	}

	return filtered, scanner.Err()
}
