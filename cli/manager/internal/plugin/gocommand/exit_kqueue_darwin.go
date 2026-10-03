//go:build darwin

package gocommand

import (
	"errors"
	"fmt"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func waitDirectExit(pid int) error {
	// Native Darwin runners reject waitid(WNOWAIT) with EPERM. A kqueue
	// NOTE_EXIT observes the same leader without reaping it, leaving its
	// numeric PID/PGID pinned until beforeWait stops all group callbacks.
	queue, err := unix.Kqueue()
	if err != nil {
		return fmt.Errorf("open Go process exit queue: %w", err)
	}
	defer unix.Close(queue)
	change := unix.Kevent_t{
		Ident:  uint64(pid),
		Filter: unix.EVFILT_PROC,
		Flags:  unix.EV_ADD | unix.EV_ONESHOT,
		Fflags: unix.NOTE_EXIT,
	}
	for {
		_, err = unix.Kevent(queue, []unix.Kevent_t{change}, nil, nil)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if errors.Is(err, syscall.ESRCH) {
			// Start succeeded and no code has called Cmd.Wait. If registration
			// misses an already-exited child, its unreaped PID is still ours.
			return nil
		}
		if err != nil {
			return fmt.Errorf("watch Go process exit: %w", err)
		}
		break
	}
	// EVFILT_PROC is edge-triggered: the leader may have exited just before
	// registration. Checking its non-reaping status after registration closes
	// that gap; if still live, the registered NOTE_EXIT will be delivered.
	exited, err := darwinChildExited(pid)
	if err != nil {
		return err
	}
	if exited {
		return nil
	}
	var events [1]unix.Kevent_t
	for {
		// A finite wait also closes a missed-edge race if the status probe
		// observed a transient live state after an already-exited leader.
		timeout := unix.NsecToTimespec((250 * time.Millisecond).Nanoseconds())
		n, err := unix.Kevent(queue, nil, events[:], &timeout)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			return fmt.Errorf("wait for Go process exit: %w", err)
		}
		if n == 0 {
			exited, err := darwinChildExited(pid)
			if err != nil {
				return err
			}
			if exited {
				return nil
			}
			continue
		}
		if n != 1 {
			continue
		}
		if events[0].Flags&unix.EV_ERROR != 0 {
			return fmt.Errorf("Go process exit watch: %w", syscall.Errno(events[0].Data))
		}
		if events[0].Fflags&unix.NOTE_EXIT != 0 {
			return nil
		}
	}
}

func darwinChildExited(pid int) (bool, error) {
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if errors.Is(err, syscall.ESRCH) || errors.Is(err, syscall.ENOENT) {
		// The direct child is still waitable; an absent live entry means it
		// has reached the zombie state and cannot have a recycled PID.
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect Go process after exit watch: %w", err)
	}
	if info == nil {
		return false, fmt.Errorf("inspect Go process after exit watch: no process record")
	}
	return info.Proc.P_stat == 5 /* SZOMB */, nil
}
