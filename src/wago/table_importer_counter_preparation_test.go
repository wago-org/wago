//go:build linux && (amd64 || arm64 || (riscv64 && wago_rv64_experimental)) && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"testing"
	"unsafe"
)

func counterTable(typ ValType, producer *Instance, store *referenceStore, count int) *Table {
	return &Table{desc: make([]byte, 8), owner: &tableOwner{instance: producer, elementType: typ, store: store, importers: count}}
}

func TestTableImporterCounterBoundaryAndRootBalance(t *testing.T) {
	for _, typ := range []ValType{ValFuncRef, ValExternRef} {
		for _, instanceOwned := range []bool{false, true} {
			for _, count := range []int{-1, 0, 254, 255, 256, int(^uint32(0)) - 1, int(^uint32(0)), int(^uint32(0)) + 1, maxInt() - 1, maxInt()} {
				t.Run(fmt.Sprintf("%s-instance%t-count%d", typ, instanceOwned, count), func(t *testing.T) {
					// Counts/descriptors and unmapped owners are synthetic. Actual
					// retainer/counter operations are balanced here; mapped tests
					// separately establish executable and physical cleanup behavior.
					var producer *Instance
					if instanceOwned {
						producer = &Instance{}
					}
					store := newReferenceStore(true)
					table := counterTable(typ, producer, store, count)
					exact, _ := valueTypeDescriptorFromValType(typ)
					err := table.attachImporter(typ, exact, nil, store, false)
					if count < 0 || count == maxInt() {
						if err == nil || table.owner.importers != count || producer != nil && producer.resourceRefs != 0 || len(table.owner.funcrefStores) != 0 || store.gcDomains != nil {
							t.Fatal("denied count acquired an owner/store/topology unit", err)
						}
						return
					}
					if err != nil || table.owner.importers != count+1 || producer != nil && producer.resourceRefs != 1 {
						t.Fatal("successful acquisition did not own exactly one count/root", err)
					}
					if count == maxInt()-1 {
						if err := table.attachImporter(typ, exact, nil, store, false); err == nil || table.owner.importers != maxInt() || producer != nil && producer.resourceRefs != 1 {
							t.Fatal("maximum/+1 changed a successful acquisition", err)
						}
					}
					if owner, consumed := table.detachImporterCounter(newReferenceStore(true)); owner != nil || consumed || table.owner.importers != count+1 {
						t.Fatal("wrong store consumed another store's acquisition")
					}
					owner, consumed := table.detachImporterCounter(store)
					if !consumed || owner != producer || table.owner.importers != count || producer != nil && producer.resourceRefs != 1 {
						t.Fatal("counter-only operation consumed an incorrect unit or released storage")
					}
					if owner != nil {
						owner.releaseResourceRoot()
					}
					if producer != nil && producer.resourceRefs != 0 || len(table.owner.funcrefStores) != 0 || store.gcDomains != nil && len(store.gcDomains.funcrefTables) != 0 {
						t.Fatal("exact count/root/store/topology unit did not drain")
					}
					table.owner.importers = 0 // discard only synthetic background count
					if owner, consumed := table.detachImporterCounter(store); owner != nil || consumed {
						t.Fatal("repeated detach fabricated a new obligation")
					}
				})
			}
		}
	}
}

func TestTableImporterCounterHostTopologyLimits(t *testing.T) {
	for _, exhausted := range []string{"store", "topology"} {
		t.Run(exhausted, func(t *testing.T) {
			store := newReferenceStore(true)
			topology := store.ensureGCTopology()
			topology.funcrefGCActive = true
			table := counterTable(ValFuncRef, nil, nil, 0)
			table.owner.funcrefStores = map[*referenceStore]uint32{store: 0}
			topology.funcrefTables = map[*Table]uint32{table: 0}
			if exhausted == "store" {
				table.owner.funcrefStores[store] = ^uint32(0)
			} else {
				topology.funcrefTables[table] = ^uint32(0)
			}
			beforeStore, beforeTopology := table.owner.funcrefStores[store], topology.funcrefTables[table]
			exact, _ := valueTypeDescriptorFromValType(ValFuncRef)
			if err := table.attachImporter(ValFuncRef, exact, nil, store, false); err == nil || table.owner.importers != 0 || table.owner.funcrefGCStore != nil || table.owner.funcrefStores[store] != beforeStore || topology.funcrefTables[table] != beforeTopology {
				t.Fatal("denied store/topology counter changed GC binding or another unit", err)
			}
		})
	}
}

func TestTableImporterCounterAliasesAndStoreMismatch(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		t.Run(fmt.Sprintf("duplicate%t", duplicate), func(t *testing.T) {
			firstStore, otherStore := newReferenceStore(true), newReferenceStore(true)
			first, other := counterTable(ValFuncRef, nil, nil, 0), counterTable(ValFuncRef, nil, nil, 0)
			if duplicate {
				other = first
			}
			exact, _ := valueTypeDescriptorFromValType(ValFuncRef)
			var batch tableImportAttachments
			if err := batch.attach(first, ValFuncRef, exact, nil, firstStore, nil, false); err != nil {
				t.Fatal(err)
			}
			if err := batch.attach(other, ValFuncRef, exact, nil, otherStore, nil, false); err == nil || batch.store != firstStore || batch.set.n != 1 || first.owner.importers != 1 || !duplicate && other.owner.importers != 0 || otherStore.gcDomains != nil || len(other.owner.funcrefStores) != map[bool]int{false: 0, true: 1}[duplicate] {
				t.Fatal("store mismatch bypassed dedup or acquired an unrecorded unit", err)
			}
			first.owner.importers = maxInt() // a duplicate does not acquire a unit
			if err := batch.attach(first, ValFuncRef, exact, nil, firstStore, nil, false); err != nil || batch.set.n != 1 || first.owner.importers != maxInt() || first.owner.funcrefStores[firstStore] != 1 || firstStore.gcDomains.funcrefTables[first] != 1 {
				t.Fatal("full-count duplicate validation acquired or rejected a unit", err)
			}
			first.owner.importers = 1
			batch.detachAll()
			batch.detachAll()
			if first.owner.importers != 0 || len(first.owner.funcrefStores) != 0 || len(firstStore.gcDomains.funcrefTables) != 0 || batch.set.n != 0 || batch.store != nil {
				t.Fatal("exact batch store obligations did not drain once")
			}
		})
	}
}

func TestTableImporterCounterMissingActiveTopologyDoesNotConsume(t *testing.T) {
	store := newReferenceStore(true)
	table := counterTable(ValFuncRef, nil, nil, 0)
	exact, _ := valueTypeDescriptorFromValType(ValFuncRef)
	if err := table.attachImporter(ValFuncRef, exact, nil, store, false); err != nil {
		t.Fatal(err)
	}
	topology := store.gcDomains
	delete(topology.funcrefTables, table) // synthetic corruption of one actual unit
	if owner, consumed := table.detachImporterCounter(store); owner != nil || consumed || table.owner.importers != 1 || table.owner.funcrefStores[store] != 1 {
		t.Fatal("missing active topology unit falsely consumed its obligation")
	}
	topology.funcrefTables[table] = 1
	table.detachImporter(store)
	if table.owner.importers != 0 || len(table.owner.funcrefStores) != 0 || len(topology.funcrefTables) != 0 {
		t.Fatal("restored exact obligation did not drain")
	}
}

func TestTableImporterCounterFootprintAndWarmAllocation(t *testing.T) {
	if unsafe.Sizeof(tableImportAttachments{}) != unsafe.Sizeof(importDedup[*Table]{})+unsafe.Sizeof((*referenceStore)(nil)) || unsafe.Offsetof(tableImportAttachments{}.set) != 0 {
		t.Fatal("counter preparation changed attachment footprint")
	}
	table := counterTable(ValFuncRef, nil, nil, 0)
	store := newReferenceStore(true)
	exact, _ := valueTypeDescriptorFromValType(ValFuncRef)
	failed := false
	allocs := testing.AllocsPerRun(100, func() {
		var batch tableImportAttachments
		if batch.attach(table, ValFuncRef, exact, nil, store, nil, false) != nil || batch.attach(table, ValFuncRef, exact, nil, store, nil, false) != nil {
			failed = true
		}
		batch.detachAll()
		if table.owner.importers != 0 || len(table.owner.funcrefStores) != 0 || len(store.gcDomains.funcrefTables) != 0 {
			failed = true
		}
	})
	// Runtime-backed batches and direct attachments both allocate once on the
	// baseline. Keep those bounds while reusing all warmed ownership maps.
	if failed || allocs != 1 {
		t.Fatal("warmed batch changed its existing allocation bound", failed, allocs)
	}
	allocs = testing.AllocsPerRun(100, func() {
		if table.attachImporter(ValFuncRef, exact, nil, store, false) != nil {
			failed = true
		}
		table.detachImporter(store)
		if table.owner.importers != 0 || len(table.owner.funcrefStores) != 0 || len(store.gcDomains.funcrefTables) != 0 {
			failed = true
		}
	})
	if failed || allocs != 1 {
		t.Fatal("warmed direct attachment changed its allocation bound", failed, allocs)
	}
}
