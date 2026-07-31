//go:build linux && amd64

package wago

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func gcCrossInstanceProviderModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01} // (struct (field (mut i32)))
	refCallType := []byte{0x60, 0x01, 0x63, 0x00, 0x01, 0x63, 0x00}
	runType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	body := []byte{0x01, 0x01, 0x7f,
		0x02, 0x40, 0x03, 0x40,
		0x20, 0x01, 0x41, 0xe8, 0x07, 0x4f, 0x0d, 0x01,
		0xfb, 0x01, 0x00, 0x1a,
		0x20, 0x01, 0x41, 0x01, 0x6a, 0x21, 0x01, 0x0c, 0x00,
		0x0b, 0x0b,
		0x20, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, refCallType, runType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("retain", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func gcCrossInstanceConsumerModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	refCallType := []byte{0x60, 0x01, 0x63, 0x00, 0x01, 0x63, 0x00}
	runType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	imp := append(append(wasmtest.Name("provider"), wasmtest.Name("retain")...), 0x00)
	imp = append(imp, wasmtest.ULEB(1)...)
	body := []byte{0x01, 0x01, 0x63, 0x00,
		0x20, 0x00, 0xfb, 0x00, 0x00, 0x21, 0x01,
		0x20, 0x01, 0x10, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, refCallType, runType)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestGCCrossInstanceSharedCollectorOwnership(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	providerCode, err := Compile(cfg, gcCrossInstanceProviderModule())
	if err != nil {
		t.Fatal(err)
	}
	defer providerCode.Close()
	consumerCode, err := Compile(cfg, gcCrossInstanceConsumerModule())
	if err != nil {
		t.Fatal(err)
	}
	defer consumerCode.Close()
	if providerCode.genericGCFrameRoots() == nil || consumerCode.genericGCFrameRoots() == nil {
		t.Fatal("cross-instance GC modules lost exact native root admission")
	}

	store := newReferenceStore(false)
	gcConfig := GCConfig{Profile: GCProfileThroughput, StressNurseryBytes: 64, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096}
	provider, err := instantiateCore(providerCode, InstantiateOptions{GC: gcConfig, store: store})
	if err != nil {
		t.Fatal(err)
	}
	export, err := provider.ExportedFunc("retain")
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := instantiateCore(consumerCode, InstantiateOptions{GC: gcConfig, store: store, Imports: Imports{"provider.retain": export}})
	if err != nil {
		t.Fatal(err)
	}
	if provider.gc == nil || provider.gc != consumer.gc || !store.ownsGCCollector(provider.gc) {
		t.Fatal("instances do not share the Runtime GC domain")
	}
	for i := 0; i < 10; i++ {
		got, callErr := consumer.Invoke("run", uint64(100+i))
		if callErr != nil || !reflect.DeepEqual(got, []uint64{uint64(100 + i)}) {
			t.Fatalf("run %d = %v, %v", i, got, callErr)
		}
	}
	if err := provider.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := consumer.Invoke("run", 777)
	if err != nil || !reflect.DeepEqual(got, []uint64{777}) {
		t.Fatalf("retained producer run = %v, %v", got, err)
	}
	if err := consumer.Close(); err != nil {
		t.Fatal(err)
	}
	store.closeRuntime()
}
