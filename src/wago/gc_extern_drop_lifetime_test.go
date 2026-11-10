//go:build linux && (amd64 || arm64) && !tinygo && !wago_guardpage

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func droppedConvertedStructModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00}, // (type $s (struct (field i32)))
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("convert", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, // local.get 0
			0xfb, 0x00, 0x00, // struct.new $s
			0xfb, 0x1b, // extern.convert_any
			0x1a, // drop
			0x0b, // end
		}))),
	)
}

func TestDroppedConvertedObjectsDoNotExhaustTinyHeap(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), droppedConvertedStructModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{
		Profile: GCProfileTiny, TinyHeapBytes: 1024, TinyBlockBytes: 16, TinyCollectEveryAlloc: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for i := 0; i < 128; i++ {
		if _, err := instance.Invoke("convert", I32(int32(i))); err != nil {
			t.Fatalf("dropped conversion %d: %v", i, err)
		}
	}
	if allocated := instance.gc.Stats().Allocations; allocated != 128 {
		t.Fatalf("dropped conversion evaluated %d struct allocations, want 128", allocated)
	}
	if err := instance.CollectGC(); err != nil {
		t.Fatal(err)
	}
	if live := instance.gc.Stats().LiveObjects; live != 0 {
		t.Fatalf("dropped conversions retained %d GC objects", live)
	}
}

func droppedI31ConversionModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("convert", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, // local.get 0
			0xfb, 0x1c, // ref.i31
			0xfb, 0x1b, // extern.convert_any
			0x1a, // drop
			0x0b,
		}))),
	)
}

func TestDroppedI31ConversionDoesNotCreateIdentity(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), droppedI31ConversionModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for i := 0; i < 128; i++ {
		if _, err := instance.Invoke("convert", I32(int32(i))); err != nil {
			t.Fatalf("dropped i31 conversion %d: %v", i, err)
		}
	}
	if conversion := instance.existingGCExternConversionState(); conversion == nil || conversion.count != 0 {
		if conversion == nil {
			t.Fatal("missing extern conversion state")
		}
		t.Fatalf("dropped i31 conversions retained %d identities", conversion.count)
	}
}

func TestDroppedNullConversionDoesNotCreateIdentity(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("convert", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0xd0, 0x6e, // ref.null any
			0xfb, 0x1b, // extern.convert_any
			0x1a, // drop
			0x0b,
		}))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err := instance.Invoke("convert"); err != nil {
		t.Fatal(err)
	}
	if conversion := instance.existingGCExternConversionState(); conversion == nil || conversion.count != 0 {
		if conversion == nil {
			t.Fatal("missing extern conversion state")
		}
		t.Fatalf("dropped null conversion retained %d identities", conversion.count)
	}
}

func TestDroppedConversionPreservesOperandTrap(t *testing.T) {
	body := []byte{
		0x20, 0x00, // local.get 0
		0x41, 0x00, // i32.const 0
		0x6d,       // i32.div_s (traps)
		0xfb, 0x1c, // ref.i31
		0xfb, 0x1b, // extern.convert_any
		0x1a, // drop
		0x0b,
	}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("convert", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err := instance.Invoke("convert", I32(7)); err == nil {
		t.Fatal("dropped conversion suppressed operand divide-by-zero trap")
	}
}
