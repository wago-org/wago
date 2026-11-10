//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func core3RefAsNonNullDeclarationsSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			[]byte{0x60, 1, 0x64, 0, 0},
			[]byte{0x60, 1, 0x64, 0x70, 0},
			[]byte{0x60, 1, 0x64, 0x6f, 0},
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0xd4, 0x1a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0xd4, 0x1a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0xd4, 0x1a, 0x0b}),
		)),
	)
}

func core3RefAsNonNullDeclarationsDecode(t *testing.T) *wasm.Module {
	t.Helper()
	m, err := wasm.DecodeModule(core3RefAsNonNullDeclarationsSource())
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCore3RefAsNonNullDeclarationsPinnedProduction(t *testing.T) {
	source := core3RefAsNonNullDeclarationsSource()
	if len(source) != 59 || !bytes.Equal(source, core3RefAsNonNullDeclarationsWire) {
		t.Fatal("fixture is not the exact canonical wire module", len(source))
	}
	if got, want := fmt.Sprintf("%x", sha256.Sum256(source)), "45fee4fd0cc00312b4760a7159290fe1c5c5dfd23d6d80de5eb18fc6c5985eb7"; got != want {
		t.Fatalf("fixture hash = %s, want %s", got, want)
	}
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal("wire contract", err)
	}
	m := core3RefAsNonNullDeclarationsDecode(t)
	if !admitCore3RefAsNonNullDeclarations(m) || admitCore3BrOnNullDeclarations(m) || admitCore3BrOnNonNullDeclarations(m) {
		t.Fatal("separate exact decoded contract")
	}
	pressure := 0
	opts := closedReferenceProductionOptions(false, true)
	opts.MemoryPressure = func() { pressure++ }
	image, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	if image == nil || !image.NativeReferenceFunctions || !image.NativeReferenceControl || image.NativeReferenceCalls || image.NativeTail != nil || len(image.Entry) != 3 || len(image.InternalEntry) != 3 || image.CodeImage != nil || pressure != 1 {
		t.Fatal("declaration-only production witness", pressure)
	}
}

func TestCore3RefAsNonNullDeclarationsWireProfileSeparation(t *testing.T) {
	debug := append(bytes.Clone(core3RefAsNonNullDeclarationsWire),
		0x00, 0x1d, 0x04, 0x6e, 0x61, 0x6d, 0x65, 0x02,
		0x10, 0x03, 0x00, 0x01, 0x00, 0x01, 0x72, 0x01,
		0x01, 0x00, 0x01, 0x72, 0x02, 0x01, 0x00, 0x01,
		0x72, 0x04, 0x04, 0x01, 0x00, 0x01, 0x74,
	)
	if len(debug) != 90 || fmt.Sprintf("%x", sha256.Sum256(debug)) != "61a237b98e3b2f2cd603ea05a02bf11fce31f7186c5f7972b0ba822e49b5c149" {
		t.Fatal("canonical WABT debug-name witness changed")
	}
	mutations := map[string][]byte{
		"debug-name":       debug,
		"indexed-nullable": bytes.Replace(bytes.Clone(core3RefAsNonNullDeclarationsWire), []byte{0x64, 0, 0}, []byte{0x63, 0, 0}, 1),
		"indexed-heap":     bytes.Replace(bytes.Clone(core3RefAsNonNullDeclarationsWire), []byte{0x64, 0, 0}, []byte{0x64, 1, 0}, 1),
		"func-nullable":    bytes.Replace(bytes.Clone(core3RefAsNonNullDeclarationsWire), []byte{0x64, 0x70, 0}, []byte{0x63, 0x70, 0}, 1),
		"extern-nullable":  bytes.Replace(bytes.Clone(core3RefAsNonNullDeclarationsWire), []byte{0x64, 0x6f, 0}, []byte{0x63, 0x6f, 0}, 1),
		"extern-heap":      bytes.Replace(bytes.Clone(core3RefAsNonNullDeclarationsWire), []byte{0x64, 0x6f, 0}, []byte{0x64, 0x6e, 0}, 1),
		"binding":          bytes.Replace(bytes.Clone(core3RefAsNonNullDeclarationsWire), []byte{3, 1, 2, 3}, []byte{3, 1, 2, 2}, 1),
		"opcode":           bytes.Replace(bytes.Clone(core3RefAsNonNullDeclarationsWire), []byte{0xd4, 0x1a}, []byte{0xd0, 0x1a}, 1),
		"body-order":       bytes.Replace(bytes.Clone(core3RefAsNonNullDeclarationsWire), []byte{0x20, 0, 0xd4, 0x1a}, []byte{0x20, 0, 0x1a, 0xd4}, 1),
		"drop":             bytes.Replace(bytes.Clone(core3RefAsNonNullDeclarationsWire), []byte{0xd4, 0x1a, 0x0b}, []byte{0xd4, 0x01, 0x0b}, 1),
	}
	for name, source := range mutations {
		t.Run(name, func(t *testing.T) {
			checkGeneralDeclarationWire(t, source)
			if admitCore3RefAsNonNullDeclarationsWire(source) {
				t.Fatal("nearby wire module admitted")
			}
		})
	}
}

func TestCore3RefAsNonNullDeclarationsDecodedProfileSeparation(t *testing.T) {
	mutations := map[string]func(*wasm.Module){
		"indexed-nullable": func(m *wasm.Module) {
			m.Types[1].SubTypes[0].Comp.Params[0] = wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{}), false))
		},
		"indexed-heap": func(m *wasm.Module) {
			m.Types[1].SubTypes[0].Comp.Params[0] = wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 1}), false))
		},
		"func-nullable":   func(m *wasm.Module) { m.Types[2].SubTypes[0].Comp.Params[0] = wasm.FuncRef },
		"extern-nullable": func(m *wasm.Module) { m.Types[3].SubTypes[0].Comp.Params[0] = wasm.ExternRef },
		"extern-heap": func(m *wasm.Module) {
			m.Types[3].SubTypes[0].Comp.Params[0] = wasm.RefVal(wasm.Ref(false, wasm.AbsHeap(wasm.HeapAny), false))
		},
		"binding": func(m *wasm.Module) { m.FuncTypes[2].Index = 2 },
		"result": func(m *wasm.Module) {
			m.Types[3].SubTypes[0].Comp.Results = []wasm.ValType{m.Types[3].SubTypes[0].Comp.Params[0]}
		},
		"local":  func(m *wasm.Module) { m.Code[2].Locals.Runs = []wasm.LocalRun{{Count: 1, Type: wasm.ExternRef}} },
		"opcode": func(m *wasm.Module) { m.Code[2].BodyBytes[2] = 0xd0 },
		"body-order": func(m *wasm.Module) {
			m.Code[2].BodyBytes[2], m.Code[2].BodyBytes[3] = m.Code[2].BodyBytes[3], m.Code[2].BodyBytes[2]
		},
		"rec-group":  func(m *wasm.Module) { m.Types[1].SubTypes = append(m.Types[1].SubTypes, m.Types[1].SubTypes[0]) },
		"descriptor": func(m *wasm.Module) { m.Types[3].SubTypes[0].Metadata.Descriptor = wasm.SomeTypeIdx(wasm.TypeIdx{}) },
		"import": func(m *wasm.Module) {
			m.Imports = []wasm.Import{{Module: "env", Name: "f", Type: wasm.NewFuncExternType(wasm.TypeIdx{})}}
		},
		"export": func(m *wasm.Module) {
			m.Exports = []wasm.Export{{Name: "escape", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 2}}}
		},
		"start":  func(m *wasm.Module) { start := wasm.FuncIdx(2); m.Start = &start },
		"table":  func(m *wasm.Module) { m.Tables = []wasm.Table{{Type: wasm.TableType{Ref: wasm.FuncRef.Ref()}}} },
		"memory": func(m *wasm.Module) { m.Memories = []wasm.MemType{{}} },
		"global": func(m *wasm.Module) {
			m.Globals = []wasm.Global{{Type: wasm.GlobalType{Type: wasm.ExternRef}, Init: wasm.Expr{BodyBytes: []byte{0xd0, 0x6f, 0x0b}}}}
		},
		"element": func(m *wasm.Module) {
			m.Elements = []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemPassive}, Kind: wasm.ElemKind{Kind: wasm.ElemFuncs, Funcs: []wasm.FuncIdx{2}}}}
		},
		"data": func(m *wasm.Module) {
			m.Data = []wasm.Data{{Mode: wasm.DataMode{Kind: wasm.DataPassive}, Init: []byte{1}}}
		},
		"data-count":      func(m *wasm.Module) { count := uint32(0); m.DataCount = &count },
		"custom":          func(m *wasm.Module) { m.Customs = []wasm.CustomSec{{Name: "x"}} },
		"name":            func(m *wasm.Module) { m.NameSec = &wasm.NameSec{} },
		"raw-name":        func(m *wasm.Module) { m.RawNameSecPayload = []byte{0} },
		"branch-hint":     func(m *wasm.Module) { m.BranchHints = []wasm.FuncBranchHints{{FuncIndex: 0}} },
		"compact-imports": func(m *wasm.Module) { m.UsesCompactImports = true },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			m := core3RefAsNonNullDeclarationsDecode(t)
			mutate(m)
			if admitCore3RefAsNonNullDeclarations(m) {
				t.Fatal("nearby decoded module inherited the exact declaration profile")
			}
			checkGeneralDeclarationModule(t, m)
		})
	}
}

func TestCore3RefAsNonNullDeclarationExternParameterOnly(t *testing.T) {
	nonNullExtern := wasm.RefVal(wasm.Ref(false, wasm.AbsHeap(wasm.HeapExtern), false))
	types := internalReferenceTypes{declarationNonNullExtern: true}
	if !internalFunctionReferenceWithTypes(nonNullExtern, true, types) || internalFunctionReferenceWithTypes(wasm.ExternRef, true, types) || internalFunctionReferenceWithTypes(nonNullExtern, true, internalReferenceTypes{declarationExtern: true}) {
		t.Fatal("exact non-null extern parameter permission")
	}
	for name, body := range map[string][]byte{
		"block":  {0x64, 0x6f},
		"select": {0x64, 0x6f},
	} {
		t.Run(name, func(t *testing.T) {
			r := wasm.ReaderFrom(body)
			var err error
			if name == "block" {
				_, err = internalReferenceBlockType(&r, true, types)
			} else {
				_, err = internalSelectType(&r, true, types)
			}
			if err == nil {
				t.Fatal("non-null extern permission escaped function-parameter classification")
			}
		})
	}
}
