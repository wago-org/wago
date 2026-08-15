//go:build linux && !tinygo && !wago_lean

package run

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/wago-org/wago/cli/internal/watchsupervisor"
)

func TestWatchedStopDetectsExitBeforeWaitPublication(t *testing.T) {
	stdin, input, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	command := exec.Command("/bin/sh", "-c", "read value; exit 1")
	command.Stdin, command.Stdout, command.Stderr = stdin, io.Discard, io.Discard
	platform, err := startWatchedProcess(command)
	if err != nil {
		t.Fatal(err)
	}
	if err := input.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		process, ok := watchedProcess(command.Process.Pid)
		if ok && process.state == 'Z' {
			break
		}
		time.Sleep(time.Millisecond)
	}
	process, ok := watchedProcess(command.Process.Pid)
	if !ok || process.state != 'Z' {
		_ = command.Process.Kill()
		t.Fatal("watched root did not exit before its wait result")
	}
	child := &watchedChild{command: command, platform: platform, done: make(chan watchedProcessResult, 1)}
	go func() {
		time.Sleep(50 * time.Millisecond)
		child.done <- waitWatchedProcess(platform, command)
		close(child.done)
	}()
	result, completed, err := child.stop(time.Second, nil)
	if err != nil {
		t.Fatalf("stop exited child: %v", err)
	}
	if !completed || result.exitCode != 1 {
		t.Fatalf("completed = %v, exit code = %d, want true and 1", completed, result.exitCode)
	}
}

func TestWatchedStopErrorRejectsCleanupFailure(t *testing.T) {
	cleanup := errors.New("supervisor cleanup failed")
	if got := watchedStopError(watchedProcessResult{err: cleanup, exitCode: 1}, nil, false); !errors.Is(got, cleanup) {
		t.Fatalf("cleanup failure = %v, want %v", got, cleanup)
	}
	if got := watchedStopError(watchedProcessResult{err: errors.New("exit status 143"), exitCode: 143}, nil, false); got != nil {
		t.Fatalf("expected termination = %v", got)
	}
	if got := watchedStopError(watchedProcessResult{err: errors.New("signal: killed"), exitSignal: syscall.SIGKILL}, nil, true); got != nil {
		t.Fatalf("expected forced stop = %v", got)
	}
}

func detachWatchHelperProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func configureWatchTestSupervisor(options *watchOptions) {
	options.environment = watchsupervisor.Environment(options.environment, os.Args[0])
}
