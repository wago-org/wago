//go:build linux

package gc

import "syscall"

func peakRSSBytes() uint64 {
	var usage syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &usage) != nil || usage.Maxrss < 0 {
		return 0
	}
	return uint64(usage.Maxrss) * 1024
}
