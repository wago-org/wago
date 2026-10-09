//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func referenceOwnedTableDecode(t *testing.T, vector, tails, wide bool, start, target int) *wasm.Module {
	t.Helper()
	m, err := wasm.DecodeModule(wasmtest.ClosedReferenceOwnedTable(vector, tails, wide, start, target))
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func TestReferenceFunctionOwnedTableSource(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, tails := range []bool{false, true} {
			for _, wide := range []bool{false, true} {
				for _, start := range []int{0, 1, 2} {
					for _, target := range []int{0, 1, 2} {
						t.Run(fmt.Sprintf("vector%t-tails%t-wide%t-start%d-target%d", vector, tails, wide, start, target), func(t *testing.T) {
							m := referenceOwnedTableDecode(t, vector, tails, wide, start, target)
							if err := admitClosedReferenceFunctionModule(m, vector); err != nil {
								t.Fatal(err)
							}
							if has, err := admitLocalMutableFunctionGlobals(m); !has || err != nil {
								t.Fatal("owned table/private cells", has, err)
							}
							o := closedReferenceProductionOptions(tails, true)
							o.ImportBindings = hostImportBindings(3)
							image, err := CompileModuleWith(m, o)
							if err != nil || image == nil || !image.NativeReferenceFunctions || !image.NativeReferenceCalls || len(image.Entry) != 10 {
								t.Fatal("owned table production", err)
							}
							if vector && !tails {
								mode := closedReferenceMode(tails)
								mode.importTailCalls = tails
								sw, err := compileSWARDecodedModuleProfile(m, mode, swarModuleTables, swarStateProfile{publicStarts: true, exports: true, publicInitializers: true, importedMemory: true, indexedMemories: true, importedGlobals: true, importedTable: true, functionTableImports: true})
								if err != nil || !sw.publicTables || len(sw.functionKeys) != 13 {
									t.Fatal("sparse SWAR table directory", err)
								}
								for _, hole := range []int{4, 8, 9, 12} {
									if sw.functionKeys[hole] != 0 {
										t.Fatal("private SWAR callable key", hole)
									}
								}
							}
						})
					}
				}
			}
		}
	}
}
func TestReferenceFunctionOwnedTableConfinement(t *testing.T) {
	mutations := map[string]func(*wasm.Module){
		"import-table": func(m *wasm.Module) {
			m.Imports = append(m.Imports, wasm.Import{Type: wasm.NewTableExternType(m.Tables[0].Type)})
		},
		"export-table": func(m *wasm.Module) {
			m.Exports = append(m.Exports, wasm.Export{Name: "escape", Index: wasm.ExternIdx{Kind: wasm.ExternTable}})
		},
		"two-tables":          func(m *wasm.Module) { m.Tables = append(m.Tables, m.Tables[0]) },
		"table64":             func(m *wasm.Module) { m.Tables[0].Type.Limits.Addr64 = true },
		"inactive-max":        func(m *wasm.Module) { m.Tables[0].Type.Limits.HasMax = false },
		"capacity":            func(m *wasm.Module) { m.Tables[0].Type.Limits.Max = MaxOwnedTableEntries + 1 },
		"private-active":      func(m *wasm.Module) { m.Elements[0].Kind.Funcs[0] = 12 },
		"private-declarative": func(m *wasm.Module) { m.Elements[1].Kind.Funcs[0] = 12 },
		"missing-active":      func(m *wasm.Module) { m.Elements[0].Kind.Funcs[0] = MaxFunctions },
		"passive-offset":      func(m *wasm.Module) { m.Elements[0].Mode.Kind = wasm.ElemPassive },
		"expression-active": func(m *wasm.Module) {
			m.Elements[0].Kind = wasm.ElemKind{Kind: wasm.ElemFuncExprs, Exprs: []wasm.Expr{{BodyBytes: []byte{0xd2, 0, 0x0b}}}}
		},
		"global-offset":   func(m *wasm.Module) { m.Elements[0].Mode.Offset = wasm.Expr{BodyBytes: []byte{0x23, 0, 0x0b}} },
		"inactive-offset": func(m *wasm.Module) { m.Elements[1].Mode.Offset = wasm.Expr{BodyBytes: []byte{0x41, 0, 0x0b}} },
		"private-zero-init": func(m *wasm.Module) {
			m.Tables[0].Type.Limits.Min = 0
			m.Tables[0].Init = &wasm.Expr{BodyBytes: []byte{0xd2, 12, 0x0b}}
		},
		"missing-zero-init": func(m *wasm.Module) {
			m.Tables[0].Type.Limits.Min = 0
			m.Tables[0].Init = &wasm.Expr{BodyBytes: []byte{0xd2, 127, 0x0b}}
		},
		"nofunc-zero-init": func(m *wasm.Module) {
			m.Tables[0].Type.Limits.Min = 0
			m.Tables[0].Init = &wasm.Expr{BodyBytes: []byte{0xd0, 0x73, 0x0b}}
		},
	}
	for name, body := range map[string][]byte{
		"dead-private-indirect": {0, 0x11, 2, 0, 0x0b}, "dead-missing-indirect": {0, 0x11, 127, 0, 0x0b}, "dead-wrong-table": {0, 0x11, 0, 1, 0x0b},
		"dead-get-table": {0, 0x25, 1, 0x0b}, "dead-set-table": {0, 0x26, 1, 0x0b}, "dead-size-table": {0, 0xfc, 16, 1, 0x0b},
		"dead-indirect-tail": {0, 0x13, 0, 0, 0x0b}, "dead-reference-tail": {0, 0x15, 0, 0x0b},
		"dead-init": {0, 0xfc, 12, 127, 0, 0x0b}, "dead-drop": {0, 0xfc, 13, 127, 0x0b}, "dead-copy": {0, 0xfc, 14, 1, 0, 0x0b}, "dead-grow": {0, 0xfc, 15, 1, 0x0b}, "dead-fill": {0, 0xfc, 17, 1, 0x0b},
	} {
		mutations[name] = func(m *wasm.Module) { m.Code[0].BodyBytes = body }
	}
	for _, vector := range []bool{false, true} {
		for name, mutate := range mutations {
			t.Run(fmt.Sprintf("vector%t-%s", vector, name), func(t *testing.T) {
				m := referenceOwnedTableDecode(t, vector, true, true, 2, 0)
				mutate(m)
				o := closedReferenceProductionOptions(true, true)
				o.ImportBindings = hostImportBindings(3)
				pressure := 0
				o.MemoryPressure = func() { pressure++ }
				if image, err := CompileModuleWith(m, o); image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
					t.Fatal("unproven table reached publication", err, pressure)
				}
			})
		}
	}
}
func TestReferenceFunctionOwnedTableOperandFloors(t *testing.T) {
	numeric := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}
	private := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.FuncRef}}
	table := wasm.TableType{Ref: wasm.FuncRef.Ref(), Limits: wasm.Limits{Min: 1, Max: 1, HasMax: true}}
	state := &moduleState{production: true, referenceFunctions: true, referenceCalls: true, importReferences: true, foreignTableCalls: true, ownedTable: true,
		functions: []functionSpec{{signature: private}}, types: []functionSpec{{signature: numeric, params: []bool{false}, results: []bool{false}}}}
	ops := map[string][]byte{"indirect": {0x11, 0, 0, 0x1a}, "get": {0x25, 0, 0x1a}, "set": {0x26, 0}}
	for op, site := range ops {
		live := []byte{0x41, 0}
		unknown := []byte{0, 0x1b}
		badIndex := []byte{0, 0x42, 0}
		badRef := []byte{0, 0xd0, 0x70}
		if op == "indirect" {
			live = []byte{0x41, 3, 0x41, 0}
			badRef = []byte{0, 0xd0, 0x70, 0x41, 0}
		} else if op == "set" {
			live = []byte{0x41, 0, 0xd0, 0x70}
			badIndex = []byte{0, 0x42, 0, 0xd0, 0x70}
			badRef = []byte{0, 0x41, 0, 0x41, 0}
		}
		for _, tc := range []struct {
			name   string
			prefix []byte
			valid  bool
		}{
			{"live", live, true}, {"explicit-dead", []byte{0}, true}, {"unknown", unknown, true}, {"wide-index", badIndex, false}, {"wrong-class", badRef, false},
			{"inherited-floor", []byte{0, 2, 0x40}, false}, {"explicit-nested", []byte{0, 2, 0x40, 0}, true}, {"live-underflow", nil, false},
		} {
			t.Run(op+"-"+tc.name, func(t *testing.T) {
				body := append(append([]byte{}, tc.prefix...), site...)
				body = append(body, 0x0b)
				if tc.name == "inherited-floor" || tc.name == "explicit-nested" {
					body = append(body, 0x0b)
				}
				fn := wasm.Func{BodyBytes: body}
				m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Final: true, Comp: *numeric}}}, {SubTypes: []wasm.SubType{{Final: true, Comp: *private}}}}, FuncTypes: []wasm.TypeIdx{{Index: 1}}, Code: []wasm.Func{fn}, Tables: []wasm.Table{{Type: table}}}
				front := wasm.ValidateModuleWithWorkers(m, 1)
				if (front == nil) != tc.valid {
					t.Fatal("frontend oracle", front, tc.valid)
				}
				_, scalar := walkInteger(&fn, private, state, nil)
				_, swar := walkSWARPairsWithReferenceFunctionMode(&fn, private, memoryProfile{}, []*wasm.CompType{numeric, private}, []*wasm.CompType{private}, nil, 0, &table, []uint64{1, 2}, 0, 0, true, false, false, false, false, false, true, true, nil)
				if (scalar == nil) != tc.valid || (swar == nil) != tc.valid {
					t.Fatal("table floor disagrees with frontend", scalar, swar, front)
				}
			})
		}
	}
}
func TestReferenceFunctionOwnedTableBounds(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, capacity := range []uint64{0, MaxOwnedTableEntries, MaxOwnedTableEntries + 1} {
			t.Run(fmt.Sprintf("vector%t-cap%d", vector, capacity), func(t *testing.T) {
				m := referenceOwnedTableDecode(t, vector, false, false, 0, 0)
				m.Tables[0].Type.Limits = wasm.Limits{Min: capacity, Max: capacity, HasMax: true}
				m.Tables[0].Init = &wasm.Expr{BodyBytes: []byte{0xd2, 0, 0x0b}}
				err := admitClosedReferenceFunctionModule(m, vector)
				if (err == nil) != (capacity <= MaxOwnedTableEntries) {
					t.Fatal("capacity bound", err)
				}
			})
		}
	}
	for _, n := range []int{MaxOwnedTableEntries, MaxOwnedTableEntries + 1} {
		t.Run(fmt.Sprintf("values%d", n), func(t *testing.T) {
			m := referenceOwnedTableDecode(t, false, false, false, 0, 0)
			m.Elements[0].Kind.Funcs = make([]wasm.FuncIdx, n-len(m.Elements[1].Kind.Funcs))
			if err := admitClosedReferenceFunctionModule(m, false); (err == nil) != (n == MaxOwnedTableEntries) {
				t.Fatal("aggregate element bound", err)
			}
		})
	}
	for _, remaining := range []int{21 + 2*16 + 3, 21 + 2*16 + 2} {
		t.Run(fmt.Sprintf("bytes%d", remaining), func(t *testing.T) {
			m := referenceOwnedTableDecode(t, false, false, false, 0, 0)
			left := remaining
			if err := admitClosedReferenceOwnedTableState(m, &left); (err == nil) != (remaining == 56) {
				t.Fatal("table metadata budget", err, left)
			}
		})
	}
}

func TestReferenceFunctionOwnedTableDeadCountBoundary(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, n := range []int{maxDirectCalls, maxDirectCalls + 1} {
			t.Run(fmt.Sprintf("vector%t-count%d", vector, n), func(t *testing.T) {
				m := referenceOwnedTableDecode(t, vector, false, false, 0, 0)
				body := []byte{0}
				for range n {
					body = append(body, 0x11, 1, 0)
				}
				body = append(body, 0x0b)
				m.Code[0].BodyBytes = body
				o := closedReferenceProductionOptions(false, true)
				o.ImportBindings = hostImportBindings(3)
				pressure := 0
				o.MemoryPressure = func() { pressure++ }
				image, err := CompileModuleWith(m, o)
				if n == maxDirectCalls {
					if image == nil || err != nil || pressure != 1 {
						t.Fatal("exact indirect site bound", err, pressure)
					}
				} else if image != nil || err == nil || pressure != 0 {
					t.Fatal("indirect site bound plus one", err, pressure)
				}
			})
		}
	}
}

func TestReferenceFunctionOwnedTableAggregateBoundary(t *testing.T) {
	for _, extra := range []int{0, 1} {
		t.Run(fmt.Sprintf("extra%d", extra), func(t *testing.T) {
			m := &wasm.Module{
				Types:     []wasm.RecType{{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}}}}, {SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.FuncRef}, Results: []wasm.ValType{wasm.FuncRef}}}}}},
				FuncTypes: []wasm.TypeIdx{{Index: 0}, {Index: 1}}, Code: []wasm.Func{{BodyBytes: []byte{0x20, 0, 0x0b}, LocalDeclBytes: 1}, {BodyBytes: []byte{0x20, 0, 0x0b}, LocalDeclBytes: 1}},
				Tables:   []wasm.Table{{Type: wasm.TableType{Ref: wasm.FuncRef.Ref(), Limits: wasm.Limits{Min: 1, Max: 1, HasMax: true}}, Init: &wasm.Expr{BodyBytes: []byte{0xd2, 0, 0x0b}}}},
				Elements: []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemActive, Offset: wasm.Expr{BodyBytes: []byte{0x41, 0, 0x0b}}}, Kind: wasm.ElemKind{Kind: wasm.ElemFuncs, Funcs: []wasm.FuncIdx{0}}}},
				Globals:  []wasm.Global{{Type: wasm.GlobalType{Type: wasm.FuncRef, Mutable: true}, Init: wasm.Expr{BodyBytes: []byte{0xd2, 0, 0x0b}}}},
				Exports:  []wasm.Export{{Name: "id", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 0}}},
			}
			// Declared carriers14 + bodies/locals8 + table24 + element23 +
			// reference global5 + export name2 + passive data header4 =80 bytes.
			m.Data = []wasm.Data{{Mode: wasm.DataMode{Kind: wasm.DataPassive}, Init: make([]byte, MaxModuleBytes-80+extra)}}
			if err := admitClosedReferenceFunctionModule(m, false); (err == nil) != (extra == 0) {
				t.Fatal("combined source aggregate bound", extra, err)
			}
		})
	}
	for _, vector := range []bool{false, true} {
		t.Run(fmt.Sprintf("no-max-vector%t", vector), func(t *testing.T) {
			m := referenceOwnedTableDecode(t, vector, false, false, 0, 0)
			m.Tables[0].Type.Limits.HasMax = false
			m.Tables[0].Type.Limits.Max = 0
			if err := admitClosedReferenceFunctionModule(m, vector); err != nil {
				t.Fatal("raw no-max own table", err)
			}
		})
	}
}
