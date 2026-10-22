//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func frozenDependencyCompile(t *testing.T, imported, null, artifact bool) *Compiled {
	t.Helper()
	return rv64NativeTailCompile(t, fmt.Sprintf("frozen-dep-import%t-null%t", imported, null), rv64ReferenceNoTableModule(false, imported, null), artifact)
}

func frozenDependencyInstantiate(t *testing.T, rt *Runtime, c *Compiled, imports *Imports, direct bool, after func(*Instance) error) (*Instance, error) {
	t.Helper()
	if direct {
		return Instantiate(c, InstantiateOptions{Imports: imports, store: rt.refStore, afterCreate: after})
	}
	m, err := rt.Module(c)
	if err != nil {
		return nil, err
	}
	defer m.Close()
	if after != nil {
		rt.hooks.afterCreate = append(rt.hooks.afterCreate, func(event InstantiationEvent) error { return after(event.Instance.value) })
		defer func() { rt.hooks.afterCreate = rt.hooks.afterCreate[:len(rt.hooks.afterCreate)-1] }()
	}
	return rt.Instantiate(context.Background(), m, WithImports(imports))
}

func TestRV64PublicFrozenGlobalDependenciesClosedHostOwner(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, direct := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-direct%t", artifact, direct), func(t *testing.T) {
				rt := NewRuntime()
				defer rt.Close()
				producer := rv64NativeTokenInstantiate(t, rt, frozenDependencyCompile(t, false, false, artifact), NewImports())
				defer producer.Close()
				value, err := producer.GlobalValue("reference")
				if err != nil {
					t.Fatal(err)
				}
				global, err := rt.NewFuncRefGlobal(value.FuncRef(), false)
				if err != nil {
					t.Fatal(err)
				}
				defer global.Close()
				if rv64ExistingFunctionDependencyOwnership(producer) != nil {
					t.Fatal("producer member registered before logical Close")
				}
				if err := producer.Close(); err != nil || producer.resourcesClosed {
					t.Fatal("host global did not retain actual descriptor storage", err)
				}
				before := producer.resourceRefs
				consumer, err := frozenDependencyInstantiate(t, rt, frozenDependencyCompile(t, true, false, artifact), NewImports().Global("env", "ref", global), direct, nil)
				if err != nil {
					t.Fatal("first registration after actual logical Close", err)
				}
				defer consumer.Close()
				rows := rv64ExistingFrozenGlobalDependencyRows(consumer)
				if rows == nil || rows.rows.n != 1 || producer.resourceRefs != before+1 || global.owner.importers != 1 || rows.rows.inline[0].root.producer != producer {
					t.Fatal("exact closed-B frozen acquisition")
				}
				frozenDependencyTestState(t, rows.rows.inline[0], dependencyTyped, consumer)
				if rv64ExistingFunctionDependencyOwnership(consumer).member.outgoing.Load() != 1 || !producer.nativeControlIsShared() {
					t.Fatal("host cell invented a storage edge or missed quiescence")
				}
				importedGlobalInvoke(t, consumer, "carrier", []uint64{0})
				if err := consumer.Close(); err != nil || !rows.complete() || producer.resourceRefs != before || global.owner.importers != 0 {
					t.Fatal("closed B frozen receipt did not drain", err)
				}
				rv64CheckGlobalDependencyRetirement(t, consumer)
				if err := global.Close(); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				if err := rt.CloseContext(ctx); err != nil {
					t.Fatal(err)
				}
				rv64CheckGlobalDependencyRetirement(t, producer)
				if producer.jm != nil || producer.ar != nil || producer.eng != nil || producer.resourceRefs != 0 {
					t.Fatal("closed B actual storage stranded")
				}
			})
		}
	}
}

func TestRV64PublicFrozenGlobalDependenciesNull(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, direct := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-direct%t", artifact, direct), func(t *testing.T) {
				rt := NewRuntime()
				defer rt.Close()
				global, err := rt.NewFuncRefGlobal(NullFuncRef(), false)
				if err != nil {
					t.Fatal(err)
				}
				defer global.Close()
				consumer, err := frozenDependencyInstantiate(t, rt, frozenDependencyCompile(t, true, false, artifact), NewImports().Global("env", "ref", global), direct, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer consumer.Close()
				if len(consumer.importGlobalReferences()) != 1 || railshotHasFrozenGlobalDependencies(consumer.importGlobalReferences()) || rv64ExistingFrozenGlobalDependencyRows(consumer) != nil || rv64ExistingFunctionDependencyOwnership(consumer).member.graph.Load() != nil {
					t.Fatal("null-only descriptor manufactured a graph acquisition")
				}
				importedGlobalInvoke(t, consumer, "carrier", []uint64{1})
				entries := consumer.importGlobalReferences()
				if err := consumer.Close(); err != nil || global.owner.importers != 0 || entries[0] != (importGlobalReference{}) {
					t.Fatal("null-only metadata/count cleanup", err)
				}
			})
		}
	}
}

func TestRV64PublicFrozenGlobalDependenciesBlockedCallback(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, callbackPanic := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-panic%t", artifact, callbackPanic), func(t *testing.T) {
				rt := NewRuntime()
				defer rt.Close()
				pc := frozenDependencyCompile(t, false, false, artifact)
				producer := rv64NativeTokenInstantiate(t, rt, pc, NewImports())
				defer producer.Close()
				reentry := rv64NativeTokenInstantiate(t, rt, pc, NewImports())
				defer reentry.Close()
				global, err := producer.ExportedGlobalObject("reference")
				if err != nil {
					t.Fatal(err)
				}
				consumer := rv64NativeTokenInstantiate(t, rt, frozenDependencyCompile(t, true, false, artifact), NewImports().Global("env", "ref", global))
				defer consumer.Close()
				rows, ownership := rv64ExistingFrozenGlobalDependencyRows(consumer), rv64ExistingFunctionDependencyOwnership(consumer)
				row := rows.rows.inline[0]
				receipt := row.root.receipt
				if err := producer.Close(); err != nil {
					t.Fatal(err)
				}
				entered, resume, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
				resumed, joined := false, false
				defer func() {
					if !resumed {
						close(resume)
					}
					if !joined {
						select {
						case <-done:
						case <-time.After(3 * time.Second):
							t.Error("physical callback cleanup join timed out")
						}
					}
				}()
				marker := new(int)
				unlocked, reentered := false, false
				producer.referenceLifetime().afterPhysicalRelease(func() {
					unlocked = nativeExecutionMu.TryLock()
					if unlocked {
						nativeExecutionMu.Unlock()
					}
					for _, mu := range []*Instance{producer, consumer} {
						if !mu.lifeMu.TryLock() {
							unlocked = false
						} else {
							mu.lifeMu.Unlock()
						}
					}
					if !receipt.graph.mu.TryLock() {
						unlocked = false
					} else {
						receipt.graph.mu.Unlock()
					}
					if !global.owner.mu.TryLock() {
						unlocked = false
					} else {
						global.owner.mu.Unlock()
					}
					if !rt.refStore.mu.TryLock() {
						unlocked = false
					} else {
						rt.refStore.mu.Unlock()
					}
					if !rows.mu.TryLock() {
						unlocked = false
					} else {
						rows.mu.Unlock()
					}
					if unlocked {
						result, err := reentry.Invoke("identity", 17)
						reentered = err == nil && slices.Equal(result, []uint64{17})
					}
					close(entered)
					<-resume
					if callbackPanic {
						panic(marker)
					}
				})
				go func() { done <- consumer.Close() }()
				select {
				case <-entered:
				case <-time.After(3 * time.Second):
					t.Fatal("actual descriptor callback did not begin")
				}
				if !unlocked || !reentered || !consumer.resourcesClosed || consumer.jm == nil || ownership.cleanupCompleted.Load() || !ownership.member.active.Load() || ownership.member.outgoing.Load() != 1 || rows.complete() || row.state != rv64FrozenGlobalDependencyReleasing {
					t.Fatal("consumer retired before actual descriptor callback completion")
				}
				for _, entry := range rows.entries {
					if entry != (importGlobalReference{}) {
						t.Fatal("frozen aliases visible in actual callback")
					}
				}
				receipt.graph.mu.Lock()
				record, _ := receipt.graph.recordLocked(receipt.identity)
				releasing := record != nil && record.state == dependencyReleasing
				receipt.graph.mu.Unlock()
				if !releasing {
					t.Fatal("callback lost its non-adoptable graph claim")
				}
				close(resume)
				resumed = true
				select {
				case err = <-done:
					joined = true
				case <-time.After(3 * time.Second):
					t.Fatal("physical callback release join")
				}
				if callbackPanic && !errors.Is(err, ErrCallbackPanic) || !callbackPanic && err != nil || !rows.complete() || global.owner.importers != 0 {
					t.Fatal("terminal descriptor drain outcome", err)
				}
				for _, in := range []*Instance{producer, consumer} {
					rv64CheckGlobalDependencyRetirement(t, in)
					if in.jm != nil || in.ar != nil || in.eng != nil {
						t.Fatal("actual descriptor teardown stranded storage")
					}
				}
			})
		}
	}
}

func TestRV64PublicFrozenGlobalDependenciesDeniedTerminalClaim(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			producer := rv64NativeTokenInstantiate(t, rt, frozenDependencyCompile(t, false, false, artifact), NewImports())
			defer producer.Close()
			global, err := producer.ExportedGlobalObject("reference")
			if err != nil {
				t.Fatal(err)
			}
			consumer := rv64NativeTokenInstantiate(t, rt, frozenDependencyCompile(t, true, false, artifact), NewImports().Global("env", "ref", global))
			defer consumer.Close()
			rows, ownership := rv64ExistingFrozenGlobalDependencyRows(consumer), rv64ExistingFunctionDependencyOwnership(consumer)
			row, root := rows.rows.inline[0], rows.rows.inline[0].root
			g := root.receipt.graph
			g.mu.Lock()
			record, _ := g.recordLocked(root.receipt.identity)
			record.state = dependencyReserved
			g.mu.Unlock() // explicit private fault
			defer func() {
				g.mu.Lock()
				record, _ := g.recordLocked(root.receipt.identity)
				if record != nil && record.state == dependencyReserved {
					record.state = dependencyTyped
				}
				g.mu.Unlock()
				_, panicked := cleanupTestPanic(func() { railshotReleaseInstanceGlobalReferences(consumer) })
				if panicked {
					t.Error("repaired deferred frozen cleanup panicked")
				}
				railshotFunctionDependencyCleanupCompleted(consumer)
			}()
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := consumer.Close(); !errors.Is(err, ErrCallbackPanic) {
				t.Fatal("denied terminal claim outcome", err)
			}
			if row.state != rv64FrozenGlobalDependencyAttached || row.root != root || producer.resourceRefs != 1 || producer.resourcesClosed || rows.entries[0].dependency != row || len(consumer.importGlobalReferences()) != 0 || ownership.cleanupCompleted.Load() || !ownership.member.active.Load() || ownership.member.outgoing.Load() != 1 {
				t.Fatal("denied claim lost authoritative obligation or retired consumer")
			}
			g.mu.Lock()
			record, _ = g.recordLocked(root.receipt.identity)
			record.state = dependencyTyped
			g.mu.Unlock()
			railshotReleaseInstanceGlobalReferences(consumer)
			railshotFunctionDependencyCleanupCompleted(consumer)
			for _, in := range []*Instance{producer, consumer} {
				rv64CheckGlobalDependencyRetirement(t, in)
			}
			if !rows.complete() || !producer.resourcesClosed || producer.jm != nil {
				t.Fatal("repaired holder retry stranded B storage")
			}
		})
	}
}

func TestRV64PublicFrozenGlobalDependenciesObserverRollback(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, observerPanic := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-panic%t", artifact, observerPanic), func(t *testing.T) {
				rt := NewRuntime()
				defer rt.Close()
				producer := rv64NativeTokenInstantiate(t, rt, frozenDependencyCompile(t, false, false, artifact), NewImports())
				defer producer.Close()
				global, err := producer.ExportedGlobalObject("reference")
				if err != nil {
					t.Fatal(err)
				}
				primary, raw := errors.New("frozen terminal observer failure"), new(int)
				var consumer *Instance
				callbacks := 0
				result, err := frozenDependencyInstantiate(t, rt, frozenDependencyCompile(t, true, false, artifact), NewImports().Global("env", "ref", global), false, func(in *Instance) error {
					consumer = in
					frozenDependencyTestState(t, rv64ExistingFrozenGlobalDependencyRows(in).rows.inline[0], dependencyTyped, in)
					producer.referenceLifetime().afterPhysicalRelease(func() { callbacks++; panic(raw) })
					consumer.referenceLifetime().afterPhysicalRelease(func() { callbacks++; panic(new(int)) })
					if err := producer.Close(); err != nil {
						return err
					}
					if observerPanic {
						panic(primary)
					}
					return primary
				})
				if result != nil || consumer == nil || !errors.Is(err, primary) || !errors.Is(err, ErrCallbackPanic) || callbacks != 2 || global.owner.importers != 0 || !rv64ExistingFrozenGlobalDependencyRows(consumer).complete() {
					t.Fatal("frozen observer primary/cleanup ownership", err, callbacks)
				}
				for _, in := range []*Instance{producer, consumer} {
					rv64CheckGlobalDependencyRetirement(t, in)
				}
			})
		}
	}
}

func TestRV64PublicFrozenGlobalDependenciesPartialFreezeRollback(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			pc := frozenDependencyCompile(t, false, false, artifact)
			owners := []*Instance{rv64NativeTokenInstantiate(t, rt, pc, NewImports()), rv64NativeTokenInstantiate(t, rt, pc, NewImports())}
			var globals []*resolvedGlobalImport
			for _, owner := range owners {
				defer owner.Close()
				g, err := owner.ExportedGlobalObject("reference")
				if err != nil {
					t.Fatal(err)
				}
				if err := g.attachReferenceImporter(rt.refStore); err != nil {
					t.Fatal(err)
				}
				defer g.detachReferenceImporter()
				globals = append(globals, &resolvedGlobalImport{global: g})
			}
			globals = append(globals, &resolvedGlobalImport{global: &Global{Type: ValFuncRef}})
			var cleanup resourceCleanup
			entries, err := railshotFreezeGlobalReferences(&Compiled{GlobalImports: []GlobalImportDef{{Type: ValFuncRef}, {Type: ValFuncRef}, {Type: ValFuncRef}}}, globals, rt.refStore, &cleanup)
			cleanup.finish()
			if err == nil || entries != nil {
				t.Fatal("partial freeze did not reject missing owner")
			}
			for _, owner := range owners {
				if owner.resourceRefs != 1 {
					t.Fatal("partial freeze retained a Pending/root acquisition")
				}
			}
		})
	}
}

// A real table/memory importer can transfer its counted attachments and run
// another owner's physical callback while a frozen Pending unit still pins it.
func frozenPrefinalizerSource(t *testing.T) []byte {
	t.Helper()
	globals := wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.FuncRef, false, []byte{0xd2, 0, 0x0b})))
	exports := wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("safe", 0, 0), wasmtest.ExportEntry("reference", 3, 0)))
	return rv64ReplaceTableStageSection(t, prefinalizerTableMemorySource(true), 7, append(globals, exports...))
}

func TestRV64PublicFrozenGlobalDependenciesBootstrapPrefinalizerPanic(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			ac := rv64NativeTailCompile(t, "frozen-dep-prefinalizer-provider", prefinalizerTableMemorySource(false), artifact)
			a := rv64NativeTokenInstantiate(t, rt, ac, NewImports())
			defer a.Close()
			memory, err := a.ExportedMemory("memory")
			if err != nil {
				t.Fatal(err)
			}
			table, err := a.ExportedTable("table")
			if err != nil {
				t.Fatal(err)
			}
			bc := rv64NativeTailCompile(t, "frozen-dep-prefinalizer-consumer", frozenPrefinalizerSource(t), artifact)
			b := rv64NativeTokenInstantiate(t, rt, bc, NewImports().Memory("env", "memory", memory).Table("env", "table", table))
			defer b.Close()
			value, err := b.GlobalValue("reference")
			if err != nil {
				t.Fatal(err)
			}
			global, err := rt.NewFuncRefGlobal(value.FuncRef(), false)
			if err != nil {
				t.Fatal(err)
			}
			defer global.Close()
			if err := global.attachReferenceImporter(rt.refStore); err != nil {
				t.Fatal(err)
			}
			attached := true
			defer func() {
				if attached {
					global.detachReferenceImporter()
				}
			}()
			if err := a.Close(); err != nil || a.resourcesClosed {
				t.Fatal("actual imported owner retention", err)
			}
			// Delay real Close's scans until the bootstrap exchange. This private
			// staging flag changes timing, not any real root/graph/decrement action.
			b.beginConstruction(nil)
			construction := true
			defer func() {
				if construction {
					b.endConstruction()
				}
			}()
			if err := b.Close(); err != nil {
				t.Fatal(err)
			}
			b.lifeMu.Lock()
			b.constructionActive = false
			b.lifeMu.Unlock()
			construction = false
			first := new(int)
			callbacks := 0
			a.referenceLifetime().afterPhysicalRelease(func() { callbacks++; panic(first) })
			before := b.resourceRefs
			var cleanup resourceCleanup
			entries, freezeErr := railshotFreezeGlobalReferences(&Compiled{GlobalImports: []GlobalImportDef{{Type: ValFuncRef}}}, []*resolvedGlobalImport{{global: global}}, rt.refStore, &cleanup)
			released := false
			defer func() {
				if !released {
					railshotReleaseGlobalReferences(entries)
				}
			}()
			valuePanic, panicked := cleanupTestPanic(cleanup.finish)
			if freezeErr != nil || len(entries) != 1 || entries[0].dependency == nil || panicked || valuePanic != nil || callbacks != 1 || !a.resourcesClosed || b.resourceRefs != before || b.resourcesClosed {
				t.Fatal("terminal journal prevented a complete frozen bootstrap exchange", freezeErr, valuePanic, panicked, callbacks, before, b.resourceRefs)
			}
			if err := b.waitTerminalClose(); !errors.Is(err, ErrCallbackPanic) {
				t.Fatal("pre-finalizer callback failure missing from terminal journal", err)
			}
			bm := &rv64ExistingFunctionDependencyOwnership(b).member
			g := bm.graph.Load()
			g.mu.Lock()
			receipts := bm.receipts
			g.mu.Unlock()
			if receipts != 1 || a.jm != nil || a.ar != nil || a.eng != nil {
				t.Fatal("successful bootstrap exchange lost its exact pending ownership")
			}
			railshotReleaseGlobalReferences(entries)
			released = true
			g.mu.Lock()
			receipts = bm.receipts
			g.mu.Unlock()
			if receipts != 0 || b.resourcesClosed || b.resourceRefs != 1 {
				t.Fatal("released bootstrap exchange retained actual ownership", receipts, b.resourcesClosed, b.resourceRefs, b.jm != nil, b.ar != nil, b.eng != nil)
			}
			global.detachReferenceImporter()
			attached = false
			if err := global.Close(); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := rt.CloseContext(ctx); err != nil && !errors.Is(err, ErrCallbackPanic) {
				t.Fatal(err)
			}
			if !b.resourcesClosed || b.resourceRefs != 0 || b.jm != nil || b.ar != nil || b.eng != nil {
				t.Fatal("closed public global retained actual bootstrap owner", b.resourcesClosed, b.resourceRefs, b.jm != nil, b.ar != nil, b.eng != nil)
			}
		})
	}
}

func TestRV64PublicFrozenGlobalDependenciesMultipleOwnersRecursivePanic(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, recursive := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-recursive%t", artifact, recursive), func(t *testing.T) {
				rt := NewRuntime()
				defer rt.Close()
				pc := frozenDependencyCompile(t, false, false, artifact)
				owners := []*Instance{rv64NativeTokenInstantiate(t, rt, pc, NewImports()), rv64NativeTokenInstantiate(t, rt, pc, NewImports())}
				imports := NewImports()
				for index, owner := range owners {
					defer owner.Close()
					g, err := owner.ExportedGlobalObject("reference")
					if err != nil {
						t.Fatal(err)
					}
					imports.Global("env", fmt.Sprint(index), g)
				}
				source := rv64ReferenceNoTableModule(false, true, false)
				source = rv64ReplaceTableStageSection(t, source, 2, wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "0", wasm.FuncRef, false), wasmtest.GlobalImportEntry("env", "1", wasm.FuncRef, false), wasmtest.GlobalImportEntry("env", "0", wasm.FuncRef, false))))
				var globals [][]byte
				for index := uint32(3); index < 256; index++ {
					globals = append(globals, wasmtest.GlobalEntry(wasm.FuncRef, false, append(append([]byte{0x23}, wasmtest.ULEB(index-1)...), 0x0b)))
				}
				source = rv64ReplaceTableStageSection(t, source, 6, wasmtest.Section(6, wasmtest.Vec(globals...)))
				cc := rv64NativeTailCompile(t, "frozen-dep-multiple-owners", source, artifact)
				consumer := rv64NativeTokenInstantiate(t, rt, cc, imports)
				defer consumer.Close()
				rows := rv64ExistingFrozenGlobalDependencyRows(consumer)
				if rows == nil || rows.rows.n != 2 || len(rows.entries) != 2 || rv64ExistingFunctionDependencyOwnership(consumer).member.outgoing.Load() != 4 {
					t.Fatal("two counted cells/two exact frozen owners")
				}
				for _, owner := range owners {
					if err := owner.Close(); err != nil {
						t.Fatal(err)
					}
				}
				first, later := new(int), new(int)
				var order []int
				for index, owner := range owners {
					owner.referenceLifetime().afterPhysicalRelease(func() {
						order = append(order, index)
						for _, entry := range rows.entries {
							if entry.owner == owner {
								panic("current frozen alias survived actual callback")
							}
						}
						if index == 0 {
							if recursive {
								railshotReleaseInstanceGlobalReferences(consumer)
							}
							panic(first)
						}
						if !recursive {
							panic(later)
						}
					})
				}
				// Delay actual terminal finalization so this call owns the terminal
				// journal. No early frozen pruning: normal releaseResources drains the
				// counted cells before these final descriptor acquisitions.
				consumer.beginConstruction(nil)
				construction := true
				defer func() {
					if construction {
						_, panicked := cleanupTestPanic(consumer.endConstruction)
						if panicked {
							t.Error("deferred terminal finalization panicked")
						}
					}
				}()
				if err := consumer.Close(); err != nil {
					t.Fatal(err)
				}
				consumer.lifeMu.Lock()
				consumer.constructionActive = false
				consumer.lifeMu.Unlock()
				construction = false
				value, panicked := cleanupTestPanic(consumer.tryFinalize)
				terminalErr := consumer.waitTerminalClose()
				if panicked || value != nil || !errors.Is(terminalErr, ErrCallbackPanic) || !rows.complete() {
					t.Fatal("terminal frozen journal did not contain callback failure", value, panicked, terminalErr)
				}
				if !slices.Equal(order, []int{0, 1}) {
					t.Fatal("actual callback order", order)
				}
				for _, in := range append(owners, consumer) {
					rv64CheckGlobalDependencyRetirement(t, in)
				}
			})
		}
	}
}

func TestRV64PublicFrozenGlobalDependenciesColdBound(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			producer := rv64NativeTokenInstantiate(t, rt, frozenDependencyCompile(t, false, false, artifact), NewImports())
			defer producer.Close()
			global, err := producer.ExportedGlobalObject("reference")
			if err != nil {
				t.Fatal(err)
			}
			if err := global.attachReferenceImporter(rt.refStore); err != nil {
				t.Fatal(err)
			}
			defer global.detachReferenceImporter()
			c := &Compiled{GlobalImports: []GlobalImportDef{{Type: ValFuncRef}, {Type: ValFuncRef}}}
			globals := []*resolvedGlobalImport{{global: global}, {global: global}}
			// AllocsPerRun warms the real member, receipt free slot and root ledger.
			allocations := testing.AllocsPerRun(20, func() {
				var cleanup resourceCleanup
				entries, err := railshotFreezeGlobalReferences(c, globals, rt.refStore, &cleanup)
				if err != nil {
					panic(err)
				}
				cleanup.finish()
				railshotReleaseGlobalReferences(entries)
			})
			if allocations > 4 || producer.resourceRefs != 1 || global.owner.importers != 1 {
				t.Fatalf("bounded frozen cold metadata allocations=%v refs=%d", allocations, producer.resourceRefs)
			}
			t.Logf("real retainer/registration/acquisition/alias cold metadata allocations=%v (bound 4)", allocations)
		})
	}
}

func TestRV64PublicFrozenGlobalDependenciesStartRollback(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			producer := rv64NativeTokenInstantiate(t, rt, frozenDependencyCompile(t, false, false, artifact), NewImports())
			defer producer.Close()
			global, err := producer.ExportedGlobalObject("reference")
			if err != nil {
				t.Fatal(err)
			}
			source := rv64ReferenceNoTableModule(false, true, false)
			source = rv64ReplaceTableStageSection(t, source, 1, wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, nil))))
			source = rv64ReplaceTableStageSection(t, source, 3, []byte{3, 5, 4, 0, 1, 1, 2})
			carrier := []byte{1, 1, 0x70, 0x23, 0xff, 1, 0x21, 0, 0x02, 0x70, 0x20, 0, 0x0c, 0, 0x0b, 0xd1, 0x0b}
			code := wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x0b}), wasmtest.Code([]byte{0x23, 0xff, 1, 0xd1, 0x0b}), append(wasmtest.ULEB(uint32(len(carrier))), carrier...), wasmtest.Code([]byte{0, 0x0b})))
			source = rv64ReplaceTableStageSection(t, source, 10, append(wasmtest.Section(8, []byte{3}), code...))
			cc := rv64NativeTailCompile(t, "frozen-dep-start-failure", source, artifact)
			callbacks := 0
			var consumer *Instance
			producer.referenceLifetime().afterPhysicalRelease(func() { callbacks++; panic("frozen producer start cleanup") })
			result, err := frozenDependencyInstantiate(t, rt, cc, NewImports().Global("env", "ref", global), true, func(in *Instance) error {
				consumer = in
				frozenDependencyTestState(t, rv64ExistingFrozenGlobalDependencyRows(in).rows.inline[0], dependencyTyped, in)
				in.referenceLifetime().afterPhysicalRelease(func() { callbacks++; panic("frozen consumer start cleanup") })
				return producer.Close()
			})
			var trap *TrapError
			if result != nil || consumer == nil || !errors.As(err, &trap) || trap.Code != coreruntime.TrapUnreachable || !errors.Is(err, ErrCallbackPanic) || callbacks != 2 || !rv64ExistingFrozenGlobalDependencyRows(consumer).complete() {
				t.Fatal("start trap/frozen cleanup outcome", err, callbacks)
			}
			for _, in := range []*Instance{producer, consumer} {
				rv64CheckGlobalDependencyRetirement(t, in)
				if in.jm != nil || in.ar != nil || in.eng != nil {
					t.Fatal("failed start native storage")
				}
			}
		})
	}
}

func TestRV64PublicFrozenGlobalDependenciesMappedCapacity(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, mode := range []string{"node", "receipt", "pending", "typed"} {
			t.Run(fmt.Sprintf("artifact%t-%s", artifact, mode), func(t *testing.T) {
				rt := NewRuntime()
				defer rt.Close()
				pc := frozenDependencyCompile(t, false, false, artifact)
				producer := rv64NativeTokenInstantiate(t, rt, pc, NewImports())
				defer producer.Close()
				consumer := rv64NativeTokenInstantiate(t, rt, pc, NewImports())
				defer consumer.Close()
				global, err := producer.ExportedGlobalObject("reference")
				if err != nil {
					t.Fatal(err)
				}
				if err := global.attachReferenceImporter(rt.refStore); err != nil {
					t.Fatal(err)
				}
				attached := true
				defer func() {
					if attached {
						global.detachReferenceImporter()
					}
				}()
				nodes := 2
				if mode == "node" || mode == "pending" {
					nodes = 1
				}
				g, err := newRV64FunctionDependencyGraph(nodes, 1)
				if err != nil {
					t.Fatal(err)
				}
				if mode == "node" || mode == "receipt" {
					// A real separate graph owner fills one exact node/receipt; this
					// capacity seam never resets or substitutes the process registry.
					blocker := rv64FunctionDependencyUnitOwners(t, 1)[0]
					member := &dependencyMember{owner: blocker}
					if err := g.register(member); err != nil {
						t.Fatal(err)
					}
					if mode == "receipt" {
						r, err := g.acquirePending(member, false)
						if err != nil {
							t.Fatal(err)
						}
						defer r.release()
					}
				}
				var cleanup resourceCleanup
				entries, err := rv64FreezeGlobalReferences(&Compiled{GlobalImports: []GlobalImportDef{{Type: ValFuncRef}, {Type: ValFuncRef}}}, []*resolvedGlobalImport{{global: global}, {global: global}}, rt.refStore, g, &cleanup)
				cleanup.finish()
				if err != nil {
					t.Fatal(err)
				}
				defer railshotReleaseGlobalReferences(entries)
				// Manual holder installation qualifies real mapped capacity units;
				// other tests prove the production constructor's same handoff.
				consumer.beginConstruction(nil)
				construction := true
				defer func() {
					if construction {
						consumer.endConstruction()
					}
				}()
				railshotInstallFrozenGlobalDependencies(consumer, entries)
				if err := railshotAdoptFrozenGlobalDependencies(consumer, true); err != nil {
					t.Fatal(err)
				}
				consumer.endConstruction()
				construction = false
				rows := rv64ExistingFrozenGlobalDependencyRows(consumer)
				row := rows.rows.inline[0]
				if len(entries) != 1 || producer.resourceRefs != 2 || row.root.producer != producer || rows.rows.n != 1 {
					t.Fatal("mapped exact capacity root/alias")
				}
				switch mode {
				case "typed":
					frozenDependencyTestState(t, row, dependencyTyped, consumer)
				case "pending":
					frozenDependencyTestState(t, row, dependencyPending, nil)
				default:
					if row.root.receipt.graph != nil {
						t.Fatal("capacity anonymous unit mislabeled as graph receipt")
					}
				}
				if err := producer.Close(); err != nil {
					t.Fatal(err)
				}
				global.detachReferenceImporter()
				attached = false
				if producer.resourcesClosed || producer.resourceRefs != 1 {
					t.Fatal("capacity root did not retain mapped B after ordinary attachment")
				}
				if err := consumer.Close(); err != nil || !rows.complete() || !producer.resourcesClosed || producer.resourceRefs != 0 {
					t.Fatal("mapped capacity terminal drain", err)
				}
				for _, in := range []*Instance{producer, consumer} {
					if in.jm != nil || in.ar != nil || in.eng != nil {
						t.Fatal("mapped capacity storage stranded")
					}
				}
				if mode == "typed" {
					rv64CheckGlobalDependencyRetirement(t, producer)
					rv64CheckGlobalDependencyRetirement(t, consumer)
				}
			})
		}
	}
}

func TestRV64PublicFrozenGlobalDependenciesRegistrationRollback(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			pc := frozenDependencyCompile(t, false, false, artifact)
			producer := rv64NativeTokenInstantiate(t, rt, pc, NewImports())
			defer producer.Close()
			reentry := rv64NativeTokenInstantiate(t, rt, pc, NewImports())
			defer reentry.Close()
			global, err := producer.ExportedGlobalObject("reference")
			if err != nil {
				t.Fatal(err)
			}
			producer.markNativeControlShared()
			cc := frozenDependencyCompile(t, true, false, artifact)
			imports := NewImports().Global("env", "ref", global)
			resolved, err := imports.snapshot()
			if err != nil {
				t.Fatal(err)
			}
			if err := railshotValidateResolvedImports(cc, resolved, rt.refStore); err != nil {
				t.Fatal(err)
			}
			if err := cc.prepareCodeMapping(); err != nil {
				t.Fatal(err)
			}
			view, err := cc.freezeExecution(0)
			if err != nil {
				t.Fatal(err)
			}
			rejected := NewRuntime()
			if err := rejected.Close(); err != nil {
				t.Fatal(err)
			}
			builder := instanceBuilder{c: view, opts: InstantiateOptions{Imports: imports, store: rt.refStore, runtime: rejected}, imports: resolved}
			marker := new(int)
			callbacks, unlocked, reentered := 0, false, false
			producer.referenceLifetime().afterPhysicalRelease(func() {
				callbacks++
				unlocked = nativeExecutionMu.TryLock()
				if unlocked {
					nativeExecutionMu.Unlock()
				}
				for _, mu := range []*Instance{producer} {
					if !mu.lifeMu.TryLock() {
						unlocked = false
					} else {
						mu.lifeMu.Unlock()
					}
				}
				if !global.owner.mu.TryLock() {
					unlocked = false
				} else {
					global.owner.mu.Unlock()
				}
				if !rt.refStore.mu.TryLock() {
					unlocked = false
				} else {
					rt.refStore.mu.Unlock()
				}
				if unlocked {
					result, err := reentry.Invoke("identity", 17)
					reentered = err == nil && slices.Equal(result, []uint64{17})
				}
				panic(marker)
			})
			// Pause the actual rejected Runtime registration itself. A table-free
			// artifact may use an independent construction native lock, so the
			// process native mutex is not a reliable registration barrier.
			rejected.mu.Lock()
			locked := true
			type outcome struct {
				value    any
				panicked bool
				in       *Instance
				err      error
			}
			done := make(chan outcome, 1)
			joined := false
			defer func() {
				if locked {
					rejected.mu.Unlock()
				}
				if !joined {
					select {
					case <-done:
					case <-time.After(3 * time.Second):
						t.Error("frozen registration rollback worker failed to join")
					}
				}
			}()
			go func() {
				var out outcome
				out.value, out.panicked = cleanupTestPanic(func() { out.in, out.err = builder.instantiate(cc) })
				done <- out
			}()
			deadline := time.Now().Add(3 * time.Second)
			for {
				producer.lifeMu.Lock()
				refs := producer.resourceRefs
				producer.lifeMu.Unlock()
				ownership := rv64ExistingFunctionDependencyOwnership(producer)
				receipts := 0
				if ownership != nil {
					if graph := ownership.member.graph.Load(); graph != nil {
						graph.mu.Lock()
						receipts = ownership.member.receipts
						graph.mu.Unlock()
					}
				}
				if refs == 2 && receipts == 2 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("constructor did not acquire separate counted/frozen Pending units before Runtime registration")
				}
				time.Sleep(time.Millisecond)
			}
			if err := producer.Close(); err != nil || producer.resourcesClosed {
				t.Fatal("concrete builder roots did not retain B", err)
			}
			rejected.mu.Unlock()
			locked = false
			select {
			case out := <-done:
				joined = true
				if !out.panicked || out.value != marker || out.in != nil || callbacks != 1 || !unlocked || !reentered || railshotHasBuilderGlobalDependencies(&builder.globalAttachments) {
					t.Fatal("pre-install Runtime failure lost builder frozen ownership/first panic", out, callbacks, unlocked, reentered)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("frozen registration rollback did not complete")
			}
			rv64CheckGlobalDependencyRetirement(t, producer)
			if global.owner.importers != 0 || producer.resourceRefs != 0 || producer.jm != nil || producer.ar != nil || producer.eng != nil {
				t.Fatal("pre-install frozen rollback retained actual B storage")
			}
		})
	}
}
