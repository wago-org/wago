//go:build linux && (amd64 || arm64) && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"encoding/binary"
	"fmt"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func h2ReceiptFixture(t *testing.T, store *referenceStore, shift uint8) (*Instance, *Instance, *gcRefTestTableState, uint64) {
	t.Helper()
	providerCode, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), wasmtest.Module(
		wasmtest.Section(4, wasmtest.Vec([]byte{0x6f, 0, 2})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x6f, 1, 0xd0, 0x6f, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("slots", 1, 0), wasmtest.ExportEntry("g", 3, 0)))))
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
	if actual := reflect.ValueOf(publisher.gc).Elem().FieldByName("edenRegions").Len() != 0; actual != (shift != 0) {
		t.Fatal("STOP receipt fixture has wrong actual region policy")
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
	container, err := newGCConversionContainer(provider, exported.desc)
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
	if table.conversionContainers[0].slots[index] != nil {
		h2AssertReceipt(t, table.conversionContainers[0], index)
	}
	err := table.setTable(domains.at(0).collector, 0, index, word)
	if err == nil && table.conversionContainers[0].slots[index] != nil {
		h2AssertReceipt(t, table.conversionContainers[0], index)
	}
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
			if a.root == b.root || a.identity != b.identity || len(a.identity.receipts) != 3 {
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
			if len(c.slots) != 0 || len(store.gcDomains.conversions) != 0 || store.gcDomainForCollector(a.identity.domain.collector) != nil {
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

func h2WithAuthority(t *testing.T, store *referenceStore, f func()) {
	t.Helper()
	topology := store.ensureGCTopology()
	topology.RLock()
	store.mu.Lock()
	var set gcInvocationDomainSet
	for d := topology.first; d != nil; d = d.next {
		set.add(d)
	}
	store.mu.Unlock()
	set.sort()
	view := gcInvocationDomainView{set: &set}
	view.lock()
	id := newInvocationID()
	view.claim(id)
	nativeExecutionMu.Lock()
	defer func() { nativeExecutionMu.Unlock(); view.release(id); view.unlock(); topology.RUnlock() }()
	f()
}
func TestR9H2ReceiptIndependentContainersAndRollback(t *testing.T) {
	store := newReferenceStore(false)
	a, pa, ta, wa := h2ReceiptFixture(t, store, 0)
	b, pb, tb, wb := h2ReceiptFixture(t, store, 0)
	h2TableSet(t, ta, 0, wa)
	h2TableSet(t, tb, 0, wb)
	ca, cb := ta.conversionContainers[0], tb.conversionContainers[0]
	aliasContainer, e := newGCConversionContainer(pa, ta.Descriptors[0])
	if e != nil || aliasContainer != ca {
		t.Fatal("physical alias duplicated container identity")
	}
	if _, e = newGCConversionContainer(pb, ta.Descriptors[0]); e == nil {
		t.Fatal("foreign physical owner impersonated container")
	}
	ra, rb := h2AssertReceipt(t, ca, 0), h2AssertReceipt(t, cb, 0)
	if ra.root != rb.root || ra.identity.domain == rb.identity.domain {
		t.Fatal("STOP fixture lacks equal indexes in distinct domains")
	}
	// A second independently reachable physical container receives the first
	// identity. It must not steal the first owner's conditional checked slot.
	h2TableSet(t, tb, 1, wa)
	alias := h2AssertReceipt(t, cb, 1)
	if alias.root == ra.root || alias.container == ra.container || alias.identity != ra.identity {
		t.Fatal("STOP independently reachable containers share a receipt")
	}
	h2WithAuthority(t, store, func() {
		tx, err := ca.prepare(0, wb, b.existingGCExternConversionState())
		if err != nil {
			t.Fatal(err)
		}
		if len(rb.identity.receipts) != 3 {
			t.Fatal("prepare did not acquire incoming receipt")
		}
		fail := fmt.Errorf("reject before visible write")
		if err = tx.commit(func() error { return fail }); err != fail {
			t.Fatal(err)
		}
		tx.rollback()
		if ca.slots[0] != ra || len(rb.identity.receipts) != 2 || binary.LittleEndian.Uint64(ta.Descriptors[0][8:]) != wa {
			t.Fatal("STOP rollback changed physical owner or count")
		}
		same, err := ca.prepare(0, wa, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err = same.commit(func() error { return nil }); err != nil {
			t.Fatal(err)
		}
		if ca.slots[0] != ra || len(ra.identity.receipts) != 3 {
			t.Fatal("same-value transaction duplicated ownership")
		}
	})
	drainManagedFunctionStoreReclaims(store)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	h2AssertReceipt(t, ca, 0)
	h2AssertReceipt(t, cb, 0)
	h2AssertReceipt(t, cb, 1)
	h2TableSet(t, ta, 0, wb)
	h2AssertReceipt(t, cb, 1)
	if !ra.released || len(rb.identity.receipts) != 2 {
		t.Fatal("replacement balance")
	}
	if err := pa.Close(); err != nil {
		t.Fatal(err)
	}
	h2AssertReceipt(t, cb, 0)
	h2AssertReceipt(t, cb, 1)
	if err := pb.Close(); err != nil {
		t.Fatal(err)
	}
	drainManagedFunctionStoreReclaims(store)
	if len(store.gcDomains.conversions) != 0 || store.gcDomains.n != 0 {
		t.Fatal("final container teardown retained domain")
	}
}
func TestR9H2ReceiptCloseOrders(t *testing.T) {
	for _, order := range []string{"provider-first", "publisher-first", "store-first"} {
		t.Run(order, func(t *testing.T) {
			store := newReferenceStore(false)
			publisher, provider, table, word := h2ReceiptFixture(t, store, 0)
			h2TableSet(t, table, 0, word)
			receipt := h2AssertReceipt(t, table.conversionContainers[0], 0)
			if order == "store-first" {
				store.closeRuntime()
			}
			if order == "publisher-first" {
				if err := publisher.Close(); err != nil {
					t.Fatal(err)
				}
				h2AssertReceipt(t, table.conversionContainers[0], 0)
			}
			if err := provider.Close(); err != nil {
				t.Fatal(err)
			}
			if !receipt.released {
				t.Fatal("provider teardown did not consume its receipt")
			}
			if err := publisher.Close(); err != nil {
				t.Fatal(err)
			}
			store.closeRuntime()
			drainManagedFunctionStoreReclaims(store)
			if len(store.gcDomains.conversions) != 0 || store.gcDomains.n != 0 {
				t.Fatal("closed store retained receipt authority")
			}
		})
	}
}

func TestR9H2ReceiptConvertedProducerCycle(t *testing.T) {
	for _, mode := range []string{"direct", "callback", "contention"} {
		for _, shift := range []uint8{0, 6} {
			t.Run(fmt.Sprintf("%s/region=%d", mode, shift), func(t *testing.T) {
				store := newReferenceStore(false)
				initial, provider, _, _ := h2ReceiptFixture(t, store, shift)
				if err := initial.Close(); err != nil {
					t.Fatal(err)
				}
				exported, err := provider.ExportedGlobalObject("g")
				if err != nil {
					t.Fatal(err)
				}
				imp := append(append(wasmtest.Name("p"), wasmtest.Name("g")...), 3, 0x6f, 1)
				code, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), wasmtest.Module(
					wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x70, 1}, []byte{0x60, 0, 0})), wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{1})),
					wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
					wasmtest.Section(9, wasmtest.Vec([]byte{3, 0, 1, 0})), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}), wasmtest.Code([]byte{0xd2, 0, 0xfb, 0, 0, 0xfb, 0x1b, 0x24, 0, 0x0b})))))
				if err != nil {
					t.Fatal(err)
				}
				defer code.Close()
				producer, err := instantiateCore(code, InstantiateOptions{store: store, Imports: testImports("p.g", exported), GC: GCConfig{NurseryBytes: 4096, SurvivorBytes: 4096, NurseryRegionShift: shift, VerifyAfterCollect: true}})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = producer.Invoke("run"); err != nil {
					t.Fatal(err)
				}
				s := producer.existingGCExternConversionState()
				if s == nil || s.count != 1 || !producer.hasPhysicalResources() {
					t.Fatal("STOP converted producer absent")
				}
				// The compiled global.set above is the sole publication hook.
				store.mu.Lock()
				container := store.gcDomains.conversionContainers[&exported.cell[0]]
				store.mu.Unlock()
				if container == nil {
					t.Fatal("STOP guest global store has no physical receipt")
				}
				r := h2AssertReceipt(t, container, 0)
				stateValue, ok := managedFunctionCollectors.Load(producer.gc)
				if !ok || len(stateValue.(*managedFunctionCollector).envs) != 1 {
					t.Fatal("STOP converted object has no producer edge")
				}
				graph := stateValue.(*managedFunctionCollector).graph
				var released atomic.Int32
				var lockedCallback atomic.Bool
				callback := func() {
					if !nativeExecutionMu.TryLock() {
						lockedCallback.Store(true)
					} else {
						nativeExecutionMu.Unlock()
					}
					if !graph.collecting.TryLock() {
						lockedCallback.Store(true)
					} else {
						graph.collecting.Unlock()
					}
					topology := store.ensureGCTopology()
					topology.mu.Lock()
					if topology.readers != 0 || topology.writer {
						lockedCallback.Store(true)
					}
					topology.mu.Unlock()
					released.Add(1)
				}
				provider.referenceLifetime().afterPhysicalRelease(callback)
				producer.referenceLifetime().afterPhysicalRelease(callback)
				if !provider.retainResourceRoot() {
					t.Fatal("STOP external provider root unavailable")
				}
				if err = producer.Close(); err != nil {
					t.Fatal(err)
				}
				if err = provider.Close(); err != nil {
					t.Fatal(err)
				}
				h2AssertReceipt(t, container, 0)
				if !producer.hasPhysicalResources() || released.Load() != 0 {
					t.Fatal("STOP external edge failed to retain converted cycle")
				}
				graph.pendingCollection.Store(true)
				graph.drainAfterGate(0)
				h2AssertReceipt(t, container, 0)
				switch mode {
				case "direct":
					provider.releaseResourceRoot()
					drainManagedFunctionStoreReclaims(store)
				case "contention":
					nativeExecutionMu.Lock()
					provider.releaseResourceRoot()
					graph.drainAfterGate(0)
					graph.hostRetry.mu.Lock()
					idle := graph.hostRetry.idle
					running := graph.hostRetry.running
					graph.hostRetry.mu.Unlock()
					if !running || released.Load() != 0 {
						nativeExecutionMu.Unlock()
						t.Fatal("STOP missing retirement contention waiter")
					}
					for i := 0; i < 8; i++ {
						graph.pendingCollection.Store(true)
						graph.drainAfterGate(0)
					}
					nativeExecutionMu.Unlock()
					select {
					case <-idle:
					case <-time.After(5 * time.Second):
						t.Fatal("receipt retirement wakeup lost")
					}
				case "callback":
					probe := slotHostFunc(func(host HostModule, _ []uint64, _ []uint64) {
						h2AssertReceipt(t, container, 0)
						provider.releaseResourceRoot()
						graph.drainAfterGate(0)
						h2WithAuthority(t, store, func() {
							h2AssertReceipt(t, container, 0)
							if !producer.hasPhysicalResources() {
								t.Fatal("STOP active callback lost producer before collection")
							}
							if err := r.identity.domain.collector.CollectFull(gc.EmptyRoots{}); err != nil {
								t.Fatal(err)
							}
							h2AssertReceipt(t, container, 0)
						})
						if !producer.hasPhysicalResources() || released.Load() != 0 {
							t.Fatal("STOP active callback lost conditional owner")
						}
					})
					imp := append(append(wasmtest.Name("env"), wasmtest.Name("probe")...), 0, 0)
					callerCode, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 0})), wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x0b})))))
					if e != nil {
						t.Fatal(e)
					}
					caller, e := instantiateCore(callerCode, InstantiateOptions{store: store, Imports: testImports("env.probe", probe)})
					if e != nil {
						t.Fatal(e)
					}
					if _, e = caller.Invoke("run"); e != nil {
						t.Fatal(e)
					}
					drainManagedFunctionStoreReclaims(store)
					if e = caller.Close(); e != nil {
						t.Fatal(e)
					}
					callerCode.Close()
				}
				if lockedCallback.Load() {
					t.Fatal("physical callback ran under collection authority")
				}
				if producer.hasPhysicalResources() || provider.hasPhysicalResources() || released.Load() != 2 || !r.released {
					t.Fatalf("cycle survived final external edge: released=%d receipt=%t", released.Load(), r.released)
				}
				if len(store.gcDomains.conversions) != 0 || store.gcDomains.n != 0 {
					t.Fatal("cycle retained domain")
				}
			})
		}
	}

}

func TestR9H2ReceiptOnlyContention(t *testing.T) {
	for _, blocker := range []string{"native", "foreign-runtime", "topology", "domain", "driver"} {
		t.Run(blocker, func(t *testing.T) {
			store := newReferenceStore(false)
			publisher, provider, table, word := h2ReceiptFixture(t, store, 0)
			h2TableSet(t, table, 0, word)
			receipt := h2AssertReceipt(t, table.conversionContainers[0], 0)
			if err := publisher.Close(); err != nil {
				t.Fatal(err)
			}
			h2AssertReceipt(t, table.conversionContainers[0], 0)
			store.mu.Lock()
			for owner, entry := range store.instances {
				if !entry.resourcesReleased && owner.gc != nil {
					store.mu.Unlock()
					t.Fatal("STOP receipt-only fixture has a collector owner")
				}
			}
			store.mu.Unlock()
			value, ok := managedFunctionGraphs.Load(store)
			if !ok {
				t.Fatal("STOP receipt-only graph absent")
			}
			g := value.(*managedFunctionGraph)
			var unlock func()
			var otherPublisher, otherProvider *Instance
			switch blocker {
			case "native":
				nativeExecutionMu.Lock()
				unlock = nativeExecutionMu.Unlock
			case "foreign-runtime":
				otherPublisher, otherProvider, _, _ = h2ReceiptFixture(t, newReferenceStore(false), 0)
				mu := otherProvider.acquireInstanceNativeStateForHostAccess()
				if mu != &nativeExecutionMu {
					mu.Unlock()
					t.Fatal("foreign Runtime did not own global native lock")
				}
				unlock = mu.Unlock
			case "topology":
				topology := store.ensureGCTopology()
				topology.Lock()
				unlock = topology.Unlock
			case "domain":
				receipt.identity.domain.invocationMu.Lock()
				unlock = receipt.identity.domain.invocationMu.Unlock
			case "driver":
				g.collecting.Lock()
				unlock = g.collecting.Unlock
			}
			for i := 0; i < 8; i++ {
				g.pendingCollection.Store(true)
				g.drainAfterGate(0)
			}
			g.hostRetry.mu.Lock()
			idle, running := g.hostRetry.idle, g.hostRetry.running
			g.hostRetry.mu.Unlock()
			unlock()
			if !running || idle == nil {
				t.Fatal("receipt-only retry not queued")
			}
			select {
			case <-idle:
			case <-time.After(5 * time.Second):
				t.Fatal("receipt-only retry lost wakeup")
			}
			if g.pendingCollection.Load() {
				t.Fatal("receipt-only request not consumed")
			}
			h2AssertReceipt(t, table.conversionContainers[0], 0)
			g.pendingCollection.Store(true)
			g.drainAfterGate(0)
			if g.pendingCollection.Load() {
				t.Fatal("post-worker request lost")
			}
			h2TableSet(t, table, 0, 0)
			if err := provider.Close(); err != nil {
				t.Fatal(err)
			}
			if otherPublisher != nil {
				if err := otherPublisher.Close(); err != nil {
					t.Fatal(err)
				}
				if err := otherProvider.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// Lifetime registration does not authorize joining a private type universe.
func TestR9H2ReceiptDomainKeepsPrivateTypeAdmission(t *testing.T) {
	store := newReferenceStore(false)
	publisher, provider, table, word := h2ReceiptFixture(t, store, 0)
	h2TableSet(t, table, 0, word)
	receipt := h2AssertReceipt(t, table.conversionContainers[0], 0)
	if store.ownsGCCollector(publisher.gc) {
		t.Fatal("STOP receipt authority impersonated shared type admission")
	}
	if _, _, err := store.acquireGCCollector(context.Background(), gc.Config{}, publisher.c, publisher.gc); err == nil || !strings.Contains(err.Error(), "not a shared type domain") {
		t.Fatalf("STOP private conversion type domain admitted: %v", err)
	}
	h2AssertReceipt(t, table.conversionContainers[0], 0)
	if receipt.identity.domain.claims != 2 {
		t.Fatal("rejected type import changed receipt claim")
	}
	if err := publisher.Close(); err != nil {
		t.Fatal(err)
	}
	h2AssertReceipt(t, table.conversionContainers[0], 0)
	h2TableSet(t, table, 0, 0)
	if err := provider.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestR9H2ReceiptFinalJournalRemainsCollectionAuthority(t *testing.T) {
	store := newReferenceStore(false)
	publisher, provider, table, word := h2ReceiptFixture(t, store, 0)
	h2TableSet(t, table, 0, word)
	receipt := h2AssertReceipt(t, table.conversionContainers[0], 0)
	domain := receipt.identity.domain
	if err := publisher.Close(); err != nil {
		t.Fatal(err)
	}
	h2AssertReceipt(t, table.conversionContainers[0], 0)
	h2WithAuthority(t, store, func() {
		tx, err := table.conversionContainers[0].prepare(0, 0, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err = tx.commit(func() error { binary.LittleEndian.PutUint64(table.Descriptors[0][8:], 0); return nil }); err != nil {
			t.Fatal(err)
		}
		store.mu.Lock()
		var domains gcInvocationDomainSet
		conversionAddClaimedDomainsLocked(store, &domains)
		valid := len(store.gcDomains.conversions) == 0 && domain.conversionClaims == 1 && domain.claims == 1 && domains.len() == 1 && domains.at(0) == domain && store.gcDomains.domainForCollectorLocked(domain.collector) == domain
		store.mu.Unlock()
		if !valid {
			t.Fatal("STOP final journal lost registered collection authority")
		}
		domain.invocationState.Lock()
		id := domain.invocationOwner
		domain.invocationState.Unlock()
		if handled, err := collectManagedFunctionStore(store, id); !handled || err != nil {
			t.Fatalf("final journal domain could not collect: %t %v", handled, err)
		}
	})
	drainManagedFunctionStoreReclaims(store)
	if store.gcDomainForCollector(domain.collector) != nil || domain.conversionClaims != 0 {
		t.Fatal("final claim journal did not retire")
	}
	if err := provider.Close(); err != nil {
		t.Fatal(err)
	}
}
