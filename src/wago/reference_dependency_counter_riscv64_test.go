//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"testing"
)

func conditionalReleaseMappedCompile(t *testing.T, kind string, artifact bool) *Compiled {
	t.Helper()
	var c *Compiled
	if kind == "memory" {
		c = importedMemoryCompile(t, false, 0, false, false, 0)
	} else {
		c = importedGlobalCompile(t, false, 0, true)
	}
	if artifact {
		blob, err := c.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		c, err = LoadTrustedArtifact(blob)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close() })
	}
	return c
}

func TestRV64ConditionalCounterMappedRepairRetry(t *testing.T) {
	for _, kind := range []string{"memory", "global"} {
		for _, artifact := range []bool{false, true} {
			for _, anonymous := range []bool{false, true} {
				for _, fault := range []string{"missing", "foreign-owner"} {
					t.Run(fmt.Sprintf("%s-artifact%t-anonymous%t-%s", kind, artifact, anonymous, fault), func(t *testing.T) {
						producer, err := Instantiate(conditionalReleaseMappedCompile(t, kind, artifact))
						if err != nil {
							t.Fatal(err)
						}
						defer producer.Close()
						if kind == "memory" {
							importedMemoryInvoke(t, producer, "size", 1)
						} else {
							importedGlobalInvoke(t, producer, "get", []uint64{7, 9, 0x80000000, 0x8000000000000000, 33})
						}
						graph, err := newRV64FunctionDependencyGraph(1, 1)
						if err != nil {
							t.Fatal(err)
						}
						if anonymous {
							// Honest isolated node-capacity fallback, not a substitute
							// retainer or a reset of the live process registry.
							blocker := rv64FunctionDependencyUnitOwners(t, 1)[0]
							if _, err := rv64RegisteredFunctionDependencyMember(graph, blocker); err != nil {
								t.Fatal(err)
							}
						}
						var memory *Memory
						var global *Global
						var memories memoryImportAttachments
						var globals globalImportAttachments
						var cleanup resourceCleanup
						var drain func()
						var held func() bool
						if kind == "memory" {
							memory, err = producer.ExportedMemory("memory")
							if err == nil {
								err = rv64AcquireMemoryDependency(&memories, memory, graph, &cleanup)
							}
							drain = memories.detachAll
							held = func() bool {
								return railshotHasBuilderMemoryDependencies(&memories) && memories.dependencies.rows.rows.inline[0].state == rv64MemoryDependencyAttached && memories.dependencies.rows.rows.inline[0].root.producer == producer
							}
						} else {
							global, err = producer.ExportedGlobalObject("g0")
							if err == nil {
								err = rv64AcquireGlobalDependency(&globals, global, nil, nil, graph, &cleanup)
							}
							drain = globals.detachAll
							held = func() bool {
								return railshotHasBuilderGlobalDependencies(&globals) && globals.dependencies.rows.rows.inline[0].state == rv64GlobalDependencyAttached && globals.dependencies.rows.rows.inline[0].root.producer == producer
							}
						}
						cleanup.finish()
						if err != nil || producer.resourceRefs != 1 {
							t.Fatal("actual counted mapped acquisition", err)
						}
						foreign := &Instance{}
						// Alter only private counter metadata after actual acquisition.
						// No guest executes with these synthetic faults installed.
						faultCounter := func(broken bool) {
							if memory != nil {
								s := memory.state.Load()
								s.mu.Lock()
								if fault == "missing" {
									if broken {
										s.setImporterCount(0)
									} else {
										s.setImporterCount(1)
									}
								} else {
									if broken {
										s.owner = foreign
									} else {
										s.owner = producer
									}
								}
								s.mu.Unlock()
							} else {
								o := global.owner
								o.mu.Lock()
								if fault == "missing" {
									if broken {
										o.importers = 0
									} else {
										o.importers = 1
									}
								} else {
									if broken {
										o.instance = foreign
									} else {
										o.instance = producer
									}
								}
								o.mu.Unlock()
							}
						}
						callbacks, unlocked := 0, false
						marker := new(int)
						producer.referenceLifetime().afterPhysicalRelease(func() {
							callbacks++
							unlocked = nativeExecutionMu.TryLock()
							if unlocked {
								nativeExecutionMu.Unlock()
							}
							if memory != nil {
								s := memory.state.Load()
								if !s.mu.TryLock() {
									unlocked = false
								} else {
									s.mu.Unlock()
								}
							} else if !global.owner.mu.TryLock() {
								unlocked = false
							} else {
								global.owner.mu.Unlock()
							}
							panic(marker)
						})
						if err := producer.Close(); err != nil || producer.resourcesClosed {
							t.Fatal("logical Close lost real receipt", err)
						}
						faultCounter(true)
						needsRepair := true
						defer func() {
							if needsRepair {
								faultCounter(false)
							}
						}()
						value, panicked := cleanupTestPanic(drain)
						if !panicked || value == marker || callbacks != 0 || !held() || producer.resourceRefs != 1 || producer.resourcesClosed || producer.jm == nil || producer.ar == nil || producer.eng == nil || foreign.resourceRefs != 0 {
							t.Fatal("denied counter lost its concrete row or actual mapped root", value, panicked, callbacks)
						}
						if !anonymous && graph.live != 1 {
							t.Fatal("denied counter freed the real pending receipt")
						}
						faultCounter(false)
						needsRepair = false
						value, panicked = cleanupTestPanic(drain)
						if !panicked || value != marker || callbacks != 1 || !unlocked || held() || producer.resourceRefs != 0 || !producer.resourcesClosed || producer.jm != nil || producer.ar != nil || producer.eng != nil || producer.nativeContext != 0 || graph.live != 0 {
							t.Fatal("repaired claim failed actual teardown or replaced callback panic", value, panicked, callbacks)
						}
						drain()
						if callbacks != 1 {
							t.Fatal("retry duplicated physical callback")
						}
					})
				}
			}
		}
	}
}
