//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func localMutableReferenceSource(vector bool) []byte {
	types := [][]byte{wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, nil)}
	if vector {
		types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.V128}, nil))
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, []byte{2, 0, 1}),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.FuncRef, true, []byte{0xd0, 0x70, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("set", 0, 0), wasmtest.ExportEntry("target", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd2, 1, 0x24, 0, 0x23, 0, 0xd1, 0x0b}), wasmtest.Code([]byte{0x0b}))),
	)
}

func TestLocalMutableReferenceSourceAdmission(t *testing.T) {
	for _, vector := range []bool{false, true} {
		m := localTailDecode(t, localMutableReferenceSource(vector))
		if ok, err := admitLocalMutableFunctionGlobals(m); !ok || err != nil {
			t.Fatal(ok, err)
		}
		image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true})
		if err != nil || image == nil {
			t.Fatal(vector, err)
		}
		if image.CodeImage != nil {
			image.CodeImage.Close()
		}
		for _, opts := range []CompileOptions{{DeferCodeMapping: true, Interruptible: true}, {DeferCodeMapping: true, ForeignTableCalls: true}} {
			if image, err := CompileModuleWith(m, opts); image != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("private profile admitted", vector, err)
			}
		}
	}
}

func TestLocalMutableReferenceWholeModuleRejections(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for name, mutate := range map[string]func(*wasm.Module){
			"export": func(m *wasm.Module) {
				m.Exports = append(m.Exports, wasm.Export{Name: "hidden", Index: wasm.ExternIdx{Kind: wasm.ExternGlobal, Index: 0}})
			},
			"alias": func(m *wasm.Module) {
				m.Exports = append(m.Exports, wasm.Export{Name: "a", Index: wasm.ExternIdx{Kind: wasm.ExternGlobal, Index: 0}}, wasm.Export{Name: "b", Index: wasm.ExternIdx{Kind: wasm.ExternGlobal, Index: 0}})
			},
			"owned-table": func(m *wasm.Module) { m.Tables = referenceGlobalFixture(t, vector).Tables },
			"unused-reference-import": func(m *wasm.Module) {
				m.Imports = append(m.Imports, wasm.Import{Module: "env", Name: "ref", Type: wasm.NewGlobalExternType(wasm.GlobalType{Type: wasm.FuncRef})})
			},
			"mutable-import": func(m *wasm.Module) {
				m.Imports = append(m.Imports, wasm.Import{Module: "env", Name: "ref", Type: wasm.NewGlobalExternType(wasm.GlobalType{Type: wasm.FuncRef, Mutable: true})})
			},
			"reference-signature": func(m *wasm.Module) { m.Types[1].SubTypes[0].Comp.Params = []wasm.ValType{wasm.FuncRef} },
			"wrong-type":          func(m *wasm.Module) { m.Globals[0].Type.Type = wasm.ExternRef },
			"mutable-initializer-source": func(m *wasm.Module) {
				g := m.Globals[0]
				g.Init = wasm.Expr{BodyBytes: []byte{0x23, 0, 0x0b}}
				m.Globals = append(m.Globals, g)
			},
			"wrong-store":      func(m *wasm.Module) { m.Code[0].BodyBytes = []byte{0x41, 1, 0x24, 0, 0x41, 0, 0x0b} },
			"dead-wrong-store": func(m *wasm.Module) { m.Code[0].BodyBytes = []byte{0, 0x41, 1, 0x24, 0, 0x41, 0, 0x0b} },
		} {
			t.Run(name+map[bool]string{false: "-scalar", true: "-swar"}[vector], func(t *testing.T) {
				m := localTailDecode(t, localMutableReferenceSource(vector))
				mutate(m)
				pressure := 0
				image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, MemoryPressure: func() { pressure++ }})
				if err == nil || image != nil || pressure != 0 {
					t.Fatal("unsupported local reference shape staged", err, pressure)
				}
			})
		}
	}
}

func TestLocalMutableReferenceWalkerTypesAndDefaultGate(t *testing.T) {
	ft := &wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32}}
	g := globalSpec{wide: true, mutable: true, typ: wasm.FuncRef}
	state := moduleState{production: true, importReferences: true, localMutableReferences: true, globals: []globalSpec{g}}
	good := &wasm.Func{BodyBytes: []byte{0xd0, 0x70, 0x24, 0, 0x23, 0, 0xd1, 0x0b}}
	if _, err := walkInteger(good, ft, &state, nil); err != nil {
		t.Fatal("valid full-width reference store", err)
	}
	for _, body := range [][]byte{{0x41, 0, 0x24, 0, 0x41, 0, 0x0b}} {
		if _, err := walkInteger(&wasm.Func{BodyBytes: body}, ft, &state, nil); !errors.Is(err, ErrUnsupported) {
			t.Fatal("numeric reference store admitted", err)
		}
	}
	state.localMutableReferences = false
	if _, err := walkInteger(good, ft, &state, nil); !errors.Is(err, ErrUnsupported) {
		t.Fatal("unproven scalar profile", err)
	}
	globals := []wasm.GlobalType{{Type: wasm.FuncRef, Mutable: true}}
	if _, err := walkSWARPairsWithTailMode(good, ft, memoryProfile{}, nil, nil, globals, 0, nil, nil, 0, 0, true, false, false, nil); !errors.Is(err, ErrUnsupported) {
		t.Fatal("unproven SWAR profile", err)
	}
}

func TestLocalMutableReferenceStateOnlyRequiresModuleProfile(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, flags := range [][2]bool{{true, true}, {false, true}, {true, false}, {false, false}} {
			m := localTailDecode(t, localMutableReferenceSource(vector))
			m.Code = nil
			m.FuncTypes = nil
			m.Exports = nil
			image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: flags[0], ForeignTableCalls: flags[1]})
			if flags[0] && flags[1] {
				if image == nil || err != nil {
					t.Fatal("valid state-only reference profile", vector, err)
				}
				if image.CodeImage != nil {
					image.CodeImage.Close()
				}
			} else if image != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("state-only profile bypass", vector, flags, err)
			}
		}
	}
}
