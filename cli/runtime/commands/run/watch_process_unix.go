//go:build linux && !wago_lean

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
const maxWatchedThreads = 4096

type watchedProcessInfo struct {
	pid, parent int
	group       int
	started     uint64
}

type watchedProcessTracker struct {
	mu        sync.Mutex
	owner     int
	root      int
	rootStart uint64
	processes map[int]uint64
	stop      func()
	drain     func() error
	eventErr  error
}

func startWatchedProcess(command *exec.Cmd) (watchedChildPlatform, error) {
	if err := prepareWatchedCommand(command); err != nil {
		return watchedChildPlatform{terminalFD: -1}, err
	}
	unlockStart := lockWatchedCommandStart()
	defer unlockStart()
	if err := command.Start(); err != nil {
		abortWatchedCommand(command)
		return watchedChildPlatform{terminalFD: -1}, err
	}
	if err := waitWatchedCommandStart(command); err != nil {
		abortWatchedCommand(command)
		_ = command.Process.Kill()
		_ = command.Wait()
		return watchedChildPlatform{terminalFD: -1}, err
	}
	platform, err := attachWatchedProcess(command)
	if err != nil {
		abortWatchedCommand(command)
		_ = killWatchedProcess(platform, command)
		_ = command.Process.Kill()
		_ = command.Wait()
		return watchedChildPlatform{terminalFD: -1}, err
	}
	if err := resumeWatchedCommand(command); err != nil {
		_ = killWatchedProcess(platform, command)
		_ = command.Process.Kill()
		_ = command.Wait()
		return watchedChildPlatform{terminalFD: -1}, err
	}
	return platform, nil
}

func prepareWatchedCommand(command *exec.Cmd) error {
	if err := prepareWatchedProcessTracking(); err != nil {
		return err
	}
	attributes := &syscall.SysProcAttr{Setpgid: true}
	configureWatchedCommandStart(attributes)
	if _, _, ok := watchedCommandTerminal(command); ok {
		attributes.Setpgid = false
	}
	command.SysProcAttr = attributes
	return nil
}

func abortWatchedCommand(command *exec.Cmd) {
	restoreWatchedTerminal(command)
}

func attachWatchedProcess(command *exec.Cmd) (watchedChildPlatform, error) {
	root, ok := watchedProcess(command.Process.Pid)
	if !ok {
		return watchedChildPlatform{terminalFD: -1}, errors.New("inspect watched root process")
	}
	tracker := &watchedProcessTracker{owner: os.Getpid(), root: command.Process.Pid, rootStart: root.started, processes: make(map[int]uint64)}
	platform := watchedChildPlatform{terminalFD: -1, processes: tracker}
	if fd, _, ok := watchedCommandTerminal(command); ok {
		platform.terminalFD, platform.foreground = fd, syscall.Getpgrp()
	}
	if command.SysProcAttr == nil || !command.SysProcAttr.Foreground {
		if err := tracker.refresh(); err != nil {
			return platform, err
		}
		return platform, startWatchedProcessTracking(tracker)
	}
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
		foreground, err := unix.IoctlGetInt(platform.terminalFD, unix.TIOCGPGRP)
		if err == nil && watchedTerminalGroupCanRestore(platform.processes, foreground) {
			_ = setWatchedTerminalForeground(platform.terminalFD, platform.foreground)
		}
	}
	_ = signalWatchedProcessTree(platform, command, syscall.SIGKILL)
	platform.processes.close()
	finishWatchedProcessTracking(platform.processes)
	_ = command.Process.Release()
}

func watchedTerminalGroupCanRestore(tracker *watchedProcessTracker, group int) bool {
	if group <= 0 {
		return false
	}
	if tracker != nil && tracker.ownsProcessGroup(group) {
		return true
	}
	return errors.Is(syscall.Kill(-group, 0), syscall.ESRCH)
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
			return watchedProcessResult{err: fmt.Errorf("signal: %s", exitSignal)}
		}
	}
}

func mirrorWatchedProcessStop(platform watchedChildPlatform, command *exec.Cmd) error {
	if platform.terminalFD < 0 {
		return nil
	}
	group, err := syscall.Getpgid(command.Process.Pid)
	if err == nil && group == platform.foreground {
		return nil
	}
	if err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	if err := signalWatchedDescendants(platform, syscall.SIGSTOP); err != nil {
		return err
	}
	if err := syscall.Kill(-platform.foreground, syscall.SIGSTOP); err != nil {
		return err
	}
	return continueWatchedProcess(platform, command)
}

func continueWatchedProcess(platform watchedChildPlatform, command *exec.Cmd) error {
	if platform.terminalFD >= 0 {
		foreground, err := unix.IoctlGetInt(platform.terminalFD, unix.TIOCGPGRP)
		if err == nil && foreground == platform.foreground {
			group, groupErr := syscall.Getpgid(command.Process.Pid)
			if groupErr != nil && !errors.Is(groupErr, syscall.ESRCH) {
				return groupErr
			}
			if groupErr == nil && group != platform.foreground {
				if err := setWatchedTerminalForeground(platform.terminalFD, group); err != nil {
					return err
				}
			}
		}
	}
	return signalWatchedProcessTree(platform, command, syscall.SIGCONT)
}

func watchedCommandTerminal(command *exec.Cmd) (int, int, bool) {
	streams := []any{command.Stdin, command.Stdout, command.Stderr}
	for _, stream := range streams {
		file, ok := stream.(*os.File)
		if !ok {
			continue
		}
		fd := int(file.Fd())
		foreground, err := unix.IoctlGetInt(fd, unix.TIOCGPGRP)
		if err == nil {
			return fd, foreground, true
		}
	}
	return -1, 0, false
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

func signalWatchedProcessGroup(platform watchedChildPlatform, command *exec.Cmd, signal syscall.Signal) error {
	if command.Process == nil {
		return os.ErrProcessDone
	}
	process, ok := watchedProcess(command.Process.Pid)
	if !ok || platform.processes == nil || process.started != platform.processes.rootStart {
		return os.ErrProcessDone
	}
	target := -process.group
	if process.group == syscall.Getpgrp() {
		target = command.Process.Pid
	}
	err := syscall.Kill(target, signal)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}

func signalWatchedProcessTree(platform watchedChildPlatform, command *exec.Cmd, value syscall.Signal) error {
	descendantErr := signalWatchedDescendants(platform, value)
	groupErr := signalWatchedProcessGroup(platform, command, value)
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
		process, ok := watchedProcess(pid)
		if !ok || process.started != tracker.processes[pid] {
			continue
		}
		if err := syscall.Kill(pid, value); err != nil && !errors.Is(err, syscall.ESRCH) {
			signalErr = errors.Join(signalErr, err)
		}
	}
	return errors.Join(refreshErr, signalErr)
}

func (tracker *watchedProcessTracker) ownsProcessGroup(group int) bool {
	if tracker == nil {
		return false
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if root, ok := watchedProcess(tracker.root); ok && root.started == tracker.rootStart && root.group == group {
		return true
	}
	if err := tracker.refreshLocked(); err != nil {
		return false
	}
	for pid, started := range tracker.processes {
		process, ok := watchedProcess(pid)
		if ok && started == process.started && process.group == group {
			return true
		}
	}
	return false
}

func (tracker *watchedProcessTracker) refreshLocked() error {
	descendants, err := watchedProcessDescendants(tracker.owner, tracker.root, tracker.processes, maxWatchedDescendants)
	active := make(map[int]uint64, len(descendants))
	for _, process := range descendants {
		if process.pid != tracker.owner && process.pid != tracker.root {
			active[process.pid] = process.started
		}
	}
	tracker.processes = active
	return err
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

func (tracker *watchedProcessTracker) drainEvents() error {
	if tracker == nil || tracker.drain == nil {
		return nil
	}
	return tracker.drain()
}

func (tracker *watchedProcessTracker) processCount() int {
	if tracker == nil {
		return 0
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	return len(tracker.processes)
}
