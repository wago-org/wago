//go:build windows

package run

import "os"

func watchedSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}

func watchedContinueSignal(os.Signal) bool { return false }

func watchedSignalExitCode(os.Signal) int { return 130 }
