//go:build !linux

package monitor

func defaultMTRTransport() MTRTransport { return UnsupportedMTRTransport{} }
