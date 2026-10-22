//go:build !linux || !riscv64 || tinygo || wago_target_tinygo

package wago

import (
	"fmt"
	coreruntime "github.com/wago-org/wago/src/core/runtime"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func compactExternWordReserved(*referenceStore, uint64) bool { return false }

func railshotClosedCompactExternHostObjectsLocked(*referenceStore, uint32) bool { return false }

func railshotCompactExternUnpinnedAfterClose(*referenceStore) {}

func visitCompactExternFrameRoot(*Instance, *gc.Collector, uint64, func(gc.RootSlot) bool, gc.RootRefSink) bool {
	return false
}

func (*Instance) dispatchCompactExternConversion(*gcStoreDomain, uint32, uint64, gc.RootSet) uint64 {
	panic("wago: compact extern conversion is unavailable")
}

func (*Instance) issueCompactExternPublicToken(uint64) (uint64, error) {
	return 0, fmt.Errorf("compact extern results are unavailable")
}

func compactForeignAnyToken(*gc.Collector, gc.Ref) (uint64, bool) { return 0, false }
func compactForeignAnyMatches(*gc.Collector, gc.Ref, ValueTypeDescriptor) (bool, bool) {
	return false, false
}
func (*Instance) stageCompactExternAnyArgument(uint64, ValueTypeDescriptor) (gc.Ref, error) {
	return gc.Null(), fmt.Errorf("compact extern arguments are unavailable")
}

func releaseInstanceCompactExtern(*Instance) {}
func rangeLocalCompactExternRoots(*Instance, func(gc.RootSlot) bool, gc.RootRefSink) bool {
	return true
}

func (*Instance) dispatchCompactExternStore(uintptr, uint32, uint32, []uint64, []uint64, *coreruntime.Engine) bool {
	return false
}

func (*Instance) storeCompactExternTable(uint64, uint64, uint64, *gcStoreDomain, gc.RootSet) bool {
	return false
}

func (*Instance) dispatchCompactExternTableBulk(uint32, []uint64, []uint64, *gcStoreDomain, gc.RootSet) bool {
	return false
}

func gcRetainedTableDescriptor(in *Instance, index int) []byte { return in.tableDescriptor(index) }

type compactExternConstruction struct{}

func compactExternConstructionNeeded(*Compiled) bool { return false }
func newCompactExternConstruction(*Compiled, *gc.Collector, *referenceStore, *gcStoreDomain, *gc.Slots) *compactExternConstruction {
	return nil
}
func (j *compactExternConstruction) roots(roots gc.RootSet) gc.RootSet { return roots }
func (j *compactExternConstruction) convert(uint32, gcConstStackValue, gc.RootSet) (uint64, error) {
	return 0, fmt.Errorf("compact extern constants unavailable")
}
func (j *compactExternConstruction) record([]byte, *Instance) error { return nil }
func (j *compactExternConstruction) bind(*Instance) error           { return nil }
func (j *compactExternConstruction) close()                         {}

func (*Instance) compactExternPersistentMayAllocate(uint32, []uint64) bool { return false }

func railshotAttachCompactExternHostTable(*Table, *referenceStore, *gc.Collector) error { return nil }
func railshotCompactExternHostTableCollector(*Table, *referenceStore) (*gc.Collector, error) {
	return nil, nil
}
func railshotCompactExternHostTableMatches(*Table, *referenceStore, *gc.Collector) bool { return false }
func railshotReleaseCompactExternHostTable(*tableOwner)                                 {}

func railshotTryCloseCompactExternHostTable(*Table) (bool, error) { return false, nil }
func railshotFinishCompactExternHostTableDetach(*Table)           {}

func railshotCompactExternHostTableOpen(*tableOwner) bool { return true }

func railshotAttachCompactExternHostGlobal(*Global, *referenceStore, *gc.Collector) error { return nil }
func railshotCompactExternHostGlobalCollector(*Global, *referenceStore) (*gc.Collector, error) {
	return nil, nil
}
func railshotCompactExternHostGlobalMatches(*Global, *referenceStore, *gc.Collector) bool {
	return false
}
func railshotCompactExternHostGlobalOpen(*globalOwner) bool         { return true }
func railshotReleaseCompactExternHostGlobal(*globalOwner)           {}
func railshotTryCloseCompactExternHostGlobal(*Global) (bool, error) { return false, nil }
func railshotFinishCompactExternHostGlobalDetach(*Global)           {}

func railshotValidCompactExternHostGlobalWord(*Global, *referenceStore, *gc.Collector, uint64) bool {
	return false
}

func railshotGetCompactExternHostGlobal(*Global) (Value, bool, error) { return Value{}, false, nil }

func railshotSetCompactExternHostGlobal(*Global, Value, *globalValueRoots) (bool, error) {
	return false, nil
}
