//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package wago

import (
	"encoding/binary"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/wasmtest"
)

func gcMutableFuncrefTailProviderModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01} // (struct (field (mut i32)))
	targetType := []byte{0x60, 0x01, 0x63, 0x00, 0x01, 0x7f}
	voidType := wasmtest.FuncType(nil, nil)
	tableType := []byte{0x63, 0x01, 0x01, 0x01, 0x01} // (table 1 1 (ref null 1))
	target := func(add bool, collect bool) []byte {
		body := []byte{0x01, 0x01, 0x7f}
		if collect {
			body = append(body,
				0x02, 0x40, 0x03, 0x40,
				0x20, 0x01, 0x41, 0xe8, 0x07, 0x4f, 0x0d, 0x01,
				0xfb, 0x01, 0x00, 0x1a,
				0x20, 0x01, 0x41, 0x01, 0x6a, 0x21, 0x01, 0x0c, 0x00,
				0x0b, 0x0b,
			)
		}
		body = append(body, 0x20, 0x00, 0xfb, 0x02, 0x00, 0x00)
		if add {
			body = append(body, 0x41, 0xe4, 0x00, 0x6a) // i32.const 100
		}
		return append(body, 0x0b)
	}
	set := func(index byte) []byte {
		return []byte{0x41, 0x00, 0xd2, index, 0x26, 0x00, 0x0b}
	}
	codeWithLocals := func(body []byte) []byte {
		return append(wasmtest.ULEB(uint32(len(body))), body...)
	}
	declared := append([]byte{0x03, 0x00}, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, targetType, voidType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(2))),
		wasmtest.Section(4, wasmtest.Vec(tableType)),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x01, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("table", byte(wasm.ExternTable), 0),
			wasmtest.ExportEntry("memory", byte(wasm.ExternMem), 0),
			wasmtest.ExportEntry("set0", byte(wasm.ExternFunc), 2),
			wasmtest.ExportEntry("set1", byte(wasm.ExternFunc), 3),
		)),
		wasmtest.Section(9, wasmtest.Vec(declared)),
		wasmtest.Section(10, wasmtest.Vec(
			codeWithLocals(target(false, true)),
			codeWithLocals(target(true, false)),
			wasmtest.Code(set(0)),
			wasmtest.Code(set(1)),
		)),
	)
}

func gcMutableFuncrefTailConsumerModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	targetType := []byte{0x60, 0x01, 0x63, 0x00, 0x01, 0x7f}
	runType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	tableType := []byte{0x63, 0x01, 0x01, 0x01, 0x01}
	tableImport := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), byte(wasm.ExternTable))
	tableImport = append(tableImport, tableType...)
	memoryImport := append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), byte(wasm.ExternMem), 0x01, 0x01, 0x01)
	body := []byte{
		0x20, 0x00, 0xfb, 0x00, 0x00,
		0x41, 0x00, 0x25, 0x00,
		0x15, 0x01,
		0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, targetType, runType)),
		wasmtest.Section(2, wasmtest.Vec(tableImport, memoryImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func instanceNativeGCDomainID(in *Instance) uint64 {
	if in == nil || in.nativeContext == 0 {
		return 0
	}
	context := unsafe.Slice((*byte)(offHeapPtr(in.nativeContext)), coreruntime.InstanceContextBytes)
	return binary.LittleEndian.Uint64(context[coreruntime.InstanceContextGCDomainOffset:])
}

func gcTailStressConfig(profile GCProfile) GCConfig {
	return GCConfig{
		Profile:       profile,
		TinyHeapBytes: 256, TinyBlockBytes: 16, TinyCollectEveryAlloc: true,
		TinyStepEveryAlloc: true, TinyStepBudget: 1,
		ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096,
		StressNurseryBytes: 64, CollectEveryAlloc: true, ForceMajorEveryMinor: true,
		VerifyAfterCollect: true, StressBarriers: true,
	}
}

func TestNativeGCDomainIdentityDistinguishesPrivateCollectors(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcMutableFuncrefTailProviderModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	first, err := instantiateCore(compiled, InstantiateOptions{GC: gcTailStressConfig(GCProfileTiny)})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := instantiateCore(compiled, InstantiateOptions{GC: gcTailStressConfig(GCProfileTiny)})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	firstID, secondID := instanceNativeGCDomainID(first), instanceNativeGCDomainID(second)
	if firstID == 0 || secondID == 0 || firstID == secondID {
		t.Fatalf("private native GC-domain identities = %d, %d", firstID, secondID)
	}
}

func TestGCMutableImportedFuncrefTableReturnCallRef(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	providerCode, err := Compile(cfg, gcMutableFuncrefTailProviderModule())
	if err != nil {
		t.Fatal(err)
	}
	defer providerCode.Close()
	consumerCode, err := Compile(cfg, gcMutableFuncrefTailConsumerModule())
	if err != nil {
		t.Fatal(err)
	}
	defer consumerCode.Close()
	if providerCode.genericGCFrameRoots() == nil || consumerCode.genericGCFrameRoots() == nil {
		t.Fatal("mutable/imported funcref modules lost exact native root admission")
	}

	for _, codec := range []bool{false, true} {
		t.Run(map[bool]string{false: "compiled", true: "codec"}[codec], func(t *testing.T) {
			providerCandidate, consumerCandidate := providerCode, consumerCode
			if codec {
				providerCandidate = roundTripCompiled(t, providerCode)
				consumerCandidate = roundTripCompiled(t, consumerCode)
				providerCandidate.ensureCodeCache()
				consumerCandidate.ensureCodeCache()
				providerCandidate.codeCache.stagedFeatures |= CoreFeatureTailCall
				consumerCandidate.codeCache.stagedFeatures |= CoreFeatureTailCall
				defer providerCandidate.Close()
				defer consumerCandidate.Close()
			}
			for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
				profileName := map[GCProfile]string{GCProfileThroughput: "throughput", GCProfileTiny: "tiny"}[profile]
				t.Run(profileName, func(t *testing.T) {
					store := newReferenceStore(false)
					defer store.closeRuntime()
					gcConfig := gcTailStressConfig(profile)
					provider, err := instantiateCore(providerCandidate, InstantiateOptions{GC: gcConfig, store: store})
					if err != nil {
						t.Fatal(err)
					}
					table, err := provider.ExportedTable("table")
					if err != nil {
						provider.Close()
						t.Fatal(err)
					}
					memory, err := provider.ExportedMemory("memory")
					if err != nil {
						provider.Close()
						t.Fatal(err)
					}
					consumer, err := instantiateCore(consumerCandidate, InstantiateOptions{GC: gcConfig, store: store, Imports: Imports{"env.table": table, "env.memory": memory}})
					if err != nil {
						provider.Close()
						t.Fatal(err)
					}
					defer consumer.Close()
					defer provider.Close()
					providerDomain, consumerDomain := instanceNativeGCDomainID(provider), instanceNativeGCDomainID(consumer)
					if provider.jm.LinMemBase() != consumer.jm.LinMemBase() {
						t.Fatal("tail context test did not share one linear-memory base")
					}
					if provider.gc != consumer.gc || providerDomain == 0 || providerDomain != consumerDomain {
						t.Fatalf("mutable funcref tail collector/domain = %p/%d vs %p/%d", provider.gc, providerDomain, consumer.gc, consumerDomain)
					}
					for _, tc := range []struct {
						setter    string
						arg, want uint64
					}{{"set0", 37, 37}, {"set1", 41, 141}, {"set0", 52, 52}} {
						if _, err := provider.Invoke(tc.setter); err != nil {
							t.Fatal(err)
						}
						got, err := consumer.Invoke("run", tc.arg)
						if err != nil || !reflect.DeepEqual(got, []uint64{tc.want}) {
							t.Fatalf("%s run(%d) = %v, %v; want %d", tc.setter, tc.arg, got, err, tc.want)
						}
					}
					if err := provider.Close(); err != nil {
						t.Fatal(err)
					}
					if got, err := consumer.Invoke("run", 63); err != nil || !reflect.DeepEqual(got, []uint64{63}) {
						t.Fatalf("retained mutable-table producer = %v, %v; want 63", got, err)
					}
				})
			}
		})
	}
}

func TestGCMutableFuncrefTailRejectsForeignCollectorDomain(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	providerCode, err := Compile(cfg, gcMutableFuncrefTailProviderModule())
	if err != nil {
		t.Fatal(err)
	}
	defer providerCode.Close()
	consumerCode, err := Compile(cfg, gcMutableFuncrefTailConsumerModule())
	if err != nil {
		t.Fatal(err)
	}
	defer consumerCode.Close()

	providerStore, consumerStore := newReferenceStore(false), newReferenceStore(false)
	defer providerStore.closeRuntime()
	defer consumerStore.closeRuntime()
	gcConfig := gcTailStressConfig(GCProfileTiny)
	provider, err := instantiateCore(providerCode, InstantiateOptions{GC: gcConfig, store: providerStore})
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	if _, err := provider.Invoke("set0"); err != nil {
		t.Fatal(err)
	}
	table, err := provider.ExportedTable("table")
	if err != nil {
		t.Fatal(err)
	}
	memory, err := provider.ExportedMemory("memory")
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := instantiateCore(consumerCode, InstantiateOptions{GC: gcConfig, store: consumerStore, Imports: Imports{"env.table": table, "env.memory": memory}})
	if err == nil {
		consumer.Close()
		t.Fatal("foreign Runtime funcref table unexpectedly instantiated")
	}
	if !strings.Contains(err.Error(), "same Runtime GC domain") {
		t.Fatalf("foreign-domain funcref table error = %v", err)
	}
}

func BenchmarkGCMutableImportedFuncrefTableReturnCallRef(b *testing.B) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	providerCode, err := Compile(cfg, gcMutableFuncrefTailProviderModule())
	if err != nil {
		b.Fatal(err)
	}
	defer providerCode.Close()
	consumerCode, err := Compile(cfg, gcMutableFuncrefTailConsumerModule())
	if err != nil {
		b.Fatal(err)
	}
	defer consumerCode.Close()
	store := newReferenceStore(false)
	defer store.closeRuntime()
	provider, err := instantiateCore(providerCode, InstantiateOptions{store: store})
	if err != nil {
		b.Fatal(err)
	}
	defer provider.Close()
	if _, err := provider.Invoke("set1"); err != nil {
		b.Fatal(err)
	}
	table, err := provider.ExportedTable("table")
	if err != nil {
		b.Fatal(err)
	}
	memory, err := provider.ExportedMemory("memory")
	if err != nil {
		b.Fatal(err)
	}
	consumer, err := instantiateCore(consumerCode, InstantiateOptions{store: store, Imports: Imports{"env.table": table, "env.memory": memory}})
	if err != nil {
		b.Fatal(err)
	}
	defer consumer.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		want := uint64(i) + 100
		if got, err := consumer.Invoke("run", uint64(i)); err != nil || len(got) != 1 || got[0] != want {
			b.Fatalf("run(%d) = %v, %v; want %d", i, got, err, want)
		}
	}
}
