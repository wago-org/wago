//go:build linux && !tinygo

package riscv64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func expressionElementModule(t *testing.T, vector bool, mode wasm.ElemModeKind, count int) *wasm.Module {
	t.Helper()
	m, err := wasm.DecodeModule(combinedTableImportSource(1, vector, false, 3))
	if err != nil {
		t.Fatal(err)
	}
	e := &m.Elements[0]
	e.Mode.Kind = mode
	if mode == wasm.ElemActive {
		e.Mode.Offset.BodyBytes = []byte{0x41, 0, 0x0b}
	}
	e.Kind = wasm.ElemKind{Kind: wasm.ElemTypedExprs, Ref: wasm.FuncRef.Ref(), Exprs: make([]wasm.Expr, count)}
	for i := range e.Kind.Exprs {
		e.Kind.Exprs[i].BodyBytes = []byte{0xd0, 0x70, 0x0b}
	}
	if count != 0 {
		e.Kind.Exprs[0].BodyBytes = []byte{0xd2, 0, 0x0b}
	}
	return m
}

func TestPublicElementExpressionsAdmission(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, mode := range []wasm.ElemModeKind{wasm.ElemActive, wasm.ElemPassive, wasm.ElemDeclarative} {
			for _, count := range []int{0, 2, MaxOwnedTableEntries} {
				m := expressionElementModule(t, vector, mode, count)
				opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, ImportBindings: hostImportBindings(1)}
				if image, err := CompileModuleWith(m, opts); image == nil || err != nil {
					t.Fatal(vector, mode, count, err)
				}
				if _, err := admitOwnedTable(m, true); !errors.Is(err, ErrUnsupported) {
					t.Fatal("private pilot widened", err)
				}
				if vector {
					image, err := compileSWARDecodedModuleProfile(m, emissionMode{production: true, interruptible: true, foreignTableCalls: true}, swarModuleTables, swarStateProfile{publicStarts: true, exports: true, importedMemory: true, importedGlobals: true, importedTable: true, functionTableImports: true})
					if err != nil || !image.publicTables || len(image.elements) != 0 {
						t.Fatal("public image retained private initializer copies", err)
					}
					allocations := 0
					if _, err := image.instantiateTablesWithArena(new(coreruntime.JobMemory), nil, func(int) (*coreruntime.Arena, error) { allocations++; return nil, nil }); err == nil || allocations != 0 {
						t.Fatal("public image reached private owner", err, allocations)
					}
				}
			}
		}
	}
}

func TestPublicElementExpressionsRejectBeforeStaging(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, tc := range []struct {
			name   string
			mutate func(*wasm.Module)
		}{
			{"aggregate", func(m *wasm.Module) { m.Elements[0].Kind.Exprs = make([]wasm.Expr, MaxOwnedTableEntries+1) }},
			{"aggregate-dropped", func(m *wasm.Module) {
				m.Elements = append(m.Elements, m.Elements[0])
				m.Elements[1].Kind.Exprs = make([]wasm.Expr, MaxOwnedTableEntries)
			}},
			{"huge", func(m *wasm.Module) { m.Elements[0].Kind.Exprs[0].BodyBytes = make([]byte, 8) }},
			{"structured", func(m *wasm.Module) {
				m.Elements[0].Kind.Exprs[0].Instrs = []wasm.Instruction{{Kind: wasm.InstrRefFunc}}
			}},
			{"empty", func(m *wasm.Module) { m.Elements[0].Kind.Exprs[0].BodyBytes = nil }},
			{"global", func(m *wasm.Module) { m.Elements[0].Kind.Exprs[0].BodyBytes = []byte{0x23, 0, 0x0b} }},
			{"nofunc", func(m *wasm.Module) { m.Elements[0].Kind.Exprs[0].BodyBytes = []byte{0xd0, 0x73, 0x0b} }},
			{"extern", func(m *wasm.Module) { m.Elements[0].Kind.Exprs[0].BodyBytes = []byte{0xd0, 0x6f, 0x0b} }},
			{"trailing", func(m *wasm.Module) { m.Elements[0].Kind.Exprs[0].BodyBytes = []byte{0xd2, 0, 0x0b, 0} }},
			{"multi", func(m *wasm.Module) { m.Elements[0].Kind.Exprs[0].BodyBytes = []byte{0xd2, 0, 0xd2, 0, 0x0b} }},
			{"index", func(m *wasm.Module) { m.Elements[0].Kind.Exprs[0].BodyBytes = []byte{0xd2, 2, 0x0b} }},
			{"dual-payload", func(m *wasm.Module) { m.Elements[0].Kind.Funcs = []wasm.FuncIdx{0} }},
			{"extern-segment", func(m *wasm.Module) { m.Elements[0].Kind.Ref = wasm.ExternRef.Ref() }},
			{"empty-extern-segment", func(m *wasm.Module) { m.Elements[0].Kind.Exprs = nil; m.Elements[0].Kind.Ref = wasm.ExternRef.Ref() }},
			{"empty-nonnull-segment", func(m *wasm.Module) {
				m.Elements[0].Kind.Exprs = nil
				m.Elements[0].Kind.Ref = wasm.Ref(false, wasm.AbsHeap(wasm.HeapFunc), false)
			}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				m := expressionElementModule(t, vector, wasm.ElemDeclarative, 2)
				tc.mutate(m)
				pressure := 0
				image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, ImportBindings: hostImportBindings(1), MemoryPressure: func() { pressure++ }})
				if image != nil || err == nil || pressure != 0 {
					t.Fatal("unsupported expression staged", vector, err, pressure)
				}
			})
		}
	}
}
