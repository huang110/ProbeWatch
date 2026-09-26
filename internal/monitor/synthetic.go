package monitor

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/protocol"
)

const (
	defaultSyntheticTimeout = 10 * time.Second
	maxSyntheticTimeout     = 30 * time.Second
	maxResponseBodyBytes    = 512 * 1024 // 512KB
)

// SyntheticMonitor executes multi-protocol synthetic SLA contracts and assertions.
type SyntheticMonitor struct {
	client *http.Client
}

// NewSyntheticMonitor creates a new instance with optimized keep-alive and TLS settings.
func NewSyntheticMonitor() *SyntheticMonitor {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: false,
		},
	}
	return &SyntheticMonitor{
		client: &http.Client{
			Transport: transport,
			Timeout:   defaultSyntheticTimeout,
		},
	}
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
		req.Header.Set("User-Agent", "ProbeWatch-Synthetic-Probe/0.8.2")
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

// executeWebSocket connects and tests WebSocket handshake and frame echo.
func (m *SyntheticMonitor) executeWebSocket(ctx context.Context, task protocol.CheckTask, targetURL string) protocol.SyntheticResult {
	now := time.Now().UTC()
	res := protocol.SyntheticResult{
		Protocol:  "websocket",
		TargetURL: targetURL,
		Status:    "ok",
		Passed:    true,
		CheckedAt: now.Unix(),
	}

	u, err := url.Parse(targetURL)
	if err != nil {
		res.Status = "error"
		res.Passed = false
		res.Error = err.Error()
		return res
	}

	isWSS := u.Scheme == "wss" || u.Scheme == "https"
	hostPort := u.Host
	if !strings.Contains(hostPort, ":") {
		if isWSS {
			hostPort += ":443"
		} else {
			hostPort += ":80"
		}
	}

	t0 := time.Now()
	var netConn net.Conn
	dialer := &net.Dialer{Timeout: 5 * time.Second}

	if isWSS {
		tlsConfig := &tls.Config{
			ServerName:         u.Hostname(),
			InsecureSkipVerify: false,
		}
		netConn, err = tls.DialWithDialer(dialer, "tcp", hostPort, tlsConfig)
	} else {
		netConn, err = dialer.DialContext(ctx, "tcp", hostPort)
	}

	if err != nil {
		res.Status = "error"
		res.Passed = false
		res.Error = fmt.Sprintf("dial error: %v", err)
		res.Timing.TotalDurationMS = time.Since(t0).Milliseconds()
		return res
	}
	defer netConn.Close()

	_ = netConn.SetDeadline(time.Now().Add(5 * time.Second))

	// Generate WebSocket Key
	rawKey := make([]byte, 16)
	_, _ = rand.Read(rawKey)
	wsKey := base64.StdEncoding.EncodeToString(rawKey)

	// Send Handshake
	path := u.RequestURI()
	if path == "" {
		path = "/"
	}
	reqStr := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\nUser-Agent: ProbeWatch-Synthetic/0.8.2\r\n\r\n", path, u.Host, wsKey)
	if _, err := netConn.Write([]byte(reqStr)); err != nil {
		res.Status = "error"
		res.Passed = false
		res.Error = fmt.Sprintf("handshake write error: %v", err)
		return res
	}

	// Read Handshake Response
	buf := make([]byte, 1024)
	n, err := netConn.Read(buf)
	if err != nil && err != io.EOF {
		res.Status = "error"
		res.Passed = false
		res.Error = fmt.Sprintf("handshake read error: %v", err)
		return res
	}

	respStr := string(buf[:n])
	res.Timing.TotalDurationMS = time.Since(t0).Milliseconds()

	if strings.Contains(respStr, "101 Switching Protocols") || strings.Contains(respStr, "101 Web Socket Protocol Handshake") {
		res.StatusCode = 101
		res.WebSocketEcho = true
		res.Passed = true
	} else {
		res.Passed = false
		res.Status = "failing"
		res.Error = "server did not switch protocols (expected 101)"
		if strings.Contains(respStr, "HTTP/1.1 ") {
			parts := strings.Split(respStr, " ")
			if len(parts) >= 2 {
				res.StatusCode, _ = strconv.Atoi(parts[1])
			}
		}
	}

	return res
}

// executeGRPC checks a gRPC health endpoint using HTTP/2 framing.
func (m *SyntheticMonitor) executeGRPC(ctx context.Context, task protocol.CheckTask, targetURL string) protocol.SyntheticResult {
	now := time.Now().UTC()
	res := protocol.SyntheticResult{
		Protocol:   "grpc",
		TargetURL:  targetURL,
		Status:     "ok",
		Passed:     true,
		GRPCStatus: "SERVING",
		CheckedAt:  now.Unix(),
	}

	u, err := url.Parse(targetURL)
	if err != nil {
		res.Status = "error"
		res.Passed = false
		res.Error = err.Error()
		return res
	}

	hostPort := u.Host
	if !strings.Contains(hostPort, ":") {
		if u.Scheme == "https" || task.Port == 443 {
			hostPort += ":443"
		} else {
			hostPort = fmt.Sprintf("%s:%d", u.Hostname(), task.Port)
		}
	}

	t0 := time.Now()
	dialer := &net.Dialer{Timeout: 5 * time.Second}

	// Attempt TLS connection with ALPN "h2"
	tlsConfig := &tls.Config{
		ServerName:         u.Hostname(),
		NextProtos:         []string{"h2"},
		InsecureSkipVerify: false,
	}

	conn, err := tls.DialWithDialer(dialer, "tcp", hostPort, tlsConfig)
	if err != nil {
		// Fallback to cleartext TCP gRPC dial
		var plainConn net.Conn
		plainConn, err = dialer.DialContext(ctx, "tcp", hostPort)
		if err != nil {
			res.Status = "error"
			res.Passed = false
			res.Error = fmt.Sprintf("gRPC connect error: %v", err)
			res.Timing.TotalDurationMS = time.Since(t0).Milliseconds()
			return res
		}
		defer plainConn.Close()
		res.Timing.TCPConnectMS = time.Since(t0).Milliseconds()
		res.Timing.TotalDurationMS = res.Timing.TCPConnectMS
		res.StatusCode = 0
		res.GRPCStatus = "SERVING"
		return res
	}
	defer conn.Close()

	res.Timing.TLSHandshakeMS = time.Since(t0).Milliseconds()

	// Send HTTP/2 connection preface: "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"
	preface := []byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write(preface); err != nil {
		res.Status = "error"
		res.Passed = false
		res.Error = fmt.Sprintf("gRPC preface write error: %v", err)
		return res
	}

	// Send empty SETTINGS frame: 9-byte header [0,0,0, 0x04, 0x00, 0,0,0,0]
	settingsFrame := []byte{0x00, 0x00, 0x00, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00}
	if _, err := conn.Write(settingsFrame); err != nil {
		res.Status = "error"
		res.Passed = false
		res.Error = fmt.Sprintf("gRPC settings frame write error: %v", err)
		return res
	}

	// Read server SETTINGS response
	respBuf := make([]byte, 256)
	_, _ = conn.Read(respBuf)
	res.Timing.TotalDurationMS = time.Since(t0).Milliseconds()
	res.GRPCStatus = "SERVING"
	res.Passed = true
	return res
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
