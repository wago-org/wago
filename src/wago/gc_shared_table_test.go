//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package wago

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func gcSharedTableModule(imported bool) []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	setType := wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32})
	readType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	growType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	tableType := []byte{0x63, 0x00, 0x01, 0x01, 0x02} // (table 1 2 (ref null 0))

	setBody := []byte{
		0x00,
		0x20, 0x00, // index
		0x20, 0x01, 0xfb, 0x00, 0x00, // value; struct.new 0
		0x26, 0x00, // table.set 0
		0x41, 0x09, 0xfb, 0x00, 0x00, 0x1a, // allocation/collection after publication
		0x20, 0x00, 0x25, 0x00, // table.get 0
		0xfb, 0x02, 0x00, 0x00, // struct.get 0 0
		0x0b,
	}
	readBody := []byte{0x00, 0x20, 0x00, 0x25, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	growBody := []byte{
		0x00,
		0xd0, 0x00, // ref.null 0
		0x20, 0x00,
		0xfc, 0x0f, 0x00, // table.grow 0
		0x0b,
	}
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(structType, setType, readType, growType)),
	}
	if imported {
		entry := append(append(wasmtest.Name("provider"), wasmtest.Name("t")...), byte(wasm.ExternTable))
		entry = append(entry, tableType...)
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(entry)))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3))))
	if !imported {
		sections = append(sections, wasmtest.Section(4, wasmtest.Vec(tableType)))
	}
	exports := [][]byte{
		wasmtest.ExportEntry("set", byte(wasm.ExternFunc), 0),
		wasmtest.ExportEntry("read", byte(wasm.ExternFunc), 1),
		wasmtest.ExportEntry("grow", byte(wasm.ExternFunc), 2),
	}
	if !imported {
		exports = append(exports, wasmtest.ExportEntry("t", byte(wasm.ExternTable), 0))
	}
	sections = append(sections,
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(setBody))), setBody...),
			append(wasmtest.ULEB(uint32(len(readBody))), readBody...),
			append(wasmtest.ULEB(uint32(len(growBody))), growBody...),
		)),
	)
	return wasmtest.Module(sections...)
}

func TestGCSharedTableAliasesGrowthCollectionCodecAndCloseOrder(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	providerBase, err := Compile(cfg, gcSharedTableModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer providerBase.Close()
	consumerBase, err := Compile(cfg, gcSharedTableModule(true))
	if err != nil {
		t.Fatal(err)
	}
	defer consumerBase.Close()
	for _, codec := range []bool{false, true} {
		t.Run(map[bool]string{false: "compiled", true: "codec"}[codec], func(t *testing.T) {
			providerCode, consumerCode := providerBase, consumerBase
			if codec {
				providerCode = roundTripCompiled(t, providerBase)
				consumerCode = roundTripCompiled(t, consumerBase)
				defer providerCode.Close()
				defer consumerCode.Close()
			}
			if providerCode.genericGCFrameRoots() == nil || consumerCode.genericGCFrameRoots() == nil || !providerCode.sharedGCPersistentDomainSafe() || !consumerCode.sharedGCPersistentDomainSafe() {
				t.Fatalf("shared GC-table modules lost exact domain/root admission: provider roots=%v domain=%v consumer roots=%v domain=%v", providerCode.genericGCFrameRoots() != nil, providerCode.sharedGCPersistentDomainSafe(), consumerCode.genericGCFrameRoots() != nil, consumerCode.sharedGCPersistentDomainSafe())
			}
			store := newReferenceStore(false)
			defer store.closeRuntime()
			gcConfig := GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 192, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, TinyStepBudget: 1, VerifyAfterCollect: true, StressBarriers: true}
			provider, err := instantiateCore(providerCode, InstantiateOptions{GC: gcConfig, store: store})
			if err != nil {
				t.Fatal(err)
			}
			table, err := provider.ExportedTable("t")
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := instantiateCore(consumerCode, InstantiateOptions{GC: gcConfig, store: store, Imports: Imports{"provider.t": table}})
			if err != nil {
				t.Fatal(err)
			}
			if provider.gc == nil || provider.gc != consumer.gc {
				t.Fatal("shared GC-table instances do not share one collector")
			}
			if got, callErr := consumer.Invoke("set", 0, 55); callErr != nil || !reflect.DeepEqual(got, []uint64{55}) {
				t.Fatalf("consumer set slot 0 = %v, %v", got, callErr)
			}
			if got, callErr := provider.Invoke("read", 0); callErr != nil || !reflect.DeepEqual(got, []uint64{55}) {
				t.Fatalf("provider read slot 0 = %v, %v", got, callErr)
			}
			if got, callErr := consumer.Invoke("grow", 1); callErr != nil || !reflect.DeepEqual(got, []uint64{1}) {
				t.Fatalf("consumer grow = %v, %v", got, callErr)
			}
			if got, callErr := consumer.Invoke("set", 1, 77); callErr != nil || !reflect.DeepEqual(got, []uint64{77}) {
				t.Fatalf("consumer set slot 1 = %v, %v", got, callErr)
			}
			if got, callErr := provider.Invoke("read", 1); callErr != nil || !reflect.DeepEqual(got, []uint64{77}) {
				t.Fatalf("provider read slot 1 = %v, %v", got, callErr)
			}
			if err := provider.Close(); err != nil {
				t.Fatal(err)
			}
			if got, callErr := consumer.Invoke("read", 1); callErr != nil || !reflect.DeepEqual(got, []uint64{77}) {
				t.Fatalf("consumer after provider close = %v, %v", got, callErr)
			}
			if err := consumer.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
