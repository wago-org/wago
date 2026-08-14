//go:build darwin

package run

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"syscall"

	"golang.org/x/sys/unix"
)

func prepareWatchedProcessTracking() error {
	return nil
}

func configureWatchedCommandStart(attributes *syscall.SysProcAttr) {
	attributes.Ptrace = true
}

func lockWatchedCommandStart() func() {
	runtime.LockOSThread()
	return runtime.UnlockOSThread
}

func resumeWatchedCommand(command *exec.Cmd) error {
	var status syscall.WaitStatus
	for {
		_, err := syscall.Wait4(command.Process.Pid, &status, syscall.WUNTRACED, nil)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		break
	}
	if !status.Stopped() {
		return fmt.Errorf("watched process did not stop before tracking: %v", status)
	}
	return unix.PtraceDetach(command.Process.Pid)
}

func finishWatchedProcessTracking(*watchedProcessTracker) {}

func startWatchedProcessTracking(tracker *watchedProcessTracker) error {
	queue, err := unix.Kqueue()
	if err != nil {
		return err
	}
	change := unix.Kevent_t{}
	unix.SetKevent(&change, tracker.root, unix.EVFILT_PROC, unix.EV_ADD|unix.EV_ENABLE|unix.EV_CLEAR)
	change.Fflags = unix.NOTE_FORK | unix.NOTE_TRACK | unix.NOTE_EXIT
	if _, err := unix.Kevent(queue, []unix.Kevent_t{change}, nil, nil); err != nil {
		unix.Close(queue)
		return err
	}
	tracker.stop = func() { _ = unix.Close(queue) }
	go trackWatchedProcessEvents(queue, tracker)
	return nil
}

func trackWatchedProcessEvents(queue int, tracker *watchedProcessTracker) {
	events := make([]unix.Kevent_t, 16)
	for {
		count, err := unix.Kevent(queue, nil, events, nil)
		if errors.Is(err, syscall.EBADF) || errors.Is(err, syscall.EINTR) {
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			return
		}
		if err != nil {
			tracker.fail(err)
			return
		}
		for _, event := range events[:count] {
			if event.Flags&unix.EV_ERROR != 0 {
				tracker.fail(fmt.Errorf("track watched process: %w", syscall.Errno(event.Data)))
				continue
			}
			if event.Fflags&unix.NOTE_TRACKERR != 0 {
				tracker.fail(errors.New("kernel could not track a watched descendant"))
			}
			if event.Fflags&unix.NOTE_CHILD != 0 {
				if process, ok := watchedProcess(int(event.Ident)); ok {
					tracker.record(process)
				}
			}
		}
	}
}

func watchedProcess(pid int) (watchedProcessInfo, bool) {
	entry, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || entry == nil || int(entry.Proc.P_pid) != pid {
		return watchedProcessInfo{}, false
	}
	return watchedProcessInfo{
		pid:     pid,
		parent:  int(entry.Eproc.Ppid),
		group:   int(entry.Eproc.Pgid),
		started: uint64(entry.Proc.P_starttime.Sec)*1_000_000 + uint64(entry.Proc.P_starttime.Usec),
	}, true
}

func (tracker *watchedProcessTracker) record(process watchedProcessInfo) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if process.pid == tracker.root || process.pid == tracker.owner {
		return
	}
	if len(tracker.processes) >= maxWatchedDescendants {
		tracker.eventErr = fmt.Errorf("watched process tree exceeds %d descendants", maxWatchedDescendants)
		return
	}
	tracker.processes[process.pid] = process.started
}

func (tracker *watchedProcessTracker) fail(err error) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	tracker.eventErr = errors.Join(tracker.eventErr, err)
}

func watchedProcessDescendants(_, _ int, tracked map[int]uint64, limit int) ([]watchedProcessInfo, error) {
	processes := make([]watchedProcessInfo, 0, len(tracked))
	for pid, started := range tracked {
		process, ok := watchedProcess(pid)
		if !ok || process.started != started {
			continue
		}
		if len(processes) >= limit {
			return processes, fmt.Errorf("watched process tree exceeds %d descendants", limit)
		}
		processes = append(processes, process)
	}
	return processes, nil
}
