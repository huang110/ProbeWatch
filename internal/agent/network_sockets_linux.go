//go:build linux

package agent

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/probewatch/probewatch/internal/protocol"
)

type linuxSocketCollector struct {
	tcpPath  string
	tcp6Path string
	udpPath  string
	udp6Path string
	procPath string
}

func newPlatformSocketCollector() SocketCollector {
	return &linuxSocketCollector{
		tcpPath:  "/proc/net/tcp",
		tcp6Path: "/proc/net/tcp6",
		udpPath:  "/proc/net/udp",
		udp6Path: "/proc/net/udp6",
		procPath: "/proc",
	}
}

type procInfo struct {
	PID     int
	Process string
}

type rawSocketEntry struct {
	proto   string
	local   string
	state   string
	inode   string
	isIPv6  bool
}

func (c *linuxSocketCollector) Collect() (*protocol.SocketStats, []protocol.ListeningPort, error) {
	stats := &protocol.SocketStats{}
	var candidates []rawSocketEntry
	listeningInodes := make(map[string]bool)

	// 1. Parse TCP
	tcpEntries := parseProcNetFile(c.tcpPath, "tcp", false)
	for _, entry := range tcpEntries {
		stats.TCPTotal++
		switch entry.state {
		case "01":
			stats.TCPEstablished++
		case "06":
			stats.TCPTimeWait++
		case "08":
			stats.TCPCloseWait++
		case "0A":
			stats.TCPListen++
			candidates = append(candidates, entry)
			if entry.inode != "" && entry.inode != "0" {
				listeningInodes[entry.inode] = true
			}
		}
	}

	// 2. Parse TCP6
	tcp6Entries := parseProcNetFile(c.tcp6Path, "tcp6", true)
	for _, entry := range tcp6Entries {
		stats.TCPTotal++
		switch entry.state {
		case "01":
			stats.TCPEstablished++
		case "06":
			stats.TCPTimeWait++
		case "08":
			stats.TCPCloseWait++
		case "0A":
			stats.TCPListen++
			candidates = append(candidates, entry)
			if entry.inode != "" && entry.inode != "0" {
				listeningInodes[entry.inode] = true
			}
		}
	}

	// 3. Parse UDP
	udpEntries := parseProcNetFile(c.udpPath, "udp", false)
	for _, entry := range udpEntries {
		stats.UDPTotal++
		candidates = append(candidates, entry)
		if entry.inode != "" && entry.inode != "0" {
			listeningInodes[entry.inode] = true
		}
	}

	// 4. Parse UDP6
	udp6Entries := parseProcNetFile(c.udp6Path, "udp6", true)
	for _, entry := range udp6Entries {
		stats.UDPTotal++
		candidates = append(candidates, entry)
		if entry.inode != "" && entry.inode != "0" {
			listeningInodes[entry.inode] = true
		}
	}

	// 5. Inode to process mapping
	inodeToProc := scanProcSockets(c.procPath, listeningInodes)

	// 6. Build ListeningPort items
	seen := make(map[string]bool)
	var ports []protocol.ListeningPort

	for _, cand := range candidates {
		ipStr, portNum, err := parseNetHexAddr(cand.local, cand.isIPv6)
		if err != nil || portNum < 1 || portNum > 65535 {
			continue
		}

		key := fmt.Sprintf("%s:%d:%s", cand.proto, portNum, ipStr)
		if seen[key] {
			continue
		}
		seen[key] = true

		proc := inodeToProc[cand.inode]
		ports = append(ports, protocol.ListeningPort{
			Proto:    cand.proto,
			Port:     portNum,
			BindIP:   ipStr,
			Process:  proc.Process,
			PID:      proc.PID,
			IsPublic: isPublicAddress(ipStr),
		})
	}

	// 7. Sort by port ascending, then proto ascending
	sort.Slice(ports, func(i, j int) bool {
		if ports[i].Port != ports[j].Port {
			return ports[i].Port < ports[j].Port
		}
		return ports[i].Proto < ports[j].Proto
	})

	// 8. Truncate to maximum 64 entries
	if len(ports) > 64 {
		ports = ports[:64]
	}

	return stats, ports, nil
}

// parseProcNetFile reads a /proc/net/{tcp,tcp6,udp,udp6} file into raw entries.
func parseProcNetFile(path string, proto string, isIPv6 bool) []rawSocketEntry {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var entries []rawSocketEntry
	scanner := bufio.NewScanner(f)
	firstLine := true

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if firstLine {
			firstLine = false
			continue // skip header
		}

		fields := strings.Fields(line)
		// Expected at least: [0]=sl [1]=local_address [2]=rem_address [3]=st ... [9]=inode
		if len(fields) < 10 {
			continue
		}

		entries = append(entries, rawSocketEntry{
			proto:   proto,
			local:   fields[1],
			state:   strings.ToUpper(fields[3]),
			inode:   fields[9],
			isIPv6:  isIPv6,
		})
	}

	return entries
}

// scanProcSockets scans /proc to match listening socket inodes to their owning PID and command name.
func scanProcSockets(procPath string, targetInodes map[string]bool) map[string]procInfo {
	result := make(map[string]procInfo)
	if len(targetInodes) == 0 {
		return result
	}

	entries, err := os.ReadDir(procPath)
	if err != nil {
		return result
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}

		fdDir := filepath.Join(procPath, entry.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}

		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}

			if strings.HasPrefix(link, "socket:[") && strings.HasSuffix(link, "]") {
				inode := link[8 : len(link)-1]
				if targetInodes[inode] {
					if _, exists := result[inode]; !exists {
						commBytes, _ := os.ReadFile(filepath.Join(procPath, entry.Name(), "comm"))
						comm := strings.TrimSpace(string(commBytes))
						if comm == "" {
							comm = "-"
						}
						result[inode] = procInfo{PID: pid, Process: comm}
					}
				}
			}
		}
	}

	return result
}
