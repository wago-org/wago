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

func core3BrOnNonNullDeclarationsSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			[]byte{0x60, 1, 0x63, 0, 0},
			wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.ExternRef}, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x02, 0x64, 0, 0x20, 0, 0xd6, 0, 0x00, 0x0b, 0x1a, 0x0b}),
			wasmtest.Code([]byte{0x02, 0x64, 0x70, 0x20, 0, 0xd6, 0, 0x00, 0x0b, 0x1a, 0x0b}),
			wasmtest.Code([]byte{0x02, 0x64, 0x6f, 0x20, 0, 0xd6, 0, 0x00, 0x0b, 0x1a, 0x0b}),
		)),
	)
}

func core3BrOnNonNullDeclarationsDecode(t *testing.T) *wasm.Module {
	t.Helper()
	m, err := wasm.DecodeModule(core3BrOnNonNullDeclarationsSource())
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCore3BrOnNonNullDeclarationsPinnedProduction(t *testing.T) {
	source := core3BrOnNonNullDeclarationsSource()
	if len(source) != 75 || !bytes.Equal(source, core3BrOnNonNullDeclarationsWire) {
		t.Fatal("fixture is not the exact canonical wire module", len(source))
	}
	if got, want := fmt.Sprintf("%x", sha256.Sum256(source)), "fd36a463ea49ff5ce0eae90789b44bab38b09ca0bc83083e125b351a1b01010a"; got != want {
		t.Fatalf("fixture hash = %s, want %s", got, want)
	}
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal("wire contract", err)
	}
	m := core3BrOnNonNullDeclarationsDecode(t)
	if !admitCore3BrOnNonNullDeclarations(m) || admitCore3BrOnNullDeclarations(m) {
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

func TestCore3BrOnNonNullDeclarationsWireConfinement(t *testing.T) {
	debug := append(bytes.Clone(core3BrOnNonNullDeclarationsWire),
		0x00, 0x1d, 0x04, 0x6e, 0x61, 0x6d, 0x65, 0x02,
		0x10, 0x03, 0x00, 0x01, 0x00, 0x01, 0x72, 0x01,
		0x01, 0x00, 0x01, 0x72, 0x02, 0x01, 0x00, 0x01,
		0x72, 0x04, 0x04, 0x01, 0x00, 0x01, 0x74,
	)
	if len(debug) != 106 || fmt.Sprintf("%x", sha256.Sum256(debug)) != "3905f10de5be56a7f4d65f7e85aaffffa76be69306719d5c4d6fac09f122f813" {
		t.Fatal("canonical WABT debug-name witness changed")
	}
	mutations := map[string][]byte{
		"debug-name": debug,
		"opcode":     bytes.Replace(bytes.Clone(core3BrOnNonNullDeclarationsWire), []byte{0xd6, 0}, []byte{0xd5, 0}, 1),
		"depth":      bytes.Replace(bytes.Clone(core3BrOnNonNullDeclarationsWire), []byte{0xd6, 0}, []byte{0xd6, 1}, 1),
		"nullable":   bytes.Replace(bytes.Clone(core3BrOnNonNullDeclarationsWire), []byte{0x02, 0x64, 0x6f}, []byte{0x02, 0x63, 0x6f}, 1),
		"heap":       bytes.Replace(bytes.Clone(core3BrOnNonNullDeclarationsWire), []byte{0x02, 0x64, 0x6f}, []byte{0x02, 0x64, 0x6e}, 1),
		"drop":       bytes.Replace(bytes.Clone(core3BrOnNonNullDeclarationsWire), []byte{0x0b, 0x1a, 0x0b}, []byte{0x0b, 0x01, 0x0b}, 1),
	}
	for name, source := range mutations {
		t.Run(name, func(t *testing.T) {
			if admitCore3BrOnNonNullDeclarationsWire(source) || AdmitProductionSource(source) == nil {
				t.Fatal("nearby wire module admitted")
			}
		})
	}
}

func TestCore3BrOnNonNullDeclarationsDecodedConfinement(t *testing.T) {
	mutations := map[string]func(*wasm.Module){
		"opcode":            func(m *wasm.Module) { m.Code[0].BodyBytes[5] = 0xd5 },
		"depth":             func(m *wasm.Module) { m.Code[0].BodyBytes[6] = 1 },
		"block-nullability": func(m *wasm.Module) { m.Code[2].BodyBytes[1] = 0x63 },
		"block-heap":        func(m *wasm.Module) { m.Code[2].BodyBytes[2] = 0x6e },
		"body-order": func(m *wasm.Module) {
			m.Code[2].BodyBytes[3], m.Code[2].BodyBytes[7] = m.Code[2].BodyBytes[7], m.Code[2].BodyBytes[3]
		},
		"unreachable":      func(m *wasm.Module) { m.Code[2].BodyBytes[7] = 0x01 },
		"drop":             func(m *wasm.Module) { m.Code[2].BodyBytes[9] = 0x01 },
		"local":            func(m *wasm.Module) { m.Code[2].Locals.Runs = []wasm.LocalRun{{Count: 1, Type: wasm.ExternRef}} },
		"binding":          func(m *wasm.Module) { m.FuncTypes[2].Index = 2 },
		"signature-result": func(m *wasm.Module) { m.Types[3].SubTypes[0].Comp.Results = []wasm.ValType{wasm.ExternRef} },
		"signature-nonnull": func(m *wasm.Module) {
			m.Types[3].SubTypes[0].Comp.Params[0] = wasm.RefVal(wasm.Ref(false, wasm.AbsHeap(wasm.HeapExtern), false))
		},
		"rec-group": func(m *wasm.Module) { m.Types[1].SubTypes = append(m.Types[1].SubTypes, m.Types[1].SubTypes[0]) },
		"subtyping": func(m *wasm.Module) { m.Types[3].SubTypes[0].HasPrefix = true },
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
		"custom":          func(m *wasm.Module) { m.Customs = []wasm.CustomSec{{Name: "x"}} },
		"name":            func(m *wasm.Module) { m.NameSec = &wasm.NameSec{} },
		"branch-hint":     func(m *wasm.Module) { m.BranchHints = []wasm.FuncBranchHints{{FuncIndex: 0}} },
		"compact-imports": func(m *wasm.Module) { m.UsesCompactImports = true },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			m := core3BrOnNonNullDeclarationsDecode(t)
			mutate(m)
			if admitCore3BrOnNonNullDeclarations(m) {
				t.Fatal("nearby decoded module inherited the exact declaration profile")
			}
			pressure := 0
			opts := closedReferenceProductionOptions(false, true)
			opts.MemoryPressure = func() { pressure++ }
			if image, err := CompileModuleWith(m, opts); image != nil || err == nil || pressure != 0 {
				t.Fatal("nearby declaration shape published", err, pressure)
			}
		})
	}
}

func TestCore3BrOnNonNullDeclarationExternBlockOnly(t *testing.T) {
	types := internalReferenceTypes{declarationExtern: true}
	block := wasm.ReaderFrom([]byte{0x64, 0x6f})
	if got, err := internalReferenceBlockType(&block, true, types); got != -16 || err != nil || block.HasNext() {
		t.Fatal("exact non-null extern block", got, err)
	}
	for name, body := range map[string][]byte{
		"nullable-block": {0x63, 0x6f},
		"untyped-block":  {0x64, 0x6f},
		"typed-select":   {0x64, 0x6f},
	} {
		t.Run(name, func(t *testing.T) {
			r := wasm.ReaderFrom(body)
			var err error
			if name == "typed-select" {
				_, err = internalSelectType(&r, true, types)
			} else {
				_, err = internalReferenceBlockType(&r, name != "untyped-block", types)
			}
			if err == nil {
				t.Fatal("extern form escaped block-only typed confinement")
			}
		})
	}
}
