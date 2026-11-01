//go:build (linux && (amd64 || arm64 || riscv64)) || (darwin && arm64)

package wago

import (
	"context"
	"encoding/binary"
	"fmt"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"reflect"
	"testing"
)

func immutableNoneProviderModule(withType, mutable bool) []byte {
	var sections [][]byte
	if withType {
		sections = append(sections, wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 0})))
	}
	mut := byte(0)
	if mutable {
		mut = 1
	}
	sections = append(sections, wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0x71, mut, 0xd0, 0x71, 0x0b})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 3, 0))))
	return wasmtest.Module(sections...)
}

func TestGCImmutableNoneGlobalImport(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		for _, withType := range []bool{false, true} {
			for _, shape := range []struct {
				name string
				typ  []byte
			}{{"struct", []byte{0x5f, 0}}, {"array", []byte{0x5e, 0x7f, 0}}} {
				t.Run(fmt.Sprintf("region=%d/type=%t/%s", shift, withType, shape.name), func(t *testing.T) {
					cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
					rt := NewRuntime(WithRuntimeConfig(cfg))
					defer rt.Close()
					settings := GCConfig{NurseryBytes: 1024, SurvivorBytes: 1024, NurseryRegionShift: shift, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true}
					instantiate := func(runtime *Runtime, b []byte, imports *Imports) (*Instance, error) {
						t.Helper()
						c, err := Compile(cfg, b)
						if err != nil {
							t.Fatal(err)
						}
						defer c.Close()
						m, err := runtime.Module(c)
						if err != nil {
							t.Fatal(err)
						}
						defer m.Close()
						return runtime.Instantiate(context.Background(), m, WithGC(settings), WithImports(imports))
					}
					consumerBytes := func(mutable bool) []byte {
						mut := byte(0)
						if mutable {
							mut = 1
						}
						entry := append(append(wasmtest.Name("env"), wasmtest.Name("g")...), 3, 0x63, 0, mut)
						allocation := []byte{0xfb, 1, 0} // struct.new_default
						if shape.name == "array" {
							allocation = []byte{0x41, 1, 0xfb, 7, 0}
						} // array.new_default
						body := append(allocation, 0x1a, 0x23, 0, 0xd1, 0x0b)
						return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(shape.typ, []byte{0x60, 0, 1, 0x7f})), wasmtest.Section(2, wasmtest.Vec(entry)), wasmtest.Section(3, []byte{1, 1}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 3, 0), wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
					}
					provider, err := instantiate(rt, immutableNoneProviderModule(withType, false), NewImports())
					if err != nil {
						t.Fatal(err)
					}
					defer provider.Close()
					if provider.gc != nil {
						t.Fatal("fixture must be collector-free")
					}
					g, err := provider.ExportedGlobalObject("g")
					if err != nil {
						t.Fatal(err)
					}
					bindings := NewImports().Global("env", "g", g)
					consumer, err := instantiate(rt, consumerBytes(false), bindings)
					if err != nil {
						t.Fatal(err)
					}
					if v, err := consumer.GlobalValue("g"); err != nil || v.Bits() != 0 {
						t.Fatalf("null=%v,%v", v, err)
					}
					got, err := consumer.Invoke("run")
					if err != nil || len(got) != 1 || got[0] != 1 {
						t.Fatalf("run=%v,%v", got, err)
					}
					if consumer.gc == nil || consumer.gc.Stats().FullCollections == 0 {
						t.Fatal("no collection")
					}
					if enabled := reflect.ValueOf(consumer.gc).Elem().FieldByName("edenRegions").Len() != 0; enabled != (shift != 0) {
						t.Fatal("wrong actual policy")
					}
					consumer.Close()
					foreign := NewRuntime(WithRuntimeConfig(cfg))
					defer foreign.Close()
					if in, err := instantiate(foreign, consumerBytes(false), bindings); err == nil {
						in.Close()
						t.Fatal("cross-Runtime null import accepted")
					}
					// A null value without the exact none guarantee must not bypass admission.
					old := g.owner.valueType
					g.owner.valueType.Ref.Heap.Abstract = AbstractHeapAny
					if err := g.validateReferenceImportWithCollector(provider.refStore, nil); err == nil {
						t.Fatal("wider null slot bypassed domain")
					}
					g.owner.valueType = old
					binary.LittleEndian.PutUint64(g.cell, 1)
					if err := g.validateReferenceImportWithCollector(provider.refStore, nil); err == nil {
						t.Fatal("non-null none slot accepted")
					}
					binary.LittleEndian.PutUint64(g.cell, 0)
					provider.Close()
					if in, err := instantiate(rt, consumerBytes(false), bindings); err == nil {
						in.Close()
						t.Fatal("closed provider accepted")
					}
					mutable, err := instantiate(rt, immutableNoneProviderModule(withType, true), NewImports())
					if err != nil {
						t.Fatal(err)
					}
					defer mutable.Close()
					mg, err := mutable.ExportedGlobalObject("g")
					if err != nil {
						t.Fatal(err)
					}
					if in, err := instantiate(rt, consumerBytes(true), NewImports().Global("env", "g", mg)); err == nil {
						in.Close()
						t.Fatal("mutable covariance accepted")
					}
					if err := mg.validateReferenceImportWithCollector(mutable.refStore, nil); err == nil {
						t.Fatal("mutable null slot bypassed domain")
					}
				})
			}
		}
	}
}

func TestGCImmutableNoneObjectGlobalControl(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		t.Run(fmt.Sprint(shift), func(t *testing.T) {
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
			pc, err := Compile(cfg, gcSharedGlobalProviderModule(false))
			if err != nil {
				t.Fatal(err)
			}
			defer pc.Close()
			cc, err := Compile(cfg, gcSharedImmutableGlobalConsumerModule(false))
			if err != nil {
				t.Fatal(err)
			}
			defer cc.Close()
			store := newReferenceStore(false)
			defer store.closeRuntime()
			settings := GCConfig{NurseryBytes: 1024, SurvivorBytes: 1024, NurseryRegionShift: shift, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true}
			provider, err := instantiateCore(pc, InstantiateOptions{store: store, GC: settings})
			if err != nil {
				t.Fatal(err)
			}
			defer provider.Close()
			g, err := provider.ExportedGlobalObject("g")
			if err != nil {
				t.Fatal(err)
			}
			if err := g.validateReferenceImportWithCollector(store, nil); err == nil {
				t.Fatal("object import accepted without collector")
			}
			consumer, err := instantiateCore(cc, InstantiateOptions{store: store, GC: settings, Imports: testImports("provider.g", g)})
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			got, err := consumer.Invoke("read")
			if err != nil || len(got) != 1 || got[0] != 42 {
				t.Fatalf("read=%v,%v", got, err)
			}
			if provider.gc != consumer.gc || consumer.gc.Stats().FullCollections == 0 {
				t.Fatal("missing shared collection")
			}
		})
	}
}
