package monitor

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/probewatch/probewatch/internal/protocol"
)

type fakeMTRResolver struct {
	addresses []netip.Addr
	err       error
}

func (r fakeMTRResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.addresses, nil
}

type fakeMTRTransport struct {
	hops  map[int]protocol.MTRHop
	err   map[int]error
	block bool
	calls []int
	dests []netip.Addr
}

func (t *fakeMTRTransport) Probe(ctx context.Context, dest netip.Addr, ttl int, _ time.Duration) (protocol.MTRHop, error) {
	t.calls = append(t.calls, ttl)
	t.dests = append(t.dests, dest)
	if t.block {
		<-ctx.Done()
		return protocol.MTRHop{}, ctx.Err()
	}
	if err := t.err[ttl]; err != nil {
		return protocol.MTRHop{}, err
	}
	return t.hops[ttl], nil
}

func mtrTask() protocol.CheckTask {
	return protocol.CheckTask{ID: "mtr-1", Kind: "mtr", Host: "example.com", Port: 443, TimeoutMS: 1000, MaxHops: 3, IntervalSeconds: 10, Enabled: true}
}
func TestMTRMonitorMaxHopsAndPartialPath(t *testing.T) {
	transport := &fakeMTRTransport{hops: map[int]protocol.MTRHop{1: {IP: "192.0.2.1"}, 2: {IP: "192.0.2.2"}, 3: {IP: "192.0.2.3"}}}
	result := (&MTRMonitor{Transport: transport, Resolver: fakeMTRResolver{addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}}}).Run(context.Background(), mtrTask())
	if len(result.Hops) != 3 || result.Reached {
		t.Fatalf("hops=%d reached=%v", len(result.Hops), result.Reached)
	}
	if result.Fingerprint == "" || !strings.HasPrefix(result.Error, "incomplete:") {
		t.Fatalf("result=%+v", result)
	}
}
func TestMTRMonitorReachedAndStableFingerprint(t *testing.T) {
	transport := &fakeMTRTransport{hops: map[int]protocol.MTRHop{1: {IP: "93.184.216.34"}}}
	monitor := &MTRMonitor{Transport: transport, Resolver: fakeMTRResolver{addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}}}
	task := mtrTask()
	task.MaxHops = 1
	a := monitor.Run(context.Background(), task)
	b := monitor.Run(context.Background(), task)
	if !a.Reached || a.Error != "" || a.Fingerprint != b.Fingerprint {
		t.Fatalf("a=%+v b=%+v", a, b)
	}
}
func TestMTRMonitorHopTimeout(t *testing.T) {
	transport := &fakeMTRTransport{block: true}
	result := (&MTRMonitor{Transport: transport, Resolver: fakeMTRResolver{addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}}, HopTimeout: 10 * time.Millisecond}).Run(context.Background(), mtrTask())
	if !strings.HasPrefix(result.Error, "timeout:") {
		t.Fatalf("error=%q", result.Error)
	}
}
func TestMTRMonitorCancel(t *testing.T) {
	transport := &fakeMTRTransport{block: true}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := (&MTRMonitor{Transport: transport, Resolver: fakeMTRResolver{addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}}}).Run(ctx, mtrTask())
	if !strings.HasPrefix(result.Error, "canceled:") {
		t.Fatalf("error=%q", result.Error)
	}
}
func TestMTRMonitorBlocksPrivateTarget(t *testing.T) {
	transport := &fakeMTRTransport{}
	result := (&MTRMonitor{Transport: transport, Resolver: fakeMTRResolver{addresses: []netip.Addr{netip.MustParseAddr("10.0.0.1")}}}).Run(context.Background(), mtrTask())
	if !strings.HasPrefix(result.Error, "blocked:") || len(transport.calls) != 0 {
		t.Fatalf("result=%+v calls=%v", result, transport.calls)
	}
}
func TestMTRMonitorUnsupportedIsStructured(t *testing.T) {
	result := (&MTRMonitor{Transport: UnsupportedMTRTransport{}, Resolver: fakeMTRResolver{addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}}}).Run(context.Background(), mtrTask())
	if !strings.HasPrefix(result.Error, "unsupported:") || errors.Is(errors.New(result.Error), ErrMTRUnsupported) {
		t.Fatalf("error=%q", result.Error)
	}
}

func TestMTRMonitorPureIPv4(t *testing.T) {
	transport := &fakeMTRTransport{hops: map[int]protocol.MTRHop{1: {IP: "8.8.8.8"}}}
	task := protocol.CheckTask{ID: "mtr-pure-v4", Kind: "mtr", Host: "8.8.8.8", Port: 443, TimeoutMS: 1000, MaxHops: 1, IntervalSeconds: 10, Enabled: true}
	result := (&MTRMonitor{Transport: transport, Resolver: fakeMTRResolver{addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8")}}}).Run(context.Background(), task)
	if !result.Reached || result.Error != "" {
		t.Fatalf("expected reached, got error=%q result=%+v", result.Error, result)
	}
	if result.DestinationIP != "8.8.8.8" {
		t.Fatalf("expected DestinationIP 8.8.8.8, got %q", result.DestinationIP)
	}
	if len(transport.dests) == 0 || !transport.dests[0].Is4() {
		t.Fatalf("expected pure IPv4 destination passed to transport, got %+v", transport.dests)
	}
}

func TestMTRMonitorIPv4MappedIPv6(t *testing.T) {
	transport := &fakeMTRTransport{hops: map[int]protocol.MTRHop{1: {IP: "8.8.8.8"}}}
	task := protocol.CheckTask{ID: "mtr-mapped-v6", Kind: "mtr", Host: "8.8.8.8", Port: 443, TimeoutMS: 1000, MaxHops: 1, IntervalSeconds: 10, Enabled: true}
	// Simulate resolver returning an IPv4-mapped IPv6 address (::ffff:8.8.8.8)
	mappedAddr := netip.MustParseAddr("::ffff:8.8.8.8")
	result := (&MTRMonitor{Transport: transport, Resolver: fakeMTRResolver{addresses: []netip.Addr{mappedAddr}}}).Run(context.Background(), task)
	if !result.Reached || result.Error != "" {
		t.Fatalf("expected reached after unmapping, got error=%q result=%+v", result.Error, result)
	}
	if result.DestinationIP != "8.8.8.8" {
		t.Fatalf("expected unmapped DestinationIP 8.8.8.8, got %q", result.DestinationIP)
	}
	if len(transport.dests) == 0 || !transport.dests[0].Is4() || transport.dests[0].Is6() {
		t.Fatalf("expected transport to receive unmapped pure IPv4, got %+v", transport.dests)
	}
}

func TestMTRMonitorNativeIPv6(t *testing.T) {
	transport := &fakeMTRTransport{hops: map[int]protocol.MTRHop{1: {IP: "2001:4860:4860::8888"}}}
	task := protocol.CheckTask{ID: "mtr-native-v6", Kind: "mtr", Host: "2001:4860:4860::8888", Port: 443, TimeoutMS: 1000, MaxHops: 1, IntervalSeconds: 10, Enabled: true}
	nativeV6 := netip.MustParseAddr("2001:4860:4860::8888")
	result := (&MTRMonitor{Transport: transport, Resolver: fakeMTRResolver{addresses: []netip.Addr{nativeV6}}}).Run(context.Background(), task)
	if result.DestinationIP != "2001:4860:4860::8888" {
		t.Fatalf("expected native IPv6 DestinationIP 2001:4860:4860::8888, got %q", result.DestinationIP)
	}
	if len(transport.dests) == 0 || !transport.dests[0].Is6() || transport.dests[0].Is4() {
		t.Fatalf("expected transport to receive native IPv6, got %+v", transport.dests)
	}
}

func TestMTRMonitorDomainResolvedToIPv4(t *testing.T) {
	transport := &fakeMTRTransport{hops: map[int]protocol.MTRHop{1: {IP: "1.1.1.1"}}}
	task := protocol.CheckTask{ID: "mtr-domain", Kind: "mtr", Host: "one.one.one.one", Port: 443, TimeoutMS: 1000, MaxHops: 1, IntervalSeconds: 10, Enabled: true}
	result := (&MTRMonitor{Transport: transport, Resolver: fakeMTRResolver{addresses: []netip.Addr{netip.MustParseAddr("1.1.1.1")}}}).Run(context.Background(), task)
	if !result.Reached || result.Error != "" {
		t.Fatalf("expected reached, got error=%q result=%+v", result.Error, result)
	}
	if result.DestinationIP != "1.1.1.1" {
		t.Fatalf("expected DestinationIP 1.1.1.1, got %q", result.DestinationIP)
	}
}

func TestMTRMonitorUnreachableOrTimeout(t *testing.T) {
	transport := &fakeMTRTransport{
		hops: map[int]protocol.MTRHop{
			1: {TTL: 1, TimedOut: true},
			2: {TTL: 2, TimedOut: true},
			3: {TTL: 3, TimedOut: true},
		},
	}
	task := protocol.CheckTask{ID: "mtr-unreach", Kind: "mtr", Host: "8.8.8.8", Port: 443, TimeoutMS: 1000, MaxHops: 3, IntervalSeconds: 10, Enabled: true}
	result := (&MTRMonitor{Transport: transport, Resolver: fakeMTRResolver{addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8")}}}).Run(context.Background(), task)
	if result.Reached {
		t.Fatal("expected reached to be false for unreachable/timeout target")
	}
	if len(result.Hops) != 3 {
		t.Fatalf("expected 3 timed out hops, got %d", len(result.Hops))
	}
	if !strings.HasPrefix(result.Error, "incomplete:") {
		t.Fatalf("expected incomplete failure, got %q", result.Error)
	}
}

func TestMTRMonitorInvalidAddress(t *testing.T) {
	// Case 1: Invalid host syntax
	taskInvalid := protocol.CheckTask{ID: "mtr-invalid-host", Kind: "mtr", Host: "invalid host/with/slash", Port: 443, TimeoutMS: 1000, MaxHops: 1, IntervalSeconds: 10, Enabled: true}
	result := (&MTRMonitor{Transport: &fakeMTRTransport{}}).Run(context.Background(), taskInvalid)
	if !strings.HasPrefix(result.Error, "blocked:") && !strings.HasPrefix(result.Error, "invalid:") {
		t.Fatalf("expected blocked or invalid error for malformed host, got %q", result.Error)
	}

	// Case 2: Resolver error (e.g. NXDOMAIN or DNS failure)
	taskLookupErr := protocol.CheckTask{ID: "mtr-nxdomain", Kind: "mtr", Host: "nonexistent.example.local", Port: 443, TimeoutMS: 1000, MaxHops: 1, IntervalSeconds: 10, Enabled: true}
	resLookupErr := (&MTRMonitor{Transport: &fakeMTRTransport{}, Resolver: fakeMTRResolver{err: errors.New("no such host")}}).Run(context.Background(), taskLookupErr)
	if !strings.HasPrefix(resLookupErr.Error, "error:") {
		t.Fatalf("expected error prefix for resolver error, got %q", resLookupErr.Error)
	}

	// Case 3: Empty resolved addresses list
	resEmpty := (&MTRMonitor{Transport: &fakeMTRTransport{}, Resolver: fakeMTRResolver{addresses: []netip.Addr{}}}).Run(context.Background(), mtrTask())
	if !strings.HasPrefix(resEmpty.Error, "blocked:") {
		t.Fatalf("expected blocked prefix for empty addresses, got %q", resEmpty.Error)
	}
}
