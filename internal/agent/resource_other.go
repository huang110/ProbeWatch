//go:build !linux

package agent

type filesystemStats struct{ total, used uint64 }

func rootFilesystemUsage() (filesystemStats, error) {
	return filesystemStats{}, nil
}
