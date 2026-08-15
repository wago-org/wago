//go:build (linux || darwin) && !wago_lean

package run

import (
	"os"
	"syscall"
)

func watchedSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGCONT}
}

func watchedContinueSignal(signal os.Signal) bool { return signal == syscall.SIGCONT }

func watchedSignalExitCode(signal os.Signal) int {
	if value, ok := signal.(syscall.Signal); ok {
		return 128 + int(value)
	}
	return 1
}
