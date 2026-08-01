//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package wago

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func gcCrossInstancePersistentProviderModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01} // (struct (field (mut i32)))
	refCallType := []byte{0x60, 0x01, 0x63, 0x00, 0x01, 0x63, 0x00}
	runType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	tableType := []byte{0x63, 0x00, 0x01, 0x01, 0x02}    // (table 1 2 (ref null 0))
	global := []byte{0x63, 0x00, 0x01, 0xd0, 0x00, 0x0b} // (global (mut (ref null 0)) (ref.null 0))
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
		wasmtest.Section(4, wasmtest.Vec(tableType)),
		wasmtest.Section(6, wasmtest.Vec(global)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("retain", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("t", byte(wasm.ExternTable), 0),
			wasmtest.ExportEntry("g", byte(wasm.ExternGlobal), 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func gcCrossInstancePersistentConsumerModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	refCallType := []byte{0x60, 0x01, 0x63, 0x00, 0x01, 0x63, 0x00}
	runType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	tableType := []byte{0x63, 0x00, 0x01, 0x01, 0x02}
	funcImport := append(append(wasmtest.Name("provider"), wasmtest.Name("retain")...), byte(wasm.ExternFunc))
	funcImport = append(funcImport, wasmtest.ULEB(1)...)
	tableImport := append(append(wasmtest.Name("provider"), wasmtest.Name("t")...), byte(wasm.ExternTable))
	tableImport = append(tableImport, tableType...)
	globalImport := append(append(wasmtest.Name("provider"), wasmtest.Name("g")...), byte(wasm.ExternGlobal), 0x63, 0x00, 0x01)
	body := []byte{0x01, 0x01, 0x63, 0x00,
		0x20, 0x00, 0xfb, 0x00, 0x00, 0x24, 0x00,
		0x41, 0x00,
		0x20, 0x00, 0x41, 0x01, 0x6a, 0xfb, 0x00, 0x00,
		0x22, 0x01, 0x26, 0x00,
		0x20, 0x01, 0x10, 0x00, 0x1a,
		0xd0, 0x00, 0x21, 0x01,
		0x41, 0x09, 0xfb, 0x00, 0x00, 0x1a,
		0x23, 0x00, 0xfb, 0x02, 0x00, 0x00,
		0x41, 0x00, 0x25, 0x00, 0xfb, 0x02, 0x00, 0x00,
		0x6a, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, refCallType, runType)),
		wasmtest.Section(2, wasmtest.Vec(funcImport, tableImport, globalImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestGCCrossInstanceCallsWithSharedPersistentRoots(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	providerCode, err := Compile(cfg, gcCrossInstancePersistentProviderModule())
	if err != nil {
		t.Fatal(err)
	}
	defer providerCode.Close()
	consumerCode, err := Compile(cfg, gcCrossInstancePersistentConsumerModule())
	if err != nil {
		t.Fatal(err)
	}
	defer consumerCode.Close()
	if providerCode.genericGCFrameRoots() == nil || consumerCode.genericGCFrameRoots() == nil {
		t.Fatal("persistent-state cross-instance modules lost exact native roots")
	}

	for _, codec := range []bool{false, true} {
		mode := map[bool]string{false: "compiled", true: "codec"}[codec]
		t.Run(mode, func(t *testing.T) {
			providerCandidate, consumerCandidate := providerCode, consumerCode
			if codec {
				providerCandidate = roundTripCompiled(t, providerCode)
				consumerCandidate = roundTripCompiled(t, consumerCode)
				defer providerCandidate.Close()
				defer consumerCandidate.Close()
			}
			for _, tc := range []struct {
				name    string
				profile GCProfile
			}{{"tiny", GCProfileTiny}, {"throughput", GCProfileThroughput}} {
				t.Run(tc.name, func(t *testing.T) {
					store := newReferenceStore(false)
					defer store.closeRuntime()
					gcConfig := GCConfig{Profile: tc.profile, TinyHeapBytes: 256, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, TinyStepBudget: 1, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096, StressNurseryBytes: 64, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true, StressBarriers: true}
					provider, err := instantiateCore(providerCandidate, InstantiateOptions{GC: gcConfig, store: store})
					if err != nil {
						t.Fatal(err)
					}
					retain, err := provider.ExportedFunc("retain")
					if err != nil {
						provider.Close()
						t.Fatal(err)
					}
					table, err := provider.ExportedTable("t")
					if err != nil {
						provider.Close()
						t.Fatal(err)
					}
					global, err := provider.ExportedGlobalObject("g")
					if err != nil {
						provider.Close()
						t.Fatal(err)
					}
					consumer, err := instantiateCore(consumerCandidate, InstantiateOptions{GC: gcConfig, store: store, Imports: Imports{"provider.retain": retain, "provider.t": table, "provider.g": global}})
					if err != nil {
						provider.Close()
						t.Fatal(err)
					}
					defer consumer.Close()
					if provider.gc == nil || provider.gc != consumer.gc || !store.ownsGCCollector(provider.gc) {
						t.Fatal("persistent-state call instances do not share one collector domain")
					}
					for i := uint64(20); i < 24; i++ {
						got, callErr := consumer.Invoke("run", i)
						want := []uint64{2*i + 1}
						if callErr != nil || !reflect.DeepEqual(got, want) {
							t.Fatalf("run %d = %v, %v; want %v", i, got, callErr, want)
						}
					}
					if err := provider.Close(); err != nil {
						t.Fatal(err)
					}
					got, callErr := consumer.Invoke("run", 31)
					if callErr != nil || !reflect.DeepEqual(got, []uint64{63}) {
						t.Fatalf("run after provider close = %v, %v", got, callErr)
					}
				})
			}
		})
	}
}
