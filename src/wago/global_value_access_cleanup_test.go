//go:build linux && (amd64 || arm64 || (riscv64 && wago_rv64_experimental)) && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func globalValueAccessCompile(t *testing.T, imported, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_GLOBAL_VALUE_ACCESS_ARTIFACT_DIR"), os.Getenv("WAGO_GLOBAL_VALUE_ACCESS_ARTIFACT_MODE")
	path := filepath.Join(dir, fmt.Sprintf("global-value-access-import%t.wago", imported))
	if mode == "load" {
		if !artifact {
			t.Fatal("compiler-free runner selected source")
		}
		blob, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		c, err := LoadTrustedArtifact(blob)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close() })
		return c
	}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})))}
	function, body := uint32(0), []byte{0x41, 17, 0x0b}
	if imported {
		im := append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0)
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(im)))
		function, body = 1, []byte{0x10, 0, 0x0b}
	}
	sections = append(sections,
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(6, wasmtest.Vec(
			wasmtest.GlobalEntry(wasm.FuncRef, false, []byte{0xd2, 0, 0x0b}),
			wasmtest.GlobalEntry(wasm.FuncRef, true, []byte{0xd0, 0x70, 0x0b}),
			wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b}),
		)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, function), wasmtest.ExportEntry("reference", 3, 0), wasmtest.ExportEntry("counter", 3, 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	module := wasmtest.Module(sections...)
	c, err := Compile(nil, module)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if !artifact {
		return c
	}
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if mode == "prepare" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, blob, 0600); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := LoadTrustedArtifact(blob)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loaded.Close() })
	return loaded
}

func globalValueAccessInstantiate(t *testing.T, rt *Runtime, c *Compiled, imports *Imports) *Instance {
	t.Helper()
	m, err := rt.Module(c)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	in, err := rt.Instantiate(context.Background(), m, WithImports(imports))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = in.Close() })
	if in.jm == nil || in.ar == nil || in.eng == nil {
		t.Fatal("fixture lacks actual mappings")
	}
	got, err := in.Invoke("run")
	if err != nil || len(got) != 1 || got[0] != 17 {
		t.Fatal("actual native invocation", got, err)
	}
	return in
}

func globalValueAccessRuntime(t *testing.T) *Runtime {
	t.Helper()
	rt := NewRuntime()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := rt.CloseContext(ctx); err != nil {
			t.Error("Runtime cleanup", err)
		}
	})
	return rt
}

// The source/artifact deliberately has an unexported mutable cell. Only this
// private fixture adds an isolated export entry after instantiation, then uses
// genuine lazy owner attachment and the original mapped cell/physical teardown.
// It proves accessor ownership; it does not qualify new RV64 module admission.
func globalValueAccessOwnedCell(t *testing.T, in *Instance) *Global {
	t.Helper()
	view := *in.c
	view.GlobalExports = maps.Clone(view.GlobalExports)
	view.GlobalExports["mutable"] = 1
	in.c = &view
	g, err := in.ExportedGlobalObject("mutable")
	if err != nil {
		t.Fatal(err)
	}
	if g != in.globalCells[1] || g.owner.instance != in || g.owner.arena != nil {
		t.Fatal("fixture replaced actual instance cell")
	}
	return g
}

func globalValueAccessAssertLocks(g *Global, owner *Instance, rt *Runtime) {
	for _, mu := range []*sync.Mutex{&nativeExecutionMu, &g.owner.mu, &rt.refStore.mu} {
		if !mu.TryLock() {
			panic("global value callback under native/container/store lock")
		}
		mu.Unlock()
	}
	if owner != nil {
		if !owner.lifeMu.TryLock() {
			panic("global value callback under owner lifetime lock")
		}
		owner.lifeMu.Unlock()
	}
}

func TestGlobalValueAccessMappedCallbacks(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, entry := range []string{"host-global", "owned-global", "owned-instance"} {
			for _, panics := range []bool{false, true} {
				t.Run(fmt.Sprintf("artifact%t-%s-panic%t", artifact, entry, panics), func(t *testing.T) {
					rt := globalValueAccessRuntime(t)
					c := globalValueAccessCompile(t, false, artifact)
					producer := globalValueAccessInstantiate(t, rt, c, NewImports())
					reentry := globalValueAccessInstantiate(t, rt, c, NewImports())
					reentry.markNativeControlShared()
					var owner *Instance
					var g *Global
					if entry == "host-global" {
						var err error
						g, err = rt.NewFuncRefGlobal(NullFuncRef(), true)
						if err != nil {
							t.Fatal(err)
						}
						t.Cleanup(func() { _ = g.Close() })
					} else {
						owner = globalValueAccessInstantiate(t, rt, c, NewImports())
						g = globalValueAccessOwnedCell(t, owner)
					}
					descriptor, ok := producer.localFuncrefDescriptor(0)
					if !ok {
						t.Fatal("actual published descriptor")
					}
					g.owner.mu.Lock()
					writeGlobalObject(g, ValFuncRef, descriptor)
					g.owner.mu.Unlock()
					if !g.retainProducerInstance(producer) || producer.resourceRefs != 1 {
						t.Fatal("actual retained descriptor acquisition")
					}
					marker, callbacks := new(int), 0
					producer.referenceLifetime().afterPhysicalRelease(func() {
						callbacks++
						globalValueAccessAssertLocks(g, owner, rt)
						if len(g.owner.retained) != 0 || readGlobalObject(g, ValFuncRef) != 0 {
							panic("old root/cell still published")
						}
						got, err := reentry.Invoke("run")
						if err != nil || len(got) != 1 || got[0] != 17 {
							panic("native callback reentry failed")
						}
						if err := g.SetValue(ValueFuncRef(NullFuncRef())); err != nil {
							panic(err)
						}
						if panics {
							panic(marker)
						}
					})
					if err := producer.Close(); err != nil || producer.resourcesClosed {
						t.Fatal("logical retained Close", err)
					}
					var err error
					value, panicked := cleanupTestPanic(func() {
						if entry == "owned-instance" {
							err = owner.SetGlobalValue("mutable", ValueFuncRef(NullFuncRef()))
						} else {
							err = g.SetValue(ValueFuncRef(NullFuncRef()))
						}
					})
					if err != nil || panicked != panics || panics && value != marker || callbacks != 1 {
						t.Fatal("callback boundary", err, value, panicked, callbacks)
					}
					if producer.resourceRefs != 0 || !producer.resourcesClosed || producer.jm != nil || producer.ar != nil || producer.eng != nil || rt.activeOperations != 0 {
						t.Fatal("mapped resources or Runtime operation stranded")
					}
				})
			}
		}
	}
}

func TestGlobalValueAccessMappedOwnerCloseFirstPanic(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, instanceEntry := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-instance%t", artifact, instanceEntry), func(t *testing.T) {
				rt := globalValueAccessRuntime(t)
				c := globalValueAccessCompile(t, false, artifact)
				owner := globalValueAccessInstantiate(t, rt, c, NewImports())
				producer := globalValueAccessInstantiate(t, rt, c, NewImports())
				g := globalValueAccessOwnedCell(t, owner)
				descriptor, ok := producer.localFuncrefDescriptor(0)
				if !ok {
					t.Fatal("producer descriptor")
				}
				writeGlobalObject(g, ValFuncRef, descriptor)
				if !g.retainProducerInstance(producer) {
					t.Fatal("real global root")
				}
				first, later := new(int), new(int)
				counts := [2]int{}
				producer.referenceLifetime().afterPhysicalRelease(func() {
					counts[0]++
					globalValueAccessAssertLocks(g, owner, rt)
					if owner.invocationState.Load()&instanceInvocationCount != 1 || rt.activeOperations != 1 {
						panic("setter lifetime ended before root callback")
					}
					if err := owner.Close(); err != nil {
						panic(err)
					}
					if owner.resourcesClosed {
						panic("logical Close finalized protected setter")
					}
					panic(first)
				})
				owner.referenceLifetime().afterPhysicalRelease(func() {
					counts[1]++
					if !nativeExecutionMu.TryLock() {
						panic("owner finalizer under native mutex")
					}
					nativeExecutionMu.Unlock()
					if rt.activeOperations != 1 {
						panic("Runtime unit ended before owner finalizer")
					}
					panic(later)
				})
				if err := producer.Close(); err != nil {
					t.Fatal(err)
				}
				value, panicked := cleanupTestPanic(func() {
					var err error
					if instanceEntry {
						err = owner.SetGlobalValue("mutable", ValueFuncRef(NullFuncRef()))
					} else {
						err = g.SetValue(ValueFuncRef(NullFuncRef()))
					}
					if err != nil {
						panic(err)
					}
				})
				if !panicked || value != first || counts != [2]int{1, 1} || rt.activeOperations != 0 {
					t.Fatal("first-panic/operation completion", value, panicked, counts, rt.activeOperations)
				}
				for _, in := range []*Instance{owner, producer} {
					if !in.resourcesClosed || in.jm != nil || in.ar != nil || in.eng != nil {
						t.Fatal("actual native teardown incomplete")
					}
				}
			})
		}
	}
}

func TestGlobalValueAccessMappedBootstrapBalance(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := globalValueAccessRuntime(t)
			c := globalValueAccessCompile(t, false, artifact)
			owner := globalValueAccessInstantiate(t, rt, c, NewImports())
			producer := globalValueAccessInstantiate(t, rt, c, NewImports())
			g := globalValueAccessOwnedCell(t, owner)
			own, err := owner.GlobalValue("reference")
			if err != nil {
				t.Fatal(err)
			}
			foreign, err := producer.GlobalValue("reference")
			if err != nil {
				t.Fatal(err)
			}
			beforeOwn, beforeForeign := owner.resourceRefs, producer.resourceRefs
			if err := g.SetValue(own); err != nil || owner.resourceRefs != beforeOwn || len(g.owner.retained) != 0 {
				t.Fatal("own-owner bootstrap lost or leaked", err)
			}
			if err := g.SetValue(foreign); err != nil || producer.resourceRefs != beforeForeign+1 || len(g.owner.retained) != 1 {
				t.Fatal("new-row bootstrap transfer", err)
			}
			if allocations := testing.AllocsPerRun(100, func() {
				if err := g.SetValue(foreign); err != nil {
					panic(err)
				}
			}); allocations != 0 || producer.resourceRefs != beforeForeign+1 {
				t.Fatal("duplicate bootstrap/warm allocation", allocations, producer.resourceRefs)
			}
			// The real initial metadata snapshot accepts this; its actual commit guard
			// rejects after store-authorized bootstrap retain. Only slice length is
			// faulted; backing storage and real acquire/release remain unchanged.
			var count, refs int
			var root *Instance
			value, panicked := cleanupTestPanic(func() {
				if err := owner.beginInvocation(); err != nil {
					panic(err)
				}
				mu := owner.acquireInstanceNativeStateForHostAccess()
				var roots globalValueRoots
				var cleanup resourceCleanup
				original := g.cell
				defer func() {
					g.cell = original
					finishGlobalValueAccess(mu, owner, &roots, &cleanup)
				}()
				g.cell = original[:0]
				cleanup.run(func() { err = g.setValueNoLease(foreign, &roots) })
				count, root, refs = roots.count, roots.inline[0], producer.resourceRefs
			})
			if panicked {
				t.Fatal("late rejection drain", value)
			}
			if err == nil || count != 1 || root != producer || refs != beforeForeign+2 || producer.resourceRefs != beforeForeign+1 {
				t.Fatal("rejected exact bootstrap", err, count, root, refs, producer.resourceRefs)
			}
			if err := g.SetValue(ValueFuncRef(NullFuncRef())); err != nil || producer.resourceRefs != beforeForeign {
				t.Fatal("null cleanup", err)
			}
			for _, operation := range []func() error{func() error { return g.SetValue(ValueFuncRef(NullFuncRef())) }, func() error { return owner.SetGlobalValue("mutable", ValueFuncRef(NullFuncRef())) }, func() error { return owner.SetGlobalValue("counter", ValueI32(9)) }} {
				if allocations := testing.AllocsPerRun(100, func() {
					if err := operation(); err != nil {
						panic(err)
					}
				}); allocations != 0 {
					t.Fatal("no-delta setter allocation", allocations)
				}
			}
		})
	}
}

func TestGlobalValueAccessMappedManyOwners(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			if runtime.GOARCH == "riscv64" {
				globalValueAccessMappedBatch(t, artifact)
				return
			}
			rt := globalValueAccessRuntime(t)
			c := globalValueAccessCompile(t, false, artifact)
			producer := globalValueAccessInstantiate(t, rt, c, NewImports())
			export, err := producer.ExportedFunc("run")
			if err != nil {
				t.Fatal(err)
			}
			wc := globalValueAccessCompile(t, true, artifact)
			owners := []*Instance{producer}
			for range 4 {
				owners = append(owners, globalValueAccessInstantiate(t, rt, wc, NewImports().Function("env", "target", export)))
			}
			g, err := rt.NewFuncRefGlobal(NullFuncRef(), true)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = g.Close() })
			descriptor, ok := producer.localFuncrefDescriptor(0)
			if !ok {
				t.Fatal("actual canonical descriptor")
			}
			writeGlobalObject(g, ValFuncRef, descriptor)
			for _, owner := range owners {
				if !owner.reachesFuncrefDescriptor(descriptor) || !g.retainProducerInstance(owner) {
					t.Fatal("genuine canonical owner reachability/root")
				}
			}
			if len(g.owner.retained) != 5 {
				t.Fatal("fixture lacks five distinct actual rows")
			}
			var first any
			counts := [5]int{}
			for i, in := range owners {
				index, marker := i, new(int)
				in.referenceLifetime().afterPhysicalRelease(func() {
					counts[index]++
					globalValueAccessAssertLocks(g, nil, rt)
					if len(g.owner.retained) != 0 || readGlobalObject(g, ValFuncRef) != 0 {
						panic("old map still published")
					}
					if first == nil {
						first = marker
					}
					panic(marker)
				})
			}
			for _, owner := range owners {
				if err := owner.Close(); err != nil {
					t.Fatal(err)
				}
			}
			value, panicked := cleanupTestPanic(func() {
				if err := g.SetValue(ValueFuncRef(NullFuncRef())); err != nil {
					panic(err)
				}
			})
			if !panicked || value != first || counts != [5]int{1, 1, 1, 1, 1} {
				t.Fatal("inline/overflow root drain", value, first, panicked, counts)
			}
			for _, owner := range owners {
				if owner.resourceRefs != 0 || !owner.resourcesClosed || owner.jm != nil || owner.ar != nil || owner.eng != nil {
					t.Fatal("physical root stranded")
				}
			}
		})
	}
}

func TestGlobalValueAccessMappedBlockedClose(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, entry := range []string{"host-global", "owned-global", "owned-instance"} {
			for _, panics := range []bool{false, true} {
				t.Run(fmt.Sprintf("artifact%t-%s-panic%t", artifact, entry, panics), func(t *testing.T) {
					rt := globalValueAccessRuntime(t)
					c := globalValueAccessCompile(t, false, artifact)
					producer := globalValueAccessInstantiate(t, rt, c, NewImports())
					var owner *Instance
					var g *Global
					if entry == "host-global" {
						var err error
						g, err = rt.NewFuncRefGlobal(NullFuncRef(), true)
						if err != nil {
							t.Fatal(err)
						}
						t.Cleanup(func() { _ = g.Close() })
					} else {
						owner = globalValueAccessInstantiate(t, rt, c, NewImports())
						g = globalValueAccessOwnedCell(t, owner)
					}
					descriptor, ok := producer.localFuncrefDescriptor(0)
					if !ok {
						t.Fatal("actual descriptor")
					}
					writeGlobalObject(g, ValFuncRef, descriptor)
					if !g.retainProducerInstance(producer) {
						t.Fatal("real retained root")
					}
					entered, release := make(chan struct{}), make(chan struct{})
					var once sync.Once
					unblock := func() { once.Do(func() { close(release) }) }
					var workerDone chan struct{}
					defer func() {
						unblock()
						if workerDone != nil {
							select {
							case <-workerDone:
							case <-time.After(3 * time.Second):
								t.Error("setter worker did not join")
							}
						}
					}()
					marker, callbacks := new(int), 0
					producer.referenceLifetime().afterPhysicalRelease(func() {
						callbacks++
						globalValueAccessAssertLocks(g, owner, rt)
						close(entered)
						<-release
						if panics {
							panic(marker)
						}
					})
					if err := producer.Close(); err != nil {
						t.Fatal(err)
					}
					type result struct {
						err      error
						value    any
						panicked bool
					}
					done := make(chan result, 1)
					workerDone = make(chan struct{})
					go func() {
						defer close(workerDone)
						var err error
						value, panicked := cleanupTestPanic(func() {
							if entry == "owned-instance" {
								err = owner.SetGlobalValue("mutable", ValueFuncRef(NullFuncRef()))
							} else {
								err = g.SetValue(ValueFuncRef(NullFuncRef()))
							}
						})
						done <- result{err, value, panicked}
					}()
					select {
					case <-entered:
					case <-time.After(3 * time.Second):
						t.Fatal("physical callback did not enter")
					}
					if owner != nil {
						if err := owner.Close(); err != nil {
							t.Fatal(err)
						}
						if owner.resourcesClosed || owner.jm == nil || owner.ar == nil || owner.eng == nil || owner.invocationState.Load() != instanceInvocationClosed+1 {
							t.Fatal("concurrent logical Close lost protecting invocation")
						}
					} else {
						if err := g.Close(); err != nil {
							t.Fatal(err)
						}
						if !g.owner.closed || g.cell != nil || g.owner.arena != nil {
							t.Fatal("host Close did not claim actual arena")
						}
					}
					want := uint64(0)
					if owner != nil {
						want = 1
					}
					rt.mu.Lock()
					active := rt.activeOperations
					rt.mu.Unlock()
					if active != want {
						t.Fatal("operation lifetime during root callback", active, want)
					}
					unblock()
					var got result
					select {
					case got = <-done:
					case <-time.After(3 * time.Second):
						t.Fatal("setter did not finish")
					}
					if got.err != nil || got.panicked != panics || panics && got.value != marker || callbacks != 1 {
						t.Fatal("blocked cleanup", got, callbacks)
					}
					if rt.activeOperations != 0 {
						t.Fatal("Runtime operation stranded")
					}
					for _, in := range []*Instance{owner, producer} {
						if in != nil && (!in.resourcesClosed || in.jm != nil || in.ar != nil || in.eng != nil) {
							t.Fatal("physical resources stranded")
						}
					}
				})
			}
		}
	}
}

// RV native imports own distinct thunk descriptors, so they cannot manufacture
// the existing-target canonical Global alias fixture. This private boundary
// witness owns six real root units on five mapped producers, without fake map
// rows, and exercises the same unlock/inline/overflow physical cleanup journal.
func globalValueAccessMappedBatch(t *testing.T, artifact bool) {
	rt := globalValueAccessRuntime(t)
	c := globalValueAccessCompile(t, false, artifact)
	g, err := rt.NewFuncRefGlobal(NullFuncRef(), true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.Close() })
	var owners [5]*Instance
	var roots globalValueRoots
	nativeHeld := false
	defer func() {
		var cleanup resourceCleanup
		if nativeHeld {
			nativeHeld = false
			cleanup.run(nativeExecutionMu.Unlock)
		}
		cleanup.run(roots.release)
		if cleanup.panicked {
			t.Error("fixture failure drain", cleanup.value)
		}
	}()
	for i := range owners {
		owners[i] = globalValueAccessInstantiate(t, rt, c, NewImports())
		if !owners[i].retainResourceRoot() {
			t.Fatal("real mapped root")
		}
		roots.record(owners[i])
	}
	if !owners[0].retainResourceRoot() {
		t.Fatal("independent duplicate root")
	}
	roots.record(owners[0])
	if roots.count != 6 || owners[0].resourceRefs != 2 {
		t.Fatal("exact six acquisitions")
	}
	var first any
	counts := [5]int{}
	for i, in := range owners {
		index, marker := i, new(int)
		in.referenceLifetime().afterPhysicalRelease(func() {
			counts[index]++
			globalValueAccessAssertLocks(g, nil, rt)
			if first == nil {
				first = marker
			}
			panic(marker)
		})
	}
	nativeExecutionMu.Lock()
	nativeHeld = true
	var cleanup resourceCleanup
	cleanup.run(func() {
		if err := g.setValueNoLease(ValueFuncRef(NullFuncRef()), &roots); err != nil {
			panic(err)
		}
	})
	for _, owner := range owners {
		cleanup.run(func() {
			if err := owner.Close(); err != nil {
				panic(err)
			}
		})
	}
	nativeHeld = false // finishGlobalValueAccess now owns this native unlock.
	value, panicked := cleanupTestPanic(func() { finishGlobalValueAccess(&nativeExecutionMu, nil, &roots, &cleanup) })
	if !panicked || value != first || counts != [5]int{1, 1, 1, 1, 1} {
		t.Fatal("real overflow journal", value, panicked, first, counts)
	}
	for _, owner := range owners {
		if owner.resourceRefs != 0 || !owner.resourcesClosed || owner.jm != nil || owner.ar != nil || owner.eng != nil {
			t.Fatal("actual physical unit stranded")
		}
	}
	for _, root := range roots.inline {
		if root != nil {
			t.Fatal("inline unit still owned")
		}
	}
	for _, root := range roots.extra {
		if root != nil {
			t.Fatal("overflow unit still owned")
		}
	}
	t.Log("private RV boundary: six real acquisition units, five actual mapped callbacks")
}
