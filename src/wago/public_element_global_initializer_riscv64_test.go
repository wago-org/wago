//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Direct and copied immutable globals initialize distinct slots; the last slot
// is a literal null. The reference's target signature stays numeric/vector.
func rv64ElementGlobalModule(vector, imported, borrowed, null bool, flags byte, offset byte) []byte {
	typ := wasm.I32
	if vector {
		typ = wasm.V128
	}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(
		wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ}),
		wasmtest.FuncType([]wasm.ValType{typ, wasm.I32}, []wasm.ValType{typ}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	))}
	var imports [][]byte
	if imported {
		imports = append(imports, wasmtest.GlobalImportEntry("env", "ref", wasm.FuncRef, false))
	}
	if borrowed {
		imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name("table")...), 1, 0x70, 1, 3, 3))
	}
	if len(imports) != 0 {
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(imports...)))
	}
	sections = append(sections, wasmtest.Section(3, []byte{3, 0, 1, 2}))
	if !borrowed {
		sections = append(sections, wasmtest.Section(4, []byte{1, 0x70, 1, 3, 3}))
	}
	var globals [][]byte
	if !imported {
		expr := []byte{0xd2, 0, 0x0b}
		if null {
			expr = []byte{0xd0, 0x70, 0x0b}
		}
		globals = append(globals, wasmtest.GlobalEntry(wasm.FuncRef, false, expr))
	}
	globals = append(globals, wasmtest.GlobalEntry(wasm.FuncRef, false, []byte{0x23, 0, 0x0b}))
	sections = append(sections,
		wasmtest.Section(6, wasmtest.Vec(globals...)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("identity", 0, 0), wasmtest.ExportEntry("call", 0, 1), wasmtest.ExportEntry("install", 0, 2), wasmtest.ExportEntry("table", 1, 0))),
		wasmtest.Section(9, wasmtest.Vec(rv64ExpressionSegment(flags, offset, []byte{0x23, 0, 0x0b}, []byte{0x23, 1, 0x0b}, []byte{0xd0, 0x70, 0x0b}))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x11, 0, 0, 0x0b}),
			wasmtest.Code([]byte{0x41, 0, 0x41, 0, 0x41, 3, 0xfc, 12, 0, 0, 0xfc, 13, 0, 0x41, 3, 0x0b}),
		)),
	)
	return wasmtest.Module(sections...)
}

func rv64ElementGlobalArguments(vector bool) ([]uint64, []uint64) {
	if vector {
		return []uint64{0x0123456789abcdef, 0xfedcba9876543210}, []uint64{0x0123456789abcdef, 0xfedcba9876543210}
	}
	return []uint64{0xfeedface00000007}, []uint64{7}
}

func TestRV64PublicElementGlobalInitializers(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, imported := range []bool{false, true} {
			for _, null := range []bool{false, true} {
				for _, flags := range []byte{4, 5, 6} {
					for _, artifact := range []bool{false, true} {
						t.Run(fmt.Sprintf("vector%t-imported%t-null%t-flags%d-artifact%t", vector, imported, null, flags, artifact), func(t *testing.T) {
							rt := NewRuntime()
							defer rt.Close()
							imports := NewImports()
							var producer *Instance
							var pc *Compiled
							if imported {
								if null {
									g, err := rt.NewFuncRefGlobal(FuncRef{}, false)
									if err != nil {
										t.Fatal(err)
									}
									defer g.Close()
									imports.Global("env", "ref", g)
								} else {
									pc = rv64TableStageCompile(t, fmt.Sprintf("element-global-producer-vector%t", vector), rv64NativeTokenModule(vector, true, true, false), artifact)
									producer = rv64NativeTokenInstantiate(t, rt, pc, NewImports())
									g, err := producer.ExportedGlobalObject("reference")
									if err != nil {
										t.Fatal(err)
									}
									imports.Global("env", "ref", g)
								}
							}
							key := fmt.Sprintf("element-global-vector%t-imported%t-null%t-flags%d", vector, imported, null, flags)
							c := rv64TableStageCompile(t, key, rv64ElementGlobalModule(vector, imported, false, null, flags, 0), artifact)
							in := rv64NativeTokenInstantiate(t, rt, c, imports)
							c.Close()
							if producer != nil {
								producer.Close()
								pc.Close()
							}
							if flags == 5 {
								importedGlobalInvoke(t, in, "install", []uint64{3})
							}
							args, want := rv64ElementGlobalArguments(vector)
							for index := uint64(0); index < 3; index++ {
								call := append(slices.Clone(args), index)
								if null || index == 2 {
									_, err := in.Invoke("call", call...)
									tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
								} else {
									importedGlobalInvoke(t, in, "call", want, call...)
								}
							}
							_, err := in.Invoke("install")
							tableTestExpectTrap(t, err, TrapTableOutOfBounds)
							if !null {
								bits := readGlobalObject(in.globalCells[0], ValFuncRef)
								owner := in
								if producer != nil {
									owner = producer
								}
								canonical, valid := rv64PublishedDescriptor(owner, bits)
								if !valid {
									t.Fatal("global descriptor has no authenticated owner")
								}
								for index := 0; index < 2; index++ {
									row := in.tableDescriptor(0)[8+index*coreruntime.TableEntryBytes : 8+(index+1)*coreruntime.TableEntryBytes]
									if !bytes.Equal(row, canonical[:coreruntime.TableEntryBytes]) {
										t.Fatal("copied global changed canonical code, key, home or descriptor")
									}
								}
							}
							in.Close()
							if producer != nil && producer.hasPhysicalResources() {
								t.Fatal("element global retained producer after consumer close")
							}
						})
					}
				}
			}
		}
	}
}

// Declarative global expressions are validated against their construction-time
// globals, but they never materialize a table payload or retain a callable
// descriptor. Source and artifact execution must preserve that dropped state.
func TestRV64PublicDeclarativeElementGlobalInitializers(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, imported := range []bool{false, true} {
			for _, null := range []bool{false, true} {
				for _, artifact := range []bool{false, true} {
					t.Run(fmt.Sprintf("vector%t-imported%t-null%t-artifact%t", vector, imported, null, artifact), func(t *testing.T) {
						rt := NewRuntime()
						defer rt.Close()
						imports := NewImports()
						var producer *Instance
						var pc *Compiled
						if imported {
							if null {
								g, err := rt.NewFuncRefGlobal(FuncRef{}, false)
								if err != nil {
									t.Fatal(err)
								}
								defer g.Close()
								imports.Global("env", "ref", g)
							} else {
								pc = rv64TableStageCompile(t, fmt.Sprintf("declarative-element-global-producer-vector%t", vector), rv64NativeTokenModule(vector, true, true, false), artifact)
								producer = rv64NativeTokenInstantiate(t, rt, pc, NewImports())
								g, err := producer.ExportedGlobalObject("reference")
								if err != nil {
									t.Fatal(err)
								}
								imports.Global("env", "ref", g)
							}
						}
						key := fmt.Sprintf("declarative-element-global-vector%t-imported%t-null%t", vector, imported, null)
						c := rv64TableStageCompile(t, key, rv64ElementGlobalModule(vector, imported, false, null, 7, 0), artifact)
						view := c.executionView()
						if len(view.passiveElems) != 1 || view.passiveElems[0].Mode != ElemModeDeclarative || len(view.passiveElems[0].Values) != 0 || !view.requiredFeatures.IsEnabled(CoreFeatureExtendedConstExpressions) {
							t.Fatal("declarative global payload was retained or lost its feature contract")
						}
						in := rv64NativeTokenInstantiate(t, rt, c, imports)
						dropSource := rv64ElementGlobalModule(vector, imported, false, null, 7, 0)
						dropSource = rv64ReplaceTableStageSection(t, dropSource, 10, wasmtest.Section(10, wasmtest.Vec(
							wasmtest.Code([]byte{0x20, 0, 0x0b}),
							wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x11, 0, 0, 0x0b}),
							wasmtest.Code([]byte{0xfc, 13, 0, 0xfc, 13, 0, 0x41, 7, 0x0b}),
						)))
						dropC := rv64TableStageCompile(t, key+"-drop", dropSource, artifact)
						dropIn := rv64NativeTokenInstantiate(t, rt, dropC, imports)
						c.Close()
						dropC.Close()
						if producer != nil {
							producer.Close()
							pc.Close()
						}
						args, _ := rv64ElementGlobalArguments(vector)
						for index := uint64(0); index < 3; index++ {
							_, err := in.Invoke("call", append(slices.Clone(args), index)...)
							tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
						}
						beforeRoots := in.referenceLifetime().snapshot().ResourceRoots
						beforeDropRoots := dropIn.referenceLifetime().snapshot().ResourceRoots
						beforeTokens := len(rt.refStore.byToken)
						producerRoots := 0
						if producer != nil {
							producerRoots = producer.referenceLifetime().snapshot().ResourceRoots
						}
						for range 3 {
							importedGlobalInvoke(t, dropIn, "install", []uint64{7})
						}
						if in.referenceLifetime().snapshot().ResourceRoots != beforeRoots || dropIn.referenceLifetime().snapshot().ResourceRoots != beforeDropRoots || len(rt.refStore.byToken) != beforeTokens || producer != nil && producer.referenceLifetime().snapshot().ResourceRoots != producerRoots {
							t.Fatal("declarative global drop changed tokens or roots")
						}
						_, err := in.Invoke("install")
						tableTestExpectTrap(t, err, TrapTableOutOfBounds)
						if table := in.tableDescriptor(0); !bytes.Equal(table[8:], make([]byte, len(table)-8)) {
							t.Fatal("declarative global expression materialized table entries")
						}
						if table := dropIn.tableDescriptor(0); !bytes.Equal(table[8:], make([]byte, len(table)-8)) {
							t.Fatal("declarative global drop materialized table entries")
						}
						dropIn.Close()
						in.Close()
						if producer != nil && producer.hasPhysicalResources() {
							t.Fatal("declarative global retained producer after consumer close")
						}
					})
				}
			}
		}
	}
}

func TestRV64PublicUnusedDeclarativeElementGlobalInitializer(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("vector%t-artifact%t", vector, artifact), func(t *testing.T) {
				source := rv64ElementGlobalModule(vector, false, false, false, 7, 0)
				source = rv64ReplaceTableStageSection(t, source, 10, wasmtest.Section(10, wasmtest.Vec(
					wasmtest.Code([]byte{0x20, 0, 0x0b}),
					wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x11, 0, 0, 0x0b}),
					wasmtest.Code([]byte{0x41, 3, 0x0b}),
				)))
				c := rv64TableStageCompile(t, fmt.Sprintf("unused-declarative-element-global-vector%t", vector), source, artifact)
				if len(c.executionView().passiveElems) != 0 || !c.requiredFeatures.IsEnabled(CoreFeatureExtendedConstExpressions) {
					t.Fatal("unused declaration retained a directory or lost its feature contract")
				}
				rt := NewRuntime()
				defer rt.Close()
				in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
				c.Close()
				table := in.tableDescriptor(0)
				if in.jm.PassiveElemPtr() != 0 || !bytes.Equal(table[8:], make([]byte, len(table)-8)) {
					t.Fatal("unused declaration materialized runtime state")
				}
				importedGlobalInvoke(t, in, "install", []uint64{3})
				in.Close()
			})
		}
	}
}

// A reader must see construction-time copied references without a first native
// call through the writer to publish its imported-global descriptor owner.
func TestRV64PublicElementGlobalHostTableConstruction(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, flags := range []byte{4, 5} {
			for _, artifact := range []bool{false, true} {
				t.Run(fmt.Sprintf("vector%t-flags%d-artifact%t", vector, flags, artifact), func(t *testing.T) {
					rt := NewRuntime()
					defer rt.Close()
					table, err := NewTable(3, 3)
					if err != nil {
						t.Fatal(err)
					}
					defer table.Close()
					pc := rv64TableStageCompile(t, fmt.Sprintf("element-global-producer-vector%t", vector), rv64NativeTokenModule(vector, true, true, false), artifact)
					producer := rv64NativeTokenInstantiate(t, rt, pc, NewImports())
					g, err := producer.ExportedGlobalObject("reference")
					if err != nil {
						t.Fatal(err)
					}
					rc := rv64TableStageCompile(t, fmt.Sprintf("element-global-reader-vector%t", vector), rv64ReferenceGlobalModule(vector, true, false, true, 1), artifact)
					reader := rv64NativeTokenInstantiate(t, rt, rc, NewImports().Table("env", "table", table))
					c := rv64TableStageCompile(t, fmt.Sprintf("element-global-writer-vector%t-flags%d", vector, flags), rv64ElementGlobalModule(vector, true, true, false, flags, 0), artifact)
					writer := rv64NativeTokenInstantiate(t, rt, c, NewImports().Table("env", "table", table).Global("env", "ref", g))
					if flags == 5 {
						importedGlobalInvoke(t, writer, "install", []uint64{3})
					}
					args, want := rv64ElementGlobalArguments(vector)
					importedGlobalInvoke(t, reader, "call", want, append(args, 0)...)
					producer.Close()
					pc.Close()
					writer.Close()
					c.Close()
					importedGlobalInvoke(t, reader, "call", want, append(args, 1)...)
					if _, err := reader.Invoke("fill", 0, 3); err != nil {
						t.Fatal(err)
					}
					reader.Close()
					if err := table.Close(); err != nil {
						t.Fatal(err)
					}
					if producer.hasPhysicalResources() || writer.hasPhysicalResources() {
						t.Fatal("construction-time global roots remain")
					}
				})
			}
		}
	}
}

func TestRV64PublicElementGlobalInitializerRollback(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			pc := rv64TableStageCompile(t, "element-global-producer-vectorfalse", rv64NativeTokenModule(false, true, true, false), artifact)
			producer := rv64NativeTokenInstantiate(t, rt, pc, NewImports())
			g, err := producer.ExportedGlobalObject("reference")
			if err != nil {
				t.Fatal(err)
			}
			table, err := NewTable(3, 3)
			if err != nil {
				t.Fatal(err)
			}
			defer table.Close()
			c := rv64TableStageCompile(t, "element-global-out-of-bounds", rv64ElementGlobalModule(false, true, true, false, 4, 1), artifact)
			mod, err := rt.Module(c)
			if err != nil {
				t.Fatal(err)
			}
			defer mod.Close()
			before := producer.referenceLifetime().snapshot().ResourceRoots
			for range 2 {
				in, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports().Table("env", "table", table).Global("env", "ref", g)))
				if in != nil {
					in.Close()
				}
				if err == nil || in != nil {
					t.Fatal("out-of-bounds element initializer accepted")
				}
				if !bytes.Equal(table.desc[8:], make([]byte, len(table.desc)-8)) || table.owner.importers != 0 || g.owner.importers != 0 || c.codeCache.refs != 0 || producer.referenceLifetime().snapshot().ResourceRoots != before {
					t.Fatal("failed initializer wrote entries or retained attachments")
				}
			}
		})
	}
}

// Earlier active segments remain observable when a later initializer fails.
// Retain their actual raw owners before failed-construction roots unwind.
func TestRV64PublicElementGlobalPartialConstruction(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("vector%t-artifact%t", vector, artifact), func(t *testing.T) {
				rt := NewRuntime()
				defer rt.Close()
				pc := rv64TableStageCompile(t, fmt.Sprintf("element-global-producer-vector%t", vector), rv64NativeTokenModule(vector, true, true, false), artifact)
				producer := rv64NativeTokenInstantiate(t, rt, pc, NewImports())
				g, err := producer.ExportedGlobalObject("reference")
				if err != nil {
					t.Fatal(err)
				}
				table, err := NewTable(3, 3)
				if err != nil {
					t.Fatal(err)
				}
				defer table.Close()
				rc := rv64TableStageCompile(t, fmt.Sprintf("element-global-reader-vector%t", vector), rv64ReferenceGlobalModule(vector, true, false, true, 1), artifact)
				reader := rv64NativeTokenInstantiate(t, rt, rc, NewImports().Table("env", "table", table))
				source := rv64ElementGlobalModule(vector, true, true, false, 4, 0)
				source = rv64ReplaceTableStageSection(t, source, 9, wasmtest.Section(9, wasmtest.Vec(
					rv64ExpressionSegment(4, 0, []byte{0x23, 0, 0x0b}),
					rv64ExpressionSegment(4, 3, []byte{0x23, 1, 0x0b}),
				)))
				c := rv64TableStageCompile(t, fmt.Sprintf("element-global-partial-vector%t", vector), source, artifact)
				mod, err := rt.Module(c)
				if err != nil {
					t.Fatal(err)
				}
				defer mod.Close()
				for range 2 {
					in, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports().Table("env", "table", table).Global("env", "ref", g)))
					if in != nil {
						in.Close()
					}
					if err == nil || in != nil {
						t.Fatal("later out-of-bounds segment accepted")
					}
					if c.codeCache.refs != 0 || table.retained[producer] == nil {
						t.Fatal("failed writer retained instead of actual global descriptor owner")
					}
				}
				mod.Close()
				c.Close()
				producer.Close()
				pc.Close()
				args, want := rv64ElementGlobalArguments(vector)
				importedGlobalInvoke(t, reader, "call", want, append(args, 0)...)
				_, err = reader.Invoke("call", append(args, 1)...)
				tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
				if _, err := reader.Invoke("fill", 0, 3); err != nil {
					t.Fatal(err)
				}
				reader.Close()
				if err := table.Close(); err != nil {
					t.Fatal(err)
				}
				if producer.hasPhysicalResources() {
					t.Fatal("partial-construction raw owner retained after clear/close")
				}
			})
		}
	}
}
