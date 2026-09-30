//go:build !tinygo

package runtime

// NativeCancellationSchedulerAvailable reports whether cancellation callbacks
// can run while a native call owns its execution thread.
func NativeCancellationSchedulerAvailable() bool { return true }
