//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package wago

import (
	"reflect"
	"strings"
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

func gcSharedMultiTableModule(imported bool) []byte {
	struct0 := []byte{0x5f, 0x01, 0x7f, 0x01}
	struct1 := []byte{0x5f, 0x02, 0x7f, 0x01, 0x7f, 0x01}
	setType := wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32})
	indexType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	table0 := []byte{0x63, 0x00, 0x01, 0x01, 0x02} // (table 1 2 (ref null 0))
	table1 := []byte{0x63, 0x01, 0x01, 0x01, 0x02} // (table 1 2 (ref null 1))
	set0 := []byte{
		0x00,
		0x20, 0x00, 0x20, 0x01, 0xfb, 0x00, 0x00, 0x26, 0x00,
		0x41, 0x09, 0xfb, 0x00, 0x00, 0x1a,
		0x20, 0x00, 0x25, 0x00, 0xfb, 0x02, 0x00, 0x00,
		0x0b,
	}
	set1 := []byte{
		0x00,
		0x20, 0x00, 0x20, 0x01, 0x20, 0x01, 0xfb, 0x00, 0x01, 0x26, 0x01,
		0x41, 0x0b, 0xfb, 0x00, 0x00, 0x1a,
		0x20, 0x00, 0x25, 0x01, 0xfb, 0x02, 0x01, 0x00,
		0x0b,
	}
	read0 := []byte{0x00, 0x20, 0x00, 0x25, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	read1 := []byte{0x00, 0x20, 0x00, 0x25, 0x01, 0xfb, 0x02, 0x01, 0x00, 0x0b}
	grow1 := []byte{0x00, 0xd0, 0x01, 0x20, 0x00, 0xfc, 0x0f, 0x01, 0x0b}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(struct0, struct1, setType, indexType))}
	if imported {
		entry0 := append(append(wasmtest.Name("provider"), wasmtest.Name("t0")...), byte(wasm.ExternTable))
		entry0 = append(entry0, table0...)
		entry1 := append(append(wasmtest.Name("provider"), wasmtest.Name("t1")...), byte(wasm.ExternTable))
		entry1 = append(entry1, table1...)
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(entry0, entry1)))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2), wasmtest.ULEB(2), wasmtest.ULEB(3), wasmtest.ULEB(3), wasmtest.ULEB(3))))
	if !imported {
		sections = append(sections, wasmtest.Section(4, wasmtest.Vec(table0, table1)))
	}
	exports := [][]byte{
		wasmtest.ExportEntry("set0", byte(wasm.ExternFunc), 0),
		wasmtest.ExportEntry("set1", byte(wasm.ExternFunc), 1),
		wasmtest.ExportEntry("read0", byte(wasm.ExternFunc), 2),
		wasmtest.ExportEntry("read1", byte(wasm.ExternFunc), 3),
		wasmtest.ExportEntry("grow1", byte(wasm.ExternFunc), 4),
	}
	if !imported {
		exports = append(exports,
			wasmtest.ExportEntry("t0", byte(wasm.ExternTable), 0),
			wasmtest.ExportEntry("t1", byte(wasm.ExternTable), 1),
		)
	}
	sections = append(sections,
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(set0))), set0...),
			append(wasmtest.ULEB(uint32(len(set1))), set1...),
			append(wasmtest.ULEB(uint32(len(read0))), read0...),
			append(wasmtest.ULEB(uint32(len(read1))), read1...),
			append(wasmtest.ULEB(uint32(len(grow1))), grow1...),
		)),
	)
	return wasmtest.Module(sections...)
}

func TestGCMultipleLocalTablesCollectWithPrivateStore(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcSharedMultiTableModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if compiled.genericGCFrameRoots() == nil || !validCompiledGCFunctionTables(compiled) {
		t.Fatal("multiple local collector tables lost exact frame-root admission")
	}
	in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 192, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, TinyStepBudget: 1, VerifyAfterCollect: true, StressBarriers: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for name, args := range map[string][]uint64{"set0": {0, 41}, "set1": {0, 43}} {
		got, callErr := in.Invoke(name, args...)
		if callErr != nil || !reflect.DeepEqual(got, []uint64{args[1]}) {
			t.Fatalf("private %s = %v, %v", name, got, callErr)
		}
	}
	for name, want := range map[string]uint64{"read0": 41, "read1": 43} {
		got, callErr := in.Invoke(name, 0)
		if callErr != nil || !reflect.DeepEqual(got, []uint64{want}) {
			t.Fatalf("private %s = %v, %v", name, got, callErr)
		}
	}
}

func TestGCSharedMultipleHeterogeneousTables(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	providerBase, err := Compile(cfg, gcSharedMultiTableModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer providerBase.Close()
	consumerBase, err := Compile(cfg, gcSharedMultiTableModule(true))
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
			if providerCode.tableCount() != 2 || consumerCode.tableCount() != 2 || providerCode.genericGCFrameRoots() == nil || consumerCode.genericGCFrameRoots() == nil || !providerCode.sharedGCPersistentDomainSafe() || !consumerCode.sharedGCPersistentDomainSafe() {
				t.Fatalf("multi-table GC domain/root admission failed: provider tables=%d roots=%v domain=%v consumer tables=%d roots=%v domain=%v", providerCode.tableCount(), providerCode.genericGCFrameRoots() != nil, providerCode.sharedGCPersistentDomainSafe(), consumerCode.tableCount(), consumerCode.genericGCFrameRoots() != nil, consumerCode.sharedGCPersistentDomainSafe())
			}
			store := newReferenceStore(false)
			defer store.closeRuntime()
			gcConfig := GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, TinyStepBudget: 1, VerifyAfterCollect: true, StressBarriers: true}
			provider, err := instantiateCore(providerCode, InstantiateOptions{GC: gcConfig, store: store})
			if err != nil {
				t.Fatal(err)
			}
			table0, err := provider.ExportedTable("t0")
			if err != nil {
				provider.Close()
				t.Fatal(err)
			}
			table1, err := provider.ExportedTable("t1")
			if err != nil {
				provider.Close()
				t.Fatal(err)
			}
			consumer, err := instantiateCore(consumerCode, InstantiateOptions{GC: gcConfig, store: store, Imports: Imports{"provider.t0": table0, "provider.t1": table1}})
			if err != nil {
				provider.Close()
				t.Fatal(err)
			}
			if provider.gc == nil || provider.gc != consumer.gc {
				consumer.Close()
				provider.Close()
				t.Fatal("multi-table instances do not share one collector")
			}
			for name, args := range map[string][]uint64{"set0": {0, 11}, "set1": {0, 22}} {
				got, callErr := consumer.Invoke(name, args...)
				if callErr != nil || !reflect.DeepEqual(got, []uint64{args[1]}) {
					consumer.Close()
					provider.Close()
					t.Fatalf("consumer %s = %v, %v", name, got, callErr)
				}
			}
			if got, callErr := consumer.Invoke("grow1", 1); callErr != nil || !reflect.DeepEqual(got, []uint64{1}) {
				consumer.Close()
				provider.Close()
				t.Fatalf("consumer grow1 = %v, %v", got, callErr)
			}
			if got, callErr := consumer.Invoke("set1", 1, 33); callErr != nil || !reflect.DeepEqual(got, []uint64{33}) {
				consumer.Close()
				provider.Close()
				t.Fatalf("consumer set1 grown slot = %v, %v", got, callErr)
			}
			for name, args := range map[string][]uint64{"read0": {0, 11}, "read1": {0, 22}} {
				got, callErr := provider.Invoke(name, args[0])
				if callErr != nil || !reflect.DeepEqual(got, []uint64{args[1]}) {
					consumer.Close()
					provider.Close()
					t.Fatalf("provider %s = %v, %v", name, got, callErr)
				}
			}
			if got, callErr := provider.Invoke("read1", 1); callErr != nil || !reflect.DeepEqual(got, []uint64{33}) {
				consumer.Close()
				provider.Close()
				t.Fatalf("provider read1 grown slot = %v, %v", got, callErr)
			}
			if err := provider.Close(); err != nil {
				consumer.Close()
				t.Fatal(err)
			}
			if got, callErr := consumer.Invoke("read1", 1); callErr != nil || !reflect.DeepEqual(got, []uint64{33}) {
				consumer.Close()
				t.Fatalf("consumer after producer close = %v, %v", got, callErr)
			}
			if err := consumer.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGCSharedMultipleTableAttachmentRollback(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	providerCode, err := Compile(cfg, gcSharedMultiTableModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer providerCode.Close()
	consumerCode, err := Compile(cfg, gcSharedMultiTableModule(true))
	if err != nil {
		t.Fatal(err)
	}
	defer consumerCode.Close()
	storeA, storeB := newReferenceStore(false), newReferenceStore(false)
	defer storeA.closeRuntime()
	defer storeB.closeRuntime()
	gcConfig := GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 16}
	providerA, err := instantiateCore(providerCode, InstantiateOptions{GC: gcConfig, store: storeA})
	if err != nil {
		t.Fatal(err)
	}
	defer providerA.Close()
	providerB, err := instantiateCore(providerCode, InstantiateOptions{GC: gcConfig, store: storeB})
	if err != nil {
		t.Fatal(err)
	}
	defer providerB.Close()
	table0, err := providerA.ExportedTable("t0")
	if err != nil {
		t.Fatal(err)
	}
	table1, err := providerB.ExportedTable("t1")
	if err != nil {
		t.Fatal(err)
	}
	if in, err := instantiateCore(consumerCode, InstantiateOptions{GC: gcConfig, store: storeA, Imports: Imports{"provider.t0": table0, "provider.t1": table1}}); err == nil {
		in.Close()
		t.Fatal("cross-domain second table import unexpectedly succeeded")
	} else if got := err.Error(); !strings.Contains(got, "same Runtime GC domain") {
		t.Fatalf("cross-domain second table error = %v", err)
	}
	for i, table := range []*Table{table0, table1} {
		table.owner.mu.Lock()
		importers := table.owner.importers
		table.owner.mu.Unlock()
		if importers != 0 {
			t.Fatalf("table %d retained %d importer roots after rollback", i, importers)
		}
	}
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
