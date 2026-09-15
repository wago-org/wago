//go:build tinygo && scheduler.threads

package runtime

func NativeCancellationSchedulerAvailable() bool { return true }
