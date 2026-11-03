//go:build ((linux && amd64) || (darwin && arm64)) && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"fmt"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"reflect"
	"testing"
)

// A valid failed-start fixture: a declared function is nested in a struct/array,
// published to an imported anyref global, and start traps. There is no public
// funcref token, direct funcref container, or invocation of the escaped function.
func r9OwnershipNestedModule(array, nested, trap bool) []byte {
	aggregate := []byte{0x5f, 1, 0x70, 1}
	allocation := []byte{0xfb, 0, 0}
	if array {
		aggregate = []byte{0x5e, 0x70, 1}
		allocation = []byte{0xfb, 8, 0, 1}
	}
	g := append(append(wasmtest.Name("p"), wasmtest.Name("slot")...), 3, 0x6e, 1)
	probe := append(append(wasmtest.Name("env"), wasmtest.Name("probe")...), 0, 1)
	body := append([]byte{0xd2, 1}, allocation...)
	if nested {
		body = append(body, 0xfb, 0, 2, 0xfb, 0, 2)
	}
	body = append(body, 0x24, 0, 0x10, 0)
	if trap {
		body = append(body, 0)
	}
	body = append(body, 0x0b)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(aggregate, []byte{0x60, 0, 0}, []byte{0x5f, 1, 0x6e, 1})), wasmtest.Section(2, wasmtest.Vec(g, probe)), wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{1})), wasmtest.Section(8, []byte{2}), wasmtest.Section(9, wasmtest.Vec([]byte{3, 0, 1, 1})), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}), wasmtest.Code(body))))
}

// Keep failed probes reachable until process exit. On a violated ownership
// assertion we do not touch the published aggregate or run further teardown.
var r9OwnershipStoppedStores []*referenceStore

func TestR9OwnershipStateH6FailedInitialization(t *testing.T) {
	for _, cycle := range []bool{false, true} {
		for _, shift := range []uint8{0, 6} {
			for _, nested := range []bool{false, true} {
				for _, trap := range []bool{false, true} {
					for _, array := range []bool{false, true} {
						t.Run(fmt.Sprintf("cycle=%t/region=%d/nested=%t/trap=%t/array=%t", cycle, shift, nested, trap, array), func(t *testing.T) {
							cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
							c, err := Compile(cfg, r9OwnershipNestedModule(array, nested, trap))
							if err != nil {
								t.Fatalf("ADMISSION nested fixture: %v", err)
							}
							// A live provider in a collector domain. Its allocator is never invoked.
							providerSource := wasmtest.Module(
								wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, []byte{0x60, 0, 0})),
								wasmtest.Section(3, wasmtest.Vec([]byte{1})),
								wasmtest.Section(6, wasmtest.Vec([]byte{0x6e, 1, 0xd0, 0x6e, 0x0b})),
								wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("slot", 3, 0), wasmtest.ExportEntry("allocate", 0, 0))),
								wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfb, 1, 0, 0x1a, 0x0b}))))
							pc, err := Compile(cfg, providerSource)
							if err != nil {
								t.Fatal(err)
							}
							store := newReferenceStore(false)
							r9OwnershipStoppedStores = append(r9OwnershipStoppedStores, store)
							opts := InstantiateOptions{store: store, GC: GCConfig{NurseryBytes: 4096, SurvivorBytes: 4096, NurseryRegionShift: shift, VerifyAfterCollect: true}}
							p, err := instantiateCore(pc, opts)
							if err != nil {
								t.Fatalf("ADMISSION provider: %v", err)
							}
							if p.gc == nil || !p.hasPhysicalResources() {
								t.Fatal("STOP provider domain absent")
							}
							g, err := p.ExportedGlobalObject("slot")
							if err != nil {
								t.Fatal(err)
							}
							if got := reflect.ValueOf(p.gc).Elem().FieldByName("edenRegions").Len() != 0; got != (shift != 0) {
								t.Fatal("STOP wrong region policy")
							}
							var producer *Instance
							released := 0
							beforeRefs := 0
							observed := false
							checkpointOK := false
							var callbackErr error
							probe := slotHostFunc(func(host HostModule, _ []uint64, _ []uint64) {
								store.mu.Lock()
								for in, state := range store.instances {
									if in != p && !state.resourcesReleased {
										producer = in
										break
									}
								}
								tokenCount := len(store.byToken)
								store.mu.Unlock()
								if producer == nil {
									return
								}
								observed = true
								producer.lifeMu.Lock()
								beforeRefs = producer.resourceRefs
								physical := !producer.resourcesClosed
								producer.lifeMu.Unlock()
								rootCount := 0
								store.rangeGCDomainPersistentRoots(p.gc, func(slot gc.RootSlot) bool {
									if slot.GetRef() != 0 {
										rootCount++
									}
									return true
								}, nil)
								checkpointOK = physical && producer.gc == p.gc && tokenCount == 0 && rootCount >= 1
								t.Logf("publication checkpoint: nonzero root slots=%d producerRefs=%d tokens=%d sameDomain=%t", rootCount, beforeRefs, tokenCount, producer.gc == p.gc)
								producer.referenceLifetime().afterPhysicalRelease(func() { released++ })
								if !checkpointOK {
									return
								}
								if _, callbackErr = p.InvokeFromHost(context.Background(), host, "allocate"); callbackErr != nil {
									return
								}
								if !producer.hasPhysicalResources() || released != 0 {
									checkpointOK = false
									return
								}
								callbackErr = host.(GCHostModule).CollectGC()
								if !producer.hasPhysicalResources() || released != 0 {
									checkpointOK = false
								}

							})
							opts.Imports = testImports("p.slot", g, "env.probe", probe)
							failed, startErr := instantiateCore(c, opts)
							if !observed {
								if failed != nil {
									failed.Close()
								}
								t.Fatalf("ADMISSION/checkpoint not reached: %v", startErr)
							}
							if !checkpointOK {
								t.Fatal("STOP pre-cleanup ownership checkpoint failed")
							}
							if callbackErr != nil {
								t.Fatal(callbackErr)
							}
							if trap {
								if failed != nil || startErr == nil {
									t.Fatal("expected failed initialization")
								}
							} else {
								if failed != producer || startErr != nil {
									t.Fatalf("successful start: %v", startErr)
								}
								if !producer.hasPhysicalResources() || released != 0 {
									t.Fatal("STOP owner lost before close")
								}
								if err := producer.Close(); err != nil {
									t.Fatal(err)
								}
							}
							producer.lifeMu.Lock()
							afterRefs := producer.resourceRefs
							closed := producer.resourcesClosed
							producer.lifeMu.Unlock()
							store.mu.Lock()
							state := store.instances[producer]
							registered := state != nil && !state.resourcesReleased
							tokens := len(store.byToken)
							store.mu.Unlock()
							t.Logf("cleanup: beforeRefs=%d afterRefs=%d releasedCallbacks=%d resourcesClosed=%t registered=%t publicTokens=%d error=%v", beforeRefs, afterRefs, released, closed, registered, tokens, startErr)
							if closed || released != 0 || !registered {
								t.Fatal("STOP nested edge did not retain producer ownership; no escaped code invoked, no aggregate inspected, no collection/teardown attempted")
							}
							if tokens != 0 {
								t.Fatal("STOP public token masks nested retention")
							}
							// Only after retention is established may the ordinary API drop the edge.
							if cycle {
								if err := p.Close(); err != nil {
									t.Fatal(err)
								}
							} else if err := g.SetValue(ValueGCRef(GCRef{})); err != nil {
								t.Fatal(err)
							}
							if producer.hasPhysicalResources() || released != 1 {
								t.Fatal("owner not released at edge-clear quiescence")
							}
							if err := p.Close(); err != nil {
								t.Fatal(err)
							}
							store.closeRuntime()
							if producer.hasPhysicalResources() || released != 1 {
								t.Fatal("producer resources not eventually released")
							}
							c.Close()
							pc.Close()
						})
					}
				}

			}
		}
	}

}

// Drop the last persistent edge inside start's host callback. A raw function
// could still be parked in native frames: collect only with the conservative
// owner slot, and require reclamation at the outer failed-start boundary.
func TestR9OwnershipStateH6CallbackQuiescence(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		t.Run(fmt.Sprint(shift), func(t *testing.T) {
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
			providerSource := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, []byte{0x60, 0, 0})),
				wasmtest.Section(3, wasmtest.Vec([]byte{1})),
				wasmtest.Section(6, wasmtest.Vec([]byte{0x6e, 1, 0xd0, 0x6e, 0x0b})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("slot", 3, 0), wasmtest.ExportEntry("allocate", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfb, 1, 0, 0x1a, 0x0b}))))
			pc, err := Compile(cfg, providerSource)
			if err != nil {
				t.Fatal(err)
			}
			c, err := Compile(cfg, r9OwnershipNestedModule(false, true, true))
			if err != nil {
				t.Fatal(err)
			}
			store := newReferenceStore(false)
			r9OwnershipStoppedStores = append(r9OwnershipStoppedStores, store)
			opts := InstantiateOptions{store: store, GC: GCConfig{NurseryBytes: 4096, SurvivorBytes: 4096, NurseryRegionShift: shift, VerifyAfterCollect: true}}
			p, err := instantiateCore(pc, opts)
			if err != nil {
				t.Fatal(err)
			}
			g, err := p.ExportedGlobalObject("slot")
			if err != nil {
				t.Fatal(err)
			}
			for round := 0; round < 8; round++ {
				var producer *Instance
				released := 0
				var callbackErr error
				checkpoint := false
				opts.Imports = testImports("p.slot", g, "env.probe", slotHostFunc(func(host HostModule, _ []uint64, _ []uint64) {
					store.mu.Lock()
					for in, entry := range store.instances {
						if in != p && !entry.resourcesReleased {
							producer = in
							break
						}
					}
					store.mu.Unlock()
					if producer == nil || !producer.hasPhysicalResources() {
						return
					}
					producer.referenceLifetime().afterPhysicalRelease(func() { released++ })
					if callbackErr = producer.Close(); callbackErr != nil {
						return
					}
					if !producer.hasPhysicalResources() || released != 0 {
						return
					}
					if callbackErr = g.SetValue(ValueGCRef(GCRef{})); callbackErr != nil {
						return
					}
					if !producer.hasPhysicalResources() || released != 0 {
						return
					}
					if _, callbackErr = p.InvokeFromHost(context.Background(), host, "allocate"); callbackErr != nil {
						return
					}
					if !producer.hasPhysicalResources() || released != 0 {
						return
					}
					if callbackErr = host.(GCHostModule).CollectGC(); callbackErr != nil {
						return
					}
					checkpoint = producer.hasPhysicalResources() && released == 0
					if value, ok := managedFunctionGraphs.Load(store); ok {
						graph := value.(*managedFunctionGraph)
						graph.hostRetry.mu.Lock()
						checkpoint = checkpoint && !graph.hostRetry.running
						graph.hostRetry.mu.Unlock()
					}
				}))
				_, startErr := instantiateCore(c, opts)
				if !checkpoint {
					t.Fatalf("STOP callback owner invariant round=%d error=%v start=%v", round, callbackErr, startErr)
				}
				if callbackErr != nil || startErr == nil {
					t.Fatalf("callback=%v start=%v", callbackErr, startErr)
				}
				if producer.hasPhysicalResources() || released != 1 {
					t.Fatalf("outermost failed-start boundary retained owner round=%d", round)
				}
				if state, ok := managedFunctionCollectors.Load(p.gc); ok && len(state.(*managedFunctionCollector).envs) != 0 {
					t.Fatal("repeated cycle retained environments")
				}
			}
			p.Close()
			store.closeRuntime()
			c.Close()
			pc.Close()
			if _, ok := managedFunctionGraphs.Load(store); ok {
				t.Fatal("closed store retained owner graph")
			}
			r9OwnershipStoppedStores = r9OwnershipStoppedStores[:len(r9OwnershipStoppedStores)-1]
		})
	}
}

func TestR9OwnershipStateH6CollectingMutations(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		for _, operation := range []string{"struct.set", "array.set", "array.fill", "mixed.new"} {
			t.Run(fmt.Sprintf("%s/region=%d", operation, shift), func(t *testing.T) {
				aggregate := []byte{0x5f, 1, 0x70, 1}
				body := []byte{0xfb, 1, 0, 0xd2, 0, 0xfb, 5, 0, 0, 0x0b}
				switch operation {
				case "array.set":
					aggregate = []byte{0x5e, 0x70, 1}
					body = []byte{0x41, 1, 0xfb, 7, 0, 0x41, 0, 0xd2, 0, 0xfb, 14, 0, 0x0b}
				case "array.fill":
					aggregate = []byte{0x5e, 0x70, 1}
					body = []byte{0x41, 2, 0xfb, 7, 0, 0x41, 0, 0xd2, 0, 0x41, 2, 0xfb, 16, 0, 0x0b}
				case "mixed.new":
					aggregate = []byte{0x5f, 2, 0x6e, 1, 0x70, 1}
					body = []byte{0xfb, 1, 2, 0xd2, 0, 0xfb, 0, 0, 0x1a, 0x0b}
				}
				source := wasmtest.Module(
					wasmtest.Section(1, wasmtest.Vec(aggregate, []byte{0x60, 0, 0}, []byte{0x5f, 0})),
					wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{1})),
					wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
					wasmtest.Section(9, wasmtest.Vec([]byte{3, 0, 1, 0})),
					wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}), wasmtest.Code(body))))
				c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), source)
				if err != nil {
					t.Fatal(err)
				}
				in, err := instantiateCore(c, InstantiateOptions{GC: GCConfig{NurseryBytes: 4096, SurvivorBytes: 4096, NurseryRegionShift: shift, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true}})
				if err != nil {
					t.Fatal(err)
				}
				if !in.hasPhysicalResources() {
					t.Fatal("STOP missing producer")
				}
				var slotBound uint32
				for round := 0; round < 8; round++ {
					if !in.hasPhysicalResources() {
						t.Fatal("STOP owner lost before collecting mutation")
					}
					if _, err = in.Invoke("run"); err != nil {
						t.Fatal(err)
					}
					state, ok := managedFunctionCollectors.Load(in.gc)
					if !ok || len(state.(*managedFunctionCollector).envs) != 0 || len(state.(*managedFunctionCollector).hostEnvSlots) != 0 {
						t.Fatal("quiescent live owner retained a dropped function environment")
					}
					slots := in.gc.PersistentSlotCount(gc.SlotGlobal)
					if round == 0 {
						slotBound = slots
					} else if slots != slotBound {
						t.Fatal("repeated live-owner drops grew floating root slots")
					}
				}
				if got := reflect.ValueOf(in.gc).Elem().FieldByName("edenRegions").Len() != 0; got != (shift != 0) {
					t.Fatal("wrong actual region")
				}
				if stats := in.gc.Stats(); stats.MinorCollections+stats.FullCollections == 0 {
					t.Fatal("no actual collection")
				}
				released := 0
				in.referenceLifetime().afterPhysicalRelease(func() { released++ })
				if err = in.Close(); err != nil {
					t.Fatal(err)
				}
				if in.hasPhysicalResources() || released != 1 {
					t.Fatal("closed mutation producer retained")
				}
				c.Close()
			})
		}
	}
}
