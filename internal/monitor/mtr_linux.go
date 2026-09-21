//go:build linux

package monitor

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"syscall"
	"time"
	"unsafe"

	"github.com/probewatch/probewatch/internal/protocol"
)

// defaultMTRTransport uses a TCP SYN with increasing IPv4 TTLs and a raw ICMP
// receive socket. It does not invoke mtr/traceroute or any shell command.
func defaultMTRTransport() MTRTransport { return linuxTCPMTRTransport{} }

type linuxTCPMTRTransport struct{}

func (linuxTCPMTRTransport) Probe(ctx context.Context, destination netip.Addr, ttl int, timeout time.Duration) (protocol.MTRHop, error) {
	if !destination.Is4() || ttl < 1 || ttl > 30 {
		return protocol.MTRHop{}, fmt.Errorf("unsupported destination or ttl")
	}
	if err := ctx.Err(); err != nil {
		return protocol.MTRHop{}, err
	}
	icmpFD, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_RAW, syscall.IPPROTO_ICMP)
	if err != nil {
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
			return protocol.MTRHop{}, fmt.Errorf("%w: %v", ErrMTRPermission, err)
		}
		return protocol.MTRHop{}, err
	}
	defer syscall.Close(icmpFD)
	tcpFD, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, syscall.IPPROTO_TCP)
	if err != nil {
		return protocol.MTRHop{}, err
	}
	defer syscall.Close(tcpFD)
	if err := syscall.SetsockoptInt(tcpFD, syscall.IPPROTO_IP, syscall.IP_TTL, ttl); err != nil {
		return protocol.MTRHop{}, err
	}
	if err := syscall.SetNonblock(tcpFD, true); err != nil {
		return protocol.MTRHop{}, err
	}
	addr := destination.As4()
	sa := &syscall.SockaddrInet4{Port: 443, Addr: addr}
	started := time.Now()
	connectErr := syscall.Connect(tcpFD, sa)
	if connectErr != nil && connectErr != syscall.EINPROGRESS && connectErr != syscall.EWOULDBLOCK {
		return protocol.MTRHop{}, connectErr
	}
	local, err := syscall.Getsockname(tcpFD)
	if err != nil {
		return protocol.MTRHop{}, err
	}
	localPort := 0
	if localAddr, ok := local.(*syscall.SockaddrInet4); ok {
		localPort = localAddr.Port
	}
	deadline := time.Now().Add(timeout)
	for {
		if err := ctx.Err(); err != nil {
			return protocol.MTRHop{TTL: ttl}, err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return protocol.MTRHop{TTL: ttl, TimedOut: true}, nil
		}
		sec := remaining / time.Second
		usec := (remaining - sec*time.Second) / time.Microsecond
		tv := syscall.Timeval{Sec: int64(sec), Usec: int64(usec)}
		readSet := syscall.FdSet{}
		writeSet := syscall.FdSet{}
		fdSet(icmpFD, &readSet)
		fdSet(tcpFD, &writeSet)
		maxFD := icmpFD
		if tcpFD > maxFD {
			maxFD = tcpFD
		}
		n, err := syscall.Select(maxFD+1, &readSet, &writeSet, nil, &tv)
		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			return protocol.MTRHop{TTL: ttl}, err
		}
		if n == 0 {
			return protocol.MTRHop{TTL: ttl, TimedOut: true}, nil
		}
		if fdIsSet(tcpFD, &writeSet) {
			soErr, err := syscall.GetsockoptInt(tcpFD, syscall.SOL_SOCKET, syscall.SO_ERROR)
			if err != nil {
				return protocol.MTRHop{TTL: ttl}, err
			}
			if soErr == 0 || soErr == int(syscall.ECONNREFUSED) {
				return protocol.MTRHop{TTL: ttl, IP: destination.String(), LatencyMS: time.Since(started).Milliseconds()}, nil
			}
		}
		if fdIsSet(icmpFD, &readSet) {
			var buf [1500]byte
			n, from, err := syscall.Recvfrom(icmpFD, buf[:], 0)
			if err != nil {
				if err == syscall.EINTR {
					continue
				}
				return protocol.MTRHop{TTL: ttl}, err
			}
			if hopIP, reached := parseICMP(buf[:n], from, destination, localPort); reached {
				return protocol.MTRHop{TTL: ttl, IP: hopIP, LatencyMS: time.Since(started).Milliseconds()}, nil
			}
		}
	}
}

func fdSet(fd int, set *syscall.FdSet) {
	set.Bits[fd/(8*int(unsafe.Sizeof(uintptr(0))))] |= 1 << uint(fd%(8*int(unsafe.Sizeof(uintptr(0)))))
}
func fdIsSet(fd int, set *syscall.FdSet) bool {
	return set.Bits[fd/(8*int(unsafe.Sizeof(uintptr(0))))]&(1<<uint(fd%(8*int(unsafe.Sizeof(uintptr(0)))))) != 0
}

func parseICMP(packet []byte, from syscall.Sockaddr, destination netip.Addr, localPort int) (string, bool) {
	if len(packet) < 28 {
		return "", false
	}
	if packet[0]>>4 == 4 {
		ihl := int(packet[0]&0x0f) * 4
		if ihl < 20 || len(packet) < ihl+28 {
			return "", false
		}
		packet = packet[ihl:]
	}
	if packet[0] != 11 && !(packet[0] == 3 && packet[1] == 3) {
		return "", false
	}
	if packet[0] == 3 && packet[1] == 3 {
		return destination.String(), true
	}
	if len(packet) < 36 || binary.BigEndian.Uint16(packet[28:30]) != uint16(localPort) {
		return "", false
	}
	if sa, ok := from.(*syscall.SockaddrInet4); ok {
		return netip.AddrFrom4(sa.Addr).String(), true
	}
	return "", false
}
