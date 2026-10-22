//go:build linux && !tinygo

package riscv64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func importedGlobalTableInitializerModule(t *testing.T, vector bool) *wasm.Module {
	t.Helper()
	m, err := wasm.DecodeModule(combinedTableImportSource(1, vector, false, 3))
	if err != nil {
		t.Fatal(err)
	}
	m.Imports[0].Type = wasm.NewGlobalExternType(wasm.GlobalType{Type: wasm.FuncRef})
	m.Tables[0].Init = &wasm.Expr{BodyBytes: []byte{0x23, 0, 0x0b}}
	return m
}

func TestPublicTableInitializerAdmission(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, body := range [][]byte{{0xd0, 0x70, 0x0b}, {0xd2, 0, 0x0b}, {0xd2, 1, 0x0b}, {0xd2, 0x80, 0x80, 0x80, 0x80, 0, 0x0b}} {
			m, err := wasm.DecodeModule(combinedTableImportSource(1, vector, false, 3))
			if err != nil {
				t.Fatal(err)
			}
			m.Tables[0].Init = &wasm.Expr{BodyBytes: body}
			opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, ImportBindings: hostImportBindings(1)}
			image, err := CompileModuleWith(m, opts)
			if err != nil || image == nil {
				t.Fatal(vector, body, err)
			}
			if image.CodeImage != nil {
				image.CodeImage.Close()
			}
			if _, err := admitOwnedTable(m, true); err == nil {
				t.Fatal("private table initializer admitted")
			}
			if err := admitSWARDecodedBudget(m); err == nil {
				t.Fatal("private SWAR initializer admitted")
			}
		}
	}
}

func TestPublicTableInitializerRejectBeforeStaging(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, expr := range []wasm.Expr{
			{BodyBytes: nil}, {BodyBytes: []byte{0xd2, 2, 0x0b}}, {BodyBytes: []byte{0xd2, 0, 0x0b, 0}},
			{BodyBytes: []byte{0xd0, 0x6f, 0x0b}}, {BodyBytes: []byte{0xd0, 0x73, 0x0b}},
			{BodyBytes: []byte{0x23, 0, 0x0b}}, {BodyBytes: make([]byte, 8)},
			{BodyBytes: []byte{0xd2, 0, 0x0b}, Instrs: []wasm.Instruction{{Kind: wasm.InstrRefFunc}}},
		} {
			m, err := wasm.DecodeModule(combinedTableImportSource(1, vector, false, 3))
			if err != nil {
				t.Fatal(err)
			}
			m.Tables[0].Init = &expr
			m.Tables[0].Type.Limits.Min = 0
			pressure := 0
			image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, ImportBindings: hostImportBindings(1), MemoryPressure: func() { pressure++ }})
			if image != nil || err == nil || pressure != 0 {
				t.Fatal("unsupported table initializer staged", vector, expr, err, pressure)
			}
		}
	}
}

func TestPublicTableInitializerCountPreflight(t *testing.T) {
	m := &wasm.Module{Tables: make([]wasm.Table, MaxFunctions+1)}
	if err := AdmitPublicInitializers(m); err == nil {
		t.Fatal("table count not checked before initializer walk")
	}
}

func TestPublicImportedGlobalTableInitializerAdmission(t *testing.T) {
	for _, vector := range []bool{false, true} {
		m := importedGlobalTableInitializerModule(t, vector)
		image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, ImportBindings: hostImportBindings(1)})
		if err != nil || image == nil {
			t.Fatal("immutable imported-global table initializer", vector, err)
		}
		if image.CodeImage != nil {
			image.CodeImage.Close()
		}
		if _, err := admitOwnedTable(m, true); !errors.Is(err, ErrUnsupported) {
			t.Fatal("private table initializer admitted", vector, err)
		}
		if err := admitSWARDecodedBudget(m); !errors.Is(err, ErrUnsupported) {
			t.Fatal("private SWAR table initializer admitted", vector, err)
		}
	}
}

func TestPublicImportedGlobalTableInitializerRejectBeforeStaging(t *testing.T) {
	for name, mutate := range map[string]func(*wasm.Module){
		"mutable": func(m *wasm.Module) {
			m.Imports[0].Type = wasm.NewGlobalExternType(wasm.GlobalType{Type: wasm.FuncRef, Mutable: true})
		},
		"numeric": func(m *wasm.Module) { m.Imports[0].Type = wasm.NewGlobalExternType(wasm.GlobalType{Type: wasm.I32}) },
		"externref": func(m *wasm.Module) {
			m.Imports[0].Type = wasm.NewGlobalExternType(wasm.GlobalType{Type: wasm.ExternRef})
		},
		"index": func(m *wasm.Module) { m.Tables[0].Init.BodyBytes = []byte{0x23, 1, 0x0b} },
		"local": func(m *wasm.Module) {
			m.Globals = []wasm.Global{{Type: wasm.GlobalType{Type: wasm.FuncRef}, Init: wasm.Expr{BodyBytes: []byte{0xd0, 0x70, 0x0b}}}}
			m.Tables[0].Init.BodyBytes = []byte{0x23, 1, 0x0b}
		},
		"trailing": func(m *wasm.Module) { m.Tables[0].Init.BodyBytes = []byte{0x23, 0, 0x0b, 0} },
		"structured": func(m *wasm.Module) {
			m.Tables[0].Init = &wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrGlobalGet}}}
		},
		"mixed": func(m *wasm.Module) {
			m.Tables[0].Init.Instrs = []wasm.Instruction{{Kind: wasm.InstrGlobalGet}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := importedGlobalTableInitializerModule(t, false)
			m.Tables[0].Type.Limits.Min = 0 // malformed values still validate at size zero
			mutate(m)
			staged := false
			image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, ImportBindings: hostImportBindings(1), MemoryPressure: func() { staged = true }})
			if image != nil || err == nil || staged {
				t.Fatal("unsupported table global reached staging", err, staged)
			}
		})
	}
}

func TestPublicImportedGlobalTableInitializerAggregateBounds(t *testing.T) {
	exact := importedGlobalTableInitializerModule(t, false)
	exact.Tables[0].Type.Limits.Min = MaxOwnedTableEntries - 1
	exact.Tables[0].Type.Limits.Max = MaxOwnedTableEntries
	if err := AdmitPublicInitializers(exact); err != nil {
		t.Fatal("exact synthetic table initializer budget", err)
	}

	over := importedGlobalTableInitializerModule(t, false)
	over.Tables[0].Type.Limits.Min = MaxOwnedTableEntries
	over.Tables[0].Type.Limits.Max = MaxOwnedTableEntries
	if err := AdmitPublicInitializers(over); !errors.Is(err, ErrUnsupported) {
		t.Fatal("synthetic initializer value budget admitted", err)
	}

	segments := importedGlobalTableInitializerModule(t, false)
	segments.Tables[0].Type.Limits.Min = 0
	segments.Elements = make([]wasm.Elem, MaxOwnedElementSegments)
	if err := AdmitPublicInitializers(segments); err != nil {
		t.Fatal("public empty initializer metadata used a private segment count limit", err)
	}
}
