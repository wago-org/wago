//go:build linux && (amd64 || arm64)

package runtime

import "syscall"

const idleNativeStackHotBytes = 512 << 10

func (e *Engine) prepareIdleStackForCache() bool {
	if len(e.stack) > int(DefaultNativeStackBytes) {
		return false
	}
	// Round down so the discard cannot reach the retained top of the stack.
	page := syscall.Getpagesize()
	cold := (len(e.stack) - idleNativeStackHotBytes) &^ (page - 1)
	return madviseDontNeed(e.stack[:cold]) == nil
}
