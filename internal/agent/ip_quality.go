package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/probewatch/probewatch/internal/protocol"
)

const (
	defaultIPQualityEndpoint = "https://ipwho.is/"
	defaultIPQualityCacheTTL = 6 * time.Hour
	maxIPQualityHTTPTimeout  = 5 * time.Second
	ipQualityRetryBackoff    = 5 * time.Minute
)

var asnPattern = regexp.MustCompile(`(?i)\b(?:AS)?(\d+)\b`)

// IPQualityCollector queries and caches outbound node IP intelligence.
type IPQualityCollector interface {
	Get() *protocol.IPQualityInfo
}

// DefaultIPQualityCollector is the thread-safe, non-blocking IP quality provider.
type DefaultIPQualityCollector struct {
	endpoint string
	cacheTTL time.Duration
	client   *http.Client

	mu         sync.RWMutex
	cached     *protocol.IPQualityInfo
	lastFetch  time.Time
	lastFailed time.Time
	fetching   bool

	// customFetch allows test injection
	customFetch func(ctx context.Context, endpoint string) (*protocol.IPQualityInfo, error)
}

// NewIPQualityCollector creates an IP quality collector with env configuration.
func NewIPQualityCollector() *DefaultIPQualityCollector {
	endpoint := strings.TrimSpace(os.Getenv("PROBEWATCH_IP_QUALITY_ENDPOINT"))
	if endpoint == "" {
		endpoint = defaultIPQualityEndpoint
	} else if !strings.HasPrefix(strings.ToLower(endpoint), "https://") {
		// Enforce HTTPS security constraint
		slog.Warn("PROBEWATCH_IP_QUALITY_ENDPOINT must use HTTPS; falling back to default", "endpoint_prefix", "insecure")
		endpoint = defaultIPQualityEndpoint
	}

	ttl := defaultIPQualityCacheTTL
	if rawTTL := strings.TrimSpace(os.Getenv("PROBEWATCH_IP_QUALITY_CACHE_TTL")); rawTTL != "" {
		if parsed, err := time.ParseDuration(rawTTL); err == nil && parsed > 0 {
			ttl = parsed
		} else if sec, err := strconv.Atoi(rawTTL); err == nil && sec > 0 {
			ttl = time.Duration(sec) * time.Second
		}
	}

	return &DefaultIPQualityCollector{
		endpoint: endpoint,
		cacheTTL: ttl,
		client: &http.Client{
			Timeout: maxIPQualityHTTPTimeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return fmt.Errorf("ip quality redirects disallowed")
			},
		},
	}
}

// Get returns the latest cached IPQualityInfo without blocking the caller.
// It initiates asynchronous background fetching if the cache is empty or expired.
func (c *DefaultIPQualityCollector) Get() *protocol.IPQualityInfo {
	if c == nil {
		return nil
	}

	c.mu.RLock()
	cached := c.cached
	lastFetch := c.lastFetch
	lastFailed := c.lastFailed
	fetching := c.fetching
	ttl := c.cacheTTL
	c.mu.RUnlock()

	now := time.Now()
	needsFetch := false
	if cached == nil {
		if lastFailed.IsZero() || now.Sub(lastFailed) >= ipQualityRetryBackoff {
			needsFetch = true
		}
	} else if now.Sub(lastFetch) >= ttl {
		needsFetch = true
	}

	if needsFetch && !fetching {
		c.mu.Lock()
		if !c.fetching {
			c.fetching = true
			go c.backgroundFetch()
		}
		c.mu.Unlock()
	}

	return cached
}

func (c *DefaultIPQualityCollector) backgroundFetch() {
	defer func() {
		c.mu.Lock()
		c.fetching = false
		c.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), maxIPQualityHTTPTimeout)
	defer cancel()

	var result *protocol.IPQualityInfo
	var err error

	if c.customFetch != nil {
		result, err = c.customFetch(ctx, c.endpoint)
	} else {
		result, err = c.doHTTPFetch(ctx, c.endpoint)
	}

	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()

	if err != nil {
		c.lastFailed = now
		// Keep cached result if present, never overwrite with nil on error
		slog.Warn("ip quality collection failed; keeping previous result", "error_type", fmt.Sprintf("%T", err))
		return
	}

	if result != nil {
		c.cached = result
		c.lastFetch = now
		c.lastFailed = time.Time{}
	}
}

func (c *DefaultIPQualityCollector) doHTTPFetch(ctx context.Context, endpoint string) (*protocol.IPQualityInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ProbeWatch-Agent-IPQuality/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ip quality service returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 128*1024))
	if err != nil {
		return nil, err
	}

	return ParseIPQualityResponse(body)
}

// ParseIPQualityResponse parses JSON responses from various public IP intelligence services.
func ParseIPQualityResponse(body []byte) (*protocol.IPQualityInfo, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("empty ip quality response")
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("invalid json: %w", err)
	}

	// Some providers return { "success": false } or { "error": true }
	if success, ok := raw["success"].(bool); ok && !success {
		return nil, fmt.Errorf("provider reported failure")
	}
	if isErr, ok := raw["error"].(bool); ok && isErr {
		return nil, fmt.Errorf("provider returned error")
	}

	info := &protocol.IPQualityInfo{
		CheckedAt: time.Now().UTC().Unix(),
		IPType:    "unknown",
		Risk:      "unknown",
	}

	// 1. Country
	if cc := getString(raw, "country_code", "countryCode"); cc != "" {
		info.Country = strings.ToUpper(truncateString(cc, 64))
	} else if c := getString(raw, "country"); c != "" {
		info.Country = truncateString(c, 64)
	} else if loc, ok := raw["location"].(map[string]any); ok {
		if cc := getString(loc, "country_code", "countryCode", "country"); cc != "" {
			info.Country = strings.ToUpper(truncateString(cc, 64))
		}
	}

	// 2. Region
	if r := getString(raw, "region", "regionName", "state"); r != "" {
		info.Region = truncateString(r, 128)
	} else if loc, ok := raw["location"].(map[string]any); ok {
		if r := getString(loc, "region", "regionName", "state", "city"); r != "" {
			info.Region = truncateString(r, 128)
		}
	} else if city := getString(raw, "city"); city != "" {
		info.Region = truncateString(city, 128)
	}

	// 3. ASN
	rawASN := extractField(raw, "connection.asn", "as.number", "asn", "isp.asn")
	if rawASN != nil {
		info.ASN = formatASN(rawASN)
	}

	// 4. Organization
	rawOrg := extractField(raw, "connection.org", "org", "organization", "company", "connection.isp", "isp", "isp.org", "isp.isp")
	if rawOrg != nil {
		orgStr := strings.TrimSpace(fmt.Sprint(rawOrg))
		// If info.ASN is empty, check if org starts with "AS12345 " (e.g. ipinfo.io format)
		if info.ASN == "" {
			if matches := asnPattern.FindStringSubmatch(orgStr); len(matches) > 1 {
				info.ASN = "AS" + matches[1]
			}
		}
		// If org starts with "AS12345 ", strip the ASN prefix
		if info.ASN != "" && strings.HasPrefix(strings.ToUpper(orgStr), strings.ToUpper(info.ASN)) {
			orgStr = strings.TrimSpace(orgStr[len(info.ASN):])
		}
		info.Organization = truncateString(orgStr, 256)
	}

	// 5. Proxy, VPN, Tor, Abuse flags
	info.Proxy = extractBool(raw, "security.proxy", "risk.is_proxy", "is_proxy", "proxy")
	info.VPN = extractBool(raw, "security.vpn", "risk.is_vpn", "is_vpn", "vpn")
	info.Tor = extractBool(raw, "security.tor", "risk.is_tor", "is_tor", "tor")
	info.Abuse = extractBool(raw, "security.abuse", "risk.is_abuse", "is_abuse", "is_abuser", "abuse")

	// 6. IP Type
	info.IPType = parseIPType(raw)

	// 7. Risk and Sources
	info.Risk, info.Sources = parseRiskAndSources(raw, info.Proxy, info.VPN, info.Tor, info.Abuse)

	return info, nil
}

func parseIPType(raw map[string]any) string {
	// Check boolean flags
	if isDC := extractBool(raw, "security.hosting", "risk.is_datacenter", "is_datacenter", "is_hosting", "hosting"); isDC != nil && *isDC {
		return "hosting"
	}
	if isRes := extractBool(raw, "risk.is_residential", "is_residential"); isRes != nil && *isRes {
		return "residential"
	}

	// Check string type indicators
	typeVal := getString(raw, "type", "ip_type", "usage_type", "connection_type")
	if typeVal != "" {
		lower := strings.ToLower(typeVal)
		if strings.Contains(lower, "datacenter") || strings.Contains(lower, "hosting") || strings.Contains(lower, "cloud") || strings.Contains(lower, "server") || strings.Contains(lower, "vps") {
			return "hosting"
		}
		if strings.Contains(lower, "residential") || strings.Contains(lower, "home") {
			return "residential"
		}
		if strings.Contains(lower, "isp") || strings.Contains(lower, "business") || strings.Contains(lower, "commercial") {
			return "isp"
		}
	}

	return "unknown"
}

func parseRiskAndSources(raw map[string]any, proxy, vpn, tor, abuse *bool) (string, map[string]float64) {
	sources := make(map[string]float64)

	// Check explicit risk scores
	var scoreVal float64 = -1
	for _, key := range []string{"fraud_score", "risk_score", "risk.risk_score", "score"} {
		if val := extractField(raw, key); val != nil {
			if s, ok := toFloat(val); ok && s >= 0 && s <= 100 {
				scoreVal = s
				cleanKey := key
				if strings.Contains(cleanKey, ".") {
					parts := strings.Split(cleanKey, ".")
					cleanKey = parts[len(parts)-1]
				}
				sources[cleanKey] = s
				break
			}
		}
	}

	// Explicit risk string from provider
	if r := getString(raw, "risk", "risk_level"); r != "" {
		lower := strings.ToLower(r)
		switch lower {
		case "low", "medium", "high", "unknown":
			if len(sources) == 0 {
				return lower, nil
			}
			return lower, sources
		}
	}

	// Derive from numerical score if available
	if scoreVal >= 0 {
		var risk string
		if scoreVal >= 75 {
			risk = "high"
		} else if scoreVal >= 25 {
			risk = "medium"
		} else {
			risk = "low"
		}
		return risk, sources
	}

	// Derive from threat flags
	if (tor != nil && *tor) || (abuse != nil && *abuse) {
		return "high", nil
	}
	if (proxy != nil && *proxy) || (vpn != nil && *vpn) {
		return "medium", nil
	}
	hasAnyFlag := (proxy != nil || vpn != nil || tor != nil || abuse != nil)
	allFalse := (proxy == nil || !*proxy) && (vpn == nil || !*vpn) && (tor == nil || !*tor) && (abuse == nil || !*abuse)
	if hasAnyFlag && allFalse {
		return "low", nil
	}

	return "unknown", nil
}

func formatASN(val any) string {
	switch v := val.(type) {
	case float64:
		return fmt.Sprintf("AS%d", int64(v))
	case int:
		return fmt.Sprintf("AS%d", v)
	case int64:
		return fmt.Sprintf("AS%d", v)
	case string:
		str := strings.TrimSpace(v)
		if matches := asnPattern.FindStringSubmatch(str); len(matches) > 1 {
			return "AS" + matches[1]
		}
		if str != "" && !strings.HasPrefix(strings.ToUpper(str), "AS") {
			return truncateString("AS"+str, 64)
		}
		return truncateString(str, 64)
	default:
		return ""
	}
}

func extractField(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if strings.Contains(k, ".") {
			parts := strings.Split(k, ".")
			curr := any(m)
			found := true
			for _, part := range parts {
				if subMap, ok := curr.(map[string]any); ok {
					curr = subMap[part]
				} else {
					found = false
					break
				}
			}
			if found && curr != nil {
				return curr
			}
		} else if val, ok := m[k]; ok && val != nil {
			return val
		}
	}
	return nil
}

func getString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if val := extractField(m, k); val != nil {
			if s, ok := val.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func extractBool(m map[string]any, keys ...string) *bool {
	for _, k := range keys {
		if val := extractField(m, k); val != nil {
			switch v := val.(type) {
			case bool:
				return &v
			case int:
				b := v != 0
				return &b
			case float64:
				b := v != 0
				return &b
			case string:
				lower := strings.ToLower(strings.TrimSpace(v))
				if lower == "true" || lower == "yes" || lower == "1" {
					b := true
					return &b
				}
				if lower == "false" || lower == "no" || lower == "0" {
					b := false
					return &b
				}
			}
		}
	}
	return nil
}

func toFloat(val any) (float64, bool) {
	switch v := val.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

func truncateString(s string, limit int) string {
	if len([]byte(s)) > limit {
		return string([]byte(s)[:limit])
	}
	return s
}
