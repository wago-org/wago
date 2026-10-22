//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"testing"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestRV64ManagedFunctionPublicCollectionClosedCycle(t *testing.T) {
	for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
		for _, artifact := range []bool{false, true} {
			for _, closeOnly := range []bool{false, true} {
				for _, concurrent := range []bool{false, true} {
					t.Run(fmt.Sprintf("profile%d/artifact%t/close%t/concurrent%t", profile, artifact, closeOnly, concurrent), func(t *testing.T) {
						cfg := rv64I31Config()
						features := cfg.frontendFeatures()
						features.ExceptionHandling = true
						features.ExceptionReferences = true
						c, err := compileWithFrontendFeatures(cfg, ownedExceptionPublicModule(), features)
						if err != nil {
							t.Fatal(err)
						}
						defer c.Close()
						if artifact {
							blob, err := c.MarshalBinary()
							if err != nil {
								t.Fatal(err)
							}
							loaded, err := LoadTrustedArtifact(blob)
							if err != nil {
								t.Fatal(err)
							}
							defer loaded.Close()
							c = loaded
						}
						rt := NewRuntime()
						defer rt.Close()
						makeInstance := func() *Instance {
							in, err := instantiateCore(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{Profile: profile, TinyHeapBytes: 64 << 10, VerifyAfterCollect: true}})
							if err != nil {
								t.Fatal(err)
							}
							t.Cleanup(func() { in.Close() })
							return in
						}
						a, b, observer := makeInstance(), makeInstance(), makeInstance()
						if a.gc != b.gc || a.gc != observer.gc {
							t.Fatal("fixture did not share its collector domain")
						}
						state := a.managedFunctionCollector(a.gcInvocationDomain())
						ra, err := a.Invoke("make", 123)
						if err != nil {
							t.Fatal(err)
						}
						ta := ra[0]
						rb, err := b.Invoke("make", 456)
						if err != nil {
							t.Fatal(err)
						}
						tb := rb[0]
						rt.refStore.mu.Lock()
						oa, ob := rt.refStore.gcByToken[ta].ref, rt.refStore.gcByToken[tb].ref
						rt.refStore.mu.Unlock()
						domain := a.lockGCCollector()
						ea, err := state.newManagedFunctionEnv(b, gc.EmptyRoots{})
						if err != nil {
							t.Fatal(err)
						}
						eb, err := state.newManagedFunctionEnv(a, managedFunctionEnvRoots{gc.Root(ea)})
						if err != nil {
							t.Fatal(err)
						}
						state.shadows[oa] = managedFunctionEdge{ref: ea, count: 1}
						a.gc.WriteBarrierObject(oa, ea)
						state.shadows[ob] = managedFunctionEdge{ref: eb, count: 1}
						a.gc.WriteBarrierObject(ob, eb)
						ga, gb := a.gc.NewGlobalSlot(oa), a.gc.NewTableSlot(ob)
						if err = state.bindPersistentSlot(a, gc.SlotGlobal, ga); err != nil {
							t.Fatal(err)
						}
						if err = state.bindPersistentSlot(b, gc.SlotTable, gb); err != nil {
							t.Fatal(err)
						}
						unlockGCCollector(domain)
						exact := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Abstract: AbstractHeapExn}}}
						held, err := rt.refStore.issueGCRef(observer, oa, exact)
						if err != nil {
							t.Fatal(err)
						}
						if err = a.ReleaseGCRef(GCRef{token: ta}); err != nil {
							t.Fatal(err)
						}
						if err = b.ReleaseGCRef(GCRef{token: tb}); err != nil {
							t.Fatal(err)
						}
						var active chan error
						if concurrent {
							active = make(chan error, 1)
							go func() {
								for i := 0; i < 32; i++ {
									result, err := observer.Invoke("make", uint64(i))
									if err != nil {
										active <- err
										return
									}
									if len(result) != 1 {
										active <- fmt.Errorf("concurrent result %v", result)
										return
									}
									if err = observer.ReleaseGCRef(GCRef{token: result[0]}); err != nil {
										active <- err
										return
									}
								}
								active <- nil
							}()
						}
						a.Close()
						b.Close()
						if active != nil {
							if err := <-active; err != nil {
								t.Fatal(err)
							}
						}
						if err = observer.CollectGC(); err != nil {
							t.Fatal(err)
						}
						if !a.hasPhysicalResources() || !b.hasPhysicalResources() || len(state.envs) != 2 {
							t.Fatal("external GC token lost its closed producer graph")
						}
						if err = observer.ReleaseGCRef(GCRef{token: held}); err != nil {
							t.Fatal(err)
						}
						if closeOnly {
							err = observer.Close()
						} else {
							err = observer.CollectGC()
						}
						if err != nil {
							t.Fatal(err)
						}
						if a.hasPhysicalResources() || b.hasPhysicalResources() || len(state.envs) != 0 || len(state.shadows) != 0 || len(state.globalOwners) != 0 || len(state.tableOwners) != 0 {
							t.Fatalf("closed cycle retained resources or metadata: envs=%d shadows=%d globals=%d tables=%d", len(state.envs), len(state.shadows), len(state.globalOwners), len(state.tableOwners))
						}
						if state.graph.pendingReclaims.Load() != 0 {
							t.Fatal("public collection left a physical journal")
						}
					})
				}
			}

		}
	}
}

func TestRV64ManagedFunctionPrivateStoreUsesDomainAdmission(t *testing.T) {
	cfg := rv64I31Config()
	features := cfg.frontendFeatures()
	features.ExceptionHandling = true
	features.ExceptionReferences = true
	c, err := compileWithFrontendFeatures(cfg, ownedExceptionPublicModule(), features)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if in.refStore == nil || !in.refStore.private || in.executionFlags.Load()&executionFlagDynamicGCDomain == 0 {
		t.Fatal("private managed instance lost its dynamic domain admission")
	}
}
