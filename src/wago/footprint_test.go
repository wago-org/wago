package wago

import (
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestFunctionImportArenaNeedUsesConcreteBindingShape(t *testing.T) {
	compiled := MustCompile(voidI32ImportCallerModule())
	defer compiled.Close()
	baseline := compiled.instantiateArenaNeed
	if got := compiled.arenaNeedForImports(Imports{"env.log": HostFunc(func(HostModule, []uint64, []uint64) {})}, false); got != baseline {
		t.Fatalf("async host arena need = %d, want baseline %d", got, baseline)
	}
	crossWant := baseline - coreruntime.HostCallLogBytes
	if got := compiled.arenaNeedForImports(Imports{"env.log": &InstanceExport{}}, false); got != crossWant {
		t.Fatalf("cross-only arena need = %d, want %d", got, crossWant)
	}
	syncWant := crossWant + coreruntime.HostCtrlFrameBytes
	if got := compiled.arenaNeedForImports(Imports{"env.log": HostFunc(func(HostModule, []uint64, []uint64) {})}, true); got != syncWant {
		t.Fatalf("sync host arena need = %d, want %d", got, syncWant)
	}
}

func requireBoundedInstanceFootprint(t *testing.T, got uintptr) {
	t.Helper()
	// Go 1.22 and Go 1.26 lay out synchronization primitives differently.
	// Packing lifecycle booleans around the arena-backed native-context pointer
	// keeps both supported layouts below the prior 864-byte ceiling.
	if got != 776 && got != 856 {
		t.Fatalf("Instance size = %d, want supported 776- or 856-byte layout", got)
	}
}
