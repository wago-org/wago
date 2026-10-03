//go:build linux

package gocommand

import (
	"errors"
	"syscall"

	"golang.org/x/sys/unix"
)

func waitDirectExit(pid int) error {
	var info unix.Siginfo
	for {
		// WNOWAIT observes the direct child without freeing its PID/PGID.
		err := unix.Waitid(unix.P_PID, pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		return err
	}
}
