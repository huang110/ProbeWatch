package monitor

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/protocol"
)

type fakeResolver struct {
	mu        sync.Mutex
	addresses [][]netip.Addr
	networks  []string
	hosts     []string
	cname     string
}

func (r *fakeResolver) LookupNetIP(_ context.Context, network, host string) ([]netip.Addr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.networks = append(r.networks, network)
	r.hosts = append(r.hosts, host)
	if len(r.addresses) == 0 {
		return nil, errors.New("no fake DNS result")
	}
	result := r.addresses[0]
	if len(r.addresses) > 1 {
		r.addresses = r.addresses[1:]
	}
	return result, nil
}

func (r *fakeResolver) LookupCNAME(_ context.Context, _ string) (string, error) {
	if r.cname == "" {
		return "", errors.New("no fake CNAME result")
	}
	return r.cname, nil
}

type fakeDialer struct {
	mu        sync.Mutex
	addresses []string
	err       error
	conn      net.Conn
	block     bool
}

func (d *fakeDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	d.mu.Lock()
	d.addresses = append(d.addresses, network+" "+address)
	err := d.err
	conn := d.conn
	block := d.block
	d.mu.Unlock()
	if block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	return conn, nil
}

type countingBody struct {
	remaining int
	read      int
}

func (b *countingBody) Read(p []byte) (int, error) {
	if b.remaining == 0 {
		return 0, io.EOF
	}
	n := len(p)
	if n > b.remaining {
		n = b.remaining
	}
	for i := 0; i < n; i++ {
		p[i] = 'x'
	}
	b.remaining -= n
	b.read += n
	return n, nil
}

func (b *countingBody) Close() error { return nil }

func publicAddress(t *testing.T, value string) netip.Addr {
	t.Helper()
	address, err := netip.ParseAddr(value)
	if err != nil {
		t.Fatal(err)
	}
	return address
}

func networkTask(kind string) protocol.CheckTask {
	return protocol.CheckTask{
		ID: "check-1", Kind: kind, Host: "example.com", Port: 443,
		Path: "/health", ExpectedStatus: 200, DNSType: "A", TimeoutMS: 1000,
		MaxHops: 1, IntervalSeconds: 10, Enabled: true,
	}
}

func TestProbeRunRejectsInvalidTargetBeforeDNSOrDial(t *testing.T) {
	resolver := &fakeResolver{addresses: [][]netip.Addr{{publicAddress(t, "93.184.216.34")}}}
	dialer := &fakeDialer{conn: nopConn{}}
	result := (&Probe{Resolver: resolver, Dialer: dialer}).Run(context.Background(), protocol.CheckTask{
		ID: "bad", Kind: "tcp", Host: "127.0.0.1", Port: 443, TimeoutMS: 1000,
		IntervalSeconds: 10, MaxHops: 1,
	})
	if result.Status != "blocked" {
		t.Fatalf("status = %q, want blocked: %s", result.Status, result.Error)
	}
	if len(resolver.hosts) != 0 || len(dialer.addresses) != 0 {
		t.Fatal("invalid target reached DNS or dial")
	}
}

func TestProbeRunRejectsInvalidPortBeforeDNSOrDial(t *testing.T) {
	resolver := &fakeResolver{addresses: [][]netip.Addr{{publicAddress(t, "93.184.216.34")}}}
	dialer := &fakeDialer{conn: nopConn{}}
	result := (&Probe{Resolver: resolver, Dialer: dialer}).Run(context.Background(), protocol.CheckTask{
		ID: "bad-port", Kind: "tcp", Host: "example.com", Port: 0, TimeoutMS: 1000,
		IntervalSeconds: 10, MaxHops: 1,
	})
	if result.Status != "blocked" {
		t.Fatalf("status = %q, want blocked: %s", result.Status, result.Error)
	}
	if len(resolver.hosts) != 0 || len(dialer.addresses) != 0 {
		t.Fatal("invalid port reached DNS or dial")
	}
}

func TestProbeRunRejectsOversizedHTTPPathBeforeDNSOrDial(t *testing.T) {
	resolver := &fakeResolver{addresses: [][]netip.Addr{{publicAddress(t, "93.184.216.34")}}}
	dialer := &fakeDialer{conn: nopConn{}}
	task := networkTask("http")
	task.Path = "/" + strings.Repeat("x", 2048)

	result := (&Probe{Resolver: resolver, Dialer: dialer}).Run(context.Background(), task)
	if result.Status != "blocked" {
		t.Fatalf("status = %q, want blocked: %s", result.Status, result.Error)
	}
	if len(resolver.hosts) != 0 || len(dialer.addresses) != 0 {
		t.Fatal("oversized path reached DNS or dial")
	}
}

func TestProbeRunTCPRevalidatesAndNormalizesResolvedAddress(t *testing.T) {
	resolver := &fakeResolver{addresses: [][]netip.Addr{{publicAddress(t, "93.184.216.34")}}}
	dialer := &fakeDialer{conn: nopConn{}}
	result := (&Probe{Resolver: resolver, Dialer: dialer}).Run(context.Background(), networkTask("tcp"))
	if result.Status != "success" {
		t.Fatalf("status = %q, want success: %s", result.Status, result.Error)
	}
	if len(resolver.hosts) != 1 || resolver.networks[0] != "ip" || resolver.hosts[0] != "example.com" {
		t.Fatalf("resolver calls = %#v %#v, want one ip lookup for example.com", resolver.networks, resolver.hosts)
	}
	if len(dialer.addresses) != 1 || dialer.addresses[0] != "tcp 93.184.216.34:443" {
		t.Fatalf("dial calls = %#v, want normalized public address", dialer.addresses)
	}
}

func TestProbeRunBlocksDNSRebindingBeforeSecondDial(t *testing.T) {
	resolver := &fakeResolver{addresses: [][]netip.Addr{
		{publicAddress(t, "93.184.216.34")},
		{publicAddress(t, "127.0.0.1")},
	}}
	dialer := &fakeDialer{conn: nopConn{}}
	p := &Probe{Resolver: resolver, Dialer: dialer}
	if _, err := p.dialValidated(context.Background(), "example.com", 443); err != nil {
		t.Fatalf("first dial returned error: %v", err)
	}
	if _, err := p.dialValidated(context.Background(), "example.com", 443); err == nil {
		t.Fatal("second dial accepted a rebinding to loopback")
	}
	if len(dialer.addresses) != 1 {
		t.Fatalf("dial calls = %d, want only the first dial", len(dialer.addresses))
	}
}

func TestProbeRunDNSUsesBoundedLookupAndNormalizedType(t *testing.T) {
	resolver := &fakeResolver{addresses: [][]netip.Addr{{publicAddress(t, "2001:4860:4860::8888")}}}
	dialer := &fakeDialer{conn: nopConn{}}
	task := networkTask("dns")
	task.DNSType = "AAAA"
	result := (&Probe{Resolver: resolver, Dialer: dialer}).Run(context.Background(), task)
	if result.Status != "success" {
		t.Fatalf("status = %q, want success: %s", result.Status, result.Error)
	}
	if len(resolver.networks) != 1 || resolver.networks[0] != "ip6" {
		t.Fatalf("DNS networks = %#v, want [ip6]", resolver.networks)
	}
	if len(dialer.addresses) != 0 {
		t.Fatal("DNS check performed a dial")
	}
}

func TestProbeRunDNSCNAMEUsesBoundedCNAMELookup(t *testing.T) {
	resolver := &fakeResolver{
		addresses: [][]netip.Addr{{publicAddress(t, "93.184.216.34")}},
		cname:     "alias.example.com.",
	}
	task := networkTask("dns")
	task.DNSType = "CNAME"
	result := (&Probe{Resolver: resolver, Dialer: &fakeDialer{conn: nopConn{}}}).Run(context.Background(), task)
	if result.Status != "success" {
		t.Fatalf("status = %q, want success: %s", result.Status, result.Error)
	}
	if len(resolver.hosts) != 1 || resolver.hosts[0] != "alias.example.com" {
		t.Fatalf("resolver hosts = %#v, want canonical hostname lookup", resolver.hosts)
	}
	if len(resolver.networks) != 1 || resolver.networks[0] != "ip" {
		t.Fatalf("resolver networks = %#v, want [ip]", resolver.networks)
	}
}

func TestProbeRunDNSCNAMERejectsPrivateOrMetadataCanonicalAddress(t *testing.T) {
	for _, test := range []struct {
		name string
		addr string
	}{
		{name: "private", addr: "10.0.0.1"},
		{name: "metadata", addr: "169.254.169.254"},
	} {
		t.Run(test.name, func(t *testing.T) {
			resolver := &fakeResolver{
				addresses: [][]netip.Addr{{publicAddress(t, test.addr)}},
				cname:     "canonical.example.com.",
			}
			task := networkTask("dns")
			task.DNSType = "CNAME"

			result := (&Probe{Resolver: resolver}).Run(context.Background(), task)
			if result.Status != "blocked" {
				t.Fatalf("status = %q, want blocked: %s", result.Status, result.Error)
			}
			if len(resolver.hosts) != 1 || resolver.hosts[0] != "canonical.example.com" {
				t.Fatalf("resolver hosts = %#v, want canonical hostname lookup", resolver.hosts)
			}
		})
	}
}

func TestProbeRunDNSCNAMEResolvesSafeCanonicalAddress(t *testing.T) {
	resolver := &fakeResolver{
		addresses: [][]netip.Addr{{publicAddress(t, "93.184.216.34")}},
		cname:     "canonical.example.com.",
	}
	task := networkTask("dns")
	task.DNSType = "CNAME"

	result := (&Probe{Resolver: resolver}).Run(context.Background(), task)
	if result.Status != "success" {
		t.Fatalf("status = %q, want success: %s", result.Status, result.Error)
	}
	if len(resolver.hosts) != 1 || resolver.hosts[0] != "canonical.example.com" {
		t.Fatalf("resolver hosts = %#v, want canonical hostname lookup", resolver.hosts)
	}
	if len(resolver.networks) != 1 || resolver.networks[0] != "ip" {
		t.Fatalf("resolver networks = %#v, want [ip]", resolver.networks)
	}
}

func TestProbeRunBlocksRedirectToPrivateHost(t *testing.T) {
	resolver := &fakeResolver{addresses: [][]netip.Addr{
		{publicAddress(t, "93.184.216.34")},
		{publicAddress(t, "10.0.0.1")},
	}}
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"http://private.example/health"}},
			Body:       io.NopCloser(strings.NewReader("redirect")),
			Request:    req,
		}, nil
	})
	p := &Probe{Resolver: resolver, Dialer: &fakeDialer{conn: nopConn{}}, roundTripper: transport}
	result := p.Run(context.Background(), networkTask("http"))
	if result.Status != "blocked" {
		t.Fatalf("status = %q, want blocked: %s", result.Status, result.Error)
	}
	if len(resolver.hosts) != 2 || resolver.hosts[1] != "private.example" {
		t.Fatalf("resolver hosts = %#v, want redirect host validation", resolver.hosts)
	}
}

func TestProbeRunBoundsHTTPResponseBodyAndUsesGETWithoutBody(t *testing.T) {
	body := &countingBody{remaining: 8}
	var request *http.Request
	p := &Probe{
		Resolver:     &fakeResolver{addresses: [][]netip.Addr{{publicAddress(t, "93.184.216.34")}}},
		Dialer:       &fakeDialer{conn: nopConn{}},
		MaxBodyBytes: 8,
		roundTripper: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			request = req
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body, Request: req}, nil
		}),
	}
	result := p.Run(context.Background(), networkTask("https"))
	if result.Status != "success" {
		t.Fatalf("status = %q, want success: %s", result.Status, result.Error)
	}
	if body.read > 8 {
		t.Fatalf("body read = %d, want at most 8", body.read)
	}
	if request == nil || request.Method != http.MethodGet || request.Body == nil && request.GetBody != nil {
		t.Fatalf("request = %#v, want GET without caller body", request)
	}
	if request.Body != nil {
		t.Fatal("HTTP probe supplied a request body")
	}
}

func TestProbeRunRejectsHTTPResponseBodyOverLimit(t *testing.T) {
	body := &countingBody{remaining: 64}
	p := &Probe{
		Resolver:     &fakeResolver{addresses: [][]netip.Addr{{publicAddress(t, "93.184.216.34")}}},
		Dialer:       &fakeDialer{conn: nopConn{}},
		MaxBodyBytes: 8,
		roundTripper: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body, Request: req}, nil
		}),
	}
	result := p.Run(context.Background(), networkTask("https"))
	if result.Status != "error" || !strings.Contains(result.Error, "response body exceeds limit") {
		t.Fatalf("result = %#v, want body limit error", result)
	}
}

func TestProbeRunClassifiesDialTimeout(t *testing.T) {
	resolver := &fakeResolver{addresses: [][]netip.Addr{{publicAddress(t, "93.184.216.34")}}}
	dialer := &fakeDialer{block: true}
	task := networkTask("tcp")
	task.TimeoutMS = 100
	result := (&Probe{Resolver: resolver, Dialer: dialer}).Run(context.Background(), task)
	if result.Status != "timeout" {
		t.Fatalf("status = %q, want timeout: %s", result.Status, result.Error)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type nopConn struct{}

func (nopConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (nopConn) Write(p []byte) (int, error)      { return len(p), nil }
func (nopConn) Close() error                     { return nil }
func (nopConn) LocalAddr() net.Addr              { return fakeAddr("local") }
func (nopConn) RemoteAddr() net.Addr             { return fakeAddr("remote") }
func (nopConn) SetDeadline(time.Time) error      { return nil }
func (nopConn) SetReadDeadline(time.Time) error  { return nil }
func (nopConn) SetWriteDeadline(time.Time) error { return nil }

type fakeAddr string

func (a fakeAddr) Network() string { return "tcp" }
func (a fakeAddr) String() string  { return string(a) }

func TestProbeRunHTTPKeywordAssertionSuccess(t *testing.T) {
	p := &Probe{
		Resolver: &fakeResolver{addresses: [][]netip.Addr{{publicAddress(t, "93.184.216.34")}}},
		Dialer:   &fakeDialer{conn: nopConn{}},
		roundTripper: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"status":"healthy","uptime":9999}`)),
				Request:    req,
			}, nil
		}),
	}
	task := networkTask("http")
	task.Keyword = "healthy"
	result := p.Run(context.Background(), task)
	if result.Status != "success" {
		t.Fatalf("status = %q, want success; error = %s", result.Status, result.Error)
	}
	if result.HTTP == nil || !result.HTTP.KeywordFound {
		t.Fatalf("http result = %#v, want KeywordFound = true", result.HTTP)
	}
	if result.HTTP.ContentType != "application/json" {
		t.Fatalf("content-type = %q, want application/json", result.HTTP.ContentType)
	}
}

func TestProbeRunHTTPKeywordAssertionFailure(t *testing.T) {
	p := &Probe{
		Resolver: &fakeResolver{addresses: [][]netip.Addr{{publicAddress(t, "93.184.216.34")}}},
		Dialer:   &fakeDialer{conn: nopConn{}},
		roundTripper: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"status":"maintenance"}`)),
				Request:    req,
			}, nil
		}),
	}
	task := networkTask("http")
	task.Keyword = "healthy"
	result := p.Run(context.Background(), task)
	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed", result.Status)
	}
	if !strings.Contains(result.Error, `expected keyword "healthy" not found`) {
		t.Fatalf("error = %q, want keyword not found message", result.Error)
	}
	if result.HTTP == nil || result.HTTP.KeywordFound {
		t.Fatalf("http result = %#v, want KeywordFound = false", result.HTTP)
	}
}

func TestProbeRunHTTPStatusCodeMismatch(t *testing.T) {
	p := &Probe{
		Resolver: &fakeResolver{addresses: [][]netip.Addr{{publicAddress(t, "93.184.216.34")}}},
		Dialer:   &fakeDialer{conn: nopConn{}},
		roundTripper: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("server busy")),
				Request:    req,
			}, nil
		}),
	}
	task := networkTask("http")
	task.ExpectedStatus = 200
	result := p.Run(context.Background(), task)
	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed", result.Status)
	}
	if !strings.Contains(result.Error, "expected status 200, got 503") {
		t.Fatalf("error = %q, want status mismatch message", result.Error)
	}
}

func TestParseTLSCert(t *testing.T) {
	now := time.Now()
	cert := &x509.Certificate{
		Subject:   pkix.Name{CommonName: "tz.115yu.us.ci"},
		Issuer:    pkix.Name{Organization: []string{"Let's Encrypt"}},
		DNSNames:  []string{"tz.115yu.us.ci", "*.115yu.us.ci"},
		NotBefore: now.Add(-30 * 24 * time.Hour),
		NotAfter:  now.Add(60 * 24 * time.Hour),
	}
	state := tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{cert},
		Version:          tls.VersionTLS13,
		CipherSuite:      tls.TLS_AES_128_GCM_SHA256,
	}

	result := parseTLSCert(state)
	if result == nil {
		t.Fatal("parseTLSCert returned nil")
	}
	if result.Subject != "tz.115yu.us.ci" {
		t.Fatalf("subject = %q, want tz.115yu.us.ci", result.Subject)
	}
	if result.Issuer != "Let's Encrypt" {
		t.Fatalf("issuer = %q, want Let's Encrypt", result.Issuer)
	}
	if len(result.DNSNames) != 2 || result.DNSNames[0] != "tz.115yu.us.ci" {
		t.Fatalf("dns_names = %#v", result.DNSNames)
	}
	if result.DaysLeft < 58 || result.DaysLeft > 61 {
		t.Fatalf("days_left = %d, want ~60", result.DaysLeft)
	}
	if result.IsExpired {
		t.Fatal("is_expired = true, want false")
	}
	if result.ExpiringSoon {
		t.Fatal("expiring_soon = true, want false")
	}
	if result.Protocol != "TLS 1.3" {
		t.Fatalf("protocol = %q, want TLS 1.3", result.Protocol)
	}
	if !strings.Contains(result.CipherSuite, "TLS_AES_128_GCM_SHA256") {
		t.Fatalf("cipher_suite = %q, want TLS_AES_128_GCM_SHA256", result.CipherSuite)
	}
}

func TestParseTLSCertExpiredAndExpiringSoon(t *testing.T) {
	now := time.Now()

	// Expired certificate
	expiredCert := &x509.Certificate{
		Subject:   pkix.Name{CommonName: "expired.example.com"},
		Issuer:    pkix.Name{CommonName: "DigiCert"},
		NotBefore: now.Add(-100 * 24 * time.Hour),
		NotAfter:  now.Add(-2 * 24 * time.Hour),
	}
	expiredResult := parseTLSCert(tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{expiredCert},
		Version:          tls.VersionTLS12,
		CipherSuite:      tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
	})
	if !expiredResult.IsExpired {
		t.Fatal("expired cert: is_expired = false, want true")
	}
	if expiredResult.DaysLeft >= 0 {
		t.Fatalf("expired cert: days_left = %d, want < 0", expiredResult.DaysLeft)
	}
	if expiredResult.ExpiringSoon {
		t.Fatal("expired cert: expiring_soon = true, want false (already expired)")
	}

	// Expiring soon certificate (5 days left)
	soonCert := &x509.Certificate{
		Subject:   pkix.Name{CommonName: "soon.example.com"},
		Issuer:    pkix.Name{CommonName: "Cloudflare"},
		NotBefore: now.Add(-85 * 24 * time.Hour),
		NotAfter:  now.Add(5 * 24 * time.Hour),
	}
	soonResult := parseTLSCert(tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{soonCert},
		Version:          tls.VersionTLS13,
		CipherSuite:      tls.TLS_AES_256_GCM_SHA384,
	})
	if soonResult.IsExpired {
		t.Fatal("soon cert: is_expired = true, want false")
	}
	if !soonResult.ExpiringSoon {
		t.Fatal("soon cert: expiring_soon = false, want true")
	}
	if soonResult.DaysLeft != 4 && soonResult.DaysLeft != 5 {
		t.Fatalf("soon cert: days_left = %d, want ~5", soonResult.DaysLeft)
	}
}

func TestProbeRunDNSResultRecords(t *testing.T) {
	resolver := &fakeResolver{addresses: [][]netip.Addr{
		{publicAddress(t, "1.1.1.1"), publicAddress(t, "1.0.0.1")},
	}}
	task := networkTask("dns")
	task.DNSType = "A"
	p := &Probe{Resolver: resolver, Dialer: &fakeDialer{conn: nopConn{}}}
	result := p.Run(context.Background(), task)
	if result.Status != "success" {
		t.Fatalf("status = %q, want success; error = %s", result.Status, result.Error)
	}
	if result.DNS == nil {
		t.Fatal("dns result is nil")
	}
	if len(result.DNS.Records) != 2 || result.DNS.Records[0] != "1.1.1.1" || result.DNS.Records[1] != "1.0.0.1" {
		t.Fatalf("dns records = %#v, want [1.1.1.1, 1.0.0.1]", result.DNS.Records)
	}
}
