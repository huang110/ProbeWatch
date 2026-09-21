package monitor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/probewatch/probewatch/internal/protocol"
	"github.com/probewatch/probewatch/internal/security"
)

const (
	mtrDefaultTimeout     = 10 * time.Second
	mtrMaxTimeout         = 30 * time.Second
	mtrDefaultHopTimeout  = 1 * time.Second
	mtrMaxHopTimeout      = 5 * time.Second
	mtrDefaultResultBytes = 32 << 10
	mtrMaxResultBytes     = 256 << 10
)

var ErrMTRUnsupported = errors.New("mtr transport unsupported")
var ErrMTRPermission = errors.New("mtr transport permission denied")

// MTRTransport performs one bounded TTL probe. It must return only observed,
// passive hop data; it must not perform route discovery through shell commands.
type MTRTransport interface {
	Probe(ctx context.Context, destination netip.Addr, ttl int, timeout time.Duration) (protocol.MTRHop, error)
}

// MTRMonitor orchestrates bounded MTR probes. Transport is injectable so the
// orchestration can be tested without requiring privileged raw sockets.
type MTRMonitor struct {
	Transport      MTRTransport
	Resolver       Resolver
	Timeout        time.Duration
	HopTimeout     time.Duration
	MaxResultBytes int
}

// UnsupportedMTRTransport is the explicit portable default. Platforms may
// provide an implementation without changing orchestration or Agent dispatch.
type UnsupportedMTRTransport struct{}

func (UnsupportedMTRTransport) Probe(context.Context, netip.Addr, int, time.Duration) (protocol.MTRHop, error) {
	return protocol.MTRHop{}, ErrMTRUnsupported
}

// Run executes a bounded route trace and always returns a structured result.
func (m *MTRMonitor) Run(parent context.Context, task protocol.CheckTask) protocol.MTRResult {
	started := time.Now()
	result := protocol.MTRResult{Host: task.Host, CheckedAt: started.Unix()}
	if err := task.Validate(); err != nil {
		result.Error = formatMTRFailure("invalid", err)
		return result
	}
	if task.Kind != "mtr" {
		result.Error = formatMTRFailure("invalid", errors.New("task kind must be mtr"))
		return result
	}
	if err := security.ValidateHost(task.Host); err != nil {
		result.Error = formatMTRFailure("blocked", err)
		return result
	}

	total := m.totalTimeout(task.TimeoutMS)
	ctx, cancel := context.WithTimeout(parent, total)
	defer cancel()
	addresses, err := m.resolver().LookupNetIP(ctx, "ip", task.Host)
	if err != nil {
		result.Error = formatMTRFailure(m.errorClass(ctx, err), err)
		return result
	}
	if err := security.ValidateResolvedIPs(addresses); err != nil {
		result.Error = formatMTRFailure("blocked", err)
		return result
	}
	// Resolver order is not stable across platforms; use a canonical destination.
	sort.Slice(addresses, func(i, j int) bool { return addresses[i].String() < addresses[j].String() })
	result.DestinationIP = addresses[0].String()

	transport := m.Transport
	if transport == nil {
		transport = defaultMTRTransport()
	}
	for ttl := 1; ttl <= task.MaxHops; ttl++ {
		if err := ctx.Err(); err != nil {
			result.Error = formatMTRFailure(m.errorClass(ctx, err), err)
			break
		}
		hopCtx, hopCancel := context.WithTimeout(ctx, m.hopTimeout())
		hop, probeErr := transport.Probe(hopCtx, addresses[0], ttl, m.hopTimeout())
		hopCancel()
		if hop.TTL == 0 {
			hop.TTL = ttl
		}
		if hop.TTL != ttl || hop.TTL < 1 || hop.TTL > 30 {
			probeErr = fmt.Errorf("transport returned invalid ttl %d", hop.TTL)
		}
		if probeErr != nil {
			if errors.Is(probeErr, context.DeadlineExceeded) || errors.Is(hopCtx.Err(), context.DeadlineExceeded) {
				result.Error = formatMTRFailure("timeout", probeErr)
			} else {
				result.Error = formatMTRFailure(m.errorClass(ctx, probeErr), probeErr)
			}
			break
		}
		result.Hops = append(result.Hops, hop)
		if hop.IP != "" && hop.IP == result.DestinationIP {
			result.Reached = true
			break
		}
	}
	if result.Error == "" && !result.Reached {
		result.Error = formatMTRFailure("incomplete", errors.New("destination not reached within max hops"))
	}
	result.Fingerprint = mtrFingerprint(result)
	if err := enforceMTRSize(&result, m.maxResultBytes()); err != nil {
		result.Error = formatMTRFailure("size_limit", err)
		result.Hops = nil
		result.Fingerprint = mtrFingerprint(result)
	}
	return result
}

func (m *MTRMonitor) resolver() Resolver {
	if m.Resolver != nil {
		return m.Resolver
	}
	return net.DefaultResolver
}

func (m *MTRMonitor) totalTimeout(taskMS int) time.Duration {
	t := m.Timeout
	if t <= 0 {
		t = mtrDefaultTimeout
	}
	if taskMS > 0 && time.Duration(taskMS)*time.Millisecond < t {
		t = time.Duration(taskMS) * time.Millisecond
	}
	if t > mtrMaxTimeout {
		t = mtrMaxTimeout
	}
	return t
}

func (m *MTRMonitor) hopTimeout() time.Duration {
	t := m.HopTimeout
	if t <= 0 {
		t = mtrDefaultHopTimeout
	}
	if t > mtrMaxHopTimeout {
		t = mtrMaxHopTimeout
	}
	return t
}

func (m *MTRMonitor) maxResultBytes() int {
	if m.MaxResultBytes <= 0 {
		return mtrDefaultResultBytes
	}
	if m.MaxResultBytes > mtrMaxResultBytes {
		return mtrMaxResultBytes
	}
	return m.MaxResultBytes
}

func (m *MTRMonitor) errorClass(ctx context.Context, err error) string {
	if errors.Is(err, ErrMTRPermission) {
		return "permission_denied"
	}
	if errors.Is(err, ErrMTRUnsupported) {
		return "unsupported"
	}
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "timeout"
	}
	return "error"
}

func formatMTRFailure(class string, err error) string {
	message := strings.Join(strings.Fields(err.Error()), " ")
	if len(message) > 512 {
		message = message[:512]
	}
	return class + ": " + message
}

type mtrFingerprintHop struct {
	TTL      int    `json:"ttl"`
	IP       string `json:"ip,omitempty"`
	TimedOut bool   `json:"timed_out,omitempty"`
}

func mtrFingerprint(result protocol.MTRResult) string {
	hops := make([]mtrFingerprintHop, 0, len(result.Hops))
	for _, hop := range result.Hops {
		hops = append(hops, mtrFingerprintHop{TTL: hop.TTL, IP: hop.IP, TimedOut: hop.TimedOut})
	}
	payload, _ := json.Marshal(struct {
		Destination string              `json:"destination"`
		Reached     bool                `json:"reached"`
		Hops        []mtrFingerprintHop `json:"hops"`
	}{result.DestinationIP, result.Reached, hops})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func enforceMTRSize(result *protocol.MTRResult, limit int) error {
	b, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if len(b) > limit {
		return fmt.Errorf("result exceeds %d bytes", limit)
	}
	return nil
}
