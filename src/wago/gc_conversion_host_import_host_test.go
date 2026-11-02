//go:build linux && (amd64 || arm64) && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

// Retain failed fixtures: a failed ownership guard must stop before collection,
// teardown, or any subsequent access to potentially unowned native storage.
var r9HostStopped []*Runtime

func r9HostAssertRegion(t *testing.T, in *Instance, shift uint8) {
	t.Helper()
	if in == nil || in.gc == nil || !in.hasPhysicalResources() {
		t.Fatal("STOP missing live publisher collector")
	}
	actual := reflect.ValueOf(in.gc).Elem().FieldByName("cfg").FieldByName("NurseryRegionShift").Uint()
	if shift == 6 && actual != 6 {
		t.Fatalf("STOP requested region not active: %d", actual)
	}
	t.Logf("actual collector region shift=%d", actual)
}

func r9HostAssertReceipt(t *testing.T, c *gcConversionContainer, index uint64) *gcConversionReceipt {
	t.Helper()
	if c == nil || c.owner != nil || c.closed || c.store == nil || len(c.storage) == 0 {
		t.Fatal("STOP missing physical host container")
	}
	r := c.slots[index]
	if r == nil || r.released || r.container != c || r.index != index || r.identity == nil || r.identity.domain == nil {
		t.Fatal("STOP missing host receipt")
	}
	store := c.store
	store.mu.Lock()
	registered := store.gcDomains != nil && store.gcDomains.conversionContainers[&c.storage[0]] == c && store.gcDomains.domainForCollectorLocked(r.identity.domain.collector) == r.identity.domain && r.identity.domain.claims > 0
	store.mu.Unlock()
	if !registered {
		t.Fatal("STOP unregistered physical host authority/domain")
	}
	if ref, err := r.identity.domain.collector.CheckedTableSlot(r.root); err != nil || ref != r.identity.ref {
		t.Fatal("STOP checked host root mismatch")
	}
	return r
}

func r9HostContainer(t *testing.T, store *referenceStore, storage []byte) *gcConversionContainer {
	t.Helper()
	if len(storage) == 0 {
		t.Fatal("STOP missing host storage")
	}
	store.mu.Lock()
	var c *gcConversionContainer
	if store.gcDomains != nil {
		c = store.gcDomains.conversionContainers[&storage[0]]
	}
	store.mu.Unlock()
	if c == nil {
		t.Fatal("STOP host storage has no canonical receipt container")
	}
	return c
}

// Compile the collector-free helper inside a loop and behind a native call.
// This exercises both loop planning and call/return frames on the actual host.
func r9HostClearer(table bool) []byte {
	imp := append(wasmtest.Name("p"), wasmtest.Name("slot")...)
	body := []byte{0x03, 0x40, 0xd0, 0x6f, 0x24, 0, 0x0b, 0x0b}
	if table {
		imp = append(imp, 1, 0x6f, 0, 1)
		body = []byte{0x03, 0x40, 0x41, 0, 0xd0, 0x6f, 0x26, 0, 0x0b, 0x0b}
	} else {
		imp = append(imp, 3, 0x6f, 1)
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 0})), wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("clear", 0, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code([]byte{0x10, 0, 0x0b}))))
}

func TestR9H2HostImportGlobal(t *testing.T) { r9HostImportSingle(t, false) }
func TestR9H2HostImportTable(t *testing.T)  { r9HostImportSingle(t, true) }

func r9HostImportSingle(t *testing.T, table bool) {
	for _, shift := range []uint8{0, 6} {
		t.Run(fmt.Sprint(shift), func(t *testing.T) {
			rt := NewRuntime()
			r9HostStopped = append(r9HostStopped, rt)
			store := rt.refStore
			var handle any
			var storage []byte
			var closeHandle func() error
			if table {
				v, e := rt.NewExternRefTable(1, 1)
				if e != nil {
					t.Fatal(e)
				}
				handle, storage, closeHandle = v, v.desc, v.Close
			} else {
				v, e := rt.NewExternRefGlobal(NullExternRef(), true)
				if e != nil {
					t.Fatal(e)
				}
				handle, storage, closeHandle = v, v.cell, v.Close
			}
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
			clearCode, e := Compile(cfg, r9HostClearer(table))
			if e != nil {
				t.Fatal(e)
			}
			// Native writer predates every conversion domain and owns no collector.
			clearer, e := instantiateCore(clearCode, InstantiateOptions{store: store, Imports: testImports("p.slot", handle)})
			if e != nil {
				t.Fatal(e)
			}
			if clearer.gc != nil || !clearer.syncMode || len(clearer.ctrl) == 0 {
				t.Fatal("STOP missing old writer helper frame")
			}
			code, e := Compile(cfg, r9ConvertedPublisher(table))
			if e != nil {
				t.Fatal(e)
			}
			publisher, e := instantiateCore(code, InstantiateOptions{store: store, Imports: testImports("p.slot", handle), GC: GCConfig{NurseryBytes: 4096, SurvivorBytes: 4096, NurseryRegionShift: shift}})
			if e != nil {
				t.Fatal(e)
			}
			r9HostAssertRegion(t, publisher, shift)
			out, e := publisher.Invoke("publish")
			if e != nil || len(out) != 1 || out[0] != 0 {
				t.Fatalf("STOP guest publication: %v %v", out, e)
			}
			c := r9HostContainer(t, store, storage)
			r := r9HostAssertReceipt(t, c, 0)
			collector := r.identity.domain.collector
			if e = publisher.Close(); e != nil {
				t.Fatal(e)
			}
			if publisher.hasPhysicalResources() {
				t.Fatal("STOP host receipt retained publisher")
			}
			r9HostAssertReceipt(t, c, 0)
			graphValue, ok := managedFunctionGraphs.Load(store)
			if !ok {
				t.Fatal("STOP missing host collection graph")
			}
			graph := graphValue.(*managedFunctionGraph)
			graph.pendingCollection.Store(true)
			graph.drainAfterGate(0)
			r9HostAssertReceipt(t, c, 0)
			value := Value{typ: ValExternRef, bits: r.identity.publicExtern}
			if table {
				v := handle.(*Table)
				if null, err := v.EntryIsNull(0); err != nil || null {
					t.Fatalf("surviving host table: %v %v", null, err)
				}
				if e = v.SetValue(0, value); e != nil {
					t.Fatal(e)
				}
			} else {
				v := handle.(*Global)
				got, err := v.GetValue()
				if err != nil || got != value {
					t.Fatalf("surviving host global: %v %v", got, err)
				}
				if e = v.SetValue(value); e != nil {
					t.Fatal(e)
				}
			}
			if c.slots[0] != r {
				t.Fatal("same-value host store duplicated receipt")
			}
			if _, e = clearer.Invoke("clear"); e != nil {
				t.Fatal(e)
			}
			if !r.released || len(c.slots) != 0 || store.gcDomainForCollector(collector) != nil {
				t.Fatal("final guest clear retained domain/receipt")
			}
			_ = clearer.Close()
			_ = closeHandle()
			_ = clearCode.Close()
			_ = code.Close()
			_ = rt.Close()
		})
	}
}

func TestR9H2HostImportBulk(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		t.Run(fmt.Sprint(shift), func(t *testing.T) {
			rt := NewRuntime()
			r9HostStopped = append(r9HostStopped, rt)
			store := rt.refStore
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
			handle, e := rt.NewExternRefTable(4, 8)
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
			r9HostAssertRegion(t, publisher, shift)
			if _, e = publisher.Invoke("publish"); e != nil {
				t.Fatal(e)
			}
			store.mu.Lock()
			c := store.gcDomains.conversionContainers[&handle.desc[0]]
			store.mu.Unlock()
			if c == nil {
				t.Fatal("STOP missing bulk source receipt")
			}
			r := r9HostAssertReceipt(t, c, 0)
			if e = publisher.Close(); e != nil {
				t.Fatal(e)
			}
			if publisher.hasPhysicalResources() {
				t.Fatal("STOP bulk publisher retained")
			}
			r9HostAssertReceipt(t, c, 0)
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
				r9HostAssertReceipt(t, c, i)
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
				r9HostAssertReceipt(t, c, i)
			}
			before = append([]byte(nil), handle.desc...)
			if out = call("grow", 3); out[0] != uint64(^uint32(0)) || len(c.slots) != 6 || !bytes.Equal(before, handle.desc) {
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
			// Nulls distinguish each source position, exposing both overlap directions.
			call("copy", 1, 0, 3)
			for i, want := range []bool{true, true, false, false, true, true} {
				if (c.slots[uint64(i)] != nil) != want || (binary.LittleEndian.Uint64(handle.desc[8+i*8:]) != 0) != want {
					t.Fatalf("right overlap snapshot at %d", i)
				}
			}
			call("copy", 0, 1, 3)
			for i, want := range []bool{true, false, false, false, true, true} {
				if (c.slots[uint64(i)] != nil) != want || (binary.LittleEndian.Uint64(handle.desc[8+i*8:]) != 0) != want {
					t.Fatalf("left overlap snapshot at %d", i)
				}
			}
			before = append([]byte(nil), handle.desc...)
			for _, bad := range []struct {
				name string
				args []uint64
			}{{"fill", []uint64{5, 2}}, {"init", []uint64{0, 1, 2}}, {"init", []uint64{6, 0, 1}}} {
				if _, e = writer.Invoke(bad.name, bad.args...); e == nil || !bytes.Equal(before, handle.desc) || len(c.slots) != 3 {
					t.Fatalf("failed %s changed physical storage/receipts: %v", bad.name, e)
				}
			}
			call("clear")
			if len(c.slots) != 0 || store.gcDomainForCollector(r.identity.domain.collector) != nil {
				t.Fatal("final guest clear retained collector")
			}
			_ = writer.Close()
			_ = handle.Close()
			_ = rt.Close()
			_ = wc.Close()
			_ = cc.Close()
		})
	}
}

func TestR9H2HostImportInitialization(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		t.Run(fmt.Sprint(shift), func(t *testing.T) {
			rt := NewRuntime()
			r9HostStopped = append(r9HostStopped, rt)
			store := rt.refStore
			handle, e := rt.NewExternRefTable(2, 2)
			if e != nil {
				t.Fatal(e)
			}
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
			code, e := Compile(cfg, r9ConvertedPublisher(true))
			if e != nil {
				t.Fatal(e)
			}
			publisher, e := instantiateCore(code, InstantiateOptions{store: store, Imports: testImports("p.slot", handle), GC: GCConfig{NurseryBytes: 4096, SurvivorBytes: 4096, NurseryRegionShift: shift}})
			if e != nil {
				t.Fatal(e)
			}
			r9HostAssertRegion(t, publisher, shift)
			if _, e = publisher.Invoke("publish"); e != nil {
				t.Fatal(e)
			}
			c := r9HostContainer(t, store, handle.desc)
			r := r9HostAssertReceipt(t, c, 0)
			if e = handle.SetValue(1, Value{typ: ValExternRef, bits: r.identity.publicExtern}); e != nil {
				t.Fatal(e)
			}
			r9HostAssertReceipt(t, c, 1)
			if e = publisher.Close(); e != nil {
				t.Fatal(e)
			}
			if publisher.hasPhysicalResources() {
				t.Fatal("STOP initializer publisher retained")
			}
			r9HostAssertReceipt(t, c, 0)
			r9HostAssertReceipt(t, c, 1)
			imp := append(append(wasmtest.Name("p"), wasmtest.Name("slot")...), 1, 0x6f, 0, 2)
			segment := func(offset byte) []byte { return []byte{6, 0, 0x41, offset, 0x0b, 0x6f, 1, 0xd0, 0x6f, 0x0b} }
			// A completed first segment must commit even though the second traps.
			bad, e := Compile(cfg, wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(9, wasmtest.Vec(segment(0), segment(2)))))
			if e != nil {
				t.Fatal(e)
			}
			if _, e = instantiateCore(bad, InstantiateOptions{store: store, Imports: testImports("p.slot", handle)}); e == nil {
				t.Fatal("missing later-segment bounds trap")
			}
			r9HostAssertReceipt(t, c, 1)
			if c.slots[0] != nil || binary.LittleEndian.Uint64(handle.desc[8:]) != 0 || len(c.slots) != 1 {
				t.Fatal("earlier completed segment lost its effects")
			}
			good, e := Compile(cfg, wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(9, wasmtest.Vec(segment(1)))))
			if e != nil {
				t.Fatal(e)
			}
			initialized, e := instantiateCore(good, InstantiateOptions{store: store, Imports: testImports("p.slot", handle)})
			if e != nil {
				t.Fatal(e)
			}
			if len(c.slots) != 0 || store.gcDomainForCollector(r.identity.domain.collector) != nil {
				t.Fatal("initializer retained final domain")
			}
			_ = initialized.Close()
			_ = handle.Close()
			_ = code.Close()
			_ = bad.Close()
			_ = good.Close()
			_ = rt.Close()
		})
	}
}

func TestR9H2HostImportCycle(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		t.Run(fmt.Sprint(shift), func(t *testing.T) {
			rt := NewRuntime()
			r9HostStopped = append(r9HostStopped, rt)
			store := rt.refStore
			handle, e := rt.NewExternRefGlobal(NullExternRef(), true)
			if e != nil {
				t.Fatal(e)
			}
			imp := append(append(wasmtest.Name("p"), wasmtest.Name("g")...), 3, 0x6f, 1)
			code, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x70, 1}, []byte{0x60, 0, 0})), wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{1})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))), wasmtest.Section(9, wasmtest.Vec([]byte{3, 0, 1, 0})), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}), wasmtest.Code([]byte{0xd2, 0, 0xfb, 0, 0, 0xfb, 0x1b, 0x24, 0, 0x0b})))))
			if e != nil {
				t.Fatal(e)
			}
			producer, e := instantiateCore(code, InstantiateOptions{store: store, Imports: testImports("p.g", handle), GC: GCConfig{NurseryBytes: 4096, SurvivorBytes: 4096, NurseryRegionShift: shift, VerifyAfterCollect: true}})
			if e != nil {
				t.Fatal(e)
			}
			r9HostAssertRegion(t, producer, shift)
			if _, e = producer.Invoke("run"); e != nil {
				t.Fatal(e)
			}
			c := r9HostContainer(t, store, handle.cell)
			r := r9HostAssertReceipt(t, c, 0)
			collector := r.identity.domain.collector
			state, ok := managedFunctionCollectors.Load(collector)
			if !ok || len(state.(*managedFunctionCollector).envs) != 1 {
				t.Fatal("STOP missing real converted producer edge")
			}
			graph := state.(*managedFunctionCollector).graph
			var released atomic.Int32
			var locked atomic.Bool
			producer.referenceLifetime().afterPhysicalRelease(func() {
				if nativeExecutionMu.TryLock() {
					nativeExecutionMu.Unlock()
				} else {
					locked.Store(true)
				}
				if graph.collecting.TryLock() {
					graph.collecting.Unlock()
				} else {
					locked.Store(true)
				}
				topology := store.ensureGCTopology()
				topology.mu.Lock()
				if topology.readers != 0 || topology.writer {
					locked.Store(true)
				}
				topology.mu.Unlock()
				released.Add(1)
			})
			if e = producer.Close(); e != nil {
				t.Fatal(e)
			}
			r9HostAssertReceipt(t, c, 0)
			if !producer.hasPhysicalResources() || released.Load() != 0 {
				t.Fatal("STOP live external host container failed to root cycle")
			}
			graph.pendingCollection.Store(true)
			graph.drainAfterGate(0)
			r9HostAssertReceipt(t, c, 0)
			if !producer.hasPhysicalResources() {
				t.Fatal("STOP host cycle lost producer")
			}
			if e = handle.SetValue(Value{typ: ValExternRef}); e != nil {
				t.Fatal(e)
			}
			drainManagedFunctionStoreReclaims(store)
			if producer.hasPhysicalResources() || released.Load() != 1 || !r.released || locked.Load() || store.gcDomainForCollector(collector) != nil {
				t.Fatalf("final host clear retained cycle or reclaimed under locks: released=%d locked=%v", released.Load(), locked.Load())
			}
			_ = handle.Close()
			_ = code.Close()
			_ = rt.Close()
		})
	}
}

func TestR9H2HostImportTableIndependentPhysical(t *testing.T) {
	rt := NewRuntime()
	r9HostStopped = append(r9HostStopped, rt)
	store := rt.refStore
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	a, e := rt.NewExternRefTable(1, 1)
	if e != nil {
		t.Fatal(e)
	}
	b, e := rt.NewExternRefTable(1, 1)
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
	r9HostAssertRegion(t, publisher, 0)
	if _, e = publisher.Invoke("publish"); e != nil {
		t.Fatal(e)
	}
	store.mu.Lock()
	ca, cb := store.gcDomains.conversionContainers[&a.desc[0]], store.gcDomains.conversionContainers[&b.desc[0]]
	store.mu.Unlock()
	if ca == nil || cb == nil || ca == cb {
		t.Fatal("STOP independent table ownership missing")
	}
	ra, rb := r9HostAssertReceipt(t, ca, 0), r9HostAssertReceipt(t, cb, 0)
	if ra.identity != rb.identity || ra.root == rb.root {
		t.Fatal("STOP independent checked roots missing")
	}
	if e = publisher.Close(); e != nil {
		t.Fatal(e)
	}
	r9HostAssertReceipt(t, ca, 0)
	r9HostAssertReceipt(t, cb, 0)
	foreign := NewRuntime()
	r9HostStopped = append(r9HostStopped, foreign)
	other, e := foreign.NewExternRefTable(1, 1)
	if e != nil {
		t.Fatal(e)
	}
	if e = other.SetValue(0, Value{typ: ValExternRef, bits: ra.identity.publicExtern}); e == nil {
		t.Fatal("cross-Runtime public identity admitted")
	}
	if e = a.SetValue(0, Value{typ: ValExternRef}); e != nil {
		t.Fatal(e)
	}
	r9HostAssertReceipt(t, cb, 0)
	if e = b.SetValue(0, Value{typ: ValExternRef}); e != nil {
		t.Fatal(e)
	}
	if store.gcDomainForCollector(ra.identity.domain.collector) != nil {
		t.Fatal("final independent owner retained collector")
	}
	_ = a.Close()
	_ = b.Close()
	_ = other.Close()
	_ = foreign.Close()
	_ = rt.Close()
	_ = cc.Close()
}

func TestR9H2HostImportAuthority(t *testing.T) {
	rt := NewRuntime()
	r9HostStopped = append(r9HostStopped, rt)
	store := rt.refStore
	a, e := rt.NewExternRefTable(1, 1)
	if e != nil {
		t.Fatal(e)
	}
	b, e := rt.NewExternRefTable(1, 1)
	if e != nil {
		t.Fatal(e)
	}
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	clearerCode, e := Compile(cfg, r9HostClearer(true))
	if e != nil {
		t.Fatal(e)
	}
	// Compile and instantiate before either of the independent private domains.
	writer, e := instantiateCore(clearerCode, InstantiateOptions{store: store, Imports: testImports("p.slot", a)})
	if e != nil {
		t.Fatal(e)
	}
	code, e := Compile(cfg, r9ConvertedPublisher(true))
	if e != nil {
		t.Fatal(e)
	}
	var containers []*gcConversionContainer
	var publishers []*Instance
	for _, table := range []*Table{a, b} {
		publisher, err := instantiateCore(code, InstantiateOptions{store: store, Imports: testImports("p.slot", table)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = publisher.Invoke("publish"); err != nil {
			t.Fatal(err)
		}
		c := r9HostContainer(t, store, table.desc)
		r9HostAssertReceipt(t, c, 0)
		containers = append(containers, c)
		publishers = append(publishers, publisher)
	}
	ra, rb := r9HostAssertReceipt(t, containers[0], 0), r9HostAssertReceipt(t, containers[1], 0)
	if ra.identity.domain == rb.identity.domain {
		t.Fatal("STOP fixture did not create independent domains")
	}
	for _, publisher := range publishers {
		if e = publisher.Close(); e != nil {
			t.Fatal(e)
		}
		if publisher.hasPhysicalResources() {
			t.Fatal("STOP publisher retained")
		}
	}
	r9HostAssertReceipt(t, containers[0], 0)
	r9HostAssertReceipt(t, containers[1], 0)
	graphValue, ok := managedFunctionGraphs.Load(store)
	if !ok {
		t.Fatal("STOP missing receipt-only host graph")
	}
	graph := graphValue.(*managedFunctionGraph)
	nativeExecutionMu.Lock()
	graph.pendingCollection.Store(true)
	graph.drainAfterGate(0)
	graph.hostRetry.mu.Lock()
	idle, running := graph.hostRetry.idle, graph.hostRetry.running
	graph.hostRetry.mu.Unlock()
	if !running {
		nativeExecutionMu.Unlock()
		t.Fatal("STOP missing host authority contention waiter")
	}
	for i := 0; i < 8; i++ {
		graph.pendingCollection.Store(true)
		graph.drainAfterGate(0)
	}
	nativeExecutionMu.Unlock()
	select {
	case <-idle:
	case <-time.After(5 * time.Second):
		t.Fatal("host authority retry wakeup lost")
	}
	r9HostAssertReceipt(t, containers[0], 0)
	r9HostAssertReceipt(t, containers[1], 0)
	// The old writer must admit both receipt-only domains before entering native
	// execution, although only one of the two physical containers is imported.
	lease := writer.lockGCInvocation(newInvocationID())
	admitted := lease.topology != nil && lease.domains.len() == 2 && lease.domains.ownedBy(lease.owner)
	lease.unlock()
	if !admitted {
		t.Fatal("STOP incomplete pre-admitted host receipt domains")
	}
	before := append([]byte(nil), a.desc...)
	rejected := func() (caught bool) {
		defer func() { caught = recover() != nil }()
		writer.dispatchConversionStore(53, []uint64{0, 0, 0}, nil)
		return false
	}()
	if !rejected || !bytes.Equal(before, a.desc) || containers[0].slots[0] != ra {
		t.Fatal("helper published without invocation authority")
	}
	if _, e = writer.Invoke("clear"); e != nil {
		t.Fatal(e)
	}
	r9HostAssertReceipt(t, containers[1], 0)
	if store.gcDomainForCollector(ra.identity.domain.collector) != nil {
		t.Fatal("first final claim not drained")
	}
	if e = b.SetValue(0, Value{typ: ValExternRef}); e != nil {
		t.Fatal(e)
	}
	if store.gcDomainForCollector(rb.identity.domain.collector) != nil {
		t.Fatal("second final claim not drained")
	}
	_ = writer.Close()
	_ = a.Close()
	_ = b.Close()
	_ = code.Close()
	_ = clearerCode.Close()
	_ = rt.Close()
}

func TestR9H2HostImportGlobalOrdinaryAndGuards(t *testing.T) {
	rt := NewRuntime()
	r9HostStopped = append(r9HostStopped, rt)
	handle, e := rt.NewExternRefGlobal(NullExternRef(), true)
	if e != nil {
		t.Fatal(e)
	}
	imp := append(append(wasmtest.Name("p"), wasmtest.Name("slot")...), 3, 0x6f, 1)
	raw := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 1, 0x6f, 0})), wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("set", 0, 0), wasmtest.ExportEntry("slot", 3, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x24, 0, 0x0b}))))
	code, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), raw)
	if e != nil {
		t.Fatal(e)
	}
	writer, e := instantiateCore(code, InstantiateOptions{store: rt.refStore, Imports: testImports("p.slot", handle)})
	if e != nil {
		t.Fatal(e)
	}
	ref := issueExternref(t, rt, "host ordinary")
	if _, e = writer.InvokeValues(context.Background(), "set", ValueExternRef(ref)); e != nil {
		t.Fatal(e)
	}
	got, e := handle.GetValue()
	if e != nil || got != ValueExternRef(ref) {
		t.Fatalf("ordinary guest global: %v %v", got, e)
	}
	before := append([]byte(nil), handle.cell...)
	if e = writer.SetGlobalValue("slot", ValueI32(1)); e == nil || !bytes.Equal(before, handle.cell) {
		t.Fatal("wrong-type public forwarder changed host global")
	}
	foreign := NewRuntime()
	r9HostStopped = append(r9HostStopped, foreign)
	other := issueExternref(t, foreign, "foreign")
	if e = writer.SetGlobalValue("slot", ValueExternRef(other)); e == nil || !bytes.Equal(before, handle.cell) {
		t.Fatal("foreign token changed host global")
	}
	if e = writer.SetGlobalValue("slot", ValueExternRef(NullExternRef())); e != nil {
		t.Fatal(e)
	}
	if got, e = handle.GetValue(); e != nil || got.bits != 0 {
		t.Fatal("public forwarder did not clear host global")
	}
	immutable, e := rt.NewExternRefGlobal(ref, false)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = instantiateCore(code, InstantiateOptions{store: rt.refStore, Imports: testImports("p.slot", immutable)}); e == nil {
		t.Fatal("mutable import admitted immutable global")
	}
	if e = immutable.SetValue(ValueExternRef(NullExternRef())); e == nil {
		t.Fatal("immutable host global mutated")
	}
	_ = writer.Close()
	_ = handle.Close()
	_ = immutable.Close()
	_ = code.Close()
	_ = foreign.Close()
	_ = rt.Close()
}

func TestR9H2HostImportTableOrdinaryAndGuards(t *testing.T) {
	rt := NewRuntime()
	r9HostStopped = append(r9HostStopped, rt)
	handle, e := rt.NewExternRefTable(1, 1)
	if e != nil {
		t.Fatal(e)
	}
	imp := append(append(wasmtest.Name("p"), wasmtest.Name("slot")...), 1, 0x6f, 0, 1)
	raw := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 2, 0x7f, 0x6f, 0})), wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("set", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x26, 0, 0x0b}))))
	code, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), raw)
	if e != nil {
		t.Fatal(e)
	}
	writer, e := instantiateCore(code, InstantiateOptions{store: rt.refStore, Imports: testImports("p.slot", handle)})
	if e != nil {
		t.Fatal(e)
	}
	ref := issueExternref(t, rt, "host ordinary")
	if _, e = writer.InvokeValues(context.Background(), "set", ValueI32(0), ValueExternRef(ref)); e != nil {
		t.Fatal(e)
	}
	if binary.LittleEndian.Uint64(handle.desc[8:]) != ref.token {
		t.Fatal("ordinary guest table store lost token")
	}
	before := append([]byte(nil), handle.desc...)
	if _, e = writer.InvokeValues(context.Background(), "set", ValueI32(1), ValueExternRef(NullExternRef())); e == nil || !bytes.Equal(before, handle.desc) {
		t.Fatal("bounds trap changed host table")
	}
	if e = handle.SetValue(0, ValueI32(1)); e == nil || !bytes.Equal(before, handle.desc) {
		t.Fatal("wrong-type public forwarder changed host table")
	}
	foreign := NewRuntime()
	r9HostStopped = append(r9HostStopped, foreign)
	if _, e = instantiateCore(code, InstantiateOptions{store: foreign.refStore, Imports: testImports("p.slot", handle)}); e == nil {
		t.Fatal("foreign store imported host table")
	}
	if _, e = instantiateCore(code, InstantiateOptions{store: rt.refStore, Imports: testImports("p.slot", &Table{desc: handle.desc})}); e == nil {
		t.Fatal("ownerless handle imported")
	}
	if _, e = writer.InvokeValues(context.Background(), "set", ValueI32(0), ValueExternRef(NullExternRef())); e != nil {
		t.Fatal(e)
	}
	if binary.LittleEndian.Uint64(handle.desc[8:]) != 0 {
		t.Fatal("guest null did not clear host table")
	}
	_ = writer.Close()
	_ = handle.Close()
	_ = code.Close()
	_ = foreign.Close()
	_ = rt.Close()
}
