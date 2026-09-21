package monitor

import (
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

const mediaMaxReasonBytes = 512

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
		var response *http.Response
		response, err = client.Do(request)
		if err == nil {
			defer response.Body.Close()
			var n int64
			n, err = io.Copy(io.Discard, io.LimitReader(response.Body, d.maxBodyBytes()+1))
			if err == nil && n > d.maxBodyBytes() {
				err = errMediaBodyExceedsLimit
			}
			if err == nil {
				if response.StatusCode == task.ExpectedStatus || task.ExpectedStatus == 0 && response.StatusCode >= 200 && response.StatusCode < 300 {
					result.Status = "available"
				} else {
					result.Status = "unavailable"
				}
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

func (d *MediaDetector) httpTransport(probe *Probe) http.RoundTripper {
	if d.roundTripper != nil {
		return d.roundTripper
	}
	return &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, DisableKeepAlives: true, ForceAttemptHTTP2: false, ResponseHeaderTimeout: d.timeoutFor(0), DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
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
