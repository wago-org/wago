//go:build linux && (amd64 || arm64) && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"fmt"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"reflect"
	"testing"
	"unsafe"
)

// No payload reads, collections, escaped calls, or cleanup after STOP.
var r9ConvertedStopped []*referenceStore

func r9ConvertedProvider(table, typed bool) []byte {
	var sections [][]byte
	if typed {
		sections = append(sections, wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 0})))
	}
	kind := byte(3)
	if table {
		kind = 1
		sections = append(sections, wasmtest.Section(4, wasmtest.Vec([]byte{0x6f, 0, 1})))
	} else {
		sections = append(sections, wasmtest.Section(6, wasmtest.Vec([]byte{0x6f, 1, 0xd0, 0x6f, 0x0b})))
	}
	return wasmtest.Module(append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("slot", kind, 0))))...)
}
func r9ConvertedPublisher(table bool) []byte {
	imp := append(wasmtest.Name("p"), wasmtest.Name("slot")...)
	body := []byte{0xfb, 1, 0, 0xfb, 0x1b, 0x24, 0, 0x23, 0, 0xd1, 0x0b}
	if table {
		imp = append(imp, 1, 0x6f, 0, 1)
		body = []byte{0x41, 0, 0xfb, 1, 0, 0xfb, 0x1b, 0x26, 0, 0x41, 0, 0x25, 0, 0xd1, 0x0b}
	} else {
		imp = append(imp, 3, 0x6f, 1)
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, []byte{0x60, 0, 1, 0x7f})), wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, wasmtest.Vec([]byte{1})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("publish", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
}
func TestR9H2StoreHooksOriginalProvider(t *testing.T) {
	for _, region := range []bool{false, true} {
		for _, table := range []bool{false, true} {
			for _, typed := range []bool{false, true} {
				t.Run(fmt.Sprintf("region=%t/table=%t/type=%t", region, table, typed), func(t *testing.T) {
					settings := GCConfig{NurseryBytes: 1 << 20, SurvivorBytes: 1 << 20}
					if region {
						f := reflect.ValueOf(&settings).Elem().FieldByName("NurseryRegionShift")
						if !f.IsValid() {
							t.Skip("ADMISSION region unavailable at source pin")
						}
						f.SetUint(6)
					}
					cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
					pc, err := Compile(cfg, r9ConvertedProvider(table, typed))
					if err != nil {
						t.Fatalf("provider compile: %v", err)
					}
					cc, err := Compile(cfg, r9ConvertedPublisher(table))
					if err != nil {
						t.Fatalf("publisher compile: %v", err)
					}
					store := newReferenceStore(false)
					r9ConvertedStopped = append(r9ConvertedStopped, store)
					p, err := instantiateCore(pc, InstantiateOptions{store: store, GC: settings})
					if err != nil {
						t.Fatalf("provider instantiate: %v", err)
					}
					if p.gc != nil || !p.hasPhysicalResources() {
						t.Fatal("STOP provider not pre-collector/live")
					}
					var imports *Imports
					if table {
						v, e := p.ExportedTable("slot")
						if e != nil {
							t.Fatal(e)
						}
						imports = testImports("p.slot", v)
					} else {
						v, e := p.ExportedGlobalObject("slot")
						if e != nil {
							t.Fatal(e)
						}
						imports = testImports("p.slot", v)
					}
					c, err := instantiateCore(cc, InstantiateOptions{store: store, Imports: imports, GC: settings})
					if err != nil {
						t.Fatalf("publisher instantiate before publication: %v", err)
					}
					live := func(in *Instance) bool {
						store.mu.Lock()
						defer store.mu.Unlock()
						s := store.instances[in]
						return s != nil && !s.resourcesReleased
					}
					if !live(p) || !live(c) || c.gc == nil || !c.hasPhysicalResources() {
						t.Fatal("STOP initial membership/domain absent")
					}
					conversion := c.existingGCExternConversionState()
					if conversion == nil {
						t.Fatal("STOP conversion state absent")
					}
					collector := c.gc
					if region {
						actual := reflect.ValueOf(collector).Elem().FieldByName("cfg").FieldByName("NurseryRegionShift").Uint()
						if actual != 6 {
							t.Fatalf("STOP requested region not active: %d", actual)
						}
						t.Log("actual collector region shift=6")
					}
					store.mu.Lock()
					publicTokens := len(store.gcByToken) + len(store.externrefs)
					store.mu.Unlock()
					if publicTokens != 0 {
						t.Fatal("STOP public tokens mask conversion root")
					}
					_ = collector.Stats()
					result, err := c.Invoke("publish")
					if err != nil || len(result) != 1 || result[0] != 0 {
						t.Fatalf("STOP publication nonnull checkpoint: %v %v", result, err)
					}
					conversion.mu.Lock()
					var entry gcExternConversionEntry
					n := 0
					for _, e := range conversion.entries {
						if e.kind == gcExternConversionData {
							entry = e
							n++
						}
					}
					attached := !conversion.closed && conversion.store == store && conversion.collector == collector
					conversion.mu.Unlock()
					if !attached || n != 1 || !entry.hasRoot || !entry.ref.IsObj() {
						t.Fatal("STOP converted object/root receipt absent")
					}
					root, err := collector.CheckedTableSlot(entry.rootSlot)
					if err != nil || root != entry.ref {
						t.Fatal("STOP checked root absent")
					}
					slots := 0
					store.rangeGCDomainPersistentRoots(collector, func(s gc.RootSlot) bool { slots++; return true }, nil)

					t.Logf("publication: conversion entries=%d uses=%d checkedRoot=true providerCollector=%t storeOwned=%t persistentSlots=%d", n, entry.uses, p.gc != nil, store.ownsGCCollector(collector), slots)
					if err = c.Close(); err != nil {
						t.Fatal(err)
					}

					var storage []byte
					if table {
						storage = p.tableDescriptor(0)
					} else {
						storage = p.globalCells[0].cell
					}
					store.mu.Lock()
					container := store.gcDomains.conversionContainers[&storage[0]]
					store.mu.Unlock()
					if container == nil || container.owner != p || !conversion.closed || c.hasPhysicalResources() || !live(p) {
						t.Fatal("STOP missing publisher-independent physical receipt")
					}
					receipt := h2AssertReceipt(t, container, 0)
					if receipt.identity.word != entry.externWord || receipt.identity.domain.collector != collector {
						t.Fatal("STOP wrong receipt identity")
					}
					graphValue, ok := managedFunctionGraphs.Load(store)
					if !ok {
						t.Fatal("STOP missing receipt graph")
					}
					graph := graphValue.(*managedFunctionGraph)
					graph.pendingCollection.Store(true)
					graph.drainAfterGate(0)
					h2AssertReceipt(t, container, 0)
					off := 0
					if table {
						off = 8
					}
					if binary.LittleEndian.Uint64(storage[off:]) != receipt.identity.word {
						t.Fatal("publication changed")
					}

					if table {
						handle, e := p.ExportedTable("slot")
						if e != nil {
							t.Fatal(e)
						}
						if null, e := handle.EntryIsNull(0); e != nil || null {
							t.Fatalf("surviving table access: %v %v", null, e)
						}
						clearerCode, e := Compile(cfg, r9ConvertedClearer(true))
						if e != nil {
							t.Fatal(e)
						}
						clearer, e := instantiateCore(clearerCode, InstantiateOptions{store: store, Imports: testImports("p.slot", handle)})
						if e != nil {
							t.Fatal(e)
						}
						h2AssertReceipt(t, container, 0)
						if !clearer.syncMode || len(clearer.ctrl) == 0 {
							t.Fatal("STOP missing synchronous helper control frame")
						}
						if _, e = clearer.Invoke("clear"); e != nil {
							t.Fatal(e)
						}
						_ = clearer.Close()
						_ = clearerCode.Close()
					} else {
						handle, e := p.ExportedGlobalObject("slot")
						if e != nil {
							t.Fatal(e)
						}
						value, e := handle.GetValue()
						if e != nil || value.bits == 0 {
							t.Fatalf("surviving global access: %v", e)
						}
						if e = handle.SetValue(value); e != nil {
							t.Fatal(e)
						}
						if container.slots[0] != receipt {
							t.Fatal("same-value public set duplicated receipt")
						}
						if e = handle.SetValue(Value{typ: ValExternRef}); e != nil {
							t.Fatal(e)
						}
					}

					drainManagedFunctionStoreReclaims(store)
					if store.gcDomainForCollector(collector) != nil {
						t.Fatal("final clear retained collector")
					}
					if e := p.Close(); e != nil {
						t.Fatal(e)
					}
					_ = pc.Close()
					_ = cc.Close()

				})
			}
		}
	}
}

func r9ConvertedClearer(table bool) []byte {
	imp := append(wasmtest.Name("p"), wasmtest.Name("slot")...)
	body := []byte{0xd0, 0x6f, 0x24, 0, 0x0b}
	if table {
		imp = append(imp, 1, 0x6f, 0, 1)
		body = []byte{0x41, 0, 0xd0, 0x6f, 0x26, 0, 0x0b}
	} else {
		imp = append(imp, 3, 0x6f, 1)
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 0})), wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("clear", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
}

// Writer is compiled and instantiated before the allocating publisher. Its two
// imports alias one physical table; the copy snapshots must preserve overlap.
func TestR9H2StoreHooksBulk(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		t.Run(fmt.Sprint(shift), func(t *testing.T) {
			store := newReferenceStore(false)
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
			pc, e := Compile(cfg, wasmtest.Module(wasmtest.Section(4, wasmtest.Vec([]byte{0x6f, 1, 4, 8})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("slot", 1, 0)))))
			if e != nil {
				t.Fatal(e)
			}
			p, e := instantiateCore(pc, InstantiateOptions{store: store})
			if e != nil {
				t.Fatal(e)
			}
			handle, e := p.ExportedTable("slot")
			if e != nil {
				t.Fatal(e)
			}
			imp := append(wasmtest.Name("p"), wasmtest.Name("slot")...)
			imp = append(imp, 1, 0x6f, 1, 4, 8)
			wc, e := Compile(cfg, wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 3, 0x7f, 0x7f, 0x7f, 0}, []byte{0x60, 2, 0x7f, 0x7f, 0}, []byte{0x60, 1, 0x7f, 1, 0x7f}, []byte{0x60, 0, 0})),
				wasmtest.Section(2, wasmtest.Vec(imp, imp)),
				wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1}, []byte{2}, []byte{0}, []byte{3})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("copy", 0, 0), wasmtest.ExportEntry("fill", 0, 1), wasmtest.ExportEntry("grow", 0, 2), wasmtest.ExportEntry("init", 0, 3), wasmtest.ExportEntry("clear", 0, 4))),
				wasmtest.Section(9, wasmtest.Vec([]byte{5, 0x6f, 2, 0xd0, 0x6f, 0x0b, 0xd0, 0x6f, 0x0b})),
				wasmtest.Section(10, wasmtest.Vec(
					wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 14, 1, 0, 0x0b}),
					wasmtest.Code([]byte{0x20, 0, 0x41, 0, 0x25, 0, 0x20, 1, 0xfc, 17, 1, 0x0b}),
					wasmtest.Code([]byte{0x41, 0, 0x25, 0, 0x20, 0, 0xfc, 15, 1, 0x0b}),
					wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 12, 0, 1, 0x0b}),
					wasmtest.Code([]byte{0x41, 0, 0xd0, 0x6f, 0xfc, 16, 0, 0xfc, 17, 0, 0x0b})))))
			if e != nil {
				t.Fatal(e)
			}
			writer, e := instantiateCore(wc, InstantiateOptions{store: store, Imports: testImports("p.slot", handle)})
			if e != nil {
				t.Fatal(e)
			}
			if writer.gc != nil || !writer.syncMode || len(writer.ctrl) == 0 {
				t.Fatal("STOP old writer helper admission")
			}
			cc, e := Compile(cfg, r9ConvertedPublisher(true))
			if e != nil {
				t.Fatal(e)
			}
			publisher, e := instantiateCore(cc, InstantiateOptions{store: store, Imports: testImports("p.slot", handle), GC: GCConfig{NurseryBytes: 4096, SurvivorBytes: 4096, NurseryRegionShift: shift}})
			if e != nil {
				t.Fatal(e)
			}
			if _, e = publisher.Invoke("publish"); e != nil {
				t.Fatal(e)
			}
			store.mu.Lock()
			c := store.gcDomains.conversionContainers[&handle.desc[0]]
			store.mu.Unlock()
			if c == nil {
				t.Fatal("STOP missing bulk source receipt")
			}
			r := h2AssertReceipt(t, c, 0)
			if e = publisher.Close(); e != nil {
				t.Fatal(e)
			}
			h2AssertReceipt(t, c, 0)
			call := func(name string, args ...uint64) []uint64 {
				t.Helper()
				out, e := writer.Invoke(name, args...)
				if e != nil {
					t.Fatalf("%s: %v", name, e)
				}
				return out
			}
			call("fill", 1, 3)
			for i := uint64(0); i < 4; i++ {
				h2AssertReceipt(t, c, i)
			}
			if len(c.slots) != 4 || len(r.identity.receipts) != 4 {
				t.Fatal("alias duplicated owner or lost slots")
			}
			call("copy", 1, 0, 3)
			call("copy", 0, 1, 3)
			call("copy", 0, 0, 4)
			before := append([]byte(nil), handle.desc...)
			if _, e = writer.Invoke("copy", 2, 0, 3); e == nil {
				t.Fatal("missing bounds trap")
			}
			if !bytes.Equal(before, handle.desc) || len(c.slots) != 4 {
				t.Fatal("trapping copy changed destination")
			}
			out := call("grow", 2)
			if len(out) != 1 || out[0] != 4 {
				t.Fatalf("grow: %v", out)
			}
			for i := uint64(0); i < 6; i++ {
				h2AssertReceipt(t, c, i)
			}
			if out = call("grow", 3); out[0] != uint64(^uint32(0)) || len(c.slots) != 6 {
				t.Fatal("failed grow changed receipts")
			}
			// Public aliases share the exact physical receipts and public token identity.
			public := Value{typ: ValExternRef, bits: r.identity.publicExtern}
			beforeReceipt := c.slots[0]
			if e = handle.SetValue(0, public); e != nil {
				t.Fatal(e)
			}
			if c.slots[0] != beforeReceipt {
				t.Fatal("public same-value table store duplicated receipt")
			}
			if e = handle.SetValue(5, Value{typ: ValExternRef}); e != nil {
				t.Fatal(e)
			}
			if e = handle.SetValue(5, public); e != nil {
				t.Fatal(e)
			}
			call("init", 1, 0, 2)
			if len(c.slots) != 4 || c.slots[1] != nil || c.slots[2] != nil {
				t.Fatal("init did not clear exact receipts")
			}
			call("clear")
			if len(c.slots) != 0 || store.gcDomainForCollector(r.identity.domain.collector) != nil {
				t.Fatal("final guest clear retained collector")
			}
			_ = writer.Close()
			_ = p.Close()
			_ = wc.Close()
			_ = cc.Close()
			_ = pc.Close()
		})
	}
}

func TestR9H2StoreHooksActiveInitialization(t *testing.T) {
	store := newReferenceStore(false)
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	publisher, provider, table, word := h2ReceiptFixture(t, store, 0)
	h2TableSet(t, table, 0, word)
	h2TableSet(t, table, 1, word)
	c := table.conversionContainers[0]
	r := h2AssertReceipt(t, c, 0)
	if e := publisher.Close(); e != nil {
		t.Fatal(e)
	}
	h2AssertReceipt(t, c, 1)
	handle, e := provider.ExportedTable("slots")
	if e != nil {
		t.Fatal(e)
	}
	imp := append(wasmtest.Name("p"), wasmtest.Name("slots")...)
	imp = append(imp, 1, 0x6f, 0, 2)
	makeInit := func(bad bool) []byte {
		elems := [][]byte{{6, 0, 0x41, 0, 0x0b, 0x6f, 2, 0xd0, 0x6f, 0x0b, 0xd0, 0x6f, 0x0b}}
		if bad {
			elems = [][]byte{{6, 0, 0x41, 2, 0x0b, 0x6f, 1, 0xd0, 0x6f, 0x0b}}
		}
		return wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(9, wasmtest.Vec(elems...)))
	}
	bad, e := Compile(cfg, makeInit(true))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = instantiateCore(bad, InstantiateOptions{store: store, Imports: testImports("p.slots", handle)}); e == nil {
		t.Fatal("expected failed initialization")
	}
	h2AssertReceipt(t, c, 0)
	h2AssertReceipt(t, c, 1)
	if binary.LittleEndian.Uint64(handle.desc[8:]) != word {
		t.Fatal("failed initializer published partial write")
	}
	good, e := Compile(cfg, makeInit(false))
	if e != nil {
		t.Fatal(e)
	}
	initialized, e := instantiateCore(good, InstantiateOptions{store: store, Imports: testImports("p.slots", handle)})
	if e != nil {
		t.Fatal(e)
	}
	if len(c.slots) != 0 || store.gcDomainForCollector(r.identity.domain.collector) != nil {
		t.Fatal("active clear retained collector")
	}
	_ = initialized.Close()
	_ = provider.Close()
	_ = bad.Close()
	_ = good.Close()
}

func TestR9H2StoreHooksDescriptorMigration(t *testing.T) {
	store := newReferenceStore(false)
	publisher, provider, table, word := h2ReceiptFixture(t, store, 0)
	h2TableSet(t, table, 0, word)
	c := table.conversionContainers[0]
	r := h2AssertReceipt(t, c, 0)
	handle, e := provider.ExportedTable("slots")
	if e != nil {
		t.Fatal(e)
	}
	arena, e := coreruntime.NewArena(len(handle.desc))
	if e != nil {
		t.Fatal(e)
	}
	next := arena.Alloc(len(handle.desc))
	copy(next, handle.desc)
	h2WithAuthority(t, store, func() {
		rejected := fmt.Errorf("reject migration")
		if e := c.migrate(next, func() error { return rejected }); e != rejected {
			t.Fatal(e)
		}
		if &c.storage[0] != &handle.desc[0] || c.slots[0] != r {
			t.Fatal("migration rollback changed identity")
		}
		if e := c.migrate(next, func() error {
			pointer := uintptr(unsafe.Pointer(&next[0]))
			provider.tableDescPtr = pointer
			provider.jm.SetTablePtr(pointer)
			if provider.nativeContext != 0 {
				context := unsafe.Slice((*byte)(offHeapPtr(provider.nativeContext)), coreruntime.InstanceContextBytes)
				binary.LittleEndian.PutUint64(context[8:], uint64(pointer))
			}
			handle.desc = next
			return nil
		}); e != nil {
			t.Fatal(e)
		}
		canonical, e := newGCConversionContainer(provider, next)
		if e != nil || canonical != c || c.slots[0] != r {
			t.Fatal("migration changed canonical receipt")
		}
	})
	if e = publisher.Close(); e != nil {
		t.Fatal(e)
	}
	h2AssertReceipt(t, c, 0)
	if e = handle.SetValue(0, Value{typ: ValExternRef}); e != nil {
		t.Fatal(e)
	}
	if !r.released || store.gcDomainForCollector(r.identity.domain.collector) != nil {
		t.Fatal("migrated final clear retained collector")
	}
	_ = provider.Close()
	_ = arena.Close()
}

func TestR9H2StoreHooksIndependentPhysicalTables(t *testing.T) {
	store := newReferenceStore(false)
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	pc, e := Compile(cfg, r9ConvertedProvider(true, false))
	if e != nil {
		t.Fatal(e)
	}
	p1, e := instantiateCore(pc, InstantiateOptions{store: store})
	if e != nil {
		t.Fatal(e)
	}
	p2, e := instantiateCore(pc, InstantiateOptions{store: store})
	if e != nil {
		t.Fatal(e)
	}
	a, e := p1.ExportedTable("slot")
	if e != nil {
		t.Fatal(e)
	}
	b, e := p2.ExportedTable("slot")
	if e != nil {
		t.Fatal(e)
	}
	imp := func(name string) []byte {
		v := append(wasmtest.Name("p"), wasmtest.Name(name)...)
		return append(v, 1, 0x6f, 0, 1)
	}
	cc, e := Compile(cfg, wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, []byte{0x60, 0, 0})),
		wasmtest.Section(2, wasmtest.Vec(imp("a"), imp("b"))), wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("publish", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0, 0xfb, 1, 0, 0xfb, 0x1b, 0x26, 0, 0x41, 0, 0x41, 0, 0x41, 1, 0xfc, 14, 1, 0, 0x0b})))))
	if e != nil {
		t.Fatal(e)
	}
	publisher, e := instantiateCore(cc, InstantiateOptions{store: store, Imports: testImports("p.a", a, "p.b", b)})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = publisher.Invoke("publish"); e != nil {
		t.Fatal(e)
	}
	store.mu.Lock()
	ca, cb := store.gcDomains.conversionContainers[&a.desc[0]], store.gcDomains.conversionContainers[&b.desc[0]]
	store.mu.Unlock()
	if ca == nil || cb == nil || ca == cb {
		t.Fatal("STOP independent table ownership missing")
	}
	ra, rb := h2AssertReceipt(t, ca, 0), h2AssertReceipt(t, cb, 0)
	if ra.identity != rb.identity || ra.root == rb.root {
		t.Fatal("STOP independent checked roots missing")
	}
	if e = publisher.Close(); e != nil {
		t.Fatal(e)
	}
	h2AssertReceipt(t, ca, 0)
	h2AssertReceipt(t, cb, 0)
	foreign := newReferenceStore(false)
	p3, e := instantiateCore(pc, InstantiateOptions{store: foreign})
	if e != nil {
		t.Fatal(e)
	}
	other, e := p3.ExportedTable("slot")
	if e != nil {
		t.Fatal(e)
	}
	if e = other.SetValue(0, Value{typ: ValExternRef, bits: ra.identity.publicExtern}); e == nil {
		t.Fatal("cross-Runtime public identity admitted")
	}
	if e = a.SetValue(0, Value{typ: ValExternRef}); e != nil {
		t.Fatal(e)
	}
	h2AssertReceipt(t, cb, 0)
	if e = b.SetValue(0, Value{typ: ValExternRef}); e != nil {
		t.Fatal(e)
	}
	if store.gcDomainForCollector(ra.identity.domain.collector) != nil {
		t.Fatal("final independent owner retained collector")
	}
	_ = p1.Close()
	_ = p2.Close()
	_ = p3.Close()
	_ = cc.Close()
	_ = pc.Close()
}
