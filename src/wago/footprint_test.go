package wago

import (
	"testing"
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestSyncHostBindingStaysCompact(t *testing.T) {
	// One tagged callback, eight dedicated hot representations, three pointers,
	// the index, and a
	// classification byte.
	// TinyGo function values are larger than standard Go function values. Keep
	// the budget exact for this compiler instead of accepting either footprint.
	want := unsafe.Sizeof(any(nil)) + unsafe.Sizeof(noArgsHostFunc(nil)) + unsafe.Sizeof(i32HostFunc(nil)) +
		unsafe.Sizeof(i32ToI32HostFunc(nil)) + unsafe.Sizeof(i32I32HostFunc(nil)) +
		unsafe.Sizeof(i32I32ToI32HostFunc(nil)) + unsafe.Sizeof(i32ToI32I32HostFunc(nil)) +
		unsafe.Sizeof(i32I32ToI32I32HostFunc(nil)) + unsafe.Sizeof((func(int64) int64)(nil)) +
		3*unsafe.Sizeof((*DefinedTypeDescriptor)(nil)) + 5
	align := unsafe.Alignof(syncHostBinding{})
	want = (want + align - 1) &^ (align - 1)
	if got := unsafe.Sizeof(syncHostBinding{}); got != want {
		t.Fatalf("syncHostBinding size = %d, want %d", got, want)
	}
}

func TestFunctionImportArenaNeedUsesConcreteBindingShape(t *testing.T) {
	compiled := MustCompile(voidI32ImportCallerModule())
	defer compiled.Close()
	baseline := compiled.instantiateArenaNeed
	if got := compiled.arenaNeedForImports(testImports("env.log", slotHostFunc(func(HostModule, []uint64, []uint64) {})).bindings, false); got != baseline {
		t.Fatalf("async host arena need = %d, want baseline %d", got, baseline)
	}
	crossWant := baseline - coreruntime.HostCallLogBytes
	if got := compiled.arenaNeedForImports(testImports("env.log", &InstanceExport{}).bindings, false); got != crossWant {
		t.Fatalf("cross-only arena need = %d, want %d", got, crossWant)
	}
	syncWant := crossWant + coreruntime.HostCtrlFrameBytes
	if got := compiled.arenaNeedForImports(testImports("env.log", slotHostFunc(func(HostModule, []uint64, []uint64) {})).bindings, true); got != syncWant {
		t.Fatalf("sync host arena need = %d, want %d", got, syncWant)
	}
}

func TestNoImportSynchronousArenaNeedIncludesControlFrame(t *testing.T) {
	compiled := MustCompile(wasmtest.Module())
	defer compiled.Close()
	want := compiled.instantiateArenaNeed + coreruntime.HostCtrlFrameBytes
	if got := compiled.arenaNeedForImports(nil, true); got != want {
		t.Fatalf("no-import sync arena need = %d, want %d", got, want)
	}
	if got := compiled.arenaNeedForImports(nil, false); got != compiled.instantiateArenaNeed {
		t.Fatalf("no-import async arena need = %d, want baseline %d", got, compiled.instantiateArenaNeed)
	}
}

func TestHostControlModeDependsOnModuleBoundaryNotArchitecture(t *testing.T) {
	compiled := MustCompile(wasmtest.Module())
	defer compiled.Close()
	if compiled.importsRequireSync(nil, false) {
		t.Fatal("host-free module unexpectedly requires synchronous host control")
	}
	if !compiled.importsRequireSync(nil, true) {
		t.Fatal("explicit synchronous-host option was ignored")
	}

	importer := MustCompile(voidI32ImportCallerModule())
	defer importer.Close()
	imports := testImports("env.log", slotHostFunc(func(HostModule, []uint64, []uint64) {}))
	if !importer.importsRequireSync(imports.bindings, false) {
		t.Fatal("actual host binding did not require synchronous host control")
	}
}

func requireBoundedInstanceFootprint(t *testing.T, got uintptr) {
	t.Helper()
	// Go 1.22 and Go 1.26 lay out synchronization primitives differently.
	// Indexed-memory state, canonical Runtime-domain GC type translation, and
	// close state each add one nil sidecar pointer; ordinary single-memory
	// instances retain no additional slice headers.
	if got != 808 && got != 832 && got != 840 && got != 856 && got != 872 && got != 888 && got != 896 {
		t.Fatalf("Instance size = %d, want supported 808-, 832-, 840-, 856-, 872-, 888-, or 896-byte layout", got)
	}
}
