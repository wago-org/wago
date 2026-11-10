//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func core3BrOnNullDeclarationsSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			[]byte{0x60, 1, 0x63, 0, 0},
			wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.ExternRef}, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0xd5, 0, 0x1a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0xd5, 0, 0x1a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0xd5, 0, 0x1a, 0x0b}),
		)),
	)
}

func core3BrOnNullDeclarationsDecode(t *testing.T) *wasm.Module {
	t.Helper()
	m, err := wasm.DecodeModule(core3BrOnNullDeclarationsSource())
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCore3BrOnNullDeclarationsPinnedProduction(t *testing.T) {
	source := core3BrOnNullDeclarationsSource()
	if got, want := fmt.Sprintf("%x", sha256.Sum256(source)), "fcc2e80deb76197719945fa297657e768c4f39b5930cc31f2708eec694550993"; got != want {
		t.Fatalf("fixture hash = %s, want %s", got, want)
	}
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal("wire contract", err)
	}
	m := core3BrOnNullDeclarationsDecode(t)
	if !admitCore3BrOnNullDeclarations(m) {
		t.Fatal("exact decoded contract")
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

func TestCore3BrOnNullDeclarationsConfinement(t *testing.T) {
	mutations := map[string]func(*wasm.Module){
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
		"tableless-active": func(m *wasm.Module) {
			m.Elements = []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemActive}, Kind: wasm.ElemKind{Kind: wasm.ElemFuncs, Funcs: []wasm.FuncIdx{0}}}}
		},
		"typed-passive": func(m *wasm.Module) {
			m.Elements = []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemPassive}, Kind: wasm.ElemKind{Kind: wasm.ElemTypedExprs, Ref: wasm.ExternRef.Ref(), Exprs: []wasm.Expr{{BodyBytes: []byte{0xd0, 0x6f, 0x0b}}}}}}
		},
		"custom":    func(m *wasm.Module) { m.Customs = []wasm.CustomSec{{Name: "x"}} },
		"name-view": func(m *wasm.Module) { m.NameSec = &wasm.NameSec{} },
		"raw-name":  func(m *wasm.Module) { m.RawNameSecPayload = []byte{0} },
		"result": func(m *wasm.Module) {
			m.Types[3].SubTypes[0].Comp.Results = []wasm.ValType{wasm.ExternRef}
		},
		"nonnull-extern": func(m *wasm.Module) {
			m.Types[3].SubTypes[0].Comp.Params[0] = wasm.RefVal(wasm.Ref(false, wasm.AbsHeap(wasm.HeapExtern), false))
		},
		"wrong-binding": func(m *wasm.Module) { m.FuncTypes[2].Index = 2 },
		"local": func(m *wasm.Module) {
			m.Code[2].Locals.Runs = []wasm.LocalRun{{Count: 1, Type: wasm.ExternRef}}
		},
		"body": func(m *wasm.Module) { m.Code[2].BodyBytes = []byte{0x20, 0, 0x1a, 0x0b} },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			m := core3BrOnNullDeclarationsDecode(t)
			mutate(m)
			if admitCore3BrOnNullDeclarations(m) {
				t.Fatal("nearby shape inherited the exact declaration profile")
			}
			checkGeneralDeclarationModule(t, m)
		})
	}
}
