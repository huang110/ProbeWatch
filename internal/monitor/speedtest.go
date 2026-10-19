package monitor

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/protocol"
	"github.com/probewatch/probewatch/internal/security"
)

const (
	defaultSpeedtestTimeout = 25 * time.Second
	maxSpeedtestTimeout     = 30 * time.Second
	defaultDownloadBytes    = 10 * 1024 * 1024 // 10MB
	maxDownloadBytes        = 50 * 1024 * 1024 // 50MB
	defaultUploadBytes      = 5 * 1024 * 1024  // 5MB
	maxUploadBytes          = 20 * 1024 * 1024 // 20MB
	streamBufferSize        = 64 * 1024        // 64KB
	pingProbeCount          = 3
)

// zeroReader generates infinite zero bytes without memory allocation.
type zeroReader struct{}

func (z zeroReader) Read(p []byte) (n int, err error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

// byteCountingReader wraps an io.Reader to count bytes read.
type byteCountingReader struct {
	reader io.Reader
	count  int64
}

func (b *byteCountingReader) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	b.count += int64(n)
	return n, err
}

// SpeedtestRunner executes bandwidth and latency benchmark probes.
type SpeedtestRunner struct {
	Client       *http.Client
	RoundTripper http.RoundTripper
	Timeout      time.Duration
	allowPrivate bool // test-only escape hatch; production constructors leave this false
}

// NewSpeedtestRunner creates the SSRF-safe production speedtest runner.
func NewSpeedtestRunner() *SpeedtestRunner { return &SpeedtestRunner{} }

// NewSpeedtestRunnerWithOptions is intentionally limited to isolated tests.
// Production code must use NewSpeedtestRunner so private/link-local/metadata
// destinations remain blocked.
func NewSpeedtestRunnerWithOptions(allowPrivate bool) *SpeedtestRunner {
	return &SpeedtestRunner{allowPrivate: allowPrivate}
}

// calculateSpeedMbps converts a byte count and elapsed duration to Mbps.
// Windows can report a zero-duration loop for very small local test payloads;
// use a conservative floor so the result remains finite and protocol-valid.
func calculateSpeedMbps(bytes int64, duration time.Duration) float64 {
	if bytes <= 0 {
		return 0
	}
	if duration < time.Millisecond {
		duration = time.Millisecond
	}
	mbps := (float64(bytes) * 8.0 * float64(time.Second)) / (float64(duration.Nanoseconds()) * 1_000_000.0)
	if mbps > 1000000 {
		return 1000000
	}
	return math.Round(mbps*100) / 100
}

// Run executes a speedtest benchmark probe against the task's configured server.
func (r *SpeedtestRunner) Run(parent context.Context, task protocol.CheckTask) protocol.SpeedtestResult {
	started := time.Now()
	result := protocol.SpeedtestResult{
		ServerName: task.Host,
		TestedAt:   started.Unix(),
		Status:     "ok",
	}

	targetURL := strings.TrimSpace(task.ServerURL)
	if targetURL == "" {
		scheme := "https"
		if task.Port == 80 {
			scheme = "http"
		}
		path := task.Path
		if path == "" {
			path = "/"
		}
		targetURL = fmt.Sprintf("%s://%s:%d%s", scheme, task.Host, task.Port, path)
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = defaultSpeedtestTimeout
	}
	if task.TimeoutMS > 0 {
		taskTimeout := time.Duration(task.TimeoutMS) * time.Millisecond
		if taskTimeout < timeout {
			timeout = taskTimeout
		}
	}
	if timeout > maxSpeedtestTimeout {
		timeout = maxSpeedtestTimeout
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	validatedURL, err := r.validateURL(ctx, targetURL)
	if err != nil {
		result.Status = "error"
		result.Error = "invalid speedtest target: " + err.Error()
		result.DurationMS = time.Since(started).Milliseconds()
		return result
	}
	targetURL = validatedURL.String()
	result.ServerURL = targetURL
	if result.ServerName == "" {
		result.ServerName = validatedURL.Hostname()
	}

	client := r.getClient()

	// 1. Latency and Jitter Probing (3 pings)
	rtts := make([]int64, 0, pingProbeCount)
	latencyLoop:
	for i := 0; i < pingProbeCount; i++ {
		select {
		case <-ctx.Done():
			break latencyLoop
		default:
		}
		pingStart := time.Now()
		pingReq, reqErr := http.NewRequestWithContext(ctx, http.MethodHead, targetURL, nil)
		if reqErr != nil {
			continue
		}
		pingReq.Header.Set("User-Agent", "ProbeWatch-Benchmark/1.0")
		resp, respErr := client.Do(pingReq)
		if respErr != nil {
			// Try small GET if HEAD fails
			pingReq, reqErr = http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
			if reqErr == nil {
				pingReq.Header.Set("Range", "bytes=0-0")
				pingReq.Header.Set("User-Agent", "ProbeWatch-Benchmark/1.0")
				resp, respErr = client.Do(pingReq)
			}
		}
		if respErr == nil {
			_ = resp.Body.Close()
			rtts = append(rtts, time.Since(pingStart).Milliseconds())
		}
		select {
		case <-ctx.Done():
			break latencyLoop
		case <-time.After(20 * time.Millisecond):
		}
	}

	if len(rtts) > 0 {
		var sum int64
		for _, val := range rtts {
			sum += val
		}
		result.LatencyMS = sum / int64(len(rtts))
		if len(rtts) > 1 {
			var diffSum float64
			for i := 1; i < len(rtts); i++ {
				diffSum += math.Abs(float64(rtts[i] - rtts[i-1]))
			}
			result.JitterMS = int64(math.Round(diffSum / float64(len(rtts)-1)))
		}
	}

	// 2. Download Speed Measurement
	dlBytes := task.DownloadBytes
	if dlBytes <= 0 {
		dlBytes = defaultDownloadBytes
	}
	if dlBytes > maxDownloadBytes {
		dlBytes = maxDownloadBytes
	}

	dlURL := targetURL
	if strings.Contains(targetURL, "speed.cloudflare.com") && !strings.Contains(targetURL, "bytes=") {
		if strings.Contains(targetURL, "?") {
			dlURL = fmt.Sprintf("%s&bytes=%d", targetURL, dlBytes)
		} else {
			dlURL = fmt.Sprintf("%s?bytes=%d", targetURL, dlBytes)
		}
	}

	dlReq, err := http.NewRequestWithContext(ctx, http.MethodGet, dlURL, nil)
	if err == nil {
		dlReq.Header.Set("User-Agent", "ProbeWatch-Benchmark/1.0")
		dlStart := time.Now()
		dlResp, dlErr := client.Do(dlReq)
		if dlErr == nil {
			if dlResp.StatusCode < http.StatusOK || dlResp.StatusCode >= http.StatusMultipleChoices {
				_ = dlResp.Body.Close()
				if result.Error == "" {
					result.Error = fmt.Sprintf("download returned HTTP status %d", dlResp.StatusCode)
				}
			} else {
				buf := make([]byte, streamBufferSize)
				var totalRead int64
				limitedBody := io.LimitReader(dlResp.Body, dlBytes)
				for {
					n, rErr := limitedBody.Read(buf)
					totalRead += int64(n)
					if rErr != nil || totalRead >= dlBytes {
						break
					}
				}
				_ = dlResp.Body.Close()
				dlDuration := time.Since(dlStart)
				result.BytesReceived = totalRead
				result.DownloadSpeedMbps = calculateSpeedMbps(totalRead, dlDuration)
			}
		} else if result.Error == "" {
			result.Error = fmt.Sprintf("download failed: %v", dlErr)
		}
	}

	// 3. Upload Speed Measurement
	ulBytes := task.UploadBytes
	if ulBytes <= 0 {
		ulBytes = defaultUploadBytes
	}
	if ulBytes > maxUploadBytes {
		ulBytes = maxUploadBytes
	}

	ulURL := targetURL
	if strings.Contains(targetURL, "speed.cloudflare.com") && strings.Contains(targetURL, "__down") {
		ulURL = strings.Replace(targetURL, "__down", "__up", 1)
		// strip query params for upload
		if idx := strings.Index(ulURL, "?"); idx != -1 {
			ulURL = ulURL[:idx]
		}
	}

	countingBody := &byteCountingReader{reader: io.LimitReader(zeroReader{}, ulBytes)}
	ulReq, err := http.NewRequestWithContext(ctx, http.MethodPost, ulURL, countingBody)
	if err == nil {
		ulReq.ContentLength = ulBytes
		ulReq.Header.Set("Content-Type", "application/octet-stream")
		ulReq.Header.Set("User-Agent", "ProbeWatch-Benchmark/1.0")
		ulStart := time.Now()
		ulResp, ulErr := client.Do(ulReq)
		ulDuration := time.Since(ulStart)
		result.BytesSent = countingBody.count
		if ulErr == nil {
			// A hostile endpoint must not be able to keep an agent draining an
			// unbounded response after the upload has completed.
			_, _ = io.CopyN(io.Discard, ulResp.Body, 64<<10)
			_ = ulResp.Body.Close()
			if ulResp.StatusCode < http.StatusOK || ulResp.StatusCode >= http.StatusMultipleChoices {
				if result.Error == "" {
					result.Error = fmt.Sprintf("upload returned HTTP status %d", ulResp.StatusCode)
				}
			} else {
				result.UploadSpeedMbps = calculateSpeedMbps(countingBody.count, ulDuration)
			}
		} else {
			if result.Error == "" {
				result.Error = fmt.Sprintf("upload failed: %v", ulErr)
			}
		}
	}

	result.DurationMS = time.Since(started).Milliseconds()
	if result.BytesReceived == 0 && result.BytesSent == 0 && result.LatencyMS == 0 {
		result.Status = "error"
		if result.Error == "" {
			result.Error = "benchmark unreachable"
		}
	} else if result.Error == "" {
		result.Status = "ok"
	} else {
		result.Status = "error"
	}

	return result
}

func (r *SpeedtestRunner) getClient() *http.Client {
	var client http.Client
	if r.Client != nil && r.allowPrivate {
		// Private destinations are only enabled by isolated tests. Clone the
		// client so caller-owned redirect policy is not mutated.
		client = *r.Client
	} else {
		tr := r.RoundTripper
		if tr == nil {
			tr = &http.Transport{
				Proxy:           nil,
				DialContext:     r.dialContext,
				TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
				DisableKeepAlives: false,
				MaxIdleConns:      10,
				IdleConnTimeout:   30 * time.Second,
			}
		}
		client.Transport = tr
	}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("redirect limit exceeded")
		}
		if _, err := r.validateURL(req.Context(), req.URL.String()); err != nil {
			return fmt.Errorf("unsafe redirect: %w", err)
		}
		return nil
	}
	return &client
}

func (r *SpeedtestRunner) validateURL(ctx context.Context, raw string) (*url.URL, error) {
	u, err := security.ValidateURLWithOptions(raw, []string{"http", "https"}, true, r.allowPrivate)
	if err != nil {
		return nil, err
	}
	if _, err := r.resolveHost(ctx, u.Hostname()); err != nil {
		return nil, err
	}
	return u, nil
}

func (r *SpeedtestRunner) resolveHost(ctx context.Context, host string) ([]netip.Addr, error) {
	if addr, err := netip.ParseAddr(host); err == nil {
		if !r.allowPrivate && security.IsBlockedAddress(addr) {
			return nil, fmt.Errorf("target resolved to blocked address %q", addr)
		}
		return []netip.Addr{addr}, nil
	}
	if err := security.ValidateHost(host); err != nil {
		return nil, err
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if !r.allowPrivate {
		if err := security.ValidateResolvedIPs(addresses); err != nil {
			return nil, err
		}
	}
	return addresses, nil
}

func (r *SpeedtestRunner) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := r.resolveHost(ctx, host)
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
