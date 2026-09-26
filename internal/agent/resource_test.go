package agent

import (
	"math"
	"testing"

	"github.com/probewatch/probewatch/internal/protocol"
)

func TestParseMemInfo(t *testing.T) {
	stats, err := parseMemInfo("MemTotal: 100 kB\nMemAvailable: 40 kB\nSwapTotal: 20 kB\nSwapFree: 5 kB\n")
	if err != nil {
		t.Fatal(err)
	}
	if stats.total != 102400 || stats.available != 40960 || stats.used != 61440 || stats.swapTotal != 20480 || stats.swapUsed != 15360 {
		t.Fatalf("stats = %#v", stats)
	}

	stats, err = parseMemInfo("MemTotal: 100 kB\nMemFree: 10 kB\nBuffers: 20 kB\nCached: 30 kB\nnot valid\nBad: nope kB\n")
	if err != nil {
		t.Fatal(err)
	}
	if stats.available != 61440 || stats.used != 40960 {
		t.Fatalf("fallback stats = %#v", stats)
	}
	if _, err := parseMemInfo("MemAvailable: 1 kB\n"); err == nil {
		t.Fatal("missing MemTotal accepted")
	}
}

func TestParseLoadAvg(t *testing.T) {
	got, err := parseLoadAvg("1.25 0.50 0.00 1/100 123")
	if err != nil || got != [3]float64{1.25, 0.5, 0} {
		t.Fatalf("got %v, err %v", got, err)
	}
	for _, input := range []string{"", "1 2", "1 -2 3", "1 nope 3"} {
		if _, err := parseLoadAvg(input); err == nil {
			t.Errorf("parseLoadAvg(%q) accepted", input)
		}
	}
}

func TestParseNetworkCounters(t *testing.T) {
	data := "Inter-| Receive | Transmit\n face |bytes packets errs drop fifo frame compressed multicast|bytes packets errs drop fifo colls carrier compressed\n eth0: 10 1 0 0 0 0 0 0 20 2 0 0 0 0 0 0\n lo: 100 1 0 0 0 0 0 0 200 2 0 0 0 0 0 0\n eth1: 3 1 0 0 0 0 0 0 4 1 0 0 0 0 0 0\n"
	got, err := parseNetworkCounters(data)
	if err != nil || got != (networkStats{rx: 13, tx: 24}) {
		t.Fatalf("got %#v, err %v", got, err)
	}
	for _, input := range []string{"", "lo: 1 1 0 0 0 0 0 0 2", "eth0: nope 1 0 0 0 0 0 0 2"} {
		if _, err := parseNetworkCounters(input); err == nil {
			t.Errorf("parseNetworkCounters(%q) accepted", input)
		}
	}
}

func TestCPUTrackerUsesCounterDeltas(t *testing.T) {
	values := []cpuStats{{total: 100, idle: 40}, {total: 200, idle: 80}}
	index := 0
	tracker := &cpuTracker{read: func() (cpuStats, error) {
		value := values[index]
		index++
		return value, nil
	}}
	if got, err := tracker.Sample(); err != nil || got != 0 {
		t.Fatalf("first sample = %v, %v", got, err)
	}
	if got, err := tracker.Sample(); err != nil || math.Abs(got-60) > 1e-9 {
		t.Fatalf("delta sample = %v, %v", got, err)
	}
}

func TestCollectResourceKeepsStartedAt(t *testing.T) {
	first := collectResourceWith(1234, nil, nil, nil, nil, nil)
	second := collectResourceWith(1234, nil, nil, nil, nil, nil)
	if first.StartedAt != 1234 || second.StartedAt != 1234 {
		t.Fatalf("started_at = %d, %d", first.StartedAt, second.StartedAt)
	}
}

type mockSocketCollector struct {
	stats *protocol.SocketStats
	ports []protocol.ListeningPort
}

func (m *mockSocketCollector) Collect() (*protocol.SocketStats, []protocol.ListeningPort, error) {
	return m.stats, m.ports, nil
}

type mockHealthCollector struct {
	info *protocol.HostHealthInfo
}

func (m *mockHealthCollector) Collect(snap *protocol.ResourceSnapshot) *protocol.HostHealthInfo {
	return m.info
}

func TestCollectResourceWithSockets(t *testing.T) {
	mock := &mockSocketCollector{
		stats: &protocol.SocketStats{
			TCPEstablished: 10,
			TCPListen:      5,
		},
		ports: []protocol.ListeningPort{
			{Proto: "tcp", Port: 80, BindIP: "0.0.0.0", Process: "nginx", PID: 123, IsPublic: true},
		},
	}
	snap := collectResourceWith(1234, nil, nil, nil, mock, nil)
	if snap.SocketStats == nil || snap.SocketStats.TCPEstablished != 10 {
		t.Fatalf("expected TCPEstablished = 10, got %v", snap.SocketStats)
	}
	if len(snap.ListeningPorts) != 1 || snap.ListeningPorts[0].Port != 80 {
		t.Fatalf("expected 1 port with port 80, got %v", snap.ListeningPorts)
	}
}

func TestCollectResourceWithHealth(t *testing.T) {
	mock := &mockHealthCollector{
		info: &protocol.HostHealthInfo{
			HealthScore:  95,
			HealthStatus: "optimal",
		},
	}
	snap := collectResourceWith(1234, nil, nil, nil, nil, mock)
	if snap.HealthInfo == nil || snap.HealthInfo.HealthScore != 95 {
		t.Fatalf("expected HealthScore = 95, got %v", snap.HealthInfo)
	}
}

func TestParseProcStat(t *testing.T) {
	got, err := parseProcStat("cpu 10 20 30 40 5 0 0 0")
	if err != nil || got != (cpuStats{total: 105, idle: 45}) {
		t.Fatalf("got %#v, err %v", got, err)
	}
	for _, input := range []string{"", "cpu 1 2 3", "cpux 1 2 3 4", "cpu 1 nope 3 4", "cpu 0 0 0 0 0 0 0 0"} {
		if _, err := parseProcStat(input); err == nil {
			t.Errorf("parseProcStat(%q) accepted", input)
		}
	}
	if _, err := parseProcStat("cpu 1 2 3 10 0 0 0 0"); err != nil {
		t.Fatal(err)
	}
}
