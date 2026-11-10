//go:build linux && !tinygo

package riscv64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestPublicImportedElementOffset(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, expression := range []bool{false, true} {
			m, err := wasm.DecodeModule(combinedTableImportSource(1, vector, true, 0))
			if err != nil {
				t.Fatal(err)
			}
			if expression {
				m = expressionElementModule(t, vector, wasm.ElemActive, 1)
			}
			m.Elements[0].Mode.Offset = wasm.Expr{BodyBytes: []byte{0x23, 0, 0x0b}}
			opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, ImportBindings: hostImportBindings(1)}
			if image, err := CompileModuleWith(m, opts); image == nil || err != nil {
				t.Fatal(vector, expression, err)
			}
			if _, err := admitOwnedTable(m, true); !errors.Is(err, ErrUnsupported) {
				t.Fatal("private offset profile widened", err)
			}
			for _, tc := range []struct {
				name   string
				mutate func(*wasm.Module)
			}{
				{"wrong-type", func(m *wasm.Module) { m.Imports[0].Type = wasm.NewGlobalExternType(wasm.GlobalType{Type: wasm.F32}) }},
				{"mutable", func(m *wasm.Module) {
					m.Imports[0].Type = wasm.NewGlobalExternType(wasm.GlobalType{Type: wasm.I32, Mutable: true})
				}},
				{"missing", func(m *wasm.Module) { m.Elements[0].Mode.Offset.BodyBytes = []byte{0x23, 1, 0x0b} }},
				{"mutable-defined", func(m *wasm.Module) {
					m.Globals = append(m.Globals, wasm.Global{Type: wasm.GlobalType{Type: wasm.I32, Mutable: true}, Init: wasm.Expr{BodyBytes: []byte{0x41, 0, 0x0b}}})
					m.Elements[0].Mode.Offset.BodyBytes = []byte{0x23, 1, 0x0b}
				}},
				{"oversized", func(m *wasm.Module) {
					m.Elements[0].Mode.Offset.BodyBytes = []byte{0x23, 0x80, 0x80, 0x80, 0x80, 0, 0x0b, 0x0b}
				}},
				{"truncated", func(m *wasm.Module) { m.Elements[0].Mode.Offset.BodyBytes = []byte{0x23, 0x80} }},
				{"trailing", func(m *wasm.Module) { m.Elements[0].Mode.Offset.BodyBytes = []byte{0x23, 0, 0x0b, 0x0b} }},
				{"unsupported-arithmetic", func(m *wasm.Module) { m.Elements[0].Mode.Offset.BodyBytes = []byte{0x23, 0, 0x41, 1, 0x71, 0x0b} }},
				{"structured", func(m *wasm.Module) {
					m.Elements[0].Mode.Offset.Instrs = []wasm.Instruction{{Kind: wasm.InstrGlobalGet}}
				}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					bad := *m
					bad.Imports = append([]wasm.Import(nil), m.Imports...)
					bad.Elements = append([]wasm.Elem(nil), m.Elements...)
					tc.mutate(&bad)
					pressure := 0
					opts.MemoryPressure = func() { pressure++ }
					if image, err := CompileModuleWith(&bad, opts); image != nil || err == nil || pressure != 0 {
						t.Fatal("unqualified offset staged", err, pressure)
					}
				})
			}
		}
	}
}
