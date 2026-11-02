//go:build linux && (amd64 || arm64) && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func h2ReceiptFixture(t *testing.T, store *referenceStore, shift uint8) (*Instance, *Instance, *gcRefTestTableState, uint64) {
	t.Helper()
	providerCode, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), wasmtest.Module(
		wasmtest.Section(4, wasmtest.Vec([]byte{0x6f, 0, 2})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("slots", 1, 0)))))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = providerCode.Close() })
	provider, err := instantiateCore(providerCode, InstantiateOptions{store: store})
	if err != nil {
		t.Fatal(err)
	}
	code, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, []byte{0x60, 0, 0})),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x6f, 1, 0xd0, 0x6f, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 7, 0xfb, 0, 0, 0xfb, 0x1b, 0x24, 0, 0x0b})))))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = code.Close() })
	publisher, err := instantiateCore(code, InstantiateOptions{store: store, GC: GCConfig{NurseryBytes: 4096, SurvivorBytes: 4096, NurseryRegionShift: shift, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = publisher.Invoke("run"); err != nil {
		t.Fatal(err)
	}
	conversion := publisher.existingGCExternConversionState()
	if conversion == nil || conversion.core == nil || conversion.count != 1 || !publisher.hasPhysicalResources() {
		t.Fatal("STOP missing publisher conversion authority")
	}
	entry := conversion.entries[0]
	if !entry.hasRoot || entry.externWord>>32 == 0 {
		t.Fatal("STOP conversion is not a checked opaque identity")
	}
	if ref, e := publisher.gc.CheckedTableSlot(entry.rootSlot); e != nil || ref != entry.ref {
		t.Fatal("STOP missing floating conversion root")
	}
	exported, err := provider.ExportedTable("slots")
	if err != nil {
		t.Fatal(err)
	}
	container, err := newGCConversionContainer(provider)
	if err != nil {
		t.Fatal(err)
	}
	table := &gcRefTestTableState{Descriptors: [][]byte{exported.desc}, Conversion: conversion, TableCount: 1, kinds: []gcRefTableKind{gcRefTableExtern}, entryBytes: []int{8}, conversionContainers: []*gcConversionContainer{container}}
	return publisher, provider, table, entry.externWord
}

// Directly exercise the production mirrored-table transaction on the provider's
// real exported descriptor. Backend imported/global stores are a subsequent hook.
func h2TableSet(t *testing.T, table *gcRefTestTableState, index, word uint64) {
	t.Helper()
	store := table.conversionContainers[0].store
	topology := store.ensureGCTopology()
	topology.RLock()
	store.mu.Lock()
	var domains gcInvocationDomainSet
	for d := topology.first; d != nil; d = d.next {
		domains.add(d)
	}
	store.mu.Unlock()
	domains.sort()
	view := gcInvocationDomainView{set: &domains}
	view.lock()
	id := newInvocationID()
	view.claim(id)
	nativeExecutionMu.Lock()
	err := table.setTable(domains.at(0).collector, 0, index, word)
	nativeExecutionMu.Unlock()
	view.release(id)
	view.unlock()
	topology.RUnlock()
	if err != nil {
		t.Fatal(err)
	}
	drainManagedFunctionStoreReclaims(store)
}
func h2AssertReceipt(t *testing.T, c *gcConversionContainer, index uint64) *gcConversionReceipt {
	t.Helper()
	r := c.slots[index]
	if r == nil || r.released || r.identity.domain == nil || !c.owner.hasPhysicalResources() {
		t.Fatal("STOP missing live physical receipt")
	}
	store := c.store
	store.mu.Lock()
	registered := store.gcDomains.domainForCollectorLocked(r.identity.domain.collector) == r.identity.domain && r.identity.domain.claims > 0
	store.mu.Unlock()
	if !registered {
		t.Fatal("STOP missing independent domain claim")
	}
	if ref, err := r.identity.domain.collector.CheckedTableSlot(r.root); err != nil || ref != r.identity.ref {
		t.Fatal("STOP receipt root mismatch")
	}
	return r
}
func TestR9H2ReceiptPublisherRetirement(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		t.Run(fmt.Sprintf("region=%d", shift), func(t *testing.T) {
			store := newReferenceStore(false)
			publisher, provider, table, word := h2ReceiptFixture(t, store, shift)
			h2TableSet(t, table, 0, word)
			h2TableSet(t, table, 1, word)
			c := table.conversionContainers[0]
			a := h2AssertReceipt(t, c, 0)
			b := h2AssertReceipt(t, c, 1)
			if a.root == b.root || a.identity != b.identity || len(a.identity.receipts) != 2 {
				t.Fatal("STOP aliases do not have distinct roots")
			}
			if err := publisher.Close(); err != nil {
				t.Fatal(err)
			}
			if publisher.hasPhysicalResources() || !table.Conversion.closed {
				t.Fatal("publisher retained by receipt")
			}
			h2AssertReceipt(t, c, 0)
			h2AssertReceipt(t, c, 1)
			value, ok := managedFunctionGraphs.Load(store)
			if !ok {
				t.Fatal("STOP missing receipt-only graph")
			}
			g := value.(*managedFunctionGraph)
			g.pendingCollection.Store(true)
			g.drainAfterGate(0)
			if g.pendingCollection.Load() {
				t.Fatal("receipt-only collection not serviced")
			}
			h2AssertReceipt(t, c, 0)
			// The publisher's conversion state is closed. Existing opaque identity and
			// physical receipt remain sufficient to clear both aliases independently.
			h2TableSet(t, table, 0, 0)
			h2AssertReceipt(t, c, 1)
			h2TableSet(t, table, 1, 0)
			if len(c.slots) != 0 || len(store.conversions) != 0 || store.gcDomainForCollector(a.identity.domain.collector) != nil {
				t.Fatal("final receipt did not retire its domain")
			}
			if binary.LittleEndian.Uint64(table.Descriptors[0][8:]) != 0 {
				t.Fatal("visible clear missing")
			}
			if err := provider.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
