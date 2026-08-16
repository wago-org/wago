package wago

import (
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestCallRefOnlyArenaNeedUsesFixedContextHeader(t *testing.T) {
	callRef := MustCompile(parameterFuncrefRelayModule())
	defer callRef.Close()
	if callRef.NeedsFuncRefDescs || !callRef.needsFuncRefContextHeader {
		t.Fatalf("call_ref metadata = full %v header %v, want false/true", callRef.NeedsFuncRefDescs, callRef.needsFuncRefContextHeader)
	}
	// The header requirement must remain O(1) even for a function-heavy consumer.
	// Full per-function descriptors would exceed the bounded arena at this count.
	wide := *callRef
	wide.Funcs = make([]FuncSig, 30_000)
	wide.FuncTypeID = make([]uint64, 30_000)
	withoutHeader := wide
	withoutHeader.needsFuncRefContextHeader = false
	if err := withoutHeader.validateArenaFootprint(); err != nil {
		t.Fatalf("wide baseline footprint: %v", err)
	}
	if err := wide.validateArenaFootprint(); err != nil {
		t.Fatalf("fixed call_ref context header rejected wide consumer: %v", err)
	}
	if got, want := wide.instantiateArenaNeed-withoutHeader.instantiateArenaNeed, coreruntime.FuncRefDescBytes; got != want {
		t.Fatalf("wide call_ref context delta = %d, want fixed header %d", got, want)
	}
	if callRef.boundsMode != BoundsChecksSignalsBased {
		blob, err := callRef.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		var loaded Compiled
		if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
			t.Fatal(err)
		}
		if loaded.NeedsFuncRefDescs || !loaded.needsFuncRefContextHeader {
			t.Fatalf("loaded call_ref metadata = full %v header %v, want false/true", loaded.NeedsFuncRefDescs, loaded.needsFuncRefContextHeader)
		}
	}
}

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
	imports := Imports{"env.log": HostFunc(func(HostModule, []uint64, []uint64) {})}
	if !importer.importsRequireSync(imports, false) {
		t.Fatal("actual host binding did not require synchronous host control")
	}
}

func requireBoundedInstanceFootprint(t *testing.T, got uintptr) {
	t.Helper()
	// Go 1.22 and Go 1.26 lay out synchronization primitives differently.
	// Indexed-memory state and canonical Runtime-domain GC type translation each
	// add one nil sidecar pointer; ordinary single-memory instances retain no
	// additional slice headers.
	if got != 808 && got != 832 && got != 840 && got != 856 && got != 872 && got != 888 {
		t.Fatalf("Instance size = %d, want supported 808-, 832-, 840-, 856-, 872-, or 888-byte layout", got)
	}
}
