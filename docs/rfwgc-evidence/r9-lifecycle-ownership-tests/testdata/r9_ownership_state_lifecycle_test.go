//go:build linux && amd64 && !tinygo

package wago

import (
	"fmt"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

// These probes never collect, dereference object payloads, or call escaped code.
// Null-only H2 preflight deliberately stops before publishing a managed object.
func r9OwnershipProvider(table, harmless bool, ref byte) []byte {
	var sections [][]byte
	if harmless {
		sections = append(sections, wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 0})))
	}
	if table {
		sections = append(sections, wasmtest.Section(4, wasmtest.Vec([]byte{ref, 0, 1})))
	} else {
		sections = append(sections, wasmtest.Section(6, wasmtest.Vec([]byte{ref, 1, 0xd0, ref, 0x0b})))
	}
	kind := byte(3)
	if table {
		kind = 1
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("slot", kind, 0))))
	return wasmtest.Module(sections...)
}

func r9OwnershipConsumer(table bool, ref byte) []byte {
	imp := append(wasmtest.Name("p"), wasmtest.Name("slot")...)
	if table {
		imp = append(imp, 1, ref, 0, 1)
	} else {
		imp = append(imp, 3, ref, 1)
	}
	// One unused allocator establishes ordinary collector admission; never invoked.
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, []byte{0x60, 0, 0})),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("allocate", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfb, 1, 0, 0x1a, 0x0b}))))
}

func r9OwnershipTokenConsumer(table bool) []byte {
	imp := append(wasmtest.Name("p"), wasmtest.Name("slot")...)
	put := []byte{0x20, 0, 0x24, 0, 0x0b}
	get := []byte{0x23, 0, 0x0b}
	if table {
		imp = append(imp, 1, 0x6f, 0, 1)
		put = []byte{0x41, 0, 0x20, 0, 0x26, 0, 0x0b}
		get = []byte{0x41, 0, 0x25, 0, 0x0b}
	} else {
		imp = append(imp, 3, 0x6f, 1)
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, []byte{0x60, 0, 0}, []byte{0x60, 1, 0x6f, 0}, []byte{0x60, 0, 1, 0x6f})),
		wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2}, []byte{3})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("allocate", 0, 0), wasmtest.ExportEntry("put", 0, 1), wasmtest.ExportEntry("get", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfb, 1, 0, 0x1a, 0x0b}), wasmtest.Code(put), wasmtest.Code(get))))
}

func TestR9OwnershipStateH2NullPreflight(t *testing.T) {
	for _, table := range []bool{false, true} {
		for _, harmless := range []bool{false, true} {
			for _, ref := range []byte{0x6e, 0x6f} {
				t.Run(fmt.Sprintf("table=%t/type=%t/ref=%x", table, harmless, ref), func(t *testing.T) {
					cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
					pc, err := Compile(cfg, r9OwnershipProvider(table, harmless, ref))
					if err != nil {
						t.Skipf("ADMISSION provider compile: %v", err)
					}
					consumerSource := r9OwnershipConsumer(table, ref)
					if ref == 0x6f {
						consumerSource = r9OwnershipTokenConsumer(table)
					}
					cc, err := Compile(cfg, consumerSource)
					if err != nil {
						pc.Close()
						t.Skipf("ADMISSION consumer compile: %v", err)
					}
					store := newReferenceStore(false)
					p, err := instantiateCore(pc, InstantiateOptions{store: store})
					if err != nil {
						t.Skipf("ADMISSION provider instantiate: %v", err)
					}
					t.Logf("provider initial collector=%p physical=%t", p.gc, p.hasPhysicalResources())
					imports := NewImports()
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
					c, err := instantiateCore(cc, InstantiateOptions{store: store, Imports: imports})
					if err != nil {
						p.Close()
						store.closeRuntime()
						cc.Close()
						pc.Close()
						t.Skipf("ADMISSION consumer instantiate: %v", err)
					}
					if !p.hasPhysicalResources() || !c.hasPhysicalResources() {
						t.Fatal("STOP physical ownership absent")
					}
					store.mu.Lock()
					ps := store.instances[p]
					cs := store.instances[c]
					registered := ps != nil && cs != nil && !ps.resourcesReleased && !cs.resourcesReleased
					store.mu.Unlock()
					if !registered {
						t.Fatal("STOP registration absent")
					}
					collector := c.gc
					count := func() int {
						n := 0
						store.rangeGCDomainPersistentRoots(collector, func(slot gc.RootSlot) bool {
							if slot.GetRef() != 0 {
								t.Fatal("STOP unexpected non-null root")
							}
							n++
							return true
						}, nil)
						return n
					}
					before := count()
					t.Logf("provider collector=%p consumer collector=%p storeOwned=%t null-root-slots=%d", p.gc, c.gc, store.ownsGCCollector(c.gc), before)
					var token ExternRef
					if ref == 0x6f {
						token, err = p.NewExternRef("r9-known-retained-value-42")
						if err != nil {
							t.Fatal(err)
						}
						if _, err = c.Invoke("put", token.token); err != nil {
							t.Fatal(err)
						}
						if got, ok := p.ExternRefValue(token); !ok || got != "r9-known-retained-value-42" {
							t.Fatal("STOP token ownership absent")
						}
					}
					// No managed object exists. Extern payload is retained by the live store.
					if err := c.Close(); err != nil {
						t.Fatal(err)
					}
					if !p.hasPhysicalResources() {
						t.Fatal("STOP provider resources disappeared")
					}
					after := count()
					t.Logf("after consumer close null-root-slots=%d", after)
					// Extern-token roots are not compact collector slots. Zero is not a failure there.
					if ref == 0x6e && (collector == nil || before == 0 || after == 0) {
						t.Fatalf("STOP compact root preflight incomplete: before=%d after=%d; no object published or collection attempted", before, after)
					}
					if ref == 0x6f {
						if got, ok := p.ExternRefValue(token); !ok || got != "r9-known-retained-value-42" {
							t.Fatal("STOP token ownership lost after consumer close")
						}
						reader, e := instantiateCore(cc, InstantiateOptions{store: store, Imports: imports})
						if e != nil {
							t.Fatal(e)
						}
						got, e := reader.Invoke("get")
						if e != nil || len(got) != 1 || got[0] != token.token {
							t.Fatalf("retained token mismatch: %v %v", got, e)
						}
						if _, e = reader.Invoke("put", 0); e != nil {
							t.Fatal(e)
						}
						reader.Close()
						if !p.ReleaseExternRef(token) {
							t.Fatal("token release failed")
						}
						t.Log("known extern token survives consumer close; cleared before release")
					}
					if err := p.Close(); err != nil {
						t.Fatal(err)
					}
					store.closeRuntime()
					cc.Close()
					pc.Close()
				})
			}
		}
	}
}

// A valid failed-start fixture: a declared function is nested in a struct/array,
// published to an imported anyref global, and start traps. There is no public
// funcref token, direct funcref container, or invocation of the escaped function.
func r9OwnershipNestedFailure(array bool) []byte {
	aggregate := []byte{0x5f, 1, 0x70, 1}
	allocation := []byte{0xfb, 0, 0}
	if array {
		aggregate = []byte{0x5e, 0x70, 1}
		allocation = []byte{0xfb, 8, 0, 1}
	}
	g := append(append(wasmtest.Name("p"), wasmtest.Name("slot")...), 3, 0x6e, 1)
	probe := append(append(wasmtest.Name("env"), wasmtest.Name("probe")...), 0, 1)
	body := append([]byte{0xd2, 1}, allocation...)
	body = append(body, 0x24, 0, 0x10, 0, 0, 0x0b)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(aggregate, []byte{0x60, 0, 0})), wasmtest.Section(2, wasmtest.Vec(g, probe)), wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{1})), wasmtest.Section(8, []byte{2}), wasmtest.Section(9, wasmtest.Vec([]byte{3, 0, 1, 1})), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}), wasmtest.Code(body))))
}

// Keep failed probes reachable until process exit. On a violated ownership
// assertion we do not touch the published aggregate or run further teardown.
var r9OwnershipStoppedStores []*referenceStore

func TestR9OwnershipStateH6FailedInitialization(t *testing.T) {
	for _, array := range []bool{false, true} {
		t.Run(fmt.Sprintf("array=%t", array), func(t *testing.T) {
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
			c, err := Compile(cfg, r9OwnershipNestedFailure(array))
			if err != nil {
				t.Skipf("ADMISSION nested fixture: %v", err)
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
			opts := InstantiateOptions{store: store, GC: GCConfig{DisableCollection: true}}
			p, err := instantiateCore(pc, opts)
			if err != nil {
				t.Skipf("ADMISSION provider: %v", err)
			}
			if p.gc == nil || !p.hasPhysicalResources() {
				t.Fatal("STOP provider domain absent")
			}
			g, err := p.ExportedGlobalObject("slot")
			if err != nil {
				t.Fatal(err)
			}
			var producer *Instance
			released := 0
			beforeRefs := 0
			observed := false
			checkpointOK := false
			probe := slotHostFunc(func(_ HostModule, _ []uint64, _ []uint64) {
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
			})
			opts.Imports = testImports("p.slot", g, "env.probe", probe)
			failed, startErr := instantiateCore(c, opts)
			if !observed {
				if failed != nil {
					failed.Close()
				}
				t.Skipf("ADMISSION/checkpoint not reached: %v", startErr)
			}
			if !checkpointOK {
				t.Fatal("STOP pre-cleanup ownership checkpoint failed")
			}
			if failed != nil || startErr == nil {
				t.Fatal("expected failed initialization")
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
			if err := g.Set(0); err != nil {
				t.Fatal(err)
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
