package agent

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/probewatch/probewatch/internal/protocol"
)

const agentVersion = "0.2.0"

func collectResource() protocol.ResourceSnapshot {
	return collectResourceWith(processStartTime(), newCPUTracker())
}

func collectResourceWith(startedAt int64, cpu cpuSampler) protocol.ResourceSnapshot {
	resource := protocol.ResourceSnapshot{
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		AgentVersion: agentVersion,
		StartedAt:    startedAt,
	}
	if hostname, err := os.Hostname(); err == nil {
		resource.Hostname = boundedIdentity(hostname)
	}
	if kernel, err := readFirstLine("/proc/sys/kernel/osrelease"); err == nil {
		resource.Kernel = boundedIdentity(kernel)
	}
	if runtime.GOOS != "linux" {
		return resource
	}
	if cpu != nil {
		if value, err := cpu.Sample(); err == nil {
			resource.CPUPercent = value
		}
	}
	if memory, err := readMemInfo(); err == nil {
		resource.MemoryTotalBytes = memory.total
		resource.MemoryAvailableBytes = memory.available
		resource.MemoryUsedBytes = memory.used
		resource.SwapTotalBytes = memory.swapTotal
		resource.SwapUsedBytes = memory.swapUsed
	}
	if load, err := readLoadAvg(); err == nil {
		resource.Load1, resource.Load5, resource.Load15 = load[0], load[1], load[2]
	}
	if filesystem, err := rootFilesystemUsage(); err == nil {
		resource.FilesystemTotalBytes = filesystem.total
		resource.FilesystemUsedBytes = filesystem.used
	}
	if network, err := readNetworkCounters(); err == nil {
		resource.NetworkRxBytes, resource.NetworkTxBytes = network.rx, network.tx
		resource.Interfaces = network.interfaces
		resource.IPv4 = network.primaryIPv4
		resource.IPv6 = network.primaryIPv6
	}
	if tcp, udp, err := readSocketCounts(); err == nil {
		resource.TCPConnCount = tcp
		resource.UDPConnCount = udp
	}
	if procs, err := readProcessCount(); err == nil {
		resource.ProcessCount = procs
	}
	return resource
}

func boundedIdentity(value string) string {
	value = strings.TrimSpace(value)
	if len([]byte(value)) > 128 {
		return string([]byte(value)[:128])
	}
	return value
}

func readFirstLine(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	line := bufio.NewScanner(file)
	if !line.Scan() {
		if err := line.Err(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("empty file")
	}
	return line.Text(), nil
}

type memoryStats struct{ total, available, used, swapTotal, swapUsed uint64 }

func readMemInfo() (memoryStats, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return memoryStats{}, err
	}
	return parseMemInfo(string(data))
}

func parseMemInfo(data string) (memoryStats, error) {
	values := make(map[string]uint64)
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		if len(fields) > 2 && fields[2] == "kB" {
			value *= 1024
		}
		values[strings.TrimSuffix(fields[0], ":")] = value
	}
	total := values["MemTotal"]
	available := values["MemAvailable"]
	if available == 0 {
		available = values["MemFree"] + values["Buffers"] + values["Cached"]
	}
	used := uint64(0)
	if total > available {
		used = total - available
	}
	swapTotal := values["SwapTotal"]
	swapFree := values["SwapFree"]
	swapUsed := uint64(0)
	if swapTotal > swapFree {
		swapUsed = swapTotal - swapFree
	}
	if total == 0 {
		return memoryStats{}, fmt.Errorf("MemTotal missing")
	}
	return memoryStats{total: total, available: available, used: used, swapTotal: swapTotal, swapUsed: swapUsed}, nil
}

type ifaceIPs struct {
	ipv4 string
	ipv6 string
}

func getInterfaceIPs() map[string]ifaceIPs {
	ips := make(map[string]ifaceIPs)
	ifaces, err := net.Interfaces()
	if err != nil {
		return ips
	}
	for _, ifi := range ifaces {
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		var v4, v6 string
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			if ipv4 := ip.To4(); ipv4 != nil && v4 == "" {
				v4 = ipv4.String()
			} else if ip.To16() != nil && v6 == "" {
				if ip.IsGlobalUnicast() && !strings.HasPrefix(ip.String(), "fe80:") {
					v6 = ip.String()
				}
			}
		}
		ips[ifi.Name] = ifaceIPs{ipv4: v4, ipv6: v6}
	}
	return ips
}

type networkFullStats struct {
	rx, tx      uint64
	interfaces  []protocol.InterfaceStat
	primaryIPv4 string
	primaryIPv6 string
}

func readNetworkCounters() (networkFullStats, error) {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return networkFullStats{}, err
	}
	return parseNetworkFullCounters(string(data), getInterfaceIPs())
}

func parseNetworkFullCounters(data string, ifaceIPs map[string]ifaceIPs) (networkFullStats, error) {
	var result networkFullStats
	found := false
	for _, line := range strings.Split(data, "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		if name == "lo" {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 9 {
			continue
		}
		rx, rxErr := strconv.ParseUint(fields[0], 10, 64)
		tx, txErr := strconv.ParseUint(fields[8], 10, 64)
		if rxErr != nil || txErr != nil {
			continue
		}
		var rxPackets, txPackets, rxErrors, txErrors uint64
		if len(fields) >= 11 {
			rxPackets, _ = strconv.ParseUint(fields[1], 10, 64)
			rxErrors, _ = strconv.ParseUint(fields[2], 10, 64)
			txPackets, _ = strconv.ParseUint(fields[9], 10, 64)
			txErrors, _ = strconv.ParseUint(fields[10], 10, 64)
		}

		stat := protocol.InterfaceStat{
			Name:      name,
			RxBytes:   rx,
			TxBytes:   tx,
			RxPackets: rxPackets,
			TxPackets: txPackets,
			RxErrors:  rxErrors,
			TxErrors:  txErrors,
		}
		if ipInfo, ok := ifaceIPs[name]; ok {
			stat.IPv4 = ipInfo.ipv4
			stat.IPv6 = ipInfo.ipv6
		}

		result.interfaces = append(result.interfaces, stat)
		result.rx += rx
		result.tx += tx
		found = true

		if result.primaryIPv4 == "" && stat.IPv4 != "" {
			result.primaryIPv4 = stat.IPv4
		}
		if result.primaryIPv6 == "" && stat.IPv6 != "" {
			result.primaryIPv6 = stat.IPv6
		}
	}
	if !found {
		return networkFullStats{}, fmt.Errorf("no network interfaces")
	}
	return result, nil
}

type networkStats struct{ rx, tx uint64 }

func parseNetworkCounters(data string) (networkStats, error) {
	full, err := parseNetworkFullCounters(data, nil)
	if err != nil {
		return networkStats{}, err
	}
	return networkStats{rx: full.rx, tx: full.tx}, nil
}

type cpuStats struct {
	total uint64
	idle  uint64
}

type cpuTracker struct {
	mu          sync.Mutex
	previous    cpuStats
	hasPrevious bool
	read        func() (cpuStats, error)
}

func newCPUTracker() cpuSampler { return &cpuTracker{read: readProcStat} }

func (t *cpuTracker) Sample() (float64, error) {
	current, err := t.read()
	if err != nil {
		return 0, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.hasPrevious {
		t.previous, t.hasPrevious = current, true
		return 0, nil
	}
	if current.total < t.previous.total || current.idle < t.previous.idle {
		t.previous = current
		return 0, fmt.Errorf("cpu counters moved backwards")
	}
	totalDelta := current.total - t.previous.total
	idleDelta := current.idle - t.previous.idle
	t.previous = current
	if totalDelta == 0 || idleDelta > totalDelta {
		return 0, fmt.Errorf("invalid cpu counter delta")
	}
	return float64(totalDelta-idleDelta) * 100 / float64(totalDelta), nil
}

func readProcStat() (cpuStats, error) {
	line, err := readFirstLine("/proc/stat")
	if err != nil {
		return cpuStats{}, err
	}
	return parseProcStat(line)
}

func parseProcStat(line string) (cpuStats, error) {
	fields := strings.Fields(line)
	var err error
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuStats{}, fmt.Errorf("invalid cpu stat")
	}
	var values [8]uint64
	for i := 0; i < len(values) && i+1 < len(fields); i++ {
		values[i], err = strconv.ParseUint(fields[i+1], 10, 64)
		if err != nil {
			return cpuStats{}, err
		}
	}
	idle := values[3] + values[4]
	var total uint64
	for _, value := range values {
		total += value
	}
	if total == 0 || total < idle {
		return cpuStats{}, fmt.Errorf("invalid cpu totals")
	}
	return cpuStats{total: total, idle: idle}, nil
}

func readLoadAvg() ([3]float64, error) {
	line, err := readFirstLine("/proc/loadavg")
	if err != nil {
		return [3]float64{}, err
	}
	return parseLoadAvg(line)
}

func parseLoadAvg(line string) ([3]float64, error) {
	fields := strings.Fields(line)
	var err error
	if len(fields) < 3 {
		return [3]float64{}, fmt.Errorf("invalid load average")
	}
	var result [3]float64
	for i := range result {
		result[i], err = strconv.ParseFloat(fields[i], 64)
		if err != nil || result[i] < 0 {
			return [3]float64{}, fmt.Errorf("invalid load average")
		}
	}
	return result, nil
}

var agentStartTime = time.Now().UTC().Unix()

func processStartTime() int64 { return agentStartTime }

func readSocketCounts() (uint64, uint64, error) {
	data, err := os.ReadFile("/proc/net/sockstat")
	if err != nil {
		return 0, 0, err
	}
	var tcp, udp uint64
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "TCP:" && fields[1] == "inuse" {
			if v, err := strconv.ParseUint(fields[2], 10, 64); err == nil {
				tcp = v
			}
		} else if len(fields) >= 3 && fields[0] == "UDP:" && fields[1] == "inuse" {
			if v, err := strconv.ParseUint(fields[2], 10, 64); err == nil {
				udp = v
			}
		}
	}
	return tcp, udp, nil
}

func readProcessCount() (uint64, error) {
	line, err := readFirstLine("/proc/loadavg")
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(line)
	if len(fields) >= 4 {
		parts := strings.Split(fields[3], "/")
		if len(parts) == 2 {
			if total, err := strconv.ParseUint(parts[1], 10, 64); err == nil {
				return total, nil
			}
		}
	}
	return 0, fmt.Errorf("unable to parse loadavg process count")
}
