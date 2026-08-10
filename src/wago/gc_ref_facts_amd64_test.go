//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestGCBoundedLoadForwardingExecutes(t *testing.T) {
	tests := []struct {
		name      string
		composite []byte
		body      []byte
		arg       uint64
	}{
		{
			name:      "immutable-struct-get",
			composite: []byte{0x5f, 0x01, 0x7f, 0x00},
			body: []byte{
				0x02, 0x01, 0x63, 0x00, 0x01, 0x7f,
				0x20, 0x00, 0xfb, 0x00, 0x00, 0x21, 0x01,
				0x20, 0x01, 0xfb, 0x02, 0x00, 0x00, 0x21, 0x02,
				0x20, 0x01, 0xfb, 0x02, 0x00, 0x00,
				0x0b,
			},
			arg: 73,
		},
		{
			name:      "array-len",
			composite: []byte{0x5e, 0x7f, 0x01},
			body: []byte{
				0x02, 0x01, 0x63, 0x00, 0x01, 0x7f,
				0x20, 0x00, 0xfb, 0x07, 0x00, 0x21, 0x01,
				0x20, 0x01, 0xfb, 0x0f, 0x21, 0x02,
				0x20, 0x01, 0xfb, 0x0f,
				0x0b,
			},
			arg: 37,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(tc.composite, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(tc.body))), tc.body...))),
			)
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			for _, profile := range []GCConfig{
				{Profile: GCProfileThroughput, StressNurseryBytes: 64, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true},
				{Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true},
			} {
				instance, err := Instantiate(compiled, InstantiateOptions{GC: profile})
				if err != nil {
					t.Fatal(err)
				}
				got, err := instance.Invoke("run", tc.arg)
				_ = instance.Close()
				if err != nil || len(got) != 1 || got[0] != tc.arg {
					t.Fatalf("run(%d) = %v, %v", tc.arg, got, err)
				}
			}
		})
	}
}

func TestGCKnownArrayBoundsExecutesAndRetainsOutOfBoundsTrap(t *testing.T) {
	inBounds := []byte{
		0x01, 0x01, 0x63, 0x00,
		0x41, 0x01, 0x41, 0x04, 0xfb, 0x06, 0x00, 0x21, 0x00,
		0x20, 0x00, 0x41, 0x02, 0x41, 0x09, 0xfb, 0x0e, 0x00,
		0x20, 0x00, 0x41, 0x02, 0xfb, 0x0b, 0x00, 0x0b,
	}
	outOfBounds := []byte{
		0x01, 0x01, 0x63, 0x00,
		0x41, 0x01, 0x41, 0x04, 0xfb, 0x06, 0x00, 0x21, 0x00,
		0x20, 0x00, 0x41, 0x04, 0xfb, 0x0b, 0x00, 0x0b,
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5e, 0x7f, 0x01},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("in-bounds", 0, 0),
			wasmtest.ExportEntry("out-of-bounds", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(inBounds))), inBounds...),
			append(wasmtest.ULEB(uint32(len(outOfBounds))), outOfBounds...),
		)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("in-bounds"); err != nil || len(got) != 1 || got[0] != 9 {
		t.Fatalf("in-bounds = %v, %v", got, err)
	}
	if got, err := instance.Invoke("out-of-bounds"); err == nil {
		t.Fatalf("out-of-bounds = %v, want trap", got)
	}
}

func TestGCCheckedDeadDynamicArrayPreservesSizeTrapWithoutAllocation(t *testing.T) {
	body := []byte{
		0x00,                               // no locals
		0x20, 0x00, 0xfb, 0x07, 0x00, 0x1a, // array.new_default 0; drop
		0x41, 0x07, 0x0b,
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5e, 0x7f, 0x01},
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	for _, tc := range []struct {
		name  string
		gc    GCConfig
		small uint64
	}{
		{name: "throughput", small: 64},
		{name: "tiny", gc: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 16}, small: 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			instance, err := Instantiate(compiled, InstantiateOptions{GC: tc.gc})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()

			before := instance.gc.Stats()
			got, err := instance.Invoke("run", tc.small)
			after := instance.gc.Stats()
			if err != nil || len(got) != 1 || got[0] != 7 {
				t.Fatalf("small dropped array = %v, %v", got, err)
			}
			if after.Allocations != before.Allocations || after.LiveObjects != before.LiveObjects {
				t.Fatalf("dropped dynamic array allocated: before=%+v after=%+v", before, after)
			}

			before = instance.gc.Stats()
			if got, err = instance.Invoke("run", 1_073_741_817); err == nil {
				t.Fatalf("overflowing dropped array returned %v", got)
			}
			after = instance.gc.Stats()
			if after.Allocations != before.Allocations || after.LiveObjects != before.LiveObjects {
				t.Fatalf("overflowing dropped array changed collector: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestGCNullableFinalParameterRetainsNonNullCast(t *testing.T) {
	callee := []byte{
		0x00,       // no locals
		0x20, 0x00, // local.get 0
		0xfb, 0x16, 0x00, // ref.cast (ref 0)
		0xd1, // ref.is_null
		0x0b,
	}
	caller := []byte{
		0x00,       // no locals
		0xd0, 0x00, // ref.null 0
		0x10, 0x00, // call nullable-parameter callee
		0x0b,
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x00},
			[]byte{0x60, 0x01, 0x63, 0x00, 0x01, 0x7f}, // (ref null 0) -> i32
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(callee))), callee...),
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
		)),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("run"); err == nil {
		t.Fatalf("run = %v, want cast failure from nullable parameter", got)
	} else if trap, ok := err.(*TrapError); !ok || trap.Code != TrapCastFailure {
		t.Fatalf("run trap = %v, want %v", err, TrapCastFailure)
	}
}

func TestGCExactReferenceFactClearsOnLocalSet(t *testing.T) {
	body := []byte{
		0x01, 0x01, 0x63, 0x00, // one (ref null 0) local
		0xfb, 0x01, 0x00, 0x21, 0x00, // exact non-null constructor -> local 0
		0xd0, 0x00, 0x21, 0x00, // overwrite local 0 with null
		0x20, 0x00, 0xfb, 0x16, 0x00, // non-null ref.cast must still trap
		0x1a,
		0x41, 0x07,
		0x0b,
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("run"); err == nil {
		t.Fatalf("run = %v, want null cast trap", got)
	}
}
