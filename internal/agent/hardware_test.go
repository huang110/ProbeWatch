package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseLinuxCPUInfo(t *testing.T) {
	tmpDir := t.TempDir()
	cpuinfoPath := filepath.Join(tmpDir, "cpuinfo")
	content := `
processor	: 0
vendor_id	: GenuineIntel
cpu family	: 6
model		: 63
model name	: Intel(R) Xeon(R) CPU E5-2680 v3 @ 2.50GHz
stepping	: 2
microcode	: 0x1
cpu MHz		: 2497.106
cache size	: 30720 KB
physical id	: 0
siblings	: 2
core id		: 0
cpu cores	: 2

processor	: 1
vendor_id	: GenuineIntel
cpu family	: 6
model		: 63
model name	: Intel(R) Xeon(R) CPU E5-2680 v3 @ 2.50GHz
stepping	: 2
microcode	: 0x1
cpu MHz		: 2497.106
cache size	: 30720 KB
`
	if err := os.WriteFile(cpuinfoPath, []byte(content), 0644); err != nil {
		t.Fatalf("write cpuinfo: %v", err)
	}

	model, mhz, cores := parseLinuxCPUInfo(cpuinfoPath)
	if model != "Intel(R) Xeon(R) CPU E5-2680 v3 @ 2.50GHz" {
		t.Errorf("model = %q, want Intel(R) Xeon...", model)
	}
	if mhz != 2497.1 {
		t.Errorf("mhz = %v, want 2497.1", mhz)
	}
	if cores != 2 {
		t.Errorf("cores = %d, want 2", cores)
	}
}

func TestParseLinuxSensors(t *testing.T) {
	tmpDir := t.TempDir()
	// Mock /sys/class/thermal/thermal_zone0
	tz0 := filepath.Join(tmpDir, "class/thermal/thermal_zone0")
	if err := os.MkdirAll(tz0, 0755); err != nil {
		t.Fatalf("mkdir tz0: %v", err)
	}
	_ = os.WriteFile(filepath.Join(tz0, "type"), []byte("x86_pkg_temp\n"), 0644)
	_ = os.WriteFile(filepath.Join(tz0, "temp"), []byte("48500\n"), 0644)
	_ = os.WriteFile(filepath.Join(tz0, "trip_point_0_type"), []byte("critical\n"), 0644)
	_ = os.WriteFile(filepath.Join(tz0, "trip_point_0_temp"), []byte("100000\n"), 0644)

	// Mock /sys/class/hwmon/hwmon0
	hw0 := filepath.Join(tmpDir, "class/hwmon/hwmon0")
	if err := os.MkdirAll(hw0, 0755); err != nil {
		t.Fatalf("mkdir hw0: %v", err)
	}
	_ = os.WriteFile(filepath.Join(hw0, "name"), []byte("coretemp\n"), 0644)
	_ = os.WriteFile(filepath.Join(hw0, "temp1_input"), []byte("49000\n"), 0644)
	_ = os.WriteFile(filepath.Join(hw0, "temp1_label"), []byte("Package id 0\n"), 0644)
	_ = os.WriteFile(filepath.Join(hw0, "temp1_crit"), []byte("100000\n"), 0644)

	sensors, bestTemp := parseLinuxSensors(tmpDir)
	if len(sensors) == 0 {
		t.Fatalf("expected sensors, got 0")
	}
	if bestTemp < 48.0 || bestTemp > 50.0 {
		t.Errorf("bestTemp = %v, want ~48.5 or ~49.0", bestTemp)
	}
	hasPkg := false
	for _, s := range sensors {
		if s.Type == "cpu" && s.TempC > 0 {
			hasPkg = true
		}
	}
	if !hasPkg {
		t.Errorf("expected at least one cpu type sensor")
	}
}
