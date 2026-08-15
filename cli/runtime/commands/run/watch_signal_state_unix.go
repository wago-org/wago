//go:build (linux || darwin) && !tinygo && !wago_lean

package run

import (
	"os"
	"os/signal"
)

func watchedSignalWasIgnored(value os.Signal) bool {
	return signal.Ignored(value)
}
