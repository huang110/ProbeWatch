package version

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
)

const (
	ServerVersion   = "0.8.5"
	AgentVersion    = "0.8.5"
	MinAgentVersion = "0.5.0"
)

// FullAgentVersionString returns a formatted version string with OS and architecture.
func FullAgentVersionString() string {
	return fmt.Sprintf("ProbeWatch Agent v%s (%s/%s)", AgentVersion, runtime.GOOS, runtime.GOARCH)
}

// Compare compares two semantic version strings (e.g. "0.5.6" vs "0.5.5").
// Returns:
//   1 if v1 > v2
//  -1 if v1 < v2
//   0 if v1 == v2
func Compare(v1, v2 string) int {
	v1 = strings.TrimPrefix(strings.TrimSpace(v1), "v")
	v2 = strings.TrimPrefix(strings.TrimSpace(v2), "v")
	p1 := strings.Split(v1, ".")
	p2 := strings.Split(v2, ".")
	maxLen := len(p1)
	if len(p2) > maxLen {
		maxLen = len(p2)
	}
	for i := 0; i < maxLen; i++ {
		var n1, n2 int
		if i < len(p1) {
			n1, _ = strconv.Atoi(p1[i])
		}
		if i < len(p2) {
			n2, _ = strconv.Atoi(p2[i])
		}
		if n1 > n2 {
			return 1
		}
		if n1 < n2 {
			return -1
		}
	}
	return 0
}

// IsUpgradeAvailable returns true if latest is strictly newer than current.
func IsUpgradeAvailable(current, latest string) bool {
	return Compare(latest, current) > 0
}
