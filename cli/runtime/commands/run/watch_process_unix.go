//go:build linux || darwin

package run

import (
	"errors"
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
}

func restoreWatchedTerminal(command *exec.Cmd) {
	if command.SysProcAttr != nil && command.SysProcAttr.Foreground {
		_ = setWatchedTerminalForeground(command.SysProcAttr.Ctty, syscall.Getpgrp())
	}
}

func setWatchedTerminalForeground(fd, group int) error {
	signal.Ignore(syscall.SIGTTOU)
	defer signal.Reset(syscall.SIGTTOU)
	return unix.IoctlSetPointerInt(fd, unix.TIOCSPGRP, group)
}

func watchedProcessExitSignal(_ watchedChildPlatform, err error) os.Signal {
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return nil
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return nil
	}
	signal := status.Signal()
	if signal == syscall.SIGINT || signal == syscall.SIGQUIT {
		return signal
	}
	return nil
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
