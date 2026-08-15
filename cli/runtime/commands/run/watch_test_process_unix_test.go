//go:build linux && !tinygo && !wago_lean

package run

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"

	"github.com/wago-org/wago/cli/internal/watchsupervisor"
)

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
