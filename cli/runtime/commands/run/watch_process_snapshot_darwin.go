//go:build darwin

package run

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"syscall"
	"time"

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

func waitWatchedCommandStart(command *exec.Cmd) error {
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
	return nil
}

func resumeWatchedCommand(command *exec.Cmd) error {
	return unix.PtraceDetach(command.Process.Pid)
}

func finishWatchedProcessTracking(tracker *watchedProcessTracker) {
	deadline := time.Now().Add(100 * time.Millisecond)
	for {
		if err := tracker.drainEvents(); err != nil {
			tracker.fail(err)
			return
		}
		if tracker.processCount() == 0 {
			return
		}
		_ = tracker.signal(syscall.SIGKILL)
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

func startWatchedProcessTracking(tracker *watchedProcessTracker) error {
	queue, err := unix.Kqueue()
	if err != nil {
		return err
	}
	changes := make([]unix.Kevent_t, 2)
	unix.SetKevent(&changes[0], tracker.root, unix.EVFILT_PROC, unix.EV_ADD|unix.EV_ENABLE|unix.EV_CLEAR)
	changes[0].Fflags = unix.NOTE_FORK | unix.NOTE_TRACK | unix.NOTE_EXIT
	unix.SetKevent(&changes[1], 0, unix.EVFILT_USER, unix.EV_ADD|unix.EV_CLEAR)
	if _, err := unix.Kevent(queue, changes, nil, nil); err != nil {
		unix.Close(queue)
		return err
	}
	barriers := make(chan chan struct{}, 1)
	stopped := make(chan struct{})
	tracker.stop = func() { _ = unix.Close(queue) }
	tracker.drain = func() error {
		done := make(chan struct{})
		select {
		case barriers <- done:
		case <-stopped:
			return errors.New("watched process event tracker stopped")
		}
		trigger := unix.Kevent_t{}
		unix.SetKevent(&trigger, 0, unix.EVFILT_USER, 0)
		trigger.Fflags = unix.NOTE_TRIGGER
		if _, err := unix.Kevent(queue, []unix.Kevent_t{trigger}, nil, nil); err != nil {
			return err
		}
		select {
		case <-done:
			return nil
		case <-stopped:
			return errors.New("watched process event tracker stopped")
		}
	}
	go trackWatchedProcessEvents(queue, tracker, barriers, stopped)
	return nil
}

func trackWatchedProcessEvents(queue int, tracker *watchedProcessTracker, barriers <-chan chan struct{}, stopped chan<- struct{}) {
	defer close(stopped)
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
		barrier := processWatchedProcessEvents(events[:count], tracker)
		if barrier {
			for {
				timeout := unix.Timespec{}
				count, err = unix.Kevent(queue, nil, events, &timeout)
				if err != nil {
					tracker.fail(err)
					return
				}
				if count == 0 {
					break
				}
				processWatchedProcessEvents(events[:count], tracker)
			}
			close(<-barriers)
		}
	}
}

func processWatchedProcessEvents(events []unix.Kevent_t, tracker *watchedProcessTracker) bool {
	barrier := false
	for _, event := range events {
		if event.Filter == unix.EVFILT_USER {
			barrier = true
			continue
		}
		if event.Flags&unix.EV_ERROR != 0 {
			tracker.fail(fmt.Errorf("track watched process: %w", syscall.Errno(event.Data)))
			continue
		}
		if event.Fflags&unix.NOTE_TRACKERR != 0 {
			tracker.fail(errors.New("kernel could not track a watched descendant"))
		}
		pid := int(event.Ident)
		if event.Fflags&unix.NOTE_CHILD != 0 {
			if process, ok := watchedProcess(pid); ok {
				tracker.record(process)
			}
		}
		if event.Fflags&unix.NOTE_EXIT != 0 {
			tracker.remove(pid)
		}
	}
	return barrier
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

func (tracker *watchedProcessTracker) remove(pid int) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	delete(tracker.processes, pid)
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
