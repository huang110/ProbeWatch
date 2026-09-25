package monitor

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/protocol"
	"github.com/probewatch/probewatch/internal/security"
)

const (
	defaultProbeTimeout = 5 * time.Second
	maxProbeTimeout     = 30 * time.Second
	defaultMaxBodyBytes = 1 << 20
	maxMaxBodyBytes     = 8 << 20
	defaultMaxRedirects = 5
	maxMaxRedirects     = 10
)

// Resolver is the DNS dependency used by a network probe.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// Dialer is the connection dependency used by a network probe.
type Dialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// Probe executes SSRF-safe TCP, HTTP(S), and DNS checks.
type Probe struct {
	Resolver     Resolver
	Dialer       Dialer
	MaxBodyBytes int64
	MaxRedirects int
	Timeout      time.Duration

	roundTripper http.RoundTripper
}

type blockedError struct{ err error }

func (e *blockedError) Error() string { return e.err.Error() }
func (e *blockedError) Unwrap() error { return e.err }

func (p *Probe) Run(parent context.Context, task protocol.CheckTask) protocol.NetworkResult {
	started := time.Now()
	result := protocol.NetworkResult{
		Host:      task.Host,
		Port:      task.Port,
		CheckedAt: started.Unix(),
	}
	if err := task.Validate(); err != nil {
		err = &blockedError{err: err}
		result.Status = "blocked"
		result.Error = safeError(err)
		result.LatencyMS = time.Since(started).Milliseconds()
		return result
	}

	timeout := p.timeoutFor(task.TimeoutMS)
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	if task.Port < 1 || task.Port > 65535 {
		err := &blockedError{err: fmt.Errorf("invalid port %d", task.Port)}
		result.Status = "blocked"
		result.Error = safeError(err)
		result.LatencyMS = time.Since(started).Milliseconds()
		return result
	}

	var err error
	switch task.Kind {
	case "tcp":
		err = p.runTCP(ctx, task)
	case "http", "https":
		var statusCode int
		statusCode, err = p.runHTTP(ctx, task)
		result.StatusCode = statusCode
		if err == nil {
			if task.ExpectedStatus != 0 && statusCode != task.ExpectedStatus {
				result.Status = "failed"
			} else {
				result.Status = "success"
			}
		}
	case "dns":
		err = p.runDNS(ctx, task)
	default:
		err = &blockedError{err: fmt.Errorf("unsupported network task kind %q", task.Kind)}
	}

	result.LatencyMS = time.Since(started).Milliseconds()
	if err == nil && result.Status == "" {
		result.Status = "success"
		return result
	}
	if err == nil {
		return result
	}
	result.Error = safeError(err)
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(ctx.Err(), context.DeadlineExceeded):
		result.Status = "timeout"
	case errors.As(err, new(*blockedError)):
		result.Status = "blocked"
	case result.Status == "failed":
		// Preserve an HTTP status mismatch classification.
	case task.Kind == "tcp" && !errors.Is(err, context.Canceled):
		result.Status = "failed"
	default:
		result.Status = "error"
	}
	return result
}

func (p *Probe) runTCP(ctx context.Context, task protocol.CheckTask) error {
	if err := security.ValidateHost(task.Host); err != nil {
		return &blockedError{err: err}
	}
	conn, err := p.dialValidated(ctx, task.Host, task.Port)
	if err != nil {
		return err
	}
	return conn.Close()
}

func (p *Probe) runDNS(ctx context.Context, task protocol.CheckTask) error {
	if err := security.ValidateHost(task.Host); err != nil {
		return &blockedError{err: err}
	}
	resolver, err := p.resolver()
	if err != nil {
		return err
	}
	dnsType := strings.ToUpper(task.DNSType)
	if dnsType == "" {
		dnsType = "A"
	}
	if dnsType == "CNAME" {
		if cnameResolver, ok := resolver.(interface {
			LookupCNAME(context.Context, string) (string, error)
		}); ok {
			cname, lookupErr := cnameResolver.LookupCNAME(ctx, task.Host)
			if lookupErr != nil {
				return lookupErr
			}
			if err := security.ValidateHost(strings.TrimSuffix(cname, ".")); err != nil {
				return &blockedError{err: fmt.Errorf("invalid CNAME target: %w", err)}
			}
			_, err = p.resolveHost(ctx, strings.TrimSuffix(cname, "."))
			return err
		}
	}

	network := "ip"
	switch dnsType {
	case "A":
		network = "ip4"
	case "AAAA":
		network = "ip6"
	case "CNAME":
		// The public Resolver contract exposes IP lookups only. The lookup still
		// remains bounded and is useful for resolvers without LookupCNAME.
	default:
		return &blockedError{err: fmt.Errorf("unsupported DNS type %q", task.DNSType)}
	}
	addresses, err := resolver.LookupNetIP(ctx, network, task.Host)
	if err != nil {
		return err
	}
	return validateResolvedIPs(addresses)
}

func (p *Probe) runHTTP(ctx context.Context, task protocol.CheckTask) (int, error) {
	if task.Kind != "http" && task.Kind != "https" {
		return 0, &blockedError{err: fmt.Errorf("unsupported HTTP task kind %q", task.Kind)}
	}
	scheme := task.Kind
	rawURL := (&url.URL{
		Scheme: scheme,
		Host:   net.JoinHostPort(task.Host, strconv.Itoa(task.Port)),
		Path:   task.Path,
	}).String()
	target, err := security.ValidateURL(rawURL, []string{"http", "https"})
	if err != nil {
		return 0, &blockedError{err: err}
	}
	if err := p.validateAndResolveURL(ctx, target); err != nil {
		return 0, err
	}

	transport := p.httpTransport()
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= p.maxRedirects() {
				return fmt.Errorf("redirect limit exceeded")
			}
			if err := p.validateAndResolveURL(req.Context(), req.URL); err != nil {
				return err
			}
			return nil
		},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return 0, &blockedError{err: err}
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	read, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, p.maxBodyBytes()+1))
	if readErr != nil {
		return response.StatusCode, readErr
	}
	if read > p.maxBodyBytes() {
		return response.StatusCode, fmt.Errorf("response body exceeds limit")
	}
	return response.StatusCode, nil
}

func (p *Probe) validateAndResolveURL(ctx context.Context, target *url.URL) error {
	if _, err := security.ValidateURL(target.String(), []string{"http", "https"}); err != nil {
		return &blockedError{err: err}
	}
	if _, err := p.resolveHost(ctx, target.Hostname()); err != nil {
		return err
	}
	return nil
}

func (p *Probe) dialValidated(ctx context.Context, host string, port int) (net.Conn, error) {
	if err := security.ValidateHost(host); err != nil {
		return nil, &blockedError{err: err}
	}
	addresses, err := p.resolveHost(ctx, host)
	if err != nil {
		return nil, err
	}
	address := net.JoinHostPort(addresses[0].String(), strconv.Itoa(port))
	dialer, err := p.dialer()
	if err != nil {
		return nil, err
	}
	return dialer.DialContext(ctx, "tcp", address)
}

func (p *Probe) resolveHost(ctx context.Context, host string) ([]netip.Addr, error) {
	resolver, err := p.resolver()
	if err != nil {
		return nil, err
	}
	addresses, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	return addresses, validateResolvedIPs(addresses)
}

func validateResolvedIPs(addresses []netip.Addr) error {
	if err := security.ValidateResolvedIPs(addresses); err != nil {
		return &blockedError{err: err}
	}
	return nil
}

func (p *Probe) httpTransport() http.RoundTripper {
	if p.roundTripper != nil {
		return p.roundTripper
	}
	return &http.Transport{
		Proxy:                 nil,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		DisableKeepAlives:     true,
		ForceAttemptHTTP2:     false,
		ResponseHeaderTimeout: p.timeoutFor(0),
		MaxResponseHeaderBytes: 32 << 10,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, portText, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			port, err := strconv.Atoi(portText)
			if err != nil {
				return nil, err
			}
			return p.dialValidated(ctx, host, port)
		},
	}
}

func (p *Probe) resolver() (Resolver, error) {
	if p.Resolver != nil {
		return p.Resolver, nil
	}
	return net.DefaultResolver, nil
}

func (p *Probe) dialer() (Dialer, error) {
	if p.Dialer != nil {
		return p.Dialer, nil
	}
	return &net.Dialer{}, nil
}

func (p *Probe) timeoutFor(taskTimeoutMS int) time.Duration {
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = defaultProbeTimeout
	}
	if taskTimeoutMS > 0 && time.Duration(taskTimeoutMS)*time.Millisecond < timeout {
		timeout = time.Duration(taskTimeoutMS) * time.Millisecond
	}
	if timeout > maxProbeTimeout {
		return maxProbeTimeout
	}
	return timeout
}

func (p *Probe) maxBodyBytes() int64 {
	if p.MaxBodyBytes <= 0 {
		return defaultMaxBodyBytes
	}
	if p.MaxBodyBytes > maxMaxBodyBytes {
		return maxMaxBodyBytes
	}
	return p.MaxBodyBytes
}

func (p *Probe) maxRedirects() int {
	if p.MaxRedirects <= 0 {
		return defaultMaxRedirects
	}
	if p.MaxRedirects > maxMaxRedirects {
		return maxMaxRedirects
	}
	return p.MaxRedirects
}

func safeError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
