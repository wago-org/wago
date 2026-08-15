//go:build linux && !wago_lean

package run

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const watchedSupervisorEnvironmentName = "WAGO_WATCH_SUPERVISOR"
const watchedSupervisorStopGrace = time.Second
const watchedSupervisorCleanupGrace = 500 * time.Millisecond

func watchedSupervisorEnvironment(environment []string) []string {
	return setWatchedSupervisorEnvironment(environment, "1")
}

func setWatchedSupervisorEnvironment(environment []string, value string) []string {
	if environment == nil {
		environment = os.Environ()
	}
	prefix := watchedSupervisorEnvironmentName + "="
	result := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	if value != "" {
		result = append(result, prefix+value)
	}
	return result
}

func maybeSuperviseWatchedChild() bool {
	if os.Getenv(watchedSupervisorEnvironmentName) != "1" {
		return false
	}
	executable, err := os.Executable()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "wago: watch supervisor: %v\n", err)
		os.Exit(1)
	}
	command := exec.Command(executable, os.Args[1:]...)
	command.Env = setWatchedSupervisorEnvironment(nil, "")
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	code, err := superviseWatchedCommand(command, watchedSignals())
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "wago: watch supervisor: %v\n", err)
		code = 1
	}
	os.Exit(code)
	return true
}

func superviseWatchedCommand(command *exec.Cmd, signals []os.Signal) (int, error) {
	if err := prepareWatchedProcessTracking(); err != nil {
		return 1, err
	}
	if err := command.Start(); err != nil {
		return 1, err
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	interrupts := make(chan os.Signal, len(signals))
	signal.Notify(interrupts, signals...)
	defer signal.Stop(interrupts)
	for {
		select {
		case result := <-done:
			if cleanupErr := cleanupWatchedSupervisorProcesses(); cleanupErr != nil {
				return 1, cleanupErr
			}
			return watchedCommandExitCode(result), nil
		case received := <-interrupts:
			if watchedContinueSignal(received) {
				continue
			}
			timer := time.NewTimer(watchedSupervisorStopGrace)
			select {
			case <-done:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
			case <-timer.C:
				_ = command.Process.Kill()
				<-done
			}
			if cleanupErr := cleanupWatchedSupervisorProcesses(); cleanupErr != nil {
				return 1, cleanupErr
			}
			return watchedSignalExitCode(received), nil
		}
	}
}

func watchedCommandExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return 1
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok {
		return 1
	}
	if status.Signaled() {
		return 128 + int(status.Signal())
	}
	return status.ExitStatus()
}

func cleanupWatchedSupervisorProcesses() error {
	deadline := time.Now().Add(watchedSupervisorCleanupGrace)
	var scanErr error
	for {
		processes, err := watchedProcessDescendants(os.Getpid(), nil, maxWatchedDescendants)
		if err != nil && scanErr == nil {
			scanErr = err
		}
		for _, process := range processes {
			if killErr := syscall.Kill(process.pid, syscall.SIGKILL); killErr != nil && !errors.Is(killErr, syscall.ESRCH) {
				scanErr = errors.Join(scanErr, killErr)
			}
		}
		reapWatchedSupervisorChildren()
		if len(processes) == 0 {
			return scanErr
		}
		if time.Now().After(deadline) {
			return errors.Join(scanErr, errors.New("watched process cleanup timed out"))
		}
		time.Sleep(time.Millisecond)
	}
}

func reapWatchedSupervisorChildren() {
	for {
		var status syscall.WaitStatus
		pid, _ := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
		if pid <= 0 {
			return
		}
	}
}
