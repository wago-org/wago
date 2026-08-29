//go:build !tinygo

package wago

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
	"runtime"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func draglineUnaryModule(param, result wasm.ValType, body []byte) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{param}, []wasm.ValType{result}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func draglineBinaryModule(param, result wasm.ValType, body []byte) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{param, param}, []wasm.ValType{result}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestDraglineNativeTinyPreparedUsesDirectIntegerEntry(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("Dragline direct prepared integer entries are currently ARM64-only")
	}
	module := draglineUnaryModule(wasm.I32, wasm.I32, []byte{0x20, 0x00, 0x41, 0x07, 0x6a, 0x0b})
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if !compiled.directPreparedAt(0) {
		t.Fatal("tiny integer RailMach export did not publish its direct prepared entry")
	}
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	prepared, err := instance.PrepareFunction("run")
	if err != nil {
		t.Fatal(err)
	}
	if !prepared.directIntFast {
		t.Fatal("tiny integer RailMach export did not select direct prepared invocation")
	}
	result, err := prepared.Invoke1(I32(5))
	if err != nil || len(result) != 1 || AsI32(result[0]) != 12 {
		t.Fatalf("prepared run(5) = %v, %v; want 12", result, err)
	}
}

func TestDraglineNativePreparedInlinesTinyIntegerCallees(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("Dragline direct prepared integer entries are currently ARM64-only")
	}
	i32ToI32 := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(i32ToI32)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x00, 0x6a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x07, 0x6a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x00, 0x20, 0x00, 0x10, 0x01, 0x6a, 0x0b}),
		)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if !compiled.directPreparedAt(2) {
		t.Fatal("bounded integer caller did not publish its direct prepared entry")
	}
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	prepared, err := instance.PrepareFunction("run")
	if err != nil {
		t.Fatal(err)
	}
	result, err := prepared.Invoke1(I32(5))
	if err != nil || len(result) != 1 || AsI32(result[0]) != 17 {
		t.Fatalf("prepared run(5) = %v, %v; want 17", result, err)
	}
}

func draglineScalarModule(body []byte) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("mix", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func draglineProfiledCallIndirectModule(minSize uint32, targets ...uint32) []byte {
	i32 := []wasm.ValType{wasm.I32}
	twoI32 := []wasm.ValType{wasm.I32, wasm.I32}
	elem := []byte{0x00, 0x41, 0x00, 0x0b}
	elem = append(elem, wasmtest.ULEB(uint32(len(targets)))...)
	for _, target := range targets {
		elem = append(elem, wasmtest.ULEB(target)...)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, i32),
			wasmtest.FuncType(twoI32, i32),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec(append([]byte{0x70, 0x00}, wasmtest.ULEB(minSize)...))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("caller", 0, 0))),
		wasmtest.Section(9, wasmtest.Vec(elem)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x01, 0x20, 0x02, 0x20, 0x00, 0x11, 0x01, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x6b, 0x0b}),
		)),
	)
}

func draglineReferenceResultModule(result wasm.ValType, body []byte) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{result}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func requireDraglineCoreV3(t *testing.T) {
	t.Helper()
	if CoreFeaturesV3&^platformCoreFeatures() != 0 {
		t.Skip("Dragline Core 3 execution requires a complete Core 3 backend")
	}
}

func TestDraglineReferenceCoreOperations(t *testing.T) {
	requireDraglineCoreV3(t)
	for _, test := range []struct {
		name string
		body []byte
		want uint64
	}{
		{name: "is-null", body: []byte{0xd0, 0x6e, 0xd1, 0x0b}, want: 1},
		{name: "equal-null", body: []byte{0xd0, 0x6d, 0xd0, 0x6d, 0xd3, 0x0b}, want: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), draglineReferenceResultModule(wasm.I32, test.body))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			got, err := instance.Invoke("run")
			if err != nil || len(got) != 1 || got[0] != test.want {
				t.Fatalf("run() = %v, %v; want %d", got, err, test.want)
			}
		})
	}

	t.Run("ref-func", func(t *testing.T) {
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd2, 0x00, 0xd1, 0x0b}))),
		)
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		got, err := instance.Invoke("run")
		if err != nil || len(got) != 1 || got[0] != 0 {
			t.Fatalf("run() = %v, %v; want [0]", got, err)
		}
	})

	for _, test := range []struct {
		name string
		op   byte
		arg  uint64
		want uint64
	}{
		{name: "i31-get-s", op: 0x1d, arg: I32(0x40000000), want: I32(-0x40000000)},
		{name: "i31-get-u", op: 0x1e, arg: I32(0x40000000), want: I32(0x40000000)},
	} {
		t.Run(test.name, func(t *testing.T) {
			module := draglineUnaryModule(wasm.I32, wasm.I32, []byte{
				0x20, 0x00,
				0xfb, 0x1c, // ref.i31
				0xfb, test.op,
				0x0b,
			})
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			got, err := instance.Invoke("run", test.arg)
			if err != nil || len(got) != 1 || got[0] != test.want {
				t.Fatalf("run(%#x) = %#x, %v; want %#x", test.arg, got, err, test.want)
			}
		})
	}

	t.Run("i31-get-null-trap", func(t *testing.T) {
		module := draglineUnaryModule(wasm.I31Ref, wasm.I32, []byte{0x20, 0x00, 0xfb, 0x1d, 0x0b})
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		_, err = instance.Invoke("run", 0)
		var trap *TrapError
		if !errors.As(err, &trap) || trap.Code != TrapNullReference {
			t.Fatalf("run(null) error = %v; want %v", err, TrapNullReference)
		}
	})

	t.Run("as-non-null-trap", func(t *testing.T) {
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd0, 0x6e, 0xd4, 0x1a, 0x0b}))),
		)
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		_, err = instance.Invoke("run")
		var trap *TrapError
		if !errors.As(err, &trap) || trap.Code != TrapNullReference {
			t.Fatalf("run() error = %v; want %v", err, TrapNullReference)
		}
	})
}

func TestDraglineStructNewDefaultHelper(t *testing.T) {
	requireDraglineCoreV3(t)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x00}, // (struct)
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0xfb, 0x01, 0x00, // struct.new_default 0
			0xd1, // ref.is_null
			0x0b,
		}))),
	)
	cache := NewFunctionArtifactCache(1 << 20)
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative).WithFunctionArtifactCache(cache)
	compiled, err := Compile(config, module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	status := compiled.GCNativeRootAdmission()
	if !status.Required || !status.Exact || status.Safepoints != 1 || status.Reason != "" {
		t.Fatalf("Dragline allocation root admission = %#v", status)
	}
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	got, err := instance.Invoke("run")
	if err != nil || len(got) != 1 || got[0] != 0 {
		t.Fatalf("run() = %v, %v; want [0]", got, err)
	}
	warm, err := Compile(config, module)
	if err != nil {
		t.Fatal(err)
	}
	defer warm.Close()
	warmInstance, err := Instantiate(warm, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer warmInstance.Close()
	got, err = warmInstance.Invoke("run")
	if err != nil || len(got) != 1 || got[0] != 0 {
		t.Fatalf("warm run() = %v, %v; want [0]", got, err)
	}
	if stats := cache.Stats(); stats.Entries != 1 || stats.Hits != 1 || stats.Misses != 1 {
		t.Fatalf("helper cache stats = %#v", stats)
	}
}

func TestDraglineDeadStructNewDefaultUsesCheckedReservation(t *testing.T) {
	requireDraglineCoreV3(t)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0xfb, 0x01, 0x00, 0x1a,
			0x41, 0x07,
			0x0b,
		}))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	got, err := instance.Invoke("run")
	if err != nil || len(got) != 1 || got[0] != 7 {
		t.Fatalf("run() = %v, %v; want [7]", got, err)
	}

	body := []byte{0x01, 0x01, 0x63, 0x00}
	body = append(body,
		0xfb, 0x01, 0x00, 0x21, 0x00,
		0xfb, 0x01, 0x00, 0x1a,
		0x20, 0x00, 0xd1, 0x45,
		0x0b,
	)
	boundedModule := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	bounded, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), boundedModule)
	if err != nil {
		t.Fatal(err)
	}
	defer bounded.Close()
	boundedInstance, err := Instantiate(bounded, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 16, TinyBlockBytes: 16}})
	if err != nil {
		t.Fatal(err)
	}
	defer boundedInstance.Close()
	if got, err := boundedInstance.Invoke("run"); err == nil || !strings.Contains(err.Error(), "tiny heap exhausted") {
		t.Fatalf("bounded run() = %v, %v; want tiny heap exhaustion", got, err)
	}
}

func TestDraglineCheckedDeadGCConstructorFamilies(t *testing.T) {
	requireDraglineCoreV3(t)
	passive := append([]byte{0x01}, append(wasmtest.ULEB(3), []byte("abc")...)...)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7e, 0x01},
			[]byte{0x5e, 0x7e, 0x01},
			[]byte{0x5e, 0x78, 0x01},
			[]byte{0x5e, 0x63, 0x00, 0x01},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(
			wasmtest.ULEB(4), wasmtest.ULEB(4), wasmtest.ULEB(4), wasmtest.ULEB(4), wasmtest.ULEB(4), wasmtest.ULEB(4),
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("struct", 0, 0), wasmtest.ExportEntry("default", 0, 1),
			wasmtest.ExportEntry("uniform", 0, 2), wasmtest.ExportEntry("fixed", 0, 3),
			wasmtest.ExportEntry("data", 0, 4), wasmtest.ExportEntry("reference", 0, 5),
		)),
		wasmtest.Section(12, wasmtest.ULEB(1)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x42, 0x2a, 0xfb, 0x00, 0x00, 0x1a, 0x41, 0x07, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x02, 0xfb, 0x07, 0x01, 0x1a, 0x41, 0x07, 0x0b}),
			wasmtest.Code([]byte{0x42, 0x2a, 0x41, 0x02, 0xfb, 0x06, 0x01, 0x1a, 0x41, 0x07, 0x0b}),
			wasmtest.Code([]byte{0x42, 0x01, 0x42, 0x02, 0xfb, 0x08, 0x01, 0x02, 0x1a, 0x41, 0x07, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x00, 0x41, 0x02, 0xfb, 0x09, 0x02, 0x00, 0x1a, 0x41, 0x07, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0x00, 0x41, 0x02, 0xfb, 0x06, 0x03, 0x1a, 0x41, 0x07, 0x0b}),
		)),
		wasmtest.Section(11, wasmtest.Vec(passive)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for _, export := range []string{"struct", "default", "uniform", "fixed", "data", "reference"} {
		got, err := instance.Invoke(export)
		if err != nil || len(got) != 1 || got[0] != 7 {
			t.Fatalf("%s() = %v, %v; want [7]", export, got, err)
		}
	}
}

func TestDraglineProvenNoBarrierGCStores(t *testing.T) {
	requireDraglineCoreV3(t)
	structType := []byte{0x5f}
	structType = append(structType, wasmtest.Vec([]byte{0x6d, 0x01})...)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			structType,
			[]byte{0x5e, 0x6d, 0x01},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0xd0, 0x6d, 0xfb, 0x00, 0x00,
			0x41, 0x07, 0xfb, 0x1c, 0xfb, 0x05, 0x00, 0x00,
			0x41, 0x01, 0xfb, 0x07, 0x01,
			0x41, 0x00, 0xd0, 0x6d, 0xfb, 0x0e, 0x01,
			0xd0, 0x6d, 0xfb, 0x00, 0x00,
			0xd0, 0x6d, 0xfb, 0x00, 0x00, 0xfb, 0x05, 0x00, 0x00,
			0x41, 0x02, 0x0b,
		}))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	got, err := instance.Invoke("run")
	if err != nil || len(got) != 1 || got[0] != 2 {
		t.Fatalf("run() = %v, %v; want [2]", got, err)
	}
}

func TestDraglineStructGetHelpers(t *testing.T) {
	requireDraglineCoreV3(t)
	for _, test := range []struct {
		name      string
		fieldType byte
		result    wasm.ValType
		op        byte
	}{
		{name: "i64", fieldType: 0x7e, result: wasm.I64, op: 0x02},
		{name: "f64", fieldType: 0x7c, result: wasm.F64, op: 0x02},
		{name: "i8-signed", fieldType: 0x78, result: wasm.I32, op: 0x03},
		{name: "i8-unsigned", fieldType: 0x78, result: wasm.I32, op: 0x04},
	} {
		t.Run(test.name, func(t *testing.T) {
			module := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(
					[]byte{0x5f, 0x01, test.fieldType, 0x00},
					wasmtest.FuncType(nil, []wasm.ValType{test.result}),
				)),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
					0xfb, 0x01, 0x00, // struct.new_default 0
					0xfb, test.op, 0x00, 0x00, // struct.get[_s|_u] 0 0
					0x0b,
				}))),
			)
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			got, err := instance.Invoke("run")
			if err != nil || len(got) != 1 || got[0] != 0 {
				t.Fatalf("run() = %#x, %v; want [0]", got, err)
			}
		})
	}

	t.Run("nullable-reference", func(t *testing.T) {
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				[]byte{0x5f, 0x01, 0x6e, 0x00}, // (struct (field (ref null any)))
				wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
				0xfb, 0x01, 0x00, // struct.new_default 0
				0xfb, 0x02, 0x00, 0x00, // struct.get 0 0
				0xd1, // ref.is_null
				0x0b,
			}))),
		)
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		got, err := instance.Invoke("run")
		if err != nil || len(got) != 1 || got[0] != 1 {
			t.Fatalf("run() = %#x, %v; want [1]", got, err)
		}
	})

	t.Run("v128-read-write", func(t *testing.T) {
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				[]byte{0x5f, 0x01, 0x7b, 0x01},
				wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}),
			)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(24), []byte{
				0x01, 0x01, 0x63, 0x00,
				0xfb, 0x01, 0x00, 0x21, 0x01,
				0x20, 0x01, 0x20, 0x00, 0xfb, 0x05, 0x00, 0x00,
				0x20, 0x01, 0xfb, 0x02, 0x00, 0x00, 0x0b,
			}...))),
		)
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		const lo, hi = uint64(0x0706050403020100), uint64(0x0f0e0d0c0b0a0908)
		got, err := instance.Invoke("run", lo, hi)
		if err != nil || len(got) != 2 || got[0] != lo || got[1] != hi {
			t.Fatalf("run(v128) = %#x, %v; want [%#x %#x]", got, err, lo, hi)
		}
	})

	t.Run("null-trap", func(t *testing.T) {
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				[]byte{0x5f, 0x01, 0x7e, 0x00},
				[]byte{0x60, 0x01, 0x63, 0x00, 0x01, 0x7e}, // (ref null 0) -> i64
			)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}))),
		)
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		_, err = instance.Invoke("run", 0)
		var trap *TrapError
		if !errors.As(err, &trap) || trap.Code != TrapNullReference {
			t.Fatalf("run(null) error = %v; want %v", err, TrapNullReference)
		}
	})
}

func TestDraglineStructSetHelper(t *testing.T) {
	requireDraglineCoreV3(t)
	module := func(field []byte, result wasm.ValType, instructions []byte) []byte {
		structType := append([]byte{0x5f, 0x01}, field...)
		body := []byte{0x01, 0x01, 0x63, 0x00} // one (ref null 0) local
		body = append(body, instructions...)
		code := append(wasmtest.ULEB(uint32(len(body))), body...)
		return wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(structType, wasmtest.FuncType(nil, []wasm.ValType{result}))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(code)),
		)
	}
	for _, test := range []struct {
		name         string
		field        []byte
		result       wasm.ValType
		valueAndRead []byte
		want         uint64
	}{
		{
			name: "i64", field: []byte{0x7e, 0x01}, result: wasm.I64, want: 42,
			valueAndRead: []byte{0x42, 0x2a, 0xfb, 0x05, 0x00, 0x00, 0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b},
		},
		{
			name: "i8-signed", field: []byte{0x78, 0x01}, result: wasm.I32, want: I32(-1),
			valueAndRead: []byte{0x41, 0x7f, 0xfb, 0x05, 0x00, 0x00, 0x20, 0x00, 0xfb, 0x03, 0x00, 0x00, 0x0b},
		},
		{
			name: "i8-unsigned", field: []byte{0x78, 0x01}, result: wasm.I32, want: 255,
			valueAndRead: []byte{0x41, 0x7f, 0xfb, 0x05, 0x00, 0x00, 0x20, 0x00, 0xfb, 0x04, 0x00, 0x00, 0x0b},
		},
		{
			name: "i31-reference", field: []byte{0x6e, 0x01}, result: wasm.I32, want: 0,
			valueAndRead: []byte{0x41, 0x07, 0xfb, 0x1c, 0xfb, 0x05, 0x00, 0x00, 0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0xd1, 0x0b},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			instructions := []byte{0xfb, 0x01, 0x00, 0x22, 0x00} // new_default; local.tee 0
			instructions = append(instructions, test.valueAndRead...)
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module(test.field, test.result, instructions))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			got, err := instance.Invoke("run")
			if err != nil || len(got) != 1 || got[0] != test.want {
				t.Fatalf("run() = %#x, %v; want [%#x]", got, err, test.want)
			}
		})
	}
}

func TestDraglineStructNewHelper(t *testing.T) {
	requireDraglineCoreV3(t)
	t.Run("mixed-scalars", func(t *testing.T) {
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				[]byte{0x5f, 0x03, 0x7e, 0x01, 0x7c, 0x01, 0x78, 0x01},
				wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}),
			)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
				0x42, 0x2a, // i64.const 42
				0x44, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x08, 0x40, // f64.const 3
				0x41, 0x7f, // i32.const -1
				0xfb, 0x00, 0x00, // struct.new 0
				0xfb, 0x02, 0x00, 0x00, // struct.get 0 0
				0x0b,
			}))),
		)
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		got, err := instance.Invoke("run")
		if err != nil || len(got) != 1 || got[0] != 42 {
			t.Fatalf("run() = %#x, %v; want [42]", got, err)
		}
	})

	t.Run("reference-initializer-root", func(t *testing.T) {
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				[]byte{0x5f, 0x00},
				[]byte{0x5f, 0x01, 0x63, 0x00, 0x01}, // (struct (field (mut (ref null 0))))
				wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
				0xfb, 0x01, 0x00, // struct.new_default 0
				0xfb, 0x00, 0x01, // struct.new 1
				0xfb, 0x02, 0x01, 0x00, // struct.get 1 0
				0xd1, // ref.is_null
				0x0b,
			}))),
		)
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		status := compiled.GCNativeRootAdmission()
		if !status.Required || !status.Exact || status.Safepoints != 2 || status.MaximumRoots != 1 || status.Reason != "" {
			t.Fatalf("Dragline struct.new root admission = %#v", status)
		}
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		got, err := instance.Invoke("run")
		if err != nil || len(got) != 1 || got[0] != 0 {
			t.Fatalf("run() = %#x, %v; want [0]", got, err)
		}
	})

	t.Run("v128-initializer", func(t *testing.T) {
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				[]byte{0x5f, 0x02, 0x7b, 0x01, 0x7e, 0x01},
				wasmtest.FuncType([]wasm.ValType{wasm.V128, wasm.I64}, []wasm.ValType{wasm.V128}),
			)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
				0x20, 0x00, 0x20, 0x01, 0xfb, 0x00, 0x00,
				0xfb, 0x02, 0x00, 0x00, 0x0b,
			}))),
		)
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		status := compiled.GCNativeRootAdmission()
		if !status.Required || !status.Exact || status.Safepoints != 1 || status.MaximumRoots != 0 || status.Reason != "" {
			t.Fatalf("Dragline v128 struct.new root admission = %#v", status)
		}
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		const lo, hi = uint64(0x1716151413121110), uint64(0x1f1e1d1c1b1a1918)
		got, err := instance.Invoke("run", lo, hi, 42)
		if err != nil || len(got) != 2 || got[0] != lo || got[1] != hi {
			t.Fatalf("run(v128) = %#x, %v; want [%#x %#x]", got, err, lo, hi)
		}
	})
}

func TestDraglineArrayNewDefaultAndLenHelpers(t *testing.T) {
	requireDraglineCoreV3(t)
	body := []byte{
		0x01, 0x01, 0x63, 0x00, // one (ref null 0) local
		0x41, 0x03, 0xfb, 0x07, 0x00, 0x21, 0x00, // local0 = array.new_default 0, len 3
		0x42, 0x07, 0x41, 0x01, 0xfb, 0x06, 0x00, 0x1a, // initialized collecting allocation; drop
		0x20, 0x00, 0xfb, 0x0f, // array.len local0
		0x0b,
	}
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5e, 0x7e, 0x01}, // (array (mut i64))
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(code)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	status := compiled.GCNativeRootAdmission()
	if !status.Required || !status.Exact || status.Safepoints != 2 || status.MaximumRoots != 1 || status.Reason != "" {
		t.Fatalf("Dragline array root admission = %#v", status)
	}
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	got, err := instance.Invoke("run")
	if err != nil || len(got) != 1 || got[0] != 3 {
		t.Fatalf("run() = %#x, %v; want [3]", got, err)
	}
}

func TestDraglineArrayGetSetHelpers(t *testing.T) {
	requireDraglineCoreV3(t)
	module := func(field []byte, result wasm.ValType, operations []byte) []byte {
		arrayType := append([]byte{0x5e}, field...)
		body := []byte{0x01, 0x01, 0x63, 0x00}                        // one (ref null 0) local
		body = append(body, 0x41, 0x01, 0xfb, 0x07, 0x00, 0x21, 0x00) // local0 = array.new_default 0, len 1
		body = append(body, operations...)
		code := append(wasmtest.ULEB(uint32(len(body))), body...)
		return wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(arrayType, wasmtest.FuncType(nil, []wasm.ValType{result}))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(code)),
		)
	}
	for _, test := range []struct {
		name       string
		field      []byte
		result     wasm.ValType
		operations []byte
		want       uint64
	}{
		{
			name: "i64", field: []byte{0x7e, 0x01}, result: wasm.I64, want: 42,
			operations: []byte{0x20, 0x00, 0x41, 0x00, 0x42, 0x2a, 0xfb, 0x0e, 0x00, 0x20, 0x00, 0x41, 0x00, 0xfb, 0x0b, 0x00, 0x0b},
		},
		{
			name: "f64", field: []byte{0x7c, 0x01}, result: wasm.F64, want: 0x4008000000000000,
			operations: []byte{0x20, 0x00, 0x41, 0x00, 0x44, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x08, 0x40, 0xfb, 0x0e, 0x00, 0x20, 0x00, 0x41, 0x00, 0xfb, 0x0b, 0x00, 0x0b},
		},
		{
			name: "i8-signed", field: []byte{0x78, 0x01}, result: wasm.I32, want: I32(-1),
			operations: []byte{0x20, 0x00, 0x41, 0x00, 0x41, 0x7f, 0xfb, 0x0e, 0x00, 0x20, 0x00, 0x41, 0x00, 0xfb, 0x0c, 0x00, 0x0b},
		},
		{
			name: "i8-unsigned", field: []byte{0x78, 0x01}, result: wasm.I32, want: 255,
			operations: []byte{0x20, 0x00, 0x41, 0x00, 0x41, 0x7f, 0xfb, 0x0e, 0x00, 0x20, 0x00, 0x41, 0x00, 0xfb, 0x0d, 0x00, 0x0b},
		},
		{
			name: "i31-reference", field: []byte{0x6e, 0x01}, result: wasm.I32, want: 0,
			operations: []byte{0x20, 0x00, 0x41, 0x00, 0x41, 0x07, 0xfb, 0x1c, 0xfb, 0x0e, 0x00, 0x20, 0x00, 0x41, 0x00, 0xfb, 0x0b, 0x00, 0xd1, 0x0b},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module(test.field, test.result, test.operations))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			got, err := instance.Invoke("run")
			if err != nil || len(got) != 1 || got[0] != test.want {
				t.Fatalf("run() = %#x, %v; want [%#x]", got, err, test.want)
			}
		})
	}

	t.Run("bounds-trap", func(t *testing.T) {
		operations := []byte{0x20, 0x00, 0x41, 0x01, 0xfb, 0x0b, 0x00, 0x0b}
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module([]byte{0x7e, 0x01}, wasm.I64, operations))
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		_, err = instance.Invoke("run")
		var trap *TrapError
		if !errors.As(err, &trap) || trap.Code != TrapBuiltin {
			t.Fatalf("run(out-of-bounds) error = %v; want %v", err, TrapBuiltin)
		}
	})

	t.Run("v128-read-write", func(t *testing.T) {
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				[]byte{0x5e, 0x7b, 0x01},
				wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}),
			)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(28), []byte{
				0x01, 0x01, 0x63, 0x00,
				0x41, 0x02, 0xfb, 0x07, 0x00, 0x21, 0x01,
				0x20, 0x01, 0x41, 0x01, 0x20, 0x00, 0xfb, 0x0e, 0x00,
				0x20, 0x01, 0x41, 0x01, 0xfb, 0x0b, 0x00, 0x0b,
			}...))),
		)
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		const lo, hi = uint64(0x1716151413121110), uint64(0x1f1e1d1c1b1a1918)
		got, err := instance.Invoke("run", lo, hi)
		if err != nil || len(got) != 2 || got[0] != lo || got[1] != hi {
			t.Fatalf("run(v128) = %#x, %v; want [%#x %#x]", got, err, lo, hi)
		}
	})
}

func TestDraglineArrayNewHelper(t *testing.T) {
	requireDraglineCoreV3(t)
	for _, test := range []struct {
		name   string
		field  byte
		result wasm.ValType
		value  []byte
		get    byte
		want   uint64
	}{
		{name: "i64", field: 0x7e, result: wasm.I64, value: []byte{0x42, 0x2a}, get: 0x0b, want: 42},
		{name: "f64", field: 0x7c, result: wasm.F64, value: []byte{0x44, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x08, 0x40}, get: 0x0b, want: 0x4008000000000000},
		{name: "i8-signed", field: 0x78, result: wasm.I32, value: []byte{0x41, 0x7f}, get: 0x0c, want: I32(-1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := []byte{0x00}
			body = append(body, test.value...)
			body = append(body,
				0x41, 0x03, 0xfb, 0x06, 0x00, // array.new 0, len 3
				0x41, 0x02, 0xfb, test.get, 0x00, // array.get[_s] 0, index 2
				0x0b,
			)
			module := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(
					[]byte{0x5e, test.field, 0x01},
					wasmtest.FuncType(nil, []wasm.ValType{test.result}),
				)),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
			)
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			got, err := instance.Invoke("run")
			if err != nil || len(got) != 1 || got[0] != test.want {
				t.Fatalf("run() = %#x, %v; want [%#x]", got, err, test.want)
			}
		})
	}

	t.Run("v128", func(t *testing.T) {
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				[]byte{0x5e, 0x7b, 0x01},
				wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}),
			)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
				0x20, 0x00, 0x41, 0x03, 0xfb, 0x06, 0x00, // array.new 0, len 3
				0x41, 0x02, 0xfb, 0x0b, 0x00, 0x0b, // array.get 0, index 2
			}))),
		)
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		status := compiled.GCNativeRootAdmission()
		if !status.Required || !status.Exact || status.Safepoints != 1 || status.MaximumRoots != 0 || status.Reason != "" {
			t.Fatalf("Dragline v128 array.new root admission = %#v", status)
		}
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		const lo, hi = uint64(0x1716151413121110), uint64(0x1f1e1d1c1b1a1918)
		got, err := instance.Invoke("run", lo, hi)
		if err != nil || len(got) != 2 || got[0] != lo || got[1] != hi {
			t.Fatalf("run(v128) = %#x, %v; want [%#x %#x]", got, err, lo, hi)
		}
	})
}

func TestDraglineArrayNewFixedHelper(t *testing.T) {
	requireDraglineCoreV3(t)
	t.Run("i64", func(t *testing.T) {
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				[]byte{0x5e, 0x7e, 0x01}, // (array (mut i64))
				wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}),
			)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
				0x42, 0x01, 0x42, 0x02, 0x42, 0x03, // i64.const 1, 2, 3
				0xfb, 0x08, 0x00, 0x03, // array.new_fixed 0 3
				0x41, 0x01, 0xfb, 0x0b, 0x00, // array.get 0, index 1
				0x0b,
			}))),
		)
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		status := compiled.GCNativeRootAdmission()
		if !status.Required || !status.Exact || status.Safepoints != 1 || status.MaximumRoots != 0 || status.Reason != "" {
			t.Fatalf("Dragline array.new_fixed root admission = %#v", status)
		}
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		got, err := instance.Invoke("run")
		if err != nil || len(got) != 1 || got[0] != 2 {
			t.Fatalf("run() = %#x, %v; want [2]", got, err)
		}
	})

	t.Run("reference-initializers-are-exact-roots", func(t *testing.T) {
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				[]byte{0x5f, 0x00},             // (struct)
				[]byte{0x5e, 0x63, 0x00, 0x01}, // (array (mut (ref null 0)))
				wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
				0xfb, 0x01, 0x00, // struct.new_default 0
				0xfb, 0x01, 0x00, // struct.new_default 0
				0xfb, 0x08, 0x01, 0x02, // array.new_fixed 1 2
				0x41, 0x00, 0xfb, 0x0b, 0x01, // array.get 1, index 0
				0xd1, 0x0b, // ref.is_null
			}))),
		)
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		status := compiled.GCNativeRootAdmission()
		if !status.Required || !status.Exact || status.Safepoints != 3 || status.MaximumRoots != 2 || status.Reason != "" {
			t.Fatalf("Dragline array.new_fixed reference root admission = %#v", status)
		}
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		got, err := instance.Invoke("run")
		if err != nil || len(got) != 1 || got[0] != 0 {
			t.Fatalf("run() = %#x, %v; want [0]", got, err)
		}
	})

	t.Run("v128", func(t *testing.T) {
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				[]byte{0x5e, 0x7b, 0x01}, // (array (mut v128))
				wasmtest.FuncType([]wasm.ValType{wasm.V128, wasm.V128}, []wasm.ValType{wasm.V128}),
			)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
				0x20, 0x00, 0x20, 0x01, 0xfb, 0x08, 0x00, 0x02,
				0x41, 0x01, 0xfb, 0x0b, 0x00, 0x0b,
			}))),
		)
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		status := compiled.GCNativeRootAdmission()
		if !status.Required || !status.Exact || status.Safepoints != 1 || status.MaximumRoots != 0 || status.Reason != "" {
			t.Fatalf("Dragline v128 array.new_fixed root admission = %#v", status)
		}
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		const lo0, hi0 = uint64(0x0706050403020100), uint64(0x0f0e0d0c0b0a0908)
		const lo1, hi1 = uint64(0x1716151413121110), uint64(0x1f1e1d1c1b1a1918)
		got, err := instance.Invoke("run", lo0, hi0, lo1, hi1)
		if err != nil || len(got) != 2 || got[0] != lo1 || got[1] != hi1 {
			t.Fatalf("run(v128) = %#x, %v; want [%#x %#x]", got, err, lo1, hi1)
		}
	})
}

func TestDraglineArrayFillHelper(t *testing.T) {
	requireDraglineCoreV3(t)
	t.Run("i64", func(t *testing.T) {
		body := []byte{
			0x01, 0x01, 0x63, 0x00, // one (ref null 0) local
			0x41, 0x04, 0xfb, 0x07, 0x00, 0x21, 0x00, // local0 = array.new_default 0, len 4
			0x20, 0x00, 0x41, 0x01, 0x42, 0x2a, 0x41, 0x02, 0xfb, 0x10, 0x00, // fill [1:3] with 42
			0x20, 0x00, 0x41, 0x02, 0xfb, 0x0b, 0x00, 0x0b, // get index 2
		}
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				[]byte{0x5e, 0x7e, 0x01},
				wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}),
			)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
		)
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		got, err := instance.Invoke("run")
		if err != nil || len(got) != 1 || got[0] != 42 {
			t.Fatalf("run() = %#x, %v; want [42]", got, err)
		}
	})

	t.Run("f64", func(t *testing.T) {
		body := []byte{
			0x01, 0x01, 0x63, 0x00,
			0x41, 0x02, 0xfb, 0x07, 0x00, 0x21, 0x00,
			0x20, 0x00, 0x41, 0x00, 0x44, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x08, 0x40, 0x41, 0x02, 0xfb, 0x10, 0x00,
			0x20, 0x00, 0x41, 0x01, 0xfb, 0x0b, 0x00, 0x0b,
		}
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7c, 0x01}, wasmtest.FuncType(nil, []wasm.ValType{wasm.F64}))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
		)
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		got, err := instance.Invoke("run")
		if err != nil || len(got) != 1 || got[0] != 0x4008000000000000 {
			t.Fatalf("run() = %#x, %v; want [3.0]", got, err)
		}
	})

	t.Run("v128", func(t *testing.T) {
		body := []byte{
			0x01, 0x01, 0x63, 0x00,
			0x41, 0x03, 0xfb, 0x07, 0x00, 0x21, 0x01,
			0x20, 0x01, 0x41, 0x00, 0x20, 0x00, 0x41, 0x03, 0xfb, 0x10, 0x00,
			0x20, 0x01, 0x41, 0x02, 0xfb, 0x0b, 0x00, 0x0b,
		}
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				[]byte{0x5e, 0x7b, 0x01},
				wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}),
			)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
		)
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		status := compiled.GCNativeRootAdmission()
		if !status.Required || !status.Exact || status.Safepoints != 1 || status.MaximumRoots != 0 || status.Reason != "" {
			t.Fatalf("Dragline v128 array.fill root admission = %#v", status)
		}
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		const lo, hi = uint64(0x2726252423222120), uint64(0x2f2e2d2c2b2a2928)
		got, err := instance.Invoke("run", lo, hi)
		if err != nil || len(got) != 2 || got[0] != lo || got[1] != hi {
			t.Fatalf("run(v128) = %#x, %v; want [%#x %#x]", got, err, lo, hi)
		}
	})

	t.Run("reference-barrier", func(t *testing.T) {
		body := []byte{
			0x01, 0x01, 0x63, 0x01, // one (ref null 1) local
			0x41, 0x02, 0xfb, 0x07, 0x01, 0x21, 0x00, // local0 = array.new_default 1, len 2
			0x20, 0x00, 0x41, 0x00, 0xfb, 0x01, 0x00, 0x41, 0x02, 0xfb, 0x10, 0x01, // fill with struct.new_default 0
			0x20, 0x00, 0x41, 0x01, 0xfb, 0x0b, 0x01, 0xd1, 0x0b,
		}
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				[]byte{0x5f, 0x00},
				[]byte{0x5e, 0x63, 0x00, 0x01},
				wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
		)
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		got, err := instance.Invoke("run")
		if err != nil || len(got) != 1 || got[0] != 0 {
			t.Fatalf("run() = %#x, %v; want [0]", got, err)
		}
	})

	t.Run("bounds-trap", func(t *testing.T) {
		body := []byte{
			0x00,
			0x41, 0x04, 0xfb, 0x07, 0x00,
			0x41, 0x03, 0x42, 0x01, 0x41, 0x02, 0xfb, 0x10, 0x00,
			0x0b,
		}
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7e, 0x01}, wasmtest.FuncType(nil, nil))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
		)
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		_, err = instance.Invoke("run")
		var trap *TrapError
		if !errors.As(err, &trap) || trap.Code != TrapBuiltin {
			t.Fatalf("run(out-of-bounds) error = %v; want %v", err, TrapBuiltin)
		}
	})
}

func TestDraglineArrayCopyHelper(t *testing.T) {
	requireDraglineCoreV3(t)
	body := []byte{
		0x01, 0x01, 0x63, 0x00, // one (ref null 0) local
		0x42, 0x01, 0x42, 0x02, 0x42, 0x03, 0x42, 0x04,
		0xfb, 0x08, 0x00, 0x04, 0x21, 0x00, // local0 = [1,2,3,4]
		0x20, 0x00, 0x41, 0x01, 0x20, 0x00, 0x41, 0x00, 0x41, 0x03,
		0xfb, 0x11, 0x00, 0x00, // overlapping copy [0:3] to [1:4]
		0x20, 0x00, 0x41, 0x03, 0xfb, 0x0b, 0x00, 0x0b,
	}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5e, 0x7e, 0x01},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	got, err := instance.Invoke("run")
	if err != nil || len(got) != 1 || got[0] != 3 {
		t.Fatalf("run() = %#x, %v; want [3]", got, err)
	}

	t.Run("reference-barrier", func(t *testing.T) {
		body := []byte{
			0x01, 0x01, 0x63, 0x01,
			0x41, 0x03, 0xfb, 0x07, 0x01, 0x21, 0x00,
			0x20, 0x00, 0x41, 0x00, 0xfb, 0x01, 0x00, 0x41, 0x01, 0xfb, 0x10, 0x01,
			0x20, 0x00, 0x41, 0x01, 0x20, 0x00, 0x41, 0x00, 0x41, 0x01, 0xfb, 0x11, 0x01, 0x01,
			0x20, 0x00, 0x41, 0x01, 0xfb, 0x0b, 0x01, 0xd1, 0x0b,
		}
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				[]byte{0x5f, 0x00},
				[]byte{0x5e, 0x63, 0x00, 0x01},
				wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
		)
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		got, err := instance.Invoke("run")
		if err != nil || len(got) != 1 || got[0] != 0 {
			t.Fatalf("run() = %#x, %v; want [0]", got, err)
		}
	})

	for _, test := range []struct {
		name      string
		arrayType []byte
		dst       byte
		length    byte
		wantTrap  bool
	}{
		{name: "v128", arrayType: []byte{0x5e, 0x7b, 0x01}, dst: 0x00, length: 0x02},
		{name: "bounds-trap", arrayType: []byte{0x5e, 0x7e, 0x01}, dst: 0x01, length: 0x02, wantTrap: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := []byte{
				0x01, 0x01, 0x63, 0x00,
				0x41, 0x02, 0xfb, 0x07, 0x00, 0x21, 0x00,
				0x20, 0x00, 0x41, test.dst, 0x20, 0x00, 0x41, 0x00, 0x41, test.length,
				0xfb, 0x11, 0x00, 0x00, 0x0b,
			}
			module := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(test.arrayType, wasmtest.FuncType(nil, nil))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
			)
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			_, err = instance.Invoke("run")
			if !test.wantTrap {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var trap *TrapError
			if !errors.As(err, &trap) || trap.Code != TrapBuiltin {
				t.Fatalf("run(out-of-bounds) error = %v; want %v", err, TrapBuiltin)
			}
		})
	}
}

func TestDraglineArrayDataSegmentHelpers(t *testing.T) {
	requireDraglineCoreV3(t)
	newBody := []byte{
		0x00,
		0x41, 0x01, 0x41, 0x03, 0xfb, 0x09, 0x00, 0x00, // array.new_data 0 0, source 1, len 3
		0x41, 0x01, 0xfb, 0x0d, 0x00, 0x0b, // array.get_u 0, index 1
	}
	initBody := []byte{
		0x01, 0x01, 0x63, 0x00,
		0x41, 0x04, 0xfb, 0x07, 0x00, 0x21, 0x00,
		0x20, 0x00, 0x41, 0x01, 0x41, 0x01, 0x41, 0x02, 0xfb, 0x12, 0x00, 0x00, // array.init_data 0 0
		0x20, 0x00, 0x41, 0x02, 0xfb, 0x0d, 0x00, 0x0b,
	}
	passive := append([]byte{0x01}, append(wasmtest.ULEB(5), []byte("hello")...)...)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5e, 0x78, 0x01}, // (array (mut i8))
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(2), wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("new", 0, 0), wasmtest.ExportEntry("init", 0, 1),
			wasmtest.ExportEntry("new_trap", 0, 2), wasmtest.ExportEntry("init_trap", 0, 3),
			wasmtest.ExportEntry("drop", 0, 4),
		)),
		wasmtest.Section(12, wasmtest.ULEB(1)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(newBody))), newBody...),
			append(wasmtest.ULEB(uint32(len(initBody))), initBody...),
			wasmtest.Code([]byte{0x41, 0x04, 0x41, 0x02, 0xfb, 0x09, 0x00, 0x00, 0x1a, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x01, 0xfb, 0x07, 0x00, 0x41, 0x00, 0x41, 0x04, 0x41, 0x02, 0xfb, 0x12, 0x00, 0x00, 0x0b}),
			wasmtest.Code([]byte{0xfc, 0x09, 0x00, 0x0b}),
		)),
		wasmtest.Section(11, wasmtest.Vec(passive)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	status := compiled.GCNativeRootAdmission()
	if !status.Required || !status.Exact || status.Safepoints != 4 || status.MaximumRoots != 0 || status.Reason != "" {
		t.Fatalf("Dragline data-segment array root admission = %#v", status)
	}
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for _, export := range []string{"new", "init"} {
		got, err := instance.Invoke(export)
		if err != nil || len(got) != 1 || got[0] != 'l' {
			t.Fatalf("%s() = %#x, %v; want [%d]", export, got, err, 'l')
		}
	}
	for _, export := range []string{"new_trap", "init_trap"} {
		_, err := instance.Invoke(export)
		var trap *TrapError
		if !errors.As(err, &trap) {
			t.Fatalf("%s() error = %v; want trap", export, err)
		}
	}
	if _, err := instance.Invoke("drop"); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.Invoke("new"); err == nil {
		t.Fatal("array.new_data after data.drop did not trap")
	}
}

func TestDraglineArrayElementSegmentHelpers(t *testing.T) {
	requireDraglineCoreV3(t)
	newBody := []byte{
		0x00,
		0x41, 0x00, 0x41, 0x01, 0xfb, 0x0a, 0x00, 0x00, // array.new_elem 0 0
		0x41, 0x00, 0xfb, 0x0b, 0x00, 0xd1, 0x0b,
	}
	initBody := []byte{
		0x01, 0x01, 0x63, 0x00,
		0x41, 0x01, 0xfb, 0x07, 0x00, 0x21, 0x00,
		0x20, 0x00, 0x41, 0x00, 0x41, 0x00, 0x41, 0x01, 0xfb, 0x13, 0x00, 0x00, // array.init_elem 0 0
		0x20, 0x00, 0x41, 0x00, 0xfb, 0x0b, 0x00, 0xd1, 0x0b,
	}
	element := append(wasmtest.ULEB(1), 0x05, 0x70) // one passive funcref expression segment
	element = append(element, wasmtest.Vec([]byte{0xd2, 0x00, 0x0b})...)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5e, 0x70, 0x01}, // (array (mut funcref))
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(2), wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("new", 0, 0), wasmtest.ExportEntry("init", 0, 1),
			wasmtest.ExportEntry("new_trap", 0, 2), wasmtest.ExportEntry("init_trap", 0, 3),
			wasmtest.ExportEntry("drop", 0, 4),
		)),
		wasmtest.Section(9, element),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(newBody))), newBody...),
			append(wasmtest.ULEB(uint32(len(initBody))), initBody...),
			wasmtest.Code([]byte{0x41, 0x01, 0x41, 0x01, 0xfb, 0x0a, 0x00, 0x00, 0x1a, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x01, 0xfb, 0x07, 0x00, 0x41, 0x00, 0x41, 0x01, 0x41, 0x01, 0xfb, 0x13, 0x00, 0x00, 0x0b}),
			wasmtest.Code([]byte{0xfc, 0x0d, 0x00, 0x0b}),
		)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	status := compiled.GCNativeRootAdmission()
	if !status.Required || !status.Exact || status.Safepoints != 4 || status.MaximumRoots != 0 || status.Reason != "" {
		t.Fatalf("Dragline element-segment array root admission = %#v", status)
	}
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for _, export := range []string{"new", "init"} {
		got, err := instance.Invoke(export)
		if err != nil || len(got) != 1 || got[0] != 0 {
			t.Fatalf("%s() = %#x, %v; want [0]", export, got, err)
		}
	}
	for _, export := range []string{"new_trap", "init_trap"} {
		_, err := instance.Invoke(export)
		var trap *TrapError
		if !errors.As(err, &trap) {
			t.Fatalf("%s() error = %v; want trap", export, err)
		}
	}
	if _, err := instance.Invoke("drop"); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.Invoke("new"); err == nil {
		t.Fatal("array.new_elem after elem.drop did not trap")
	}
}

func TestDraglineExternConversionHelpers(t *testing.T) {
	requireDraglineCoreV3(t)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.ExternRef}, []wasm.ValType{wasm.ExternRef}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("roundtrip", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, // local.get 0
			0xfb, 0x1a, // any.convert_extern
			0xfb, 0x1b, // extern.convert_any
			0x0b,
		}))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	ref, err := instance.NewExternRef("dragline-extern-roundtrip")
	if err != nil {
		t.Fatal(err)
	}
	want := ValueExternRef(ref).Bits()
	got, err := instance.Invoke("roundtrip", want)
	if err != nil || len(got) != 1 || got[0] != want {
		t.Fatalf("roundtrip(%#x) = %#x, %v; want [%#x]", want, got, err, want)
	}
	got, err = instance.Invoke("roundtrip", 0)
	if err != nil || len(got) != 1 || got[0] != 0 {
		t.Fatalf("roundtrip(null) = %#x, %v; want [0]", got, err)
	}
}

func TestDraglineRefTestHelper(t *testing.T) {
	requireDraglineCoreV3(t)
	for _, test := range []struct {
		name string
		body []byte
		want uint64
	}{
		{name: "i31", body: []byte{0x41, 0x07, 0xfb, 0x1c, 0xfb, 0x14, 0x6c, 0x0b}, want: 1},
		{name: "nullable-null", body: []byte{0xd0, 0x6e, 0xfb, 0x15, 0x6e, 0x0b}, want: 1},
		{name: "nonnullable-null", body: []byte{0xd0, 0x6e, 0xfb, 0x14, 0x6e, 0x0b}, want: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			module := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(test.body))),
			)
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			got, err := instance.Invoke("run")
			if err != nil || len(got) != 1 || got[0] != test.want {
				t.Fatalf("run() = %#x, %v; want [%d]", got, err, test.want)
			}
		})
	}

	t.Run("defined-struct", func(t *testing.T) {
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				[]byte{0x5f, 0x00},
				wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
				0xfb, 0x01, 0x00, // struct.new_default 0
				0xfb, 0x14, 0x00, // ref.test 0
				0x0b,
			}))),
		)
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		got, err := instance.Invoke("run")
		if err != nil || len(got) != 1 || got[0] != 1 {
			t.Fatalf("run() = %#x, %v; want [1]", got, err)
		}
	})

	t.Run("nullable-function-null", func(t *testing.T) {
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd0, 0x70, 0xfb, 0x15, 0x70, 0x0b}))),
		)
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		got, err := instance.Invoke("run")
		if err != nil || len(got) != 1 || got[0] != 1 {
			t.Fatalf("run() = %#x, %v; want [1]", got, err)
		}
	})
}

func TestDraglineBranchCastHelpers(t *testing.T) {
	requireDraglineCoreV3(t)
	for _, test := range []struct {
		name        string
		blockResult wasm.ValType
		body        []byte
	}{
		{
			name: "br_on_cast-match", blockResult: wasm.EqRef,
			body: []byte{
				0x02, 0x02, // block (type 2), result eqref
				0x20, 0x00, // local.get 0 (null)
				0xfb, 0x18, 0x03, 0x00, 0x6e, 0x6d, // br_on_cast 0 anyref -> eqref
				0x00, // unreachable fallthrough
				0x0b,
				0x1a, 0x41, 0x01, // drop; i32.const 1
				0x0b,
			},
		},
		{
			name: "br_on_cast_fail-null", blockResult: wasm.AnyRef,
			body: []byte{
				0x02, 0x02, // block (type 2), result anyref
				0x20, 0x00, // local.get 0 (null)
				0xfb, 0x19, 0x01, 0x00, 0x6e, 0x6d, // br_on_cast_fail 0 anyref -> (ref eq)
				0x00, // unreachable fallthrough
				0x0b,
				0x1a, 0x41, 0x01, // drop; i32.const 1
				0x0b,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			module := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(
					[]byte{0x5f, 0x00}, // collector provision for abstract reference tests
					wasmtest.FuncType([]wasm.ValType{wasm.AnyRef}, []wasm.ValType{wasm.I32}),
					wasmtest.FuncType(nil, []wasm.ValType{test.blockResult}),
				)),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(test.body))),
			)
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			got, err := instance.Invoke("run", 0)
			if err != nil || len(got) != 1 || got[0] != 1 {
				t.Fatalf("run() = %#x, %v; want [1]", got, err)
			}
		})
	}

	t.Run("collecting-live-root", func(t *testing.T) {
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				[]byte{0x5f, 0x00},
				wasmtest.FuncType([]wasm.ValType{wasm.AnyRef}, []wasm.ValType{wasm.I32}),
				wasmtest.FuncType(nil, []wasm.ValType{wasm.EqRef}),
			)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
				0xfb, 0x01, 0x00, 0x1a, // collecting allocation; drop
				0x02, 0x02,
				0x20, 0x00,
				0xfb, 0x18, 0x03, 0x00, 0x6e, 0x6d,
				0x00,
				0x0b,
				0x1a, 0x41, 0x01,
				0x0b,
			}))),
		)
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		got, err := instance.Invoke("run", 0)
		if err != nil || len(got) != 1 || got[0] != 1 {
			t.Fatalf("run() = %#x, %v; want [1]", got, err)
		}
	})

	t.Run("multi-value-target", func(t *testing.T) {
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				[]byte{0x5f, 0x00},
				wasmtest.FuncType([]wasm.ValType{wasm.AnyRef}, []wasm.ValType{wasm.I32}),
				wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.EqRef}),
			)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
				0x02, 0x02,
				0x41, 0x07,
				0x20, 0x00,
				0xfb, 0x18, 0x03, 0x00, 0x6e, 0x6d,
				0x00,
				0x0b,
				0x1a,
				0x0b,
			}))),
		)
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		got, err := instance.Invoke("run", 0)
		if err != nil || len(got) != 1 || got[0] != 7 {
			t.Fatalf("run() = %#x, %v; want [7]", got, err)
		}
	})

	t.Run("call-bearing", func(t *testing.T) {
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				[]byte{0x5f, 0x00},
				wasmtest.FuncType([]wasm.ValType{wasm.AnyRef}, []wasm.ValType{wasm.I32}),
				wasmtest.FuncType(nil, []wasm.ValType{wasm.EqRef}),
				wasmtest.FuncType(nil, nil),
			)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(3), wasmtest.ULEB(1))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
			wasmtest.Section(10, wasmtest.Vec(
				wasmtest.Code([]byte{0x0b}),
				wasmtest.Code([]byte{
					0x10, 0x00,
					0x02, 0x02,
					0x20, 0x00,
					0xfb, 0x18, 0x03, 0x00, 0x6e, 0x6d,
					0x00,
					0x0b,
					0x1a, 0x41, 0x01,
					0x0b,
				}),
			)),
		)
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		got, err := instance.Invoke("run", 0)
		if err != nil || len(got) != 1 || got[0] != 1 {
			t.Fatalf("run() = %#x, %v; want [1]", got, err)
		}
	})

	t.Run("v128-mixed", func(t *testing.T) {
		body := []byte{0xfd, 0x0c}
		body = append(body, make([]byte, 16)...)
		body = append(body,
			0x1a,
			0x02, 0x02,
			0x20, 0x00,
			0xfb, 0x18, 0x03, 0x00, 0x6e, 0x6d,
			0x00,
			0x0b,
			0x1a, 0x41, 0x01,
			0x0b,
		)
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				[]byte{0x5f, 0x00},
				wasmtest.FuncType([]wasm.ValType{wasm.AnyRef}, []wasm.ValType{wasm.I32}),
				wasmtest.FuncType(nil, []wasm.ValType{wasm.EqRef}),
			)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
		)
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		got, err := instance.Invoke("run", 0)
		if err != nil || len(got) != 1 || got[0] != 1 {
			t.Fatalf("run() = %#x, %v; want [1]", got, err)
		}
	})

	for _, test := range []struct {
		name string
		body []byte
	}{
		{name: "br_on_cast-nonmatch-falls-through", body: []byte{0x20, 0x00, 0xfb, 0x18, 0x01, 0x00, 0x6e, 0x6d, 0x00, 0x0b}},
		{name: "br_on_cast_fail-match-falls-through", body: []byte{0x20, 0x00, 0xfb, 0x19, 0x03, 0x00, 0x6e, 0x6d, 0x00, 0x0b}},
	} {
		t.Run(test.name, func(t *testing.T) {
			module := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(
					[]byte{0x5f, 0x00},
					wasmtest.FuncType([]wasm.ValType{wasm.AnyRef}, []wasm.ValType{wasm.AnyRef}),
				)),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(test.body))),
			)
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			_, err = instance.Invoke("run", 0)
			var trap *TrapError
			if !errors.As(err, &trap) || trap.Code != TrapUnreachable {
				t.Fatalf("run() error = %v; want %v", err, TrapUnreachable)
			}
		})
	}
}

func TestDraglineRefCastHelper(t *testing.T) {
	requireDraglineCoreV3(t)
	target := TargetNative
	for _, test := range []struct {
		name string
		body []byte
		want uint64
	}{
		{name: "i31", body: []byte{0x41, 0x07, 0xfb, 0x1c, 0xfb, 0x16, 0x6c, 0xfb, 0x1e, 0x0b}, want: 7},
		{name: "nullable-null", body: []byte{0xd0, 0x6e, 0xfb, 0x17, 0x6e, 0xd1, 0x0b}, want: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			module := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(test.body))),
			)
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(target), module)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			got, err := instance.Invoke("run")
			if err != nil || len(got) != 1 || got[0] != test.want {
				t.Fatalf("run() = %#x, %v; want [%d]", got, err, test.want)
			}
		})
	}

	t.Run("nonnullable-null-trap", func(t *testing.T) {
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.AnyRef}))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd0, 0x6e, 0xfb, 0x16, 0x6e, 0x0b}))),
		)
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(target), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		_, err = instance.Invoke("run")
		var trap *TrapError
		if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
			t.Fatalf("run(null) error = %v; want %v", err, TrapCastFailure)
		}
	})

	t.Run("defined-struct", func(t *testing.T) {
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				[]byte{0x5f, 0x00},
				wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
				0xfb, 0x01, 0x00, // struct.new_default 0
				0xfb, 0x16, 0x00, // ref.cast 0
				0xd1, // ref.is_null
				0x0b,
			}))),
		)
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(target), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		got, err := instance.Invoke("run")
		if err != nil || len(got) != 1 || got[0] != 0 {
			t.Fatalf("run() = %#x, %v; want [0]", got, err)
		}
	})
}

func TestDraglineDefinedFunctionRefTestAndCast(t *testing.T) {
	requireDraglineCoreV3(t)
	declarations := append(wasmtest.ULEB(1), 0x03, 0x00) // one declarative funcidx segment
	declarations = append(declarations, wasmtest.Vec(wasmtest.ULEB(0))...)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(
			wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(1), wasmtest.ULEB(1), wasmtest.ULEB(0),
			wasmtest.ULEB(1), wasmtest.ULEB(1), wasmtest.ULEB(1),
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("test", 0, 1),
			wasmtest.ExportEntry("cast", 0, 2),
			wasmtest.ExportEntry("exact", 0, 3),
			wasmtest.ExportEntry("mismatch", 0, 4),
			wasmtest.ExportEntry("abstract", 0, 5),
			wasmtest.ExportEntry("nofunc", 0, 6),
			wasmtest.ExportEntry("abstract_cast", 0, 7),
		)),
		wasmtest.Section(9, declarations),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x0b}),
			wasmtest.Code([]byte{0xd2, 0x00, 0xfb, 0x14, 0x00, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0x00, 0xfb, 0x16, 0x00, 0xd1, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0x00, 0xfb, 0x16, 0x62, 0x00, 0xd1, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0x00, 0xfb, 0x16, 0x02, 0x1a, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0x00, 0xfb, 0x14, 0x70, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0x00, 0xfb, 0x14, 0x73, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0x00, 0xfb, 0x16, 0x70, 0xd1, 0x0b}),
		)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for _, export := range []string{"test", "cast", "exact", "abstract", "nofunc", "abstract_cast"} {
		got, err := instance.Invoke(export)
		if err != nil || len(got) != 1 {
			t.Fatalf("%s() = %#x, %v", export, got, err)
		}
		want := uint64(1)
		if export != "test" && export != "abstract" {
			want = 0
		}
		if got[0] != want {
			t.Fatalf("%s() = %#x, want [%d]", export, got, want)
		}
	}
	_, err = instance.Invoke("mismatch")
	var trap *TrapError
	if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
		t.Fatalf("mismatch() error = %v; want %v", err, TrapCastFailure)
	}
}

func TestDraglineStructNewDefaultPreservesLiveRoot(t *testing.T) {
	requireDraglineCoreV3(t)
	body := []byte{
		0x00,             // no locals
		0xfb, 0x01, 0x00, // first allocation remains live on the operand stack
		0xfb, 0x01, 0x00, 0x1a, // collecting allocation; drop
		0xd1, // ref.is_null
		0x0b,
	}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	status := compiled.GCNativeRootAdmission()
	if !status.Required || !status.Exact || status.Safepoints != 2 || status.MaximumRoots != 1 || status.Reason != "" {
		t.Fatalf("Dragline live-root admission = %#v", status)
	}
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	got, err := instance.Invoke("run")
	if err != nil || len(got) != 1 || got[0] != 0 {
		t.Fatalf("run() = %v, %v; want [0]", got, err)
	}
}

func TestDraglineStructHelperWalksCallerRoots(t *testing.T) {
	requireDraglineCoreV3(t)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x00},
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0xfb, 0x01, 0x00, 0x1a, 0x0b}),
			wasmtest.Code([]byte{
				0xfb, 0x01, 0x00, // caller root
				0x10, 0x00, // callee allocates while caller is parked
				0xd1, 0x0b,
			}),
		)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	status := compiled.GCNativeRootAdmission()
	if !status.Required || !status.Exact || status.Safepoints != 2 || status.Callsites != 1 || status.MaximumRoots != 1 || status.Reason != "" {
		t.Fatalf("Dragline caller-root admission = %#v", status)
	}
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	got, err := instance.Invoke("run")
	if err != nil || len(got) != 1 || got[0] != 0 {
		t.Fatalf("run() = %v, %v; want [0]", got, err)
	}
}

func TestDraglinePublishesExactCollectorRootCallsites(t *testing.T) {
	requireDraglineCoreV3(t)
	importEntry := append(wasmtest.Name("env"), wasmtest.Name("tick")...)
	importEntry = append(importEntry, 0)
	importEntry = append(importEntry, wasmtest.ULEB(0)...)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.AnyRef}, []wasm.ValType{wasm.AnyRef}),
		)),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0x20, 0x00, 0x0b}))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	status := compiled.GCNativeRootAdmission()
	if !status.Required || !status.Exact || status.Callsites != 1 || status.MaximumRoots != 1 || status.Reason != "" {
		t.Fatalf("Dragline root admission = %#v", status)
	}
}

func draglineUnsupportedMemoryInitModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(12, wasmtest.ULEB(1)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x41, 0x00, 0x41, 0x00, 0x41, 0x00,
			0xfc, 0x08, 0x00, 0x00, // memory.init 0 0
			0x0b,
		}))),
		wasmtest.Section(11, wasmtest.Vec([]byte{0x01, 0x00})),
	)
}

func draglineBulkMemoryModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(
			wasmtest.ULEB(0), wasmtest.ULEB(0),
			wasmtest.ULEB(1), wasmtest.ULEB(1),
			wasmtest.ULEB(2), wasmtest.ULEB(3),
		)),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("overlap", 0, 0),
			wasmtest.ExportEntry("fill_run", 0, 1),
			wasmtest.ExportEntry("copy", 0, 2),
			wasmtest.ExportEntry("fill", 0, 3),
			wasmtest.ExportEntry("load8", 0, 4),
			wasmtest.ExportEntry("store8", 0, 5),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{
				0x41, 0x00, 0x41, 0x01, 0x3a, 0x00, 0x00,
				0x41, 0x01, 0x41, 0x02, 0x3a, 0x00, 0x00,
				0x41, 0x02, 0x41, 0x03, 0x3a, 0x00, 0x00,
				0x41, 0x01, 0x41, 0x00, 0x41, 0x03,
				0xfc, 0x0a, 0x00, 0x00, // memory.copy 0 0
				0x41, 0x03, 0x2d, 0x00, 0x00, 0x0b,
			}),
			wasmtest.Code([]byte{
				0x41, 0x0a, 0x41, 0xab, 0x01, 0x41, 0x04,
				0xfc, 0x0b, 0x00, // memory.fill 0
				0x41, 0x0d, 0x2d, 0x00, 0x00, 0x0b,
			}),
			wasmtest.Code([]byte{
				0x20, 0x00, 0x20, 0x01, 0x20, 0x02,
				0xfc, 0x0a, 0x00, 0x00, 0x0b,
			}),
			wasmtest.Code([]byte{
				0x20, 0x00, 0x20, 0x01, 0x20, 0x02,
				0xfc, 0x0b, 0x00, 0x0b,
			}),
			wasmtest.Code([]byte{0x20, 0x00, 0x2d, 0x00, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x3a, 0x00, 0x00, 0x0b}),
		)),
	)
}

func draglineMultiResultCallModule() []byte {
	results := []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64, wasm.I32, wasm.I64}
	callee := []byte{0x41, 0x0b, 0x42, 0x16, 0x43, 0x00, 0x00, 0xc0, 0x3f, 0x44, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x04, 0x40, 0x41, 0x21, 0x42, 0x2c, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, results),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I64}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("callee", 0, 0),
			wasmtest.ExportEntry("caller", 0, 1),
			wasmtest.ExportEntry("consume", 0, 2),
			wasmtest.ExportEntry("indirect", 0, 3),
		)),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(callee),
			wasmtest.Code([]byte{0x10, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x10, 0x00, 0x1a, 0x1a, 0x1a, 0x1a, 0x42, 0x05, 0x7c, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x00, 0x11, 0x00, 0x00, 0x0b}),
		)),
	)
}

func TestDraglineNativeRailMachMultiResultRegisterAndOverflowABI(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), draglineMultiResultCallModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if got := compiled.Compiler(); got != CompilerDragline {
		t.Fatalf("compiler = %s, want dragline", got)
	}
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	want := []uint64{11, 22, uint64(math.Float32bits(1.5)), math.Float64bits(2.5), 33, 44}
	for _, export := range []string{"callee", "caller", "indirect"} {
		got, err := instance.Invoke(export)
		if err != nil {
			t.Fatalf("%s: %v", export, err)
		}
		if len(got) != len(want) {
			t.Fatalf("%s results = %v, want %v", export, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s result %d = %#x, want %#x", export, i, got[i], want[i])
			}
		}
	}
	prepared, err := instance.PrepareFunction("caller")
	if err != nil {
		t.Fatal(err)
	}
	preparedResults, err := prepared.Invoke0()
	if err != nil || len(preparedResults) != len(want) {
		t.Fatalf("prepared caller results = %v, %v; want %v", preparedResults, err, want)
	}
	for i := range want {
		if preparedResults[i] != want[i] {
			t.Fatalf("prepared caller result %d = %#x, want %#x", i, preparedResults[i], want[i])
		}
	}
	got, err := instance.Invoke("consume")
	if err != nil || len(got) != 2 || got[0] != 11 || got[1] != 27 {
		t.Fatalf("consume results = %v, %v; want [11 27]", got, err)
	}
}

func TestDraglineNativeRailMachMultiResultHostCall(t *testing.T) {
	results := []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64, wasm.I32, wasm.I64}
	module := returningImportModule(wasmtest.FuncType(nil, results), []byte{0x00, 0x10, 0x00, 0x0b})
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	want := []uint64{101, 202, uint64(math.Float32bits(3.5)), math.Float64bits(4.5), 505, 606}
	instance, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{"env.f": HostFunc(func(_ HostModule, _ []uint64, results []uint64) {
		copy(results, want)
	})}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	got, err := instance.Invoke("g")
	if err != nil || len(got) != len(want) {
		t.Fatalf("host multi-result = %v, %v; want %v", got, err, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("host result %d = %#x, want %#x", i, got[i], want[i])
		}
	}
}

func TestDraglineNativeRailMachMultiResultCache(t *testing.T) {
	cache := NewFunctionArtifactCache(1 << 20)
	config := NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative).WithFunctionArtifactCache(cache)
	first, err := Compile(config, draglineMultiResultCallModule())
	if err != nil {
		t.Fatal(err)
	}
	first.Close()
	compiled, err := Compile(config, draglineMultiResultCallModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if stats := cache.Stats(); stats.Entries != 4 || stats.Hits != 4 || stats.Misses != 4 {
		t.Fatalf("multi-result cache stats = %#v", stats)
	}
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	got, err := instance.Invoke("caller")
	if err != nil || len(got) != 6 || got[0] != 11 || got[5] != 44 {
		t.Fatalf("cached caller results = %v, %v", got, err)
	}
}

func TestDraglineNativeRailMachMultiValueBlockTypeIndex(t *testing.T) {
	results := []wasm.ValType{wasm.I32, wasm.I64}
	loopBody := []byte{0x01, 0x01, 0x7f, 0x41, 0x03, 0x03, 0x03, 0x41, 0x01, 0x6b, 0x22, 0x00, 0x20, 0x00, 0x0d, 0x00, 0x0b, 0x0b}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, results),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I64, wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("fallthrough", 0, 0),
			wasmtest.ExportEntry("branch", 0, 1),
			wasmtest.ExportEntry("params", 0, 2),
			wasmtest.ExportEntry("loop", 0, 3),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x02, 0x00, 0x41, 0x07, 0x42, 0x09, 0x0b, 0x0b}),
			wasmtest.Code([]byte{0x02, 0x00, 0x41, 0x15, 0x42, 0x16, 0x41, 0x01, 0x0d, 0x00, 0x1a, 0x1a, 0x41, 0x1f, 0x42, 0x20, 0x0b, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x02, 0x01, 0x1a, 0x1a, 0x20, 0x01, 0x20, 0x00, 0x0b, 0x0b}),
			append(wasmtest.ULEB(uint32(len(loopBody))), loopBody...),
		)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for export, want := range map[string][]uint64{"fallthrough": {7, 9}, "branch": {21, 22}} {
		got, err := instance.Invoke(export)
		if err != nil || len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("%s results = %v, %v; want %v", export, got, err, want)
		}
	}
	got, err := instance.Invoke("params", 7, 9)
	if err != nil || len(got) != 2 || got[0] != 9 || got[1] != 7 {
		t.Fatalf("params results = %v, %v; want [9 7]", got, err)
	}
	got, err = instance.Invoke("loop")
	if err != nil || len(got) != 1 || got[0] != 0 {
		t.Fatalf("loop results = %v, %v; want [0]", got, err)
	}
}

func draglineMemoryRoundTripModule(param, result wasm.ValType, storeOpcode, storeAlign, loadOpcode, loadAlign byte) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, param}, []wasm.ValType{result}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0, 0x20, 1, storeOpcode, storeAlign, 0,
			0x20, 0, loadOpcode, loadAlign, 0, 0x0b,
		}))),
	)
}

func TestDraglineCompileInvokeAndArtifactRoundTrip(t *testing.T) {
	wasmBytes := draglineScalarModule([]byte{0x20, 0x00, 0x20, 0x01, 0x7c, 0x42, 0x03, 0x85, 0x0b})
	c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithCompiler(CompilerDragline), wasmBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if got := c.Compiler(); got != CompilerDragline {
		t.Fatalf("compiler = %s, want dragline", got)
	}
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := in.Invoke("mix", I64(10), I64(5))
	in.Close()
	if err != nil {
		t.Fatal(err)
	}
	if got := AsI64(result[0]); got != 12 {
		t.Fatalf("mix(10, 5) = %d, want 12", got)
	}

	artifact, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTrustedArtifact(artifact)
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	if got := loaded.Compiler(); got != CompilerDragline {
		t.Fatalf("round-trip compiler = %s, want dragline", got)
	}
	roundTrip, err := Instantiate(loaded, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer roundTrip.Close()
	result, err = roundTrip.Invoke("mix", I64(10), I64(5))
	if err != nil || AsI64(result[0]) != 12 {
		t.Fatalf("round-trip mix(10, 5) = %v, %v", result, err)
	}
}

func TestDraglineNativeRailMachFinalizerExecutesIntegerSSA(t *testing.T) {
	wasmBytes := draglineScalarModule([]byte{0x20, 0x00, 0x42, 0x07, 0x7c, 0x20, 0x01, 0x42, 0x03, 0x7d, 0x84, 0x0b})
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), wasmBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	result, err := instance.Invoke("mix", I64(10), I64(5))
	if err != nil || len(result) != 1 || AsI64(result[0]) != ((10+7)|(5-3)) {
		t.Fatalf("native RailMach mix = %v, %v", result, err)
	}
}

func TestDraglineNativeRailMachExecutesCompareBranchFusion(t *testing.T) {
	module := draglineUnaryModule(wasm.I32, wasm.I32, []byte{
		0x20, 0x00, 0x41, 0x07, 0x48,
		0x04, 0x7f, 0x41, 0x07, 0x05, 0x41, 0x09, 0x0b,
		0x0b,
	})
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for input, want := range map[int32]int32{3: 7, 8: 9} {
		result, err := instance.Invoke("run", I32(input))
		if err != nil || len(result) != 1 || AsI32(result[0]) != want {
			t.Fatalf("run(%d) = %v, %v; want %d", input, result, err, want)
		}
	}
}

func TestDraglineNativeRailMachExecutesNestedLoopAndLoopIf(t *testing.T) {
	for name, body := range map[string][]byte{
		"loop_if": {
			0x02, 0x40, 0x03, 0x40,
			0x20, 0, 0x45, 0x0d, 1,
			0x20, 0, 0x45, 0x04, 0x40, 0x00, 0x0b,
			0x20, 0, 0x41, 1, 0x6b, 0x21, 0, 0x0c, 0,
			0x0b, 0x0b, 0x20, 0, 0x0b,
		},
		"nested_loop": {
			0x02, 0x40, 0x03, 0x40, 0x02, 0x40, 0x03, 0x40,
			0x20, 0, 0x45, 0x0d, 1,
			0x20, 0, 0x41, 1, 0x6b, 0x21, 0, 0x0c, 0,
			0x0b, 0x0b,
			0x20, 0, 0x45, 0x0d, 1, 0x0c, 0,
			0x0b, 0x0b, 0x20, 0, 0x0b,
		},
	} {
		t.Run(name, func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), draglineUnaryModule(wasm.I32, wasm.I32, body))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			for _, input := range []int32{0, 1, 7, 31} {
				result, err := instance.Invoke("run", I32(input))
				if err != nil || len(result) != 1 || AsI32(result[0]) != 0 {
					t.Fatalf("run(%d) = %v, %v; want 0", input, result, err)
				}
			}
		})
	}
	t.Run("loop_result_if", func(t *testing.T) {
		body := []byte{
			0x03, 0x40,
			0x20, 0, 0x41, 0, 0x47,
			0x04, 0x7f, 0x41, 11, 0x05, 0x41, 22, 0x0b,
			0x0f, 0x0b, 0x00, 0x0b,
		}
		compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), draglineUnaryModule(wasm.I32, wasm.I32, body))
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		for input, want := range map[int32]int32{0: 22, 1: 11} {
			result, err := instance.Invoke("run", I32(input))
			if err != nil || len(result) != 1 || AsI32(result[0]) != want {
				t.Fatalf("run(%d) = %v, %v; want %d", input, result, err, want)
			}
		}
	})
	t.Run("loop_result_if_with_live_prefix", func(t *testing.T) {
		body := []byte{
			0x03, 0x40,
			0x41, 0,
			0x20, 0, 0x41, 1, 0x6a, 0x20, 1, 0x46,
			0x04, 0x7f,
			0x41, 0,
			0x05,
			0x20, 0, 0x41, 2, 0x6a, 0x41, 8, 0x6c,
			0x0b,
			0x36, 2, 0,
			0x41, 0, 0x28, 2, 0, 0x0f,
			0x0b, 0x00, 0x0b,
		}
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
		)
		compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		for _, test := range []struct{ i, n, want int32 }{{0, 1, 0}, {0, 2, 16}, {5, 9, 56}} {
			result, err := instance.Invoke("run", I32(test.i), I32(test.n))
			if err != nil || len(result) != 1 || AsI32(result[0]) != test.want {
				t.Fatalf("run(%d, %d) = %v, %v; want %d", test.i, test.n, result, err, test.want)
			}
		}
	})
	t.Run("loop_carried_local_result_if_store", func(t *testing.T) {
		body := []byte{
			0x02, 0x40, 0x03, 0x40,
			0x20, 0, 0x20, 1, 0x4e, 0x0d, 1,
			0x20, 0, 0x41, 1, 0x6a, 0x41, 8, 0x6c,
			0x20, 0, 0x41, 1, 0x6a, 0x20, 1, 0x46,
			0x04, 0x7f,
			0x41, 0,
			0x05,
			0x20, 0, 0x41, 2, 0x6a, 0x41, 8, 0x6c,
			0x0b,
			0x36, 2, 4,
			0x20, 0, 0x41, 1, 0x6a, 0x21, 0, 0x0c, 0,
			0x0b, 0x0b,
			0x41, 8, 0x28, 2, 4, 0x0b,
		}
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
		)
		compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		for n, want := range map[int32]int32{1: 0, 2: 16, 7: 16} {
			result, err := instance.Invoke("run", I32(0), I32(n))
			if err != nil || len(result) != 1 || AsI32(result[0]) != want {
				t.Fatalf("run(0, %d) = %v, %v; want %d", n, result, err, want)
			}
		}
	})
}

func TestDraglineNativeRailMachExecutesAMD64MemoryFold(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x01, 0x20, 0x00, 0x28, 0x02, 0x00, 0x6a, 0x0b,
		}))),
		wasmtest.Section(11, wasmtest.Vec([]byte{0x00, 0x41, 0x00, 0x0b, 0x04, 0x05, 0x00, 0x00, 0x00})),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	result, err := instance.Invoke("run", I32(0), I32(11))
	if err != nil || len(result) != 1 || AsI32(result[0]) != 16 {
		t.Fatalf("run(0, 11) = %v, %v; want 16", result, err)
	}
	if _, err := instance.Invoke("run", I32(65535), I32(11)); err == nil {
		t.Fatal("out-of-bounds folded load did not trap")
	}
}

func TestDraglineNativeRailMachExecutesGVNElision(t *testing.T) {
	module := draglineUnaryModule(wasm.I32, wasm.I32, []byte{
		0x20, 0x00, 0x41, 0x07, 0x6a, 0x1a,
		0x20, 0x00, 0x41, 0x07, 0x6a,
		0x0b,
	})
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	result, err := instance.Invoke("run", I32(5))
	if err != nil {
		t.Fatal(err)
	}
	if got := AsI32(result[0]); got != 12 {
		t.Fatalf("run(5) = %d, want 12", got)
	}
}

func TestDraglineNativeRailMachIntegerOperations(t *testing.T) {
	for _, test := range []struct {
		name   string
		type_  wasm.ValType
		opcode byte
		lhs    uint64
		rhs    uint64
		want   uint64
	}{
		{"i32_add", wasm.I32, 0x6a, 0xfffffff0, 0x22, 0x12},
		{"i32_sub", wasm.I32, 0x6b, 0x10, 0x22, 0xffffffee},
		{"i32_mul", wasm.I32, 0x6c, 0x10001, 0x10001, 0x20001},
		{"i32_and", wasm.I32, 0x71, 0xf0f0, 0x0ff0, 0x00f0},
		{"i32_or", wasm.I32, 0x72, 0xf000, 0x0ff0, 0xfff0},
		{"i32_xor", wasm.I32, 0x73, 0xffff, 0x0ff0, 0xf00f},
		{"i32_shl", wasm.I32, 0x74, 0x80000001, 1, 0x00000002},
		{"i32_shr_s", wasm.I32, 0x75, 0x80000000, 1, 0xc0000000},
		{"i32_shr_u", wasm.I32, 0x76, 0x80000000, 1, 0x40000000},
		{"i32_rotl", wasm.I32, 0x77, 0x80000001, 1, 0x00000003},
		{"i32_rotr", wasm.I32, 0x78, 0x80000001, 1, 0xc0000000},
		{"i64_add", wasm.I64, 0x7c, ^uint64(0) - 4, 9, 4},
		{"i64_sub", wasm.I64, 0x7d, 3, 9, ^uint64(0) - 5},
		{"i64_mul", wasm.I64, 0x7e, 0x100000001, 0x100000001, 0x200000001},
		{"i64_and", wasm.I64, 0x83, 0xf0f000000000f0f0, 0x0ff0000000000ff0, 0x00f00000000000f0},
		{"i64_or", wasm.I64, 0x84, 0xf000000000000000, 0x0ff000000000000f, 0xfff000000000000f},
		{"i64_xor", wasm.I64, 0x85, 0xffff00000000ffff, 0x0ff0000000000ff0, 0xf00f00000000f00f},
		{"i64_shl", wasm.I64, 0x86, 0x8000000000000001, 1, 0x0000000000000002},
		{"i64_shr_s", wasm.I64, 0x87, 0x8000000000000000, 1, 0xc000000000000000},
		{"i64_shr_u", wasm.I64, 0x88, 0x8000000000000000, 1, 0x4000000000000000},
		{"i64_rotl", wasm.I64, 0x89, 0x8000000000000001, 1, 0x0000000000000003},
		{"i64_rotr", wasm.I64, 0x8a, 0x8000000000000001, 1, 0xc000000000000000},
	} {
		t.Run(test.name, func(t *testing.T) {
			wasmBytes := draglineBinaryModule(test.type_, test.type_, []byte{0x20, 0x00, 0x20, 0x01, test.opcode, 0x0b})
			compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), wasmBytes)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			result, err := instance.Invoke("run", test.lhs, test.rhs)
			if err != nil || len(result) != 1 || result[0] != test.want {
				t.Fatalf("run(%#x, %#x) = %#x, %v; want %#x", test.lhs, test.rhs, result, err, test.want)
			}
		})
	}
}

func TestDraglineNativeRailMachFixedShiftRepair(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x7c, 0x20, 2, 0x86, 0x0b}))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	result, err := instance.Invoke("run", I64(3), I64(4), I64(5))
	if err != nil || len(result) != 1 || result[0] != 224 {
		t.Fatalf("run = %v, %v; want 224", result, err)
	}
}

func TestDraglineNativeRailMachIntegerComparisons(t *testing.T) {
	for _, test := range []struct {
		name   string
		type_  wasm.ValType
		opcode byte
		lhs    uint64
		rhs    uint64
		want   uint64
	}{
		{"i32_eq", wasm.I32, 0x46, 7, 7, 1},
		{"i32_ne", wasm.I32, 0x47, 7, 8, 1},
		{"i32_lt_s", wasm.I32, 0x48, math.MaxUint32, 0, 1},
		{"i32_lt_u", wasm.I32, 0x49, math.MaxUint32, 0, 0},
		{"i32_gt_s", wasm.I32, 0x4a, 8, 7, 1},
		{"i32_gt_u", wasm.I32, 0x4b, math.MaxUint32, 0, 1},
		{"i32_le_s", wasm.I32, 0x4c, math.MaxUint32, 0, 1},
		{"i32_le_u", wasm.I32, 0x4d, 8, 7, 0},
		{"i32_ge_s", wasm.I32, 0x4e, 7, 8, 0},
		{"i32_ge_u", wasm.I32, 0x4f, math.MaxUint32, 0, 1},
		{"i64_eq", wasm.I64, 0x51, 7, 7, 1},
		{"i64_ne", wasm.I64, 0x52, 7, 8, 1},
		{"i64_lt_s", wasm.I64, 0x53, math.MaxUint64, 0, 1},
		{"i64_lt_u", wasm.I64, 0x54, math.MaxUint64, 0, 0},
		{"i64_gt_s", wasm.I64, 0x55, 8, 7, 1},
		{"i64_gt_u", wasm.I64, 0x56, math.MaxUint64, 0, 1},
		{"i64_le_s", wasm.I64, 0x57, math.MaxUint64, 0, 1},
		{"i64_le_u", wasm.I64, 0x58, 8, 7, 0},
		{"i64_ge_s", wasm.I64, 0x59, 7, 8, 0},
		{"i64_ge_u", wasm.I64, 0x5a, math.MaxUint64, 0, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			wasmBytes := draglineBinaryModule(test.type_, wasm.I32, []byte{0x20, 0x00, 0x20, 0x01, test.opcode, 0x0b})
			compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), wasmBytes)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			result, err := instance.Invoke("run", test.lhs, test.rhs)
			if err != nil || len(result) != 1 || result[0] != test.want {
				t.Fatalf("run(%#x, %#x) = %#x, %v; want %#x", test.lhs, test.rhs, result, err, test.want)
			}
		})
	}
}

func TestDraglineNativeRailMachIntegerUnaryAndConversions(t *testing.T) {
	for _, test := range []struct {
		name   string
		param  wasm.ValType
		result wasm.ValType
		opcode byte
		arg    uint64
		want   uint64
	}{
		{"i32_eqz", wasm.I32, wasm.I32, 0x45, 0, 1},
		{"i64_eqz", wasm.I64, wasm.I32, 0x50, 3, 0},
		{"i32_clz", wasm.I32, wasm.I32, 0x67, 0x10, 27},
		{"i32_ctz", wasm.I32, wasm.I32, 0x68, 0x10, 4},
		{"i32_popcnt", wasm.I32, wasm.I32, 0x69, 0xf010, 5},
		{"i64_clz", wasm.I64, wasm.I64, 0x79, 0x10, 59},
		{"i64_ctz", wasm.I64, wasm.I64, 0x7a, 0x10, 4},
		{"i64_popcnt", wasm.I64, wasm.I64, 0x7b, 0xf000000000000011, 6},
		{"i32_wrap_i64", wasm.I64, wasm.I32, 0xa7, 0x1234567887654321, 0x87654321},
		{"i64_extend_i32_s", wasm.I32, wasm.I64, 0xac, math.MaxUint32, math.MaxUint64},
		{"i64_extend_i32_u", wasm.I32, wasm.I64, 0xad, math.MaxUint32, math.MaxUint32},
	} {
		t.Run(test.name, func(t *testing.T) {
			wasmBytes := draglineUnaryModule(test.param, test.result, []byte{0x20, 0, test.opcode, 0x0b})
			compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), wasmBytes)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			result, err := instance.Invoke("run", test.arg)
			if err != nil || len(result) != 1 || result[0] != test.want {
				t.Fatalf("run(%#x) = %#x, %v; want %#x", test.arg, result, err, test.want)
			}
		})
	}
}

func TestDraglineNativeRailMachIntegerDivision(t *testing.T) {
	for _, test := range []struct {
		name   string
		type_  wasm.ValType
		opcode byte
		lhs    uint64
		rhs    uint64
		want   uint64
	}{
		{"i32_div_s", wasm.I32, 0x6d, uint64(uint32(0xfffffff7)), 2, uint64(uint32(0xfffffffc))},
		{"i32_div_u", wasm.I32, 0x6e, math.MaxUint32, 2, 0x7fffffff},
		{"i32_rem_s", wasm.I32, 0x6f, uint64(uint32(0xfffffff7)), 2, uint64(uint32(0xffffffff))},
		{"i32_rem_u", wasm.I32, 0x70, math.MaxUint32, 2, 1},
		{"i64_div_s", wasm.I64, 0x7f, math.MaxUint64 - 8, 2, math.MaxUint64 - 3},
		{"i64_div_u", wasm.I64, 0x80, math.MaxUint64, 2, 0x7fffffffffffffff},
		{"i64_rem_s", wasm.I64, 0x81, math.MaxUint64 - 8, 2, math.MaxUint64},
		{"i64_rem_u", wasm.I64, 0x82, math.MaxUint64, 2, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			wasmBytes := draglineBinaryModule(test.type_, test.type_, []byte{0x20, 0, 0x20, 1, test.opcode, 0x0b})
			compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), wasmBytes)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			result, err := instance.Invoke("run", test.lhs, test.rhs)
			if err != nil || len(result) != 1 || result[0] != test.want {
				t.Fatalf("run(%#x, %#x) = %#x, %v; want %#x", test.lhs, test.rhs, result, err, test.want)
			}
		})
	}
}

func TestDraglineNativeRailMachFixedDivisionRepair(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 2, 0x20, 1, 0x7f, 0x0b}))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	result, err := instance.Invoke("run", I64(20), I64(3), I64(30))
	if err != nil || len(result) != 1 || result[0] != 10 {
		t.Fatalf("run = %v, %v; want 10", result, err)
	}
}

func TestDraglineNativeRailMachCallLive(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("inc", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x20, 0, 0x10, 1, 0x7c, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x42, 1, 0x7c, 0x0b}),
		)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	inc, err := instance.Invoke("inc", I64(20))
	if err != nil || len(inc) != 1 || inc[0] != 21 {
		t.Fatalf("inc = %v, %v; want 21", inc, err)
	}
	result, err := instance.Invoke("run", I64(20))
	if err != nil || len(result) != 1 || result[0] != 41 {
		t.Fatalf("run = %v, %v; want 41", result, err)
	}
}

func TestDraglineNativeRailMachImportedFloatCallLive(t *testing.T) {
	importEntry := append(wasmtest.Name("env"), wasmtest.Name("step")...)
	importEntry = append(importEntry, 0)
	importEntry = append(importEntry, wasmtest.ULEB(0)...)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.F64}, []wasm.ValType{wasm.F64}))),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x20, 0, 0x10, 0, 0xa0, 0x0b}))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{"env.step": HostFunc(func(_ HostModule, params, results []uint64) {
		results[0] = math.Float64bits(math.Float64frombits(params[0]) + 1)
	})}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	result, err := instance.Invoke("run", math.Float64bits(20))
	if err != nil || len(result) != 1 || math.Float64frombits(result[0]) != 41 {
		t.Fatalf("run = %v, %v; want 41", result, err)
	}
}

func TestDraglineNativeRailMachIndirectFloatCallLive(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.F64}, []wasm.ValType{wasm.F64}),
			wasmtest.FuncType([]wasm.ValType{wasm.F64, wasm.I32}, []wasm.ValType{wasm.F64}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x20, 0, 0x20, 1, 0x11, 0, 0, 0xa0, 0x0b}),
		)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	result, err := instance.Invoke("run", math.Float64bits(20), I32(0))
	if err != nil || len(result) != 1 || math.Float64frombits(result[0]) != 40 {
		t.Fatalf("run = %v, %v; want 40", result, err)
	}
}

func TestDraglineNativeRailMachARM64LoadPair(t *testing.T) {
	data := make([]byte, 16)
	binary.LittleEndian.PutUint64(data, 3)
	binary.LittleEndian.PutUint64(data[8:], 5)
	segment := append([]byte{0x00, 0x41, 0x00, 0x0b}, append(wasmtest.ULEB(uint32(len(data))), data...)...)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x29, 3, 0, 0x20, 0, 0x29, 3, 8, 0x7c, 0x0b}))),
		wasmtest.Section(11, wasmtest.Vec(segment)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	result, err := instance.Invoke("run", I32(0))
	if err != nil || len(result) != 1 || result[0] != 8 {
		t.Fatalf("run = %v, %v; want 8", result, err)
	}
}

func TestDraglineNativeRailMachARM64StoreLoadForward(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x37, 3, 0, 0x20, 0, 0x29, 3, 0, 0x0b}))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	result, err := instance.Invoke("run", I32(8), I64(0x123456789abcdef0))
	if err != nil || len(result) != 1 || result[0] != 0x123456789abcdef0 {
		t.Fatalf("run = %v, %v; want stored value", result, err)
	}
}

func TestDraglineNativeRailMachSpill(t *testing.T) {
	params := make([]wasm.ValType, 21)
	args := make([]uint64, len(params))
	body := make([]byte, 0, len(params)*3)
	for index := range params {
		params[index] = wasm.I64
		args[index] = uint64(index + 1)
		body = append(body, 0x20)
		body = append(body, wasmtest.ULEB(uint32(index))...)
	}
	for range len(params) - 1 {
		body = append(body, 0x7c)
	}
	body = append(body, 0x0b)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	result, err := instance.Invoke("run", args...)
	if err != nil || len(result) != 1 || result[0] != 231 {
		t.Fatalf("run = %v, %v; want 231", result, err)
	}
}

func TestDraglineNativeRailMachSpillEdges(t *testing.T) {
	params := make([]wasm.ValType, 22)
	args := make([]uint64, len(params))
	for index := range 21 {
		params[index] = wasm.I64
		args[index] = uint64(index + 1)
	}
	params[21] = wasm.I32
	body := []byte{0x20, 21, 0x04, 0x40}
	appendUpdates := func(delta byte) {
		for index := range 21 {
			body = append(body, 0x20, byte(index), 0x42, delta, 0x7c, 0x21, byte(index))
		}
	}
	appendUpdates(1)
	body = append(body, 0x05)
	appendUpdates(2)
	body = append(body, 0x0b)
	for index := range 21 {
		body = append(body, 0x20, byte(index))
	}
	for range 20 {
		body = append(body, 0x7c)
	}
	body = append(body, 0x0b)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for _, test := range []struct {
		condition uint64
		want      uint64
	}{{1, 252}, {0, 273}} {
		args[21] = test.condition
		result, err := instance.Invoke("run", args...)
		if err != nil || len(result) != 1 || result[0] != test.want {
			t.Fatalf("run(cond=%d) = %v, %v; want %d", test.condition, result, err, test.want)
		}
	}
}

func TestDraglineNativeRailMachRematerialize(t *testing.T) {
	body := make([]byte, 0, 28*10)
	for range 28 {
		body = append(body, 0x44)
		var bits [8]byte
		binary.LittleEndian.PutUint64(bits[:], math.Float64bits(1))
		body = append(body, bits[:]...)
	}
	for range 27 {
		body = append(body, 0xa0)
	}
	body = append(body, 0x0b)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.F64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	result, err := instance.Invoke("run")
	if err != nil || len(result) != 1 || math.Float64frombits(result[0]) != 28 {
		t.Fatalf("run = %v, %v; want 28", result, err)
	}
}

func TestDraglineNativeProfiledBlockLayout(t *testing.T) {
	module := draglineUnaryModule(wasm.I32, wasm.I32, []byte{0x20, 0, 0x04, 0x7f, 0x41, 1, 0x05, 0x41, 2, 0x0b, 0x0b})
	profile := &CompilerProfile{
		Version: 1, ModuleHash: sha256.Sum256(module), Source: "static", Phase: "steady",
		EdgeCounts: []CompilerProfileEdgeCount{{Site: CompilerProfileSite{Function: 0, Offset: 2}, Target: 7, Count: 100}},
	}
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative).WithCompilerProfile(profile), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for _, test := range []struct {
		input int32
		want  uint64
	}{{0, 2}, {1, 1}} {
		result, invokeErr := instance.Invoke("run", I32(test.input))
		if invokeErr != nil || len(result) != 1 || result[0] != test.want {
			t.Fatalf("run(%d) = %v, %v; want %d", test.input, result, invokeErr, test.want)
		}
	}
}

func TestDraglineNativeProfileColdCalleeSaveShrinkWrapping(t *testing.T) {
	body := []byte{0x20, 0x00, 0x04, 0x7e}
	for value := byte(1); value <= 18; value++ {
		body = append(body, 0x20, 0x01, 0x42, value, 0x7c)
	}
	for range 17 {
		body = append(body, 0x7c)
	}
	body = append(body, 0x05, 0x42, 0x00, 0x0b, 0x0b)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	profile := &CompilerProfile{
		Version: 1, ModuleHash: sha256.Sum256(module), Source: "static", Phase: "steady",
		FunctionCounts: []uint64{100},
		EdgeCounts: []CompilerProfileEdgeCount{
			{Site: CompilerProfileSite{Function: 0, Offset: 2}, Target: 4, Count: 0},
			{Site: CompilerProfileSite{Function: 0, Offset: 2}, Target: 112, Count: 100},
		},
	}
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative).WithCompilerProfile(profile), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for _, test := range []struct {
		condition uint64
		value     uint64
		want      uint64
	}{{0, 9, 0}, {1, 9, 333}} {
		result, invokeErr := instance.Invoke("run", test.condition, test.value)
		if invokeErr != nil || len(result) != 1 || result[0] != test.want {
			t.Fatalf("run(%d, %d) = %v, %v; want %d", test.condition, test.value, result, invokeErr, test.want)
		}
	}
}

func TestDraglineNativeProfileMultiBlockColdCalleeSaveShrinkWrapping(t *testing.T) {
	pressure := func(body []byte, values byte) []byte {
		for value := byte(1); value <= values; value++ {
			body = append(body, 0x20, 0x01, 0x42, value, 0x7c)
		}
		for value := byte(1); value < values; value++ {
			body = append(body, 0x7c)
		}
		return body
	}
	body := []byte{0x20, 0x00, 0x04, 0x7e, 0x02, 0x40}
	body = pressure(body, 18)
	body = append(body, 0x21, 0x02, 0x0b)
	body = pressure(body, 17)
	body = append(body, 0x20, 0x02, 0x7c, 0x05, 0x42, 0x00, 0x0b, 0x0b)
	functionCode := append([]byte{0x01, 0x01, 0x7e}, body...)
	functionCode = append(wasmtest.ULEB(uint32(len(functionCode))), functionCode...)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(functionCode)),
	)
	profile := &CompilerProfile{
		Version: 1, ModuleHash: sha256.Sum256(module), Source: "static", Phase: "steady", FunctionCounts: []uint64{100},
		EdgeCounts: []CompilerProfileEdgeCount{
			{Site: CompilerProfileSite{Function: 0, Offset: 2}, Target: 4, Count: 0},
			{Site: CompilerProfileSite{Function: 0, Offset: 2}, Target: 225, Count: 100},
			{Site: CompilerProfileSite{Function: 0, Offset: 4}, Target: 6, Count: 0},
			{Site: CompilerProfileSite{Function: 0, Offset: 117}, Target: 118, Count: 0},
			{Site: CompilerProfileSite{Function: 0, Offset: 224}, Target: 228, Count: 0},
			{Site: CompilerProfileSite{Function: 0, Offset: 227}, Target: 228, Count: 0},
			{Site: CompilerProfileSite{Function: 0, Offset: 228}, Target: 0, Count: 0},
		},
	}
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative).WithCompilerProfile(profile), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for _, test := range []struct {
		condition uint64
		value     uint64
		want      uint64
	}{{0, 9, 0}, {1, 9, 639}} {
		result, invokeErr := instance.Invoke("run", test.condition, test.value)
		if invokeErr != nil || len(result) != 1 || result[0] != test.want {
			t.Fatalf("run(%d, %d) = %v, %v; want %d", test.condition, test.value, result, invokeErr, test.want)
		}
	}
}

func TestDraglineNativeProfiledRecursiveSCC(t *testing.T) {
	body := func(callee byte) []byte {
		return []byte{0x20, 0, 0x45, 0x04, 0x7f, 0x41, 0, 0x05, 0x20, 0, 0x41, 1, 0x6b, 0x10, callee, 0x0b, 0x0b}
	}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body(1)), wasmtest.Code(body(0)))),
	)
	observations := &CompilerProfile{
		Version: 1, ModuleHash: sha256.Sum256(module), Source: "static", Phase: "steady",
		FunctionCounts: []uint64{100, 100},
	}
	cache := NewFunctionArtifactCache(1 << 20)
	config := NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative).WithCompilerProfile(observations).WithFunctionArtifactCache(cache)
	first, err := Compile(config, module)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	compiled, err := Compile(config, module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if stats := cache.Stats(); stats.Entries != 2 || stats.Hits != 2 || stats.Misses != 2 {
		t.Fatalf("profiled recursive cache stats = %#v", stats)
	}
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	result, err := instance.Invoke("run", I32(32))
	if err != nil || len(result) != 1 || result[0] != 0 {
		t.Fatalf("run(32) = %v, %v; want 0", result, err)
	}
}

func TestDraglineNativeRailMachIntegerDivisionTraps(t *testing.T) {
	for _, test := range []struct {
		name   string
		type_  wasm.ValType
		opcode byte
		lhs    uint64
		rhs    uint64
		code   TrapCode
	}{
		{"i32_div_zero", wasm.I32, 0x6e, 1, 0, TrapDivZero},
		{"i64_div_zero", wasm.I64, 0x80, 1, 0, TrapDivZero},
		{"i32_div_overflow", wasm.I32, 0x6d, uint64(uint32(0x80000000)), uint64(uint32(0xffffffff)), TrapDivOverflow},
		{"i64_div_overflow", wasm.I64, 0x7f, uint64(1) << 63, math.MaxUint64, TrapDivOverflow},
	} {
		t.Run(test.name, func(t *testing.T) {
			wasmBytes := draglineBinaryModule(test.type_, test.type_, []byte{0x20, 0, 0x20, 1, test.opcode, 0x0b})
			compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), wasmBytes)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			_, err = instance.Invoke("run", test.lhs, test.rhs)
			var trap *TrapError
			if !errors.As(err, &trap) || trap.Code != test.code {
				t.Fatalf("run(%#x, %#x) error = %v; want trap %v", test.lhs, test.rhs, err, test.code)
			}
		})
	}
}

func TestDraglineNativeRailMachFloatOperations(t *testing.T) {
	f32 := func(value float32) uint64 { return uint64(math.Float32bits(value)) }
	f64 := math.Float64bits
	for _, test := range []struct {
		name   string
		type_  wasm.ValType
		opcode byte
		args   []uint64
		want   uint64
	}{
		{"f32_abs", wasm.F32, 0x8b, []uint64{f32(-1.25)}, f32(1.25)},
		{"f32_neg", wasm.F32, 0x8c, []uint64{f32(1.25)}, f32(-1.25)},
		{"f32_ceil", wasm.F32, 0x8d, []uint64{f32(-1.25)}, f32(-1)},
		{"f32_floor", wasm.F32, 0x8e, []uint64{f32(-1.25)}, f32(-2)},
		{"f32_trunc", wasm.F32, 0x8f, []uint64{f32(-1.75)}, f32(-1)},
		{"f32_nearest", wasm.F32, 0x90, []uint64{f32(2.5)}, f32(2)},
		{"f32_sqrt", wasm.F32, 0x91, []uint64{f32(9)}, f32(3)},
		{"f32_add", wasm.F32, 0x92, []uint64{f32(1.5), f32(2.25)}, f32(3.75)},
		{"f32_sub", wasm.F32, 0x93, []uint64{f32(1.5), f32(2.25)}, f32(-0.75)},
		{"f32_mul", wasm.F32, 0x94, []uint64{f32(1.5), f32(2)}, f32(3)},
		{"f32_div", wasm.F32, 0x95, []uint64{f32(7.5), f32(2.5)}, f32(3)},
		{"f32_min_zero", wasm.F32, 0x96, []uint64{f32(0), f32(float32(math.Copysign(0, -1)))}, f32(float32(math.Copysign(0, -1)))},
		{"f32_max_zero", wasm.F32, 0x97, []uint64{f32(float32(math.Copysign(0, -1))), f32(0)}, f32(0)},
		{"f32_copysign", wasm.F32, 0x98, []uint64{f32(3.5), f32(-1)}, f32(-3.5)},
		{"f64_abs", wasm.F64, 0x99, []uint64{f64(-1.25)}, f64(1.25)},
		{"f64_neg", wasm.F64, 0x9a, []uint64{f64(1.25)}, f64(-1.25)},
		{"f64_ceil", wasm.F64, 0x9b, []uint64{f64(-1.25)}, f64(-1)},
		{"f64_floor", wasm.F64, 0x9c, []uint64{f64(-1.25)}, f64(-2)},
		{"f64_trunc", wasm.F64, 0x9d, []uint64{f64(-1.75)}, f64(-1)},
		{"f64_nearest", wasm.F64, 0x9e, []uint64{f64(2.5)}, f64(2)},
		{"f64_sqrt", wasm.F64, 0x9f, []uint64{f64(9)}, f64(3)},
		{"f64_add", wasm.F64, 0xa0, []uint64{f64(1.5), f64(2.25)}, f64(3.75)},
		{"f64_sub", wasm.F64, 0xa1, []uint64{f64(1.5), f64(2.25)}, f64(-0.75)},
		{"f64_mul", wasm.F64, 0xa2, []uint64{f64(1.5), f64(2)}, f64(3)},
		{"f64_div", wasm.F64, 0xa3, []uint64{f64(7.5), f64(2.5)}, f64(3)},
		{"f64_min_zero", wasm.F64, 0xa4, []uint64{f64(0), f64(math.Copysign(0, -1))}, f64(math.Copysign(0, -1))},
		{"f64_max_zero", wasm.F64, 0xa5, []uint64{f64(math.Copysign(0, -1)), f64(0)}, f64(0)},
		{"f64_copysign", wasm.F64, 0xa6, []uint64{f64(3.5), f64(-1)}, f64(-3.5)},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := []byte{0x20, 0, test.opcode, 0x0b}
			module := draglineUnaryModule(test.type_, test.type_, body)
			if len(test.args) == 2 {
				body = []byte{0x20, 0, 0x20, 1, test.opcode, 0x0b}
				module = draglineBinaryModule(test.type_, test.type_, body)
			}
			compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			result, err := instance.Invoke("run", test.args...)
			if err != nil || len(result) != 1 || result[0] != test.want {
				t.Fatalf("run(%#x) = %#x, %v; want %#x", test.args, result, err, test.want)
			}
		})
	}
}

func TestDraglineCopysignIsBitExact(t *testing.T) {
	for _, target := range []CompilerTargetMode{TargetCompatibility, TargetNative} {
		for _, test := range []struct {
			name   string
			type_  wasm.ValType
			opcode byte
			lhs    uint64
			rhs    uint64
			want   uint64
		}{
			{"f32_negative_zero", wasm.F32, 0x98, 0, 0x80000000, 0x80000000},
			{"f32_nan_payload", wasm.F32, 0x98, 0x7fc12345, 0x80000000, 0xffc12345},
			{"f64_negative_zero", wasm.F64, 0xa6, 0, uint64(1) << 63, uint64(1) << 63},
			{"f64_nan_payload", wasm.F64, 0xa6, 0x7ff8123456789abc, uint64(1) << 63, 0xfff8123456789abc},
		} {
			t.Run(target.String()+"/"+test.name, func(t *testing.T) {
				module := draglineBinaryModule(test.type_, test.type_, []byte{0x20, 0, 0x20, 1, test.opcode, 0x0b})
				compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(target), module)
				if err != nil {
					t.Fatal(err)
				}
				defer compiled.Close()
				instance, err := Instantiate(compiled, InstantiateOptions{})
				if err != nil {
					t.Fatal(err)
				}
				defer instance.Close()
				result, err := instance.Invoke("run", test.lhs, test.rhs)
				if err != nil || len(result) != 1 || result[0] != test.want {
					t.Fatalf("run(%#x, %#x) = %#x, %v; want %#x", test.lhs, test.rhs, result, err, test.want)
				}
			})
		}
	}
}

func TestDraglineNativeRailMachFloatComparisons(t *testing.T) {
	for _, test := range []struct {
		name   string
		type_  wasm.ValType
		opcode byte
		lhs    uint64
		rhs    uint64
		want   uint64
	}{
		{"f32_eq", wasm.F32, 0x5b, uint64(math.Float32bits(1)), uint64(math.Float32bits(1)), 1},
		{"f32_ne_nan", wasm.F32, 0x5c, uint64(math.Float32bits(float32(math.NaN()))), uint64(math.Float32bits(1)), 1},
		{"f32_lt", wasm.F32, 0x5d, uint64(math.Float32bits(-2)), uint64(math.Float32bits(1)), 1},
		{"f32_gt", wasm.F32, 0x5e, uint64(math.Float32bits(2)), uint64(math.Float32bits(1)), 1},
		{"f32_le_nan", wasm.F32, 0x5f, uint64(math.Float32bits(float32(math.NaN()))), uint64(math.Float32bits(1)), 0},
		{"f32_ge", wasm.F32, 0x60, uint64(math.Float32bits(2)), uint64(math.Float32bits(2)), 1},
		{"f64_eq", wasm.F64, 0x61, math.Float64bits(1), math.Float64bits(1), 1},
		{"f64_ne_nan", wasm.F64, 0x62, math.Float64bits(math.NaN()), math.Float64bits(1), 1},
		{"f64_lt", wasm.F64, 0x63, math.Float64bits(-2), math.Float64bits(1), 1},
		{"f64_gt", wasm.F64, 0x64, math.Float64bits(2), math.Float64bits(1), 1},
		{"f64_le_nan", wasm.F64, 0x65, math.Float64bits(math.NaN()), math.Float64bits(1), 0},
		{"f64_ge", wasm.F64, 0x66, math.Float64bits(2), math.Float64bits(2), 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			module := draglineBinaryModule(test.type_, wasm.I32, []byte{0x20, 0, 0x20, 1, test.opcode, 0x0b})
			compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			result, err := instance.Invoke("run", test.lhs, test.rhs)
			if err != nil || len(result) != 1 || result[0] != test.want {
				t.Fatalf("run = %#x, %v; want %#x", result, err, test.want)
			}
		})
	}
}

func TestDraglineNativeRailMachNontrappingConversions(t *testing.T) {
	for _, test := range []struct {
		name   string
		param  wasm.ValType
		result wasm.ValType
		opcode byte
		arg    uint64
		want   uint64
	}{
		{"f32_convert_i32_s", wasm.I32, wasm.F32, 0xb2, uint64(uint32(0xfffffff9)), uint64(math.Float32bits(-7))},
		{"f32_convert_i32_u", wasm.I32, wasm.F32, 0xb3, math.MaxUint32, uint64(math.Float32bits(float32(uint32(math.MaxUint32))))},
		{"f32_convert_i64_s", wasm.I64, wasm.F32, 0xb4, math.MaxUint64 - 6, uint64(math.Float32bits(-7))},
		{"f32_convert_i64_u", wasm.I64, wasm.F32, 0xb5, math.MaxUint64, uint64(math.Float32bits(float32(uint64(math.MaxUint64))))},
		{"f32_demote_f64", wasm.F64, wasm.F32, 0xb6, math.Float64bits(1.25), uint64(math.Float32bits(1.25))},
		{"f64_convert_i32_s", wasm.I32, wasm.F64, 0xb7, uint64(uint32(0xfffffff9)), math.Float64bits(-7)},
		{"f64_convert_i32_u", wasm.I32, wasm.F64, 0xb8, math.MaxUint32, math.Float64bits(float64(uint32(math.MaxUint32)))},
		{"f64_convert_i64_s", wasm.I64, wasm.F64, 0xb9, math.MaxUint64 - 6, math.Float64bits(-7)},
		{"f64_convert_i64_u", wasm.I64, wasm.F64, 0xba, math.MaxUint64, math.Float64bits(float64(uint64(math.MaxUint64)))},
		{"f64_promote_f32", wasm.F32, wasm.F64, 0xbb, uint64(math.Float32bits(1.25)), math.Float64bits(1.25)},
		{"i32_reinterpret_f32", wasm.F32, wasm.I32, 0xbc, 0x81234567, 0x81234567},
		{"i64_reinterpret_f64", wasm.F64, wasm.I64, 0xbd, 0x8123456789abcdef, 0x8123456789abcdef},
		{"f32_reinterpret_i32", wasm.I32, wasm.F32, 0xbe, 0x81234567, 0x81234567},
		{"f64_reinterpret_i64", wasm.I64, wasm.F64, 0xbf, 0x8123456789abcdef, 0x8123456789abcdef},
	} {
		t.Run(test.name, func(t *testing.T) {
			module := draglineUnaryModule(test.param, test.result, []byte{0x20, 0, test.opcode, 0x0b})
			compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			result, err := instance.Invoke("run", test.arg)
			if err != nil || len(result) != 1 || result[0] != test.want {
				t.Fatalf("run(%#x) = %#x, %v; want %#x", test.arg, result, err, test.want)
			}
		})
	}
}

func TestDraglineNativeRailMachTrappingConversions(t *testing.T) {
	for _, test := range []struct {
		name   string
		param  wasm.ValType
		result wasm.ValType
		opcode byte
		arg    uint64
		want   uint64
	}{
		{"i32_trunc_f32_s", wasm.F32, wasm.I32, 0xa8, uint64(math.Float32bits(-7.75)), uint64(uint32(0xfffffff9))},
		{"i32_trunc_f32_u", wasm.F32, wasm.I32, 0xa9, uint64(math.Float32bits(7.75)), 7},
		{"i32_trunc_f64_s", wasm.F64, wasm.I32, 0xaa, math.Float64bits(-7.75), uint64(uint32(0xfffffff9))},
		{"i32_trunc_f64_u", wasm.F64, wasm.I32, 0xab, math.Float64bits(7.75), 7},
		{"i64_trunc_f32_s", wasm.F32, wasm.I64, 0xae, uint64(math.Float32bits(-7.75)), math.MaxUint64 - 6},
		{"i64_trunc_f32_u", wasm.F32, wasm.I64, 0xaf, uint64(math.Float32bits(7.75)), 7},
		{"i64_trunc_f64_s", wasm.F64, wasm.I64, 0xb0, math.Float64bits(-7.75), math.MaxUint64 - 6},
		{"i64_trunc_f64_u", wasm.F64, wasm.I64, 0xb1, math.Float64bits(7.75), 7},
	} {
		t.Run(test.name, func(t *testing.T) {
			module := draglineUnaryModule(test.param, test.result, []byte{0x20, 0, test.opcode, 0x0b})
			compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			result, err := instance.Invoke("run", test.arg)
			if err != nil || len(result) != 1 || result[0] != test.want {
				t.Fatalf("run(%#x) = %#x, %v; want %#x", test.arg, result, err, test.want)
			}
		})
	}
}

func TestDraglineNativeRailMachTrappingConversionTraps(t *testing.T) {
	for _, test := range []struct {
		name   string
		param  wasm.ValType
		result wasm.ValType
		opcode byte
		arg    uint64
	}{
		{"i32_nan", wasm.F32, wasm.I32, 0xa8, uint64(math.Float32bits(float32(math.NaN())))},
		{"i32_overflow", wasm.F64, wasm.I32, 0xaa, math.Float64bits(float64(uint64(1) << 32))},
		{"i64_nan", wasm.F64, wasm.I64, 0xb0, math.Float64bits(math.NaN())},
		{"i64_overflow", wasm.F64, wasm.I64, 0xb1, math.Float64bits(math.Inf(1))},
	} {
		t.Run(test.name, func(t *testing.T) {
			module := draglineUnaryModule(test.param, test.result, []byte{0x20, 0, test.opcode, 0x0b})
			compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			_, err = instance.Invoke("run", test.arg)
			var trap *TrapError
			if !errors.As(err, &trap) || trap.Code != TrapTruncOverflow {
				t.Fatalf("run(%#x) error = %v; want conversion trap", test.arg, err)
			}
		})
	}
}

func TestDraglineNativeRailMachMemoryLoadsAndStores(t *testing.T) {
	for _, test := range []struct {
		name                           string
		param, result                  wasm.ValType
		store, storeAlign, load, align byte
		value, want                    uint64
	}{
		{"i32", wasm.I32, wasm.I32, 0x36, 2, 0x28, 2, 0x89abcdef, 0x89abcdef},
		{"i64", wasm.I64, wasm.I64, 0x37, 3, 0x29, 3, 0x0123456789abcdef, 0x0123456789abcdef},
		{"f32", wasm.F32, wasm.F32, 0x38, 2, 0x2a, 2, uint64(math.Float32bits(-1.25)), uint64(math.Float32bits(-1.25))},
		{"f64", wasm.F64, wasm.F64, 0x39, 3, 0x2b, 3, math.Float64bits(-1.25), math.Float64bits(-1.25)},
		{"i32_load8_s", wasm.I32, wasm.I32, 0x3a, 0, 0x2c, 0, 0x80, 0xffffff80},
		{"i32_load8_u", wasm.I32, wasm.I32, 0x3a, 0, 0x2d, 0, 0x80, 0x80},
		{"i32_load16_s", wasm.I32, wasm.I32, 0x3b, 1, 0x2e, 1, 0x8000, 0xffff8000},
		{"i32_load16_u", wasm.I32, wasm.I32, 0x3b, 1, 0x2f, 1, 0x8000, 0x8000},
		{"i64_load8_s", wasm.I64, wasm.I64, 0x3c, 0, 0x30, 0, 0x80, 0xffffffffffffff80},
		{"i64_load8_u", wasm.I64, wasm.I64, 0x3c, 0, 0x31, 0, 0x80, 0x80},
		{"i64_load16_s", wasm.I64, wasm.I64, 0x3d, 1, 0x32, 1, 0x8000, 0xffffffffffff8000},
		{"i64_load16_u", wasm.I64, wasm.I64, 0x3d, 1, 0x33, 1, 0x8000, 0x8000},
		{"i64_load32_s", wasm.I64, wasm.I64, 0x3e, 2, 0x34, 2, 0x80000000, 0xffffffff80000000},
		{"i64_load32_u", wasm.I64, wasm.I64, 0x3e, 2, 0x35, 2, 0x80000000, 0x80000000},
	} {
		t.Run(test.name, func(t *testing.T) {
			module := draglineMemoryRoundTripModule(test.param, test.result, test.store, test.storeAlign, test.load, test.align)
			compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			result, err := instance.Invoke("run", I32(8), test.value)
			if err != nil || len(result) != 1 || result[0] != test.want {
				t.Fatalf("run = %#x, %v; want %#x", result, err, test.want)
			}
		})
	}
}

func TestDraglineNativeRailMachMemoryTrap(t *testing.T) {
	module := draglineMemoryRoundTripModule(wasm.I64, wasm.I64, 0x37, 3, 0x29, 3)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	_, err = instance.Invoke("run", I32(65535), I64(1))
	var trap *TrapError
	if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds {
		t.Fatalf("run error = %v; want memory bounds trap", err)
	}
}

func TestDraglineNativeRailMachMemorySizeAndGrow(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x01, 0x03})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("grow", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x40, 0, 0x0b}))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for _, test := range []struct {
		delta, want uint64
	}{{0, 1}, {1, 1}, {1, 2}, {1, math.MaxUint32}} {
		result, err := instance.Invoke("grow", test.delta)
		if err != nil || len(result) != 1 || result[0] != test.want {
			t.Fatalf("grow(%d) = %v, %v; want %d", test.delta, result, err, test.want)
		}
	}
}

func TestDraglineNativeRailMachSelect(t *testing.T) {
	for _, typ := range []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64} {
		for _, condition := range []uint64{0, 1} {
			name := typ.String() + "_false"
			if condition != 0 {
				name = typ.String() + "_true"
			}
			t.Run(name, func(t *testing.T) {
				module := wasmtest.Module(
					wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ, typ, wasm.I32}, []wasm.ValType{typ}))),
					wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
					wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
					wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0x1b, 0x0b}))),
				)
				compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
				if err != nil {
					t.Fatal(err)
				}
				defer compiled.Close()
				instance, err := Instantiate(compiled, InstantiateOptions{})
				if err != nil {
					t.Fatal(err)
				}
				defer instance.Close()
				lhs, rhs := uint64(0x11223344), uint64(0x55667788)
				want := rhs
				if condition != 0 {
					want = lhs
				}
				result, err := instance.Invoke("run", lhs, rhs, condition)
				if err != nil || len(result) != 1 || result[0] != want {
					t.Fatalf("select = %v, %v; want %#x", result, err, want)
				}
			})
		}
	}
}

func TestDraglineNativeRailMachGlobals(t *testing.T) {
	for _, test := range []struct {
		name string
		typ  wasm.ValType
		init []byte
		arg  uint64
	}{
		{"i32", wasm.I32, []byte{0x41, 0, 0x0b}, 0x89abcdef},
		{"i64", wasm.I64, []byte{0x42, 0, 0x0b}, 0x0123456789abcdef},
		{"f32", wasm.F32, []byte{0x43, 0, 0, 0, 0, 0x0b}, uint64(math.Float32bits(-1.25))},
		{"f64", wasm.F64, []byte{0x44, 0, 0, 0, 0, 0, 0, 0, 0, 0x0b}, math.Float64bits(-1.25)},
	} {
		t.Run(test.name, func(t *testing.T) {
			module := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{test.typ}, []wasm.ValType{test.typ}))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(test.typ, true, test.init))),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x24, 0, 0x23, 0, 0x0b}))),
			)
			compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			result, err := instance.Invoke("run", test.arg)
			if err != nil || len(result) != 1 || result[0] != test.arg {
				t.Fatalf("run = %v, %v; want %#x", result, err, test.arg)
			}
		})
	}
}

func TestDraglineNativeRailMachControlFlow(t *testing.T) {
	t.Run("fused compare if", func(t *testing.T) {
		module := draglineUnaryModule(wasm.I32, wasm.I32, []byte{0x20, 0, 0x41, 7, 0x48, 0x04, 0x7f, 0x41, 1, 0x05, 0x41, 2, 0x0b, 0x0b})
		compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		for _, test := range []struct{ arg, want uint64 }{{5, 1}, {9, 2}} {
			result, err := instance.Invoke("run", test.arg)
			if err != nil || len(result) != 1 || result[0] != test.want {
				t.Fatalf("run(%d) = %v, %v; want %d", test.arg, result, err, test.want)
			}
		}
	})
	t.Run("fused eqz if", func(t *testing.T) {
		module := draglineUnaryModule(wasm.I32, wasm.I32, []byte{0x20, 0, 0x45, 0x04, 0x7f, 0x41, 1, 0x05, 0x41, 2, 0x0b, 0x0b})
		compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		for _, test := range []struct{ arg, want uint64 }{{0, 1}, {9, 2}} {
			result, err := instance.Invoke("run", test.arg)
			if err != nil || len(result) != 1 || result[0] != test.want {
				t.Fatalf("run(%d) = %v, %v; want %d", test.arg, result, err, test.want)
			}
		}
	})
	t.Run("if", func(t *testing.T) {
		module := draglineUnaryModule(wasm.I32, wasm.I32, []byte{0x20, 0, 0x04, 0x7f, 0x41, 7, 0x05, 0x41, 9, 0x0b, 0x0b})
		compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		for _, test := range []struct{ arg, want uint64 }{{0, 9}, {1, 7}} {
			result, err := instance.Invoke("run", test.arg)
			if err != nil || len(result) != 1 || result[0] != test.want {
				t.Fatalf("run(%d) = %v, %v; want %d", test.arg, result, err, test.want)
			}
		}
	})
	t.Run("loop", func(t *testing.T) {
		module := draglineUnaryModule(wasm.I32, wasm.I32, []byte{
			0x03, 0x40,
			0x20, 0, 0x41, 1, 0x6b, 0x22, 0, 0x0d, 0,
			0x0b, 0x20, 0, 0x0b,
		})
		compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		result, err := instance.Invoke("run", I32(5))
		if err != nil || len(result) != 1 || result[0] != 0 {
			t.Fatalf("run(5) = %v, %v; want 0", result, err)
		}
	})
	t.Run("nested branch", func(t *testing.T) {
		module := draglineUnaryModule(wasm.I32, wasm.I32, []byte{
			0x02, 0x7f,
			0x41, 7, 0x20, 0, 0x0d, 0,
			0x1a, 0x41, 9,
			0x0b, 0x0b,
		})
		compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		for _, test := range []struct{ arg, want uint64 }{{0, 9}, {1, 7}} {
			result, err := instance.Invoke("run", test.arg)
			if err != nil || len(result) != 1 || result[0] != test.want {
				t.Fatalf("run(%d) = %v, %v; want %d", test.arg, result, err, test.want)
			}
		}
	})
	t.Run("early return", func(t *testing.T) {
		module := draglineUnaryModule(wasm.I32, wasm.I32, []byte{
			0x20, 0, 0x04, 0x40, 0x41, 7, 0x0f, 0x0b,
			0x41, 9, 0x0b,
		})
		compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		for _, test := range []struct{ arg, want uint64 }{{0, 9}, {1, 7}} {
			result, err := instance.Invoke("run", test.arg)
			if err != nil || len(result) != 1 || result[0] != test.want {
				t.Fatalf("run(%d) = %v, %v; want %d", test.arg, result, err, test.want)
			}
		}
	})
	t.Run("branch table", func(t *testing.T) {
		module := draglineUnaryModule(wasm.I32, wasm.I32, []byte{
			0x02, 0x40, 0x02, 0x40,
			0x20, 0, 0x0e, 2, 0, 1, 1,
			0x0b, 0x41, 10, 0x0f,
			0x0b, 0x41, 20, 0x0b,
		})
		compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		for _, test := range []struct{ arg, want uint64 }{{0, 10}, {1, 20}, {2, 20}, {99, 20}} {
			result, err := instance.Invoke("run", test.arg)
			if err != nil || len(result) != 1 || result[0] != test.want {
				t.Fatalf("run(%d) = %v, %v; want %d", test.arg, result, err, test.want)
			}
		}
	})
	t.Run("unreachable", func(t *testing.T) {
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x00, 0x0b}))),
		)
		compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		_, err = instance.Invoke("run")
		var trap *TrapError
		if !errors.As(err, &trap) || trap.Code != TrapUnreachable {
			t.Fatalf("run error = %v; want unreachable trap", err)
		}
	})
}

func TestDraglineNativeMixedEmitterFloatCallABI(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.F64}, []wasm.ValType{wasm.F64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x10, 0, 0x0b}),
		)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	want := math.Float64bits(-31.125)
	result, err := instance.Invoke("run", want)
	if err != nil || len(result) != 1 || result[0] != want {
		t.Fatalf("run(%#x) = %v, %v; want unchanged bits", want, result, err)
	}
}

func TestDraglineF64SelectPreservesAllBits(t *testing.T) {
	wasmBytes := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(
			[]wasm.ValType{wasm.F64, wasm.F64, wasm.I32}, []wasm.ValType{wasm.F64},
		))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("choose", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0x1b, 0x0b,
		}))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline), wasmBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	lhs, rhs := math.Float64bits(0.43852322779109514), math.Float64bits(0.85)
	for _, test := range []struct {
		condition int32
		want      uint64
	}{{0, rhs}, {1, lhs}, {-1, lhs}} {
		got, err := instance.Invoke("choose", lhs, rhs, I32(test.condition))
		if err != nil || len(got) != 1 || got[0] != test.want {
			t.Fatalf("choose(%d) = %#x, %v; want %#x", test.condition, got, err, test.want)
		}
	}
}

func TestDraglinePrivateABIUsesCanonicalArgumentsBeyondRegisters(t *testing.T) {
	params := make([]wasm.ValType, 10)
	for i := range params {
		params[i] = wasm.I64
	}
	callerBody := make([]byte, 0, 24)
	for i := range params {
		callerBody = append(callerBody, 0x20, byte(i))
	}
	callerBody = append(callerBody, 0x10, 0x00, 0x0b)
	wasmBytes := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x08, 0x20, 0x09, 0x7c, 0x0b}),
			wasmtest.Code(callerBody),
		)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline), wasmBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	args := []uint64{1, 2, 3, 4, 5, 6, 7, 8, 90, 900}
	got, err := instance.Invoke("run", args...)
	if err != nil || len(got) != 1 || got[0] != 990 {
		t.Fatalf("run = %#x, %v; want 990", got, err)
	}
}

func TestDraglineStructuredConstantMemoryUsesProvedBounds(t *testing.T) {
	segment := append([]byte{0x00, 0x41, 0x00, 0x0b}, append(wasmtest.ULEB(4), 0x78, 0x56, 0x34, 0x12)...)
	wasmBytes := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("read", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x02, 0x7f,
			0x41, 0x00,
			0x41, 0x07,
			0x36, 0x02, 0x00,
			0x41, 0x00,
			0x28, 0x02, 0x00,
			0x0b,
			0x0b,
		}))),
		wasmtest.Section(11, wasmtest.Vec(segment)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline), wasmBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	result, err := instance.Invoke("read")
	if err != nil || len(result) != 1 || uint32(result[0]) != 7 {
		t.Fatalf("read() = %#x, %v; want 7", result, err)
	}
}

func TestDraglineStructuredSIMDLoadColdTrap(t *testing.T) {
	body := []byte{0x20, 0x00, 0xfd, 0x00, 0x04, 0x00, 0xfd, 0xe4, 0x00, 0x69, 0x0b}
	payload := make([]byte, 16)
	for i := range payload {
		payload[i] = 0x80
	}
	segment := append([]byte{0x00, 0x41, 0x00, 0x0b}, append(wasmtest.ULEB(uint32(len(payload))), payload...)...)
	wasmBytes := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("load", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
		wasmtest.Section(11, wasmtest.Vec(segment)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative).WithBoundsChecks(BoundsChecksExplicit), wasmBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	result, err := instance.Invoke("load", I32(0))
	if err != nil || len(result) != 1 || result[0] != 16 {
		t.Fatalf("load(0) = %#x, %v; want 16", result, err)
	}
	_, err = instance.Invoke("load", I32(65536))
	var trap *TrapError
	if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds {
		t.Fatalf("load(65536) = %v; want linear-memory out-of-bounds trap", err)
	}
}

func TestDraglineStructuredSIMDBitmaskNonzero(t *testing.T) {
	body := []byte{
		0x20, 0x00, // local.get 0
		0xfd, 0x00, 0x04, 0x00, // v128.load align=16 offset=0
		0xfd, 0xe4, 0x00, // i8x16.bitmask
		0x41, 0x00, // i32.const 0
		0x47, // i32.ne
		0x0b,
	}
	payload := make([]byte, 32)
	payload[15] = 0x80
	segment := append([]byte{0x00, 0x41, 0x00, 0x0b}, append(wasmtest.ULEB(uint32(len(payload))), payload...)...)
	wasmBytes := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("nonzero", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
		wasmtest.Section(11, wasmtest.Vec(segment)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompiler(CompilerDragline).WithTarget(TargetNative).WithBoundsChecks(BoundsChecksExplicit), wasmBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for _, test := range []struct {
		address uint32
		want    uint64
	}{{0, 1}, {16, 0}} {
		result, err := instance.Invoke("nonzero", I32(int32(test.address)))
		if err != nil || len(result) != 1 || result[0] != test.want {
			t.Fatalf("nonzero(%d) = %#x, %v; want %d", test.address, result, err, test.want)
		}
	}
}

func TestDraglineFrameBackedF64LocalIsZeroedOnEveryCall(t *testing.T) {
	body := []byte{0x01, 0x09, 0x7c, 0x20, 0x00, 0x04, 0x40, 0x44}
	var bits [8]byte
	binary.LittleEndian.PutUint64(bits[:], math.Float64bits(42))
	body = append(body, bits[:]...)
	body = append(body, 0x21, 0x09, 0x0b, 0x20, 0x09, 0x0b)
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	wasmBytes := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.F64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(code)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline), wasmBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	first, err := instance.Invoke("run", I32(1))
	if err != nil || len(first) != 1 || first[0] != math.Float64bits(42) {
		t.Fatalf("dirty call = %#x, %v; want 42", first, err)
	}
	second, err := instance.Invoke("run", I32(0))
	if err != nil || len(second) != 1 || second[0] != 0 {
		t.Fatalf("zeroed call = %#x, %v; want +0", second, err)
	}
}

func TestDraglineRejectsUnsupportedWithoutFallback(t *testing.T) {
	wasmBytes := draglineUnsupportedMemoryInitModule()
	_, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline), wasmBytes)
	var unsupported *DraglineUnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("error = %v, want *DraglineUnsupportedError", err)
	}
	if _, railshotErr := Compile(NewRuntimeConfig(), wasmBytes); railshotErr != nil {
		t.Fatalf("default Railshot failed supported module: %v", railshotErr)
	}
}

func TestDraglineExplicitWholeModuleFallbackUsesRailshot(t *testing.T) {
	wasmBytes := draglineUnsupportedMemoryInitModule()
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithCompilerFallback(CompilerFallbackRailshot), wasmBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if compiled.Compiler() != CompilerRailshot {
		t.Fatalf("fallback compiler = %s, want railshot", compiled.Compiler())
	}
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	results, err := instance.Invoke("run")
	if err != nil || len(results) != 0 {
		t.Fatalf("fallback result = %v, %v", results, err)
	}
}

func TestDraglineBulkMemoryCopyFill(t *testing.T) {
	for _, target := range []CompilerTargetMode{TargetCompatibility, TargetNative} {
		t.Run(target.String(), func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(target), draglineBulkMemoryModule())
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			wantMOPS := target == TargetNative && hostSupportsARM64MOPS()
			if compiled.RequiresARM64MOPS() != wantMOPS {
				t.Fatalf("MOPS requirement = %t, want %t", compiled.RequiresARM64MOPS(), wantMOPS)
			}
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			if got, err := instance.Invoke("overlap"); err != nil || len(got) != 1 || got[0] != 3 {
				t.Fatalf("overlap = %v, %v; want [3]", got, err)
			}
			if got, err := instance.Invoke("fill_run"); err != nil || len(got) != 1 || got[0] != 0xab {
				t.Fatalf("fill = %v, %v; want [171]", got, err)
			}
			if _, err := instance.Invoke("copy", I32(65536), I32(65536), I32(0)); err != nil {
				t.Fatalf("zero-length boundary copy: %v", err)
			}
			if _, err := instance.Invoke("fill", I32(65536), I32(0xaa), I32(0)); err != nil {
				t.Fatalf("zero-length boundary fill: %v", err)
			}
			if _, err := instance.Invoke("store8", I32(100), I32(0x55)); err != nil {
				t.Fatal(err)
			}
			if _, err := instance.Invoke("copy", I32(100), I32(65535), I32(2)); err == nil {
				t.Fatal("out-of-bounds source copy did not trap")
			}
			if got, err := instance.Invoke("load8", I32(100)); err != nil || len(got) != 1 || got[0] != 0x55 {
				t.Fatalf("trapping copy mutated destination: %v, %v", got, err)
			}
			if _, err := instance.Invoke("fill", I32(65535), I32(0xaa), I32(2)); err == nil {
				t.Fatal("out-of-bounds fill did not trap")
			}
		})
	}
}

func TestDraglineBulkMemoryPreservesLiveParameters(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, // local.get destination
			0x20, 0x01, // local.get fill byte
			0x20, 0x02, // local.get length
			0xfc, 0x0b, 0x00, // memory.fill 0
			0x20, 0x00, // the destination remains live across memory.fill
			0x0b,
		}))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	got, err := instance.Invoke("run", I32(123), I32(0xab), I32(1))
	if err != nil || len(got) != 1 || got[0] != 123 {
		t.Fatalf("live destination after memory.fill = %v, %v; want [123]", got, err)
	}
}

func TestDraglineResourceLimitIsTypedAndExplicitlyRecoverable(t *testing.T) {
	local := append(wasmtest.ULEB(4097), byte(0x7f))
	body := append(wasmtest.Vec(local), byte(0x0b))
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	wasmBytes := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(code)),
	)
	base := NewRuntimeConfig().WithMaxFunctionLocals(4098)
	_, err := Compile(base.WithCompiler(CompilerDragline), wasmBytes)
	var limit *DraglineResourceLimitError
	if !errors.As(err, &limit) || limit.Required != 4097 || limit.Limit != 4096 {
		t.Fatalf("Dragline limit error = %#v, %v", limit, err)
	}
	compiled, err := Compile(base.WithCompiler(CompilerDragline).WithCompilerFallback(CompilerFallbackRailshot), wasmBytes)
	if err != nil {
		var unrecovered *DraglineResourceLimitError
		if errors.As(err, &unrecovered) {
			t.Fatalf("explicit fallback returned the original Dragline limit: %v", err)
		}
		// Some Railshot target encodings have a narrower frame-offset bound than
		// this deliberately oversized valid function. A non-Dragline error still
		// proves that the typed limit admitted the explicit fallback attempt.
		return
	}
	defer compiled.Close()
	if compiled.Compiler() != CompilerRailshot {
		t.Fatalf("resource-limit fallback compiler = %s, want railshot", compiled.Compiler())
	}
}

func TestDraglinePublicBoundedFunctionCacheIsOptIn(t *testing.T) {
	wasmBytes := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 7, 0x0b}),
			wasmtest.Code([]byte{0x10, 0, 0x0b}),
		)),
	)
	cache := NewFunctionArtifactCache(1 << 20)
	config := NewRuntimeConfig().WithCompiler(CompilerDragline).WithFunctionArtifactCache(cache)
	first, err := Compile(config, wasmBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Compile(config, wasmBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	stats := cache.Stats()
	if stats.Entries != 2 || stats.Hits != 2 || stats.Misses != 2 || stats.ChargedBytes > stats.MaxBytes {
		t.Fatalf("public function cache stats = %#v", stats)
	}
}

func TestDraglineUnreachableAndEarlyReturn(t *testing.T) {
	t.Run("unreachable", func(t *testing.T) {
		wasmBytes := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x00, 0x0b}))),
		)
		c, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline), wasmBytes)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		in, err := Instantiate(c, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		if _, err := in.Invoke("run"); err == nil {
			t.Fatal("unreachable returned without a trap")
		} else {
			var trap *TrapError
			if !errors.As(err, &trap) || trap.Code != TrapUnreachable {
				t.Fatalf("error = %v, want unreachable trap", err)
			}
		}
	})

	t.Run("return leaves polymorphic tail", func(t *testing.T) {
		wasmBytes := draglineScalarModule([]byte{0x20, 0x01, 0x0f, 0x00, 0x0b})
		c, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline), wasmBytes)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		in, err := Instantiate(c, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		got, err := in.Invoke("mix", I64(11), I64(29))
		if err != nil || len(got) != 1 || AsI64(got[0]) != 29 {
			t.Fatalf("mix(11, 29) = %v, %v; want 29", got, err)
		}
	})
}

func TestDraglineI32TrappingConversions(t *testing.T) {
	tests := []struct {
		name   string
		param  wasm.ValType
		opcode byte
		valid  []struct {
			in   uint64
			want int32
		}
		trapping []uint64
	}{
		{
			name: "f32_s", param: wasm.F32, opcode: 0xa8,
			valid: []struct {
				in   uint64
				want int32
			}{
				{uint64(math.Float32bits(-2147483648)), math.MinInt32},
				{uint64(math.Float32bits(123.75)), 123},
			},
			trapping: []uint64{
				uint64(math.Float32bits(float32(math.Inf(1)))),
				uint64(math.Float32bits(float32(math.NaN()))),
				uint64(math.Float32bits(2147483648)),
				uint64(math.Float32bits(math.Nextafter32(-2147483648, float32(math.Inf(-1))))),
			},
		},
		{
			name: "f32_u", param: wasm.F32, opcode: 0xa9,
			valid: []struct {
				in   uint64
				want int32
			}{
				{uint64(math.Float32bits(-0.75)), 0},
				{uint64(math.Float32bits(4294967040)), -256},
			},
			trapping: []uint64{uint64(math.Float32bits(-1)), uint64(math.Float32bits(4294967296))},
		},
		{
			name: "f64_s", param: wasm.F64, opcode: 0xaa,
			valid: []struct {
				in   uint64
				want int32
			}{
				{math.Float64bits(-2147483648.75), math.MinInt32},
				{math.Float64bits(123.75), 123},
			},
			trapping: []uint64{math.Float64bits(math.NaN()), math.Float64bits(-2147483649), math.Float64bits(2147483648)},
		},
		{
			name: "f64_u", param: wasm.F64, opcode: 0xab,
			valid: []struct {
				in   uint64
				want int32
			}{
				{math.Float64bits(-0.75), 0},
				{math.Float64bits(4294967295.75), -1},
			},
			trapping: []uint64{math.Float64bits(-1), math.Float64bits(4294967296)},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wasmBytes := draglineUnaryModule(tc.param, wasm.I32, []byte{0x20, 0x00, tc.opcode, 0x0b})
			c, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline), wasmBytes)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			for _, valid := range tc.valid {
				got, err := in.Invoke("run", valid.in)
				if err != nil || len(got) != 1 || AsI32(got[0]) != valid.want {
					t.Fatalf("run(%x) = %v, %v; want %d", valid.in, got, err, valid.want)
				}
			}
			for _, input := range tc.trapping {
				_, err := in.Invoke("run", input)
				var trap *TrapError
				if !errors.As(err, &trap) || trap.Code != TrapTruncOverflow {
					t.Fatalf("run(%x) error = %v; want truncation overflow", input, err)
				}
			}
		})
	}
}

func TestDraglineI64ToFloatConversions(t *testing.T) {
	maxU64 := uint64(math.MaxUint64)
	tests := []struct {
		name   string
		opcode byte
		result wasm.ValType
		inputs []uint64
		want   func(uint64) uint64
	}{
		{"f32_s", 0xb4, wasm.F32, []uint64{0, 1, uint64(1) << 63, uint64(math.MaxInt64)}, func(v uint64) uint64 {
			return uint64(math.Float32bits(float32(int64(v))))
		}},
		{"f32_u", 0xb5, wasm.F32, []uint64{0, 1, uint64(math.MaxInt64), maxU64}, func(v uint64) uint64 {
			return uint64(math.Float32bits(float32(v)))
		}},
		{"f64_s", 0xb9, wasm.F64, []uint64{0, 1, uint64(1) << 63, uint64(math.MaxInt64)}, func(v uint64) uint64 {
			return math.Float64bits(float64(int64(v)))
		}},
		{"f64_u", 0xba, wasm.F64, []uint64{0, 1, uint64(math.MaxInt64), maxU64}, func(v uint64) uint64 {
			return math.Float64bits(float64(v))
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wasmBytes := draglineUnaryModule(wasm.I64, tc.result, []byte{0x20, 0x00, tc.opcode, 0x0b})
			c, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline), wasmBytes)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			for _, input := range tc.inputs {
				got, err := in.Invoke("run", input)
				if err != nil || len(got) != 1 || got[0] != tc.want(input) {
					t.Fatalf("run(%x) = %v, %v; want bits %x", input, got, err, tc.want(input))
				}
			}
		})
	}
}

func TestDraglineI64TrappingConversions(t *testing.T) {
	tests := []struct {
		name     string
		param    wasm.ValType
		opcode   byte
		valid    []struct{ in, want uint64 }
		trapping []uint64
	}{
		{
			name: "f32_s", param: wasm.F32, opcode: 0xae,
			valid:    []struct{ in, want uint64 }{{0xdf000000, uint64(1) << 63}, {0x42f78000, 123}},
			trapping: []uint64{0xdf000001, 0x5f000000, uint64(math.Float32bits(float32(math.NaN())))},
		},
		{
			name: "f32_u", param: wasm.F32, opcode: 0xaf,
			valid:    []struct{ in, want uint64 }{{uint64(math.Float32bits(-0.75)), 0}, {0x5f7fffff, 0xffffff0000000000}},
			trapping: []uint64{0xbf800000, 0x5f800000},
		},
		{
			name: "f64_s", param: wasm.F64, opcode: 0xb0,
			valid:    []struct{ in, want uint64 }{{0xc3e0000000000000, uint64(1) << 63}, {math.Float64bits(123.75), 123}},
			trapping: []uint64{0xc3e0000000000001, 0x43e0000000000000, math.Float64bits(math.NaN())},
		},
		{
			name: "f64_u", param: wasm.F64, opcode: 0xb1,
			valid:    []struct{ in, want uint64 }{{math.Float64bits(-0.75), 0}, {0x43efffffffffffff, 0xfffffffffffff800}},
			trapping: []uint64{0xbff0000000000000, 0x43f0000000000000},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wasmBytes := draglineUnaryModule(tc.param, wasm.I64, []byte{0x20, 0x00, tc.opcode, 0x0b})
			c, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline), wasmBytes)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			for _, valid := range tc.valid {
				got, err := in.Invoke("run", valid.in)
				if err != nil || len(got) != 1 || got[0] != valid.want {
					t.Fatalf("run(%x) = %v, %v; want %x", valid.in, got, err, valid.want)
				}
			}
			for _, input := range tc.trapping {
				_, err := in.Invoke("run", input)
				var trap *TrapError
				if !errors.As(err, &trap) || trap.Code != TrapTruncOverflow {
					t.Fatalf("run(%x) error = %v; want truncation overflow", input, err)
				}
			}
		})
	}
}

func TestDraglineI64SaturatingConversions(t *testing.T) {
	tests := []struct {
		name     string
		param    wasm.ValType
		sub      byte
		inputs   []uint64
		expected []uint64
	}{
		{"f32_s", wasm.F32, 4,
			[]uint64{uint64(math.Float32bits(float32(math.NaN()))), uint64(math.Float32bits(float32(math.Inf(1)))), uint64(math.Float32bits(float32(math.Inf(-1)))), uint64(math.Float32bits(123.75))},
			[]uint64{0, math.MaxInt64, uint64(1) << 63, 123}},
		{"f32_u", wasm.F32, 5,
			[]uint64{uint64(math.Float32bits(float32(math.NaN()))), uint64(math.Float32bits(float32(math.Inf(1)))), uint64(math.Float32bits(-1)), uint64(math.Float32bits(123.75))},
			[]uint64{0, math.MaxUint64, 0, 123}},
		{"f64_s", wasm.F64, 6,
			[]uint64{math.Float64bits(math.NaN()), math.Float64bits(math.Inf(1)), math.Float64bits(math.Inf(-1)), math.Float64bits(123.75)},
			[]uint64{0, math.MaxInt64, uint64(1) << 63, 123}},
		{"f64_u", wasm.F64, 7,
			[]uint64{math.Float64bits(math.NaN()), math.Float64bits(math.Inf(1)), math.Float64bits(-1), math.Float64bits(123.75)},
			[]uint64{0, math.MaxUint64, 0, 123}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wasmBytes := draglineUnaryModule(tc.param, wasm.I64, []byte{0x20, 0x00, 0xfc, tc.sub, 0x0b})
			c, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline), wasmBytes)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			for i, input := range tc.inputs {
				got, err := in.Invoke("run", input)
				if err != nil || len(got) != 1 || got[0] != tc.expected[i] {
					t.Fatalf("run(%x) = %v, %v; want %x", input, got, err, tc.expected[i])
				}
			}
		})
	}
}

func TestDraglineGeneralDirectCall(t *testing.T) {
	wasmBytes := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x7d, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x10, 0x00, 0x0b}),
		)),
	)
	c, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline), wasmBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	got, err := in.Invoke("run", I64(50), I64(8))
	if err != nil || len(got) != 1 || AsI64(got[0]) != 42 {
		t.Fatalf("run(50, 8) = %v, %v; want 42", got, err)
	}
}

func TestDraglineExecutesSpilledValues(t *testing.T) {
	body := make([]byte, 0, 32)
	for value := byte(1); value <= 10; value++ {
		body = append(body, 0x42, value)
	}
	for range 9 {
		body = append(body, 0x7c)
	}
	body = append(body, 0x0b)
	wasmBytes := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("sum", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	c, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline), wasmBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	result, err := in.Invoke("sum")
	if err != nil || AsI64(result[0]) != 55 {
		t.Fatalf("sum() = %v, %v", result, err)
	}
}

func TestDraglineStructuredScalarLocalHome(t *testing.T) {
	body := []byte{0x41, 0x01, 0x21, 0x01, 0x02, 0x40, 0x0b, 0x20, 0x01, 0x0b}
	encodedBody := append([]byte{0x01, 0x01, 0x7f}, body...) // one i32 local run
	wasmBytes := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("local", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(encodedBody))), encodedBody...))),
	)
	c, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline), wasmBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	result, err := in.Invoke("local", I32(9))
	if err != nil || AsI32(result[0]) != 1 {
		t.Fatalf("local(9) = %v, %v; want 1", result, err)
	}
}

func TestDraglineStructuredScalarLoop(t *testing.T) {
	body := []byte{
		0x02, 0x40, 0x03, 0x40,
		0x20, 0x00, 0x45, 0x0d, 0x01,
		0x20, 0x00, 0x41, 0x01, 0x6b, 0x21, 0x00,
		0x0c, 0x00, 0x0b, 0x0b,
		0x20, 0x00, 0x0b,
	}
	wasmBytes := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("count", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	c, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline), wasmBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	result, err := in.Invoke("count", I32(9))
	if err != nil || AsI32(result[0]) != 0 {
		t.Fatalf("count(9) = %v, %v; want 0", result, err)
	}
}

func TestDraglineStructuredScalarEqz(t *testing.T) {
	wasmBytes := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("eqz", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x45, 0x0b}))),
	)
	c, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline), wasmBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for _, test := range []struct{ in, want int32 }{{0, 1}, {9, 0}} {
		result, err := in.Invoke("eqz", I32(test.in))
		if err != nil || AsI32(result[0]) != test.want {
			t.Fatalf("eqz(%d) = %v, %v; want %d", test.in, result, err, test.want)
		}
	}
}

func TestDraglineStructuredScalarBrIf(t *testing.T) {
	body := []byte{0x02, 0x40, 0x20, 0x00, 0x0d, 0x00, 0x41, 0x07, 0x21, 0x00, 0x0b, 0x20, 0x00, 0x0b}
	wasmBytes := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("br_if", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	c, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline), wasmBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for _, test := range []struct{ in, want int32 }{{0, 7}, {9, 9}} {
		result, err := in.Invoke("br_if", I32(test.in))
		if err != nil || AsI32(result[0]) != test.want {
			t.Fatalf("br_if(%d) = %v, %v; want %d", test.in, result, err, test.want)
		}
	}
}

func TestDraglineIntegerDivisionTraps(t *testing.T) {
	tests := []struct {
		name string
		op   byte
		a, b int64
		trap bool
		want int64
	}{
		{name: "zero", op: 0x7f, a: 1, b: 0, trap: true},
		{name: "overflow", op: 0x7f, a: -1 << 63, b: -1, trap: true},
		{name: "remainder-overflow", op: 0x81, a: -1 << 63, b: -1, want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wasmBytes := draglineScalarModule([]byte{0x20, 0x00, 0x20, 0x01, test.op, 0x0b})
			c, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline), wasmBytes)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			result, err := in.Invoke("mix", I64(test.a), I64(test.b))
			if test.trap {
				if err == nil {
					t.Fatalf("result = %v, want trap", result)
				}
				return
			}
			if err != nil || AsI64(result[0]) != test.want {
				t.Fatalf("result = %v, %v; want %d", result, err, test.want)
			}
		})
	}
}

func TestDraglineFloatMinMaxSemantics(t *testing.T) {
	tests := []struct {
		name       string
		typ        wasm.ValType
		op         byte
		lhs, rhs   uint64
		want       uint64
		wantNaN    bool
		resultMask uint64
	}{
		{name: "f32-min-positive-negative-zero", typ: wasm.F32, op: 0x96, lhs: 0, rhs: 1 << 31, want: 1 << 31},
		{name: "f32-min-negative-positive-zero", typ: wasm.F32, op: 0x96, lhs: 1 << 31, rhs: 0, want: 1 << 31},
		{name: "f32-max-positive-negative-zero", typ: wasm.F32, op: 0x97, lhs: 0, rhs: 1 << 31, want: 0},
		{name: "f32-max-negative-positive-zero", typ: wasm.F32, op: 0x97, lhs: 1 << 31, rhs: 0, want: 0},
		{name: "f32-first-nan", typ: wasm.F32, op: 0x96, lhs: 0x7fc00001, rhs: uint64(math.Float32bits(1)), wantNaN: true, resultMask: 0xffffffff},
		{name: "f32-second-nan", typ: wasm.F32, op: 0x97, lhs: uint64(math.Float32bits(1)), rhs: 0x7fc00001, wantNaN: true, resultMask: 0xffffffff},
		{name: "f64-min-positive-negative-zero", typ: wasm.F64, op: 0xa4, lhs: 0, rhs: 1 << 63, want: 1 << 63},
		{name: "f64-max-negative-positive-zero", typ: wasm.F64, op: 0xa5, lhs: 1 << 63, rhs: 0, want: 0},
		{name: "f64-first-nan", typ: wasm.F64, op: 0xa4, lhs: 0x7ff8000000000001, rhs: math.Float64bits(1), wantNaN: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			module := draglineBinaryModule(test.typ, test.typ, []byte{0x20, 0, 0x20, 1, test.op, 0x0b})
			compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline), module)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			result, err := instance.Invoke("run", test.lhs, test.rhs)
			if err != nil {
				t.Fatal(err)
			}
			got := result[0]
			if test.resultMask != 0 {
				got &= test.resultMask
			}
			if test.wantNaN {
				if test.typ == wasm.F32 {
					if math.IsNaN(float64(math.Float32frombits(uint32(got)))) {
						return
					}
				} else if math.IsNaN(math.Float64frombits(got)) {
					return
				}
				t.Fatalf("result bits = %#x, want NaN", got)
			}
			if got != test.want {
				t.Fatalf("result bits = %#x, want %#x", got, test.want)
			}
		})
	}
}

func TestDraglineI32TruncSatEdges(t *testing.T) {
	tests := []struct {
		name string
		typ  wasm.ValType
		sub  byte
		arg  uint64
		want uint32
	}{
		{name: "f32-signed-nan", typ: wasm.F32, sub: 0, arg: uint64(math.Float32bits(float32(math.NaN()))), want: 0},
		{name: "f32-signed-positive-overflow", typ: wasm.F32, sub: 0, arg: uint64(math.Float32bits(float32(math.Inf(1)))), want: math.MaxInt32},
		{name: "f32-signed-negative-overflow", typ: wasm.F32, sub: 0, arg: uint64(math.Float32bits(float32(math.Inf(-1)))), want: 1 << 31},
		{name: "f32-unsigned-negative", typ: wasm.F32, sub: 1, arg: uint64(math.Float32bits(-1)), want: 0},
		{name: "f32-unsigned-positive-overflow", typ: wasm.F32, sub: 1, arg: uint64(math.Float32bits(float32(math.Inf(1)))), want: math.MaxUint32},
		{name: "f64-signed-positive-overflow", typ: wasm.F64, sub: 2, arg: math.Float64bits(math.Inf(1)), want: math.MaxInt32},
		{name: "f64-signed-negative-overflow", typ: wasm.F64, sub: 2, arg: math.Float64bits(math.Inf(-1)), want: 1 << 31},
		{name: "f64-unsigned-nan", typ: wasm.F64, sub: 3, arg: math.Float64bits(math.NaN()), want: 0},
		{name: "f64-unsigned-positive-overflow", typ: wasm.F64, sub: 3, arg: math.Float64bits(math.Inf(1)), want: math.MaxUint32},
		{name: "f64-unsigned-in-range", typ: wasm.F64, sub: 3, arg: math.Float64bits(42.875), want: 42},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			module := draglineUnaryModule(test.typ, wasm.I32, []byte{0x20, 0, 0xfc, test.sub, 0x0b})
			compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline), module)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			result, err := instance.Invoke("run", test.arg)
			if err != nil {
				t.Fatal(err)
			}
			if got := uint32(result[0]); got != test.want {
				t.Fatalf("result = %#x, want %#x", got, test.want)
			}
		})
	}
}

func TestDraglineScalarComparisons(t *testing.T) {
	tests := []struct {
		name     string
		typ      wasm.ValType
		op       byte
		lhs, rhs uint64
		want     uint32
	}{
		{name: "i32-eq", typ: wasm.I32, op: 0x46, lhs: 7, rhs: 7, want: 1},
		{name: "i32-ne", typ: wasm.I32, op: 0x47, lhs: 7, rhs: 8, want: 1},
		{name: "i32-lt-s", typ: wasm.I32, op: 0x48, lhs: uint64(uint32(math.MaxUint32)), rhs: 0, want: 1},
		{name: "i32-lt-u", typ: wasm.I32, op: 0x49, lhs: uint64(uint32(math.MaxUint32)), rhs: 0, want: 0},
		{name: "i32-gt-s", typ: wasm.I32, op: 0x4a, lhs: 8, rhs: 7, want: 1},
		{name: "i32-le-u", typ: wasm.I32, op: 0x4d, lhs: 7, rhs: 7, want: 1},
		{name: "i32-ge-s", typ: wasm.I32, op: 0x4e, lhs: 7, rhs: 8, want: 0},
		{name: "i64-lt-s", typ: wasm.I64, op: 0x53, lhs: math.MaxUint64, rhs: 0, want: 1},
		{name: "i64-lt-u", typ: wasm.I64, op: 0x54, lhs: math.MaxUint64, rhs: 0, want: 0},
		{name: "i64-ge-u", typ: wasm.I64, op: 0x5a, lhs: math.MaxUint64, rhs: 0, want: 1},
		{name: "f32-eq", typ: wasm.F32, op: 0x5b, lhs: uint64(math.Float32bits(1)), rhs: uint64(math.Float32bits(1)), want: 1},
		{name: "f32-ne-nan", typ: wasm.F32, op: 0x5c, lhs: uint64(math.Float32bits(float32(math.NaN()))), rhs: uint64(math.Float32bits(1)), want: 1},
		{name: "f32-lt-nan", typ: wasm.F32, op: 0x5d, lhs: uint64(math.Float32bits(float32(math.NaN()))), rhs: uint64(math.Float32bits(1)), want: 0},
		{name: "f32-le", typ: wasm.F32, op: 0x5f, lhs: uint64(math.Float32bits(-2)), rhs: uint64(math.Float32bits(1)), want: 1},
		{name: "f64-gt", typ: wasm.F64, op: 0x64, lhs: math.Float64bits(2), rhs: math.Float64bits(1), want: 1},
		{name: "f64-ge-nan", typ: wasm.F64, op: 0x66, lhs: math.Float64bits(math.NaN()), rhs: math.Float64bits(1), want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			module := draglineBinaryModule(test.typ, wasm.I32, []byte{0x20, 0, 0x20, 1, test.op, 0x0b})
			compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline), module)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			result, err := instance.Invoke("run", test.lhs, test.rhs)
			if err != nil {
				t.Fatal(err)
			}
			if got := uint32(result[0]); got != test.want {
				t.Fatalf("result = %d, want %d", got, test.want)
			}
		})
	}
}

func TestDraglineCopysignAndNop(t *testing.T) {
	tests := []struct {
		name     string
		typ      wasm.ValType
		op       byte
		lhs, rhs uint64
		want     uint64
	}{
		{name: "f32-negative", typ: wasm.F32, op: 0x98, lhs: uint64(math.Float32bits(3.5)), rhs: uint64(math.Float32bits(-1)), want: uint64(math.Float32bits(-3.5))},
		{name: "f32-positive", typ: wasm.F32, op: 0x98, lhs: uint64(math.Float32bits(-3.5)), rhs: uint64(math.Float32bits(1)), want: uint64(math.Float32bits(3.5))},
		{name: "f64-negative", typ: wasm.F64, op: 0xa6, lhs: math.Float64bits(3.5), rhs: math.Float64bits(-1), want: math.Float64bits(-3.5)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			module := draglineBinaryModule(test.typ, test.typ, []byte{0x01, 0x20, 0, 0x20, 1, test.op, 0x0b})
			compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline), module)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			result, err := instance.Invoke("run", test.lhs, test.rhs)
			if err != nil {
				t.Fatal(err)
			}
			got := result[0]
			if test.typ == wasm.F32 {
				got = uint64(uint32(got))
			}
			if got != test.want {
				t.Fatalf("result bits = %#x, want %#x", got, test.want)
			}
		})
	}
}

func TestDraglineNarrowMemoryOperations(t *testing.T) {
	tests := []struct {
		name          string
		valueType     wasm.ValType
		store, load   byte
		value, result uint64
	}{
		{name: "i32-load8-s", valueType: wasm.I32, store: 0x3a, load: 0x2c, value: 0xff, result: math.MaxUint32},
		{name: "i32-load8-u", valueType: wasm.I32, store: 0x3a, load: 0x2d, value: 0xff, result: 0xff},
		{name: "i32-load16-s", valueType: wasm.I32, store: 0x3b, load: 0x2e, value: 0xffff, result: math.MaxUint32},
		{name: "i64-load8-s", valueType: wasm.I64, store: 0x3c, load: 0x30, value: 0xff, result: math.MaxUint64},
		{name: "i64-load16-u", valueType: wasm.I64, store: 0x3d, load: 0x33, value: 0xffff, result: 0xffff},
		{name: "i64-load32-s", valueType: wasm.I64, store: 0x3e, load: 0x34, value: math.MaxUint32, result: math.MaxUint64},
		{name: "i64-load32-u", valueType: wasm.I64, store: 0x3e, load: 0x35, value: math.MaxUint32, result: math.MaxUint32},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := []byte{0x20, 0, 0x20, 1, test.store, 0, 0, 0x20, 0, test.load, 0, 0, 0x0b}
			module := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, test.valueType}, []wasm.ValType{test.valueType}))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
			)
			compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline), module)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			result, err := instance.Invoke("run", 8, test.value)
			if err != nil {
				t.Fatal(err)
			}
			got := result[0]
			if test.valueType == wasm.I32 {
				got = uint64(uint32(got))
			}
			if got != test.result {
				t.Fatalf("result = %#x, want %#x", got, test.result)
			}
		})
	}
}

func TestDraglineMemorySizeGrow(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x01, 0x02})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("grow", 0, 0),
			wasmtest.ExportEntry("size", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x40, 0, 0x0b}),
			wasmtest.Code([]byte{0x3f, 0, 0x0b}),
		)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	invoke := func(name string, args ...uint64) uint32 {
		t.Helper()
		result, err := instance.Invoke(name, args...)
		if err != nil {
			t.Fatal(err)
		}
		return uint32(result[0])
	}
	if got := invoke("size"); got != 1 {
		t.Fatalf("initial size = %d, want 1", got)
	}
	if got := invoke("grow", 1); got != 1 {
		t.Fatalf("grow(1) = %d, want previous size 1", got)
	}
	if got := invoke("size"); got != 2 {
		t.Fatalf("grown size = %d, want 2", got)
	}
	if got := invoke("grow", 1); got != math.MaxUint32 {
		t.Fatalf("grow past max = %#x, want -1", got)
	}
	if got := invoke("size"); got != 2 {
		t.Fatalf("size after failed grow = %d, want 2", got)
	}
}

func TestDraglineModuleInitializationContracts(t *testing.T) {
	t.Run("start-and-global", func(t *testing.T) {
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				wasmtest.FuncType(nil, nil),
				wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
			wasmtest.Section(6, wasmtest.Vec([]byte{0x7f, 0x01, 0x41, 0x00, 0x0b})),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("read", 0, 1))),
			wasmtest.Section(8, wasmtest.ULEB(0)),
			wasmtest.Section(10, wasmtest.Vec(
				wasmtest.Code([]byte{0x41, 0x07, 0x24, 0, 0x0b}),
				wasmtest.Code([]byte{0x23, 0, 0x0b}),
			)),
		)
		compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		result, err := instance.Invoke("read")
		if err != nil || uint32(result[0]) != 7 {
			t.Fatalf("read() = %v, %v; want 7", result, err)
		}
	})

	t.Run("active-data", func(t *testing.T) {
		segment := append([]byte{0x00, 0x41, 0x08, 0x0b}, append(wasmtest.ULEB(1), 0xab)...)
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("read", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x08, 0x2d, 0, 0, 0x0b}))),
			wasmtest.Section(11, wasmtest.Vec(segment)),
		)
		compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		result, err := instance.Invoke("read")
		if err != nil || uint32(result[0]) != 0xab {
			t.Fatalf("read() = %v, %v; want 0xab", result, err)
		}
	})
}

func TestDraglineGeneralCallIndirect(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(2), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x03})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x41, 1, 0x6a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x11, 0, 0, 0x0b}),
		)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("run", I32(41), I32(0)); err != nil || AsI32(got[0]) != 42 {
		t.Fatalf("matching call_indirect = %v, %v; want 42", got, err)
	}
	for _, test := range []struct {
		name  string
		index uint32
		code  TrapCode
	}{
		{name: "wrong-signature", index: 1, code: TrapIndirectWrongSig},
		{name: "null", index: 2, code: TrapIndirectOutOfBounds},
		{name: "out-of-bounds", index: 3, code: TrapIndirectOutOfBounds},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := instance.Invoke("run", I32(41), I32(int32(test.index)))
			var trap *TrapError
			if !errors.As(err, &trap) || trap.Code != test.code {
				t.Fatalf("error = %v, want trap %v", err, test.code)
			}
		})
	}
}

func TestDraglineProfiledIndirectCallGuardAndFallback(t *testing.T) {
	module := draglineProfiledCallIndirectModule(2, 1, 2)
	profile := &CompilerProfile{
		Version: 1, ModuleHash: sha256.Sum256(module), Source: "static", Phase: "steady",
		CallTargets: []CompilerProfileTargetHistogram{{
			Site:    CompilerProfileSite{Function: 0, Offset: 6},
			Targets: []CompilerProfileTargetCount{{Function: 1, Count: 90}, {Function: 2, Count: 10}},
		}},
	}
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative).WithCompilerProfile(profile), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for _, test := range []struct {
		name        string
		index       uint64
		left, right uint64
		want        uint64
	}{
		{name: "profile-hit", index: 0, left: 10, right: 3, want: 13},
		{name: "profile-miss", index: 1, left: 10, right: 3, want: 7},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, invokeErr := instance.Invoke("caller", test.index, test.left, test.right)
			if invokeErr != nil || len(got) != 1 || got[0] != test.want {
				t.Fatalf("caller(%d,%d,%d) = %v, %v; want %d", test.index, test.left, test.right, got, invokeErr, test.want)
			}
		})
	}
	if _, err := instance.Invoke("caller", 2, 10, 3); err == nil {
		t.Fatal("profiled out-of-bounds call_indirect did not trap")
	}
}

func TestDraglineImportedCalls(t *testing.T) {
	consumerBytes := portableFuncImportEntry("env", "add", 0)
	consumerBytes = wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(consumerBytes)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 20, 0x41, 22, 0x10, 0, 0x0b}))),
	)

	t.Run("host", func(t *testing.T) {
		compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative).WithCompilerHostEffects(map[HostImport]HostEffectContract{
			{Module: "env", Name: "add"}: {},
		}), consumerBytes)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{"env.add": HostFunc(func(_ HostModule, params, results []uint64) {
			results[0] = uint64(uint32(params[0]) + uint32(params[1]))
		})}})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		if got, err := instance.Invoke("run"); err != nil || AsI32(got[0]) != 42 {
			t.Fatalf("host import = %v, %v; want 42", got, err)
		}
	})

	t.Run("host-indirect", func(t *testing.T) {
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(2, wasmtest.Vec(portableFuncImportEntry("env", "inc", 0))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
			wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x41, 0, 0x11, 0, 0, 0x0b}))),
		)
		compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		instance, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{"env.inc": HostFunc(func(_ HostModule, params, results []uint64) {
			results[0] = uint64(uint32(params[0]) + 1)
		})}})
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		if got, err := instance.Invoke("run", I32(41)); err != nil || AsI32(got[0]) != 42 {
			t.Fatalf("indirect host import = %v, %v; want 42", got, err)
		}
	})

	t.Run("cross-instance", func(t *testing.T) {
		producerBytes := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("add", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x6a, 0x0b}))),
		)
		producerCode, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline), producerBytes)
		if err != nil {
			t.Fatal(err)
		}
		defer producerCode.Close()
		producer, err := Instantiate(producerCode, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer producer.Close()
		add, err := producer.ExportedFunc("add")
		if err != nil {
			t.Fatal(err)
		}
		consumerCode, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline), consumerBytes)
		if err != nil {
			t.Fatal(err)
		}
		defer consumerCode.Close()
		consumer, err := Instantiate(consumerCode, InstantiateOptions{Imports: Imports{"env.add": add}})
		if err != nil {
			t.Fatal(err)
		}
		defer consumer.Close()
		if got, err := consumer.Invoke("run"); err != nil || AsI32(got[0]) != 42 {
			t.Fatalf("cross-instance import = %v, %v; want 42", got, err)
		}
	})

	t.Run("cross-instance-trap", func(t *testing.T) {
		producerBytes := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("fail", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x00, 0x0b}))),
		)
		producerCode, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline), producerBytes)
		if err != nil {
			t.Fatal(err)
		}
		defer producerCode.Close()
		producer, err := Instantiate(producerCode, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer producer.Close()
		fail, err := producer.ExportedFunc("fail")
		if err != nil {
			t.Fatal(err)
		}
		consumerBytes := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(2, wasmtest.Vec(portableFuncImportEntry("env", "fail", 0))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x0b}))),
		)
		consumerCode, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline), consumerBytes)
		if err != nil {
			t.Fatal(err)
		}
		defer consumerCode.Close()
		consumer, err := Instantiate(consumerCode, InstantiateOptions{Imports: Imports{"env.fail": fail}})
		if err != nil {
			t.Fatal(err)
		}
		defer consumer.Close()
		_, err = consumer.Invoke("run")
		var trap *TrapError
		if !errors.As(err, &trap) || trap.Code != TrapUnreachable {
			t.Fatalf("cross-instance trap = %v, want unreachable", err)
		}
	})
}

func TestRuntimeConfigRejectsUnknownCompiler(t *testing.T) {
	err := NewRuntimeConfig().WithCompiler(CompilerEngine(99)).Validate()
	if err == nil {
		t.Fatal("unknown compiler engine accepted")
	}
}
