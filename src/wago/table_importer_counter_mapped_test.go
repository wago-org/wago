//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func tableCounterModule(imported bool) []byte {
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})))}
	if imported {
		entry := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), 1, 0x70, 1, 1, 2)
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(entry)))
	}
	sections = append(sections, wasmtest.Section(3, []byte{1, 0}))
	if !imported {
		sections = append(sections, wasmtest.Section(4, []byte{1, 0x70, 1, 1, 2}))
	}
	return wasmtest.Module(append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("table", 1, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 17, 0x0b}))))...)
}

func tableCounterCompile(t *testing.T, imported, artifact bool) *Compiled {
	t.Helper()
	c := ownedTableCompile(t, tableCounterModule(imported))

	return c
}

func TestTableImporterCounterMappedRelease(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, callbackPanic := range []bool{false, true} {
			for _, counterOnly := range []bool{false, true} {
				t.Run(fmt.Sprintf("artifact%t-panic%t-counterOnly%t", artifact, callbackPanic, counterOnly), func(t *testing.T) {
					producer, err := Instantiate(tableCounterCompile(t, false, artifact))
					if err != nil {
						t.Fatal(err)
					}
					defer producer.Close()
					table, err := producer.ExportedTable("table")
					if err != nil || table.instanceOwner() != producer || producer.jm == nil || producer.ar == nil || producer.eng == nil {
						t.Fatal("fixture lacks actual mapped table ownership", err)
					}
					callbacks := 0
					unlocked := false
					marker := new(int)
					producer.referenceLifetime().afterPhysicalRelease(func() {
						callbacks++
						unlocked = table.owner.mu.TryLock()
						if unlocked {
							table.owner.mu.Unlock()
						}
						if callbackPanic {
							panic(marker)
						}
					})
					exact, _ := valueTypeDescriptorFromValType(ValFuncRef)
					if err := table.attachImporter(ValFuncRef, exact, nil, nil, false); err != nil {
						t.Fatal(err)
					}
					if err := producer.Close(); err != nil || producer.resourcesClosed || producer.resourceRefs != 1 || table.owner.importers != 1 {
						t.Fatal("logical Close lost its actual counted table root", err)
					}
					release := func() { table.detachImporter(nil) }
					if counterOnly {
						owner, consumed := table.detachImporterCounter(nil)
						if !consumed || owner != producer || table.owner.importers != 0 || producer.resourceRefs != 1 || producer.resourcesClosed || producer.ar == nil || callbacks != 0 {
							t.Fatal("counter consumption released its actual producer too early")
						}
						release = owner.releaseResourceRoot
					}
					value, panicked := cleanupTestPanic(release)
					if panicked != callbackPanic || callbackPanic && value != marker || callbacks != 1 || !unlocked || !producer.resourcesClosed || producer.finalizing || producer.resourceRefs != 0 || producer.jm != nil || producer.ar != nil || producer.eng != nil || producer.nativeContext != 0 || !table.closed {
						t.Fatal("actual table root cleanup skipped storage or replaced callback panic", value, panicked, callbacks, unlocked)
					}
					if owner, consumed := table.detachImporterCounter(nil); owner != nil || consumed {
						t.Fatal("released table consumed another producer obligation")
					}
				})
			}
		}
	}
}

func TestTableImporterCounterRuntimeCloseRetainsTopologyUntilPhysicalDetach(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime(WithRuntimeConfig(ownedTableConfig()))
			defer rt.Close()
			table, err := NewTable(1, 2)
			if err != nil {
				t.Fatal(err)
			}
			defer table.Close()
			if table.owner.store != nil {
				t.Fatal("NewTable fixture must not fabricate store-object ownership")
			}
			mod, err := rt.AdoptModule(tableCounterCompile(t, true, artifact))
			if err != nil {
				t.Fatal(err)
			}
			defer mod.Close()
			consumer, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports().Table("env", "table", table)))
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			if got, err := consumer.Invoke("run"); err != nil || len(got) != 1 || got[0] != 17 {
				t.Fatal("actual Runtime table importer failed execution", got, err)
			}
			store, topology := rt.refStore, rt.refStore.gcDomains
			if topology == nil || table.owner.importers != 1 || table.owner.funcrefStores[store] != 1 || topology.funcrefTables[table] != 1 {
				t.Fatal("actual importer lacks all three exact counter units")
			}
			// An explicit real root isolates delayed physical cleanup from token
			// issuance. Runtime shutdown may retire entries after Close/quiescence.
			if !consumer.retainResourceRoot() {
				t.Fatal("cannot retain actual Runtime importer")
			}
			held := true
			defer func() {
				if held {
					consumer.releaseResourceRoot()
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := rt.CloseContext(ctx); err != nil || !consumer.closed || consumer.resourcesClosed || consumer.resourceRefs != 1 || consumer.jm == nil || consumer.ar == nil || consumer.eng == nil {
				t.Fatal("Runtime Close overtook an actual physical root", err)
			}
			store.mu.Lock()
			retired := store.canReleaseEntriesLocked()
			entry := store.instances[consumer]
			quiesced := entry != nil && entry.closeAccounted && entry.quiesced && !entry.resourcesReleased
			store.mu.Unlock()
			if !retired || !quiesced {
				t.Fatal("Runtime fixture did not reach actual token retirement before physical release")
			}
			topology.funcrefMu.Lock()
			count := topology.funcrefTables[table]
			topology.funcrefMu.Unlock()
			if count != 1 || table.owner.funcrefStores[store] != 1 || table.owner.importers != 1 {
				t.Fatal("logical/token retirement lost a physical table attachment", count)
			}
			consumer.releaseResourceRoot()
			held = false
			if !consumer.resourcesClosed || consumer.resourceRefs != 0 || consumer.jm != nil || consumer.ar != nil || consumer.eng != nil || consumer.nativeContext != 0 || table.owner.importers != 0 || len(table.owner.funcrefStores) != 0 || len(topology.funcrefTables) != 0 {
				t.Fatalf("physical teardown: closed=%t refs=%d jm=%t arena=%t engine=%t context=%#x importers=%d stores=%v topology=%v", consumer.resourcesClosed, consumer.resourceRefs, consumer.jm != nil, consumer.ar != nil, consumer.eng != nil, consumer.nativeContext, table.owner.importers, table.owner.funcrefStores, topology.funcrefTables)
			}
			if owner, consumed := table.detachImporterCounter(store); owner != nil || consumed {
				t.Fatal("late duplicate detach fabricated another store unit")
			}
			if err := table.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
