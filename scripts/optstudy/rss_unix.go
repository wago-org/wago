//go:build darwin || linux

package main

import (
	"runtime"
	"syscall"
)

func peakRSS() int64 {
	var r syscall.Rusage
	must(syscall.Getrusage(syscall.RUSAGE_SELF, &r))
	if runtime.GOOS == "darwin" {
		return r.Maxrss
	}
	return r.Maxrss * 1024
}
