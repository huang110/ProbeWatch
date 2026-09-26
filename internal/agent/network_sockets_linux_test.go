//go:build linux

package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxSocketCollectorMock(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Mock /proc/net/tcp
	tcpContent := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 10001 1 ffff8bc6caa08000 100 0 0 10 0
   1: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000   999        0 10002 1 ffff8bc6faa06880 100 0 0 10 0
   2: 0100007F:1F90 0100007F:C000 01 00000000:00000000 00:00000000 00000000   999        0 10003 1 ffff8bc6faa06880 100 0 0 10 0
   3: 0100007F:1F90 0100007F:C001 06 00000000:00000000 00:00000000 00000000   999        0 10004 1 ffff8bc6faa06880 100 0 0 10 0
   4: 0100007F:1F90 0100007F:C002 08 00000000:00000000 00:00000000 00000000   999        0 10005 1 ffff8bc6faa06880 100 0 0 10 0
`
	tcpPath := filepath.Join(tmpDir, "tcp")
	if err := os.WriteFile(tcpPath, []byte(tcpContent), 0644); err != nil {
		t.Fatal(err)
	}

	// 2. Mock /proc/net/udp
	udpContent := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode ref pointer drops
   0: 00000000:0035 00000000:0000 07 00000000:00000000 00:00000000 00000000     0        0 10006 2 ffff8bc6cba25a40 0
`
	udpPath := filepath.Join(tmpDir, "udp")
	if err := os.WriteFile(udpPath, []byte(udpContent), 0644); err != nil {
		t.Fatal(err)
	}

	// 3. Mock /proc/net/tcp6 and udp6 as empty
	tcp6Path := filepath.Join(tmpDir, "tcp6")
	os.WriteFile(tcp6Path, []byte("  sl  local_address remote_address st ...\n"), 0644)
	udp6Path := filepath.Join(tmpDir, "udp6")
	os.WriteFile(udp6Path, []byte("  sl  local_address remote_address st ...\n"), 0644)

	// 4. Mock /proc tree with PIDs and socket symlinks
	procDir := filepath.Join(tmpDir, "proc")
	pid100Dir := filepath.Join(procDir, "100")
	pid100Fd := filepath.Join(pid100Dir, "fd")
	if err := os.MkdirAll(pid100Fd, 0755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(pid100Dir, "comm"), []byte("nginx\n"), 0644)

	pid200Dir := filepath.Join(procDir, "200")
	pid200Fd := filepath.Join(pid200Dir, "fd")
	if err := os.MkdirAll(pid200Fd, 0755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(pid200Dir, "comm"), []byte("probewatch\n"), 0644)

	collector := &linuxSocketCollector{
		tcpPath:  tcpPath,
		tcp6Path: tcp6Path,
		udpPath:  udpPath,
		udp6Path: udp6Path,
		procPath: procDir,
	}

	stats, ports, err := collector.Collect()
	if err != nil {
		t.Fatalf("Collect error = %v", err)
	}

	if stats.TCPTotal != 5 {
		t.Errorf("TCPTotal = %d, want 5", stats.TCPTotal)
	}
	if stats.TCPListen != 2 {
		t.Errorf("TCPListen = %d, want 2", stats.TCPListen)
	}
	if stats.TCPEstablished != 1 {
		t.Errorf("TCPEstablished = %d, want 1", stats.TCPEstablished)
	}
	if stats.TCPTimeWait != 1 {
		t.Errorf("TCPTimeWait = %d, want 1", stats.TCPTimeWait)
	}
	if stats.TCPCloseWait != 1 {
		t.Errorf("TCPCloseWait = %d, want 1", stats.TCPCloseWait)
	}
	if stats.UDPTotal != 1 {
		t.Errorf("UDPTotal = %d, want 1", stats.UDPTotal)
	}

	if len(ports) < 2 {
		t.Fatalf("expected at least 2 ports, got %d", len(ports))
	}

	// Port 53 (UDP) and Port 80 (TCP) and Port 8080 (TCP)
	found80 := false
	found8080 := false
	found53 := false
	for _, p := range ports {
		if p.Port == 80 && p.Proto == "tcp" {
			found80 = true
			if !p.IsPublic {
				t.Errorf("port 80 on 0.0.0.0 should be marked public")
			}
		}
		if p.Port == 8080 && p.Proto == "tcp" {
			found8080 = true
			if p.IsPublic {
				t.Errorf("port 8080 on 127.0.0.1 should not be marked public")
			}
		}
		if p.Port == 53 && p.Proto == "udp" {
			found53 = true
		}
	}

	if !found80 {
		t.Errorf("port 80 not found in %v", ports)
	}
	if !found8080 {
		t.Errorf("port 8080 not found in %v", ports)
	}
	if !found53 {
		t.Errorf("port 53 not found in %v", ports)
	}

	_ = fmt.Sprintf("ok")
}
