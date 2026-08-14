//go:build linux || darwin

package run

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

type watchedChildPlatform struct{}

func prepareWatchedCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func attachWatchedProcess(*exec.Cmd) (watchedChildPlatform, error) {
	return watchedChildPlatform{}, nil
}

func interruptWatchedProcess(_ watchedChildPlatform, command *exec.Cmd, interrupt os.Signal) error {
	sig := syscall.SIGTERM
	if value, ok := interrupt.(syscall.Signal); ok && (value == syscall.SIGINT || value == syscall.SIGTERM) {
		sig = value
	}
	return signalWatchedProcessGroup(command, sig)
}

func killWatchedProcess(_ watchedChildPlatform, command *exec.Cmd) error {
	return signalWatchedProcessGroup(command, syscall.SIGKILL)
}

func releaseWatchedProcess(_ watchedChildPlatform, command *exec.Cmd) {
	_ = signalWatchedProcessGroup(command, syscall.SIGKILL)
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
