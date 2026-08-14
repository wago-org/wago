//go:build (linux || darwin) && tinygo

package run

import "os"

func watchedSignalWasIgnored(os.Signal) bool {
	return false
}
