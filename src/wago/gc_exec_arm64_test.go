//go:build arm64

package wago

import (
	"reflect"
	"strings"
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

func TestGCArm64UnsupportedHiddenOperandRemainsCollectionDisabled(t *testing.T) {
	features := CoreFeaturesV2 | CoreFeatureTypedFunctionReferences | CoreFeatureGC
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(features), arm64GCHiddenOperandLoopModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if roots := compiled.genericGCFrameRoots(); roots != nil {
		t.Fatalf("hidden-operand module unexpectedly received exact arm64 roots: %+v", roots)
	}
	in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 64, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("allocate", 100); err == nil || !strings.Contains(err.Error(), "collection-disabled heap exhausted") {
		t.Fatalf("unsupported hidden-root allocation error = %v", err)
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
