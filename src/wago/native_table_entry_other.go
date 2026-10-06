//go:build !riscv64

package wago

type railshotTableEntryLease struct{}

func railshotAcquireNativeTableTargets(*Instance) (railshotTableEntryLease, error) {
	return railshotTableEntryLease{}, nil
}
func railshotReleaseNativeTableTargets(railshotTableEntryLease) {}
func railshotRefreshNativeTableTargets(*Instance)               {}
