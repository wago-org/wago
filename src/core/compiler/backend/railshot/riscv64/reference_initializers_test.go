//go:build linux && !tinygo

package riscv64

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func referenceGlobalFixture(t *testing.T, vector bool) *wasm.Module {
	t.Helper()
	m, err := wasm.DecodeModule(combinedTableImportSource(1, vector, false, 3))
	if err != nil {
		t.Fatal(err)
	}
	m.Globals = []wasm.Global{{Type: wasm.GlobalType{Type: wasm.FuncRef}, Init: wasm.Expr{BodyBytes: []byte{0xd2, 0, 0x0b}}}}
	m.Code[0].BodyBytes = append([]byte{0, 0x23, 1, 0xd1, 0x1a}, m.Code[0].BodyBytes[1:]...)
	return m
}

func TestPublicImmutableReferenceGlobals(t *testing.T) {
	for _, vector := range []bool{false, true} {
		m := referenceGlobalFixture(t, vector)
		opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, ImportBindings: hostImportBindings(1)}
		image, err := CompileModuleWith(m, opts)
		if image == nil || err != nil {
			t.Fatal(vector, err)
		}
		if image.CodeImage != nil {
			image.CodeImage.Close()
		}
		opts.ForeignTableCalls = false
		if image, err := CompileModuleWith(m, opts); image != nil || err == nil {
			t.Fatal("private reference global admitted", vector, err)
		}
	}
}

func TestPublicImmutableReferenceGlobalRejectBeforeStaging(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, mutate := range []func(*wasm.Module){
			func(m *wasm.Module) { m.Globals[0].Type.Mutable = true },
			func(m *wasm.Module) {
				g := m.Globals[0]
				g.Init = wasm.Expr{BodyBytes: []byte{0x23, 2, 0x0b}}
				m.Globals = append(m.Globals, g)
			},
			func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Results = []wasm.ValType{wasm.FuncRef} },
			func(m *wasm.Module) { m.Globals[0].Type.Type = wasm.ExternRef },
			func(m *wasm.Module) { m.Globals[0].Init.BodyBytes = []byte{0x23, 0, 0x0b} },
			func(m *wasm.Module) { m.Globals[0].Init.BodyBytes = []byte{0xd2, 2, 0x0b} },
			func(m *wasm.Module) { m.Globals[0].Init.BodyBytes = []byte{0xd0, 0x73, 0x0b} },
			func(m *wasm.Module) { m.Globals[0].Init.BodyBytes = make([]byte, 8) },
			func(m *wasm.Module) { m.Globals[0].Init.Instrs = []wasm.Instruction{{Kind: wasm.InstrRefFunc}} },
			func(m *wasm.Module) { m.Tables = nil; m.Elements = nil },
			func(m *wasm.Module) {
				ex := m.Exports[0]
				ex.Name = "reference"
				ex.Index.Kind = wasm.ExternGlobal
				ex.Index.Index = 1
				m.Exports = append(m.Exports, ex)
			},
			func(m *wasm.Module) { m.Code[0].BodyBytes = []byte{0, 0x23, 1, 0x50, 0x1a, 0x20, 0, 0x0b} },
		} {
			m := referenceGlobalFixture(t, vector)
			mutate(m)
			pressure := 0
			image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, ImportBindings: hostImportBindings(1), MemoryPressure: func() { pressure++ }})
			if image != nil || err == nil || pressure != 0 {
				t.Fatal("unsupported reference global staged", vector, err, pressure)
			}
		}
	}
}

func TestNumericPublicInitializersPreserveMixedExportBounds(t *testing.T) {
	m, err := wasm.DecodeModule(combinedTableImportSource(1, false, false, 3))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxFunctions; i++ {
		ex := m.Exports[0]
		ex.Name = fmt.Sprintf("table%d", i)
		ex.Index.Kind = wasm.ExternTable
		ex.Index.Index = 0
		m.Exports = append(m.Exports, ex)
	}
	if len(m.Exports) != MaxFunctions+1 {
		t.Fatal("mixed export boundary fixture")
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, ImportBindings: hostImportBindings(1)})
	if image == nil || err != nil {
		t.Fatal("previously supported scalar mixed exports narrowed", err)
	}
	if image.CodeImage != nil {
		image.CodeImage.Close()
	}
}

func TestPublicLocalReferenceGlobalExports(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, null := range []bool{false, true} {
			m := referenceGlobalFixture(t, vector)
			m.Globals[0].Init.BodyBytes = []byte{0xd2, 1, 0x0b}
			if null {
				m.Globals[0].Init.BodyBytes = []byte{0xd0, 0x70, 0x0b}
			}
			ex := m.Exports[0]
			ex.Name = "reference"
			ex.Index.Kind = wasm.ExternGlobal
			ex.Index.Index = 1
			m.Exports = append(m.Exports, ex)
			image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, ImportBindings: hostImportBindings(1)})
			if image == nil || err != nil {
				t.Fatal("local reference global export", vector, null, err)
			}
			if image.CodeImage != nil {
				image.CodeImage.Close()
			}
		}
	}
}

func TestPublicReferenceGlobalCopies(t *testing.T) {
	for _, vector := range []bool{false, true} {
		m := referenceGlobalFixture(t, vector)
		m.Globals[0].Init.BodyBytes = []byte{0xd2, 1, 0x0b}
		m.Globals = append(m.Globals, wasm.Global{Type: wasm.GlobalType{Type: wasm.FuncRef}, Init: wasm.Expr{BodyBytes: []byte{0x23, 1, 0x0b}}})
		ex := m.Exports[0]
		ex.Name = "copied"
		ex.Index.Kind = wasm.ExternGlobal
		ex.Index.Index = 2
		m.Exports = append(m.Exports, ex)
		for _, mutate := range []func(*wasm.Module){
			nil,
			func(m *wasm.Module) { m.Globals[1].Init.BodyBytes = []byte{0x23, 0, 0x0b} },
			func(m *wasm.Module) { m.Globals[1].Init.BodyBytes = []byte{0x23, 2, 0x0b} },
			func(m *wasm.Module) { m.Globals[1].Init.BodyBytes = []byte{0x23, 3, 0x0b} },
			func(m *wasm.Module) { m.Globals[1].Init.BodyBytes = []byte{0x23, 1, 0x0b, 0x0b} },
			func(m *wasm.Module) {
				m.Globals[1].Init.BodyBytes = []byte{0x23, 0x81, 0x80, 0x80, 0x80, 0x80, 0, 0x0b}
			},
			func(m *wasm.Module) { m.Globals[0].Type.Mutable = true },
			func(m *wasm.Module) {
				m.Globals[0].Type.Type = wasm.I32
				m.Globals[0].Init.BodyBytes = []byte{0x41, 0, 0x0b}
			},
			func(m *wasm.Module) { m.Globals[0].Init.BodyBytes = []byte{0xd2, 0, 0x0b} },
		} {
			copy := *m
			copy.Globals = append([]wasm.Global(nil), m.Globals...)
			if mutate != nil {
				mutate(&copy)
			}
			pressure := 0
			image, err := CompileModuleWith(&copy, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, ImportBindings: hostImportBindings(1), MemoryPressure: func() { pressure++ }})
			if mutate == nil {
				if image == nil || err != nil {
					t.Fatal("valid prior-local reference copy", vector, err)
				}
			} else if image != nil || err == nil || pressure != 0 {
				t.Fatal("invalid copy reached staging", vector, err, pressure)
			}
			if image != nil && image.CodeImage != nil {
				image.CodeImage.Close()
			}
		}
	}
}
