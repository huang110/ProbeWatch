package monitor

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/protocol"
	"github.com/probewatch/probewatch/internal/security"
)

const (
	mediaMaxReasonBytes = 512
	// mediaRegionProbeBytes bounds how much of the response body may be held
	// in memory for region matching. The prefix is never persisted.
	mediaRegionProbeBytes = 64 * 1024
)

var errMediaBodyExceedsLimit = errors.New("body exceeds limit")

// MediaDetector performs a deliberately restricted HTTP availability check.
// It accepts only the fields present in protocol.CheckTask; no caller-supplied
// headers, cookies, credentials, or response content are ever persisted.
type MediaDetector struct {
	Detector     string
	Resolver     Resolver
	Dialer       Dialer
	MaxBodyBytes int64
	MaxRedirects int
	Timeout      time.Duration
	roundTripper http.RoundTripper
}

// Run checks the media endpoint and returns one of the fixed media statuses:
// available, unavailable, timeout, blocked, invalid, or error.
func (d *MediaDetector) Run(parent context.Context, task protocol.CheckTask) protocol.MediaResult {
	started := time.Now()
	result := protocol.MediaResult{Detector: d.Detector, CheckedAt: started.Unix()}
	if result.Detector == "" {
		result.Detector = task.ID
	}
	if err := task.Validate(); err != nil {
		result.Status = "invalid"
		result.Reason = mediaReason(err)
		result.LatencyMS = time.Since(started).Milliseconds()
		return result
	}
	if task.Kind != "media_http" {
		result.Status = "invalid"
		result.Reason = "task kind must be media_http"
		result.LatencyMS = time.Since(started).Milliseconds()
		return result
	}

	timeout := d.timeoutFor(task.TimeoutMS)
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	// media_http has no scheme field. HTTPS is the safe default.
	target, err := security.ValidateURL((&url.URL{
		Scheme: "https",
		Host:   net.JoinHostPort(task.Host, strconv.Itoa(task.Port)),
		Path:   task.Path,
	}).String(), []string{"https"})
	if err != nil {
		result.Status = "blocked"
		result.Reason = mediaReason(err)
		result.LatencyMS = time.Since(started).Milliseconds()
		return result
	}
	probe := &Probe{Resolver: d.Resolver, Dialer: d.Dialer, MaxRedirects: d.MaxRedirects, Timeout: d.Timeout, roundTripper: d.roundTripper}
	if err = probe.validateAndResolveURL(ctx, target); err != nil {
		result.Status = mediaStatusForError(ctx, err, "blocked")
		result.Reason = mediaReason(err)
		result.LatencyMS = time.Since(started).Milliseconds()
		return result
	}

	client := &http.Client{Transport: d.httpTransport(probe), CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= probe.maxRedirects() {
			return errors.New("redirect limit exceeded")
		}
		if _, err := security.ValidateURL(req.URL.String(), []string{"https"}); err != nil {
			return &blockedError{err: err}
		}
		return probe.validateAndResolveURL(req.Context(), req.URL)
	}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err == nil {
		request.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36")
		request.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
		request.Header.Set("Accept-Language", "en-US,en;q=0.9,zh-CN;q=0.8,zh;q=0.7")
		var response *http.Response
		response, err = client.Do(request)
		if err == nil {
			defer response.Body.Close()
			capture := &bodyPrefixCapture{}
			var n int64
			n, err = io.Copy(capture, io.LimitReader(response.Body, d.maxBodyBytes()+1))
			if err == nil && n > d.maxBodyBytes() {
				err = errMediaBodyExceedsLimit
			}
			if err == nil {
				if response.StatusCode == task.ExpectedStatus || task.ExpectedStatus == 0 && response.StatusCode >= 200 && response.StatusCode < 300 {
					result.Status = "available"
				} else {
					result.Status = "unavailable"
				}
				result.Region = matchRegionRule(task.RegionRules, capture.prefix)
			}
		}

	}
	if err != nil {
		result.Status = mediaStatusForError(ctx, err, "error")
		result.Reason = mediaReason(err)
	}
	result.LatencyMS = time.Since(started).Milliseconds()
	return result
}

// bodyPrefixCapture counts bytes streamed to the size limit while retaining a
// bounded prefix of the response body for region matching. It never stores
// more than mediaRegionProbeBytes and nothing it holds is persisted.
type bodyPrefixCapture struct {
	prefix []byte
}

func (w *bodyPrefixCapture) Write(p []byte) (int, error) {
	if len(w.prefix) < mediaRegionProbeBytes {
		remaining := mediaRegionProbeBytes - len(w.prefix)
		if remaining > len(p) {
			remaining = len(p)
		}
		w.prefix = append(w.prefix, p[:remaining]...)
	}
	return len(p), nil
}

// matchRegionRule returns the region of the first rule whose marker occurs in
// the bounded body prefix, or autodetects standard region patterns if none match.
func matchRegionRule(rules []protocol.RegionRule, prefix []byte) string {
	if len(prefix) == 0 {
		return ""
	}
	for _, rule := range rules {
		if rule.Contains != "" && bytes.Contains(prefix, []byte(rule.Contains)) {
			return rule.Region
		}
	}
	// Autodetect Cloudflare trace loc=XX (e.g. ChatGPT, Claude)
	if idx := bytes.Index(prefix, []byte("loc=")); idx != -1 && idx+6 <= len(prefix) {
		code := string(prefix[idx+4 : idx+6])
		if len(code) == 2 && code[0] >= 'A' && code[0] <= 'Z' && code[1] >= 'A' && code[1] <= 'Z' {
			return code
		}
	}
	// Autodetect standard JSON country_code: "XX"
	if idx := bytes.Index(prefix, []byte(`"country_code":"`)); idx != -1 && idx+18 <= len(prefix) {
		code := string(prefix[idx+16 : idx+18])
		if len(code) == 2 && code[0] >= 'A' && code[0] <= 'Z' && code[1] >= 'A' && code[1] <= 'Z' {
			return code
		}
	}
	// Autodetect countryCode: "XX"
	if idx := bytes.Index(prefix, []byte(`"countryCode":"`)); idx != -1 && idx+17 <= len(prefix) {
		code := string(prefix[idx+15 : idx+17])
		if len(code) == 2 && code[0] >= 'A' && code[0] <= 'Z' && code[1] >= 'A' && code[1] <= 'Z' {
			return code
		}
	}
	return ""
}

func (d *MediaDetector) httpTransport(probe *Probe) http.RoundTripper {
	if d.roundTripper != nil {
		return d.roundTripper
	}
	return &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, DisableKeepAlives: true, ForceAttemptHTTP2: false, ResponseHeaderTimeout: d.timeoutFor(0), MaxResponseHeaderBytes: 32 << 10, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, portText, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		port, err := strconv.Atoi(portText)
		if err != nil {
			return nil, err
		}
		return probe.dialValidated(ctx, host, port)
	}}
}

func (d *MediaDetector) timeoutFor(taskTimeoutMS int) time.Duration {
	t := d.Timeout
	if t <= 0 {
		t = defaultProbeTimeout
	}
	if taskTimeoutMS > 0 && time.Duration(taskTimeoutMS)*time.Millisecond < t {
		t = time.Duration(taskTimeoutMS) * time.Millisecond
	}
	if t > maxProbeTimeout {
		t = maxProbeTimeout
	}
	return t
}
func (d *MediaDetector) maxBodyBytes() int64 {
	if d.MaxBodyBytes <= 0 {
		return defaultMaxBodyBytes
	}
	if d.MaxBodyBytes > maxMaxBodyBytes {
		return maxMaxBodyBytes
	}
	return d.MaxBodyBytes
}
func mediaStatusForError(ctx context.Context, err error, fallback string) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "timeout"
	}
	var blocked *blockedError
	if errors.As(err, &blocked) {
		return "blocked"
	}
	return fallback
}
func mediaReason(err error) string {
	if err == nil {
		return ""
	}
	reason := strings.Join(strings.Fields(err.Error()), " ")
	if len(reason) > mediaMaxReasonBytes {
		reason = reason[:mediaMaxReasonBytes]
	}
	return reason
}
