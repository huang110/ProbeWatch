package agent

import (
	"math"
	"sort"
	"sync"
	"time"

	"github.com/probewatch/probewatch/internal/protocol"
)

type rawDiskMetrics struct {
	readsCompleted  uint64
	sectorsRead     uint64
	writesCompleted uint64
	sectorsWritten  uint64
	ioTimeMS        uint64
}

// DiskIOTracker samples real-time read/write throughput, IOPS, and I/O wait latency across block devices.
type DiskIOTracker interface {
	Sample() []protocol.DiskStat
}

type diskIOTrackerImpl struct {
	mu        sync.Mutex
	prevTime  time.Time
	prevStats map[string]rawDiskMetrics
	reader    func() (map[string]rawDiskMetrics, error)
}

func newDiskIOTracker(reader func() (map[string]rawDiskMetrics, error)) *diskIOTrackerImpl {
	return &diskIOTrackerImpl{
		reader: reader,
	}
}

func (t *diskIOTrackerImpl) Sample() []protocol.DiskStat {
	if t.reader == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	current, err := t.reader()
	if err != nil || len(current) == 0 {
		return nil
	}

	if t.prevTime.IsZero() || t.prevStats == nil {
		t.prevTime = now
		t.prevStats = current
		var initial []protocol.DiskStat
		for dev := range current {
			initial = append(initial, protocol.DiskStat{Device: dev})
		}
		sort.Slice(initial, func(i, j int) bool { return initial[i].Device < initial[j].Device })
		return initial
	}

	dt := now.Sub(t.prevTime).Seconds()
	if dt < 0.2 {
		var res []protocol.DiskStat
		for dev := range current {
			res = append(res, protocol.DiskStat{Device: dev})
		}
		sort.Slice(res, func(i, j int) bool { return res[i].Device < res[j].Device })
		return res
	}

	var stats []protocol.DiskStat
	for dev, cur := range current {
		prev, ok := t.prevStats[dev]
		if !ok || cur.sectorsRead < prev.sectorsRead || cur.sectorsWritten < prev.sectorsWritten {
			stats = append(stats, protocol.DiskStat{Device: dev})
			continue
		}

		deltaReadSectors := cur.sectorsRead - prev.sectorsRead
		deltaWriteSectors := cur.sectorsWritten - prev.sectorsWritten
		deltaReads := cur.readsCompleted - prev.readsCompleted
		deltaWrites := cur.writesCompleted - prev.writesCompleted
		deltaIOMs := uint64(0)
		if cur.ioTimeMS >= prev.ioTimeMS {
			deltaIOMs = cur.ioTimeMS - prev.ioTimeMS
		}

		readBytesPerSec := uint64(float64(deltaReadSectors*512) / dt)
		writeBytesPerSec := uint64(float64(deltaWriteSectors*512) / dt)
		readIOPS := roundOneDecimal(float64(deltaReads) / dt)
		writeIOPS := roundOneDecimal(float64(deltaWrites) / dt)

		utilPercent := roundOneDecimal(math.Min(100.0, math.Max(0.0, (float64(deltaIOMs)/(dt*1000.0))*100.0)))

		totalOps := deltaReads + deltaWrites
		var ioWaitMS float64
		if totalOps > 0 && deltaIOMs > 0 {
			ioWaitMS = roundTwoDecimals(float64(deltaIOMs) / float64(totalOps))
		}

		stats = append(stats, protocol.DiskStat{
			Device:           dev,
			ReadBytesPerSec:  readBytesPerSec,
			WriteBytesPerSec: writeBytesPerSec,
			ReadIOPS:         readIOPS,
			WriteIOPS:        writeIOPS,
			IOWaitMS:         ioWaitMS,
			UtilPercent:      utilPercent,
		})
	}

	t.prevTime = now
	t.prevStats = current

	sort.Slice(stats, func(i, j int) bool { return stats[i].Device < stats[j].Device })
	return stats
}
