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
		if err == nil && task.CheckTLS {
			if tlsCert, tlsErr := p.inspectTLSCert(ctx, task.Host, task.Port); tlsErr == nil {
				result.TLSCert = tlsCert
			}
		}
	case "http", "https":
		var (
			statusCode int
			tlsCert    *protocol.TLSCertResult
			httpResult *protocol.HTTPResult
		)
		statusCode, tlsCert, httpResult, err = p.runHTTP(ctx, task)
		result.StatusCode = statusCode
		result.TLSCert = tlsCert
		result.HTTP = httpResult
		if err == nil {
			if task.ExpectedStatus != 0 && statusCode != task.ExpectedStatus {
				result.Status = "failed"
				result.Error = fmt.Sprintf("expected status %d, got %d", task.ExpectedStatus, statusCode)
			} else if task.Keyword != "" && (httpResult == nil || !httpResult.KeywordFound) {
				result.Status = "failed"
				result.Error = fmt.Sprintf("expected keyword %q not found in response", task.Keyword)
			} else {
				result.Status = "success"
			}
		}
	case "dns":
		var dnsResult *protocol.DNSResult
		dnsResult, err = p.runDNS(ctx, task)
		result.DNS = dnsResult
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

func (p *Probe) runDNS(ctx context.Context, task protocol.CheckTask) (*protocol.DNSResult, error) {
	if err := security.ValidateHost(task.Host); err != nil {
		return nil, &blockedError{err: err}
	}
	resolver, err := p.resolverForTask(task)
	if err != nil {
		return nil, err
	}
	dnsType := strings.ToUpper(task.DNSType)
	if dnsType == "" {
		dnsType = "A"
	}
	if dnsType == "CNAME" {
		if cnameResolver, ok := resolver.(interface {
			LookupCNAME(context.Context, string) (string, error)
		}); ok {
			dnsStart := time.Now()
			cname, lookupErr := cnameResolver.LookupCNAME(ctx, task.Host)
			queryTime := time.Since(dnsStart).Milliseconds()
			if lookupErr != nil {
				return nil, lookupErr
			}
			if err := security.ValidateHost(strings.TrimSuffix(cname, ".")); err != nil {
				return nil, &blockedError{err: fmt.Errorf("invalid CNAME target: %w", err)}
			}
			addresses, err := p.resolveHost(ctx, strings.TrimSuffix(cname, "."))
			if err != nil {
				return nil, err
			}
			records := []string{cname}
			for _, addr := range addresses {
				records = append(records, addr.String())
			}
			return &protocol.DNSResult{
				Records:     records,
				Nameserver:  task.Nameserver,
				QueryTimeMS: queryTime,
			}, nil
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
		return nil, &blockedError{err: fmt.Errorf("unsupported DNS type %q", task.DNSType)}
	}
	dnsStart := time.Now()
	addresses, err := resolver.LookupNetIP(ctx, network, task.Host)
	queryTime := time.Since(dnsStart).Milliseconds()
	if err != nil {
		return nil, err
	}
	if err := validateResolvedIPs(addresses); err != nil {
		return nil, err
	}
	records := make([]string, 0, len(addresses))
	for _, addr := range addresses {
		records = append(records, addr.String())
	}
	return &protocol.DNSResult{
		Records:     records,
		Nameserver:  task.Nameserver,
		QueryTimeMS: queryTime,
	}, nil
}

func (p *Probe) runHTTP(ctx context.Context, task protocol.CheckTask) (int, *protocol.TLSCertResult, *protocol.HTTPResult, error) {
	if task.Kind != "http" && task.Kind != "https" {
		return 0, nil, nil, &blockedError{err: fmt.Errorf("unsupported HTTP task kind %q", task.Kind)}
	}
	scheme := task.Kind
	rawURL := (&url.URL{
		Scheme: scheme,
		Host:   net.JoinHostPort(task.Host, strconv.Itoa(task.Port)),
		Path:   task.Path,
	}).String()
	target, err := security.ValidateURL(rawURL, []string{"http", "https"})
	if err != nil {
		return 0, nil, nil, &blockedError{err: err}
	}
	if err := p.validateAndResolveURL(ctx, target); err != nil {
		return 0, nil, nil, err
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
		return 0, nil, nil, &blockedError{err: err}
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, nil, nil, err
	}
	defer response.Body.Close()

	var tlsCert *protocol.TLSCertResult
	if response.TLS != nil && len(response.TLS.PeerCertificates) > 0 {
		tlsCert = parseTLSCert(*response.TLS)
	}

	bodyBytes, readErr := io.ReadAll(io.LimitReader(response.Body, p.maxBodyBytes()+1))
	if readErr != nil {
		return response.StatusCode, tlsCert, nil, readErr
	}
	if int64(len(bodyBytes)) > p.maxBodyBytes() {
		return response.StatusCode, tlsCert, nil, fmt.Errorf("response body exceeds limit")
	}

	httpResult := &protocol.HTTPResult{
		ResponseBytes: len(bodyBytes),
		ContentType:   response.Header.Get("Content-Type"),
	}
	if task.Keyword != "" {
		httpResult.KeywordFound = strings.Contains(string(bodyBytes), task.Keyword)
	}

	return response.StatusCode, tlsCert, httpResult, nil
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
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true},
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

func (p *Probe) inspectTLSCert(ctx context.Context, host string, port int) (*protocol.TLSCertResult, error) {
	conn, err := p.dialValidated(ctx, host, port)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	tlsConn := tls.Client(conn, &tls.Config{
		ServerName:         host,
		InsecureSkipVerify: true,
	})
	defer tlsConn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		_ = tlsConn.SetDeadline(deadline)
	}

	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return nil, err
	}

	state := tlsConn.ConnectionState()
	return parseTLSCert(state), nil
}

func parseTLSCert(state tls.ConnectionState) *protocol.TLSCertResult {
	if len(state.PeerCertificates) == 0 {
		return nil
	}
	cert := state.PeerCertificates[0]
	now := time.Now()

	issuer := cert.Issuer.CommonName
	if len(cert.Issuer.Organization) > 0 {
		issuer = cert.Issuer.Organization[0]
	} else if issuer == "" {
		issuer = cert.Issuer.String()
	}

	subject := cert.Subject.CommonName
	if subject == "" {
		subject = cert.Subject.String()
	}

	daysLeft := int(time.Until(cert.NotAfter).Hours() / 24)
	isExpired := now.After(cert.NotAfter)
	expiringSoon := !isExpired && daysLeft <= 14

	var proto string
	switch state.Version {
	case tls.VersionTLS13:
		proto = "TLS 1.3"
	case tls.VersionTLS12:
		proto = "TLS 1.2"
	case tls.VersionTLS11:
		proto = "TLS 1.1"
	case tls.VersionTLS10:
		proto = "TLS 1.0"
	default:
		proto = fmt.Sprintf("TLS 0x%04x", state.Version)
	}

	cipher := tls.CipherSuiteName(state.CipherSuite)
	if cipher == "" {
		cipher = fmt.Sprintf("0x%04x", state.CipherSuite)
	}

	return &protocol.TLSCertResult{
		Issuer:       issuer,
		Subject:      subject,
		DNSNames:     cert.DNSNames,
		NotBefore:    cert.NotBefore.Unix(),
		NotAfter:     cert.NotAfter.Unix(),
		DaysLeft:     daysLeft,
		Protocol:     proto,
		CipherSuite:  cipher,
		IsExpired:    isExpired,
		ExpiringSoon: expiringSoon,
	}
}

func (p *Probe) resolverForTask(task protocol.CheckTask) (Resolver, error) {
	if p.Resolver != nil {
		return p.Resolver, nil
	}
	if task.Nameserver != "" {
		nsHost := task.Nameserver
		nsPort := "53"
		if h, pt, err := net.SplitHostPort(task.Nameserver); err == nil {
			nsHost = h
			nsPort = pt
		}
		if err := security.ValidateHost(nsHost); err != nil {
			return nil, &blockedError{err: fmt.Errorf("invalid nameserver %q: %w", task.Nameserver, err)}
		}
		addrs, err := net.DefaultResolver.LookupNetIP(context.Background(), "ip", nsHost)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve nameserver %q: %w", task.Nameserver, err)
		}
		if err := validateResolvedIPs(addrs); err != nil {
			return nil, err
		}
		nsTarget := net.JoinHostPort(addrs[0].String(), nsPort)
		return &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{Timeout: p.timeoutFor(task.TimeoutMS)}
				return d.DialContext(ctx, "udp", nsTarget)
			},
		}, nil
	}
	return net.DefaultResolver, nil
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
