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

func gcCrossInstanceRelayModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	refCallType := []byte{0x60, 0x01, 0x63, 0x00, 0x01, 0x63, 0x00}
	runType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	imp := append(append(wasmtest.Name("provider"), wasmtest.Name("retain")...), 0x00)
	imp = append(imp, wasmtest.ULEB(1)...)
	relay := []byte{0x00,
		0x41, 0x37, 0xfb, 0x00, 0x00, 0x1a,
		0x20, 0x00, 0x10, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, refCallType, runType)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("relay", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(relay))), relay...))),
	)
}

func gcDistinctDomainModule() []byte {
	structType := []byte{0x5f, 0x02, 0x7f, 0x01, 0x7f, 0x01}
	runType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	body := []byte{0x00, 0x20, 0x00, 0x41, 0x02, 0xfb, 0x00, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, runType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func gcUnresolvedImportDistinctDomainModule() []byte {
	structType := []byte{0x5f, 0x02, 0x7f, 0x01, 0x7f, 0x01}
	voidType := wasmtest.FuncType(nil, nil)
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("missing")...), 0x00)
	imp = append(imp, wasmtest.ULEB(1)...)
	body := []byte{0x00, 0x41, 0x01, 0x41, 0x02, 0xfb, 0x00, 0x00, 0x1a, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, voidType)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func gcStoreDomainCount(store *referenceStore) int {
	store.mu.Lock()
	defer store.mu.Unlock()
	count := 0
	for domain := store.gcDomains; domain != nil; domain = domain.next {
		count++
	}
	return count
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

func TestGCCrossInstanceMultiHopFrameRootsAndCodec(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	providerCode, err := Compile(cfg, gcCrossInstanceProviderModule())
	if err != nil {
		t.Fatal(err)
	}
	defer providerCode.Close()
	relayCode, err := Compile(cfg, gcCrossInstanceRelayModule())
	if err != nil {
		t.Fatal(err)
	}
	defer relayCode.Close()
	consumerCode, err := Compile(cfg, gcCrossInstanceConsumerModule())
	if err != nil {
		t.Fatal(err)
	}
	defer consumerCode.Close()

	for _, codec := range []bool{false, true} {
		t.Run(map[bool]string{false: "compiled", true: "codec"}[codec], func(t *testing.T) {
			providerCandidate, relayCandidate, consumerCandidate := providerCode, relayCode, consumerCode
			if codec {
				providerCandidate = roundTripCompiled(t, providerCode)
				relayCandidate = roundTripCompiled(t, relayCode)
				consumerCandidate = roundTripCompiled(t, consumerCode)
				defer providerCandidate.Close()
				defer relayCandidate.Close()
				defer consumerCandidate.Close()
			}
			if relayCandidate.genericGCFrameRoots() == nil {
				t.Fatal("relay lost exact native roots")
			}
			store := newReferenceStore(false)
			defer store.closeRuntime()
			gcConfig := GCConfig{Profile: GCProfileThroughput, StressNurseryBytes: 64, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096}
			provider, err := instantiateCore(providerCandidate, InstantiateOptions{GC: gcConfig, store: store})
			if err != nil {
				t.Fatal(err)
			}
			defer provider.Close()
			providerExport, err := provider.ExportedFunc("retain")
			if err != nil {
				t.Fatal(err)
			}
			relay, err := instantiateCore(relayCandidate, InstantiateOptions{GC: gcConfig, store: store, Imports: Imports{"provider.retain": providerExport}})
			if err != nil {
				t.Fatal(err)
			}
			defer relay.Close()
			relayExport, err := relay.ExportedFunc("relay")
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := instantiateCore(consumerCandidate, InstantiateOptions{GC: gcConfig, store: store, Imports: Imports{"provider.retain": relayExport}})
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			if provider.gc != relay.gc || relay.gc != consumer.gc {
				t.Fatal("multi-hop instances do not share one collector domain")
			}
			for i := 0; i < 10; i++ {
				want := uint64(900 + i)
				got, err := consumer.Invoke("run", want)
				if err != nil || !reflect.DeepEqual(got, []uint64{want}) {
					t.Fatalf("run %d = %v, %v", i, got, err)
				}
			}
		})
	}
}

func TestGCRuntimeMaintainsIndependentCollectorDomains(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	firstCode, err := Compile(cfg, gcCrossInstanceProviderModule())
	if err != nil {
		t.Fatal(err)
	}
	defer firstCode.Close()
	secondCode, err := Compile(cfg, gcDistinctDomainModule())
	if err != nil {
		t.Fatal(err)
	}
	defer secondCode.Close()

	store := newReferenceStore(false)
	gcConfig := GCConfig{Profile: GCProfileThroughput, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096}
	first, err := instantiateCore(firstCode, InstantiateOptions{GC: gcConfig, store: store})
	if err != nil {
		t.Fatal(err)
	}
	second, err := instantiateCore(secondCode, InstantiateOptions{GC: gcConfig, store: store})
	if err != nil {
		first.Close()
		t.Fatal(err)
	}
	if first.gc == nil || second.gc == nil || first.gc == second.gc {
		t.Fatalf("descriptor-distinct modules did not receive independent collectors: first=%p second=%p descriptors-equal=%v first-types=%v second-types=%v", first.gc, second.gc, reflect.DeepEqual(firstCode.GCTypeDescs, secondCode.GCTypeDescs), firstCode.GCTypeDescs, secondCode.GCTypeDescs)
	}
	if got := gcStoreDomainCount(store); got != 2 {
		t.Fatalf("GC domain count = %d, want 2", got)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if got := gcStoreDomainCount(store); got != 1 {
		t.Fatalf("GC domain count after first close = %d, want 1", got)
	}
	if got, err := second.Invoke("run", 19); err != nil || !reflect.DeepEqual(got, []uint64{19}) {
		t.Fatalf("remaining domain execution = %v, %v", got, err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if got := gcStoreDomainCount(store); got != 0 {
		t.Fatalf("GC domain count after final close = %d, want 0", got)
	}
	store.closeRuntime()
}

func TestGCRuntimeCollectorDomainRollbackAndConfigMismatch(t *testing.T) {
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
	failedCode, err := Compile(cfg, gcUnresolvedImportDistinctDomainModule())
	if err != nil {
		t.Fatal(err)
	}
	defer failedCode.Close()

	store := newReferenceStore(false)
	baseConfig := GCConfig{Profile: GCProfileThroughput, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096}
	provider, err := instantiateCore(providerCode, InstantiateOptions{GC: baseConfig, store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	export, err := provider.ExportedFunc("retain")
	if err != nil {
		t.Fatal(err)
	}
	mismatch := baseConfig
	mismatch.StressNurseryBytes = 64
	if _, err := instantiateCore(consumerCode, InstantiateOptions{GC: mismatch, store: store, Imports: Imports{"provider.retain": export}}); err == nil {
		t.Fatal("cross-instance GC link accepted incompatible collector configuration")
	}
	if got := gcStoreDomainCount(store); got != 1 {
		t.Fatalf("GC domain count after configuration rejection = %d, want 1", got)
	}
	if _, err := instantiateCore(failedCode, InstantiateOptions{GC: baseConfig, store: store}); err == nil {
		t.Fatal("GC module with unresolved import instantiated")
	}
	if got := gcStoreDomainCount(store); got != 1 {
		t.Fatalf("GC domain count after failed distinct-domain instantiation = %d, want 1", got)
	}
	if got, err := provider.Invoke("retain", 0); err != nil || !reflect.DeepEqual(got, []uint64{0}) {
		t.Fatalf("provider after rollback = %v, %v", got, err)
	}
}
