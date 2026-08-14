//go:build linux || darwin

package run

import (
	"os"
	"syscall"
)

func watchedSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}

func watchedSignalExitCode(signal os.Signal) int {
	if value, ok := signal.(syscall.Signal); ok {
		return 128 + int(value)
	}
	return 1
}
