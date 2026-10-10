//go:build !riscv64

package wago

import (
	"testing"
	"unsafe"
)

const globalDependencyColdRowAllocationBound = 0

func globalDependencyUnitOwner(t *testing.T) *Instance {
	return &Instance{}
}

func TestGlobalDependenciesExistingTargetFootprintAndCounter(t *testing.T) {
	type previous struct{ set importDedup[*Global] }
	var batch globalImportAttachments
	if unsafe.Sizeof(globalDependencyAttachments{}) != 0 || unsafe.Sizeof(batch) != unsafe.Sizeof(previous{}) || unsafe.Offsetof(batch.set) != 0 {
		t.Fatal("existing-target global attachment footprint changed")
	}
	producer := &Instance{}
	g := counterGlobal(ValI32, producer, 0, nil)
	failed := false
	allocs := testing.AllocsPerRun(100, func() {
		var cleanup resourceCleanup
		if batch.attach(g, nil, nil, &cleanup) != nil || batch.attach(g, nil, nil, &cleanup) != nil || producer.resourceRefs != 1 || g.owner.importers != 1 || railshotHasBuilderGlobalDependencies(&batch) {
			failed = true
		}
		batch.detachAll()
	})
	if failed || allocs != 0 || producer.resourceRefs != 0 || g.owner.importers != 0 || producer.importState.Load() != nil {
		t.Fatal("ordinary exact anonymous global acquisition changed", failed, allocs)
	}
}

func TestGlobalDescriptorDependenciesExistingTargetFootprint(t *testing.T) {
	if unsafe.Sizeof(globalDescriptorDependencies{}) != 0 || unsafe.Offsetof(globalOwner{}.mu) != 0 {
		t.Fatal("existing-target global descriptor sidecar changed globalOwner footprint")
	}
}
