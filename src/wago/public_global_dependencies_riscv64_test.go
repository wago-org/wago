//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64PublicGlobalDependenciesFrozenOwnerSeparation(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, alias := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-alias%t", artifact, alias), func(t *testing.T) {
				rt := NewRuntime()
				defer rt.Close()
				bc := rv64NativeTailCompile(t, "global-dep-frozen-descriptor-provider", rv64ReferenceNoTableModule(false, false, false), artifact)
				b := rv64NativeTokenInstantiate(t, rt, bc, NewImports())
				defer b.Close()
				bg, err := b.ExportedGlobalObject("reference")
				if err != nil {
					t.Fatal(err)
				}
				asource := rv64ReferenceNoTableModule(false, true, false)
				asource = rv64ReplaceTableStageSection(t, asource, 7, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("first", 3, 254), wasmtest.ExportEntry("second", 3, 255), wasmtest.ExportEntry("isnull", 0, 1), wasmtest.ExportEntry("carrier", 0, 2))))
				ac := rv64NativeTailCompile(t, "global-dep-frozen-cell-relay", asource, artifact)
				a := rv64NativeTokenInstantiate(t, rt, ac, NewImports().Global("env", "ref", bg))
				defer a.Close()
				first, err := a.ExportedGlobalObject("first")
				if err != nil {
					t.Fatal(err)
				}
				second, err := a.ExportedGlobalObject("second")
				if err != nil || first == second || first.instanceOwner() != a || second.instanceOwner() != a {
					t.Fatal("fixture does not have actual distinct A-owned cells", err)
				}
				if alias {
					second = first
				}
				csource := rv64ReferenceImportModule(false, false)
				csource = rv64ReplaceTableStageSection(t, csource, 2, wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "first", wasm.FuncRef, false), wasmtest.GlobalImportEntry("env", "second", wasm.FuncRef, false), wasmtest.GlobalImportEntry("env", "first", wasm.FuncRef, false))))
				var locals [][]byte
				for index := uint32(3); index < 256; index++ {
					locals = append(locals, wasmtest.GlobalEntry(wasm.FuncRef, false, append(append([]byte{0x23}, wasmtest.ULEB(index-1)...), 0x0b)))
				}
				csource = rv64ReplaceTableStageSection(t, csource, 6, wasmtest.Section(6, wasmtest.Vec(locals...)))
				cc := rv64NativeTailCompile(t, "global-dep-frozen-consumer", csource, artifact)
				imports := NewImports().Global("env", "first", first).Global("env", "second", second)
				resolved, err := imports.snapshot()
				if err != nil {
					t.Fatal(err)
				}
				// Admission's temporary ordinary attach/detach pairs are separate
				// from constructor rows and must leave no counted/frozen acquisition.
				beforeA, beforeB := a.resourceRefs, b.resourceRefs
				if err := rv64ValidateResolvedGlobals(cc.executionView(), resolved, rt.refStore); err != nil || a.resourceRefs != beforeA || b.resourceRefs != beforeB || first.owner.importers != 0 || second.owner.importers != 0 {
					t.Fatal("anonymous preflight changed terminal ownership", err)
				}
				consumer := rv64NativeTokenInstantiate(t, rt, cc, imports)
				defer consumer.Close()
				want := 2
				if alias {
					want = 1
				}
				rows := rv64ExistingGlobalDependencyRows(consumer)
				frozen := consumer.importGlobalReferences()
				if rows == nil || rows.rows.n != want || len(frozen) != want || a.resourceRefs != beforeA+want || b.resourceRefs != beforeB+1 || int(rv64ExistingFunctionDependencyOwnership(consumer).member.outgoing.Load()) != want+1 {
					t.Fatal("counted A cells and separately deduplicated B roots were conflated")
				}
				for _, entry := range frozen {
					if entry.owner != b || entry.owner == a || entry.descriptor == 0 || entry.dependency == nil || entry.dependency.root.producer != b {
						t.Fatal("frozen descriptor does not name its real B owner")
					}
					frozenDependencyTestState(t, entry.dependency, dependencyTyped, consumer)
				}
				if rv64ExistingFrozenGlobalDependencyRows(consumer).rows.n != 1 {
					t.Fatal("distinct/aliased A cells acquired multiple B descriptor units")
				}
				rows.rows.each(func(row *rv64GlobalDependencyRow) {
					if row.root.producer != a || row.global.instanceOwner() != a {
						t.Fatal("counted cell receipt was mislabeled as its descriptor owner")
					}
				})
				if err := b.Close(); err != nil || b.resourcesClosed {
					t.Fatal("frozen B storage was not retained", err)
				}
				if err := a.Close(); err != nil || a.resourcesClosed {
					t.Fatal("counted A storage was not retained", err)
				}
				importedGlobalInvoke(t, consumer, "isnull", []uint64{0})
				if err := consumer.Close(); err != nil || !rows.complete() || first.owner.importers != 0 || second.owner.importers != 0 {
					t.Fatal("independent A/B cleanup left a count/row", err)
				}
				for _, in := range []*Instance{a, b, consumer} {
					rv64CheckGlobalDependencyRetirement(t, in)
					if !in.resourcesClosed || in.resourceRefs != 0 || in.jm != nil || in.ar != nil || in.eng != nil {
						t.Fatal("A/B cleanup stranded actual native storage")
					}
				}
			})
		}
	}
}

func TestRV64PublicGlobalDependenciesObserverFailure(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, observerPanic := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-panic%t", artifact, observerPanic), func(t *testing.T) {
				pc := rv64NativeTailCompile(t, "global-dep-provider-vfalse-mtrue", rv64GlobalDependencyModule(false, false, false, true), artifact)
				producer, err := Instantiate(pc)
				if err != nil {
					t.Fatal(err)
				}
				defer producer.Close()
				first, err := producer.ExportedGlobalObject("g0")
				if err != nil {
					t.Fatal(err)
				}
				second, err := producer.ExportedGlobalObject("g1")
				if err != nil {
					t.Fatal(err)
				}
				memory, err := producer.ExportedMemory("memory")
				if err != nil {
					t.Fatal(err)
				}
				fn, err := producer.ExportedFunc("identity")
				if err != nil {
					t.Fatal(err)
				}
				imports := NewImports().Global("env", "first", first).Global("env", "second", second).Memory("env", "memory", memory).Function("env", "function", fn)
				cc := rv64NativeTailCompile(t, "global-dep-consumer-vfalse-ftrue-mtrue", rv64GlobalDependencyModule(true, false, true, true), artifact)
				primary := errors.New("first global observer failure")
				var consumer *Instance
				var result *Instance
				callbacks := 0
				producer.referenceLifetime().afterPhysicalRelease(func() { callbacks++; panic("producer cleanup") })
				_, panicked := cleanupTestPanic(func() {
					result, err = Instantiate(cc, InstantiateOptions{Imports: imports, afterCreate: func(in *Instance) error {
						consumer = in
						in.referenceLifetime().afterPhysicalRelease(func() { callbacks++; panic("consumer cleanup") })
						if err := producer.Close(); err != nil {
							return err
						}
						if observerPanic {
							panic(primary)
						}
						return primary
					}})
				})
				if panicked || result != nil || !errors.Is(err, primary) || !errors.Is(err, ErrCallbackPanic) || callbacks != 2 || consumer == nil || !rv64ExistingGlobalDependencyRows(consumer).complete() {
					t.Fatal("global cleanup replaced the primary observer failure or skipped a unit", err, panicked, callbacks)
				}
				for _, in := range []*Instance{producer, consumer} {
					rv64CheckGlobalDependencyRetirement(t, in)
					if !in.resourcesClosed || in.finalizing || in.resourceRefs != 0 || in.jm != nil || in.ar != nil || in.eng != nil {
						t.Fatal("failed observer stranded actual native ownership")
					}
				}
				if first.owner.importers != 0 || second.owner.importers != 0 || memory.state.Load().importerCount() != 0 {
					t.Fatal("failed observer stranded a global/memory counter")
				}
			})
		}
	}
}

func rv64GlobalDependencyModule(imported, vector, function, memory bool) []byte {
	typ := wasm.I32
	if vector {
		typ = wasm.V128
	}
	types := wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ}), wasmtest.FuncType(nil, []wasm.ValType{typ, typ}), wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ, typ})))
	sections := [][]byte{types}
	shift := uint32(0)
	if imported {
		imports := [][]byte{wasmtest.GlobalImportEntry("env", "first", typ, true), wasmtest.GlobalImportEntry("env", "second", typ, true)}
		if function {
			imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name("function")...), 0, 0))
			shift = 1
		}
		if memory {
			imports = append(imports, rv64M64Import("memory", false, 1, 4))
		}
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(imports...)))
	}
	sections = append(sections, wasmtest.Section(3, []byte{3, 1, 2, 0}))
	if memory && !imported {
		sections = append(sections, wasmtest.Section(5, wasmtest.Vec(rv64M64Limit(false, 1, 4))))
	}
	if !imported {
		first, second := []byte{0x41, 0x7f, 0x0b}, []byte{0x41, 7, 0x0b}
		if vector {
			first, second = []byte{0xfd, 12}, []byte{0xfd, 12}
			for _, bits := range []uint64{0x7f80000180000001, 0xfff0000000000001} {
				first = binary.LittleEndian.AppendUint64(first, bits)
			}
			for _, bits := range []uint64{0x0123456789abcdef, 0xfedcba9876543210} {
				second = binary.LittleEndian.AppendUint64(second, bits)
			}
			first, second = append(first, 0x0b), append(second, 0x0b)
		}
		sections = append(sections, wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(typ, true, first), wasmtest.GlobalEntry(typ, true, second))))
	}
	identity := shift + 2
	if imported && function {
		identity = 0
	}
	exports := [][]byte{wasmtest.ExportEntry("get", 0, shift), wasmtest.ExportEntry("set", 0, shift+1), wasmtest.ExportEntry("identity", 0, identity), wasmtest.ExportEntry("g0", 3, 0), wasmtest.ExportEntry("g1", 3, 1)}
	if memory {
		exports = append(exports, wasmtest.ExportEntry("memory", 2, 0))
	}
	return wasmtest.Module(append(sections, wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0, 0x23, 1, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x24, 0, 0x23, 0, 0x23, 1, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x0b}))))...)
}

func TestRV64PublicGlobalDependenciesSameHome(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			home, err := NewSharedMemory(1, 4)
			if err != nil {
				t.Fatal(err)
			}
			defer home.Close()
			source := rv64GlobalDependencyModule(false, false, false, false)
			types := wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32, wasm.I32})))
			source = rv64ReplaceTableStageSection(t, source, 1, append(types, wasmtest.Section(2, wasmtest.Vec(rv64M64Import("home", false, 1, 4)))...))
			pc := rv64NativeTailCompile(t, "global-dep-same-home-provider", source, artifact)
			var producers [2]*Instance
			var globals [2]*Global
			for i := range producers {
				producers[i], err = Instantiate(pc, NewImports().Memory("env", "home", home))
				if err != nil {
					t.Fatal(err)
				}
				defer producers[i].Close()
				globals[i], err = producers[i].ExportedGlobalObject("g0")
				if err != nil || globals[i].instanceOwner() != producers[i] {
					t.Fatal(err)
				}
			}
			if producers[0] == producers[1] || producers[0].jm != producers[1].jm || producers[0].ar == producers[1].ar || globals[0].owner == globals[1].owner {
				t.Fatal("fixture does not share JobMemory across distinct global arena owners")
			}
			cc := rv64NativeTailCompile(t, "global-dep-consumer-vfalse-ffalse-mfalse", rv64GlobalDependencyModule(true, false, false, false), artifact)
			consumer, err := Instantiate(cc, NewImports().Global("env", "first", globals[0]).Global("env", "second", globals[1]))
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			rows := rv64ExistingGlobalDependencyRows(consumer)
			if rows.rows.n != 2 || rows.rows.inline[0].root.producer != producers[0] || rows.rows.inline[1].root.producer != producers[1] {
				t.Fatal("same-home producers were conflated by backing identity")
			}
			for _, producer := range producers {
				if err := producer.Close(); err != nil || producer.resourcesClosed || producer.resourceRefs != 1 {
					t.Fatal("distinct actual global producer root lost", err)
				}
			}
			importedGlobalInvoke(t, consumer, "set", []uint64{0x80000001, 0xffffffff}, 0xfeedface80000001)
			if err := consumer.Close(); err != nil || !rows.complete() {
				t.Fatal(err)
			}
			for _, producer := range producers {
				if !producer.resourcesClosed || producer.jm != nil || producer.ar != nil || producer.eng != nil || producer.resourceRefs != 0 {
					t.Fatal("same-home global ownership stranded native storage")
				}
			}
			if err := home.Close(); err != nil {
				t.Fatal("host-only memory count remained attached", err)
			}
		})
	}
}

func TestRV64PublicGlobalDependenciesOverflowCleanup(t *testing.T) {
	graph, err := newRV64FunctionDependencyGraph(10, 4)
	if err != nil {
		t.Fatal(err)
	}
	pc := importedGlobalCompile(t, false, 0, true)
	var producers [9]*Instance
	var globals [10]*Global
	var visited [9]int
	first := new(int)
	for i := range producers {
		producers[i], err = Instantiate(pc)
		if err != nil {
			t.Fatal(err)
		}
		defer producers[i].Close()
		globals[i], err = producers[i].ExportedGlobalObject("g0")
		if err != nil {
			t.Fatal(err)
		}
		index, marker := i, new(int)
		if i == 0 {
			marker = first
		}
		producers[i].referenceLifetime().afterPhysicalRelease(func() { visited[index]++; panic(marker) })
	}
	globals[9] = NewGlobalI32(7, true)
	defer globals[9].Close()
	consumer, err := Instantiate(pc)
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	// Additional actual mapped attachments exercise overflow and mixed capacity;
	// the source/artifact fixture separately establishes native imported execution.
	var batch globalImportAttachments
	var cleanup resourceCleanup
	for _, global := range globals {
		if err := rv64AcquireGlobalDependency(&batch, global, nil, nil, graph, &cleanup); err != nil {
			t.Fatal(err)
		}
	}
	consumer.beginConstruction(nil)
	railshotInstallGlobalDependencies(consumer, &batch)
	if err := railshotAdoptGlobalDependencies(consumer, true); err != nil {
		t.Fatal(err)
	}
	consumer.endConstruction()
	rows := rv64ExistingGlobalDependencyRows(consumer)
	if rows.rows.n != 10 || rows.rows.index == nil || len(rows.rows.extra) != 6 {
		t.Fatal("fixture lacks actual inline, overflow and indexed rows")
	}
	for _, producer := range producers {
		if err := producer.Close(); err != nil || producer.resourcesClosed || producer.resourceRefs != 1 {
			t.Fatal("mixed receipt failed to retain a real producer", err)
		}
	}
	// Close reports callback panics as errors. Pin its real storage until a
	// final raw release so this oracle can compare the first panic itself.
	if !consumer.retainResourceRoot() {
		t.Fatal("cannot retain actual consumer before logical Close")
	}
	if err := consumer.Close(); err != nil || consumer.resourcesClosed {
		t.Fatal("logical Close overtook the final physical root", err)
	}
	value, panicked := cleanupTestPanic(consumer.releaseResourceRoot)
	if !panicked || value != first || !rows.complete() || graph.live != 0 || !consumer.resourcesClosed || consumer.jm != nil || consumer.ar != nil || consumer.eng != nil {
		t.Fatal("overflow drain skipped an ownership unit or replaced first panic", value, panicked)
	}
	rv64CheckGlobalDependencyRetirement(t, consumer)
	for i, producer := range producers {
		rv64CheckGlobalDependencyRetirement(t, producer)
		if visited[i] != 1 || !producer.resourcesClosed || producer.finalizing || producer.resourceRefs != 0 || producer.jm != nil || producer.ar != nil || producer.eng != nil || globals[i].owner.importers != 0 {
			t.Fatal("later overflow producer did not complete native cleanup", i, visited)
		}
	}
	if globals[9].owner.importers != 0 {
		t.Fatal("host count was lost after an earlier physical panic")
	}
	consumer.transferImportedGlobalAttachment(globals[0])
	if _, exists := transferredImportAttachments.Load(consumer); exists {
		t.Fatal("repeated terminal transfer resurrected legacy state")
	}
}

func TestRV64PublicGlobalDependenciesSourceArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, alias := range []bool{false, true} {
				for _, function := range []bool{false, true} {
					for _, memory := range []bool{false, true} {
						for _, runtimeMode := range []bool{false, true} {
							t.Run(fmt.Sprintf("artifact%t-vector%t-alias%t-function%t-memory%t-runtime%t", artifact, vector, alias, function, memory, runtimeMode), func(t *testing.T) {
								pc := rv64NativeTailCompile(t, fmt.Sprintf("global-dep-provider-v%t-m%t", vector, memory), rv64GlobalDependencyModule(false, vector, false, memory), artifact)
								producer, err := Instantiate(pc)
								if err != nil {
									t.Fatal(err)
								}
								defer producer.Close()
								first, err := producer.ExportedGlobalObject("g0")
								if err != nil {
									t.Fatal(err)
								}
								second, err := producer.ExportedGlobalObject("g1")
								if err != nil || second == first {
									t.Fatal("actual distinct cells", err)
								}
								if alias {
									second = first
								}
								bindings := func(source *Instance) *Imports {
									a, e := source.ExportedGlobalObject("g0")
									if e != nil || a != first || a.instanceOwner() != producer {
										t.Fatal("canonical global reexport changed actual owner", e)
									}
									b, e := source.ExportedGlobalObject("g1")
									if e != nil || b != second {
										if source == producer && alias {
											b = first
										} else {
											t.Fatal("second canonical global reexport", e)
										}
									}
									imports := NewImports().Global("env", "first", a).Global("env", "second", b)
									if function {
										export, e := source.ExportedFunc("identity")
										if e != nil || export.inst != producer {
											t.Fatal("canonical native function owner", e)
										}
										imports.Function("env", "function", export)
									}
									if memory {
										m, e := source.ExportedMemory("memory")
										if e != nil || m.instanceOwner() != producer {
											t.Fatal("canonical memory owner", e)
										}
										imports.Memory("env", "memory", m)
									}
									return imports
								}
								cc := rv64NativeTailCompile(t, fmt.Sprintf("global-dep-consumer-v%t-f%t-m%t", vector, function, memory), rv64GlobalDependencyModule(true, vector, function, memory), artifact)
								expectedRows := 2
								if alias {
									expectedRows = 1
								}
								var consumers [2]*Instance
								source := producer
								for i := range consumers {
									opts := InstantiateOptions{Imports: bindings(source), afterCreate: func(in *Instance) error {
										if !in.constructionIsActive() || !nativeExecutionMu.TryLock() {
											return fmt.Errorf("global handoff lacks construction exclusion or owns native lease")
										}
										nativeExecutionMu.Unlock()
										return rv64CheckGlobalDependencyAdoption(in, producer, expectedRows)
									}}
									if runtimeMode {
										rt := NewRuntime()
										defer rt.Close()
										opts.runtime = rt
									}
									consumers[i], err = Instantiate(cc, opts)
									if err != nil {
										t.Fatal(err)
									}
									defer consumers[i].Close()
									source = consumers[i]
									rows := rv64ExistingGlobalDependencyRows(source)
									want := 2
									if alias {
										want = 1
									}
									outgoing := want
									if function {
										outgoing++
									}
									if memory {
										outgoing++
									}
									if rows == nil || rows.rows.n != want || int(rv64ExistingFunctionDependencyOwnership(source).member.outgoing.Load()) != outgoing {
										t.Fatal("counted global/function/memory units were conflated")
									}
									rows.rows.each(func(row *rv64GlobalDependencyRow) {
										graph := row.root.receipt.graph
										if graph == nil {
											t.Fatal("unexpected anonymous fallback in bounded fixture")
										}
										graph.mu.Lock()
										record, _ := graph.recordLocked(row.root.receipt.identity)
										valid := record != nil && record.state == dependencyTyped && record.kind == dependencyGlobalCell && record.target.owner == producer && record.consumer.owner == source
										graph.mu.Unlock()
										if !valid || row.root.producer != producer {
											t.Fatal("receipt names an incorrect actual cell producer")
										}
									})
								}
								if err := producer.Close(); err != nil || producer.resourcesClosed {
									t.Fatal("counted acquisitions failed to pin producer", err)
								}
								args := []uint64{0xfeedface80000001}
								firstWant, secondWant := []uint64{0x80000001}, []uint64{7}
								if vector {
									args = []uint64{0x7f80000180000001, 0xfff0000000000001}
									firstWant, secondWant = slices.Clone(args), []uint64{0x0123456789abcdef, 0xfedcba9876543210}
								}
								if alias {
									secondWant = firstWant
								}
								want := append(slices.Clone(firstWant), secondWant...)
								importedGlobalInvoke(t, consumers[0], "set", want, args...)
								importedGlobalInvoke(t, consumers[1], "get", want)
								importedGlobalInvoke(t, consumers[1], "identity", firstWant, args...)
								for i, consumer := range consumers {
									if err := consumer.Close(); err != nil || !rv64ExistingGlobalDependencyRows(consumer).complete() || i == 0 && producer.resourcesClosed {
										t.Fatal("global counter drain released too early or lost a row", err)
									}
								}
								for _, in := range append(consumers[:], producer) {
									rv64CheckGlobalDependencyRetirement(t, in)
								}
								if !producer.resourcesClosed || producer.resourceRefs != 0 || producer.jm != nil || producer.ar != nil || producer.eng != nil || first.owner.importers != 0 || second.owner.importers != 0 || len(first.cell) != 0 || len(second.cell) != 0 {
									t.Fatal("global import receipts stranded actual native cells/resources")
								}
							})
						}
					}
				}
			}
		}
	}
}

// The same oracle runs inside afterCreate and after Instantiate returns: the
// callback must never observe pending cell receipts during constructor handoff.
func rv64CheckGlobalDependencyAdoption(consumer, producer *Instance, want int) error {
	rows := rv64ExistingGlobalDependencyRows(consumer)
	if rows == nil || rows.rows.n != want {
		return errors.New("observer saw missing counted global rows")
	}
	var result error
	rows.rows.each(func(row *rv64GlobalDependencyRow) {
		graph := row.root.receipt.graph
		if graph == nil {
			result = errors.New("observer saw anonymous fallback in bounded fixture")
			return
		}
		graph.mu.Lock()
		record, _ := graph.recordLocked(row.root.receipt.identity)
		valid := record != nil && record.state == dependencyTyped && record.kind == dependencyGlobalCell && record.target != nil && record.consumer != nil && record.target.owner == producer && record.consumer.owner == consumer
		graph.mu.Unlock()
		if !valid || row.root.producer != producer {
			result = errors.New("observer saw an unadopted or incorrect global receipt")
		}
	})
	return result
}

// These are additional real mapped counter attachments used to qualify transfer
// exclusion; source/artifact constructors separately qualify native imports.
func TestRV64PublicGlobalDependenciesOperationLease(t *testing.T) {
	for _, selected := range []int{0, 1, 2} {
		t.Run(fmt.Sprintf("row%d", selected), func(t *testing.T) {
			g, err := newRV64FunctionDependencyGraph(3, 1)
			if err != nil {
				t.Fatal(err)
			}
			pc := importedGlobalCompile(t, false, 0, true)
			var producers [2]*Instance
			var globals [3]*Global
			for i := range producers {
				producers[i], err = Instantiate(pc)
				if err != nil {
					t.Fatal(err)
				}
				defer producers[i].Close()
				globals[i], err = producers[i].ExportedGlobalObject("g0")
				if err != nil {
					t.Fatal(err)
				}
			}
			globals[2] = NewGlobalI32(7, true)
			defer globals[2].Close()
			consumer, err := Instantiate(pc)
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			var batch globalImportAttachments
			var cleanup resourceCleanup
			for _, global := range globals {
				if err := rv64AcquireGlobalDependency(&batch, global, nil, nil, g, &cleanup); err != nil {
					t.Fatal(err)
				}
			}
			consumer.beginConstruction(nil)
			railshotInstallGlobalDependencies(consumer, &batch)
			if err := railshotAdoptGlobalDependencies(consumer, true); err != nil {
				t.Fatal(err)
			}
			consumer.endConstruction()
			rows := rv64ExistingGlobalDependencyRows(consumer)
			for _, producer := range producers {
				if err := producer.Close(); err != nil || producer.resourcesClosed {
					t.Fatal("actual imported root failed to retain producer", err)
				}
			}
			state := globals[selected].owner
			state.mu.Lock()
			done := make(chan struct{})
			go func() { consumer.transferImportedGlobalAttachment(globals[selected]); close(done) }()
			deadline := time.Now().Add(3 * time.Second)
			for {
				rows.mu.Lock()
				releasing := rows.rows.inline[selected].state == rv64GlobalDependencyReleasing
				rows.mu.Unlock()
				if releasing {
					break
				}
				if time.Now().After(deadline) {
					state.mu.Unlock()
					t.Fatal("operation did not claim its exact row")
				}
				time.Sleep(time.Millisecond)
			}
			if err := consumer.Close(); err != nil || consumer.resourcesClosed || consumer.finalizing || consumer.resourceRefs != 1 || consumer.jm == nil || consumer.ar == nil || consumer.eng == nil {
				state.mu.Unlock()
				t.Fatal("counter-in-flight operation lost native consumer lifetime", err)
			}
			state.mu.Unlock()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("operation root failed to complete terminal drain")
			}
			if !rows.complete() || !consumer.resourcesClosed || consumer.resourceRefs != 0 || consumer.jm != nil || consumer.ar != nil || consumer.eng != nil || g.live != 0 {
				t.Fatal("operation publication/root drop failed to complete physical cleanup")
			}
			for _, global := range globals {
				global.owner.mu.Lock()
				count := global.owner.importers
				global.owner.mu.Unlock()
				if count != 0 {
					t.Fatal("typed/anonymous/host counter leak", count)
				}
			}
			for _, in := range append(producers[:], consumer) {
				rv64CheckGlobalDependencyRetirement(t, in)
			}
			if _, ok := transferredImportAttachments.Load(consumer); ok {
				t.Fatal("concurrent transfer resurrected legacy state")
			}
			consumer.transferImportedGlobalAttachment(globals[selected])
		})
	}
}

func TestRV64PublicGlobalDependenciesProducerCallbackLease(t *testing.T) {
	for _, selected := range []int{0, 1} { // typed receipt, capacity fallback
		for _, callbackPanic := range []bool{false, true} {
			t.Run(fmt.Sprintf("row%d-panic%t", selected, callbackPanic), func(t *testing.T) {
				graph, err := newRV64FunctionDependencyGraph(3, 1)
				if err != nil {
					t.Fatal(err)
				}
				pc := importedGlobalCompile(t, false, 0, true)
				var producers [2]*Instance
				var globals [3]*Global
				for i := range producers {
					producers[i], err = Instantiate(pc)
					if err != nil {
						t.Fatal(err)
					}
					defer producers[i].Close()
					globals[i], err = producers[i].ExportedGlobalObject("g0")
					if err != nil {
						t.Fatal(err)
					}
				}
				globals[2] = NewGlobalI32(7, true)
				defer globals[2].Close()
				consumer, err := Instantiate(pc)
				if err != nil {
					t.Fatal(err)
				}
				defer consumer.Close()
				var batch globalImportAttachments
				var cleanup resourceCleanup
				for _, global := range globals {
					if err := rv64AcquireGlobalDependency(&batch, global, nil, nil, graph, &cleanup); err != nil {
						t.Fatal(err)
					}
				}
				consumer.beginConstruction(nil)
				railshotInstallGlobalDependencies(consumer, &batch)
				if err := railshotAdoptGlobalDependencies(consumer, true); err != nil {
					t.Fatal(err)
				}
				consumer.endConstruction()
				rows := rv64ExistingGlobalDependencyRows(consumer)
				if (rows.rows.inline[selected].root.receipt.graph != nil) != (selected == 0) {
					t.Fatal("fixture did not establish typed and anonymous rows")
				}
				entered, resume := make(chan struct{}), make(chan struct{})
				defer close(resume)
				marker := new(int)
				producers[selected].referenceLifetime().afterPhysicalRelease(func() {
					close(entered)
					<-resume
					if callbackPanic {
						panic(marker)
					}
				})
				for _, producer := range producers {
					if err := producer.Close(); err != nil || producer.resourcesClosed {
						t.Fatal("actual global root failed to retain producer", err)
					}
				}
				type outcome struct {
					value    any
					panicked bool
				}
				done := make(chan outcome, 1)
				go func() {
					value, panicked := cleanupTestPanic(func() { consumer.transferImportedGlobalAttachment(globals[selected]) })
					done <- outcome{value, panicked}
				}()
				select {
				case <-entered:
				case <-time.After(3 * time.Second):
					t.Fatal("producer physical callback did not begin")
				}
				rows.mu.Lock()
				releasing := rows.rows.inline[selected].state == rv64GlobalDependencyReleasing
				rows.mu.Unlock()
				globals[selected].owner.mu.Lock()
				count := globals[selected].owner.importers
				globals[selected].owner.mu.Unlock()
				if !releasing || count != 0 {
					t.Fatal("callback must run after counter consumption and before row publication")
				}
				if err := consumer.Close(); err != nil || consumer.resourcesClosed || consumer.finalizing || consumer.resourceRefs != 1 || consumer.jm == nil || consumer.ar == nil || consumer.eng == nil || consumer.nativeContext == 0 {
					t.Fatal("producer callback overtook consumer operation lifetime", err)
				}
				resume <- struct{}{}
				select {
				case out := <-done:
					if out.panicked != callbackPanic || callbackPanic && out.value != marker {
						t.Fatal("operation replaced its first physical callback panic", out)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("producer callback did not finish terminal cleanup")
				}
				if !rows.complete() || graph.live != 0 || !consumer.resourcesClosed || consumer.resourceRefs != 0 || consumer.jm != nil || consumer.ar != nil || consumer.eng != nil || consumer.nativeContext != 0 {
					t.Fatal("operation root was dropped before publication or lost terminal cleanup")
				}
				for i, global := range globals {
					if global.owner.importers != 0 || i < 2 && (!producers[i].resourcesClosed || producers[i].resourceRefs != 0 || producers[i].jm != nil || producers[i].ar != nil || producers[i].eng != nil) {
						t.Fatal("callback transfer stranded a later counter or real native producer")
					}
				}
				for _, in := range append(producers[:], consumer) {
					rv64CheckGlobalDependencyRetirement(t, in)
				}
				consumer.transferImportedGlobalAttachment(globals[selected])
				if _, exists := transferredImportAttachments.Load(consumer); exists {
					t.Fatal("callback transfer resurrected legacy state")
				}
			})
		}
	}
}
