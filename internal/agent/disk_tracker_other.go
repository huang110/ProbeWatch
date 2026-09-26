//go:build !linux

package agent

import "github.com/probewatch/probewatch/internal/protocol"

type nonLinuxDiskTracker struct{}

func newPlatformDiskTracker() DiskIOTracker {
	return &nonLinuxDiskTracker{}
}

func (t *nonLinuxDiskTracker) Sample() []protocol.DiskStat {
	return nil
}
