//go:build arm64

package wago

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc"
	"github.com/wago-org/wago/testutil/wasmtest"
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

func TestGCStructExecutionArm64(t *testing.T) {
	features := CoreFeaturesV2 | CoreFeatureTypedFunctionReferences | CoreFeatureGC
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(features), arm64GCStructModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	for _, candidate := range []*Compiled{compiled, roundTripCompiled(t, compiled)} {
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
	for _, candidate := range []*Compiled{compiled, roundTripCompiled(t, compiled)} {
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
	for _, candidate := range []*Compiled{compiled, roundTripCompiled(t, compiled)} {
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
	for _, candidate := range []*Compiled{compiled, roundTripCompiled(t, compiled)} {
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
		{Profile: GCProfileThroughput, StressNurseryBytes: 64, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096},
		{Profile: GCProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true},
	}
	for _, candidate := range []*Compiled{compiled, roundTripCompiled(t, compiled)} {
		if candidate != compiled {
			defer candidate.Close()
		}
		for _, profile := range profiles {
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
				t.Fatalf("recursive arm64 root walking allocations = %v, want 0", allocs)
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
	for _, candidate := range []*Compiled{compiled, roundTripCompiled(t, compiled)} {
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
