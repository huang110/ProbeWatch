package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseLinuxDiskstats(t *testing.T) {
	tmpDir := t.TempDir()
	diskstatsPath := filepath.Join(tmpDir, "diskstats")
	content := `
   7       0 loop0 11 0 28 0 0 0 0 0 0 0 0 0 0 0 0 0 0
   7       1 loop1 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0
   8       0 sda 1000 50 20000 500 2000 100 40000 1000 0 600 1500 0 0 0 0 0 0
   8       1 sda1 500 25 10000 250 1000 50 20000 500 0 300 750 0 0 0 0 0 0
 259       0 nvme0n1 3000 100 60000 800 4000 200 80000 1200 0 900 2000 0 0 0 0 0 0
 259       1 nvme0n1p1 1500 50 30000 400 2000 100 40000 600 0 450 1000 0 0 0 0 0 0
`
	if err := os.WriteFile(diskstatsPath, []byte(content), 0644); err != nil {
		t.Fatalf("write diskstats: %v", err)
	}

	disks, err := parseLinuxDiskstats(diskstatsPath)
	if err != nil {
		t.Fatalf("parseLinuxDiskstats err: %v", err)
	}

	// Should include sda and nvme0n1, but filter out loop0, loop1, sda1, nvme0n1p1
	if _, ok := disks["loop0"]; ok {
		t.Errorf("loop0 should be filtered out")
	}
	if _, ok := disks["sda1"]; ok {
		t.Errorf("sda1 partition should be filtered out when sda exists")
	}
	if _, ok := disks["nvme0n1p1"]; ok {
		t.Errorf("nvme0n1p1 partition should be filtered out when nvme0n1 exists")
	}
	if sda, ok := disks["sda"]; !ok {
		t.Errorf("sda should be present")
	} else {
		if sda.readsCompleted != 1000 || sda.sectorsRead != 20000 {
			t.Errorf("sda metrics mismatch: %+v", sda)
		}
	}
	if nvme, ok := disks["nvme0n1"]; !ok {
		t.Errorf("nvme0n1 should be present")
	} else {
		if nvme.writesCompleted != 4000 || nvme.sectorsWritten != 80000 {
			t.Errorf("nvme0n1 metrics mismatch: %+v", nvme)
		}
	}
}

func TestDiskIOTrackerSampleRate(t *testing.T) {
	callCount := 0
	mockReader := func() (map[string]rawDiskMetrics, error) {
		callCount++
		if callCount == 1 {
			return map[string]rawDiskMetrics{
				"vda": {
					readsCompleted:  1000,
					sectorsRead:     2048, // 1 MB
					writesCompleted: 500,
					sectorsWritten:  4096, // 2 MB
					ioTimeMS:        100,
				},
			}, nil
		}
		// 1 second later: 100 new reads, 2048 new sectors (1MB), 50 new writes, 2048 new sectors (1MB)
		return map[string]rawDiskMetrics{
			"vda": {
				readsCompleted:  1100,
				sectorsRead:     4096, // +2048 sectors = +1,048,576 bytes
				writesCompleted: 550,
				sectorsWritten:  6144, // +2048 sectors = +1,048,576 bytes
				ioTimeMS:        250,  // +150 ms
			},
		}, nil
	}

	tracker := newDiskIOTracker(mockReader)

	// First sample primes baseline counters
	first := tracker.Sample()
	if len(first) != 1 || first[0].Device != "vda" {
		t.Fatalf("first sample = %+v", first)
	}
	if first[0].ReadBytesPerSec != 0 {
		t.Errorf("first sample read rate should be 0, got %d", first[0].ReadBytesPerSec)
	}

	// Wait 250ms so dt is valid
	time.Sleep(250 * time.Millisecond)

	second := tracker.Sample()
	if len(second) != 1 || second[0].Device != "vda" {
		t.Fatalf("second sample = %+v", second)
	}
	stat := second[0]
	if stat.ReadBytesPerSec == 0 || stat.WriteBytesPerSec == 0 {
		t.Errorf("expected non-zero rates, got read=%d, write=%d", stat.ReadBytesPerSec, stat.WriteBytesPerSec)
	}
	if stat.ReadIOPS <= 0 || stat.WriteIOPS <= 0 {
		t.Errorf("expected non-zero IOPS, got read=%v, write=%v", stat.ReadIOPS, stat.WriteIOPS)
	}
	if stat.UtilPercent <= 0 || stat.UtilPercent > 100 {
		t.Errorf("util_percent invalid: %v", stat.UtilPercent)
	}
}
