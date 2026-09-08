//go:build arm64 && (linux || darwin)

package wago

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/wasmtest"
)

func arm64GCStructModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	funcType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	body := []byte{0x00, 0x20, 0x00, 0xfb, 0x00, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("roundtrip", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func arm64GCSparseLiveFrameRootModule(count uint32) []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	locals := append([]byte{0x01}, wasmtest.ULEB(count)...)
	locals = append(locals, 0x63, 0x00)
	body := append(locals, 0xfb, 0x01, 0x00, 0x1a, 0x20, 0x00, 0x1a, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestGCArm64NativeRootAdmissionCompactsDeadDeclaredLocals(t *testing.T) {
	const declaredRoots = 1138
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), arm64GCSparseLiveFrameRootModule(declaredRoots))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	status := compiled.GCNativeRootAdmission()
	if !status.Exact || status.Safepoints != 1 || status.MaximumRoots != 1 {
		t.Fatalf("%d-declared-root arm64 admission = %+v", declaredRoots, status)
	}
	in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 32, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("run"); err != nil {
		t.Fatal(err)
	}
}

func TestARM64GCNativeABIArtifactAndInstantiationPreflight(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), arm64GCStructModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if got := compiled.nativeGCABIRequirement(); got != gc.NativeABIVersion {
		t.Fatalf("native GC ABI = %d, want %d", got, gc.NativeABIVersion)
	}
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := gc.ValidateNativeInstanceView(in.gcNativeView, in.gc, uint32(len(in.c.Types))); err != nil {
		t.Fatalf("instantiated native GC view: %v", err)
	}
	_ = in.Close()

	compiled.codeCache.setNativeGCABIVersion(gc.NativeABIVersion + 1)
	blob, err := marshalCompiled(compiled)
	compiled.codeCache.setNativeGCABIVersion(gc.NativeABIVersion)
	if err != nil {
		t.Fatal(err)
	}
	var loaded Compiled
	if err := loaded.UnmarshalBinary(blob); err == nil || !strings.Contains(err.Error(), "native ABI version") {
		t.Fatalf("mismatched native GC artifact = %v", err)
	}
}

func arm64GCReferenceConstructorModule() []byte {
	childType := []byte{0x5f, 0x01, 0x7f, 0x01}
	parentType := []byte{0x5f, 0x01, 0x63, 0x00, 0x01}
	funcType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	body := []byte{0x00,
		0xfb, 0x01, 0x00,
		0xfb, 0x00, 0x01,
		0xfb, 0x02, 0x01, 0x00,
		0xfb, 0x02, 0x00, 0x00,
		0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(childType, parentType, funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("construct", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func arm64GCAllocationLoopModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	funcType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	body := []byte{0x02, 0x01, 0x63, 0x00, 0x01, 0x7f,
		0xfb, 0x01, 0x00, 0x21, 0x01,
		0x02, 0x40, 0x03, 0x40,
		0x20, 0x02, 0x20, 0x00, 0x4f, 0x0d, 0x01,
		0xfb, 0x01, 0x00, 0x1a,
		0x20, 0x02, 0x41, 0x01, 0x6a, 0x21, 0x02, 0x0c, 0x00,
		0x0b, 0x0b,
		0x20, 0x01, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("allocate", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func arm64GCHiddenOperandLoopModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	funcType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	body := []byte{0x01, 0x01, 0x7f,
		0xfb, 0x01, 0x00,
		0x02, 0x40, 0x03, 0x40,
		0x20, 0x01, 0x20, 0x00, 0x4f, 0x0d, 0x01,
		0xfb, 0x01, 0x00, 0x1a,
		0x20, 0x01, 0x41, 0x01, 0x6a, 0x21, 0x01, 0x0c, 0x00,
		0x0b, 0x0b,
		0xfb, 0x02, 0x00, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("allocate", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func arm64GCCrossFunctionModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	funcType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	callee := []byte{0x01, 0x01, 0x7f,
		0x02, 0x40, 0x03, 0x40,
		0x20, 0x01, 0x20, 0x00, 0x4f, 0x0d, 0x01,
		0xfb, 0x01, 0x00, 0x1a,
		0x20, 0x01, 0x41, 0x01, 0x6a, 0x21, 0x01, 0x0c, 0x00,
		0x0b, 0x0b,
		0x20, 0x01, 0x0b}
	caller := []byte{0x01, 0x03, 0x7e,
		0xfb, 0x01, 0x00,
		0x20, 0x00, 0x10, 0x00, 0x1a,
		0xfb, 0x02, 0x00, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(callee))), callee...),
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
		)),
	)
}

func arm64GCRecursiveModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	funcType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	body := []byte{0x01, 0x01, 0x63, 0x00,
		0xfb, 0x01, 0x00, 0x21, 0x01,
		0x20, 0x00, 0x45, 0x04, 0x40,
		0x05,
		0x20, 0x00, 0x41, 0x01, 0x6b, 0x10, 0x00, 0x1a,
		0x0b,
		0x20, 0x01, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("recurse", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func arm64GCMutableGlobalModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	funcType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	global := []byte{0x63, 0x00, 0x01, 0xd0, 0x00, 0x0b}
	body := []byte{0x01, 0x01, 0x7f,
		0x20, 0x00, 0xfb, 0x00, 0x00, 0x24, 0x00,
		0x02, 0x40, 0x03, 0x40,
		0x20, 0x01, 0x41, 0xe8, 0x07, 0x4f, 0x0d, 0x01,
		0xfb, 0x01, 0x00, 0x1a,
		0x20, 0x01, 0x41, 0x01, 0x6a, 0x21, 0x01, 0x0c, 0x00,
		0x0b, 0x0b,
		0x23, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec(global)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func arm64GCTableRootModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	funcType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	table := []byte{0x63, 0x00, 0x00, 0x01}
	body := []byte{0x01, 0x01, 0x7f,
		0x41, 0x00, 0x20, 0x00, 0xfb, 0x00, 0x00, 0x26, 0x00,
		0x02, 0x40, 0x03, 0x40,
		0x20, 0x01, 0x41, 0xe8, 0x07, 0x4f, 0x0d, 0x01,
		0xfb, 0x01, 0x00, 0x1a,
		0x20, 0x01, 0x41, 0x01, 0x6a, 0x21, 0x01, 0x0c, 0x00,
		0x0b, 0x0b,
		0x41, 0x00, 0x25, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec(table)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func arm64GCTypeSubtypingRefTestModule() []byte {
	funcType := []byte{0x60, 0x01, 0x6e, 0x01, 0x7f}   // (func (param anyref) (result i32))
	body := []byte{0x20, 0x00, 0xfb, 0x15, 0x6e, 0x0b} // ref.test_null any
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("test", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func arm64GCInlinedCallBeforeHostCollectionModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	hostType := wasmtest.FuncType(nil, nil)
	helperType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	runType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("gc")...), 0x00)
	imp = append(imp, wasmtest.ULEB(1)...)
	helper := []byte{0x20, 0x00, 0x0b}
	run := []byte{0x01, 0x01, 0x63, 0x00,
		0x20, 0x00, 0xfb, 0x00, 0x00, 0x21, 0x01,
		0x20, 0x00, 0x10, 0x01, 0x1a,
		0x10, 0x00,
		0x20, 0x01, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, hostType, helperType, runType)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2), wasmtest.ULEB(3))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(helper),
			append(wasmtest.ULEB(uint32(len(run))), run...),
		)),
	)
}

func arm64GCHostReentryModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	hostType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	outerType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	outer := []byte{0x01, 0x01, 0x63, 0x00,
		0x20, 0x00, 0xfb, 0x00, 0x00, 0x21, 0x01,
		0x10, 0x00, 0x1a,
		0x20, 0x01, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	inner := []byte{0x01, 0x01, 0x7f,
		0x02, 0x40, 0x03, 0x40,
		0x20, 0x00, 0x41, 0xe8, 0x07, 0x4f, 0x0d, 0x01,
		0xfb, 0x01, 0x00, 0x1a,
		0x20, 0x00, 0x41, 0x01, 0x6a, 0x21, 0x00, 0x0c, 0x00,
		0x0b, 0x0b, 0x41, 0x00, 0x0b}
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("reenter")...), 0x00)
	imp = append(imp, wasmtest.ULEB(1)...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, hostType, outerType)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("outer", 0, 1), wasmtest.ExportEntry("inner", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(outer))), outer...),
			append(wasmtest.ULEB(uint32(len(inner))), inner...),
		)),
	)
}

func arm64GCCallRefModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	funcType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	target := []byte{0x01, 0x01, 0x7f,
		0x02, 0x40, 0x03, 0x40,
		0x20, 0x01, 0x41, 0xe8, 0x07, 0x4f, 0x0d, 0x01,
		0xfb, 0x01, 0x00, 0x1a,
		0x20, 0x01, 0x41, 0x01, 0x6a, 0x21, 0x01, 0x0c, 0x00,
		0x0b, 0x0b, 0x20, 0x00, 0x0b}
	caller := []byte{0x01, 0x01, 0x63, 0x00,
		0x20, 0x00, 0xfb, 0x00, 0x00, 0x21, 0x01,
		0x20, 0x00, 0xd2, 0x00, 0x14, 0x01, 0x1a,
		0x20, 0x01, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	declared := []byte{0x03, 0x00, 0x01, 0x00}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec(declared)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(target))), target...),
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
		)),
	)
}

func arm64GCIndirectModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	funcType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	target := []byte{0x01, 0x01, 0x7f,
		0x02, 0x40, 0x03, 0x40,
		0x20, 0x01, 0x41, 0xe8, 0x07, 0x4f, 0x0d, 0x01,
		0xfb, 0x01, 0x00, 0x1a,
		0x20, 0x01, 0x41, 0x01, 0x6a, 0x21, 0x01, 0x0c, 0x00,
		0x0b, 0x0b, 0x41, 0x00, 0x0b}
	caller := []byte{0x01, 0x01, 0x63, 0x00,
		0x20, 0x00, 0xfb, 0x00, 0x00, 0x21, 0x01,
		0x20, 0x00, 0x41, 0x00, 0x11, 0x01, 0x00, 0x1a,
		0x20, 0x01, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	table := []byte{0x70, 0x00, 0x01}
	elem := []byte{0x00, 0x41, 0x00, 0x0b, 0x01, 0x00}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec(table)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec(elem)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(target))), target...),
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
		)),
	)
}

func arm64GCPolymorphicIndirectModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	targetType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	runType := wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32})
	target := func(delta byte) []byte {
		return []byte{0x01, 0x01, 0x7f,
			0x02, 0x40, 0x03, 0x40,
			0x20, 0x01, 0x41, 0xe8, 0x07, 0x4f, 0x0d, 0x01,
			0xfb, 0x01, 0x00, 0x1a,
			0x20, 0x01, 0x41, 0x01, 0x6a, 0x21, 0x01, 0x0c, 0x00,
			0x0b, 0x0b, 0x20, 0x00, 0x41, delta, 0x6a, 0x0b}
	}
	caller := []byte{0x01, 0x01, 0x63, 0x00,
		0x20, 0x00, 0xfb, 0x00, 0x00, 0x21, 0x02,
		0x20, 0x00, 0x20, 0x01, 0x11, 0x01, 0x00, 0x1a,
		0x20, 0x02, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	table := []byte{0x70, 0x00, 0x02}
	elem := []byte{0x00, 0x41, 0x00, 0x0b, 0x02, 0x00, 0x01}
	target0, target1 := target(0), target(1)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, targetType, runType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(4, wasmtest.Vec(table)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))),
		wasmtest.Section(9, wasmtest.Vec(elem)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(target0))), target0...),
			append(wasmtest.ULEB(uint32(len(target1))), target1...),
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
		)),
	)
}

func arm64GCForeignTailProviderModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	readType := []byte{0x60, 0x01, 0x63, 0x00, 0x01, 0x7f}
	body := []byte{0x01, 0x01, 0x7f,
		0x02, 0x40, 0x03, 0x40,
		0x20, 0x01, 0x41, 0xe8, 0x07, 0x4f, 0x0d, 0x01,
		0xfb, 0x01, 0x00, 0x1a,
		0x20, 0x01, 0x41, 0x01, 0x6a, 0x21, 0x01, 0x0c, 0x00,
		0x0b, 0x0b, 0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, readType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("read", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func arm64GCForeignTailConsumerModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	readType := []byte{0x60, 0x01, 0x63, 0x00, 0x01, 0x7f}
	runType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	imp := append(append(wasmtest.Name("provider"), wasmtest.Name("read")...), 0x00)
	imp = append(imp, wasmtest.ULEB(1)...)
	body := []byte{0x00, 0x20, 0x00, 0xfb, 0x00, 0x00, 0xd2, 0x00, 0x15, 0x01, 0x0b}
	declared := []byte{0x03, 0x00, 0x01, 0x00}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, readType, runType)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec(declared)),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func arm64GCCrossProviderModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	refCallType := []byte{0x60, 0x01, 0x63, 0x00, 0x01, 0x63, 0x00}
	runType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	body := []byte{0x01, 0x01, 0x7f,
		0x02, 0x40, 0x03, 0x40,
		0x20, 0x01, 0x41, 0xe8, 0x07, 0x4f, 0x0d, 0x01,
		0xfb, 0x01, 0x00, 0x1a,
		0x20, 0x01, 0x41, 0x01, 0x6a, 0x21, 0x01, 0x0c, 0x00,
		0x0b, 0x0b, 0x20, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, refCallType, runType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("retain", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func arm64GCCrossConsumerModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	refCallType := []byte{0x60, 0x01, 0x63, 0x00, 0x01, 0x63, 0x00}
	runType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	imp := append(append(wasmtest.Name("provider"), wasmtest.Name("retain")...), 0x00)
	imp = append(imp, wasmtest.ULEB(1)...)
	body := []byte{0x01, 0x01, 0x63, 0x00,
		0x20, 0x00, 0xfb, 0x00, 0x00, 0x21, 0x01,
		0x20, 0x01, 0x10, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, refCallType, runType)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func arm64GCCrossCallRefConsumerModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	refCallType := []byte{0x60, 0x01, 0x63, 0x00, 0x01, 0x63, 0x00}
	runType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	imp := append(append(wasmtest.Name("provider"), wasmtest.Name("retain")...), 0x00)
	imp = append(imp, wasmtest.ULEB(1)...)
	body := []byte{0x01, 0x01, 0x63, 0x00,
		0x20, 0x00, 0xfb, 0x00, 0x00, 0x21, 0x01,
		0x20, 0x01, 0xd2, 0x00, 0x14, 0x01, 0x1a,
		0x20, 0x01, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	declared := []byte{0x03, 0x00, 0x01, 0x00}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, refCallType, runType)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec(declared)),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestGCStructExecutionArm64(t *testing.T) {
	features := CoreFeaturesV2 | CoreFeatureTypedFunctionReferences | CoreFeatureGC
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(features), arm64GCStructModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	for _, candidate := range []*Compiled{compiled, publicArtifactRoundTrip(t, compiled)} {
		if candidate != compiled {
			defer candidate.Close()
		}
		in, err := Instantiate(candidate, InstantiateOptions{GC: GCConfig{ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096}})
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 1000; i++ {
			got, callErr := in.Invoke("roundtrip", uint64(i))
			if callErr != nil || !reflect.DeepEqual(got, []uint64{uint64(i)}) {
				in.Close()
				t.Fatalf("roundtrip %d = %v, %v", i, got, callErr)
			}
		}
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGCArm64ReferenceConstructorTemporaryRoots(t *testing.T) {
	features := CoreFeaturesV2 | CoreFeatureTypedFunctionReferences | CoreFeatureGC
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(features), arm64GCReferenceConstructorModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if roots := compiled.genericGCFrameRoots(); roots == nil || len(roots.safepoints) != 2 || len(roots.safepoints[1].offsets) != 0 {
		t.Fatalf("reference-constructor arm64 root plan = %+v", roots)
	}
	for _, candidate := range []*Compiled{compiled, publicArtifactRoundTrip(t, compiled)} {
		if candidate != compiled {
			defer candidate.Close()
		}
		in, err := Instantiate(candidate, InstantiateOptions{GC: GCConfig{Profile: GCProfileThroughput, StressNurseryBytes: 32, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096}})
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 1000; i++ {
			got, callErr := in.Invoke("construct")
			if callErr != nil || !reflect.DeepEqual(got, []uint64{0}) {
				in.Close()
				t.Fatalf("reference constructor %d = %v, %v", i, got, callErr)
			}
		}
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGCArm64ActiveFrameCollectionPreservesHiddenOperand(t *testing.T) {
	features := CoreFeaturesV2 | CoreFeatureTypedFunctionReferences | CoreFeatureGC
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(features), arm64GCHiddenOperandLoopModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if roots := compiled.genericGCFrameRoots(); roots == nil || len(roots.safepoints) != 2 || len(roots.safepoints[1].offsets) != 1 {
		t.Fatalf("hidden-operand arm64 root plan = %+v", roots)
	}
	for _, candidate := range []*Compiled{compiled, publicArtifactRoundTrip(t, compiled)} {
		if candidate != compiled {
			defer candidate.Close()
		}
		in, err := Instantiate(candidate, InstantiateOptions{GC: GCConfig{Profile: GCProfileThroughput, StressNurseryBytes: 64, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096}})
		if err != nil {
			t.Fatal(err)
		}
		got, callErr := in.Invoke("allocate", 1000)
		if callErr != nil || !reflect.DeepEqual(got, []uint64{0}) {
			in.Close()
			t.Fatalf("hidden-operand collection = %v, %v", got, callErr)
		}
		if err := in.gc.Verify(gc.EmptyRoots{}); err != nil {
			in.Close()
			t.Fatalf("collector verify: %v", err)
		}
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGCArm64CrossFunctionFrameWalking(t *testing.T) {
	features := CoreFeaturesV2 | CoreFeatureTypedFunctionReferences | CoreFeatureGC
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(features), arm64GCCrossFunctionModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if roots := compiled.genericGCFrameRoots(); roots == nil || len(roots.safepoints) != 2 || len(roots.callsites) != 1 || len(roots.callsites[0].offsets) != 1 {
		t.Fatalf("cross-function arm64 root plan = %+v", roots)
	}
	for _, candidate := range []*Compiled{compiled, publicArtifactRoundTrip(t, compiled)} {
		if candidate != compiled {
			defer candidate.Close()
		}
		in, err := Instantiate(candidate, InstantiateOptions{GC: GCConfig{Profile: GCProfileThroughput, StressNurseryBytes: 64, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096}})
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 10; i++ {
			got, callErr := in.Invoke("call", 1000)
			if callErr != nil || !reflect.DeepEqual(got, []uint64{0}) {
				in.Close()
				t.Fatalf("cross-function collection %d = %v, %v", i, got, callErr)
			}
		}
		if err := in.gc.Verify(gc.EmptyRoots{}); err != nil {
			in.Close()
			t.Fatalf("collector verify: %v", err)
		}
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGCArm64RecursiveFrameWalking(t *testing.T) {
	features := CoreFeaturesV2 | CoreFeatureTypedFunctionReferences | CoreFeatureGC
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(features), arm64GCRecursiveModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if roots := compiled.genericGCFrameRoots(); roots == nil || len(roots.safepoints) != 1 || len(roots.callsites) != 1 || len(roots.callsites[0].offsets) != 1 {
		t.Fatalf("recursive arm64 root plan = %+v", roots)
	}
	profiles := []GCConfig{
		{Profile: GCProfileThroughput, StressNurseryBytes: 64, CollectEveryAlloc: true, ForceMajorEveryMinor: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096},
		{Profile: GCProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true},
	}
	for _, candidate := range []*Compiled{compiled, publicArtifactRoundTrip(t, compiled)} {
		if candidate != compiled {
			defer candidate.Close()
		}
		for profileIndex, profile := range profiles {
			in, err := Instantiate(candidate, InstantiateOptions{GC: profile})
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 10; i++ {
				got, callErr := in.Invoke("recurse", 64)
				if callErr != nil || !reflect.DeepEqual(got, []uint64{0}) {
					in.Close()
					t.Fatalf("recursive collection %d = %v, %v", i, got, callErr)
				}
			}
			allocs := testing.AllocsPerRun(100, func() {
				if _, callErr := in.Invoke("recurse", 64); callErr != nil {
					panic(callErr)
				}
			})
			if allocs != 0 {
				in.Close()
				t.Fatalf("recursive arm64 root walking profile %d allocations = %v, want 0", profileIndex, allocs)
			}
			if err := in.gc.Verify(gc.EmptyRoots{}); err != nil {
				in.Close()
				t.Fatalf("collector verify: %v", err)
			}
			if err := in.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestGCArm64CallRefFrameRoots(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2|CoreFeatureTypedFunctionReferences|CoreFeatureGC), arm64GCCallRefModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	plan := compiled.genericGCFrameRoots()
	if plan == nil || len(plan.callsites) != 3 {
		t.Fatalf("call_ref arm64 root map = %+v", plan)
	}
	adjusted := 0
	for _, callsite := range plan.callsites {
		if len(callsite.offsets) != 1 {
			t.Fatalf("call_ref arm64 root map = %+v", plan)
		}
		if callsite.stackAdjust != 0 {
			adjusted++
		}
	}
	if adjusted != 1 {
		t.Fatalf("call_ref arm64 adjusted paths = %d, want 1: %+v", adjusted, plan)
	}
	for _, candidate := range []*Compiled{compiled, publicArtifactRoundTrip(t, compiled)} {
		if candidate != compiled {
			defer candidate.Close()
		}
		in, err := Instantiate(candidate, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 64, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}})
		if err != nil {
			t.Fatal(err)
		}
		got, callErr := in.Invoke("run", 55)
		if callErr != nil || !reflect.DeepEqual(got, []uint64{55}) {
			in.Close()
			t.Fatalf("run = %v, %v; want [55]", got, callErr)
		}
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGCArm64IndirectFrameRoots(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2|CoreFeatureTypedFunctionReferences|CoreFeatureGC), arm64GCIndirectModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	plan := compiled.genericGCFrameRoots()
	if plan == nil || len(plan.callsites) != 1 || len(plan.callsites[0].offsets) != 1 {
		t.Fatalf("indirect arm64 root map = %+v", plan)
	}
	for _, candidate := range []*Compiled{compiled, publicArtifactRoundTrip(t, compiled)} {
		if candidate != compiled {
			defer candidate.Close()
		}
		in, err := Instantiate(candidate, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 64, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}})
		if err != nil {
			t.Fatal(err)
		}
		got, callErr := in.Invoke("run", 77)
		if callErr != nil || !reflect.DeepEqual(got, []uint64{77}) {
			in.Close()
			t.Fatalf("run = %v, %v; want [77]", got, callErr)
		}
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGCArm64PolymorphicIndirectFrameRoots(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2|CoreFeatureTypedFunctionReferences|CoreFeatureGC), arm64GCPolymorphicIndirectModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	plan := compiled.genericGCFrameRoots()
	if plan == nil || len(plan.callsites) != 3 {
		t.Fatalf("polymorphic indirect arm64 root map = %+v", plan)
	}
	adjusted := 0
	for _, site := range plan.callsites {
		if len(site.offsets) != 1 {
			t.Fatalf("polymorphic indirect roots = %+v", plan.callsites)
		}
		if site.stackAdjust == 64 {
			adjusted++
		} else if site.stackAdjust != 0 {
			t.Fatalf("polymorphic indirect stack adjustment = %d", site.stackAdjust)
		}
	}
	if adjusted != 1 {
		t.Fatalf("polymorphic indirect adjusted sites = %d, want 1", adjusted)
	}
	profiles := []GCConfig{
		{Profile: GCProfileThroughput, StressNurseryBytes: 64, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096},
		{Profile: GCProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true},
	}
	for _, candidate := range []*Compiled{compiled, publicArtifactRoundTrip(t, compiled)} {
		if candidate != compiled {
			defer candidate.Close()
		}
		for profileIndex, profile := range profiles {
			in, err := Instantiate(candidate, InstantiateOptions{GC: profile})
			if err != nil {
				t.Fatal(err)
			}
			for tableIndex := uint64(0); tableIndex < 2; tableIndex++ {
				want := uint64(800 + profileIndex*10 + int(tableIndex))
				got, callErr := in.Invoke("run", want, tableIndex)
				if callErr != nil || !reflect.DeepEqual(got, []uint64{want}) {
					in.Close()
					t.Fatalf("profile %d table %d run = %v, %v", profileIndex, tableIndex, got, callErr)
				}
			}
			if err := in.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestGCArm64MutableGlobalAndTableRoots(t *testing.T) {
	features := CoreFeaturesV2 | CoreFeatureTypedFunctionReferences | CoreFeatureGC
	cases := []struct {
		name string
		code []byte
	}{
		{name: "global", code: arm64GCMutableGlobalModule()},
		{name: "table", code: arm64GCTableRootModule()},
	}
	profiles := []GCConfig{
		{Profile: GCProfileThroughput, StressNurseryBytes: 64, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096},
		{Profile: GCProfileTiny, TinyHeapBytes: 64, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(features), tc.code)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			if compiled.genericGCFrameRoots() == nil {
				t.Fatal("persistent-root module lost exact arm64 admission")
			}
			for _, candidate := range []*Compiled{compiled, publicArtifactRoundTrip(t, compiled)} {
				if candidate != compiled {
					defer candidate.Close()
				}
				for _, profile := range profiles {
					in, err := Instantiate(candidate, InstantiateOptions{GC: profile})
					if err != nil {
						t.Fatal(err)
					}
					got, callErr := in.Invoke("run", 88)
					if callErr != nil || !reflect.DeepEqual(got, []uint64{88}) {
						in.Close()
						t.Fatalf("run = %v, %v; want [88]", got, callErr)
					}
					if err := in.Close(); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}

func arm64GCExactDefinedCastModule(exact bool) []byte {
	target := []byte{0x00}
	if exact {
		target = []byte{0x62, 0x00}
	}
	body := []byte{
		0x01, 0x01, 0x63, 0x6e,
		0xfb, 0x01, 0x01, 0x21, 0x00,
		0x20, 0x00, 0xfb, 0x16,
	}
	body = append(body, target...)
	body = append(body, 0x1a, 0x41, 0x01, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x50, 0x00, 0x5f, 0x00},
			[]byte{0x4f, 0x01, 0x00, 0x5f, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestGCArm64ExactDefinedCastRejectsProperSubtype(t *testing.T) {
	for _, exact := range []bool{false, true} {
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), arm64GCExactDefinedCastModule(exact))
		if err != nil {
			t.Fatal(err)
		}
		in, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			compiled.Close()
			t.Fatal(err)
		}
		got, callErr := in.Invoke("run")
		in.Close()
		compiled.Close()
		if !exact {
			if callErr != nil || !reflect.DeepEqual(got, []uint64{1}) {
				t.Fatalf("ordinary cast = %v, %v; want [1], nil", got, callErr)
			}
			continue
		}
		trap, ok := callErr.(*TrapError)
		if !ok || trap.Code != TrapCastFailure {
			t.Fatalf("exact cast = %v, %v; want cast-failure trap", got, callErr)
		}
	}
}

func TestGCArm64TypeSubtypingRefTestProvisionsHelpers(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), arm64GCTypeSubtypingRefTestModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if !compiled.usesGCStructHelpers() {
		t.Fatal("arm64 ref.test did not provision checked GC helpers")
	}
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	got, callErr := in.Invoke("test", 0)
	if callErr != nil || !reflect.DeepEqual(got, []uint64{1}) {
		t.Fatalf("ref.test null = %v, %v; want [1], nil", got, callErr)
	}
}

func TestGCArm64InlinedCallKeepsLaterHostRootMapExact(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), arm64GCInlinedCallBeforeHostCollectionModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if roots := compiled.genericGCFrameRoots(); roots == nil || len(roots.callsites) == 0 {
		t.Fatalf("inlined-call host root map = %+v", roots)
	}
	in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{"env.gc": HostFunc(func(module HostModule, _, _ []uint64) {
		collector, ok := module.(GCHostModule)
		if !ok {
			panic("host module has no collector")
		}
		if err := collector.CollectGC(); err != nil {
			panic(err)
		}
	})}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	got, callErr := in.Invoke("run", 77)
	if callErr != nil || !reflect.DeepEqual(got, []uint64{77}) {
		t.Fatalf("run = %v, %v; want [77], nil", got, callErr)
	}
}

func TestGCArm64HostReentryRoots(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2|CoreFeatureTypedFunctionReferences|CoreFeatureGC), arm64GCHostReentryModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	plan := compiled.genericGCFrameRoots()
	if plan == nil || len(plan.callsites) != 1 || len(plan.callsites[0].offsets) != 1 {
		t.Fatalf("host re-entry arm64 root map = %+v", plan)
	}
	for _, candidate := range []*Compiled{compiled, publicArtifactRoundTrip(t, compiled)} {
		if candidate != compiled {
			defer candidate.Close()
		}
		for _, profile := range []GCConfig{
			{Profile: GCProfileThroughput, StressNurseryBytes: 64, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096},
			{Profile: GCProfileTiny, TinyHeapBytes: 64, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true},
		} {
			var in *Instance
			calls := 0
			in, err = Instantiate(candidate, InstantiateOptions{GC: profile, Imports: Imports{"env.reenter": HostFunc(func(caller HostModule, _, results []uint64) {
				calls++
				got, callErr := in.InvokeFromHost(context.Background(), caller, "inner")
				if callErr != nil || !reflect.DeepEqual(got, []uint64{0}) {
					panic(fmt.Sprintf("inner = %v, %v", got, callErr))
				}
				results[0] = 0
			})}})
			if err != nil {
				t.Fatal(err)
			}
			got, callErr := in.Invoke("outer", 91)
			if callErr != nil || !reflect.DeepEqual(got, []uint64{91}) || calls != 1 {
				in.Close()
				t.Fatalf("outer = %v, %v, calls %d", got, callErr, calls)
			}
			if err := in.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestGCArm64CrossInstanceRoots(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureTypedFunctionReferences | CoreFeatureGC)
	providerCode, err := Compile(cfg, arm64GCCrossProviderModule())
	if err != nil {
		t.Fatal(err)
	}
	defer providerCode.Close()
	consumerCode, err := Compile(cfg, arm64GCCrossConsumerModule())
	if err != nil {
		t.Fatal(err)
	}
	defer consumerCode.Close()
	if providerCode.genericGCFrameRoots() == nil || consumerCode.genericGCFrameRoots() == nil {
		t.Fatal("cross-instance modules lost exact arm64 admission")
	}
	store := newReferenceStore(false)
	defer store.closeRuntime()
	profile := GCConfig{Profile: GCProfileThroughput, StressNurseryBytes: 64, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096}
	provider, err := instantiateCore(providerCode, InstantiateOptions{GC: profile, store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	export, err := provider.ExportedFunc("retain")
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := instantiateCore(consumerCode, InstantiateOptions{GC: profile, store: store, Imports: Imports{"provider.retain": export}})
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	if provider.gc != consumer.gc {
		t.Fatal("cross-instance arm64 modules do not share a collector")
	}
	for i := 0; i < 10; i++ {
		want := uint64(700 + i)
		got, callErr := consumer.Invoke("run", want)
		if callErr != nil || !reflect.DeepEqual(got, []uint64{want}) {
			t.Fatalf("run %d = %v, %v", i, got, callErr)
		}
	}
}

func TestGCArm64ForeignCallRefRoots(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureTypedFunctionReferences | CoreFeatureGC)
	providerCode, err := Compile(cfg, arm64GCCrossProviderModule())
	if err != nil {
		t.Fatal(err)
	}
	defer providerCode.Close()
	consumerCode, err := Compile(cfg, arm64GCCrossCallRefConsumerModule())
	if err != nil {
		t.Fatal(err)
	}
	defer consumerCode.Close()
	plan := consumerCode.genericGCFrameRoots()
	if plan == nil || len(plan.callsites) != 2 {
		t.Fatalf("foreign call_ref root plan = %+v", plan)
	}
	adjusted := 0
	for _, site := range plan.callsites {
		if len(site.offsets) != 1 {
			t.Fatalf("foreign call_ref roots = %+v", plan.callsites)
		}
		if site.stackAdjust == 64 {
			adjusted++
		} else if site.stackAdjust != 0 {
			t.Fatalf("foreign call_ref stack adjustment = %d", site.stackAdjust)
		}
	}
	if adjusted != 1 {
		t.Fatalf("foreign call_ref adjusted sites = %d, want 1", adjusted)
	}
	profiles := []GCConfig{
		{Profile: GCProfileThroughput, StressNurseryBytes: 64, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096},
		{Profile: GCProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true},
	}
	for profileIndex, profile := range profiles {
		store := newReferenceStore(false)
		provider, err := instantiateCore(providerCode, InstantiateOptions{GC: profile, store: store})
		if err != nil {
			store.closeRuntime()
			t.Fatal(err)
		}
		export, err := provider.ExportedFunc("retain")
		if err != nil {
			provider.Close()
			store.closeRuntime()
			t.Fatal(err)
		}
		consumer, err := instantiateCore(consumerCode, InstantiateOptions{GC: profile, store: store, Imports: Imports{"provider.retain": export}})
		if err != nil {
			provider.Close()
			store.closeRuntime()
			t.Fatal(err)
		}
		if provider.gc != consumer.gc {
			consumer.Close()
			provider.Close()
			store.closeRuntime()
			t.Fatal("foreign call_ref modules do not share a collector")
		}
		for i := 0; i < 10; i++ {
			want := uint64(900 + i)
			got, callErr := consumer.Invoke("run", want)
			if callErr != nil || !reflect.DeepEqual(got, []uint64{want}) {
				consumer.Close()
				provider.Close()
				store.closeRuntime()
				t.Fatalf("profile %d run %d = %v, %v", profileIndex, i, got, callErr)
			}
		}
		if err := consumer.Close(); err != nil {
			t.Fatal(err)
		}
		if err := provider.Close(); err != nil {
			t.Fatal(err)
		}
		store.closeRuntime()
	}
}

func TestGCArm64ForeignReturnCallRef(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureTailCall | CoreFeatureTypedFunctionReferences | CoreFeatureGC)
	providerCode, err := Compile(cfg, arm64GCForeignTailProviderModule())
	if err != nil {
		t.Fatal(err)
	}
	defer providerCode.Close()
	consumerCode, err := Compile(cfg, arm64GCForeignTailConsumerModule())
	if err != nil {
		t.Fatal(err)
	}
	defer consumerCode.Close()
	if providerCode.genericGCFrameRoots() == nil || consumerCode.genericGCFrameRoots() == nil {
		t.Fatal("foreign return_call_ref modules lost exact roots")
	}
	if got := len(consumerCode.genericGCFrameRoots().callsites); got != 0 {
		t.Fatalf("foreign return_call_ref retained %d caller callsites", got)
	}
	profiles := []GCConfig{
		{Profile: GCProfileThroughput, StressNurseryBytes: 64, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096},
		{Profile: GCProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true},
	}
	for profileIndex, profile := range profiles {
		store := newReferenceStore(false)
		provider, err := instantiateCore(providerCode, InstantiateOptions{GC: profile, store: store})
		if err != nil {
			store.closeRuntime()
			t.Fatal(err)
		}
		export, err := provider.ExportedFunc("read")
		if err != nil {
			provider.Close()
			store.closeRuntime()
			t.Fatal(err)
		}
		consumer, err := instantiateCore(consumerCode, InstantiateOptions{GC: profile, store: store, Imports: Imports{"provider.read": export}})
		if err != nil {
			provider.Close()
			store.closeRuntime()
			t.Fatal(err)
		}
		if provider.gc != consumer.gc {
			consumer.Close()
			provider.Close()
			store.closeRuntime()
			t.Fatal("foreign return_call_ref modules do not share a collector")
		}
		for i := 0; i < 10; i++ {
			want := uint64(1000 + profileIndex*10 + i)
			got, callErr := consumer.Invoke("run", want)
			if callErr != nil || !reflect.DeepEqual(got, []uint64{want}) {
				consumer.Close()
				provider.Close()
				store.closeRuntime()
				t.Fatalf("profile %d run %d = %v, %v", profileIndex, i, got, callErr)
			}
		}
		if err := consumer.Close(); err != nil {
			t.Fatal(err)
		}
		if err := provider.Close(); err != nil {
			t.Fatal(err)
		}
		store.closeRuntime()
	}
}

func TestGCArm64RejectsMalformedNativeRootMetadata(t *testing.T) {
	features := CoreFeaturesV2 | CoreFeatureTypedFunctionReferences | CoreFeatureGC
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(features), arm64GCRecursiveModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	roots := compiled.genericGCFrameRoots()
	if roots == nil || len(roots.callsites) == 0 {
		t.Fatalf("recursive arm64 root plan = %+v", roots)
	}
	roots.callsites[0].returnOffset = 0
	if _, err := compiled.MarshalBinary(); err == nil {
		t.Fatal("MarshalBinary accepted malformed arm64 callsite metadata")
	}
}

func TestGCArm64ActiveFrameCollectionPreservesLocalRoot(t *testing.T) {
	features := CoreFeaturesV2 | CoreFeatureTypedFunctionReferences | CoreFeatureGC
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(features), arm64GCAllocationLoopModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if roots := compiled.genericGCFrameRoots(); roots == nil || len(roots.safepoints) != 2 || len(roots.safepoints[1].offsets) != 1 {
		t.Fatalf("arm64 native root plan = %+v", roots)
	}
	for _, candidate := range []*Compiled{compiled, publicArtifactRoundTrip(t, compiled)} {
		if candidate != compiled {
			defer candidate.Close()
		}
		in, err := Instantiate(candidate, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 64, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}})
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 10; i++ {
			got, callErr := in.Invoke("allocate", 1000)
			if callErr != nil || !reflect.DeepEqual(got, []uint64{0}) {
				in.Close()
				t.Fatalf("active-frame collection %d = %v, %v", i, got, callErr)
			}
		}
		if err := in.gc.Verify(gc.EmptyRoots{}); err != nil {
			in.Close()
			t.Fatalf("collector verify: %v", err)
		}
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
