//go:build linux

package main

import (
	"os"
	"syscall"
)

func peakRSS(state *os.ProcessState) uint64 {
	usage, ok := state.SysUsage().(*syscall.Rusage)
	if !ok || usage.Maxrss < 0 {
		return 0
	}
	return uint64(usage.Maxrss) * 1024
}
