//go:build windows

package run

import "os"

func watchedSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}

func watchedSignalExitCode(os.Signal) int { return 130 }
