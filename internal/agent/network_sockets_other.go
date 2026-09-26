//go:build !linux

package agent

import (
	"github.com/probewatch/probewatch/internal/protocol"
)

type otherSocketCollector struct{}

func newPlatformSocketCollector() SocketCollector {
	return &otherSocketCollector{}
}

func (c *otherSocketCollector) Collect() (*protocol.SocketStats, []protocol.ListeningPort, error) {
	return &protocol.SocketStats{}, nil, nil
}
