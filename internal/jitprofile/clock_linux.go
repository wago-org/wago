//go:build linux && !tinygo

package jitprofile

import "golang.org/x/sys/unix"

const Clock = "monotonic"

func Now() uint64 {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts); err != nil {
		panic(err)
	}
	return uint64(ts.Nano())
}

func threadID() uint32 { return uint32(unix.Gettid()) }
