//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package wago

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func gcSharedGlobalProviderModule(mutable bool) []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01} // (struct (field (mut i32)))
	readType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	mut := byte(0)
	if mutable {
		mut = 1
	}
	global := []byte{
		0x64, 0x00, mut, // (global (ref 0))
		0x41, 0x2a, // i32.const 42
		0xfb, 0x00, 0x00, // struct.new 0
		0x0b,
	}
	body := []byte{
		0x00,                               // no locals
		0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a, // transient struct allocation; drop
		0x23, 0x00, // global.get 0
		0xfb, 0x02, 0x00, 0x00, // struct.get 0 0
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, readType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec(global)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("g", byte(wasm.ExternGlobal), 0),
			wasmtest.ExportEntry("read", byte(wasm.ExternFunc), 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func gcSharedDistinctGlobalProviderModule() []byte {
	structType := []byte{0x5f, 0x02, 0x7f, 0x01, 0x7f, 0x01}
	readType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	global := []byte{
		0x64, 0x00, 0x00,
		0x41, 0x2a, 0x41, 0xe3, 0x00, // 42, 99
		0xfb, 0x00, 0x00,
		0x0b,
	}
	body := []byte{0x00, 0x23, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, readType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec(global)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("g", byte(wasm.ExternGlobal), 0),
			wasmtest.ExportEntry("read", byte(wasm.ExternFunc), 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func gcSharedImmutableGlobalConsumerModule(mutable bool) []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	readType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	mut := byte(0)
	if mutable {
		mut = 1
	}
	globalImport := append(append(wasmtest.Name("provider"), wasmtest.Name("g")...), byte(wasm.ExternGlobal), 0x64, 0x00, mut)
	body := []byte{0x00}
	if mutable {
		body = append(body,
			0x41, 0xcd, 0x00, 0xfb, 0x00, 0x00, // i32.const 77; struct.new 0
			0x24, 0x00, // global.set 0
		)
	}
	body = append(body,
		0x41, 0x09, 0xfb, 0x00, 0x00, 0x1a,
		0x23, 0x00,
		0xfb, 0x02, 0x00, 0x00,
		0x0b,
	)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, readType)),
		wasmtest.Section(2, wasmtest.Vec(globalImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("read", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func sharedGlobalDomainCount(store *referenceStore) int {
	store.mu.Lock()
	defer store.mu.Unlock()
	count := 0
	for domain := store.gcDomains; domain != nil; domain = domain.next {
		count++
	}
	return count
}

func TestGCSharedImmutableGlobalSameDomainCollectionAndCloseOrder(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	providerCode, err := Compile(cfg, gcSharedGlobalProviderModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer providerCode.Close()
	consumerCode, err := Compile(cfg, gcSharedImmutableGlobalConsumerModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer consumerCode.Close()
	if providerCode.genericGCFrameRoots() == nil || consumerCode.genericGCFrameRoots() == nil {
		t.Fatal("immutable GC-global modules lost exact native root admission")
	}

	store := newReferenceStore(false)
	defer store.closeRuntime()
	gcConfig := GCConfig{Profile: GCProfileThroughput, StressNurseryBytes: 64, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096}
	provider, err := instantiateCore(providerCode, InstantiateOptions{GC: gcConfig, store: store})
	if err != nil {
		t.Fatal(err)
	}
	global, err := provider.ExportedGlobalObject("g")
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := instantiateCore(consumerCode, InstantiateOptions{GC: gcConfig, store: store, Imports: Imports{"provider.g": global}})
	if err != nil {
		t.Fatal(err)
	}
	if provider.gc == nil || provider.gc != consumer.gc || !store.ownsGCCollector(provider.gc) {
		t.Fatal("immutable GC-global instances do not share one Runtime collector domain")
	}
	for i := 0; i < 20; i++ {
		if got, callErr := consumer.Invoke("read"); callErr != nil || !reflect.DeepEqual(got, []uint64{42}) {
			t.Fatalf("consumer read %d = %v, %v", i, got, callErr)
		}
	}
	if err := provider.Close(); err != nil {
		t.Fatal(err)
	}
	if got, callErr := consumer.Invoke("read"); callErr != nil || !reflect.DeepEqual(got, []uint64{42}) {
		t.Fatalf("retained global after provider close = %v, %v", got, callErr)
	}
	if err := consumer.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestGCSharedGlobalCodecAndRollback(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	providerBase, err := Compile(cfg, gcSharedGlobalProviderModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer providerBase.Close()
	consumerBase, err := Compile(cfg, gcSharedImmutableGlobalConsumerModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer consumerBase.Close()
	providerCode := roundTripCompiled(t, providerBase)
	consumerCode := roundTripCompiled(t, consumerBase)
	defer providerCode.Close()
	defer consumerCode.Close()
	if providerCode.genericGCFrameRoots() == nil || consumerCode.genericGCFrameRoots() == nil {
		t.Fatal("codec reload lost shared GC-global frame roots")
	}
	store := newReferenceStore(false)
	defer store.closeRuntime()
	gcConfig := GCConfig{Profile: GCProfileThroughput, StressNurseryBytes: 64, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096}
	provider, err := instantiateCore(providerCode, InstantiateOptions{GC: gcConfig, store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	global, err := provider.ExportedGlobalObject("g")
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := instantiateCore(consumerCode, InstantiateOptions{GC: gcConfig, store: store, Imports: Imports{"provider.g": global}})
	if err != nil {
		t.Fatal(err)
	}
	if got, callErr := consumer.Invoke("read"); callErr != nil || !reflect.DeepEqual(got, []uint64{42}) {
		t.Fatalf("codec consumer read = %v, %v", got, callErr)
	}
	if err := consumer.Close(); err != nil {
		t.Fatal(err)
	}

	distinctCode, err := Compile(cfg, gcSharedDistinctGlobalProviderModule())
	if err != nil {
		t.Fatal(err)
	}
	defer distinctCode.Close()
	distinct, err := instantiateCore(distinctCode, InstantiateOptions{GC: gcConfig, store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer distinct.Close()
	distinctGlobal, err := distinct.ExportedGlobalObject("g")
	if err != nil {
		t.Fatal(err)
	}
	before := sharedGlobalDomainCount(store)
	if _, err := instantiateCore(consumerCode, InstantiateOptions{GC: gcConfig, store: store, Imports: Imports{"provider.g": distinctGlobal}}); err == nil {
		t.Fatal("distinct GC-domain global import unexpectedly succeeded")
	}
	if got := sharedGlobalDomainCount(store); got != before {
		t.Fatalf("failed GC-global import changed domain count %d -> %d", before, got)
	}
	if got, callErr := distinct.Invoke("read"); callErr != nil || !reflect.DeepEqual(got, []uint64{42}) {
		t.Fatalf("distinct provider after rollback = %v, %v", got, callErr)
	}
}

func TestGCSharedMutableGlobalAliasesPublishRootsAndBarriers(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	providerCode, err := Compile(cfg, gcSharedGlobalProviderModule(true))
	if err != nil {
		t.Fatal(err)
	}
	defer providerCode.Close()
	consumerCode, err := Compile(cfg, gcSharedImmutableGlobalConsumerModule(true))
	if err != nil {
		t.Fatal(err)
	}
	defer consumerCode.Close()
	if !providerCode.sharedGCGlobalDomainSafe() || !consumerCode.sharedGCGlobalDomainSafe() || providerCode.genericGCFrameRoots() == nil || consumerCode.genericGCFrameRoots() == nil {
		t.Fatal("mutable GC-global modules lost shared-global root admission")
	}
	store := newReferenceStore(false)
	defer store.closeRuntime()
	gcConfig := GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, TinyStepBudget: 1, VerifyAfterCollect: true, StressBarriers: true}
	provider, err := instantiateCore(providerCode, InstantiateOptions{GC: gcConfig, store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	global, err := provider.ExportedGlobalObject("g")
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := instantiateCore(consumerCode, InstantiateOptions{GC: gcConfig, store: store, Imports: Imports{"provider.g": global}})
	if err != nil {
		t.Fatal(err)
	}
	if provider.gc == nil || provider.gc != consumer.gc {
		t.Fatal("mutable GC-global aliases do not share one collector")
	}
	for i := 0; i < 20; i++ {
		if got, callErr := consumer.Invoke("read"); callErr != nil || !reflect.DeepEqual(got, []uint64{77}) {
			t.Fatalf("mutable consumer read %d = %v, %v", i, got, callErr)
		}
		if got, callErr := provider.Invoke("read"); callErr != nil || !reflect.DeepEqual(got, []uint64{77}) {
			t.Fatalf("provider alias read %d = %v, %v", i, got, callErr)
		}
	}
	if err := consumer.Close(); err != nil {
		t.Fatal(err)
	}
	if got, callErr := provider.Invoke("read"); callErr != nil || !reflect.DeepEqual(got, []uint64{77}) {
		t.Fatalf("provider after importer close = %v, %v", got, callErr)
	}
}
