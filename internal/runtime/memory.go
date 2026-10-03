package runtime

import (
	"log/slog"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
)

// ConfigureResourceLimits applies optional Go runtime limits without making
// deployment depend on a particular init system. GOMEMLIMIT and GOGC are
// environment driven so small VPSes can tune memory use without code changes.
func ConfigureResourceLimits(component string) {
	if raw := strings.TrimSpace(os.Getenv("GOMEMLIMIT")); raw != "" {
		if limit, ok := parseMemoryLimit(raw); ok {
			previous := debug.SetMemoryLimit(limit)
			slog.Info("Go memory limit configured", "component", component, "limit", raw, "previous_bytes", previous)
		} else {
			slog.Warn("invalid GOMEMLIMIT ignored", "component", component, "value", raw)
		}
	}
	if raw := strings.TrimSpace(os.Getenv("GOGC")); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value >= 0 {
			previous := debug.SetGCPercent(value)
			slog.Info("Go GC target configured", "component", component, "gogc", value, "previous", previous)
		} else {
			slog.Warn("invalid GOGC ignored", "component", component, "value", raw)
		}
	}
}

func parseMemoryLimit(raw string) (int64, bool) {
	s := strings.TrimSpace(strings.ToUpper(raw))
	if s == "" { return 0, false }
	multiplier := int64(1)
	for _, suffix := range []struct { name string; factor int64 }{
		{"KIB", 1 << 10}, {"KB", 1 << 10}, {"MIB", 1 << 20}, {"MB", 1 << 20}, {"GIB", 1 << 30}, {"GB", 1 << 30},
	} {
		if strings.HasSuffix(s, suffix.name) {
			s = strings.TrimSpace(strings.TrimSuffix(s, suffix.name))
			multiplier = suffix.factor
			break
		}
	}
	value, err := strconv.ParseInt(s, 10, 64)
	if err != nil || value <= 0 || value > (1<<62)/multiplier { return 0, false }
	return value * multiplier, true
}
