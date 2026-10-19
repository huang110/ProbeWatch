package monitor

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/protocol"
	"github.com/probewatch/probewatch/internal/security"
	"github.com/probewatch/probewatch/internal/terminal"
	"github.com/probewatch/probewatch/internal/version"
)

const (
	defaultSyntheticTimeout = 10 * time.Second
	maxSyntheticTimeout     = 30 * time.Second
	maxResponseBodyBytes    = 512 * 1024 // 512KB
)

// SyntheticMonitor executes multi-protocol synthetic SLA contracts and assertions.
type SyntheticMonitor struct {
	client       *http.Client
	allowPrivate bool // test-only escape hatch; production constructors leave this false
}

// NewSyntheticMonitor creates a new instance with optimized keep-alive and TLS settings.
func NewSyntheticMonitor() *SyntheticMonitor {
	return NewSyntheticMonitorWithOptions(false)
}

// NewSyntheticMonitorWithOptions creates a synthetic monitor. Private target
// access is intentionally opt-in and should only be used by isolated tests;
// production callers must use NewSyntheticMonitor so all target protocols are
// SSRF-protected by default.
func NewSyntheticMonitorWithOptions(allowPrivate bool) *SyntheticMonitor {
	monitor := &SyntheticMonitor{allowPrivate: allowPrivate}
	transport := &http.Transport{
		// Synthetic probes must not inherit HTTP_PROXY/HTTPS_PROXY from the
		// agent/container environment. A proxy can bypass the resolved-target
		// checks and also leak operator-supplied headers to an intermediary.
		Proxy:                 nil,
		DialContext:           monitor.dialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: false,
		},
	}
	monitor.client = &http.Client{
		Transport: transport,
		// Per-task contexts choose the effective timeout (up to
		// maxSyntheticTimeout). Do not leave http.Client's shorter default at
		// 10s, otherwise a configured 20–30s synthetic probe is silently
		// aborted early while gRPC/WebSocket use the full task deadline.
		Timeout: maxSyntheticTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("redirect limit exceeded")
			}
			if _, err := monitor.validateURL(req.Context(), req.URL.String(), []string{"http", "https"}); err != nil {
				return fmt.Errorf("unsafe redirect: %w", err)
			}
			return nil
		},
	}
	return monitor
}

// Run executes a synthetic probe matching monitor interface convention.
func (m *SyntheticMonitor) Run(ctx context.Context, task protocol.CheckTask) protocol.SyntheticResult {
	return m.ExecuteSynthetic(ctx, task)
}

// ExecuteSynthetic runs a typed synthetic probe based on protocol.
func (m *SyntheticMonitor) ExecuteSynthetic(ctx context.Context, task protocol.CheckTask) protocol.SyntheticResult {
	targetURL := task.ServerURL
	if targetURL == "" {
		scheme := "https"
		if task.Kind == "http" || task.Port == 80 {
			scheme = "http"
		}
		path := task.Path
		if path == "" {
			path = "/"
		}
		targetURL = fmt.Sprintf("%s://%s:%d%s", scheme, task.Host, task.Port, path)
	}

	timeout := defaultSyntheticTimeout
	if task.TimeoutMS > 0 {
		timeout = time.Duration(task.TimeoutMS) * time.Millisecond
		if timeout > maxSyntheticTimeout {
			timeout = maxSyntheticTimeout
		}
	}

	subCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	switch task.Kind {
	case "grpc":
		return m.executeGRPC(subCtx, task, targetURL)
	case "websocket":
		return m.executeWebSocket(subCtx, task, targetURL)
	case "doh":
		return m.executeDoH(subCtx, task, targetURL)
	default:
		return m.executeHTTP(subCtx, task, targetURL)
	}
}

// validateURL validates the protocol-specific URL and resolves every address
// before a request starts. The transport repeats this check at dial time so a
// DNS answer changing between validation and connection cannot pivot a probe
// into a private or metadata address.
func (m *SyntheticMonitor) validateURL(ctx context.Context, raw string, schemes []string) (*url.URL, error) {
	u, err := security.ValidateURLWithOptions(raw, schemes, true, m.allowPrivate)
	if err != nil {
		return nil, err
	}
	if _, err := m.resolveHost(ctx, u.Hostname()); err != nil {
		return nil, err
	}
	return u, nil
}

func (m *SyntheticMonitor) resolveHost(ctx context.Context, host string) ([]netip.Addr, error) {
	if addr, err := netip.ParseAddr(host); err == nil {
		if !m.allowPrivate && security.IsBlockedAddress(addr) {
			return nil, fmt.Errorf("target resolved to blocked address %q", addr)
		}
		return []netip.Addr{addr}, nil
	}
	if err := security.ValidateHost(host); err != nil {
		if !m.allowPrivate || !isPrivateHostLiteral(host) {
			return nil, err
		}
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if !m.allowPrivate {
		if err := security.ValidateResolvedIPs(addresses); err != nil {
			return nil, err
		}
	}
	return addresses, nil
}

func isPrivateHostLiteral(host string) bool {
	if strings.EqualFold(host, "localhost") || strings.EqualFold(host, "localhost.localdomain") || strings.EqualFold(host, "ip6-localhost") || strings.EqualFold(host, "ip6-loopback") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && security.IsBlockedAddress(addr)
}

func (m *SyntheticMonitor) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := m.resolveHost(ctx, host)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	var lastErr error
	for _, addr := range addresses {
		conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("target resolved to no addresses")
	}
	return nil, lastErr
}

// executeHTTP executes an HTTP/HTTPS synthetic transaction with full waterfall timing.
func (m *SyntheticMonitor) executeHTTP(ctx context.Context, task protocol.CheckTask, targetURL string) protocol.SyntheticResult {
	now := time.Now().UTC()
	res := protocol.SyntheticResult{
		Protocol:  "https",
		TargetURL: targetURL,
		Status:    "ok",
		Passed:    true,
		CheckedAt: now.Unix(),
	}

	if strings.HasPrefix(targetURL, "http://") {
		res.Protocol = "http"
	}
	validatedURL, err := m.validateURL(ctx, targetURL, []string{"http", "https"})
	if err != nil {
		res.Status = "error"
		res.Passed = false
		res.Error = err.Error()
		return res
	}
	targetURL = validatedURL.String()

	method := strings.ToUpper(strings.TrimSpace(task.Method))
	if method == "" {
		method = http.MethodGet
	}

	var bodyReader io.Reader
	if task.BodyPayload != "" {
		bodyReader = strings.NewReader(task.BodyPayload)
	}

	req, err := http.NewRequestWithContext(ctx, method, targetURL, bodyReader)
	if err != nil {
		res.Status = "error"
		res.Passed = false
		res.Error = err.Error()
		return res
	}

	// Set custom headers
	for k, v := range task.Headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "ProbeWatch-Synthetic-Probe/"+version.ServerVersion)
	}

	// Trace network waterfall events
	var (
		t0           = time.Now()
		dnsStart     time.Time
		dnsDone      time.Time
		connectStart time.Time
		connectDone  time.Time
		tlsStart     time.Time
		tlsDone      time.Time
		ttfbTime     time.Time
	)

	trace := &httptrace.ClientTrace{
		DNSStart: func(_ httptrace.DNSStartInfo) { dnsStart = time.Now() },
		DNSDone:  func(_ httptrace.DNSDoneInfo) { dnsDone = time.Now() },
		ConnectStart: func(_, _ string) {
			connectStart = time.Now()
			if dnsDone.IsZero() && !dnsStart.IsZero() {
				dnsDone = connectStart
			}
		},
		ConnectDone: func(_, _ string, _ error) { connectDone = time.Now() },
		TLSHandshakeStart: func() {
			tlsStart = time.Now()
			if connectDone.IsZero() && !connectStart.IsZero() {
				connectDone = tlsStart
			}
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, _ error) { tlsDone = time.Now() },
		GotFirstResponseByte: func() {
			ttfbTime = time.Now()
		},
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))

	resp, err := m.client.Do(req)
	tEnd := time.Now()

	// Compute waterfall timings in milliseconds
	if !dnsDone.IsZero() && !dnsStart.IsZero() {
		res.Timing.DNSLookupMS = dnsDone.Sub(dnsStart).Milliseconds()
	}
	if !connectDone.IsZero() && !connectStart.IsZero() {
		res.Timing.TCPConnectMS = connectDone.Sub(connectStart).Milliseconds()
	}
	if !tlsDone.IsZero() && !tlsStart.IsZero() {
		res.Timing.TLSHandshakeMS = tlsDone.Sub(tlsStart).Milliseconds()
	}
	if !ttfbTime.IsZero() {
		res.Timing.TTFBMS = ttfbTime.Sub(t0).Milliseconds()
	} else {
		res.Timing.TTFBMS = tEnd.Sub(t0).Milliseconds()
	}
	res.Timing.TotalDurationMS = tEnd.Sub(t0).Milliseconds()

	if err != nil {
		res.Status = "error"
		res.Passed = false
		res.Error = err.Error()
		return res
	}
	defer resp.Body.Close()

	res.StatusCode = resp.StatusCode
	// Without an explicit contract assertion, an HTTP error response is not a
	// healthy synthetic check. Previously a 404/500 with no assertions stayed
	// `Passed=true`, which made a reachable-but-broken endpoint look healthy.
	hasStatusCodeAssertion := false
	for _, rule := range task.Assertions {
		if rule.Source == "status_code" {
			hasStatusCodeAssertion = true
			break
		}
	}
	if !hasStatusCodeAssertion && (resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusBadRequest) {
		res.Passed = false
		res.Status = "failing"
		res.Error = fmt.Sprintf("unexpected HTTP status %d", resp.StatusCode)
	}

	// Read bounded body
	readStart := time.Now()
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyBytes))
	res.Timing.TransferMS = time.Since(readStart).Milliseconds()
	res.Timing.TotalDurationMS = time.Since(t0).Milliseconds()

	// Evaluate Assertions
	failedRule := evaluateAssertions(resp, string(bodyBytes), res.Timing.TotalDurationMS, task.Assertions)
	if failedRule != "" {
		res.Passed = false
		res.Status = "failing"
		res.FailedAssertion = failedRule
		res.Error = fmt.Sprintf("assertion failed: %s", failedRule)
	}

	return res
}

// executeWebSocket connects and tests a WebSocket handshake and an actual
// application data echo. A successful TCP connection or a 101 response alone
// is not sufficient: a broken upgrade proxy can still return 101 while never
// accepting WebSocket frames.
func (m *SyntheticMonitor) executeWebSocket(ctx context.Context, task protocol.CheckTask, targetURL string) protocol.SyntheticResult {
	now := time.Now().UTC()
	res := protocol.SyntheticResult{
		Protocol:  "websocket",
		TargetURL: targetURL,
		Status:    "error",
		Passed:    false,
		CheckedAt: now.Unix(),
	}

	t0 := time.Now()
	if _, err := m.validateURL(ctx, targetURL, []string{"ws", "wss", "http", "https"}); err != nil {
		res.Error = err.Error()
		res.Timing.TotalDurationMS = time.Since(t0).Milliseconds()
		return res
	}
	header := make(http.Header)
	for key, value := range task.Headers {
		header.Set(key, value)
	}
	if header.Get("User-Agent") == "" {
		header.Set("User-Agent", "ProbeWatch-Synthetic/"+version.ServerVersion)
	}

	conn, err := terminal.DialWithDialer(ctx, targetURL, header, m.dialContext)
	res.Timing.TotalDurationMS = time.Since(t0).Milliseconds()
	if err != nil {
		res.Error = fmt.Sprintf("websocket handshake error: %v", err)
		return res
	}
	defer conn.Close()
	stopContextWatch := closeOnContextDone(ctx, conn)
	defer stopContextWatch()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	res.StatusCode = http.StatusSwitchingProtocols
	probe := []byte("ProbeWatch-WebSocket-Echo")
	if err := conn.WriteMessage(terminal.OpText, probe); err != nil {
		res.Error = fmt.Sprintf("websocket probe write error: %v", err)
		res.Timing.TotalDurationMS = time.Since(t0).Milliseconds()
		return res
	}
	opcode, echoed, err := conn.ReadMessage()
	res.Timing.TotalDurationMS = time.Since(t0).Milliseconds()
	if err != nil {
		res.Error = fmt.Sprintf("websocket echo read error: %v", err)
		return res
	}
	if opcode != terminal.OpText || !bytes.Equal(echoed, probe) {
		res.Status = "failing"
		res.Error = "server did not echo the WebSocket probe payload"
		return res
	}

	res.Status = "ok"
	res.Passed = true
	res.WebSocketEcho = true
	return res
}

// executeGRPC verifies the transport layer required by gRPC: TLS endpoints
// must negotiate h2 and every endpoint must return a valid HTTP/2 SETTINGS
// frame after the client preface. A bare TCP connection is deliberately not
// treated as a healthy gRPC service.
func (m *SyntheticMonitor) executeGRPC(ctx context.Context, task protocol.CheckTask, targetURL string) protocol.SyntheticResult {
	now := time.Now().UTC()
	res := protocol.SyntheticResult{
		Protocol:  "grpc",
		TargetURL: targetURL,
		Status:    "error",
		Passed:    false,
		CheckedAt: now.Unix(),
	}

	u, err := m.validateURL(ctx, targetURL, []string{"grpc", "grpcs", "http", "https"})
	if err != nil {
		res.Error = err.Error()
		return res
	}
	scheme := strings.ToLower(strings.TrimSpace(u.Scheme))
	useTLS := scheme == "https" || scheme == "grpcs"
	if scheme != "http" && scheme != "https" && scheme != "grpc" && scheme != "grpcs" {
		res.Error = fmt.Sprintf("unsupported gRPC URL scheme %q", u.Scheme)
		return res
	}
	if u.Hostname() == "" {
		res.Error = "gRPC URL must include a host"
		return res
	}
	port := u.Port()
	if port == "" {
		port = strconv.Itoa(task.Port)
		if port == "0" {
			if useTLS {
				port = "443"
			} else {
				port = "80"
			}
		}
	}
	hostPort := net.JoinHostPort(u.Hostname(), port)

	t0 := time.Now()
	conn, err := m.dialContext(ctx, "tcp", hostPort)
	if err != nil {
		res.Error = fmt.Sprintf("gRPC connect error: %v", err)
		res.Timing.TotalDurationMS = time.Since(t0).Milliseconds()
		return res
	}
	defer conn.Close()
	res.Timing.TCPConnectMS = time.Since(t0).Milliseconds()
	stopContextWatch := closeOnContextDone(ctx, conn)
	defer stopContextWatch()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	if useTLS {
		tlsConn := tls.Client(conn, &tls.Config{
			ServerName: u.Hostname(),
			NextProtos: []string{"h2"},
			MinVersion: tls.VersionTLS12,
		})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			res.Error = fmt.Sprintf("gRPC TLS handshake error: %v", err)
			res.Timing.TotalDurationMS = time.Since(t0).Milliseconds()
			return res
		}
		state := tlsConn.ConnectionState()
		if state.NegotiatedProtocol != "h2" {
			res.Error = fmt.Sprintf("gRPC TLS endpoint did not negotiate h2 (got %q)", state.NegotiatedProtocol)
			res.Timing.TotalDurationMS = time.Since(t0).Milliseconds()
			return res
		}
		conn = tlsConn
		res.Timing.TLSHandshakeMS = time.Since(t0).Milliseconds() - res.Timing.TCPConnectMS
	}

	// Send HTTP/2 connection preface: "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"
	preface := []byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")
	if err := writeSyntheticConn(conn, preface); err != nil {
		res.Error = fmt.Sprintf("gRPC preface write error: %v", err)
		return res
	}

	// Send empty SETTINGS frame: 9-byte header [0,0,0, 0x04, 0x00, 0,0,0,0]
	settingsFrame := []byte{0x00, 0x00, 0x00, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00}
	if err := writeSyntheticConn(conn, settingsFrame); err != nil {
		res.Error = fmt.Sprintf("gRPC settings frame write error: %v", err)
		return res
	}

	// A gRPC-capable HTTP/2 peer must answer the preface with a legal SETTINGS
	// frame. Reading arbitrary bytes or EOF here would make ordinary TCP/HTTP
	// services appear healthy.
	frameType, flags, streamID, payload, err := readSyntheticHTTP2Frame(conn)
	if err != nil {
		res.Error = fmt.Sprintf("gRPC SETTINGS read error: %v", err)
		res.Timing.TotalDurationMS = time.Since(t0).Milliseconds()
		return res
	}
	if frameType != 0x04 || streamID != 0 || (flags&0x01 != 0 && len(payload) != 0) || len(payload)%6 != 0 {
		res.Error = "gRPC endpoint returned an invalid HTTP/2 SETTINGS frame"
		res.Timing.TotalDurationMS = time.Since(t0).Milliseconds()
		return res
	}

	res.Timing.TotalDurationMS = time.Since(t0).Milliseconds()
	res.Status = "ok"
	res.Passed = true
	res.GRPCStatus = "SERVING"
	return res
}

// valuesForSyntheticHeader keeps the header conversion in one place and
// avoids accidentally mutating task.Headers while constructing an outbound
// request.
func closeOnContextDone(ctx context.Context, closer interface{ Close() error }) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = closer.Close()
		case <-done:
		}
	}()
	return func() { close(done) }
}

func writeSyntheticConn(conn net.Conn, payload []byte) error {
	for len(payload) > 0 {
		n, err := conn.Write(payload)
		if n > 0 {
			payload = payload[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func readSyntheticHTTP2Frame(conn net.Conn) (frameType byte, flags byte, streamID uint32, payload []byte, err error) {
	var header [9]byte
	if _, err = io.ReadFull(conn, header[:]); err != nil {
		return 0, 0, 0, nil, err
	}
	length := int(header[0])<<16 | int(header[1])<<8 | int(header[2])
	if length > 64*1024 {
		return 0, 0, 0, nil, fmt.Errorf("HTTP/2 frame payload too large: %d", length)
	}
	streamID = binary.BigEndian.Uint32(header[5:9]) & 0x7fffffff
	payload = make([]byte, length)
	if _, err = io.ReadFull(conn, payload); err != nil {
		return 0, 0, 0, nil, err
	}
	return header[3], header[4], streamID, payload, nil
}

// executeDoH executes an RFC 8484 DNS-over-HTTPS wire format query.
func (m *SyntheticMonitor) executeDoH(ctx context.Context, task protocol.CheckTask, targetURL string) protocol.SyntheticResult {
	now := time.Now().UTC()
	res := protocol.SyntheticResult{
		Protocol:  "doh",
		TargetURL: targetURL,
		Status:    "ok",
		Passed:    true,
		CheckedAt: now.Unix(),
	}
	if _, err := m.validateURL(ctx, targetURL, []string{"http", "https"}); err != nil {
		res.Status = "error"
		res.Passed = false
		res.Error = err.Error()
		return res
	}

	// Build a standard DNS query wireformat for task.Host or "example.com"
	queryDomain := task.Host
	if queryDomain == "" || queryDomain == "127.0.0.1" {
		queryDomain = "cloudflare.com"
	}

	dnsMsg := buildDNSQueryWire(queryDomain)

	t0 := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(dnsMsg))
	if err != nil {
		res.Status = "error"
		res.Passed = false
		res.Error = err.Error()
		return res
	}

	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")

	resp, err := m.client.Do(req)
	res.Timing.TotalDurationMS = time.Since(t0).Milliseconds()
	if err != nil {
		res.Status = "error"
		res.Passed = false
		res.Error = err.Error()
		return res
	}
	defer resp.Body.Close()

	res.StatusCode = resp.StatusCode
	if resp.StatusCode != http.StatusOK {
		res.Passed = false
		res.Status = "failing"
		res.Error = fmt.Sprintf("DoH server returned status %d", resp.StatusCode)
		return res
	}

	wireResp, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	answers := parseDNSAnswersWire(wireResp)
	res.DNSAnswers = answers
	if len(answers) == 0 && len(wireResp) < 12 {
		res.Passed = false
		res.Status = "failing"
		res.Error = "DoH response has empty answers"
	}

	return res
}

// evaluateAssertions tests all assertion rules against the HTTP response.
func evaluateAssertions(resp *http.Response, body string, totalLatencyMS int64, rules []protocol.SyntheticAssertionRule) string {
	for _, rule := range rules {
		passed := false
		switch rule.Source {
		case "status_code":
			expected, err := strconv.Atoi(rule.Target)
			if err == nil {
				switch rule.Operator {
				case "equals":
					passed = resp.StatusCode == expected
				case "not_equals":
					passed = resp.StatusCode != expected
				case "less_than":
					passed = resp.StatusCode < expected
				case "greater_than":
					passed = resp.StatusCode > expected
				}
			}
		case "header":
			val := resp.Header.Get(rule.Property)
			switch rule.Operator {
			case "equals":
				passed = val == rule.Target
			case "not_equals":
				passed = val != rule.Target
			case "contains":
				passed = strings.Contains(val, rule.Target)
			case "not_contains":
				passed = !strings.Contains(val, rule.Target)
			case "regex_match":
				re, err := regexp.Compile(rule.Target)
				if err == nil {
					passed = re.MatchString(val)
				}
			}
		case "body_regex":
			re, err := regexp.Compile(rule.Target)
			if err == nil {
				matched := re.MatchString(body)
				if rule.Operator == "not_contains" {
					passed = !matched
				} else {
					passed = matched
				}
			}
		case "jsonpath":
			val := extractJSONProperty(body, rule.Property)
			switch rule.Operator {
			case "equals":
				passed = val == rule.Target
			case "not_equals":
				passed = val != rule.Target
			case "contains":
				passed = strings.Contains(val, rule.Target)
			case "not_contains":
				passed = !strings.Contains(val, rule.Target)
			}
		case "max_latency_ms":
			limit, err := strconv.ParseInt(rule.Target, 10, 64)
			if err == nil {
				passed = totalLatencyMS <= limit
			}
		}

		if !passed {
			return fmt.Sprintf("[%s %s %s '%s']", rule.Source, rule.Property, rule.Operator, rule.Target)
		}
	}
	return ""
}

// extractJSONProperty extracts a nested dot-separated key from a JSON object string.
func extractJSONProperty(jsonStr, propPath string) string {
	var parsed any
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return ""
	}

	parts := strings.Split(propPath, ".")
	curr := parsed
	for _, part := range parts {
		if strings.TrimSpace(part) == "" || part == "$" {
			continue
		}
		if m, ok := curr.(map[string]any); ok {
			curr = m[part]
		} else {
			return ""
		}
	}
	if curr == nil {
		return ""
	}
	return fmt.Sprintf("%v", curr)
}

// buildDNSQueryWire generates a minimal RFC 1035 A record DNS query.
func buildDNSQueryWire(domain string) []byte {
	buf := new(bytes.Buffer)
	// ID (16-bit random)
	_ = binary.Write(buf, binary.BigEndian, uint16(0x1234))
	// Flags: Standard query, recursion desired (0x0100)
	_ = binary.Write(buf, binary.BigEndian, uint16(0x0100))
	// QDCOUNT: 1
	_ = binary.Write(buf, binary.BigEndian, uint16(1))
	// ANCOUNT: 0, NSCOUNT: 0, ARCOUNT: 0
	_ = binary.Write(buf, binary.BigEndian, uint16(0))
	_ = binary.Write(buf, binary.BigEndian, uint16(0))
	_ = binary.Write(buf, binary.BigEndian, uint16(0))

	// QNAME: labels
	for _, part := range strings.Split(domain, ".") {
		if len(part) == 0 {
			continue
		}
		buf.WriteByte(byte(len(part)))
		buf.WriteString(part)
	}
	buf.WriteByte(0) // root label

	// QTYPE: A (1)
	_ = binary.Write(buf, binary.BigEndian, uint16(1))
	// QCLASS: IN (1)
	_ = binary.Write(buf, binary.BigEndian, uint16(1))

	return buf.Bytes()
}

// parseDNSAnswersWire parses IP strings from a raw DNS response wireformat.
func parseDNSAnswersWire(wire []byte) []string {
	if len(wire) < 12 {
		return nil
	}
	ancount := binary.BigEndian.Uint16(wire[6:8])
	if ancount == 0 {
		return nil
	}

	var answers []string
	offset := 12
	// Skip QNAME
	for offset < len(wire) {
		length := int(wire[offset])
		if length == 0 {
			offset++
			break
		}
		if length >= 0xC0 {
			offset += 2
			break
		}
		offset += 1 + length
	}
	// Skip QTYPE & QCLASS
	offset += 4

	// Parse Answers
	for i := 0; i < int(ancount) && offset+10 <= len(wire); i++ {
		// Skip NAME (could be pointer 2 bytes or labels)
		if wire[offset]&0xC0 == 0xC0 {
			offset += 2
		} else {
			for offset < len(wire) && wire[offset] != 0 {
				offset += 1 + int(wire[offset])
			}
			offset++
		}
		if offset+10 > len(wire) {
			break
		}
		rtype := binary.BigEndian.Uint16(wire[offset : offset+2])
		rdlength := binary.BigEndian.Uint16(wire[offset+8 : offset+10])
		offset += 10
		if offset+int(rdlength) > len(wire) {
			break
		}
		if rtype == 1 && rdlength == 4 { // A record
			ip := net.IP(wire[offset : offset+4])
			answers = append(answers, ip.String())
		}
		offset += int(rdlength)
	}
	return answers
}
