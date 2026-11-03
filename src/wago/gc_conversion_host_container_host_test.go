//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"sync/atomic"
	"testing"
)

func h2HostReceipt(t *testing.T, store *referenceStore, storage []byte, index uint64) *gcConversionReceipt {
	t.Helper()
	c, err := conversionContainerForStore(store, nil, storage)
	if err != nil || c == nil || !c.hostLive() {
		t.Fatal("STOP host authority absent", err)
	}
	r := c.slots[index]
	if r == nil || r.released || r.container != c || r.index != index {
		t.Fatal("STOP host receipt absent")
	}
	if store.gcDomainForCollector(r.identity.domain.collector) != r.identity.domain || r.identity.domain.claims == 0 {
		t.Fatal("STOP host domain unregistered")
	}
	state, ok := managedFunctionCollectors.Load(r.identity.domain.collector)
	if !ok || state.(*managedFunctionCollector).conversionHostSlots[r.root] != c {
		t.Fatal("STOP host checked slot unbound")
	}
	if ref, err := r.identity.domain.collector.CheckedTableSlot(r.root); err != nil || ref != r.identity.ref {
		t.Fatal("STOP host checked root invalid")
	}
	return r
}

func h2HostToken(t *testing.T, publisher *Instance) Value {
	t.Helper()
	s := publisher.existingGCExternConversionState()
	if s == nil || s.count != 1 {
		t.Fatal("STOP missing conversion")
	}
	return Value{typ: ValExternRef, bits: s.entries[0].publicExternWord}
}

func TestR9H2HostContainerPublicGlobal(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		t.Run(fmt.Sprint(shift), func(t *testing.T) {
			rt := NewRuntime()
			store := rt.refStore
			p, provider, _, _ := h2ReceiptFixture(t, store, shift)
			g, err := rt.NewExternRefGlobal(ExternRef{}, true)
			if err != nil {
				t.Fatal(err)
			}
			token := h2HostToken(t, p)
			if err = g.SetValue(token); err != nil {
				t.Fatal(err)
			}
			r := h2HostReceipt(t, store, g.cell, 0)
			if err = g.SetValue(token); err != nil || h2HostReceipt(t, store, g.cell, 0) != r {
				t.Fatal("same-value replacement", err)
			}
			g.Type = ValFuncRef
			if err = g.SetValue(token); err == nil {
				t.Fatal("edited public type accepted")
			}
			g.Type = ValExternRef
			if err = p.Close(); err != nil {
				t.Fatal(err)
			}
			if p.hasPhysicalResources() {
				t.Fatal("STOP publisher retained")
			}
			h2HostReceipt(t, store, g.cell, 0)
			if v, e := g.GetValue(); e != nil || v != token {
				t.Fatal("public token after publisher retirement", e)
			}
			if err = g.SetValue(Value{typ: ValExternRef}); err != nil {
				t.Fatal(err)
			}
			if !r.released || store.gcDomainForCollector(r.identity.domain.collector) != nil {
				t.Fatal("final clear retained domain")
			}
			ordinary, e := rt.NewExternRef("ordinary")
			if e != nil {
				t.Fatal(e)
			}
			if e = g.SetValue(ValueExternRef(ordinary)); e != nil {
				t.Fatal(e)
			}
			if v, e := g.GetValue(); e != nil || v != ValueExternRef(ordinary) {
				t.Fatal("ordinary token", e)
			}
			immutable, e := rt.NewExternRefGlobal(ordinary, false)
			if e != nil {
				t.Fatal(e)
			}
			if e = immutable.SetValue(Value{typ: ValExternRef}); e == nil {
				t.Fatal("immutable host global accepted mutation")
			}
			if v, e := immutable.GetValue(); e != nil || v != ValueExternRef(ordinary) {
				t.Fatal("immutable rejection mutated storage", e)
			}
			immutable.Close()
			if err = g.Close(); err != nil {
				t.Fatal(err)
			}
			provider.Close()
			rt.Close()
		})
	}
}

func TestR9H2HostContainerPublicTable(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		t.Run(fmt.Sprint(shift), func(t *testing.T) {
			rt := NewRuntime()
			store := rt.refStore
			p, provider, _, _ := h2ReceiptFixture(t, store, shift)
			a, err := rt.NewExternRefTable(2, 4)
			if err != nil {
				t.Fatal(err)
			}
			b, err := rt.NewExternRefTable(2, 4)
			if err != nil {
				t.Fatal(err)
			}
			token := h2HostToken(t, p)
			for _, table := range []*Table{a, b} {
				if err = table.SetValue(0, token); err != nil {
					t.Fatal(err)
				}
			}
			ra, rb := h2HostReceipt(t, store, a.desc, 0), h2HostReceipt(t, store, b.desc, 0)
			if ra == rb || ra.root == rb.root || ra.container == rb.container {
				t.Fatal("independent storage aliases receipt")
			}
			alias := &Table{desc: a.desc, owner: a.owner}
			if err = alias.SetValue(0, token); err != nil || h2HostReceipt(t, store, a.desc, 0) != ra {
				t.Fatal("canonical alias", err)
			}
			if err = a.SetValue(2, token); err == nil {
				t.Fatal("bounds failure accepted")
			}
			if err = p.Close(); err != nil {
				t.Fatal(err)
			}
			if p.hasPhysicalResources() {
				t.Fatal("STOP publisher retained")
			}
			h2HostReceipt(t, store, a.desc, 0)
			h2HostReceipt(t, store, b.desc, 0)
			if err = a.SetValue(0, Value{typ: ValExternRef}); err != nil {
				t.Fatal(err)
			}
			h2HostReceipt(t, store, b.desc, 0)
			if err = b.Close(); err != nil {
				t.Fatal(err)
			}
			if !rb.released || store.gcDomainForCollector(rb.identity.domain.collector) != nil {
				t.Fatal("last physical receipt did not retire domain")
			}
			a.Close()
			provider.Close()
			rt.Close()
		})
	}
}

func TestR9H2HostContainerClose(t *testing.T) {
	for _, order := range []string{"container-first", "publisher-first", "runtime-first"} {
		t.Run(order, func(t *testing.T) {
			rt := NewRuntime()
			store := rt.refStore
			p, provider, _, _ := h2ReceiptFixture(t, store, 0)
			table, err := rt.NewExternRefTable(1, 1)
			if err != nil {
				t.Fatal(err)
			}
			if err = table.SetValue(0, h2HostToken(t, p)); err != nil {
				t.Fatal(err)
			}
			r := h2HostReceipt(t, store, table.desc, 0)
			storage := table.desc
			if order == "publisher-first" {
				p.Close()
				if p.hasPhysicalResources() {
					t.Fatal("STOP publisher retained")
				}
				h2HostReceipt(t, store, storage, 0)
			}
			if order == "runtime-first" {
				rt.Close()
				h2HostReceipt(t, store, storage, 0)
			}
			if err = table.Close(); err != nil {
				t.Fatal(err)
			}
			if !r.released {
				t.Fatal("Close did not release receipt")
			}
			if _, err = conversionContainerForStore(store, nil, storage); err == nil {
				t.Fatal("closed physical authority admitted")
			}
			if err = table.Close(); err != nil {
				t.Fatal(err)
			}
			p.Close()
			provider.Close()
			rt.Close()
			if store.gcDomainForCollector(r.identity.domain.collector) != nil {
				t.Fatal("Close retained domain")
			}
		})
	}
}

func TestR9H2HostContainerAuthority(t *testing.T) {
	rt := NewRuntime()
	store := rt.refStore
	p, provider, _, _ := h2ReceiptFixture(t, store, 0)
	table, err := rt.NewExternRefTable(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	token := h2HostToken(t, p)
	if err = table.SetValue(0, token); err != nil {
		t.Fatal(err)
	}
	r := h2HostReceipt(t, store, table.desc, 0)
	foreign := NewRuntime()
	ft, err := foreign.NewExternRefTable(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err = ft.SetValue(0, token); err == nil {
		t.Fatal("foreign converted token accepted")
	}
	if err = table.SetValue(0, Value{typ: ValExternRef, bits: r.identity.word}); err == nil {
		t.Fatal("internal opaque word accepted as public token")
	}
	if binary.LittleEndian.Uint64(table.desc[8:]) != r.identity.word || h2HostReceipt(t, store, table.desc, 0) != r {
		t.Fatal("rejection mutated receipt")
	}
	if _, err = conversionContainerForStore(foreign.refStore, nil, table.desc); err == nil {
		t.Fatal("foreign physical storage accepted")
	}
	fake := make([]byte, len(table.desc))
	copy(fake, table.desc)
	if _, err = conversionContainerForStore(store, nil, fake); err == nil {
		t.Fatal("fabricated storage accepted")
	}
	state, _ := managedFunctionCollectors.Load(r.identity.domain.collector)
	s := state.(*managedFunctionCollector)
	h2WithAuthority(t, store, func() {
		s.authorized = true
		if !s.PersistentRootIsStrong(gc.SlotTable, r.root) {
			t.Fatal("live host is not an external root")
		}
		// A removed external authority must not silently become an unconditional seed.
		r.container.closed = true
		if s.PersistentRootIsStrong(gc.SlotTable, r.root) {
			t.Fatal("closed host slot became unconditional")
		}
		r.container.closed = false
		delete(store.gcDomains.conversionContainers, &table.desc[0])
		if s.PersistentRootIsStrong(gc.SlotTable, r.root) {
			t.Fatal("unbound host slot became unconditional")
		}
		store.gcDomains.conversionContainers[&table.desc[0]] = r.container
		s.authorized = false
		if !s.PersistentRootIsStrong(gc.SlotTable, r.root) {
			t.Fatal("active/callback root lost conservative strength")
		}
		r.container.closed = false
	})
	if err = table.SetValue(0, Value{typ: ValExternRef}); err != nil {
		t.Fatal(err)
	}
	if s.conversionHostSlots[r.root] != nil {
		t.Fatal("released slot retained host binding")
	}
	if err = table.SetValue(0, token); err != nil {
		t.Fatal(err)
	}
	next := h2HostReceipt(t, store, table.desc, 0)
	if next == r || next.released || s.conversionHostSlots[next.root] != next.container {
		t.Fatal("new receipt reused stale binding")
	}
	table.Close()
	p.Close()
	provider.Close()
	rt.Close()
	ft.Close()
	foreign.Close()
}

func TestR9H2HostContainerMigration(t *testing.T) {
	rt := NewRuntime()
	store := rt.refStore
	p, provider, _, _ := h2ReceiptFixture(t, store, 0)
	table, err := rt.NewExternRefTable(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err = table.SetValue(0, h2HostToken(t, p)); err != nil {
		t.Fatal(err)
	}
	r := h2HostReceipt(t, store, table.desc, 0)
	c := r.container
	old := table.desc
	oldArena := table.owner.arena
	arena, err := coreruntime.NewArena(len(old))
	if err != nil {
		t.Fatal(err)
	}
	replacement := arena.Alloc(len(old))
	copy(replacement, old)
	unlock := lockConversionHostAccess(store)
	if err = c.migrate(replacement, func() error { return fmt.Errorf("reject") }); err == nil || &c.storage[0] != &old[0] || c.slots[0] != r {
		t.Fatal("failed migration changed identity")
	}
	err = c.migrate(replacement, func() error { table.desc = replacement; table.owner.arena = arena; return nil })
	unlock()
	if err != nil {
		t.Fatal(err)
	}
	oldArena.Close()
	if h2HostReceipt(t, store, table.desc, 0) != r {
		t.Fatal("migration replaced receipt")
	}
	if _, err = conversionContainerForStore(store, nil, old); err == nil {
		t.Fatal("old allocation remains authoritative")
	}
	table.Close()
	p.Close()
	provider.Close()
	rt.Close()
}

func TestR9H2HostContainerCycle(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		for _, drop := range []string{"clear", "close"} {
			t.Run(fmt.Sprintf("%d/%s", shift, drop), func(t *testing.T) {
				rt := NewRuntime()
				store := rt.refStore
				initial, provider, _, _ := h2ReceiptFixture(t, store, shift)
				initial.Close()
				global, err := provider.ExportedGlobalObject("g")
				if err != nil {
					t.Fatal(err)
				}
				imp := append(append(wasmtest.Name("p"), wasmtest.Name("g")...), 3, 0x6f, 1)
				code, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), wasmtest.Module(
					wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x70, 1}, []byte{0x60, 0, 0})), wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{1})),
					wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))), wasmtest.Section(9, wasmtest.Vec([]byte{3, 0, 1, 0})), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}), wasmtest.Code([]byte{0xd2, 0, 0xfb, 0, 0, 0xfb, 0x1b, 0x24, 0, 0x0b})))))
				if err != nil {
					t.Fatal(err)
				}
				producer, err := instantiateCore(code, InstantiateOptions{store: store, Imports: testImports("p.g", global), GC: GCConfig{NurseryBytes: 4096, SurvivorBytes: 4096, NurseryRegionShift: shift, VerifyAfterCollect: true}})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = producer.Invoke("run"); err != nil {
					t.Fatal(err)
				}
				table, err := rt.NewExternRefTable(1, 1)
				if err != nil {
					t.Fatal(err)
				}
				if err = table.SetValue(0, h2HostToken(t, producer)); err != nil {
					t.Fatal(err)
				}
				r := h2HostReceipt(t, store, table.desc, 0)
				var callbacks atomic.Int32
				producer.referenceLifetime().afterPhysicalRelease(func() {
					if !nativeExecutionMu.TryLock() {
						panic("locked host callback")
					}
					nativeExecutionMu.Unlock()
					callbacks.Add(1)
				})
				producer.Close()
				provider.Close()
				if !producer.hasPhysicalResources() || !provider.hasPhysicalResources() || callbacks.Load() != 0 {
					t.Fatal("STOP host external root did not preserve cycle")
				}
				h2HostReceipt(t, store, table.desc, 0)
				state, _ := managedFunctionCollectors.Load(r.identity.domain.collector)
				graph := state.(*managedFunctionCollector).graph
				graph.pendingCollection.Store(true)
				graph.drainAfterGate(0)
				h2HostReceipt(t, store, table.desc, 0)
				if drop == "clear" {
					err = table.SetValue(0, Value{typ: ValExternRef})
				} else {
					err = table.Close()
				}
				if err != nil {
					t.Fatal(err)
				}
				if producer.hasPhysicalResources() || provider.hasPhysicalResources() || callbacks.Load() != 1 || store.gcDomainForCollector(r.identity.domain.collector) != nil {
					t.Fatal("cycle survives final host edge")
				}
				table.Close()
				rt.Close()
				code.Close()
			})
		}
	}
}
