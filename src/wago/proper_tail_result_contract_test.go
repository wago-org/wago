//go:build (linux && amd64 && !tinygo && !wago_guardpage) || ((linux || darwin) && arm64 && !tinygo && !wago_guardpage)

package wago

import (
	"context"
	"encoding/binary"
	"math"
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

type properTailBehaviorKind uint8

const (
	properTailDirect properTailBehaviorKind = iota
	properTailIndirect
	properTailRef
)

func properTailResultModule(kind properTailBehaviorKind, results []wasm.ValType, targetBody []byte) []byte {
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, results))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
	}
	if kind == properTailIndirect {
		sections = append(sections, wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))))
	switch kind {
	case properTailIndirect:
		sections = append(sections, wasmtest.Section(9, wasmtest.Vec([]byte{0x00, 0x41, 0x00, 0x0b, 0x01, 0x00})))
	case properTailDirect, properTailRef:
		sections = append(sections, wasmtest.Section(9, wasmtest.Vec(append([]byte{0x03, 0x00}, wasmtest.Vec(wasmtest.ULEB(0))...))))
	}
	caller := []byte{0x12, 0x00, 0x0b}
	if kind == properTailIndirect {
		caller = []byte{0x41, 0x00, 0x13, 0x00, 0x00, 0x0b}
	} else if kind == properTailRef {
		caller = []byte{0xd2, 0x00, 0x15, 0x00, 0x0b}
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(targetBody), wasmtest.Code(caller))))
	return wasmtest.Module(sections...)
}

func compileProperTailBehavior(t testing.TB, module []byte, objective OptimizationObjective, workers int) *Compiled {
	t.Helper()
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit).WithOptimizationObjective(objective).WithFunctionWorkers(workers)
	compiled, err := cfg.Compile(module)
	if err != nil {
		t.Fatalf("compile proper-tail behavior module: %v", err)
	}
	t.Cleanup(func() { _ = compiled.Close() })
	return compiled
}

func TestProperTailResultContractsExecuteAcrossKindsAndObjectives(t *testing.T) {
	vector := [16]byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
	shapes := []struct {
		name     string
		results  []wasm.ValType
		body     []byte
		wantSlot []uint64
	}{
		{name: "scalar", results: []wasm.ValType{wasm.I32}, body: []byte{0x41, 0x2a, 0x0b}, wantSlot: []uint64{42}},
		{name: "multivalue", results: []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}, body: []byte{
			0x41, 0x2a,
			0x42, 0x09,
			0x43, 0x00, 0x00, 0xc0, 0x3f,
			0x44, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0x40,
			0x0b,
		}, wantSlot: []uint64{42, 9, uint64(math.Float32bits(1.5)), math.Float64bits(2.25)}},
		{name: "SIMD", results: []wasm.ValType{wasm.V128}, body: append(append([]byte{0xfd, 0x0c}, vector[:]...), 0x0b), wantSlot: []uint64{binary.LittleEndian.Uint64(vector[:8]), binary.LittleEndian.Uint64(vector[8:])}},
	}
	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			kinds := []properTailBehaviorKind{properTailDirect, properTailIndirect}
			if shape.name == "scalar" {
				kinds = append(kinds, properTailRef)
			}
			for _, kind := range kinds {
				t.Run([]string{"direct", "indirect", "ref"}[kind], func(t *testing.T) {
					for _, objective := range []OptimizationObjective{OptimizeBalanced, OptimizeSize, OptimizeEmbedded} {
						t.Run(objective.String(), func(t *testing.T) {
							compiled := compileProperTailBehavior(t, properTailResultModule(kind, shape.results, shape.body), objective, 2)
							in, err := instantiateCore(compiled, InstantiateOptions{})
							if err != nil {
								t.Fatal(err)
							}
							defer in.Close()
							got, err := in.Invoke("run")
							if err != nil || !reflect.DeepEqual(got, shape.wantSlot) {
								t.Fatalf("proper-tail result slots = %#x, %v; want %#x", got, err, shape.wantSlot)
							}
						})
					}
				})
			}
		})
	}
}

func recursiveProperTailResultModule(kind properTailBehaviorKind) []byte {
	body := []byte{
		0x20, 0x00, 0x45, // local.get 0; i32.eqz
		0x04, 0x7f, // if (result i32)
		0x41, 0x07, // i32.const 7
		0x05,                         // else
		0x20, 0x00, 0x41, 0x01, 0x6b, // n - 1
	}
	switch kind {
	case properTailDirect:
		body = append(body, 0x12, 0x00)
	case properTailIndirect:
		body = append(body, 0x41, 0x00, 0x13, 0x00, 0x00)
	case properTailRef:
		body = append(body, 0xd2, 0x00, 0x15, 0x00)
	}
	body = append(body, 0x0b, 0x0b)
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
	}
	if kind == properTailIndirect {
		sections = append(sections, wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))))
	if kind == properTailIndirect {
		sections = append(sections, wasmtest.Section(9, wasmtest.Vec([]byte{0x00, 0x41, 0x00, 0x0b, 0x01, 0x00})))
	} else {
		sections = append(sections, wasmtest.Section(9, wasmtest.Vec(append([]byte{0x03, 0x00}, wasmtest.Vec(wasmtest.ULEB(0))...))))
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
	return wasmtest.Module(sections...)
}

func TestProperTailResultContractsDiscardFrames(t *testing.T) {
	for _, kind := range []properTailBehaviorKind{properTailDirect, properTailIndirect, properTailRef} {
		t.Run([]string{"direct", "indirect", "ref"}[kind], func(t *testing.T) {
			compiled := compileProperTailBehavior(t, recursiveProperTailResultModule(kind), OptimizeSize, 2)
			in, err := instantiateCore(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			got, err := in.Invoke("run", 1_000_000)
			if err != nil || !reflect.DeepEqual(got, []uint64{7}) {
				t.Fatalf("million-deep proper tail = %v, %v; want [7]", got, err)
			}
		})
	}
}

func typedGlobalReturnCallRefModule() []byte {
	type0 := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	global := []byte{0x64, 0x00, 0x00, 0xd2, 0x00, 0x0b} // (ref 0), immutable, ref.func 0
	declared := append([]byte{0x03, 0x00}, wasmtest.Vec(wasmtest.ULEB(0))...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(type0)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(6, wasmtest.Vec(global)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec(declared)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0xcd, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x23, 0x00, 0x15, 0x00, 0x0b}),
		)),
	)
}

func TestProperTailTypedGlobalReturnCallRefPreservesAdapterResult(t *testing.T) {
	for _, objective := range []OptimizationObjective{OptimizeBalanced, OptimizeSize, OptimizeEmbedded} {
		t.Run(objective.String(), func(t *testing.T) {
			compiled := compileProperTailBehavior(t, typedGlobalReturnCallRefModule(), objective, 2)
			in, err := instantiateCore(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			got, err := in.Call(context.Background(), "run")
			if err != nil || len(got) != 1 || got[0].Type() != ValI32 || got[0].I32() != 77 {
				t.Fatalf("typed-global return_call_ref = %v, %v", got, err)
			}
		})
	}
}

func BenchmarkProperTailResultContracts(b *testing.B) {
	for _, kind := range []properTailBehaviorKind{properTailDirect, properTailIndirect, properTailRef} {
		b.Run([]string{"direct", "indirect", "ref"}[kind], func(b *testing.B) {
			compiled := compileProperTailBehavior(b, properTailResultModule(kind, []wasm.ValType{wasm.I32, wasm.I64}, []byte{0x41, 0x2a, 0x42, 0x09, 0x0b}), OptimizeBalanced, 1)
			in, err := instantiateCore(compiled, InstantiateOptions{})
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := in.Invoke("run"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
