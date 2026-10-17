//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func core3RefDeclarationsDecode(t *testing.T) *wasm.Module {
	t.Helper()
	m, err := wasm.DecodeModule(bytes.Clone(core3RefDeclarationsWire))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCore3RefDeclarationsPinnedProduction(t *testing.T) {
	source := bytes.Clone(core3RefDeclarationsWire)
	if len(source) != 47 {
		t.Fatal("fixture is not the exact canonical wire module", len(source))
	}
	if got, want := fmt.Sprintf("%x", sha256.Sum256(source)), "c9615f60f19a2d7a416ae8e7d6383b0f478cf8a5f9864adf8b095d14576d00a9"; got != want {
		t.Fatalf("fixture hash = %s, want %s", got, want)
	}
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal("wire contract", err)
	}
	m := core3RefDeclarationsDecode(t)
	if !admitCore3RefDeclarations(m) || admitCore3RefIsNullDeclarations(m) || admitCore3RefAsNonNullDeclarations(m) || admitCore3BrOnNullDeclarations(m) || admitCore3BrOnNonNullDeclarations(m) {
		t.Fatal("separate exact decoded contract")
	}
	pressure := 0
	opts := closedReferenceProductionOptions(false, true)
	opts.MemoryPressure = func() { pressure++ }
	image, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	if image == nil || !image.NativeReferenceFunctions || !image.NativeReferenceControl || image.NativeReferenceCalls || image.NativeTail != nil || len(image.Entry) != 1 || len(image.InternalEntry) != 1 || image.CodeImage != nil || pressure != 1 {
		t.Fatal("declaration-only production witness", pressure)
	}
	if len(image.Code) != 672 || sha256.Sum256(image.Code) != ([32]byte{0x85, 0x5d, 0xab, 0x7b, 0x1c, 0xec, 0x04, 0x4d, 0x22, 0x68, 0xf2, 0x85, 0xb3, 0x78, 0x54, 0x79, 0x12, 0x21, 0x26, 0xf4, 0x69, 0x28, 0xa7, 0x1c, 0xac, 0xfe, 0x37, 0xfc, 0x56, 0x41, 0x97, 0x58}) || image.Entry[0] != 0 || image.InternalEntry[0] != 0 {
		t.Fatal("declaration-only native code identity", len(image.Code), image.Entry, image.InternalEntry)
	}
}

func TestCore3RefDeclarationsWireProfileSeparation(t *testing.T) {
	debug := append(bytes.Clone(core3RefDeclarationsWire),
		0x00, 0x0b, 0x04, 0x6e, 0x61, 0x6d, 0x65, 0x04,
		0x04, 0x01, 0x00, 0x01, 0x74,
	)
	if len(debug) != 60 || fmt.Sprintf("%x", sha256.Sum256(debug)) != "8a3099a9c72a7064926f2e606f0b3fc720895f2ac64aec79244674c14afcfab5" {
		t.Fatal("canonical WABT debug-name witness changed")
	}
	mutations := map[string][]byte{
		"debug-name":      debug,
		"extern-nullable": bytes.Replace(bytes.Clone(core3RefDeclarationsWire), []byte{0x64, 0x6f}, []byte{0x63, 0x6f}, 1),
		"extern-heap":     bytes.Replace(bytes.Clone(core3RefDeclarationsWire), []byte{0x64, 0x6f}, []byte{0x64, 0x6e}, 1),
		"indexed-heap":    bytes.Replace(bytes.Clone(core3RefDeclarationsWire), []byte{0x64, 0x00}, []byte{0x64, 0x01}, 1),
		"binding":         bytes.Replace(bytes.Clone(core3RefDeclarationsWire), []byte{0x03, 0x02, 0x01, 0x01}, []byte{0x03, 0x02, 0x01, 0x00}, 1),
		"body":            bytes.Replace(bytes.Clone(core3RefDeclarationsWire), []byte{0x02, 0x00, 0x0b}, []byte{0x02, 0x01, 0x0b}, 1),
	}
	for name, source := range mutations {
		t.Run(name, func(t *testing.T) {
			checkGeneralDeclarationWire(t, source)
			if admitCore3RefDeclarationsWire(source) {
				t.Fatal("nearby wire module admitted")
			}
		})
	}
}

func TestCore3RefDeclarationsDecodedProfileSeparation(t *testing.T) {
	mutations := map[string]func(*wasm.Module){
		"extern-nullable": func(m *wasm.Module) {
			m.Types[1].SubTypes[0].Comp.Params[3] = wasm.ExternRef
		},
		"extern-heap": func(m *wasm.Module) {
			m.Types[1].SubTypes[0].Comp.Params[3] = wasm.RefVal(wasm.Ref(false, wasm.AbsHeap(wasm.HeapAny), false))
		},
		"indexed-heap": func(m *wasm.Module) {
			m.Types[1].SubTypes[0].Comp.Params[4] = wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 1}), false))
		},
		"result": func(m *wasm.Module) {
			m.Types[1].SubTypes[0].Comp.Results = []wasm.ValType{wasm.FuncRef}
		},
		"binding": func(m *wasm.Module) { m.FuncTypes[0].Index = 0 },
		"local":   func(m *wasm.Module) { m.Code[0].Locals.Runs = []wasm.LocalRun{{Count: 1, Type: wasm.ExternRef}} },
		"body":    func(m *wasm.Module) { m.Code[0].BodyBytes[0] = 0x01 },
		"rec-group": func(m *wasm.Module) {
			m.Types[1].SubTypes = append(m.Types[1].SubTypes, m.Types[1].SubTypes[0])
		},
		"descriptor": func(m *wasm.Module) {
			m.Types[1].SubTypes[0].Metadata.Descriptor = wasm.SomeTypeIdx(wasm.TypeIdx{})
		},
		"import": func(m *wasm.Module) {
			m.Imports = []wasm.Import{{Module: "env", Name: "f", Type: wasm.NewFuncExternType(wasm.TypeIdx{})}}
		},
		"export": func(m *wasm.Module) {
			m.Exports = []wasm.Export{{Name: "escape", Index: wasm.ExternIdx{Kind: wasm.ExternFunc}}}
		},
		"start":  func(m *wasm.Module) { start := wasm.FuncIdx(0); m.Start = &start },
		"custom": func(m *wasm.Module) { m.Customs = []wasm.CustomSec{{Name: "x"}} },
		"name":   func(m *wasm.Module) { m.NameSec = &wasm.NameSec{} },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			m := core3RefDeclarationsDecode(t)
			mutate(m)
			if admitCore3RefDeclarations(m) {
				t.Fatal("nearby decoded module inherited the exact declaration profile")
			}
			checkGeneralDeclarationModule(t, m)
		})
	}
}
