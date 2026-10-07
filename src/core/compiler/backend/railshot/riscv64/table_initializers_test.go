//go:build linux && !tinygo

package riscv64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

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
	m := &wasm.Module{Tables: make([]wasm.Table, 2)}
	if err := AdmitPublicInitializers(m); err == nil {
		t.Fatal("table count not checked before initializer walk")
	}
}
