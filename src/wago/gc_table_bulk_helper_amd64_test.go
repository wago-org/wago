//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"bytes"
	"encoding/binary"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func genericGCTableHelperModule() []byte {
	body := []byte{
		0x00,
		0x41, 0x2a, 0xfb, 0x00, 0x00, 0x1a,
		0x41, 0x00,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(4, []byte{0x02, 0x63, 0x00, 0x01, 0x02, 0x04, 0x63, 0x00, 0x01, 0x02, 0x04}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(9, []byte{0x01, 0x05, 0x63, 0x00, 0x02,
			0x41, 0x37, 0xfb, 0x00, 0x00, 0x0b,
			0x41, 0x16, 0xfb, 0x00, 0x00, 0x0b}),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func genericGCActiveTableElementModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 0x01, 0x7f, 0x01})),
		wasmtest.Section(4, []byte{0x01, 0x63, 0x00, 0x01, 0x02, 0x04}),
		wasmtest.Section(9, []byte{0x01, 0x06, 0x00, 0x41, 0x00, 0x0b, 0x63, 0x00, 0x02,
			0x41, 0x37, 0xfb, 0x00, 0x00, 0x0b,
			0xfb, 0x01, 0x00, 0x0b}),
	)
}

func genericGCActiveArrayElementModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7f, 0x01})),
		wasmtest.Section(4, []byte{0x01, 0x63, 0x00, 0x01, 0x03, 0x04}),
		wasmtest.Section(9, []byte{0x01, 0x06, 0x00, 0x41, 0x00, 0x0b, 0x63, 0x00, 0x03,
			0x41, 0x0b, 0x41, 0x16, 0xfb, 0x08, 0x00, 0x02, 0x0b,
			0x41, 0x07, 0x41, 0x03, 0xfb, 0x06, 0x00, 0x0b,
			0x41, 0x02, 0xfb, 0x07, 0x00, 0x0b}),
	)
}

func genericGCActiveNullableStructElementModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01},       // mutable i32 leaf
			[]byte{0x5f, 0x01, 0x63, 0x00, 0x01}, // mutable (ref null 0) holder
		)),
		wasmtest.Section(4, []byte{0x01, 0x63, 0x01, 0x01, 0x02, 0x04}),
		wasmtest.Section(9, []byte{0x01, 0x06, 0x00, 0x41, 0x00, 0x0b, 0x63, 0x01, 0x02,
			0xd0, 0x00, 0xfb, 0x00, 0x01, 0x0b,
			0xfb, 0x01, 0x01, 0x0b}),
	)
}

func genericGCActiveNullableArrayElementModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01}, // mutable i32 leaf
			[]byte{0x5e, 0x63, 0x00, 0x01}, // mutable (ref null 0) elements
		)),
		wasmtest.Section(4, []byte{0x01, 0x63, 0x01, 0x01, 0x03, 0x04}),
		wasmtest.Section(9, []byte{0x01, 0x06, 0x00, 0x41, 0x00, 0x0b, 0x63, 0x01, 0x03,
			0xd0, 0x00, 0xd0, 0x00, 0xfb, 0x08, 0x01, 0x02, 0x0b,
			0xd0, 0x00, 0x41, 0x02, 0xfb, 0x06, 0x01, 0x0b,
			0x41, 0x02, 0xfb, 0x07, 0x01, 0x0b}),
	)
}

func genericGCActiveNestedStructElementModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01},       // mutable i32 leaf
			[]byte{0x5f, 0x01, 0x64, 0x00, 0x01}, // mutable non-null (ref 0) holder
		)),
		wasmtest.Section(4, []byte{0x01, 0x63, 0x01, 0x01, 0x02, 0x04}),
		wasmtest.Section(9, []byte{0x01, 0x06, 0x00, 0x41, 0x00, 0x0b, 0x63, 0x01, 0x02,
			0xfb, 0x01, 0x00, 0xfb, 0x00, 0x01, 0x0b,
			0x41, 0x2a, 0xfb, 0x00, 0x00, 0xfb, 0x00, 0x01, 0x0b}),
	)
}

func genericGCActiveNestedArrayElementModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5e, 0x7f, 0x01},       // mutable i32 inner array
			[]byte{0x5e, 0x64, 0x00, 0x01}, // mutable non-null (ref 0) outer array
		)),
		wasmtest.Section(4, []byte{0x01, 0x63, 0x01, 0x01, 0x03, 0x04}),
		wasmtest.Section(9, []byte{0x01, 0x06, 0x00, 0x41, 0x00, 0x0b, 0x63, 0x01, 0x03,
			0x41, 0x02, 0xfb, 0x07, 0x00, 0x41, 0x03, 0xfb, 0x07, 0x00, 0xfb, 0x08, 0x01, 0x02, 0x0b,
			0x41, 0x01, 0xfb, 0x07, 0x00, 0x41, 0x02, 0xfb, 0x06, 0x01, 0x0b,
			0x41, 0x05, 0x41, 0x06, 0xfb, 0x08, 0x00, 0x02,
			0x41, 0x07, 0x41, 0x02, 0xfb, 0x06, 0x00,
			0xfb, 0x08, 0x01, 0x02, 0x0b}),
	)
}

func genericGCDeclarativeAggregateElementModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 0x01, 0x7f, 0x01})),
		wasmtest.Section(4, []byte{0x01, 0x63, 0x00, 0x01, 0x01, 0x01}),
		wasmtest.Section(9, []byte{0x01, 0x07, 0x63, 0x00, 0x01,
			0x41, 0x37, 0xfb, 0x00, 0x00, 0x0b}),
	)
}

func genericGCActiveFunctionReferenceElementModule() []byte {
	body := []byte{0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x70, 0x01}, // mutable funcref field
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(4, []byte{0x01, 0x63, 0x00, 0x01, 0x01, 0x01}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(9, []byte{0x01, 0x06, 0x00, 0x41, 0x00, 0x0b, 0x63, 0x00, 0x01,
			0xd2, 0x00, 0xfb, 0x00, 0x00, 0x0b}),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func genericGCAggregateTableInitializerModule() []byte {
	body := []byte{
		0x00,
		0x41, 0x01, 0xfb, 0x00, 0x00, 0x1a,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01},
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(4, []byte{
			0x01, 0x40, 0x00, 0x63, 0x00, 0x01, 0x03, 0x03,
			0x41, 0x37, 0xfb, 0x00, 0x00, 0x0b,
		}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func genericGCAggregateSubtypeTableInitializerModule() []byte {
	body := []byte{0x00, 0x41, 0x01, 0xfb, 0x00, 0x01, 0x1a, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x50, 0x00, 0x5f, 0x01, 0x7f, 0x01},       // open struct supertype
			[]byte{0x4f, 0x01, 0x00, 0x5f, 0x01, 0x7f, 0x01}, // final struct subtype
			[]byte{0x5f, 0x01, 0x7e, 0x01},                   // unrelated aggregate
			[]byte{0x5f, 0x01, 0x63, 0x00, 0x01},             // holder of (ref null 0)
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(4))),
		wasmtest.Section(4, []byte{
			0x01, 0x40, 0x00, 0x63, 0x03, 0x01, 0x02, 0x02,
			0x41, 0x37, 0xfb, 0x00, 0x01, 0xfb, 0x00, 0x03, 0x0b,
		}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func genericGCAggregateGlobalInitializerModule() []byte {
	body := []byte{0x00, 0x41, 0x01, 0xfb, 0x00, 0x00, 0x1a, 0x41, 0x07, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec([]byte{
			0x64, 0x00, 0x00,
			0x41, 0x21, 0xfb, 0x00, 0x00, 0x0b,
		})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func genericGCAggregateGlobalTableInitializerModule() []byte {
	body := []byte{
		0x00,
		0x41, 0x01, 0xfb, 0x00, 0x00, 0x1a,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01},
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(4, []byte{
			0x01, 0x40, 0x00, 0x63, 0x00, 0x01, 0x02, 0x02,
			0x23, 0x00, 0xfb, 0x00, 0x00, 0x0b,
		}),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x7f, 0x00, 0x41, 0x37, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func genericGCAggregateImportedGlobalTableInitializerModule() []byte {
	body := []byte{
		0x00,
		0x41, 0x01, 0xfb, 0x00, 0x00, 0x1a,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01},
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "base", wasm.I32, false))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(4, []byte{
			0x01, 0x40, 0x00, 0x63, 0x00, 0x01, 0x02, 0x02,
			0x23, 0x00, 0xfb, 0x00, 0x00, 0x0b,
		}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func genericGCAggregateReferenceGlobalTableInitializerModule() []byte {
	body := []byte{
		0x00,
		0x41, 0x01, 0xfb, 0x00, 0x00, 0x1a,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01},
			[]byte{0x5f, 0x01, 0x63, 0x00, 0x01},
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(4, []byte{
			0x01, 0x40, 0x00, 0x63, 0x01, 0x01, 0x02, 0x02,
			0x23, 0x00, 0xfb, 0x00, 0x01, 0x0b,
		}),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0x00, 0x00, 0xd0, 0x00, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestGenericGCAggregateTableInitializerRootsOneSharedObject(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit), genericGCAggregateTableInitializerModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if len(compiled.Elems) != 1 || len(compiled.Elems[0].Values) != 3 || len(compiled.Elems[0].Values[0].Expr) == 0 ||
		!compiled.Elems[0].Values[1].RepeatPrevious || !compiled.Elems[0].Values[2].RepeatPrevious {
		t.Fatalf("aggregate table initializer metadata = %#v", compiled.Elems)
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err := instance.Invoke("run"); err != nil {
		t.Fatal(err)
	}
	if err := instance.CollectGC(); err != nil {
		t.Fatal(err)
	}
	descriptor := instance.tableDescriptor(0)
	want := gc.Ref(uint32(binary.LittleEndian.Uint64(descriptor[8:])))
	if want.IsNull() {
		t.Fatal("aggregate table initializer produced null")
	}
	for slot := 1; slot < 3; slot++ {
		got := gc.Ref(uint32(binary.LittleEndian.Uint64(descriptor[8+slot*8:])))
		if got != want {
			t.Fatalf("aggregate table initializer slot %d = %v, want shared %v", slot, got, want)
		}
	}
	value, err := instance.gc.StructGet(want, 0)
	if err != nil || value.Bits != 55 {
		t.Fatalf("aggregate table initializer field = %+v, %v", value, err)
	}
}

func TestGenericGCAggregateSubtypeTableInitializer(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit), genericGCAggregateSubtypeTableInitializerModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err := instance.Invoke("run"); err != nil {
		t.Fatal(err)
	}
	if err := instance.CollectGC(); err != nil {
		t.Fatal(err)
	}
	descriptor := instance.tableDescriptor(0)
	holder := gc.Ref(uint32(binary.LittleEndian.Uint64(descriptor[8:])))
	repeated := gc.Ref(uint32(binary.LittleEndian.Uint64(descriptor[16:])))
	if holder.IsNull() || repeated != holder {
		t.Fatalf("aggregate subtype table initializer refs = %v/%v", holder, repeated)
	}
	childValue, err := instance.gc.StructGet(holder, 0)
	if err != nil {
		t.Fatal(err)
	}
	child := childValue.Ref
	value, err := instance.gc.StructGet(child, 0)
	if err != nil || value.Bits != 55 {
		t.Fatalf("aggregate subtype child = %v/%+v, %v", child, value, err)
	}
}

func TestGenericGCAggregateGlobalInitializer(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit), genericGCAggregateGlobalInitializerModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if len(compiled.Globals) != 1 || len(compiled.Globals[0].InitExpr) == 0 {
		t.Fatalf("aggregate global initializer metadata = %#v", compiled.Globals)
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err = instance.Invoke("run"); err != nil {
		t.Fatal(err)
	}
	if err = instance.CollectGC(); err != nil {
		t.Fatal(err)
	}
	ref := gc.Ref(uint32(readGlobalObject(instance.globalCells[0], instance.c.Globals[0].Type)))
	value, err := instance.gc.StructGet(ref, 0)
	if err != nil || value.Bits != 33 {
		t.Fatalf("aggregate global initializer = %v/%+v, %v", ref, value, err)
	}
}

func TestGenericGCAggregateGlobalTableInitializer(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit), genericGCAggregateGlobalTableInitializerModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if len(compiled.Globals) != 1 || len(compiled.Elems) != 1 || len(compiled.Elems[0].Values) != 2 || len(compiled.Elems[0].Values[0].Expr) == 0 ||
		!compiled.Elems[0].Values[1].RepeatPrevious {
		t.Fatalf("aggregate global table initializer metadata globals=%#v elems=%#v", compiled.Globals, compiled.Elems)
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err := instance.Invoke("run"); err != nil {
		t.Fatal(err)
	}
	if err := instance.CollectGC(); err != nil {
		t.Fatal(err)
	}
	descriptor := instance.tableDescriptor(0)
	want := gc.Ref(uint32(binary.LittleEndian.Uint64(descriptor[8:])))
	got := gc.Ref(uint32(binary.LittleEndian.Uint64(descriptor[16:])))
	if want.IsNull() || got != want {
		t.Fatalf("aggregate global table initializer refs = %v/%v", want, got)
	}
	value, err := instance.gc.StructGet(want, 0)
	if err != nil || value.Bits != 55 {
		t.Fatalf("aggregate global table initializer field = %+v, %v", value, err)
	}
}

func TestGenericGCAggregateImportedGlobalTableInitializer(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit), genericGCAggregateImportedGlobalTableInitializerModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{
		Imports: NewImports().Global("env", "base", GlobalImport{Type: ValI32, Bits: 55}),
		GC:      GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err := instance.Invoke("run"); err != nil {
		t.Fatal(err)
	}
	if err := instance.CollectGC(); err != nil {
		t.Fatal(err)
	}
	descriptor := instance.tableDescriptor(0)
	want := gc.Ref(uint32(binary.LittleEndian.Uint64(descriptor[8:])))
	got := gc.Ref(uint32(binary.LittleEndian.Uint64(descriptor[16:])))
	if want.IsNull() || got != want {
		t.Fatalf("aggregate imported-global table initializer refs = %v/%v", want, got)
	}
	value, err := instance.gc.StructGet(want, 0)
	if err != nil || value.Bits != 55 {
		t.Fatalf("aggregate imported-global table initializer field = %+v, %v", value, err)
	}
}

func TestGenericGCAggregateReferenceGlobalTableInitializer(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit), genericGCAggregateReferenceGlobalTableInitializerModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if len(compiled.Globals) != 1 || len(compiled.Elems) != 1 || len(compiled.Elems[0].Values) != 2 || len(compiled.Elems[0].Values[0].Expr) == 0 ||
		!compiled.Elems[0].Values[1].RepeatPrevious {
		t.Fatalf("aggregate reference-global table initializer metadata globals=%#v elems=%#v", compiled.Globals, compiled.Elems)
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err := instance.Invoke("run"); err != nil {
		t.Fatal(err)
	}
	if err := instance.CollectGC(); err != nil {
		t.Fatal(err)
	}
	descriptor := instance.tableDescriptor(0)
	want := gc.Ref(uint32(binary.LittleEndian.Uint64(descriptor[8:])))
	got := gc.Ref(uint32(binary.LittleEndian.Uint64(descriptor[16:])))
	if want.IsNull() || got != want {
		t.Fatalf("aggregate reference-global table initializer refs = %v/%v", want, got)
	}
	value, err := instance.gc.StructGet(want, 0)
	if err != nil || value.Bits != 0 {
		t.Fatalf("aggregate reference-global table initializer field = %+v, %v", value, err)
	}
}

func TestGenericGCActiveTableElementRoots(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit), genericGCActiveTableElementModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if len(compiled.Elems) != 1 || len(compiled.Elems[0].Values) != 2 || len(compiled.Elems[0].Values[0].Expr) == 0 || len(compiled.Elems[0].Values[1].Expr) == 0 {
		t.Fatalf("active aggregate element metadata = %#v", compiled.Elems)
	}
	for iteration := range 8 {
		instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
		if err != nil {
			t.Fatalf("iteration %d: %v", iteration, err)
		}
		descriptor := instance.tableDescriptor(0)
		ref := gc.Ref(uint32(binary.LittleEndian.Uint64(descriptor[8:])))
		if ref.IsNull() {
			instance.Close()
			t.Fatalf("iteration %d: active aggregate element is null", iteration)
		}
		if err := instance.CollectGC(); err != nil {
			instance.Close()
			t.Fatalf("iteration %d: collect active root: %v", iteration, err)
		}
		value, err := instance.gc.StructGet(ref, 0)
		defaultRef := gc.Ref(uint32(binary.LittleEndian.Uint64(descriptor[16:])))
		defaultValue, defaultErr := instance.gc.StructGet(defaultRef, 0)
		instance.Close()
		if err != nil || value.Bits != 55 {
			t.Fatalf("iteration %d: active aggregate root = %+v, %v", iteration, value, err)
		}
		if defaultErr != nil || defaultValue.Bits != 0 {
			t.Fatalf("iteration %d: default active aggregate root = %+v, %v", iteration, defaultValue, defaultErr)
		}
	}
}

func TestGenericGCActiveArrayElementRoots(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit), genericGCActiveArrayElementModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if err := instance.CollectGC(); err != nil {
		t.Fatal(err)
	}
	descriptor := instance.tableDescriptor(0)
	for slot, values := range [][]uint64{{11, 22}, {7, 7, 7}, {0, 0}} {
		ref := gc.Ref(uint32(binary.LittleEndian.Uint64(descriptor[8+slot*8:])))
		for index, want := range values {
			value, err := instance.gc.ArrayGet(ref, uint32(index))
			if err != nil || value.Bits != want {
				t.Fatalf("active array root[%d][%d] = %+v, %v; want %d", slot, index, value, err, want)
			}
		}
	}
}

func TestGenericGCActiveNullableReferenceElements(t *testing.T) {
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit)
	for name, source := range map[string][]byte{
		"struct": genericGCActiveNullableStructElementModule(),
		"array":  genericGCActiveNullableArrayElementModule(),
	} {
		t.Run(name, func(t *testing.T) {
			compiled, err := Compile(config, source)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			if err := instance.CollectGC(); err != nil {
				t.Fatal(err)
			}
			descriptor := instance.tableDescriptor(0)
			for slot := range len(compiled.Elems[0].Values) {
				ref := gc.Ref(uint32(binary.LittleEndian.Uint64(descriptor[8+slot*8:])))
				if ref.IsNull() {
					t.Fatalf("aggregate table slot %d is null", slot)
				}
				if name == "struct" {
					value, err := instance.gc.StructGet(ref, 0)
					if err != nil || value.Bits != 0 {
						t.Fatalf("nullable struct field[%d] = %+v, %v", slot, value, err)
					}
					continue
				}
				for index := range 2 {
					value, err := instance.gc.ArrayGet(ref, uint32(index))
					if err != nil || value.Bits != 0 {
						t.Fatalf("nullable array element[%d][%d] = %+v, %v", slot, index, value, err)
					}
				}
			}
		})
	}
}

func TestGenericGCActiveNestedReferenceElements(t *testing.T) {
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit)
	t.Run("struct", func(t *testing.T) {
		compiled, err := Compile(config, genericGCActiveNestedStructElementModule())
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		if err := instance.CollectGC(); err != nil {
			t.Fatal(err)
		}
		descriptor := instance.tableDescriptor(0)
		for slot, want := range []uint64{0, 42} {
			outer := gc.Ref(uint32(binary.LittleEndian.Uint64(descriptor[8+slot*8:])))
			innerValue, err := instance.gc.StructGet(outer, 0)
			inner := innerValue.Ref
			if err != nil || inner.IsNull() {
				t.Fatalf("nested struct reference[%d] = %+v, %v", slot, innerValue, err)
			}
			leaf, err := instance.gc.StructGet(inner, 0)
			if err != nil || leaf.Bits != want {
				t.Fatalf("nested struct leaf[%d] = %+v, %v; want %d", slot, leaf, err, want)
			}
		}
	})
	t.Run("array", func(t *testing.T) {
		compiled, err := Compile(config, genericGCActiveNestedArrayElementModule())
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		if err := instance.CollectGC(); err != nil {
			t.Fatal(err)
		}
		descriptor := instance.tableDescriptor(0)
		for slot := range 3 {
			outer := gc.Ref(uint32(binary.LittleEndian.Uint64(descriptor[8+slot*8:])))
			for index := range 2 {
				innerValue, err := instance.gc.ArrayGet(outer, uint32(index))
				inner := innerValue.Ref
				if err != nil || inner.IsNull() {
					t.Fatalf("nested array reference[%d][%d] = %+v, %v", slot, index, innerValue, err)
				}
				want := uint64(0)
				if slot == 2 {
					want = []uint64{5, 7}[index]
				}
				leaf, err := instance.gc.ArrayGet(inner, 0)
				if err != nil || leaf.Bits != want {
					t.Fatalf("nested array leaf[%d][%d] = %+v, %v; want %d", slot, index, leaf, err, want)
				}
			}
		}
	})
}

func TestGenericGCActiveFunctionReferenceElement(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit), genericGCActiveFunctionReferenceElementModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if err := instance.CollectGC(); err != nil {
		t.Fatal(err)
	}
	descriptor := instance.tableDescriptor(0)
	outer := gc.Ref(uint32(binary.LittleEndian.Uint64(descriptor[8:])))
	value, err := instance.gc.StructGet(outer, 0)
	if err != nil || value.Kind != gc.StorageFuncRefNull || value.Bits == 0 {
		t.Fatalf("active aggregate function reference = %+v, %v", value, err)
	}
}

func TestGenericGCDeclarativeAggregateElementIsDropped(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit), genericGCDeclarativeAggregateElementModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if len(compiled.Elems) != 0 || len(compiled.passiveElems) != 0 {
		t.Fatalf("declarative aggregate metadata = active %#v, state %#v", compiled.Elems, compiled.passiveElems)
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if err := instance.CollectGC(); err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint64(instance.tableDescriptor(0)[8:]); got != 0 {
		t.Fatalf("declarative aggregate populated table slot: %#x", got)
	}
}

func TestSharedGCConstructionPublishesRootsBeforeCollection(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit), genericGCActiveTableElementModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	rt := NewRuntime()
	defer rt.Close()
	gcConfig := GCConfig{VerifyAfterCollect: true}
	first, err := instantiateCore(compiled, InstantiateOptions{store: rt.refStore, GC: gcConfig})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	domain := first.gcInvocationDomain()
	if domain == nil || domain.private {
		t.Fatal("first instance has no shared GC domain")
	}
	collectionDone := make(chan error, 1)
	second, err := instantiateCore(compiled, InstantiateOptions{
		store: rt.refStore,
		GC:    gcConfig,
		beforeStoreRegister: func(candidate *Instance) {
			if candidate.gc != first.gc {
				t.Fatal("candidate did not reuse the shared collector")
			}
			go func() { collectionDone <- first.CollectGC() }()
			awaitGCLifecycle(t, func() bool {
				return domain.invocationMu.state.Load()&invocationGateWaiters != 0
			})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err := <-collectionDone; err != nil {
		t.Fatal(err)
	}
	descriptor := second.tableDescriptor(0)
	ref := gc.Ref(uint32(binary.LittleEndian.Uint64(descriptor[8:])))
	if value, err := second.gc.StructGet(ref, 0); err != nil || value.Bits != 55 {
		t.Fatalf("published active aggregate root = %+v, %v", value, err)
	}
}

func TestGenericGCTableBulkHelpers(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit), genericGCTableHelperModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if compiled.stagedGCStructProduct() != stagedGCStructGeneric {
		t.Fatalf("struct product = %s, want generic", compiled.stagedGCStructProduct())
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	descriptor := instance.tableDescriptor(0)
	secondDescriptor := instance.tableDescriptor(1)
	elemDesc := unsafe.Slice((*byte)(offHeapPtr(instance.jm.PassiveElemPtr())), coreruntime.PassiveElemDescBytes)
	elemEntries := unsafe.Slice((*byte)(offHeapPtr(uintptr(binary.LittleEndian.Uint64(elemDesc)))), 16)
	first, second := gc.Ref(uint32(binary.LittleEndian.Uint64(elemEntries))), gc.Ref(uint32(binary.LittleEndian.Uint64(elemEntries[8:])))
	for ref, want := range map[gc.Ref]uint64{first: 55, second: 22} {
		value, err := instance.gc.StructGet(ref, 0)
		if err != nil || value.Bits != want {
			t.Fatalf("instantiated passive root %v = %+v, %v; want %d", ref, value, err, want)
		}
	}
	instance.dispatchGCHelper(gcTableFill, []uint64{0, uint64(first), 1, 0}, nil)
	binary.LittleEndian.PutUint64(elemEntries[8:], uint64(1)<<63)
	beforeInit := bytes.Clone(descriptor)
	var invalidSource any
	func() {
		defer func() { invalidSource = recover() }()
		instance.dispatchGCHelper(gcTableInit, []uint64{0, 0, 2, 0, 0}, nil)
	}()
	if _, ok := invalidSource.(gcStructHelperError); !ok || !bytes.Equal(descriptor, beforeInit) {
		t.Fatalf("invalid table.init source panic/descriptor = %#v/%x, before %x", invalidSource, descriptor, beforeInit)
	}
	binary.LittleEndian.PutUint64(elemEntries[8:], uint64(second))
	instance.dispatchGCHelper(gcTableInit, []uint64{0, 0, 2, 0, 0}, nil)
	instance.dispatchGCHelper(gcTableElemDrop, []uint64{0}, nil)
	instance.dispatchGCHelper(gcTableElemDrop, []uint64{0}, nil)
	instance.dispatchGCHelper(gcTableCopy, []uint64{1, 0, 1, 0, 0}, nil)
	instance.dispatchGCHelper(gcTableCopy, []uint64{0, 0, 1, 1, 0}, nil)
	instance.dispatchGCHelper(gcTableFill, []uint64{0, 0, 1, 0}, nil)
	if got := gc.Ref(uint32(binary.LittleEndian.Uint64(descriptor[8+8:]))); got != first {
		t.Fatalf("copied table root = %v, want %v", got, first)
	}
	if got := gc.Ref(uint32(binary.LittleEndian.Uint64(secondDescriptor[8:]))); got != first {
		t.Fatalf("cross-table copied root = %v, want %v", got, first)
	}
	var result [1]uint64
	instance.dispatchGCHelper(gcTableGrow, []uint64{uint64(second), 1, 0}, result[:])
	if result[0] != 2 || binary.LittleEndian.Uint32(descriptor) != 3 || gc.Ref(uint32(binary.LittleEndian.Uint64(descriptor[8+2*8:]))) != second {
		t.Fatalf("table.grow result/descriptor = %d/%x", result[0], descriptor)
	}
	// Same-table overlap must behave like memmove: [null, first, second]
	// copied one slot right becomes [null, null, first].
	instance.dispatchGCHelper(gcTableCopy, []uint64{1, 0, 2, 0, 0}, nil)
	if binary.LittleEndian.Uint64(descriptor[8+8:]) != 0 || gc.Ref(uint32(binary.LittleEndian.Uint64(descriptor[8+2*8:]))) != first {
		t.Fatalf("overlapping table.copy descriptor = %x", descriptor)
	}
	beforeFailure := bytes.Clone(descriptor)
	instance.dispatchGCHelper(gcTableGrow, []uint64{0, 2, 0}, result[:])
	if result[0] != uint64(^uint32(0)) || !bytes.Equal(descriptor, beforeFailure) {
		t.Fatalf("failed table.grow result/descriptor = %#x/%x, before %x", result[0], descriptor, beforeFailure)
	}
	expectBoundsTrap := func(action func()) {
		t.Helper()
		var recovered any
		func() {
			defer func() { recovered = recover() }()
			action()
		}()
		trap, ok := recovered.(gcStructHelperTrap)
		if !ok || trap.code != coreruntime.TrapTableOutOfBounds {
			t.Fatalf("table helper panic = %#v, want table out-of-bounds trap", recovered)
		}
	}
	expectBoundsTrap(func() { instance.dispatchGCHelper(gcTableFill, []uint64{3, uint64(second), 1, 0}, nil) })
	expectBoundsTrap(func() { instance.dispatchGCHelper(gcTableCopy, []uint64{0, 3, 1, 0, 0}, nil) })
	expectBoundsTrap(func() { instance.dispatchGCHelper(gcTableInit, []uint64{3, 0, 1, 0, 0}, nil) })
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		instance.dispatchGCHelper(gcTableInit, []uint64{0, 0, 1, 0, 0}, nil)
	}()
	if trap, ok := recovered.(gcStructHelperTrap); !ok || trap.code != coreruntime.TrapTableOutOfBounds {
		t.Fatalf("dropped table.init panic = %#v, want table out-of-bounds trap", recovered)
	}
	if !bytes.Equal(descriptor, beforeFailure) {
		t.Fatalf("out-of-bounds mutation changed descriptor %x -> %x", beforeFailure, descriptor)
	}
	if err := instance.CollectGC(); err != nil {
		t.Fatal(err)
	}
	value, err := instance.gc.StructGet(first, 0)
	if err != nil || value.Bits != 55 {
		t.Fatalf("copied rooted object = %+v, %v", value, err)
	}
}
