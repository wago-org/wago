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

func core3RefIsNullDeclarationsSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			[]byte{0x60, 1, 0x64, 0, 0},
			[]byte{0x60, 1, 0x64, 0x70, 0},
			[]byte{0x60, 1, 0x64, 0x6f, 0},
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0xd1, 0x1a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0xd1, 0x1a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0xd1, 0x1a, 0x0b}),
		)),
	)
}

func core3RefIsNullDeclarationsDecode(t *testing.T) *wasm.Module {
	t.Helper()
	m, err := wasm.DecodeModule(core3RefIsNullDeclarationsSource())
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCore3RefIsNullDeclarationsPinnedProduction(t *testing.T) {
	source := core3RefIsNullDeclarationsSource()
	if len(source) != 59 || !bytes.Equal(source, core3RefIsNullDeclarationsWire) {
		t.Fatal("fixture is not the exact canonical wire module", len(source))
	}
	if got, want := fmt.Sprintf("%x", sha256.Sum256(source)), "34acce8bbb453baa539d7fa152076d6cbe6b09f789bee05725efa4612ba7e9a5"; got != want {
		t.Fatalf("fixture hash = %s, want %s", got, want)
	}
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal("wire contract", err)
	}
	m := core3RefIsNullDeclarationsDecode(t)
	if !admitCore3RefIsNullDeclarations(m) || admitCore3RefAsNonNullDeclarations(m) || admitCore3BrOnNullDeclarations(m) || admitCore3BrOnNonNullDeclarations(m) {
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
	if len(image.Code) != 1820 || fmt.Sprintf("%x", sha256.Sum256(image.Code)) != "d2b9e9c8c334de03404b77c7044ee894fc4585a122c601f8c7c25180765d5478" || image.Entry[0] != 0 || image.Entry[1] != 608 || image.Entry[2] != 1216 {
		t.Fatal("declaration-only native code identity", len(image.Code), image.Entry)
	}
}

func TestCore3RefIsNullDeclarationsWireProfileSeparation(t *testing.T) {
	debug := append(bytes.Clone(core3RefIsNullDeclarationsWire),
		0x00, 0x1d, 0x04, 0x6e, 0x61, 0x6d, 0x65, 0x02,
		0x10, 0x03, 0x00, 0x01, 0x00, 0x01, 0x72, 0x01,
		0x01, 0x00, 0x01, 0x72, 0x02, 0x01, 0x00, 0x01,
		0x72, 0x04, 0x04, 0x01, 0x00, 0x01, 0x74,
	)
	if len(debug) != 90 || fmt.Sprintf("%x", sha256.Sum256(debug)) != "f61f8b317236822d33571492f714c3627072c6b6985754e2f3020f464d0fad74" {
		t.Fatal("canonical WABT debug-name witness changed")
	}
	sibling := bytes.ReplaceAll(bytes.Clone(core3RefIsNullDeclarationsWire), []byte{0xd1, 0x1a}, []byte{0xd4, 0x1a})
	if !bytes.Equal(sibling, core3RefAsNonNullDeclarationsWire) || admitCore3RefIsNullDeclarationsWire(sibling) || !admitCore3RefAsNonNullDeclarationsWire(sibling) || AdmitProductionSource(sibling) != nil {
		t.Fatal("sibling declaration profiles are not exact and separate")
	}
	mutations := map[string][]byte{
		"debug-name":       debug,
		"indexed-nullable": bytes.Replace(bytes.Clone(core3RefIsNullDeclarationsWire), []byte{0x64, 0, 0}, []byte{0x63, 0, 0}, 1),
		"indexed-heap":     bytes.Replace(bytes.Clone(core3RefIsNullDeclarationsWire), []byte{0x64, 0, 0}, []byte{0x64, 1, 0}, 1),
		"func-nullable":    bytes.Replace(bytes.Clone(core3RefIsNullDeclarationsWire), []byte{0x64, 0x70, 0}, []byte{0x63, 0x70, 0}, 1),
		"extern-nullable":  bytes.Replace(bytes.Clone(core3RefIsNullDeclarationsWire), []byte{0x64, 0x6f, 0}, []byte{0x63, 0x6f, 0}, 1),
		"extern-heap":      bytes.Replace(bytes.Clone(core3RefIsNullDeclarationsWire), []byte{0x64, 0x6f, 0}, []byte{0x64, 0x6e, 0}, 1),
		"binding":          bytes.Replace(bytes.Clone(core3RefIsNullDeclarationsWire), []byte{3, 1, 2, 3}, []byte{3, 1, 2, 2}, 1),
		"opcode":           bytes.Replace(bytes.Clone(core3RefIsNullDeclarationsWire), []byte{0xd1, 0x1a}, []byte{0xd0, 0x1a}, 1),
		"body-order":       bytes.Replace(bytes.Clone(core3RefIsNullDeclarationsWire), []byte{0x20, 0, 0xd1, 0x1a}, []byte{0x20, 0, 0x1a, 0xd1}, 1),
		"drop":             bytes.Replace(bytes.Clone(core3RefIsNullDeclarationsWire), []byte{0xd1, 0x1a, 0x0b}, []byte{0xd1, 0x01, 0x0b}, 1),
	}
	for name, source := range mutations {
		t.Run(name, func(t *testing.T) {
			checkGeneralDeclarationWire(t, source)
			if admitCore3RefIsNullDeclarationsWire(source) {
				t.Fatal("nearby wire module admitted")
			}
		})
	}
}

func TestCore3RefIsNullDeclarationsDecodedProfileSeparation(t *testing.T) {
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
		"start":      func(m *wasm.Module) { start := wasm.FuncIdx(2); m.Start = &start },
		"table":      func(m *wasm.Module) { m.Tables = []wasm.Table{{Type: wasm.TableType{Ref: wasm.FuncRef.Ref()}}} },
		"memory":     func(m *wasm.Module) { m.Memories = []wasm.MemType{{}} },
		"data-count": func(m *wasm.Module) { count := uint32(0); m.DataCount = &count },
		"custom":     func(m *wasm.Module) { m.Customs = []wasm.CustomSec{{Name: "x"}} },
		"name":       func(m *wasm.Module) { m.NameSec = &wasm.NameSec{} },
		"raw-name":   func(m *wasm.Module) { m.RawNameSecPayload = []byte{0} },
		"branch-hint": func(m *wasm.Module) {
			m.BranchHints = []wasm.FuncBranchHints{{FuncIndex: 0}}
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			m := core3RefIsNullDeclarationsDecode(t)
			mutate(m)
			if admitCore3RefIsNullDeclarations(m) {
				t.Fatal("nearby decoded module inherited the exact declaration profile")
			}
			checkGeneralDeclarationModule(t, m)
		})
	}
}
