//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// No globals consume the funcref exact type: the table alone uses its pool row.
// Action0 restores a saved reference after clearing its table source; actions
// 1/2/3/4 exercise null/signature/stack/index guards in the same private frame.
func rv64ReferenceOwnedTableTrapModule(vector bool) []byte {
	types := [][]byte{wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.FuncRef, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, nil)}
	functions := []byte{5, 0, 1, 2, 3, 0}
	private := []byte{1, 1, 0x70, 0x20, 1, 0x45, 0x04, 0x40, 0x20, 1, 0x20, 0, 0x26, 0,
		0x20, 1, 0x25, 0, 0x21, 3, 0x20, 1, 0xd0, 0x70, 0x26, 0, 0x20, 1, 0x20, 3, 0x26, 0, 0x0b,
		0x20, 2, 0x20, 1, 0x11, 0, 0, 0x0b}
	private = append(wasmtest.ULEB(uint32(len(private))), private...)
	bodies := [][]byte{wasmtest.Code([]byte{0x20, 0, 0x0b}), wasmtest.Code([]byte{0xd2, 0, 0x20, 0, 0x20, 1, 0x10, 2, 0x0b}), private, wasmtest.Code([]byte{0x0b}), wasmtest.Code([]byte{0x20, 0, 0x41, 3, 0x11, 0, 0, 0x0b})}
	if vector {
		types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}))
		functions = []byte{6, 0, 1, 2, 3, 0, 4}
		bodies = append(bodies, wasmtest.Code([]byte{0x20, 0, 0x0b}))
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, functions), wasmtest.Section(4, []byte{1, 0x70, 1, 4, 4}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(9, []byte{3, 0, 0x41, 0, 0x0b, 1, 0, 0, 0x41, 2, 0x0b, 1, 3, 0, 0x41, 3, 0x0b, 1, 4}), wasmtest.Section(10, wasmtest.Vec(bodies...)))
}
func TestRV64PublicReferenceFunctionOwnedTablePoolTrapsReuse(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-vector%t", artifact, vector), func(t *testing.T) {
				c := rv64NativeTailCompile(t, fmt.Sprintf("reference-owned-table-traps-v%t", vector), rv64ReferenceOwnedTableTrapModule(vector), artifact)
				if len(c.Globals) != 0 || len(c.ValueTypes) != 1 || !c.TableHasValueType || c.TableValueTypeIndex != 0 || !c.nativeReferenceFunctionABI || c.nativeReferenceCallABI {
					t.Fatal("table-only exact pool/call witness", c.ValueTypes)
				}
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
				defer rv64HostCertificateCloseRuntime(t, rt)
				in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
				rv64RequirePublishedLocalFunction(t, in, 2)
				for _, action := range []uint64{0, 1, 0, 2, 0, 3, 0, 4, 0, 1, 0, 2, 0} {
					got, err := in.Invoke("run", 0xdeadbeef00000000|action, 0xabad1dea80000001)
					if action == 0 {
						if err != nil || !slices.Equal(got, []uint64{0x80000001}) {
							t.Fatal("normalized table index/argument/restored reference", got, err)
						}
					} else {
						code := coreruntime.TrapIndirectOutOfBounds
						if action == 2 {
							code = coreruntime.TrapIndirectWrongSig
						} else if action == 3 {
							code = coreruntime.TrapStackFenceBreached
						}
						trap := ownedTableTrap(t, err, code)
						if action != 3 && (len(trap.Frames) == 0 || trap.Frames[0].FunctionIndex != 2 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 37) {
							t.Fatal("private indirect trap source", trap.Frames)
						}
					}
					state := &in.pluginState.Load().railshotInvocation
					if state.tableDepth != 0 || state.tailGenerationBudget.count != 0 {
						t.Fatal("table private trap leaked owner scope")
					}
				}
				if allocs := testing.AllocsPerRun(10, func() {
					got, err := in.Invoke("run", 0x8000000000000000, 1)
					if err != nil || got[0] != 1 {
						panic("warm private indirect")
					}
				}); allocs != 0 {
					t.Fatal("warm private own indirect allocation", allocs)
				}
			})
		}
	}
}
func TestRV64PublicReferenceFunctionOwnedTableRetainedAdmission(t *testing.T) {
	c := rv64NativeTailCompile(t, "reference-owned-table-admission", rv64ReferenceOwnedTableTrapModule(false), false)
	mutations := map[string]func(*Compiled){
		"export-ingress":         func(v *Compiled) { v.tableExports = map[string]int{"escape": 0} },
		"legacy-export-fallback": func(v *Compiled) { v.hasTableExportMetadata = false },
		"import-ingress":         func(v *Compiled) { v.tableImport = "env.table" },
		"second-table":           func(v *Compiled) { v.extraTables = []tableDef{{Size: 1, Type: ValFuncRef}} },
		"private-init-zero":      func(v *Compiled) { v.TableSize = 0; v.HasTableInitFunc = true; v.TableInitFunc = 2; v.Elems = nil },
		"missing-init-zero":      func(v *Compiled) { v.TableSize = 0; v.HasTableInitFunc = true; v.TableInitFunc = 127; v.Elems = nil },
		"inactive-init":          func(v *Compiled) { v.TableInitFunc = 1 },
		"private-element":        func(v *Compiled) { v.Elems[0].Values[0].FuncIndex = 2 },
		"missing-element":        func(v *Compiled) { v.Elems[0].Values[0].FuncIndex = 127 },
		"expression-unbound-type": func(v *Compiled) {
			v.Elems[0].HasValueType = true
			v.Elems[0].ValueTypeIndex = uint32(len(v.ValueTypes))
		},
		"null-element":         func(v *Compiled) { v.Elems[0].Values[0] = RefInit{Null: true} },
		"global-element":       func(v *Compiled) { v.Elems[0].Values[0] = RefInit{HasGlobal: true} },
		"unused-element-type":  func(v *Compiled) { v.Elems[0].ValueTypeIndex = 1 },
		"unused-offset-global": func(v *Compiled) { v.Elems[0].Offset.Global = 1 },
		"offset-expression":    func(v *Compiled) { v.Elems[0].Offset.Expr = []byte{0x41, 0, 0x0b} },
		"table64":              func(v *Compiled) { v.TableAddr64 = true },
		"capacity":             func(v *Compiled) { v.TableMax = 1025 },
		"pool-missing":         func(v *Compiled) { v.TableHasValueType = false },
		"pool-unbound":         func(v *Compiled) { v.TableValueTypeIndex = 127 },
		"pool-unused":          func(v *Compiled) { v.ValueTypes = append(v.ValueTypes, ValueTypeDescriptor{Kind: ValueTypeI32}) },
		"pool-duplicate":       func(v *Compiled) { v.ValueTypes = append(v.ValueTypes, v.ValueTypes[0]) },
		"passive-ingress":      func(v *Compiled) { v.passiveElems = []ElemInit{{Mode: ElemModePassive, Offset: OffsetInit{Base: 1}}} },
		"dropped-payload": func(v *Compiled) {
			v.passiveElems = []ElemInit{{Mode: ElemModeDeclarative, Values: []RefInit{{FuncIndex: 0}}}}
		},
		"oversized-actual-params":  func(v *Compiled) { v.Funcs[0].Params = make([]ValType, 1<<20) },
		"oversized-actual-results": func(v *Compiled) { v.Funcs[0].Results = make([]ValType, 1<<20) },
		"missing-private":          func(v *Compiled) { v.nativeReferenceFunctionABI = false },
		"missing-reference":        func(v *Compiled) { v.nativeTableReferenceABI = false },
		"missing-context":          func(v *Compiled) { v.nativeTableContextABI = false },
		"numeric-key":              func(v *Compiled) { v.FuncTypeID[0] = 1 },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			v := cloneCompiledMetadata(c)
			v.codeCache = nil
			v.validateMemo = &validateMemo{}
			v.ensureCodeCache()
			mutate(v)
			if err := railshotValidateCompiled(v); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("bad private own table accepted", err)
			}
			blob, err := marshalCompiled(v)
			if err != nil {
				return
			}
			loaded, err := LoadTrustedArtifact(blob)
			if loaded != nil {
				defer loaded.Close()
			}
			if err == nil {
				// Existing canonical writing repairs inactive fields and always
				// writes exact table-export metadata. Direct invalid metadata was
				// rejected above; these repairs close the ingress rather than open it.
				repaired := name == "inactive-init" || name == "unused-element-type" || name == "legacy-export-fallback"
				if !repaired || !loaded.hasTableExportMetadata || loaded.TableInitFunc != 0 || loaded.Elems[0].ValueTypeIndex != 0 || rv64ValidateReferenceFunctions(loaded) != nil {
					t.Fatal("bad own table artifact accepted", name)
				}
			}
		})
	}
	cold := testing.AllocsPerRun(5, func() {
		remaining := railshot.MaxModuleBytes
		if err := rv64ValidateReferenceFunctionTable(c, &remaining); err != nil {
			panic(err)
		}
	})
	t.Logf("cold table structural validation allocations: %g", cold)
	rv64RequireCachedValidationNoAllocations(t, c)

	rt := NewRuntime()
	defer rt.Close()
	mod, err := rt.Module(c)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	in, err := rt.Instantiate(context.Background(), mod)
	if in == nil || err != nil {
		t.Fatal("public table rejected", err)
	}
	defer in.Close()
	if got, err := in.Invoke("run", 0, 42); err != nil || !slices.Equal(got, []uint64{42}) {
		t.Fatal("public table execution", got, err)
	}
}

func rv64ReferenceOwnedTableLiteralModule(vector bool, expr []byte, size, capacity uint32, emptyOOB, noMax bool) []byte {
	types := [][]byte{wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{wasm.FuncRef})}
	functions := []byte{2, 0, 1}
	bodies := [][]byte{wasmtest.Code([]byte{0x20, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x0b})}
	if vector {
		types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}))
		functions = []byte{3, 0, 1, 2}
		bodies = append(bodies, wasmtest.Code([]byte{0x20, 0, 0x0b}))
	}
	table := append([]byte{1, 0x40, 0, 0x70, 1}, wasmtest.ULEB(size)...)
	if noMax {
		table = append([]byte{1, 0x40, 0, 0x70, 0}, wasmtest.ULEB(size)...)
	} else {
		table = append(table, wasmtest.ULEB(capacity)...)
	}
	table = append(table, expr...)
	elements := []byte{1, 3, 0, 1, 0}
	if emptyOOB {
		elements = append(append([]byte{1, 0, 0x41}, wasmtest.SLEB32(int32(size+1))...), 0x0b, 0)
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, functions), wasmtest.Section(4, table), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("identity", 0, 0))), wasmtest.Section(9, elements), wasmtest.Section(10, wasmtest.Vec(bodies...)))
}
func TestRV64PublicReferenceFunctionOwnedTableLiteralLimits(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, size := range []uint32{0, 1024} {
				for _, null := range []bool{false, true} {
					t.Run(fmt.Sprintf("artifact%t-vector%t-size%d-null%t", artifact, vector, size, null), func(t *testing.T) {
						expr := []byte{0xd2, 0, 0x0b}
						if null {
							expr = []byte{0xd0, 0x70, 0x0b}
						}
						c := rv64NativeTailCompile(t, fmt.Sprintf("reference-owned-table-literal-v%t-size%d-null%t", vector, size, null), rv64ReferenceOwnedTableLiteralModule(vector, expr, size, size, false, false), artifact)
						rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
						defer rv64HostCertificateCloseRuntime(t, rt)
						in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
						table := privateTableBytes(in)
						if binary.LittleEndian.Uint32(table) != size {
							t.Fatal("table initializer size")
						}
						for index := uint32(0); index < size; index++ {
							entry := table[8+index*32 : 8+(index+1)*32]
							if null {
								if !bytes.Equal(entry, make([]byte, 32)) {
									t.Fatal("table null init")
								}
							} else {
								ref := binary.LittleEndian.Uint64(entry[24:])
								if got, ok := in.funcrefDescriptorIndex(ref); !ok || got != 0 || ref>>32 == 0 {
									t.Fatal("table numeric literal init", index, ref)
								}
							}
						}
					})
				}
			}
		}
	}
	for _, vector := range []bool{false, true} {
		t.Run(fmt.Sprintf("empty-oob-vector%t", vector), func(t *testing.T) {
			c := rv64NativeTailCompile(t, "reference-owned-table-empty-oob", rv64ReferenceOwnedTableLiteralModule(vector, []byte{0xd0, 0x70, 0x0b}, 0, 0, true, false), false)
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, rt)
			mod, err := rt.Module(c)
			if err != nil {
				t.Fatal(err)
			}
			in, err := rt.Instantiate(context.Background(), mod)
			if in != nil {
				in.Close()
			}
			if in != nil || err == nil {
				t.Fatal("empty out-of-bounds active segment accepted", err)
			}
			mod.Close()
			c.Close()
			if !c.loadCodeCache().closed {
				t.Fatal("failed table constructor retained code")
			}
		})
	}
}

func TestRV64PublicReferenceFunctionOwnedTableNoMax(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-vector%t", artifact, vector), func(t *testing.T) {
				c := rv64NativeTailCompile(t, fmt.Sprintf("reference-owned-table-no-max-v%t", vector), rv64ReferenceOwnedTableLiteralModule(vector, []byte{0xd2, 0, 0x0b}, 4, 0, false, true), artifact)
				if c.TableHasMax || c.TableSize != 4 || c.TableMax < 4 || c.TableMax > 1024 {
					t.Fatal("retained reserved no-max capacity", c.TableSize, c.TableMax, c.TableHasMax)
				}
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
				defer rv64HostCertificateCloseRuntime(t, rt)
				in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
				table := privateTableBytes(in)
				if binary.LittleEndian.Uint32(table) != 4 || in.tableDescLen != 8+int(c.TableMax)*32 {
					t.Fatal("reserved no-max physical table capacity", in.tableDescLen, c.TableMax)
				}
			})
		}
	}
}
func TestRV64PublicReferenceFunctionOwnedTableAggregateBoundary(t *testing.T) {
	c := rv64NativeTailCompile(t, "reference-owned-table-budget", rv64ReferenceOwnedTableLiteralModule(false, []byte{0xd2, 0, 0x0b}, 1, 1, false, false), false)
	for _, extra := range []int{0, 1} {
		t.Run(fmt.Sprintf("extra%d", extra), func(t *testing.T) {
			v := cloneCompiledMetadata(c)
			v.codeCache = nil
			v.validateMemo = &validateMemo{}
			v.Globals = []GlobalDef{{Type: ValFuncRef, Mutable: true, HasValueType: true, ValueTypeIndex: 0}}
			v.Elems = []ElemInit{{Mode: ElemModeActive, RefType: ValFuncRef, Values: []RefInit{{FuncIndex: 0}}}}
			v.passiveElems = []ElemInit{{Mode: ElemModeActive, RefType: ValFuncRef}}
			// Table26 + active24 + dropped16 + reference global18 + declared
			// carriers14 + export name8 + passive data header4 =110 bytes.
			v.PassiveData = []PassiveDataInit{{Bytes: make([]byte, railshot.MaxModuleBytes-110+extra)}}
			if err := rv64ValidateReferenceFunctions(v); (err == nil) != (extra == 0) {
				t.Fatal("combined retained aggregate bound", extra, err)
			}
		})
	}
}
