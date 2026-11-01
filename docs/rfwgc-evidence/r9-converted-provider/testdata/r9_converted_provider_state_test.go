//go:build linux && amd64 && !tinygo

package wago

import (
	"fmt"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"reflect"
	"testing"
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
func TestR9ConvertedProviderState(t *testing.T) {
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
						t.Skipf("ADMISSION publisher compile: %v", err)
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
						t.Skipf("ADMISSION publisher instantiate before publication: %v", err)
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
					before := collector.Stats()
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
					after := collector.Stats()
					if before.MinorCollections != after.MinorCollections || before.FullCollections != after.FullCollections {
						t.Fatal("STOP unexpected automatic collection")
					}
					t.Logf("publication: conversion entries=%d uses=%d checkedRoot=true providerCollector=%t storeOwned=%t persistentSlots=%d", n, entry.uses, p.gc != nil, store.ownsGCCollector(collector), slots)
					if err = c.Close(); err != nil {
						t.Fatal(err)
					}
					// Inspect only Go ownership metadata before any further collector/container API.
					conversion.mu.Lock()
					closed := conversion.closed
					count := conversion.count
					attached = conversion.collector == collector && conversion.store == store
					conversion.mu.Unlock()
					t.Logf("retirement: conversionClosed=%t entries=%d attached=%t publisherLive=%t providerLive=%t", closed, count, attached, live(c), live(p))
					if closed || count == 0 || !attached || !live(p) {
						t.Fatal("STOP published converted value lost its root lease; no collection, container access or teardown follows")
					}
					// This gate must be extended with provider-backed lease identity and clear/release
					// assertions if retention is repaired. Merely keeping a publisher alive is insufficient.
					t.Fatal("STOP retention changed: qualify provider-backed lease and eventual clear/release before ABSENT")
				})
			}
		}
	}
}
