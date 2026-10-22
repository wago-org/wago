//go:build (linux && (amd64 || arm64 || riscv64)) || (darwin && arm64)

package wago

import (
	"encoding/binary"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	native "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
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

func gcSharedMultiGlobalProviderModule() []byte {
	firstType := []byte{0x5f, 0x01, 0x7f, 0x01}
	secondType := []byte{0x5f, 0x02, 0x7f, 0x01, 0x7f, 0x01}
	readType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	firstGlobal := []byte{
		0x64, 0x00, 0x01,
		0x41, 0x2a, 0xfb, 0x00, 0x00,
		0x0b,
	}
	secondGlobal := []byte{
		0x64, 0x01, 0x01,
		0x41, 0x2b, 0x41, 0x63, 0xfb, 0x00, 0x01,
		0x0b,
	}
	body := []byte{0x00, 0x23, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(firstType, secondType, readType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(6, wasmtest.Vec(firstGlobal, secondGlobal)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("g0", byte(wasm.ExternGlobal), 0),
			wasmtest.ExportEntry("g1", byte(wasm.ExternGlobal), 1),
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

func gcSharedI31GlobalModule(imported bool) []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	readType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	body := []byte{
		0x00,
		0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a,
		0x23, 0x00, 0xfb, 0x1e,
		0x0b,
	}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(structType, readType))}
	if imported {
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("provider", "g", wasm.I31Ref, true))))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))))
	if !imported {
		sections = append(sections, wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I31Ref, true, []byte{0x41, 0x07, 0xfb, 0x1c, 0x0b}))))
	}
	sections = append(sections,
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("g", byte(wasm.ExternGlobal), 0),
			wasmtest.ExportEntry("read", byte(wasm.ExternFunc), 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	return wasmtest.Module(sections...)
}

func sharedGlobalDomainCount(store *referenceStore) int {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.gcDomains == nil {
		return 0
	}
	return store.gcDomains.n
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
	consumer, err := instantiateCore(consumerCode, InstantiateOptions{GC: gcConfig, store: store, Imports: testImports("provider.g", global)})
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
	providerCode := publicArtifactRoundTrip(t, providerBase)
	consumerCode := publicArtifactRoundTrip(t, consumerBase)
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
	consumer, err := instantiateCore(consumerCode, InstantiateOptions{GC: gcConfig, store: store, Imports: testImports("provider.g", global)})
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
	if _, err := instantiateCore(consumerCode, InstantiateOptions{GC: gcConfig, store: store, Imports: testImports("provider.g", distinctGlobal)}); err == nil {
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
	if !providerCode.sharedGCPersistentDomainSafe() || !consumerCode.sharedGCPersistentDomainSafe() || providerCode.genericGCFrameRoots() == nil || consumerCode.genericGCFrameRoots() == nil {
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
	consumer, err := instantiateCore(consumerCode, InstantiateOptions{GC: gcConfig, store: store, Imports: testImports("provider.g", global)})
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

func TestGCSharedGlobalImportRejectsIllTypedCompactRows(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	compile := func(source []byte) *Compiled {
		t.Helper()
		compiled, err := Compile(cfg, source)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = compiled.Close() })
		return compiled
	}
	aggregateProviderCode := compile(gcSharedGlobalProviderModule(true))
	aggregateConsumerCode := compile(gcSharedImmutableGlobalConsumerModule(true))
	multiProviderCode := compile(gcSharedMultiGlobalProviderModule())
	i31ProviderCode := compile(gcSharedI31GlobalModule(false))
	i31ConsumerCode := compile(gcSharedI31GlobalModule(true))
	gcConfig := GCConfig{DisableCollection: true}
	expectRejected := func(code *Compiled, store *referenceStore, global *Global) {
		t.Helper()
		in, err := instantiateCore(code, InstantiateOptions{GC: gcConfig, store: store, Imports: testImports("provider.g", global)})
		if in != nil {
			_ = in.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "incompatible exact structural type") {
			t.Fatalf("ill-typed compact global import error=%v", err)
		}
	}

	t.Run("aggregate-from-i31", func(t *testing.T) {
		store := newReferenceStore(false)
		defer store.closeRuntime()
		provider, err := instantiateCore(aggregateProviderCode, InstantiateOptions{GC: gcConfig, store: store})
		if err != nil {
			t.Fatal(err)
		}
		defer provider.Close()
		global, err := provider.ExportedGlobalObject("g")
		if err != nil {
			t.Fatal(err)
		}
		binary.LittleEndian.PutUint64(global.cell, uint64(native.I31New(7)))
		expectRejected(aggregateConsumerCode, store, global)
	})

	t.Run("nonnull-aggregate-from-null", func(t *testing.T) {
		store := newReferenceStore(false)
		defer store.closeRuntime()
		provider, err := instantiateCore(aggregateProviderCode, InstantiateOptions{GC: gcConfig, store: store})
		if err != nil {
			t.Fatal(err)
		}
		defer provider.Close()
		global, err := provider.ExportedGlobalObject("g")
		if err != nil {
			t.Fatal(err)
		}
		binary.LittleEndian.PutUint64(global.cell, uint64(native.Null()))
		expectRejected(aggregateConsumerCode, store, global)
	})

	t.Run("aggregate-from-wrong-nominal-object", func(t *testing.T) {
		store := newReferenceStore(false)
		defer store.closeRuntime()
		provider, err := instantiateCore(multiProviderCode, InstantiateOptions{GC: gcConfig, store: store})
		if err != nil {
			t.Fatal(err)
		}
		defer provider.Close()
		global, err := provider.ExportedGlobalObject("g0")
		if err != nil {
			t.Fatal(err)
		}
		distinctGlobal, err := provider.ExportedGlobalObject("g1")
		if err != nil {
			t.Fatal(err)
		}
		copy(global.cell, distinctGlobal.cell)
		expectRejected(aggregateConsumerCode, store, global)
	})

	t.Run("i31-from-object", func(t *testing.T) {
		store := newReferenceStore(false)
		defer store.closeRuntime()
		aggregate, err := instantiateCore(aggregateProviderCode, InstantiateOptions{GC: gcConfig, store: store})
		if err != nil {
			t.Fatal(err)
		}
		defer aggregate.Close()
		i31, err := instantiateCore(i31ProviderCode, InstantiateOptions{GC: gcConfig, store: store})
		if err != nil {
			t.Fatal(err)
		}
		defer i31.Close()
		if aggregate.gc != i31.gc {
			t.Fatal("test providers did not share one collector")
		}
		aggregateGlobal, err := aggregate.ExportedGlobalObject("g")
		if err != nil {
			t.Fatal(err)
		}
		i31Global, err := i31.ExportedGlobalObject("g")
		if err != nil {
			t.Fatal(err)
		}
		copy(i31Global.cell, aggregateGlobal.cell)
		expectRejected(i31ConsumerCode, store, i31Global)
	})
}
