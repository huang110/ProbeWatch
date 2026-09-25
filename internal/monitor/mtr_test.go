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

type fakeMTRResolver struct{ addresses []netip.Addr }

func (r fakeMTRResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return r.addresses, nil
}

type fakeMTRTransport struct {
	hops  map[int]protocol.MTRHop
	err   map[int]error
	block bool
	calls []int
}

func (t *fakeMTRTransport) Probe(ctx context.Context, _ netip.Addr, ttl int, _ time.Duration) (protocol.MTRHop, error) {
	t.calls = append(t.calls, ttl)
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
