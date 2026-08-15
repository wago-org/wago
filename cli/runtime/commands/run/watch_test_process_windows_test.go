//go:build windows && !tinygo && !wago_lean

package run

import (
	"errors"
	"os/exec"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWatchedWindowsConsoleUnavailableFallsBack(t *testing.T) {
	if err := watchedWindowsInterruptError(nil); err != nil {
		t.Fatalf("successful interrupt = %v", err)
	}
	for _, err := range []error{windows.ERROR_INVALID_HANDLE, windows.ERROR_INVALID_PARAMETER} {
		if !errors.Is(watchedWindowsInterruptError(err), errWatchedGracefulStopUnavailable) {
			t.Fatalf("interrupt error %v did not request forced fallback", err)
		}
	}
	want := errors.New("console failure")
	if got := watchedWindowsInterruptError(want); !errors.Is(got, want) {
		t.Fatalf("interrupt error = %v, want %v", got, want)
	}
}

func detachWatchHelperProcess(*exec.Cmd) {}

func configureWatchTestSupervisor(*watchOptions) {}
