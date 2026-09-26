package monitor

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/protocol"
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
	result.ServerURL = targetURL

	parsedURL, err := url.Parse(targetURL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		result.Status = "error"
		result.Error = "invalid speedtest target URL scheme"
		result.DurationMS = time.Since(started).Milliseconds()
		return result
	}
	if result.ServerName == "" {
		result.ServerName = parsedURL.Hostname()
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

	client := r.getClient()

	// 1. Latency and Jitter Probing (3 pings)
	rtts := make([]int64, 0, pingProbeCount)
	for i := 0; i < pingProbeCount; i++ {
		select {
		case <-ctx.Done():
			break
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
		time.Sleep(20 * time.Millisecond)
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
			buf := make([]byte, streamBufferSize)
			var totalRead int64
			limitedBody := io.LimitReader(dlResp.Body, dlBytes)
			for {
				n, rErr := limitedBody.Read(buf)
				totalRead += int64(n)
				if rErr != nil {
					break
				}
				if totalRead >= dlBytes {
					break
				}
			}
			_ = dlResp.Body.Close()
			dlDuration := time.Since(dlStart)
			result.BytesReceived = totalRead
			if dlDuration.Seconds() > 0 && totalRead > 0 {
				mbps := (float64(totalRead) * 8.0) / (dlDuration.Seconds() * 1_000_000.0)
				result.DownloadSpeedMbps = math.Round(mbps*100) / 100
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
			_, _ = io.Copy(io.Discard, ulResp.Body)
			_ = ulResp.Body.Close()
			if ulDuration.Seconds() > 0 && countingBody.count > 0 {
				mbps := (float64(countingBody.count) * 8.0) / (ulDuration.Seconds() * 1_000_000.0)
				result.UploadSpeedMbps = math.Round(mbps*100) / 100
			}
		} else {
			// If POST was rejected (e.g. 404 or 405 on a read-only endpoint) but some data sent
			if countingBody.count > 0 && ulDuration.Seconds() > 0 {
				mbps := (float64(countingBody.count) * 8.0) / (ulDuration.Seconds() * 1_000_000.0)
				result.UploadSpeedMbps = math.Round(mbps*100) / 100
			}
		}
	}

	result.DurationMS = time.Since(started).Milliseconds()
	if result.BytesReceived == 0 && result.BytesSent == 0 && result.LatencyMS == 0 {
		result.Status = "error"
		if result.Error == "" {
			result.Error = "benchmark unreachable"
		}
	} else {
		result.Status = "ok"
		result.Error = ""
	}

	return result
}

func (r *SpeedtestRunner) getClient() *http.Client {
	if r.Client != nil {
		return r.Client
	}
	tr := r.RoundTripper
	if tr == nil {
		tr = &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
			DisableKeepAlives: false,
			MaxIdleConns:      10,
			IdleConnTimeout:   30 * time.Second,
		}
	}
	return &http.Client{
		Transport: tr,
	}
}
