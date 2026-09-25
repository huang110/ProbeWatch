package version

import (
	"testing"
)

func TestVersionCompare(t *testing.T) {
	tests := []struct {
		v1, v2 string
		want   int
	}{
		{"0.5.6", "0.5.5", 1},
		{"v0.5.6", "0.5.6", 0},
		{"0.2.0", "0.5.6", -1},
		{"1.0.0", "0.9.9", 1},
		{"0.5.6.1", "0.5.6", 1},
		{"0.5.6", "0.5.6.0", 0},
	}
	for _, tc := range tests {
		got := Compare(tc.v1, tc.v2)
		if got != tc.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", tc.v1, tc.v2, got, tc.want)
		}
	}
}

func TestIsUpgradeAvailable(t *testing.T) {
	if !IsUpgradeAvailable("0.2.0", "0.5.6") {
		t.Fatal("expected upgrade available from 0.2.0 to 0.5.6")
	}
	if IsUpgradeAvailable("0.5.6", "0.5.6") {
		t.Fatal("expected no upgrade when versions match")
	}
	if IsUpgradeAvailable("0.5.6", "0.5.5") {
		t.Fatal("expected no upgrade when latest is older")
	}
}
