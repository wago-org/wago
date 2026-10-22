//go:build !riscv64

package wago

// Existing targets keep no container-receipt sidecar. The batch still defers
// physical release until common callers have dropped native/container locks.
type tableDescriptorDependencies struct{}

type tableDescriptorDependencyBatch struct {
	releases importDedup[*Instance]
}

func railshotRecordTableDescriptorDependencyLocked(*Table, *Instance, *retainedInstanceRoot, *tableDescriptorDependencyBatch) {
}

func railshotPublishTableDescriptorDependencyLocked(*Table, *retainedInstanceRoot, *tableDescriptorDependencyBatch) {
}

func railshotRetireTableDescriptorDependencyLocked(_ *Table, producer *Instance, _ *retainedInstanceRoot, batch *tableDescriptorDependencyBatch) {
	batch.releases.add(producer)
}

func railshotRetryTableDescriptorDependenciesLocked(*Table, *tableDescriptorDependencyBatch) {
}

func railshotFlushTableDescriptorDependencies(batch *tableDescriptorDependencyBatch) {
	batch.releases.drain((*Instance).releaseResourceRoot)
}

func railshotLinkOwnedTableDescriptorHolderLocked(*Instance, *Table) {}
func railshotReleaseOwnedTableDescriptorDependencies(*Instance)      {}
func railshotRetryClosedTableDescriptorRetirement(*Instance) error   { return nil }
