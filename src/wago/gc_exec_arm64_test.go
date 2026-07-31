//go:build arm64

package wago

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
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
