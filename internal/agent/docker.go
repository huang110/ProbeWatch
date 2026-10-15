package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/probewatch/probewatch/internal/protocol"
)

var defaultDockerSockets = []string{
	"/var/run/docker.sock",
	"/run/podman/podman.sock",
}

type dockerEngineVersion struct {
	Version string `json:"Version"`
}

type dockerContainerListItem struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	ImageID string            `json:"ImageID"`
	Command string            `json:"Command"`
	Created int64             `json:"Created"`
	State   string            `json:"State"`
	Status  string            `json:"Status"`
	Ports   []dockerPortEntry `json:"Ports"`
}

type dockerPortEntry struct {
	IP          string `json:"IP"`
	PrivatePort uint16 `json:"PrivatePort"`
	PublicPort  uint16 `json:"PublicPort"`
	Type        string `json:"Type"`
}

type dockerContainerStats struct {
	CPUStats struct {
		CPUUsage struct {
			TotalUsage  uint64   `json:"total_usage"`
			PercpuUsage []uint64 `json:"percpu_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs     uint32 `json:"online_cpus"`
	} `json:"cpu_stats"`
	PreCPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
	} `json:"precpu_stats"`
	MemoryStats struct {
		Usage uint64 `json:"usage"`
		Limit uint64 `json:"limit"`
		Stats struct {
			Cache        uint64 `json:"cache"`
			InactiveFile uint64 `json:"inactive_file"`
		} `json:"stats"`
	} `json:"memory_stats"`
	Networks map[string]struct {
		RxBytes uint64 `json:"rx_bytes"`
		TxBytes uint64 `json:"tx_bytes"`
	} `json:"networks"`
	BlkioStats struct {
		IOServiceBytesRecursive []struct {
			Op    string `json:"op"`
			Value uint64 `json:"value"`
		} `json:"io_service_bytes_recursive"`
	} `json:"blkio_stats"`
	PIDsStats struct {
		Current uint32 `json:"current"`
	} `json:"pids_stats"`
}

// FindDockerSocket locates an active docker or podman unix socket.
func FindDockerSocket() string {
	for _, sock := range defaultDockerSockets {
		if fi, err := os.Stat(sock); err == nil && (fi.Mode()&os.ModeSocket != 0) {
			return sock
		}
	}
	return ""
}

// CollectDockerWorkload probes local docker socket and extracts running container metrics.
func CollectDockerWorkload() (bool, string, []protocol.ContainerSnapshot, error) {
	sock := FindDockerSocket()
	if sock == "" {
		return false, "", nil, nil
	}
	return CollectDockerWorkloadFromSocket(sock)
}

const (
	maxScannedContainers        = 50
	maxDockerStatsConcurrency   = 4
	singleContainerStatsTimeout = 2 * time.Second
	maxDockerResponseBody       = 512 * 1024
)

// CollectDockerWorkloadFromSocket collects docker metrics from a specified socket path.
func CollectDockerWorkloadFromSocket(sockPath string) (bool, string, []protocol.ContainerSnapshot, error) {
	tr := &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sockPath)
		},
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{
		Transport: tr,
		Timeout: 5 * time.Second,
	}

	// 1. Version check
	version := "unknown"
	vResp, err := client.Get("http://localhost/version")
	if err == nil && vResp.StatusCode != http.StatusOK { vResp.Body.Close(); vResp, err = client.Get("http://localhost/v1.24/version") }
	if err != nil {
		return false, "", nil, nil
	}
	defer vResp.Body.Close()
	if vResp.StatusCode == http.StatusOK {
		var ver dockerEngineVersion
		if err := json.NewDecoder(io.LimitReader(vResp.Body, 64*1024)).Decode(&ver); err == nil && ver.Version != "" {
			version = ver.Version
		} else { vResp.Body.Close(); vResp, err = client.Get("http://localhost/v1.24/version"); if err == nil { defer vResp.Body.Close(); _ = json.NewDecoder(io.LimitReader(vResp.Body, 64*1024)).Decode(&ver); if ver.Version != "" { version = ver.Version } } }
	}

	// 2. Container list
	cResp, err := client.Get("http://localhost/containers/json?all=1")
	if err == nil && cResp.StatusCode != http.StatusOK { cResp.Body.Close(); cResp, err = client.Get("http://localhost/v1.24/containers/json?all=1") }
	if err != nil {
		return true, version, nil, fmt.Errorf("list docker containers: %w", err)
	}
	defer cResp.Body.Close()

	if cResp.StatusCode != http.StatusOK {
		return true, version, nil, fmt.Errorf("list containers returned status %d", cResp.StatusCode)
	}

	var rawList []dockerContainerListItem
	if err := json.NewDecoder(io.LimitReader(cResp.Body, 1<<20)).Decode(&rawList); err != nil {
		cResp.Body.Close()
		legacy, legacyErr := client.Get("http://localhost/v1.24/containers/json?all=1")
		if legacyErr != nil { return true, version, nil, fmt.Errorf("decode containers list: %w", err) }
		defer legacy.Body.Close()
		if decodeErr := json.NewDecoder(io.LimitReader(legacy.Body, 1<<20)).Decode(&rawList); decodeErr != nil { return true, version, nil, fmt.Errorf("decode containers list: %w", err) }
	}

	if len(rawList) > maxScannedContainers {
		rawList = rawList[:maxScannedContainers]
	}

	snapshots := make([]protocol.ContainerSnapshot, len(rawList))
	for i, c := range rawList {
		snap := protocol.ContainerSnapshot{
			ID:      c.ID,
			Names:   c.Names,
			Image:   c.Image,
			ImageID: c.ImageID,
			Command: c.Command,
			Created: c.Created,
			State:   strings.ToLower(c.State),
			Status:  c.Status,
		}

		// Detect health status
		statusLower := strings.ToLower(c.Status)
		if strings.Contains(statusLower, "(healthy)") {
			snap.Health = "healthy"
		} else if strings.Contains(statusLower, "(unhealthy)") {
			snap.Health = "unhealthy"
		} else if strings.Contains(statusLower, "(health: starting)") {
			snap.Health = "starting"
		}

		// Format ports
		for _, p := range c.Ports {
			var portStr string
			if p.PublicPort > 0 {
				portStr = fmt.Sprintf("%d:%d/%s", p.PublicPort, p.PrivatePort, p.Type)
			} else {
				portStr = fmt.Sprintf("%d/%s", p.PrivatePort, p.Type)
			}
			snap.Ports = append(snap.Ports, portStr)
		}

		snapshots[i] = snap
	}

	// 3. Concurrently query stats for running containers with bounded concurrency
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxDockerStatsConcurrency)
	for i := range snapshots {
		if snapshots[i].State != "running" {
			continue
		}
		idx := i
		cid := rawList[idx].ID
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			ctx, cancel := context.WithTimeout(context.Background(), singleContainerStatsTimeout)
			defer cancel()

			if s, err := fetchContainerStatsWithContext(ctx, client, cid); err == nil && s != nil {
				applyContainerStats(&snapshots[idx], s)
			}
		}()
	}
	wg.Wait()

	return true, version, snapshots, nil
}

func applyContainerStats(snap *protocol.ContainerSnapshot, s *dockerContainerStats) {
	snap.CPUPercent = calculateCPUPercent(s)
	snap.MemoryUsageBytes = calculateMemoryUsage(s)
	snap.MemoryLimitBytes = s.MemoryStats.Limit
	if snap.MemoryLimitBytes > 0 {
		snap.MemoryPercent = (float64(snap.MemoryUsageBytes) / float64(snap.MemoryLimitBytes)) * 100.0
		if snap.MemoryPercent > 100.0 {
			snap.MemoryPercent = 100.0
		}
	}
	for _, netStat := range s.Networks {
		snap.NetworkRxBytes += netStat.RxBytes
		snap.NetworkTxBytes += netStat.TxBytes
	}
	for _, blk := range s.BlkioStats.IOServiceBytesRecursive {
		opLower := strings.ToLower(blk.Op)
		if strings.Contains(opLower, "read") {
			snap.BlockReadBytes += blk.Value
		} else if strings.Contains(opLower, "write") {
			snap.BlockWriteBytes += blk.Value
		}
	}
	snap.PIDs = s.PIDsStats.Current
}

func fetchContainerStats(client *http.Client, containerID string) (*dockerContainerStats, error) {
	ctx, cancel := context.WithTimeout(context.Background(), singleContainerStatsTimeout)
	defer cancel()
	return fetchContainerStatsWithContext(ctx, client, containerID)
}

func fetchContainerStatsWithContext(ctx context.Context, client *http.Client, containerID string) (*dockerContainerStats, error) {
	url := fmt.Sprintf("http://localhost/containers/%s/stats?stream=false", containerID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		legacyURL := fmt.Sprintf("http://localhost/v1.24/containers/%s/stats?stream=false", containerID)
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, legacyURL, nil)
		if err != nil { return nil, err }
		resp, err = client.Do(req)
		if err != nil { return nil, err }
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stats status %d", resp.StatusCode)
	}

	var stats dockerContainerStats
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxDockerResponseBody)).Decode(&stats); err != nil {
		return nil, err
	}
	return &stats, nil
}


func calculateCPUPercent(s *dockerContainerStats) float64 {
	cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage) - float64(s.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(s.CPUStats.SystemCPUUsage) - float64(s.PreCPUStats.SystemCPUUsage)

	if systemDelta > 0 && cpuDelta > 0 {
		cpus := float64(s.CPUStats.OnlineCPUs)
		if cpus == 0 {
			cpus = float64(len(s.CPUStats.CPUUsage.PercpuUsage))
		}
		if cpus == 0 {
			cpus = 1.0
		}
		percent := (cpuDelta / systemDelta) * cpus * 100.0
		return float64(int(percent*100)) / 100.0
	}
	return 0.0
}

func calculateMemoryUsage(s *dockerContainerStats) uint64 {
	usage := s.MemoryStats.Usage
	cache := s.MemoryStats.Stats.Cache
	if cache == 0 {
		cache = s.MemoryStats.Stats.InactiveFile
	}
	if usage > cache {
		return usage - cache
	}
	return usage
}
