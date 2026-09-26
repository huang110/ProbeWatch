package agent

import (
	"fmt"
	"math"

	"github.com/probewatch/probewatch/internal/protocol"
)

// HostHealthCollector gathers health metrics and evaluates overall system health score.
type HostHealthCollector interface {
	Collect(snapshot *protocol.ResourceSnapshot) *protocol.HostHealthInfo
}

func defaultHostHealthCollector() HostHealthCollector {
	return newPlatformHostHealthCollector()
}

// computeHealthScore calculates an integer score between 0 and 100 based on snapshot metrics.
func computeHealthScore(snapshot *protocol.ResourceSnapshot, rebootRequired bool, secUpdates int, failedServices []string) (int, string, []string) {
	score := 100
	var deductions []string

	if snapshot == nil {
		return 0, "critical", []string{"未采集到有效遥测数据"}
	}

	// 1. CPU Load & Utilization
	if snapshot.CPUPercent > 90 {
		score -= 20
		deductions = append(deductions, fmt.Sprintf("CPU 使用率过高 (%.1f%%) -20分", snapshot.CPUPercent))
	} else if snapshot.CPUPercent > 80 {
		score -= 10
		deductions = append(deductions, fmt.Sprintf("CPU 使用率偏高 (%.1f%%) -10分", snapshot.CPUPercent))
	}

	cores := snapshot.CPUCores
	if cores < 1 {
		cores = 1
	}
	loadRatio := snapshot.Load1 / float64(cores)
	if loadRatio > 4.0 {
		score -= 20
		deductions = append(deductions, fmt.Sprintf("系统负载极高 (Load1=%.2f, %d 核) -20分", snapshot.Load1, cores))
	} else if loadRatio > 2.0 {
		score -= 10
		deductions = append(deductions, fmt.Sprintf("系统负载偏高 (Load1=%.2f, %d 核) -10分", snapshot.Load1, cores))
	}

	// 2. Memory Utilization
	if snapshot.MemoryTotalBytes > 0 {
		memUsedPct := float64(snapshot.MemoryUsedBytes) / float64(snapshot.MemoryTotalBytes) * 100.0
		if memUsedPct > 95 {
			score -= 25
			deductions = append(deductions, fmt.Sprintf("物理内存接近耗尽 (%.1f%%) -25分", memUsedPct))
		} else if memUsedPct > 85 {
			score -= 10
			deductions = append(deductions, fmt.Sprintf("物理内存占用过高 (%.1f%%) -10分", memUsedPct))
		}
	}

	// 3. Storage Space Utilization
	if snapshot.FilesystemTotalBytes > 0 {
		diskUsedPct := float64(snapshot.FilesystemUsedBytes) / float64(snapshot.FilesystemTotalBytes) * 100.0
		if diskUsedPct > 95 {
			score -= 25
			deductions = append(deductions, fmt.Sprintf("磁盘空间几乎已满 (%.1f%%) -25分", diskUsedPct))
		} else if diskUsedPct > 85 {
			score -= 10
			deductions = append(deductions, fmt.Sprintf("磁盘空间占用偏高 (%.1f%%) -10分", diskUsedPct))
		}
	}

	// 4. Inode Utilization across Mounts
	for _, m := range snapshot.Mounts {
		if m.InodesPercent > 90 {
			score -= 15
			deductions = append(deductions, fmt.Sprintf("挂载点 %s Inode 临近耗尽 (%.1f%%) -15分", m.MountPoint, m.InodesPercent))
			break
		} else if m.InodesPercent > 80 {
			score -= 8
			deductions = append(deductions, fmt.Sprintf("挂载点 %s Inode 占用偏高 (%.1f%%) -8分", m.MountPoint, m.InodesPercent))
			break
		}
	}

	// 5. Hardware Temperature
	if snapshot.CPUTempC > 85 {
		score -= 20
		deductions = append(deductions, fmt.Sprintf("硬件温度过热 (%.1f°C) -20分", snapshot.CPUTempC))
	} else if snapshot.CPUTempC > 75 {
		score -= 8
		deductions = append(deductions, fmt.Sprintf("硬件温度偏高 (%.1f°C) -8分", snapshot.CPUTempC))
	}

	// 6. Disk I/O Wait Latency
	for _, d := range snapshot.Disks {
		if d.IOWaitMS > 100 {
			score -= 15
			deductions = append(deductions, fmt.Sprintf("磁盘 %s I/O 阻塞严重 (%.1f ms) -15分", d.Device, d.IOWaitMS))
			break
		} else if d.IOWaitMS > 50 {
			score -= 8
			deductions = append(deductions, fmt.Sprintf("磁盘 %s I/O 延迟偏高 (%.1f ms) -8分", d.Device, d.IOWaitMS))
			break
		}
	}

	// 7. System Maintenance & Daemons
	if len(failedServices) > 0 {
		penalty := int(math.Min(float64(15*len(failedServices)), 30))
		score -= penalty
		deductions = append(deductions, fmt.Sprintf("检测到 %d 个核心服务故障退出 -%d分", len(failedServices), penalty))
	}

	if rebootRequired {
		score -= 5
		deductions = append(deductions, "内核/动态库补丁待重启生效 -5分")
	}

	if secUpdates > 0 {
		score -= 5
		deductions = append(deductions, fmt.Sprintf("存在 %d 个未修补的安全更新 -5分", secUpdates))
	}

	if score < 0 {
		score = 0
	} else if score > 100 {
		score = 100
	}

	status := "optimal"
	switch {
	case score >= 90:
		status = "optimal"
	case score >= 75:
		status = "good"
	case score >= 60:
		status = "warning"
	default:
		status = "critical"
	}

	return score, status, deductions
}
