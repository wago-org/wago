//go:build linux && !tinygo

package riscv64

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func globalElementModule(t *testing.T, vector, imported bool, mode wasm.ElemModeKind) *wasm.Module {
	t.Helper()
	m := expressionElementModule(t, vector, mode, 1)
	gt := wasm.GlobalType{Type: wasm.FuncRef}
	if imported {
		m.Imports = append(m.Imports, wasm.Import{Module: "env", Name: "reference", Type: wasm.NewGlobalExternType(gt)})
	} else {
		m.Globals = []wasm.Global{{Type: gt, Init: wasm.Expr{BodyBytes: []byte{0xd0, 0x70, 0x0b}}}}
	}
	m.Elements[0].Kind.Exprs[0].BodyBytes = []byte{0x23, 1, 0x0b}
	return m
}

func TestPublicElementGlobalInitializerAdmission(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, imported := range []bool{false, true} {
			for _, mode := range []wasm.ElemModeKind{wasm.ElemActive, wasm.ElemPassive} {
				t.Run(fmt.Sprintf("vector%t-imported%t-mode%d", vector, imported, mode), func(t *testing.T) {
					m := globalElementModule(t, vector, imported, mode)
					image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, ImportBindings: hostImportBindings(1)})
					if err != nil || image == nil {
						t.Fatal("immutable element global rejected", err)
					}
					if image.CodeImage != nil {
						image.CodeImage.Close()
					}
					if _, err := admitOwnedTable(m, true); !errors.Is(err, ErrUnsupported) {
						t.Fatal("private element global admitted", err)
					}
					if err := admitSWARDecodedBudget(m); !errors.Is(err, ErrUnsupported) {
						t.Fatal("private SWAR element global admitted", err)
					}
				})
			}
		}
	}
}

func TestPublicElementGlobalInitializerRejectBeforeStaging(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for name, mutate := range map[string]func(*wasm.Module){
			"mutable":   func(m *wasm.Module) { m.Globals[0].Type.Mutable = true },
			"numeric":   func(m *wasm.Module) { m.Globals[0].Type.Type = wasm.I32 },
			"externref": func(m *wasm.Module) { m.Globals[0].Type.Type = wasm.ExternRef },
			"index":     func(m *wasm.Module) { m.Elements[0].Kind.Exprs[0].BodyBytes = []byte{0x23, 2, 0x0b} },
			"wrapped-index": func(m *wasm.Module) {
				m.Elements[0].Kind.Exprs[0].BodyBytes = []byte{0x23, 0xff, 0xff, 0xff, 0xff, 15, 0x0b}
			},
			"trailing": func(m *wasm.Module) { m.Elements[0].Kind.Exprs[0].BodyBytes = []byte{0x23, 0, 0x0b, 0} },
			"structured": func(m *wasm.Module) {
				m.Elements[0].Kind.Exprs[0] = wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrGlobalGet}}}
			},
			"mixed": func(m *wasm.Module) {
				m.Elements[0].Kind.Exprs[0].Instrs = []wasm.Instruction{{Kind: wasm.InstrGlobalGet}}
			},
			"declarative":       func(m *wasm.Module) { m.Elements[0].Mode = wasm.ElemMode{Kind: wasm.ElemDeclarative} },
			"table-initializer": func(m *wasm.Module) { m.Tables[0].Init = &wasm.Expr{BodyBytes: []byte{0x23, 0, 0x0b}} },
		} {
			t.Run(fmt.Sprintf("vector%t-%s", vector, name), func(t *testing.T) {
				m := globalElementModule(t, vector, false, wasm.ElemActive)
				mutate(m)
				staged := false
				image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, ImportBindings: hostImportBindings(1), MemoryPressure: func() { staged = true }})
				if image != nil || err == nil || staged {
					t.Fatal("unsupported element global reached staging", err, staged)
				}
			})
		}
	}
}

func TestPublicElementGlobalInitializerAdmissionDoesNotAllocate(t *testing.T) {
	m := &wasm.Module{Globals: []wasm.Global{{Type: wasm.GlobalType{Type: wasm.FuncRef}}}}
	expr := wasm.Expr{BodyBytes: []byte{0x23, 0, 0x0b}}
	if allocs := testing.AllocsPerRun(100, func() {
		value, err := elementInitializerExpression(expr, m, wasm.ElemActive, true)
		if err != nil || !value.HasGlobal || value.GlobalIndex != 0 {
			t.Fatal(value, err)
		}
	}); allocs != 0 {
		t.Fatal("global element admission allocations", allocs)
	}
}
