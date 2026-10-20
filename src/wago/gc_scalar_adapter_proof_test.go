//go:build linux && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcScalarAdapterCallerModule(call string, ownObject, unusedCollectorSignature bool) []byte {
	types := [][]byte{}
	functionType := byte(0)
	if ownObject {
		types = append(types, []byte{0x5f, 1, 0x7f, 1})
		functionType++
	}
	types = append(types, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))
	if unusedCollectorSignature {
		types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.AnyRef}, []wasm.ValType{wasm.AnyRef}))
	}
	importEntry := append(wasmtest.Name("env"), wasmtest.Name("call")...)
	importEntry = append(importEntry, 0, functionType)
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec([]byte{functionType})),
	}
	if call != "direct" {
		sections = append(sections, wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0, 1})))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))))
	if call == "indirect" || call == "ref" {
		sections = append(sections, wasmtest.Section(9, wasmtest.Vec([]byte{0, 0x41, 0, 0x0b, 1, 0})))
	}
	body := []byte{0}
	if ownObject {
		body = []byte{1, 1, 0x63, 0, 0x41, 0xc9, 0, 0xfb, 0, 0, 0x21, 0}
	}
	switch call {
	case "direct", "unused-table":
		body = append(body, 0x10, 0)
	case "indirect":
		body = append(body, 0x41, 0, 0x11, functionType, 0)
	case "ref":
		body = append(body, 0xd2, 0, 0x14, functionType)
	default:
		panic("unknown scalar adapter call")
	}
	if ownObject {
		body = append(body, 0x1a, 0x20, 0, 0xfb, 2, 0, 0)
	}
	body = append(body, 0x0b)
	body = append(wasmtest.ULEB(uint32(len(body))), body...)
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(body)))
	return wasmtest.Module(sections...)
}

// An unrelated table or declared collector signature cannot change the scalar
// call's ownership contract. The producer's host call keeps its adapter/root
// callsite live; its earlier allocation therefore walks that adapter too.
func TestGCScalarAdapterCallerOwnership(t *testing.T) {
	for _, registerABI := range []bool{false, true} {
		for _, call := range []string{"direct", "unused-table", "indirect", "ref"} {
			for _, ownObject := range []bool{false, true} {
				for _, unusedSignature := range []bool{false, true} {
					t.Run(fmt.Sprintf("register=%t/%s/object=%t/unusedSignature=%t", registerABI, call, ownObject, unusedSignature), func(t *testing.T) {
						cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("reg-abi", registerABI)
						rt := NewRuntime(WithRuntimeConfig(cfg))
						defer rt.Close()
						producerCode, err := rt.Compile(gcAllocatingHostScalarProducerModule())
						if err != nil {
							t.Fatal(err)
						}
						defer producerCode.Close()
						settings := GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true}
						producer, err := rt.Instantiate(context.Background(), producerCode, WithGC(settings), WithImports(testImports("env.host", slotHostFunc(func(_ HostModule, _ []uint64, results []uint64) { results[0] = 7 }))))
						if err != nil {
							t.Fatal(err)
						}
						defer producer.Close()
						target, err := producer.ExportedFunc("call")
						if err != nil {
							t.Fatal(err)
						}
						callerCode, err := rt.Compile(gcScalarAdapterCallerModule(call, ownObject, unusedSignature))
						if err != nil {
							t.Fatal(err)
						}
						defer callerCode.Close()
						caller, err := rt.Instantiate(context.Background(), callerCode, WithGC(settings), WithImports(testImports("env.call", target)))
						if err != nil {
							t.Fatal(err)
						}
						defer caller.Close()
						if caller.gc == producer.gc || (caller.gc != nil) != ownObject {
							t.Fatal("fixture must have separate collector domains and the requested caller roots")
						}
						want := uint64(7)
						if ownObject {
							want = 73
						}
						for i := 0; i < 3; i++ {
							got, err := caller.Invoke("run")
							if err != nil || len(got) != 1 || got[0] != want {
								t.Fatalf("scalar boundary = %v, %v; want [%d], nil", got, err, want)
							}
						}
					})
				}
			}
		}
	}
}

func gcScalarAdapterProofModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 1, 0x7f, 1},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.AnyRef}, []wasm.ValType{wasm.I32}),
			[]byte{0x60, 1, 0x63, 0, 1, 0x7f}, // defined collector reference
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2}, []byte{3})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("scalar", 0, 0), wasmtest.ExportEntry("any", 0, 1), wasmtest.ExportEntry("defined", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0xfb, 1, 0, 0x1a, 0x41, 7, 0x0b}),
			wasmtest.Code([]byte{0xfb, 1, 0, 0x1a, 0x20, 0, 0xd1, 0x1a, 0x41, 7, 0x0b}),
			wasmtest.Code([]byte{0xfb, 1, 0, 0x1a, 0x20, 0, 0xd1, 0x1a, 0x41, 7, 0x0b}),
		)),
	)
}

// Reflection lets the tests-first commit compile before the new proof word is
// introduced. A missing word is a failed eligibility property, not a skip.
func TestGCScalarAdapterLogicalFunctionProof(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcScalarAdapterProofModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	roots := c.genericGCFrameRoots()
	if roots == nil || len(roots.safepoints) < 3 {
		t.Fatal("missing allocating function controls")
	}
	seen := [3]bool{}
	for _, point := range roots.safepoints {
		proof := reflect.ValueOf(point).FieldByName("functionProof")
		if !proof.IsValid() || proof.Kind() != reflect.Uint32 {
			t.Fatal("safepoint lacks its logical function-signature proof")
		}
		word := uint32(proof.Uint())
		function, scalar := word>>1, word&1 != 0
		if function >= uint32(len(seen)) || scalar != (function == 0) {
			t.Fatalf("function proof %#x admits a collector-bearing signature", word)
		}
		seen[function] = true
	}
	if seen != [3]bool{true, true, true} {
		t.Fatalf("missing logical function classifications: %v", seen)
	}
}

func TestGCScalarAdapterAllocOnlyCallee(t *testing.T) {
	for _, registerABI := range []bool{false, true} {
		t.Run(fmt.Sprintf("register=%t", registerABI), func(t *testing.T) {
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("reg-abi", registerABI)
			rt := NewRuntime(WithRuntimeConfig(cfg))
			defer rt.Close()
			producerCode, err := rt.Compile(gcAllocatingScalarProducerModule())
			if err != nil {
				t.Fatal(err)
			}
			defer producerCode.Close()
			settings := GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true}
			producer, err := rt.Instantiate(context.Background(), producerCode, WithGC(settings))
			if err != nil {
				t.Fatal(err)
			}
			defer producer.Close()
			if roots := producer.c.genericGCFrameRoots(); roots == nil || len(roots.callsites) != 0 {
				t.Fatal("fixture must allocate without any module-local callsites")
			}
			target, err := producer.ExportedFunc("allocate")
			if err != nil {
				t.Fatal(err)
			}
			callerCode, err := rt.Compile(gcScalarAdapterCallerModule("direct", true, false))
			if err != nil {
				t.Fatal(err)
			}
			defer callerCode.Close()
			caller, err := rt.Instantiate(context.Background(), callerCode, WithGC(settings), WithImports(testImports("env.call", target)))
			if err != nil {
				t.Fatal(err)
			}
			defer caller.Close()
			if caller.gc == nil || caller.gc != producer.gc {
				t.Fatal("fixture must retain caller objects in the collecting domain")
			}
			for i := 0; i < 3; i++ {
				got, err := caller.Invoke("run")
				if err != nil || len(got) != 1 || got[0] != 73 {
					t.Fatalf("caller root across alloc-only callee = %v, %v; want [73], nil", got, err)
				}
			}
		})
	}
}
