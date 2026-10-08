//go:build !riscv64

package wago

type railshotTableEntryLease struct{}

func railshotRequiresNativeOwnerScope(*Compiled) bool { return false }

func railshotAcquireNativeTableTargets(*Instance) (railshotTableEntryLease, error) {
	return railshotTableEntryLease{}, nil
}
func railshotReleaseNativeTableTargets(railshotTableEntryLease) {}
func railshotCurrentNativeTableTargets(*Instance) railshotTableEntryLease {
	return railshotTableEntryLease{}
}
func railshotRefreshNativeTableTargets(railshotTableEntryLease) {}

func railshotRetireNativeTableTargets(railshotTableEntryLease) error { return nil }
func railshotRetryNativeTableRetirement(*Instance) error             { return nil }

func railshotRetryClosedNativeTableRetirement(*Instance) error { return nil }
