//go:build !riscv64

package wago

// The holder is not a trailing field, so its zero size adds no sidecar padding.
type referenceRootAccounting struct{}

func railshotResourceRootRetainAllowedLocked(*Instance) bool { return true }
func railshotResourceRootAcquiredLocked(*Instance)           {}
func railshotResourceRootReleasedLocked(*Instance)           {}
func railshotDependencyComponentReleaseCompleted(*Instance)  {}
