//go:build linux && tinygo && !wago_lean

package run

import "os"

func watchedSignalWasIgnored(os.Signal) bool {
	return false
}
