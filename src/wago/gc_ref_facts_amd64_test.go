//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
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

func TestGCCheckedDeadDynamicArrayPreservesAllocationAndSizeTraps(t *testing.T) {
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
			if after.Allocations != before.Allocations+1 || after.LiveObjects != before.LiveObjects+1 {
				t.Fatalf("dropped dynamic array did not preserve allocation state: before=%+v after=%+v", before, after)
			}

			before = instance.gc.Stats()
			if got, err = instance.Invoke("run", 1_073_741_817); err == nil {
				t.Fatalf("overflowing dropped array returned %v", got)
			}
			after = instance.gc.Stats()
			if after.Allocations != before.Allocations {
				t.Fatalf("overflowing dropped array allocated: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestGCDeadReferenceUniformArrayRetainsAllocation(t *testing.T) {
	body := []byte{
		0x00,
		0xd0, 0x00,
		0x41, 0x01,
		0xfb, 0x06, 0x01,
		0x1a,
		0x41, 0x07,
		0x0b,
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x00},
			[]byte{0x5e, 0x63, 0x00, 0x01},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	for _, cfg := range []GCConfig{
		{},
		{Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 16},
	} {
		instance, err := Instantiate(compiled, InstantiateOptions{GC: cfg})
		if err != nil {
			t.Fatal(err)
		}
		before := instance.gc.Stats()
		got, invokeErr := instance.Invoke("run")
		after := instance.gc.Stats()
		_ = instance.Close()
		if invokeErr != nil || len(got) != 1 || got[0] != 7 {
			t.Fatalf("dropped reference array = %v, %v", got, invokeErr)
		}
		if after.Allocations != before.Allocations+1 {
			t.Fatalf("dropped reference array allocation count: before=%+v after=%+v", before, after)
		}
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

func TestGCLoopParameterDropsFirstEntryConstructorFacts(t *testing.T) {
	body := []byte{
		0x01, 0x01, 0x7f, // local 0: iteration flag
		0xfb, 0x01, 0x00, // first loop argument: struct.new_default 0
		0x03, 0x01, // loop (type 1): (param (ref null 0))
		0xfb, 0x16, 0x00, 0x1a, // non-null cast; drop
		0x20, 0x00, // local.get flag
		0x04, 0x40, // if
		0x41, 0x07, 0x0f, // return 7 if a null backedge incorrectly passed
		0x0b,
		0x41, 0x01, 0x21, 0x00, // flag = 1
		0xd0, 0x00, // backedge argument: ref.null 0
		0x0c, 0x00, // br loop
		0x0b,
		0x41, 0x00, // validator fallthrough result
		0x0b,
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x00},
			[]byte{0x60, 0x01, 0x63, 0x00, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
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
		t.Fatalf("loop returned %v, want null cast trap on second iteration", got)
	} else if trap, ok := err.(*TrapError); !ok || trap.Code != TrapCastFailure {
		t.Fatalf("loop trap = %v, want %v", err, TrapCastFailure)
	}
}

func TestGCLoopDeclaredFactsDoNotOverwriteNullParameterValue(t *testing.T) {
	body := []byte{
		0x00,
		0xd0, 0x00, // ref.null 0
		0x03, 0x01, // loop (type 1): (ref null 0) -> i32
		0xd1, // ref.is_null
		0x0b,
		0x0b,
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x00},
			[]byte{0x60, 0x01, 0x63, 0x00, 0x01, 0x7f},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
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
	if got, err := instance.Invoke("run"); err != nil || len(got) != 1 || got[0] != 1 {
		t.Fatalf("null loop parameter = %v, %v; want [1]", got, err)
	}
}

func TestGCVersionedLoopRestoresFactsBeforeSlowBody(t *testing.T) {
	body := []byte{
		0x00,       // no locals
		0x03, 0x40, // loop void
		0x20, 0x01, 0xfb, 0x16, 0x00, 0x1a, // nullable param cast must execute
		0xfb, 0x01, 0x00, 0x21, 0x01, // then overwrite the loop-modified ref local
	}
	for range 4 {
		body = append(body, 0x20, 0x00, 0x28, 0x02, 0x00, 0x1a) // invariant-base i32.load; drop
	}
	body = append(body,
		0x0b,
		0x41, 0x07,
		0x0b,
	)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x00},
			[]byte{0x60, 0x02, 0x7f, 0x63, 0x00, 0x01, 0x7f}, // (i32, ref null 0) -> i32
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
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
	if got, err := instance.Invoke("run", 100000, 0); err == nil {
		t.Fatalf("slow version returned %v, want cast trap", got)
	} else if trap, ok := err.(*TrapError); !ok || trap.Code != TrapCastFailure {
		t.Fatalf("slow version trap = %v, want cast before memory OOB (%v)", err, TrapCastFailure)
	}
}

func TestGCInlineDeclaredLocalFactsResetWithPhysicalZeroing(t *testing.T) {
	callee := []byte{
		0x01, 0x01, 0x63, 0x00, // local 1: (ref null 0)
		0x20, 0x00, 0x04, 0x40, // if param
		0xfb, 0x01, 0x00, 0x21, 0x01, // local 1 = new struct
		0x0b,
		0x20, 0x01, 0xfb, 0x16, 0x00, 0x1a, // cast local 1 non-null; drop
		0x41, 0x07,
		0x0b,
	}
	caller := []byte{
		0x00,
		0x41, 0x01, 0x10, 0x00, 0x1a, // first inline call leaves an exact slot fact
		0x41, 0x00, 0x10, 0x00, // second call physically zeroes and must clear it
		0x0b,
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x00},
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
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
		t.Fatalf("second inline call returned %v, want null cast trap", got)
	} else if trap, ok := err.(*TrapError); !ok || trap.Code != TrapCastFailure {
		t.Fatalf("second inline call trap = %v, want %v", err, TrapCastFailure)
	}
}

type gcFactDifferentialOutcome struct {
	Name    string   `json:"name"`
	Results []uint64 `json:"results,omitempty"`
	Trap    uint32   `json:"trap,omitempty"`
}

func runGCRefFactDifferentialModule(t *testing.T, name string, data []byte, args ...uint64) gcFactDifferentialOutcome {
	t.Helper()
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatalf("%s compile: %v", name, err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("%s instantiate: %v", name, err)
	}
	defer instance.Close()
	results, err := instance.Invoke("run", args...)
	out := gcFactDifferentialOutcome{Name: name, Results: results}
	if err == nil {
		return out
	}
	trap, ok := err.(*TrapError)
	if !ok {
		t.Fatalf("%s non-trap error: %v", name, err)
	}
	out.Results = nil
	out.Trap = uint32(trap.Code)
	return out
}

func gcNoBarrierArraySetBoundsTrapModule() []byte {
	body := []byte{
		0x01, 0x01, 0x63, 0x00, // one (ref null 0) local
		0x41, 0x01, 0xfb, 0x07, 0x00, 0x21, 0x00, // one-element default array
		0x20, 0x00, 0x41, 0x01, 0xd0, 0x6d, 0xfb, 0x0e, 0x00, // array[1] = ref.null eq: OOB
		0x41, 0x00, 0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5e, 0x6d, 0x01}, // (array (mut eqref))
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestGCDirectNoBarrierArraySetUsesBuiltinBoundsTrap(t *testing.T) {
	out := runGCRefFactDifferentialModule(t, "no-barrier-array-set-bounds", gcNoBarrierArraySetBoundsTrapModule())
	if out.Trap != uint32(TrapBuiltin) {
		t.Fatalf("array.set trap = %d, want %d", out.Trap, TrapBuiltin)
	}
}

func gcDirectScalarArrayBoundsTrapModule(set bool) []byte {
	body := []byte{
		0x01, 0x01, 0x63, 0x00, // one (ref null 0) local
		0x41, 0x01, 0xfb, 0x07, 0x00, 0x21, 0x00,
		0x20, 0x00, 0x41, 0x01,
	}
	if set {
		body = append(body, 0x41, 0x07, 0xfb, 0x0e, 0x00, 0x41, 0x00)
	} else {
		body = append(body, 0xfb, 0x0b, 0x00)
	}
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5e, 0x7f, 0x01}, // (array (mut i32))
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestGCDirectScalarArrayBoundsUseBuiltinTrap(t *testing.T) {
	for _, set := range []bool{false, true} {
		name := "get"
		if set {
			name = "set"
		}
		t.Run(name, func(t *testing.T) {
			out := runGCRefFactDifferentialModule(t, "scalar-array-"+name+"-bounds", gcDirectScalarArrayBoundsTrapModule(set))
			if out.Trap != uint32(TrapBuiltin) {
				t.Fatalf("array.%s trap = %d, want %d", name, out.Trap, TrapBuiltin)
			}
		})
	}
}

func gcDirectScalarDirtyHostIndexModule() []byte {
	hostType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	runType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("idx")...), 0x00, 0x03)
	getBody := []byte{
		0x01, 0x01, 0x63, 0x00,
		0x41, 0x25, 0x41, 0x01, 0xfb, 0x06, 0x00, 0x21, 0x00,
		0x20, 0x00, 0x10, 0x00, 0xfb, 0x0b, 0x00, 0x0b,
	}
	setBody := []byte{
		0x01, 0x01, 0x63, 0x00,
		0x41, 0x00, 0x41, 0x01, 0xfb, 0x06, 0x00, 0x21, 0x00,
		0x20, 0x00, 0x10, 0x00, 0x41, 0x2d, 0xfb, 0x0e, 0x00,
		0x20, 0x00, 0x41, 0x00, 0xfb, 0x0b, 0x00, 0x0b,
	}
	refSetBody := []byte{
		0x01, 0x01, 0x63, 0x02,
		0xd0, 0x01, 0x41, 0x01, 0xfb, 0x06, 0x02, 0x21, 0x00,
		0x20, 0x00, 0x10, 0x00, 0xd0, 0x01, 0xfb, 0x0e, 0x02,
		0x41, 0x01, 0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5e, 0x7f, 0x01},
			[]byte{0x5f, 0x00},
			[]byte{0x5e, 0x63, 0x01, 0x01},
			hostType,
			runType,
		)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(4), wasmtest.ULEB(4), wasmtest.ULEB(4))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("get", 0, 1),
			wasmtest.ExportEntry("set", 0, 2),
			wasmtest.ExportEntry("ref_set", 0, 3),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(getBody))), getBody...),
			append(wasmtest.ULEB(uint32(len(setBody))), setBody...),
			append(wasmtest.ULEB(uint32(len(refSetBody))), refSetBody...),
		)),
	)
}

func TestGCDirectScalarArrayCanonicalizesDirtyHostI32Index(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcDirectScalarDirtyHostIndexModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{"env.idx": HostFunc(func(_ HostModule, _, results []uint64) {
		results[0] = 0xffff_ffff_0000_0000
	})}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for name, want := range map[string]uint64{"get": 37, "set": 45, "ref_set": 1} {
		got, err := in.Invoke(name)
		if err != nil || len(got) != 1 || got[0] != want {
			t.Fatalf("%s = %v, %v; want [%d]", name, got, err, want)
		}
	}
}

func gcExactRefNullModule() []byte {
	body := []byte{0x00, 0xd0, 0x62, 0x00, 0xd1, 0x0b} // ref.null exact 0; ref.is_null
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestGCExactRefNullConsumesIndexedHeapImmediate(t *testing.T) {
	out := runGCRefFactDifferentialModule(t, "exact-ref-null", gcExactRefNullModule())
	if fmt.Sprint(out.Results) != "[1]" || out.Trap != 0 {
		t.Fatalf("exact ref.null = %+v, want [1]", out)
	}
}

func gcExactDefinedCastModule(exact bool) []byte {
	target := []byte{0x00}
	if exact {
		target = []byte{0x62, 0x00}
	}
	body := []byte{
		0x01, 0x01, 0x63, 0x6e, // one (ref null any) local
		0xfb, 0x01, 0x01, 0x21, 0x00, // subtype-1 object into broad local
		0x20, 0x00, 0xfb, 0x16, // ref.cast
	}
	body = append(body, target...)
	body = append(body, 0x1a, 0x41, 0x01, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x50, 0x00, 0x5f, 0x00},       // open type 0
			[]byte{0x4f, 0x01, 0x00, 0x5f, 0x00}, // final type 1 <: 0
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func gcEquivalentExactDefinedCastModule() []byte {
	body := []byte{
		0x01, 0x01, 0x63, 0x6e,
		0xfb, 0x01, 0x01, 0x21, 0x00,
		0x20, 0x00, 0xfb, 0x16, 0x62, 0x00, 0x1a,
		0x41, 0x01, 0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x00},
			[]byte{0x5f, 0x00}, // structurally equivalent exact type
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestEquivalentExactTypesSharePrivateAndRuntimeDomainIdentity(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcEquivalentExactDefinedCastModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	store := newReferenceStore(false)
	defer store.closeRuntime()
	first, err := instantiateCore(compiled, InstantiateOptions{store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := instantiateCore(compiled, InstantiateOptions{store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if first.gc != second.gc {
		t.Fatal("equivalent duplicate local types prevented Runtime-domain reuse")
	}
	for _, in := range []*Instance{first, second} {
		zero, ok0 := in.gcDomainType(0)
		one, ok1 := in.gcDomainType(1)
		if !ok0 || !ok1 || zero != one {
			t.Fatalf("equivalent local type domains = %d/%v and %d/%v", zero, ok0, one, ok1)
		}
	}
}

func TestGCExactDefinedCastRejectsOnlyProperSubtype(t *testing.T) {
	if out := runGCRefFactDifferentialModule(t, "nonexact-defined-cast", gcExactDefinedCastModule(false)); fmt.Sprint(out.Results) != "[1]" || out.Trap != 0 {
		t.Fatalf("nonexact cast = %+v, want [1]", out)
	}
	if out := runGCRefFactDifferentialModule(t, "exact-defined-cast", gcExactDefinedCastModule(true)); out.Trap != uint32(TrapCastFailure) {
		t.Fatalf("exact proper-subtype cast = %+v, want cast failure", out)
	}
	if out := runGCRefFactDifferentialModule(t, "equivalent-exact-defined-cast", gcEquivalentExactDefinedCastModule()); fmt.Sprint(out.Results) != "[1]" || out.Trap != 0 {
		t.Fatalf("equivalent exact cast = %+v, want [1]", out)
	}
}

func gcCatchEdgeFactModule() []byte {
	structType := []byte{0x5f, 0x00}
	tagType := wasmtest.FuncType(nil, nil)
	runType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	body := []byte{
		0x01, 0x01, 0x63, 0x00, // local $x (ref null 0)
		0x02, 0x40, // block $out
		0x20, 0x00, 0x04, 0x40, // if $normal
		0xfb, 0x01, 0x00, 0x21, 0x01, 0x0c, 0x01, // x = struct.new_default; br $out
		0x0b,
		0x1f, 0x40, 0x01, byte(wasm.CatchAll), 0x00, // try_table (catch_all $out)
		0x08, 0x00, // throw tag 0
		0x0b,
		0x00, // unreachable normal fallthrough
		0x0b,
		0x20, 0x01, 0xfb, 0x16, 0x00, 0x1a, // non-null cast must trap on catch edge
		0x41, 0x01, 0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, tagType, runType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(13, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func gcCatchEdgePostEntryMutationModule() []byte {
	body := []byte{
		0x01, 0x01, 0x63, byte(wasm.HeapAny), // local $x (ref null any)
		0x42, 0x00, 0xfb, 0x00, 0x00, 0x21, 0x00, // x = struct.new $A(i64.const 0)
		0x02, 0x40, // block $out
		0x1f, 0x40, 0x01, byte(wasm.CatchAll), 0x00, // try_table (catch_all $out)
		0x41, 0x07, 0xfb, 0x00, 0x01, 0x21, 0x00, // x = struct.new $B(7)
		0x08, 0x00, // throw tag 0
		0x0b,
		0x00, // unreachable normal fallthrough
		0x0b,
		0x20, 0x00, 0xfb, 0x16, 0x00, 0x1a, // cast $A must inspect post-entry x
		0x41, 0x01, 0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7e, 0x00},
			[]byte{0x5f, 0x01, 0x7f, 0x01},
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(3))),
		wasmtest.Section(13, wasmtest.Vec([]byte{0x00, 0x02})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func gcLoopAbstractHeapFactModule(source wasm.AbsHeapType, cast bool) []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	loopType := []byte{0x60, 0x01, 0x64, byte(source), 0x01, 0x7f} // (func (param (ref source)) (result i32))
	body := []byte{0x00, 0xfb, 0x01, 0x00, 0x03, 0x01}             // struct.new_default; loop (type 1)
	if cast {
		body = append(body, 0xfb, 0x16, byte(wasm.HeapStruct), 0x1a, 0x41, 0x01)
	} else {
		body = append(body, 0xfb, 0x14, byte(wasm.HeapStruct))
	}
	body = append(body, 0x0b, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, loopType, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func gcMutableFieldBackedgeModule() []byte {
	body := []byte{
		0x03, 0x01, 0x63, 0x00, 0x01, 0x7f, 0x01, 0x7f, // x, iteration, seen
		0x41, 0x01, 0xfb, 0x00, 0x00, 0x21, 0x00, // x = struct.new $S(1)
		0x02, 0x40, 0x03, 0x40, // block $done; loop $again
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x21, 0x02, // seen = x.field
		0x20, 0x01, 0x0d, 0x01, // if iteration != 0, break
		0x20, 0x00, 0x41, 0x02, 0xfb, 0x05, 0x00, 0x00, // x.field = 2
		0x41, 0x01, 0x21, 0x01, 0x0c, 0x00, // iteration = 1; continue
		0x0b, 0x0b,
		0x20, 0x02, 0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01}, // (struct (field (mut i32)))
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestGCCatchEdgeIntersectsLocalFacts(t *testing.T) {
	data := gcCatchEdgeFactModule()
	if out := runGCRefFactDifferentialModule(t, "catch-normal", data, 1); out.Trap != 0 || len(out.Results) != 1 || out.Results[0] != 1 {
		t.Fatalf("normal edge = %+v, want [1]", out)
	}
	if out := runGCRefFactDifferentialModule(t, "catch-exception", data, 0); out.Trap != uint32(TrapCastFailure) {
		t.Fatalf("catch edge = %+v, want cast failure", out)
	}
	if out := runGCRefFactDifferentialModule(t, "catch-post-entry-mutation", gcCatchEdgePostEntryMutationModule()); out.Trap != uint32(TrapCastFailure) {
		t.Fatalf("post-entry catch mutation = %+v, want cast failure", out)
	}
}

func TestGCLoopAbstractUpperBoundsDoNotFoldNarrowOperations(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source wasm.AbsHeapType
		cast   bool
	}{
		{name: "eq-to-struct-test", source: wasm.HeapEq},
		{name: "any-to-struct-cast", source: wasm.HeapAny, cast: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := runGCRefFactDifferentialModule(t, tc.name, gcLoopAbstractHeapFactModule(tc.source, tc.cast))
			if out.Trap != 0 || len(out.Results) != 1 || out.Results[0] != 1 {
				t.Fatalf("run = %+v, want [1]", out)
			}
		})
	}
}

func TestGCMutableFieldForwardingDoesNotCrossBackedge(t *testing.T) {
	out := runGCRefFactDifferentialModule(t, "mutable-field-backedge", gcMutableFieldBackedgeModule())
	if out.Trap != 0 || len(out.Results) != 1 || out.Results[0] != 2 {
		t.Fatalf("run = %+v, want [2]", out)
	}
}

func gcFactDifferentialModules() []struct {
	name string
	data []byte
	args []uint64
} {
	exactBody := []byte{0x00, 0xfb, 0x01, 0x00, 0xfb, 0x16, 0x00, 0xd1, 0x0b}
	exact := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 0x00}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(exactBody))), exactBody...))),
	)

	arrayBody := []byte{0x00, 0x41, 0x04, 0xfb, 0x07, 0x00, 0xfb, 0x0f, 0x0b}
	array := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7f, 0x01}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(arrayBody))), arrayBody...))),
	)

	castCallee := []byte{0x00, 0x20, 0x00, 0xfb, 0x16, 0x00, 0xd1, 0x0b}
	castCaller := []byte{0x00, 0xd0, 0x00, 0x10, 0x00, 0x0b}
	castTrap := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x00},
			[]byte{0x60, 0x01, 0x63, 0x00, 0x01, 0x7f},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(castCallee))), castCallee...),
			append(wasmtest.ULEB(uint32(len(castCaller))), castCaller...),
		)),
	)

	boundsBody := []byte{
		0x00,
		0x41, 0x01, 0x41, 0x02, 0xfb, 0x06, 0x00, // array.new 0, length 2
		0x41, 0x02, 0xfb, 0x0b, 0x00, // get index 2: OOB
		0x0b,
	}
	boundsTrap := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7f, 0x01}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(boundsBody))), boundsBody...))),
	)

	loopBody := []byte{
		0x01, 0x01, 0x7f,
		0xfb, 0x01, 0x00,
		0x03, 0x01,
		0xfb, 0x16, 0x00, 0x1a,
		0x20, 0x00, 0x04, 0x40, 0x41, 0x07, 0x0f, 0x0b,
		0x41, 0x01, 0x21, 0x00,
		0xd0, 0x00, 0x0c, 0x00,
		0x0b, 0x41, 0x00, 0x0b,
	}
	loopTrap := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x00},
			[]byte{0x60, 0x01, 0x63, 0x00, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(loopBody))), loopBody...))),
	)

	inlineCallee := []byte{
		0x01, 0x01, 0x63, 0x00,
		0x20, 0x00, 0x04, 0x40, 0xfb, 0x01, 0x00, 0x21, 0x01, 0x0b,
		0x20, 0x01, 0xfb, 0x16, 0x00, 0x1a, 0x41, 0x07, 0x0b,
	}
	inlineCaller := []byte{0x00, 0x41, 0x01, 0x10, 0x00, 0x1a, 0x41, 0x00, 0x10, 0x00, 0x0b}
	inlineTrap := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x00},
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(inlineCallee))), inlineCallee...),
			append(wasmtest.ULEB(uint32(len(inlineCaller))), inlineCaller...),
		)),
	)

	return []struct {
		name string
		data []byte
		args []uint64
	}{
		{name: "exact-cast-success", data: exact},
		{name: "known-array-length", data: array},
		{name: "nullable-cast-trap", data: castTrap},
		{name: "known-bounds-trap", data: boundsTrap},
		{name: "no-barrier-array-set-bounds-trap", data: gcNoBarrierArraySetBoundsTrapModule()},
		{name: "scalar-array-get-bounds-trap", data: gcDirectScalarArrayBoundsTrapModule(false)},
		{name: "scalar-array-set-bounds-trap", data: gcDirectScalarArrayBoundsTrapModule(true)},
		{name: "exact-ref-null", data: gcExactRefNullModule()},
		{name: "nonexact-defined-cast", data: gcExactDefinedCastModule(false)},
		{name: "exact-defined-cast", data: gcExactDefinedCastModule(true)},
		{name: "equivalent-exact-defined-cast", data: gcEquivalentExactDefinedCastModule()},
		{name: "catch-normal", data: gcCatchEdgeFactModule(), args: []uint64{1}},
		{name: "catch-exception", data: gcCatchEdgeFactModule(), args: []uint64{0}},
		{name: "catch-post-entry-mutation", data: gcCatchEdgePostEntryMutationModule()},
		{name: "loop-eq-struct-test", data: gcLoopAbstractHeapFactModule(wasm.HeapEq, false)},
		{name: "loop-any-struct-cast", data: gcLoopAbstractHeapFactModule(wasm.HeapAny, true)},
		{name: "mutable-field-backedge", data: gcMutableFieldBackedgeModule()},
		{name: "loop-backedge-cast-trap", data: loopTrap},
		{name: "inline-zero-reset-cast-trap", data: inlineTrap},
	}
}

func TestGCRefFactsSemanticDifferential(t *testing.T) {
	const childEnv = "WAGO_GC_FACT_DIFFERENTIAL_CHILD"
	const prefix = "GC_FACT_DIFFERENTIAL="
	if os.Getenv(childEnv) == "1" {
		out := make([]gcFactDifferentialOutcome, 0, len(gcFactDifferentialModules()))
		for _, tc := range gcFactDifferentialModules() {
			out = append(out, runGCRefFactDifferentialModule(t, tc.name, tc.data, tc.args...))
		}
		data, err := json.Marshal(out)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Println(prefix + string(data))
		return
	}

	run := func(envOverride string) []gcFactDifferentialOutcome {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=^TestGCRefFactsSemanticDifferential$", "-test.count=1")
		env := make([]string, 0, len(os.Environ())+2)
		for _, entry := range os.Environ() {
			if strings.HasPrefix(entry, "WAGO_AMD64_NO_GC_REF_FACTS=") ||
				strings.HasPrefix(entry, "WAGO_AMD64_NO_EXACT_GC_REF_FACTS=") ||
				strings.HasPrefix(entry, "WAGO_AMD64_NO_GC_LOAD_FORWARDING=") ||
				strings.HasPrefix(entry, "WAGO_LOOP_PRECHECK=") ||
				strings.HasPrefix(entry, childEnv+"=") {
				continue
			}
			env = append(env, entry)
		}
		env = append(env, childEnv+"=1")
		if envOverride != "" {
			env = append(env, envOverride)
		}
		cmd.Env = env
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("differential child %q: %v\n%s", envOverride, err, output)
		}
		for _, line := range strings.Split(string(output), "\n") {
			if !strings.HasPrefix(line, prefix) {
				continue
			}
			var out []gcFactDifferentialOutcome
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, prefix)), &out); err != nil {
				t.Fatalf("decode differential child %q: %v", envOverride, err)
			}
			return out
		}
		t.Fatalf("differential child %q produced no oracle:\n%s", envOverride, output)
		return nil
	}

	for _, knob := range []struct {
		name     string
		enabled  string
		disabled string
	}{
		{name: "GC facts", enabled: "WAGO_AMD64_NO_GC_REF_FACTS=0", disabled: "WAGO_AMD64_NO_GC_REF_FACTS=1"},
		{name: "GC facts compatibility alias", enabled: "WAGO_AMD64_NO_EXACT_GC_REF_FACTS=0", disabled: "WAGO_AMD64_NO_EXACT_GC_REF_FACTS=1"},
		{name: "GC load forwarding", enabled: "WAGO_AMD64_NO_GC_LOAD_FORWARDING=0", disabled: "WAGO_AMD64_NO_GC_LOAD_FORWARDING=1"},
		{name: "loop precheck", enabled: "WAGO_LOOP_PRECHECK=1", disabled: "WAGO_LOOP_PRECHECK=0"},
	} {
		on, off := run(knob.enabled), run(knob.disabled)
		onJSON, _ := json.Marshal(on)
		offJSON, _ := json.Marshal(off)
		if string(onJSON) != string(offJSON) {
			t.Fatalf("%s semantic mismatch:\non:  %s\noff: %s", knob.name, onJSON, offJSON)
		}
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
