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

	"github.com/wago-org/wago/cli/internal/watchsupervisor"
	"golang.org/x/sys/unix"
)

type watchedChildPlatform struct {
	terminalFD int
	foreground int
	processes  *watchedProcessTracker
	lifetime   *watchsupervisor.ChildLifetime
}

const maxWatchedDescendants = 4096
const maxWatchedThreads = 4096

type watchedProcessInfo struct {
	pid, parent int
	group       int
	state       byte
	started     uint64
}

type watchedProcessTracker struct {
	mu                sync.Mutex
	owner             int
	root              int
	rootStart         uint64
	processes         map[int]uint64
	stop              func()
	drain             func() error
	eventErr          error
	stoppedForeground int
}

func startWatchedProcess(command *exec.Cmd) (watchedChildPlatform, error) {
	lifetime, err := prepareWatchedCommand(command)
	if err != nil {
		return watchedChildPlatform{terminalFD: -1}, err
	}
	started := false
	defer func() {
		if !started {
			_ = lifetime.Close()
		}
	}()
	unlockStart := lockWatchedCommandStart()
	defer unlockStart()
	if err := command.Start(); err != nil {
		abortWatchedCommand(command)
		return watchedChildPlatform{terminalFD: -1}, err
	}
	lifetime.Started()
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
	platform.lifetime = lifetime
	started = true
	return platform, nil
}

func prepareWatchedCommand(command *exec.Cmd) (*watchsupervisor.ChildLifetime, error) {
	if err := prepareWatchedProcessTracking(); err != nil {
		return nil, err
	}
	attributes := &syscall.SysProcAttr{Setpgid: true}
	configureWatchedCommandStart(attributes)
	if _, _, ok := watchedCommandTerminal(command); ok {
		attributes.Setpgid = false
	}
	command.SysProcAttr = attributes
	return watchsupervisor.BindChild(command)
}

func abortWatchedCommand(command *exec.Cmd) {
	restoreWatchedTerminal(command)
}

func attachWatchedProcess(command *exec.Cmd) (watchedChildPlatform, error) {
	root, ok := watchedProcess(command.Process.Pid)
	if !ok {
		return watchedChildPlatform{terminalFD: -1}, errors.New("inspect watched root process")
	}
	tracker := &watchedProcessTracker{
		owner: os.Getpid(), root: command.Process.Pid, rootStart: root.started,
		processes: make(map[int]uint64),
	}
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

func interruptWatchedProcess(platform watchedChildPlatform, command *exec.Cmd, interrupt os.Signal) (bool, error) {
	sig := syscall.SIGTERM
	if value, ok := interrupt.(syscall.Signal); ok && (value == syscall.SIGHUP || value == syscall.SIGINT || value == syscall.SIGQUIT || value == syscall.SIGTERM) {
		sig = value
	}
	if sig == syscall.SIGINT || sig == syscall.SIGQUIT {
		if group, ok := watchedTerminalSignalGroup(platform); ok {
			return signalWatchedProcessTreeResult(platform, command, sig, group)
		}
	}
	return signalWatchedProcessTreeResult(platform, command, sig, 0)
}

func watchedTerminalSignalGroup(platform watchedChildPlatform) (int, bool) {
	if platform.terminalFD < 0 {
		return 0, false
	}
	group, err := unix.IoctlGetInt(platform.terminalFD, unix.TIOCGPGRP)
	return group, err == nil && group == platform.foreground
}

func killWatchedProcess(platform watchedChildPlatform, command *exec.Cmd) error {
	return signalWatchedProcessTree(platform, command, syscall.SIGKILL)
}

func releaseWatchedProcess(platform watchedChildPlatform, command *exec.Cmd) error {
	var terminalErr error
	if platform.terminalFD >= 0 {
		foreground, err := unix.IoctlGetInt(platform.terminalFD, unix.TIOCGPGRP)
		if err == nil && watchedTerminalGroupCanRestore(platform.processes, foreground) {
			terminalErr = setWatchedTerminalForeground(platform.terminalFD, platform.foreground)
		}
	}
	signalErr := signalWatchedProcessTree(platform, command, syscall.SIGKILL)
	platform.processes.close()
	finishErr := finishWatchedProcessTracking(platform.processes)
	releaseErr := command.Process.Release()
	if errors.Is(releaseErr, os.ErrProcessDone) {
		releaseErr = nil
	}
	return errors.Join(terminalErr, signalErr, finishErr, releaseErr, platform.lifetime.Close())
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
				return watchedProcessResult{err: fmt.Errorf("exit status %d", code), exitCode: code}
			}
			return watchedProcessResult{}
		}
		if status.Signaled() {
			if monitorErr != nil {
				return watchedProcessResult{err: monitorErr}
			}
			exitSignal := status.Signal()
			return watchedProcessResult{err: fmt.Errorf("signal: %s", exitSignal), exitSignal: exitSignal}
		}
	}
}

func watchedStopError(result watchedProcessResult, interrupt os.Signal, forced bool) error {
	if result.err == nil {
		return nil
	}
	expected := syscall.SIGTERM
	if value, ok := interrupt.(syscall.Signal); ok &&
		(value == syscall.SIGHUP || value == syscall.SIGINT || value == syscall.SIGQUIT || value == syscall.SIGTERM) {
		expected = value
	}
	if !forced && result.exitCode == 128+int(expected) {
		return nil
	}
	if !forced && result.exitSignal == expected {
		return nil
	}
	if forced && result.exitSignal == syscall.SIGKILL {
		return nil
	}
	return result.err
}

func mirrorWatchedProcessStop(platform watchedChildPlatform, command *exec.Cmd) error {
	if platform.terminalFD < 0 {
		return nil
	}
	root, ok := watchedRootProcess(platform, command)
	if !ok || (root.state != 'T' && root.state != 't') {
		return nil
	}
	foreground, err := unix.IoctlGetInt(platform.terminalFD, unix.TIOCGPGRP)
	if err != nil {
		return err
	}
	platform.processes.rememberStoppedForeground(foreground)
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
			group := platform.processes.stoppedForegroundGroup()
			if group > 0 && group != platform.foreground && platform.processes.ownsProcessGroup(group) {
				if err := setWatchedTerminalForeground(platform.terminalFD, group); err != nil {
					return err
				}
				platform.processes.clearStoppedForeground(group)
			} else {
				platform.processes.clearStoppedForeground(group)
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
		} else if err == nil {
			platform.processes.clearStoppedForeground(foreground)
		}
	}
	return signalWatchedProcessTree(platform, command, syscall.SIGCONT)
}

func (tracker *watchedProcessTracker) rememberStoppedForeground(group int) {
	if tracker == nil || group <= 0 {
		return
	}
	tracker.mu.Lock()
	tracker.stoppedForeground = group
	tracker.mu.Unlock()
}

func (tracker *watchedProcessTracker) stoppedForegroundGroup() int {
	if tracker == nil {
		return 0
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	return tracker.stoppedForeground
}

func (tracker *watchedProcessTracker) clearStoppedForeground(group int) {
	if tracker == nil {
		return
	}
	tracker.mu.Lock()
	if tracker.stoppedForeground == group {
		tracker.stoppedForeground = 0
	}
	tracker.mu.Unlock()
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

func signalWatchedProcessGroup(process watchedProcessInfo, command *exec.Cmd, signal syscall.Signal, excludedGroup int) error {
	if process.group == excludedGroup {
		return nil
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
	_, err := signalWatchedProcessTreeResult(platform, command, value, 0)
	return err
}

func signalWatchedProcessTreeExceptGroup(platform watchedChildPlatform, command *exec.Cmd, value syscall.Signal, excludedGroup int) error {
	_, err := signalWatchedProcessTreeResult(platform, command, value, excludedGroup)
	return err
}

func signalWatchedProcessTreeResult(platform watchedChildPlatform, command *exec.Cmd, value syscall.Signal, excludedGroup int) (bool, error) {
	root, rootOK := watchedRootProcess(platform, command)
	rootGroup := 0
	if rootOK && root.group != syscall.Getpgrp() {
		rootGroup = root.group
	}
	descendantErr := signalWatchedDescendantsExceptGroups(platform, value, excludedGroup, rootGroup)
	var groupErr error
	rootDone := !rootOK
	if rootOK {
		current, currentOK := watchedProcess(root.pid)
		rootDone = !currentOK || current.started != root.started || current.state == 'Z' || current.state == 'X'
		if !rootDone {
			groupErr = signalWatchedProcessGroup(current, command, value, excludedGroup)
		}
	}
	if errors.Is(groupErr, os.ErrProcessDone) {
		rootDone = true
		groupErr = nil
	}
	return rootDone, errors.Join(groupErr, descendantErr)
}

func watchedRootProcess(platform watchedChildPlatform, command *exec.Cmd) (watchedProcessInfo, bool) {
	if command.Process == nil || platform.processes == nil {
		return watchedProcessInfo{}, false
	}
	process, ok := watchedProcess(command.Process.Pid)
	return process, ok && process.started == platform.processes.rootStart
}

func signalWatchedDescendants(platform watchedChildPlatform, value syscall.Signal) error {
	if platform.processes == nil {
		return nil
	}
	return platform.processes.signal(value)
}

func signalWatchedDescendantsExceptGroups(platform watchedChildPlatform, value syscall.Signal, firstGroup, secondGroup int) error {
	if platform.processes == nil {
		return nil
	}
	return platform.processes.signalExceptGroups(value, firstGroup, secondGroup)
}

func (tracker *watchedProcessTracker) refresh() error {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	return tracker.refreshLocked()
}

func (tracker *watchedProcessTracker) signal(value syscall.Signal) error {
	return tracker.signalExceptGroups(value, 0, 0)
}

func (tracker *watchedProcessTracker) signalExceptGroups(value syscall.Signal, firstGroup, secondGroup int) error {
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
		if !ok || process.started != tracker.processes[pid] || process.group == firstGroup || process.group == secondGroup {
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
	descendants, err := watchedProcessDescendants(tracker.root, tracker.processes, maxWatchedDescendants)
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
