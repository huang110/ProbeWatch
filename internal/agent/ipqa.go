package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/probewatch/probewatch/internal/protocol"
)

const (
	defaultIPQADir          = ".ipqa"
	defaultIPQACacheTTL     = 6 * time.Hour
	defaultIPQAMaxLogBytes  = 1024 * 1024       // 1MB
	maxIPQALogLines         = 500
	maxIPQALineBytes        = 512
	maxIPQAArchiveFiles     = 50
	maxIPQAJSONBytes        = 512 * 1024        // 512KB per archive file
	maxIPQAChanges          = 50
	ipqaReadTimeout         = 3 * time.Second
)

var (
	archiveFilenameRegex = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})_(\d{6})\.json$`)
	beijingLocation      = time.FixedZone("CST", 8*3600)
)

// IPQACollector collects local IP-Quality-Archive telemetry without blocking.
type IPQACollector interface {
	Get() *protocol.IPQAInfo
}

// DefaultIPQACollector reads IPQA files locally and thread-safely.
type DefaultIPQACollector struct {
	enabled               bool
	baseDir               string
	cacheTTL              time.Duration
	maxLogBytes           int64
	ignoreInitialArchive  bool

	mu         sync.RWMutex
	cached     *protocol.IPQAInfo
	lastFetch  time.Time
	fetching   bool

	// customReader allows mocking in tests
	customReader func(ctx context.Context, baseDir string) (*protocol.IPQAInfo, error)
}

// NewIPQACollector constructs a new IPQACollector with environment variables.
func NewIPQACollector() *DefaultIPQACollector {
	enabled := false
	if raw := strings.TrimSpace(os.Getenv("PROBEWATCH_IPQA_ENABLED")); raw != "" {
		enabled = parseBool(raw)
	}

	dir := strings.TrimSpace(os.Getenv("PROBEWATCH_IPQA_DIR"))
	if dir == "" {
		home, err := os.UserHomeDir()
		if err == nil && home != "" {
			dir = filepath.Join(home, defaultIPQADir)
		} else {
			dir = filepath.Join("/root", defaultIPQADir)
		}
	}

	ttl := defaultIPQACacheTTL
	if rawTTL := strings.TrimSpace(os.Getenv("PROBEWATCH_IPQA_CACHE_TTL")); rawTTL != "" {
		if parsed, err := time.ParseDuration(rawTTL); err == nil && parsed > 0 {
			ttl = parsed
		} else if sec, err := strconv.Atoi(rawTTL); err == nil && sec > 0 {
			ttl = time.Duration(sec) * time.Second
		}
	}

	maxBytes := int64(defaultIPQAMaxLogBytes)
	if rawMax := strings.TrimSpace(os.Getenv("PROBEWATCH_IPQA_MAX_LOG_BYTES")); rawMax != "" {
		if val, err := strconv.ParseInt(rawMax, 10, 64); err == nil && val > 0 {
			maxBytes = val
		}
	}

	ignoreInitial := true
	if rawInit := strings.TrimSpace(os.Getenv("PROBEWATCH_IPQA_IGNORE_INITIAL")); rawInit != "" {
		ignoreInitial = parseBool(rawInit)
	}

	return &DefaultIPQACollector{
		enabled:              enabled,
		baseDir:              dir,
		cacheTTL:             ttl,
		maxLogBytes:          maxBytes,
		ignoreInitialArchive: ignoreInitial,
	}
}

// Get returns the latest IPQAInfo. If disabled, returns a minimal disabled struct.
// Does not block if a refresh is in progress.
func (c *DefaultIPQACollector) Get() *protocol.IPQAInfo {
	if c == nil {
		return nil
	}
	if !c.enabled {
		return &protocol.IPQAInfo{
			Enabled:   false,
			Installed: false,
		}
	}

	c.mu.RLock()
	cached := c.cached
	lastFetch := c.lastFetch
	fetching := c.fetching
	ttl := c.cacheTTL
	c.mu.RUnlock()

	now := time.Now()
	needsFetch := false
	if cached == nil {
		needsFetch = true
	} else if now.Sub(lastFetch) >= ttl {
		needsFetch = true
	}

	if needsFetch && !fetching {
		c.mu.Lock()
		if !c.fetching {
			c.fetching = true
			go func() {
				defer func() {
					c.mu.Lock()
					c.fetching = false
					c.mu.Unlock()
				}()
				info, err := c.collect()
				c.mu.Lock()
				if err == nil && info != nil {
					c.cached = info
					c.lastFetch = time.Now()
				} else if c.cached != nil && err != nil {
					// Retain old cached data upon failure, recording sanitized error code
					c.cached.CollectionError = "collect_failed"
				}
				c.mu.Unlock()
			}()
		}
		c.mu.Unlock()
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.cached != nil {
		// Return copy to prevent caller modification
		copyInfo := *c.cached
		return &copyInfo
	}
	return nil
}

// collect synchronously performs safe local file reading with timeout bounds.
func (c *DefaultIPQACollector) collect() (*protocol.IPQAInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ipqaReadTimeout)
	defer cancel()

	if c.customReader != nil {
		return c.customReader(ctx, c.baseDir)
	}

	dataPath := filepath.Join(c.baseDir, "data")
	fi, err := os.Stat(dataPath)
	if err != nil || !fi.IsDir() {
		// Not installed or missing data dir
		return &protocol.IPQAInfo{
			Enabled:   c.enabled,
			Installed: false,
		}, nil
	}

	info := &protocol.IPQAInfo{
		Enabled:       c.enabled,
		Installed:     true,
		LastCheckedAt: time.Now().UTC().Unix(),
	}

	// 1. Parse alerts.log
	alertLogPath := filepath.Join(dataPath, "alerts.log")
	if logFi, err := os.Stat(alertLogPath); err == nil && !logFi.IsDir() {
		if logFi.Size() > c.maxLogBytes {
			info.CollectionError = "log_too_large"
		} else {
			parsedAlerts, parseErr := parseAlertsLogFile(alertLogPath, c.ignoreInitialArchive, c.maxLogBytes)
			if parseErr != nil {
				info.CollectionError = "log_parse_error"
			} else {
				info.AlertCount = parsedAlerts.TotalCount
				info.CriticalCount = parsedAlerts.CriticalCount
				info.WarningCount = parsedAlerts.WarningCount
				info.InfoCount = parsedAlerts.InfoCount
				info.HighestSeverity = parsedAlerts.HighestSeverity
			}
		}
	}

	// 2. Read latest IPv4 and IPv6 archives & detect semantic differences
	v4Dir := filepath.Join(dataPath, "v4")
	v6Dir := filepath.Join(dataPath, "v6")

	sourcesSet := make(map[string]struct{})
	var changes []protocol.IPQAChange

	v4Info, v4Date, v4Changes, v4Sources := processFamilyArchives(v4Dir, "IPv4")
	if v4Info != nil {
		info.IPv4 = v4Info
		for _, s := range v4Sources {
			sourcesSet[s] = struct{}{}
		}
		changes = append(changes, v4Changes...)
		if v4Date > info.LatestArchiveDate {
			info.LatestArchiveDate = v4Date
		}
	}

	v6Info, v6Date, v6Changes, v6Sources := processFamilyArchives(v6Dir, "IPv6")
	if v6Info != nil {
		info.IPv6 = v6Info
		for _, s := range v6Sources {
			sourcesSet[s] = struct{}{}
		}
		changes = append(changes, v6Changes...)
		if v6Date > info.LatestArchiveDate {
			info.LatestArchiveDate = v6Date
		}
	}

	if len(changes) > maxIPQAChanges {
		changes = changes[:maxIPQAChanges]
	}
	info.Changes = changes

	sources := make([]string, 0, len(sourcesSet))
	for s := range sourcesSet {
		sources = append(sources, s)
	}
	sort.Strings(sources)
	info.Sources = sources

	if info.HighestSeverity == "" {
		if info.CriticalCount > 0 {
			info.HighestSeverity = "CRITICAL"
		} else if info.WarningCount > 0 {
			info.HighestSeverity = "WARNING"
		} else if info.InfoCount > 0 {
			info.HighestSeverity = "INFO"
		} else {
			info.HighestSeverity = "NONE"
		}
	}

	return info, nil
}

type alertsSummary struct {
	TotalCount      int
	CriticalCount   int
	WarningCount    int
	InfoCount       int
	HighestSeverity string
}

// parseAlertsLogFile parses alerts.log within bounds.
func parseAlertsLogFile(filePath string, ignoreInitial bool, maxBytes int64) (alertsSummary, error) {
	summary := alertsSummary{HighestSeverity: "NONE"}
	f, err := os.Open(filePath)
	if err != nil {
		return summary, err
	}
	defer f.Close()

	limited := io.LimitReader(f, maxBytes)
	scanner := bufio.NewScanner(limited)

	nowBJ := time.Now().In(beijingLocation)
	todayStr := nowBJ.Format("2006-01-02")

	lineCount := 0
	for scanner.Scan() {
		lineCount++
		if lineCount > maxIPQALogLines {
			break
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if len(line) > maxIPQALineBytes {
			line = line[:maxIPQALineBytes]
		}

		parsed := parseAlertLine(string(line))
		if parsed == nil {
			continue
		}

		if ignoreInitial && strings.Contains(parsed.Message, "首次完成数据存档监测") {
			continue
		}

		// Filter alerts matching target window or date
		if parsed.DateStr != "" && parsed.DateStr != todayStr {
			// Alerts from prior dates are excluded from today's alert count
			continue
		}

		summary.TotalCount++
		switch parsed.Level {
		case "CRITICAL":
			summary.CriticalCount++
			summary.HighestSeverity = "CRITICAL"
		case "WARNING":
			summary.WarningCount++
			if summary.HighestSeverity != "CRITICAL" {
				summary.HighestSeverity = "WARNING"
			}
		case "INFO":
			summary.InfoCount++
			if summary.HighestSeverity != "CRITICAL" && summary.HighestSeverity != "WARNING" {
				summary.HighestSeverity = "INFO"
			}
		}
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return summary, err
	}
	return summary, nil
}

type parsedAlert struct {
	Timestamp string
	DateStr   string
	Level     string
	Message   string
	IPVersion string
}

func parseAlertLine(raw string) *parsedAlert {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	parts := strings.Split(trimmed, "|")
	if len(parts) < 3 {
		return nil
	}
	ts := strings.TrimSpace(parts[0])
	level := strings.ToUpper(strings.TrimSpace(parts[1]))
	msg := strings.TrimSpace(parts[2])
	ipVer := ""
	if len(parts) >= 4 {
		ipVer = strings.TrimSpace(parts[3])
	}

	dateStr := ""
	if len(ts) >= 10 {
		dateStr = ts[:10]
	}

	return &parsedAlert{
		Timestamp: ts,
		DateStr:   dateStr,
		Level:     level,
		Message:   msg,
		IPVersion: ipVer,
	}
}

// rawIPQAArchive holds raw fields deserialized from IPQA daily JSON files.
type rawIPQAArchive struct {
	Info   map[string]any            `json:"Info"`
	Score  map[string]any            `json:"Score"`
	Type   map[string]any            `json:"Type"`
	Factor map[string]map[string]any `json:"Factor"`
	Media  map[string]any            `json:"Media"`
}

func processFamilyArchives(dir string, familyLabel string) (*protocol.IPQAFamilyInfo, string, []protocol.IPQAChange, []string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, "", nil, nil
	}

	var jsonFiles []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if archiveFilenameRegex.MatchString(name) {
			jsonFiles = append(jsonFiles, name)
		}
	}

	if len(jsonFiles) == 0 {
		return nil, "", nil, nil
	}

	// Sort descending so newest is first
	sort.Sort(sort.Reverse(sort.StringSlice(jsonFiles)))
	if len(jsonFiles) > maxIPQAArchiveFiles {
		jsonFiles = jsonFiles[:maxIPQAArchiveFiles]
	}

	newestFile := jsonFiles[0]
	newestRaw, err := loadArchiveFile(filepath.Join(dir, newestFile))
	if err != nil {
		return nil, "", nil, nil
	}

	match := archiveFilenameRegex.FindStringSubmatch(newestFile)
	latestDate := ""
	if len(match) > 1 {
		latestDate = match[1]
	}

	familyInfo, sources := normalizeArchive(newestRaw)

	var changes []protocol.IPQAChange
	if len(jsonFiles) >= 2 {
		prevFile := jsonFiles[1]
		prevRaw, pErr := loadArchiveFile(filepath.Join(dir, prevFile))
		if pErr == nil {
			prevInfo, _ := normalizeArchive(prevRaw)
			changes = diffArchives(prevInfo, familyInfo, familyLabel)
		}
	}

	return familyInfo, latestDate, changes, sources
}

func loadArchiveFile(path string) (*rawIPQAArchive, error) {
	fi, err := os.Stat(path)
	if err != nil || fi.Size() > maxIPQAJSONBytes {
		return nil, fmt.Errorf("archive file invalid or too large: %w", err)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	limited := io.LimitReader(f, maxIPQAJSONBytes)
	var raw rawIPQAArchive
	if err := json.NewDecoder(limited).Decode(&raw); err != nil {
		return nil, err
	}
	return &raw, nil
}

func normalizeArchive(raw *rawIPQAArchive) (*protocol.IPQAFamilyInfo, []string) {
	if raw == nil {
		return nil, nil
	}

	info := &protocol.IPQAFamilyInfo{
		SourceScores: make(map[string]float64),
	}
	var sources []string

	// Info extraction
	if raw.Info != nil {
		if v, ok := raw.Info["Country"].(string); ok {
			info.Country = strings.TrimSpace(v)
		} else if vMap, ok := raw.Info["Country"].(map[string]any); ok {
			if name, ok := vMap["Name"].(string); ok {
				info.Country = strings.TrimSpace(name)
			}
		}
		if v, ok := raw.Info["Region"].(string); ok {
			info.Region = cleanRegion(v)
		}
		if v, ok := raw.Info["ASN"].(string); ok {
			info.ASN = strings.TrimSpace(v)
		} else if vNum, ok := raw.Info["ASN"].(float64); ok {
			info.ASN = fmt.Sprintf("AS%d", int(vNum))
		}
		if v, ok := raw.Info["Organization"].(string); ok {
			info.Organization = strings.TrimSpace(v)
		} else if v, ok := raw.Info["ISP"].(string); ok {
			info.Organization = strings.TrimSpace(v)
		}
		if v, ok := raw.Info["Type"].(string); ok {
			info.IPType = strings.TrimSpace(v)
		}
	}

	// Score extraction
	var totalScore float64
	var scoreCount int
	if raw.Score != nil {
		for k, v := range raw.Score {
			num := parseScoreVal(v)
			if num >= 0 {
				info.SourceScores[k] = num
				totalScore += num
				scoreCount++
				sources = append(sources, k)
			}
		}
	}

	if scoreCount > 0 {
		avg := totalScore / float64(scoreCount)
		info.RiskScore = &avg
		if avg >= 75 {
			info.RiskLevel = "high"
		} else if avg >= 25 {
			info.RiskLevel = "medium"
		} else {
			info.RiskLevel = "low"
		}
	} else {
		info.RiskLevel = "clean"
	}

	// Factor extraction
	if raw.Factor != nil {
		for factorName, engines := range raw.Factor {
			lower := strings.ToLower(factorName)
			detected := false
			for _, val := range engines {
				if b, ok := val.(bool); ok && b {
					detected = true
					break
				}
				if s, ok := val.(string); ok && (strings.EqualFold(s, "true") || strings.EqualFold(s, "yes")) {
					detected = true
					break
				}
			}
			if strings.Contains(lower, "proxy") {
				info.Proxy = &detected
			} else if strings.Contains(lower, "vpn") {
				info.VPN = &detected
			} else if strings.Contains(lower, "tor") {
				info.Tor = &detected
			} else if strings.Contains(lower, "abuse") {
				info.Abuse = &detected
			}
		}
	}

	sort.Strings(sources)
	return info, sources
}

func parseScoreVal(v any) float64 {
	switch val := v.(type) {
	case float64:
		if val >= 0 && val <= 100 {
			return val
		}
	case int:
		if val >= 0 && val <= 100 {
			return float64(val)
		}
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSuffix(val, "%"), 64); err == nil {
			if f >= 0 && f <= 100 {
				return f
			}
		}
	}
	return -1
}

func cleanRegion(r string) string {
	trimmed := strings.TrimSpace(r)
	if trimmed == "" || trimmed == "null" || trimmed == "--" {
		return "--"
	}
	return trimmed
}

func diffArchives(before, after *protocol.IPQAFamilyInfo, familyLabel string) []protocol.IPQAChange {
	if before == nil || after == nil {
		return nil
	}
	var changes []protocol.IPQAChange
	now := time.Now().UTC().Unix()

	// Risk level change
	if before.RiskLevel != after.RiskLevel && after.RiskLevel != "" {
		sev := "INFO"
		if after.RiskLevel == "high" || after.RiskLevel == "critical" {
			sev = "CRITICAL"
		} else if after.RiskLevel == "medium" {
			sev = "WARNING"
		}
		changes = append(changes, protocol.IPQAChange{
			Category:  "risk_level",
			Severity:  sev,
			Before:    fmt.Sprintf("%s 风险等级: %s", familyLabel, before.RiskLevel),
			After:     fmt.Sprintf("%s 风险等级: %s", familyLabel, after.RiskLevel),
			ChangedAt: now,
		})
	}

	// Proxy factor change
	if (before.Proxy != nil && after.Proxy != nil && *before.Proxy != *after.Proxy) ||
		(before.Proxy == nil && after.Proxy != nil && *after.Proxy) {
		sev := "INFO"
		if *after.Proxy {
			sev = "WARNING"
		}
		changes = append(changes, protocol.IPQAChange{
			Category:  "factor",
			Severity:  sev,
			Before:    fmt.Sprintf("%s Proxy: %v", familyLabel, ptrBoolStr(before.Proxy)),
			After:     fmt.Sprintf("%s Proxy: %v", familyLabel, *after.Proxy),
			ChangedAt: now,
		})
	}

	// Abuse factor change
	if (before.Abuse != nil && after.Abuse != nil && *before.Abuse != *after.Abuse) ||
		(before.Abuse == nil && after.Abuse != nil && *after.Abuse) {
		sev := "INFO"
		if *after.Abuse {
			sev = "CRITICAL"
		}
		changes = append(changes, protocol.IPQAChange{
			Category:  "factor",
			Severity:  sev,
			Before:    fmt.Sprintf("%s Abuse: %v", familyLabel, ptrBoolStr(before.Abuse)),
			After:     fmt.Sprintf("%s Abuse: %v", familyLabel, *after.Abuse),
			ChangedAt: now,
		})
	}

	// IP Type change
	if before.IPType != "" && after.IPType != "" && !strings.EqualFold(before.IPType, after.IPType) {
		changes = append(changes, protocol.IPQAChange{
			Category:  "type",
			Severity:  "INFO",
			Before:    fmt.Sprintf("%s 类型: %s", familyLabel, before.IPType),
			After:     fmt.Sprintf("%s 类型: %s", familyLabel, after.IPType),
			ChangedAt: now,
		})
	}

	return changes
}

func ptrBoolStr(b *bool) string {
	if b == nil {
		return "false"
	}
	return strconv.FormatBool(*b)
}

func parseBool(str string) bool {
	switch strings.ToLower(strings.TrimSpace(str)) {
	case "1", "t", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}
