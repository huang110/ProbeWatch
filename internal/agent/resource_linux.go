//go:build linux

package agent

import "syscall"

type filesystemStats struct{ total, used uint64 }

func rootFilesystemUsage() (filesystemStats, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		return filesystemStats{}, err
	}
	blockSize := uint64(stat.Bsize)
	total := stat.Blocks * blockSize
	available := stat.Bavail * blockSize
	used := uint64(0)
	if total > available {
		used = total - available
	}
	return filesystemStats{total: total, used: used}, nil
}
