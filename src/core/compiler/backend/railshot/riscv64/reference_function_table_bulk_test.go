//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestReferenceFunctionTableBulkSource(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, tails := range []bool{false, true} {
			for _, wide := range []bool{false, true} {
				for _, start := range []int{0, 1, 2} {
					for _, target := range []int{0, 1, 2} {
						t.Run(fmt.Sprintf("vector%t-tails%t-wide%t-start%d-target%d", vector, tails, wide, start, target), func(t *testing.T) {
							m, err := wasm.DecodeModule(wasmtest.ClosedReferenceTableBulk(vector, tails, wide, start, target))
							if err != nil {
								t.Fatal(err)
							}
							o := closedReferenceProductionOptions(tails, true)
							o.ImportBindings = hostImportBindings(3)
							image, err := CompileModuleWith(m, o)
							if err != nil || image == nil || !image.NativeReferenceFunctions || !image.NativeReferenceCalls {
								t.Fatal("private bulk production", err)
							}
						})
					}
				}
			}
		}
	}
}

func TestReferenceFunctionTableBulkOperandFloors(t *testing.T) {
	private := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.FuncRef}}
	table := wasm.TableType{Ref: wasm.FuncRef.Ref(), Limits: wasm.Limits{Min: 1, Max: 4, HasMax: true}}
	state := &moduleState{production: true, referenceFunctions: true, referenceCalls: true, importReferences: true, foreignTableCalls: true, ownedTable: true, ownedTableGrowth: true, elementCount: 1, functions: []functionSpec{{signature: private}}}
	for _, op := range []string{"init", "copy", "fill", "grow"} {
		site := map[string][]byte{"init": {0xfc, 12, 0, 0}, "copy": {0xfc, 14, 0, 0}, "fill": {0xfc, 17, 0}, "grow": {0xfc, 15, 0, 0x1a}}[op]
		live, wrongWidth, wrongRef := []byte{0x41, 0, 0x41, 0, 0x41, 0}, []byte{0, 0x42, 0}, []byte{0, 0xd0, 0x70}
		if op == "fill" {
			live = []byte{0x41, 0, 0xd0, 0x70, 0x41, 0}
			wrongRef = []byte{0, 0x41, 0, 0x41, 0}
		}
		if op == "grow" {
			live = []byte{0xd0, 0x70, 0x41, 0}
			wrongRef = []byte{0, 0x41, 0, 0x41, 0}
		}
		for _, tc := range []struct {
			name   string
			prefix []byte
			valid  bool
		}{
			{"live", live, true}, {"explicit-dead", []byte{0}, true}, {"unknown", []byte{0, 0x1b}, true}, {"wide-count", wrongWidth, false}, {"wrong-class", wrongRef, false},
			{"inherited-floor", []byte{0, 2, 0x40}, false}, {"explicit-nested", []byte{0, 2, 0x40, 0}, true}, {"live-underflow", nil, false},
		} {
			t.Run(op+"-"+tc.name, func(t *testing.T) {
				body := append(append([]byte{}, tc.prefix...), site...)
				body = append(body, 0x0b)
				if tc.name == "inherited-floor" || tc.name == "explicit-nested" {
					body = append(body, 0x0b)
				}
				fn := wasm.Func{BodyBytes: body}
				m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Final: true, Comp: *private}}}}, FuncTypes: []wasm.TypeIdx{{Index: 0}}, Code: []wasm.Func{fn}, Tables: []wasm.Table{{Type: table}}, Elements: []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemPassive}, Kind: wasm.ElemKind{Kind: wasm.ElemFuncs}}}}
				front := wasm.ValidateModuleWithWorkers(m, 1)
				if (front == nil) != tc.valid {
					t.Fatal("frontend oracle", front, tc.valid)
				}
				_, scalar := walkInteger(&fn, private, state, nil)
				_, swar := walkSWARPairsWithReferenceFunctionMode(&fn, private, memoryProfile{}, []*wasm.CompType{private}, []*wasm.CompType{private}, nil, 0, &table, []uint64{0}, 1, 0, true, false, false, false, false, false, true, true, nil)
				if (scalar == nil) != tc.valid || (swar == nil) != tc.valid {
					t.Fatal("bulk floor disagrees with frontend", scalar, swar, front)
				}
			})
		}
	}
}

func TestReferenceFunctionTableBulkAggregateBoundary(t *testing.T) {
	for _, extra := range []int{0, 1} {
		t.Run(fmt.Sprintf("extra%d", extra), func(t *testing.T) {
			m := &wasm.Module{
				Types:     []wasm.RecType{{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}}}}, {SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.FuncRef}, Results: []wasm.ValType{wasm.FuncRef}}}}}},
				FuncTypes: []wasm.TypeIdx{{Index: 0}, {Index: 1}}, Code: []wasm.Func{{BodyBytes: []byte{0x20, 0, 0x0b}, LocalDeclBytes: 1}, {BodyBytes: []byte{0x20, 0, 0x0b}, LocalDeclBytes: 1}},
				Tables:   []wasm.Table{{Type: wasm.TableType{Ref: wasm.FuncRef.Ref(), Limits: wasm.Limits{Min: 1, Max: 1, HasMax: true}}, Init: &wasm.Expr{BodyBytes: []byte{0xd2, 0, 0x0b}}}},
				Elements: []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemActive, Offset: wasm.Expr{BodyBytes: []byte{0x41, 0, 0x0b}}}, Kind: wasm.ElemKind{Kind: wasm.ElemFuncs, Funcs: []wasm.FuncIdx{0}}}, {Mode: wasm.ElemMode{Kind: wasm.ElemPassive}, Kind: wasm.ElemKind{Kind: wasm.ElemFuncs, Funcs: []wasm.FuncIdx{0, 0, 0}}}},
				Globals:  []wasm.Global{{Type: wasm.GlobalType{Type: wasm.FuncRef, Mutable: true}, Init: wasm.Expr{BodyBytes: []byte{0xd2, 0, 0x0b}}}},
				Exports:  []wasm.Export{{Name: "id", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 0}}},
			}
			// Declared carriers14 + bodies/locals8 + table24 + active23 +
			// passive28 + reference global5 + export2 + passive data header4 =108.
			m.Data = []wasm.Data{{Mode: wasm.DataMode{Kind: wasm.DataPassive}, Init: make([]byte, MaxModuleBytes-108+extra)}}
			if err := admitClosedReferenceFunctionModule(m, false); (err == nil) != (extra == 0) {
				t.Fatal("complete source state/passive aggregate", extra, err)
			}
		})
	}
	// A selected no-table private module cannot enable element operations.
	m, err := wasm.DecodeModule(wasmtest.ClosedReferenceNumericCalls(false, false, false, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	m.Code[0].BodyBytes = []byte{0, 0xfc, 13, 0, 0x0b}
	if err := admitClosedReferenceFunctionModule(m, false); !errors.Is(err, ErrUnsupported) {
		t.Fatal("selected no-table elem.drop", err)
	}
}

func TestReferenceFunctionTableBulkConfinementAndReservation(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, tc := range []struct {
			name   string
			mutate func(*wasm.Module)
			valid  bool
		}{
			{"dead-init-table", func(m *wasm.Module) { m.Code[0].BodyBytes = []byte{0, 0xfc, 12, 2, 1, 0x0b} }, false},
			{"dead-copy-source", func(m *wasm.Module) { m.Code[0].BodyBytes = []byte{0, 0xfc, 14, 0, 1, 0x0b} }, false},
			{"passive-private", func(m *wasm.Module) { m.Elements[2].Kind.Funcs[0] = 12 }, false},
			{"passive-missing", func(m *wasm.Module) { m.Elements[2].Kind.Funcs[0] = 127 }, false},
			{"passive-offset", func(m *wasm.Module) { m.Elements[2].Mode.Offset.BodyBytes = []byte{0x41, 0, 0x0b} }, false},
			{"combined1024", func(m *wasm.Module) { m.Elements[2].Kind.Funcs = make([]wasm.FuncIdx, 1024-9) }, true},
			{"combined1025", func(m *wasm.Module) { m.Elements[2].Kind.Funcs = make([]wasm.FuncIdx, 1025-9) }, false},
			{"no-max512-dead-grow", func(m *wasm.Module) {
				m.Tables[0].Type.Limits = wasm.Limits{Min: 512}
				m.Code[0].BodyBytes = []byte{0, 0xfc, 15, 0, 0x1a, 0x0b}
			}, true},
			{"no-max513-dead-grow", func(m *wasm.Module) {
				m.Tables[0].Type.Limits = wasm.Limits{Min: 513}
				m.Code[0].BodyBytes = []byte{0, 0xfc, 15, 0, 0x1a, 0x0b}
			}, false},
			{"no-max513-without-grow", func(m *wasm.Module) { m.Tables[0].Type.Limits = wasm.Limits{Min: 513} }, true},
		} {
			t.Run(fmt.Sprintf("vector%t-%s", vector, tc.name), func(t *testing.T) {
				var m *wasm.Module
				var err error
				if tc.name == "no-max513-without-grow" {
					m = referenceOwnedTableDecode(t, vector, false, false, 0, 0)
				} else {
					m, err = wasm.DecodeModule(wasmtest.ClosedReferenceTableBulk(vector, false, false, 0, 0))
					if err != nil {
						t.Fatal(err)
					}
				}
				tc.mutate(m)
				if err = admitClosedReferenceFunctionModule(m, vector); (err == nil) != tc.valid {
					t.Fatal("complete source prerequisite", err)
				}
				o := closedReferenceProductionOptions(false, true)
				o.ImportBindings = hostImportBindings(3)
				pressure := 0
				o.MemoryPressure = func() { pressure++ }
				image, err := CompileModuleWith(m, o)
				if tc.valid {
					if image == nil || err != nil || pressure != 1 {
						t.Fatal("valid bounded source", err, pressure)
					}
				} else if image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
					t.Fatal("invalid source reached publication", err, pressure)
				}
			})
		}
	}
}
