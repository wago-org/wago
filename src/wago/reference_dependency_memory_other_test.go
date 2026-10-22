//go:build !riscv64

package wago

import (
	"testing"
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestMemoryDependenciesExistingTargetFootprintAndCounter(t *testing.T) {
	type previous struct{ set importDedup[*Memory] }
	var batch memoryImportAttachments
	if unsafe.Sizeof(memoryDependencyAttachments{}) != 0 || unsafe.Sizeof(batch) != unsafe.Sizeof(previous{}) || unsafe.Offsetof(batch.set) != 0 {
		t.Fatal("existing-target memory attachment footprint changed")
	}
	producer := &Instance{}
	state := &memoryState{owner: producer}
	state.set(memoryStateShared, true)
	memory := &Memory{jm: &coreruntime.JobMemory{}}
	memory.state.Store(state)
	failed := false
	allocs := testing.AllocsPerRun(100, func() {
		var cleanup resourceCleanup
		if batch.attach(memory, &cleanup) != nil || batch.attach(memory, &cleanup) != nil || producer.resourceRefs != 1 || state.importerCount() != 1 || railshotHasBuilderMemoryDependencies(&batch) {
			failed = true
		}
		batch.detachAll()
	})
	if failed || allocs != 0 || producer.resourceRefs != 0 || state.importerCount() != 0 || producer.importState.Load() != nil {
		t.Fatal("ordinary exact anonymous memory acquisition changed", failed, allocs)
	}
}
