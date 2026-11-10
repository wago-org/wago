//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestReferenceFunctionImmutableCellsSource(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, wide := range []bool{false, true} {
			for _, start := range []int{0, 1, 2} {
				for _, target := range []int{0, 1, 2} {
					t.Run(fmt.Sprintf("v%t-w%t-s%d-target%d", vector, wide, start, target), func(t *testing.T) {
						m, err := wasm.DecodeModule(wasmtest.ClosedReferenceImmutableCells(vector, wide, start, target))
						if err != nil {
							t.Fatal(err)
						}
						if err := wasm.ValidateModuleWithFeaturesAndWorkers(m, wasm.ValidationFeatures{MultiMemory: true}, 1); err != nil {
							t.Fatal("frontend", err)
						}
						if m.Globals[len(m.Globals)-1].Type.Mutable {
							t.Fatal("fixture cell mutable")
						}
						o := closedReferenceProductionOptions(true, true)
						o.ImportBindings = hostImportBindings(3)
						image, err := CompileModuleWith(m, o)
						if err != nil || image == nil || !image.NativeReferenceFunctions || image.NativeTail == nil {
							t.Fatal("immutable private cell dispatch", err)
						}
					})
				}
			}
		}
		for _, null := range []bool{false, true} {
			t.Run(fmt.Sprintf("v%t-bare-null%t", vector, null), func(t *testing.T) {
				m, err := wasm.DecodeModule(wasmtest.ClosedReferenceImmutableCellsBare(vector, null))
				if err != nil {
					t.Fatal(err)
				}
				if err := wasm.ValidateModuleWithWorkers(m, 1); err != nil {
					t.Fatal(err)
				}
				image, err := CompileModuleWith(m, closedReferenceProductionOptions(false, false))
				if err != nil || image == nil || !image.NativeReferenceFunctions || image.NativeReferenceCalls || image.NativeReferenceControl || image.NativeTail != nil {
					t.Fatal("immutable-only untyped contract", err)
				}
			})
		}
	}
}

func TestReferenceFunctionImmutableCellsConfinement(t *testing.T) {
	mutations := map[string]func(*wasm.Module){
		"private": func(m *wasm.Module) { m.Globals[0].Init.BodyBytes = []byte{0xd2, 1, 0x0b} },
		"missing": func(m *wasm.Module) { m.Globals[0].Init.BodyBytes = []byte{0xd2, 127, 0x0b} },
		"global-copy": func(m *wasm.Module) {
			m.Globals = append(m.Globals, m.Globals[0])
			m.Globals[1].Init.BodyBytes = []byte{0x23, 0, 0x0b}
		},
		"indexed-null": func(m *wasm.Module) { m.Globals[0].Init.BodyBytes = []byte{0xd0, 0, 0x0b} },
		"structured":   func(m *wasm.Module) { m.Globals[0].Init.Instrs = []wasm.Instruction{{Kind: wasm.InstrRefNull}} },
		"export": func(m *wasm.Module) {
			m.Exports = append(m.Exports, wasm.Export{Name: "cell", Index: wasm.ExternIdx{Kind: wasm.ExternGlobal}})
		},
		"import": func(m *wasm.Module) {
			m.Imports = append(m.Imports, wasm.Import{Type: wasm.NewGlobalExternType(wasm.GlobalType{Type: wasm.FuncRef})})
		},
		"wrong-type":         func(m *wasm.Module) { m.Globals[0].Type.Type = wasm.ExternRef },
		"live-set":           func(m *wasm.Module) { m.Code[1].BodyBytes = []byte{0x20, 0, 0x24, 0, 0x41, 0, 0x0b} },
		"dead-set":           func(m *wasm.Module) { m.Code[1].BodyBytes = []byte{0, 0x24, 0, 0x0b} },
		"dead-inherited-set": func(m *wasm.Module) { m.Code[1].BodyBytes = []byte{0, 2, 0x40, 0x24, 0, 0x0b, 0x0b} },
	}
	for _, vector := range []bool{false, true} {
		for name, mutate := range mutations {
			t.Run(fmt.Sprintf("v%t-%s", vector, name), func(t *testing.T) {
				m, err := wasm.DecodeModule(wasmtest.ClosedReferenceImmutableCellsBare(vector, false))
				if err != nil {
					t.Fatal(err)
				}
				mutate(m)
				// A sibling mutable cell must not authorize immutable global.set.
				m.Globals = append(m.Globals, wasm.Global{Type: wasm.GlobalType{Type: wasm.FuncRef, Mutable: true}, Init: wasm.Expr{BodyBytes: []byte{0xd0, 0x70, 0x0b}}})
				checkPublicPrimitiveModule(t, m, closedReferenceProductionOptions(false, true))
			})
		}
	}
}

func TestReferenceFunctionImmutableCellsSetFloors(t *testing.T) {
	ft := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.FuncRef}}
	state := &moduleState{production: true, referenceFunctions: true, importReferences: true, localMutableReferences: true, globals: []globalSpec{{wide: true, typ: wasm.FuncRef}, {wide: true, typ: wasm.FuncRef, mutable: true}}}
	globals := []wasm.GlobalType{{Type: wasm.FuncRef}, {Type: wasm.FuncRef, Mutable: true}}
	for name, body := range map[string][]byte{"live": {0x20, 0, 0x24, 0, 0x0b}, "dead": {0, 0x24, 0, 0x0b}, "inherited": {0, 2, 0x40, 0x24, 0, 0x0b, 0x0b}, "explicit-nested": {0, 2, 0x40, 0, 0x24, 0, 0x0b, 0x0b}} {
		t.Run(name, func(t *testing.T) {
			fn := &wasm.Func{BodyBytes: body}
			m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Final: true, Comp: *ft}}}}, FuncTypes: []wasm.TypeIdx{{}}, Code: []wasm.Func{*fn}, Globals: []wasm.Global{{Type: globals[0], Init: wasm.Expr{BodyBytes: []byte{0xd0, 0x70, 0x0b}}}, {Type: globals[1], Init: wasm.Expr{BodyBytes: []byte{0xd0, 0x70, 0x0b}}}}}
			if err := wasm.ValidateModuleWithWorkers(m, 1); err == nil {
				t.Fatal("frontend admitted immutable set")
			}
			_, scalar := walkInteger(fn, ft, state, nil)
			_, swar := walkSWARPairsWithReferenceFunctionMode(fn, ft, memoryProfile{}, []*wasm.CompType{ft}, []*wasm.CompType{ft}, globals, 0, nil, nil, 0, 0, true, false, false, true, false, false, false, true, nil)
			if !errors.Is(scalar, ErrUnsupported) || !errors.Is(swar, ErrUnsupported) {
				t.Fatal("immutable set under mutable permission", scalar, swar)
			}
		})
	}
}

func TestReferenceFunctionImmutableCellsBounds(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, extra := range []int{0, 1} {
			t.Run(fmt.Sprintf("v%t-bytes+%d", vector, extra), func(t *testing.T) {
				m, err := wasm.DecodeModule(wasmtest.ClosedReferenceImmutableCellsBare(vector, true))
				if err != nil {
					t.Fatal(err)
				}
				// Types8 + bodies23 + locals3 + global5 + export3 + data4 =46.
				fixed := 46
				if vector {
					fixed += 6
				}
				m.Data = []wasm.Data{{Mode: wasm.DataMode{Kind: wasm.DataPassive}, Init: make([]byte, MaxModuleBytes-fixed+extra)}}
				if err := admitClosedReferenceFunctionModule(m, vector); (err == nil) != (extra == 0) {
					t.Fatal("complete immutable source aggregate", err)
				}
			})
		}
	}
	for _, count := range []int{256, 257} {
		t.Run(fmt.Sprintf("globals%d", count), func(t *testing.T) {
			m, err := wasm.DecodeModule(wasmtest.ClosedReferenceImmutableCellsBare(false, true))
			if err != nil {
				t.Fatal(err)
			}
			g := m.Globals[0]
			m.Globals = make([]wasm.Global, count)
			for i := range m.Globals {
				m.Globals[i] = g
			}
			if err := admitClosedReferenceFunctionModule(m, false); (err == nil) != (count == 256) {
				t.Fatal("immutable source global cap", err)
			}
		})
	}
}
