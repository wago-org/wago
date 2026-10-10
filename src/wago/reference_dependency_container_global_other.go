//go:build !riscv64

package wago

// Existing targets keep no container-receipt sidecar. The batch still defers
// physical release until the common caller has dropped native/container locks.
type globalDescriptorDependencies struct{}

type globalDescriptorDependencyBatch struct {
	releases importDedup[*Instance]
}

func railshotRecordGlobalDescriptorDependencyLocked(*globalOwner, *Instance, *retainedInstanceRoot, *globalDescriptorDependencyBatch) {
}

func railshotPublishGlobalDescriptorDependencyLocked(*globalOwner, *retainedInstanceRoot, *globalDescriptorDependencyBatch) {
}

func railshotRetireGlobalDescriptorDependencyLocked(_ *globalOwner, producer *Instance, _ *retainedInstanceRoot, batch *globalDescriptorDependencyBatch) {
	batch.releases.add(producer)
}

func railshotRetryGlobalDescriptorDependenciesLocked(*globalOwner, *globalDescriptorDependencyBatch) {
}

func railshotFlushGlobalDescriptorDependencies(batch *globalDescriptorDependencyBatch) {
	batch.releases.drain((*Instance).releaseResourceRoot)
}

func railshotLinkOwnedGlobalDescriptorHolderLocked(*Instance, *globalOwner) {}
func railshotReleaseOwnedGlobalDescriptorDependencies(*Instance)            {}
func railshotRetryClosedGlobalDescriptorRetirement(*Instance) error         { return nil }
