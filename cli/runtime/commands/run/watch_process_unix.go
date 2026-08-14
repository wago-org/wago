//go:build linux || darwin

package run

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"golang.org/x/sys/unix"
)

type watchedChildPlatform struct {
	terminalFD int
	foreground int
}

func prepareWatchedCommand(command *exec.Cmd) {
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
}

func abortWatchedCommand(command *exec.Cmd) {
	restoreWatchedTerminal(command)
}

func attachWatchedProcess(command *exec.Cmd) (watchedChildPlatform, error) {
	if command.SysProcAttr == nil || !command.SysProcAttr.Foreground {
		return watchedChildPlatform{terminalFD: -1}, nil
	}
	return watchedChildPlatform{terminalFD: command.SysProcAttr.Ctty, foreground: syscall.Getpgrp()}, nil
}

func interruptWatchedProcess(_ watchedChildPlatform, command *exec.Cmd, interrupt os.Signal) error {
	sig := syscall.SIGTERM
	if value, ok := interrupt.(syscall.Signal); ok && (value == syscall.SIGINT || value == syscall.SIGQUIT || value == syscall.SIGTERM) {
		sig = value
	}
	return signalWatchedProcessGroup(command, sig)
}

func killWatchedProcess(_ watchedChildPlatform, command *exec.Cmd) error {
	return signalWatchedProcessGroup(command, syscall.SIGKILL)
}

func releaseWatchedProcess(platform watchedChildPlatform, command *exec.Cmd) {
	if platform.terminalFD >= 0 {
		_ = setWatchedTerminalForeground(platform.terminalFD, platform.foreground)
	}
	_ = signalWatchedProcessGroup(command, syscall.SIGKILL)
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
				_ = signalWatchedProcessGroup(command, syscall.SIGKILL)
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
	if err := syscall.Kill(-platform.foreground, syscall.SIGSTOP); err != nil {
		return err
	}
	foreground, err := unix.IoctlGetInt(platform.terminalFD, unix.TIOCGPGRP)
	if err == nil && foreground == platform.foreground {
		if err := setWatchedTerminalForeground(platform.terminalFD, command.Process.Pid); err != nil {
			return err
		}
	}
	return signalWatchedProcessGroup(command, syscall.SIGCONT)
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
