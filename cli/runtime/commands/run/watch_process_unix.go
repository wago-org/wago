//go:build linux || darwin

package run

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

type watchedChildPlatform struct {
	terminalFD int
	foreground int
	processes  *watchedProcessTracker
}

const maxWatchedDescendants = 4096

type watchedProcessInfo struct {
	pid, parent int
	started     uint64
}

type watchedProcessTracker struct {
	mu        sync.Mutex
	owner     int
	root      int
	processes map[int]uint64
	stop      func()
	eventErr  error
}

func prepareWatchedCommand(command *exec.Cmd) error {
	if err := prepareWatchedProcessTracking(); err != nil {
		return err
	}
	attributes := &syscall.SysProcAttr{Setpgid: true}
	if terminal, ok := command.Stdin.(*os.File); ok {
		fd := int(terminal.Fd())
		foreground, err := unix.IoctlGetInt(fd, unix.TIOCGPGRP)
		if err == nil && foreground == syscall.Getpgrp() {
			attributes.Foreground = true
			attributes.Ctty = fd
		}
	}
	command.SysProcAttr = attributes
	return nil
}

func abortWatchedCommand(command *exec.Cmd) {
	restoreWatchedTerminal(command)
}

func attachWatchedProcess(command *exec.Cmd) (watchedChildPlatform, error) {
	tracker := &watchedProcessTracker{owner: os.Getpid(), root: command.Process.Pid, processes: make(map[int]uint64)}
	platform := watchedChildPlatform{terminalFD: -1, processes: tracker}
	if command.SysProcAttr == nil || !command.SysProcAttr.Foreground {
		if err := tracker.refresh(); err != nil {
			return platform, err
		}
		return platform, startWatchedProcessTracking(tracker)
	}
	platform.terminalFD, platform.foreground = command.SysProcAttr.Ctty, syscall.Getpgrp()
	if err := tracker.refresh(); err != nil {
		return platform, err
	}
	return platform, startWatchedProcessTracking(tracker)
}

func interruptWatchedProcess(platform watchedChildPlatform, command *exec.Cmd, interrupt os.Signal) error {
	sig := syscall.SIGTERM
	if value, ok := interrupt.(syscall.Signal); ok && (value == syscall.SIGHUP || value == syscall.SIGINT || value == syscall.SIGQUIT || value == syscall.SIGTERM) {
		sig = value
	}
	return signalWatchedProcessTree(platform, command, sig)
}

func killWatchedProcess(platform watchedChildPlatform, command *exec.Cmd) error {
	return signalWatchedProcessTree(platform, command, syscall.SIGKILL)
}

func releaseWatchedProcess(platform watchedChildPlatform, command *exec.Cmd) {
	if platform.terminalFD >= 0 {
		_ = setWatchedTerminalForeground(platform.terminalFD, platform.foreground)
	}
	_ = signalWatchedProcessTree(platform, command, syscall.SIGKILL)
	platform.processes.close()
	_ = command.Process.Release()
}

func restoreWatchedTerminal(command *exec.Cmd) {
	if command.SysProcAttr != nil && command.SysProcAttr.Foreground {
		_ = setWatchedTerminalForeground(command.SysProcAttr.Ctty, syscall.Getpgrp())
	}
}

func setWatchedTerminalForeground(fd, group int) error {
	wasIgnored := watchedSignalWasIgnored(syscall.SIGTTOU)
	signal.Ignore(syscall.SIGTTOU)
	if !wasIgnored {
		defer signal.Reset(syscall.SIGTTOU)
	}
	return unix.IoctlSetPointerInt(fd, unix.TIOCSPGRP, group)
}

func waitWatchedProcess(platform watchedChildPlatform, command *exec.Cmd) watchedProcessResult {
	var monitorErr error
	for {
		var status syscall.WaitStatus
		_, err := syscall.Wait4(command.Process.Pid, &status, syscall.WUNTRACED|syscall.WCONTINUED, nil)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			return watchedProcessResult{err: err}
		}
		if status.Stopped() {
			if err := mirrorWatchedProcessStop(platform, command); err != nil {
				monitorErr = err
				_ = signalWatchedProcessTree(platform, command, syscall.SIGKILL)
			}
			continue
		}
		if status.Continued() {
			continue
		}
		if status.Exited() {
			if monitorErr != nil {
				return watchedProcessResult{err: monitorErr}
			}
			if code := status.ExitStatus(); code != 0 {
				return watchedProcessResult{err: fmt.Errorf("exit status %d", code)}
			}
			return watchedProcessResult{}
		}
		if status.Signaled() {
			if monitorErr != nil {
				return watchedProcessResult{err: monitorErr}
			}
			exitSignal := status.Signal()
			result := watchedProcessResult{err: fmt.Errorf("signal: %s", exitSignal)}
			if exitSignal == syscall.SIGINT || exitSignal == syscall.SIGQUIT {
				result.signal = exitSignal
			}
			return result
		}
	}
}

func mirrorWatchedProcessStop(platform watchedChildPlatform, command *exec.Cmd) error {
	if platform.terminalFD < 0 {
		return nil
	}
	if err := signalWatchedDescendants(platform, syscall.SIGSTOP); err != nil {
		return err
	}
	if err := syscall.Kill(-platform.foreground, syscall.SIGSTOP); err != nil {
		return err
	}
	foreground, err := unix.IoctlGetInt(platform.terminalFD, unix.TIOCGPGRP)
	if err == nil && foreground == platform.foreground {
		if err := setWatchedTerminalForeground(platform.terminalFD, command.Process.Pid); err != nil {
			return err
		}
	}
	return signalWatchedProcessTree(platform, command, syscall.SIGCONT)
}

func writeWatchedOutput(writer io.Writer, format string, arguments ...any) {
	terminal, ok := writer.(*os.File)
	if !ok {
		_, _ = fmt.Fprintf(writer, format, arguments...)
		return
	}
	fd := int(terminal.Fd())
	foreground, err := unix.IoctlGetInt(fd, unix.TIOCGPGRP)
	if err != nil || foreground == syscall.Getpgrp() {
		_, _ = fmt.Fprintf(writer, format, arguments...)
		return
	}
	wasIgnored := watchedSignalWasIgnored(syscall.SIGTTOU)
	signal.Ignore(syscall.SIGTTOU)
	_, _ = fmt.Fprintf(writer, format, arguments...)
	if !wasIgnored {
		signal.Reset(syscall.SIGTTOU)
	}
}

func signalWatchedProcessGroup(command *exec.Cmd, signal syscall.Signal) error {
	if command.Process == nil {
		return os.ErrProcessDone
	}
	err := syscall.Kill(-command.Process.Pid, signal)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}

func signalWatchedProcessTree(platform watchedChildPlatform, command *exec.Cmd, value syscall.Signal) error {
	descendantErr := signalWatchedDescendants(platform, value)
	groupErr := signalWatchedProcessGroup(command, value)
	if errors.Is(groupErr, os.ErrProcessDone) {
		groupErr = nil
	}
	return errors.Join(groupErr, descendantErr)
}

func signalWatchedDescendants(platform watchedChildPlatform, value syscall.Signal) error {
	if platform.processes == nil {
		return nil
	}
	return platform.processes.signal(value)
}

func (tracker *watchedProcessTracker) refresh() error {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	return tracker.refreshLocked()
}

func (tracker *watchedProcessTracker) signal(value syscall.Signal) error {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	refreshErr := errors.Join(tracker.eventErr, tracker.refreshLocked())
	pids := make([]int, 0, len(tracker.processes))
	for pid := range tracker.processes {
		pids = append(pids, pid)
	}
	slices.Sort(pids)
	var signalErr error
	for _, pid := range pids {
		if err := syscall.Kill(pid, value); err != nil && !errors.Is(err, syscall.ESRCH) {
			signalErr = errors.Join(signalErr, err)
		}
	}
	return errors.Join(refreshErr, signalErr)
}

func (tracker *watchedProcessTracker) refreshLocked() error {
	snapshot, err := watchedProcessSnapshot()
	if err != nil {
		return err
	}
	byPID := make(map[int]watchedProcessInfo, len(snapshot))
	children := make(map[int][]watchedProcessInfo)
	for _, process := range snapshot {
		byPID[process.pid] = process
		children[process.parent] = append(children[process.parent], process)
	}
	active := make(map[int]uint64, len(tracker.processes))
	queue := []int{tracker.owner, tracker.root}
	seen := map[int]bool{tracker.owner: true, tracker.root: true}
	for pid, started := range tracker.processes {
		if process, ok := byPID[pid]; ok && process.started == started {
			active[pid] = started
			seen[pid] = true
			queue = append(queue, pid)
		}
	}
	for len(queue) != 0 {
		parent := queue[0]
		queue = queue[1:]
		for _, child := range children[parent] {
			if child.pid == tracker.root || seen[child.pid] {
				continue
			}
			if len(active) >= maxWatchedDescendants {
				tracker.processes = active
				return fmt.Errorf("watched process tree exceeds %d descendants", maxWatchedDescendants)
			}
			seen[child.pid] = true
			active[child.pid] = child.started
			queue = append(queue, child.pid)
		}
	}
	tracker.processes = active
	return nil
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

func (tracker *watchedProcessTracker) close() {
	if tracker == nil {
		return
	}
	tracker.mu.Lock()
	stop := tracker.stop
	tracker.stop = nil
	tracker.mu.Unlock()
	if stop != nil {
		stop()
	}
}
