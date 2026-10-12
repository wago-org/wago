//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"crypto/sha256"
	"errors"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestStateOnlyProductionHasNoCodeImage(t *testing.T) {
	vector := append([]byte{0xfd, 0x0c}, make([]byte, 16)...)
	vector = append(vector, 0x0b)
	for name, source := range map[string][]byte{
		"empty":         wasmtest.Module(),
		"scalar-types":  wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil)))),
		"vector-types":  wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128}, nil)))),
		"scalar-global": wasmtest.Module(wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 7, 0x0b}))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 3, 0)))),
		"vector-global": wasmtest.Module(wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.V128, false, vector))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 3, 0)))),
		"vector-import": wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "g", wasm.V128, false))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 3, 0)))),
		"memory-data":   wasmtest.Module(wasmtest.Section(5, []byte{1, 1, 1, 1}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("memory", 2, 0))), wasmtest.Section(11, []byte{1, 0, 0x41, 0, 0x0b, 1, 42})),
	} {
		t.Run(name, func(t *testing.T) {
			if err := AdmitProductionSource(source); err != nil {
				t.Fatal(err)
			}
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal(err)
			}
			for _, deferred := range []bool{false, true} {
				image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: deferred, Interruptible: true})
				if err != nil || image == nil {
					t.Fatal("state-only compilation", err)
				}
				if image.CodeImage != nil || len(image.Code) != 0 || len(image.Entry) != 0 || len(image.InternalEntry) != 0 {
					t.Fatal("state-only compilation allocated a code image")
				}
			}
			if image, err := Compile(source); image != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("private scalar shape widened", err)
			}
			if err := admitSWARDecodedBudget(m); !errors.Is(err, ErrUnsupported) {
				t.Fatal("private SWAR shape widened", err)
			}
		})
	}
}

func mutableFuncrefGetSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("is_null", 0, 0), wasmtest.ExportEntry("cell", 3, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0, 0xd1, 0x0b}))),
	)
}

func mutableFuncrefClearSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("clear", 0, 0), wasmtest.ExportEntry("cell", 3, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd0, 0x70, 0x24, 0, 0x0b}))),
	)
}

func mutableFuncrefStoreSelfSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("store", 0, 0), wasmtest.ExportEntry("cell", 3, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}))),
	)
}

func mutableFuncrefLocalCopySource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "target", wasm.FuncRef, true))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.FuncRef, true, []byte{0xd2, 0, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("copy_local", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 1, 0x24, 0, 0x0b}))),
	)
}

func mutableFuncrefLocalPublishSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "target", wasm.FuncRef, true))),
		wasmtest.Section(3, []byte{6, 0, 0, 0, 0, 0, 0}),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.FuncRef, true, []byte{0xd0, 0x70, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target_0", 0, 0),
			wasmtest.ExportEntry("target_1", 0, 1),
			wasmtest.ExportEntry("set_local_0", 0, 2),
			wasmtest.ExportEntry("set_local_1", 0, 3),
			wasmtest.ExportEntry("clear_local", 0, 4),
			wasmtest.ExportEntry("publish", 0, 5),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x0b}),
			wasmtest.Code([]byte{0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 1, 0x0b}),
			wasmtest.Code([]byte{0xd2, 1, 0x24, 1, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0x70, 0x24, 1, 0x0b}),
			wasmtest.Code([]byte{0x23, 1, 0x24, 0, 0x0b}),
		)),
	)
}

func TestMutableFuncrefLocalPublishProduction(t *testing.T) {
	source := mutableFuncrefLocalPublishSource()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitMutableFunctionGlobalLocalPublish(m) {
		t.Fatal("exact mutable funcref local-publish source rejected")
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, SyncHostSlots: 64})
	if err != nil || image == nil {
		t.Fatal("mutable funcref local-publish compilation", err)
	}
	wantDigest := [sha256.Size]byte{0x35, 0x00, 0x75, 0x19, 0x17, 0x1f, 0x68, 0xb6, 0xb2, 0x80, 0x24, 0x55, 0xc0, 0x3b, 0x9f, 0x28, 0xbd, 0x0d, 0x6c, 0x5d, 0x3d, 0xf1, 0x48, 0x9c, 0xd7, 0x29, 0x15, 0x4a, 0x54, 0x12, 0x83, 0x46}
	wantEntry := []int{0, 208, 416, 720, 1024, 1248}
	if len(image.Code) != 1756 || sha256.Sum256(image.Code) != wantDigest || !slices.Equal(image.Entry, wantEntry) || !slices.Equal(image.InternalEntry, wantEntry) {
		t.Fatalf("mutable funcref local-publish compilation drifted: code=%d digest=%x entries=%v/%v", len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry)
	}
}

func TestMutableFuncrefLocalPublishProductionRejectsBroaderSources(t *testing.T) {
	for name, mutate := range map[string]func(*wasm.Module){
		"import-name":    func(m *wasm.Module) { m.Imports[0].Name = "other" },
		"local-nonnull":  func(m *wasm.Module) { m.Globals[0].Init.BodyBytes = []byte{0xd2, 0, 0x0b} },
		"set-other-func": func(m *wasm.Module) { m.Code[2].BodyBytes = []byte{0xd2, 2, 0x24, 1, 0x0b} },
		"clear-import":   func(m *wasm.Module) { m.Code[4].BodyBytes = []byte{0xd0, 0x70, 0x24, 0, 0x0b} },
		"reverse-copy":   func(m *wasm.Module) { m.Code[5].BodyBytes = []byte{0x23, 0, 0x24, 1, 0x0b} },
		"wrong-export":   func(m *wasm.Module) { m.Exports[5].Name = "other" },
		"global-export": func(m *wasm.Module) {
			m.Exports = append(m.Exports, wasm.Export{Name: "cell", Index: wasm.ExternIdx{Kind: wasm.ExternGlobal, Index: 1}})
		},
	} {
		t.Run(name, func(t *testing.T) {
			m, err := wasm.DecodeModule(mutableFuncrefLocalPublishSource())
			if err != nil {
				t.Fatal(err)
			}
			mutate(m)
			if admitMutableFunctionGlobalLocalPublish(m) {
				t.Fatal("broader mutable funcref local-publish source matched exact profile")
			}
			staged := false
			image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, SyncHostSlots: 64, MemoryPressure: func() { staged = true }})
			if image != nil || !errors.Is(err, ErrUnsupported) || staged {
				t.Fatal("broader mutable funcref local-publish source reached staging", image, err, staged)
			}
		})
	}
}

func TestMutableFuncrefLocalCopyProduction(t *testing.T) {
	source := mutableFuncrefLocalCopySource()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitMutableFunctionGlobalLocalCopy(m) {
		t.Fatal("exact mutable funcref local-copy source rejected")
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, SyncHostSlots: 64})
	if err != nil || image == nil {
		t.Fatal("mutable funcref local-copy compilation", err)
	}
	wantDigest := [sha256.Size]byte{0xa7, 0x01, 0xd4, 0xfd, 0x30, 0x9a, 0x26, 0x13, 0xea, 0x26, 0x8c, 0x5a, 0xa0, 0xd7, 0xd7, 0x06, 0x32, 0xe7, 0xbd, 0xd8, 0x1c, 0x5e, 0x2b, 0xdd, 0xc8, 0x6e, 0xf6, 0x1e, 0xf1, 0x1d, 0x64, 0xdd}
	if len(image.Code) != 508 || sha256.Sum256(image.Code) != wantDigest || !slices.Equal(image.Entry, []int{0}) || !slices.Equal(image.InternalEntry, []int{0}) {
		t.Fatalf("mutable funcref local-copy compilation drifted: code=%d digest=%x entries=%v/%v", len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry)
	}
}

func TestMutableFuncrefLocalCopyProductionRejectsBroaderSources(t *testing.T) {
	module := func(imports, globals, exports, body []byte) []byte {
		return wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(2, imports),
			wasmtest.Section(3, []byte{1, 0}),
			wasmtest.Section(6, globals),
			wasmtest.Section(7, exports),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
		)
	}
	exactImports := wasmtest.Vec(wasmtest.GlobalImportEntry("env", "target", wasm.FuncRef, true))
	exactGlobals := wasmtest.Vec(wasmtest.GlobalEntry(wasm.FuncRef, true, []byte{0xd2, 0, 0x0b}))
	exactExports := wasmtest.Vec(wasmtest.ExportEntry("copy_local", 0, 0))
	exactBody := []byte{0x23, 1, 0x24, 0, 0x0b}
	for name, source := range map[string][]byte{
		"import-source": module(exactImports, exactGlobals, exactExports, []byte{0x23, 0, 0x24, 0, 0x0b}),
		"reverse":       module(exactImports, exactGlobals, exactExports, []byte{0x23, 0, 0x24, 1, 0x0b}),
		"null-local":    module(exactImports, wasmtest.Vec(wasmtest.GlobalEntry(wasm.FuncRef, true, []byte{0xd0, 0x70, 0x0b})), exactExports, exactBody),
		"immutable":     module(exactImports, wasmtest.Vec(wasmtest.GlobalEntry(wasm.FuncRef, false, []byte{0xd2, 0, 0x0b})), exactExports, exactBody),
		"wrong-import": module(
			wasmtest.Vec(wasmtest.GlobalImportEntry("env", "other", wasm.FuncRef, true)), exactGlobals, exactExports, exactBody,
		),
		"wrong-export": module(exactImports, exactGlobals, wasmtest.Vec(wasmtest.ExportEntry("other", 0, 0)), exactBody),
	} {
		t.Run(name, func(t *testing.T) {
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal("fixture decoding", err)
			}
			if admitMutableFunctionGlobalLocalCopy(m) {
				t.Fatal("broader mutable funcref local-copy source matched exact profile")
			}
			staged := false
			image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, SyncHostSlots: 64, MemoryPressure: func() { staged = true }})
			if image != nil || !errors.Is(err, ErrUnsupported) || staged {
				t.Fatal("broader mutable funcref local-copy source reached staging", image, err, staged)
			}
		})
	}
}

func mutableFuncrefCopySource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(
			wasmtest.GlobalImportEntry("env", "source", wasm.FuncRef, true),
			wasmtest.GlobalImportEntry("env", "target", wasm.FuncRef, true),
		)),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("copy", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0, 0x24, 1, 0x0b}))),
	)
}

func TestMutableFuncrefCopyProduction(t *testing.T) {
	source := mutableFuncrefCopySource()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitMutableFunctionGlobalCopy(m) {
		t.Fatal("exact mutable funcref copy source rejected")
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, SyncHostSlots: 64})
	if err != nil || image == nil {
		t.Fatal("mutable funcref copy compilation", err)
	}
	wantDigest := [sha256.Size]byte{0x5c, 0xb5, 0x6b, 0x10, 0x57, 0xfe, 0xd2, 0xfc, 0x47, 0x29, 0x2c, 0xb9, 0x97, 0x60, 0xa6, 0xd6, 0x55, 0x43, 0xb5, 0xe5, 0x2e, 0x09, 0xdb, 0x42, 0xb9, 0xcb, 0x46, 0xf9, 0xf0, 0x02, 0x02, 0x1a}
	if len(image.Code) != 508 || sha256.Sum256(image.Code) != wantDigest || len(image.Entry) != 1 || image.Entry[0] != 0 || len(image.InternalEntry) != 1 || image.InternalEntry[0] != 0 {
		t.Fatalf("mutable funcref copy compilation drifted: code=%d digest=%x entries=%v/%v", len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry)
	}
}

func TestMutableFuncrefCopyProductionRejectsBroaderSources(t *testing.T) {
	module := func(imports, exports, body []byte) []byte {
		return wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(2, imports),
			wasmtest.Section(3, []byte{1, 0}),
			wasmtest.Section(7, exports),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
		)
	}
	exactImports := wasmtest.Vec(
		wasmtest.GlobalImportEntry("env", "source", wasm.FuncRef, true),
		wasmtest.GlobalImportEntry("env", "target", wasm.FuncRef, true),
	)
	exactExports := wasmtest.Vec(wasmtest.ExportEntry("copy", 0, 0))
	for name, source := range map[string][]byte{
		"reverse":      module(exactImports, exactExports, []byte{0x23, 1, 0x24, 0, 0x0b}),
		"same-cell":    module(exactImports, exactExports, []byte{0x23, 0, 0x24, 0, 0x0b}),
		"null":         module(exactImports, exactExports, []byte{0xd0, 0x70, 0x24, 1, 0x0b}),
		"wrong-export": module(exactImports, wasmtest.Vec(wasmtest.ExportEntry("other", 0, 0)), []byte{0x23, 0, 0x24, 1, 0x0b}),
		"wrong-name": module(wasmtest.Vec(
			wasmtest.GlobalImportEntry("env", "other", wasm.FuncRef, true),
			wasmtest.GlobalImportEntry("env", "target", wasm.FuncRef, true),
		), exactExports, []byte{0x23, 0, 0x24, 1, 0x0b}),
	} {
		t.Run(name, func(t *testing.T) {
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal("fixture decoding", err)
			}
			if admitMutableFunctionGlobalCopy(m) {
				t.Fatal("broader mutable funcref copy source matched exact profile")
			}
			staged := false
			image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, SyncHostSlots: 64, MemoryPressure: func() { staged = true }})
			if image != nil || !errors.Is(err, ErrUnsupported) || staged {
				t.Fatal("broader mutable funcref copy source reached staging", image, err, staged)
			}
		})
	}
}

func mutableFuncrefMoveSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(
			wasmtest.GlobalImportEntry("env", "source", wasm.FuncRef, true),
			wasmtest.GlobalImportEntry("env", "target", wasm.FuncRef, true),
		)),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("move", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0, 0x24, 1, 0xd0, 0x70, 0x24, 0, 0x0b}))),
	)
}

func mutableFuncrefBidirectionalCopySource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(
			wasmtest.GlobalImportEntry("env", "source", wasm.FuncRef, true),
			wasmtest.GlobalImportEntry("env", "target", wasm.FuncRef, true),
		)),
		wasmtest.Section(3, []byte{2, 0, 0}),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("copy_to_target", 0, 0),
			wasmtest.ExportEntry("copy_to_source", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x23, 0, 0x24, 1, 0x0b}),
			wasmtest.Code([]byte{0x23, 1, 0x24, 0, 0x0b}),
		)),
	)
}

func TestMutableFuncrefBidirectionalCopyProduction(t *testing.T) {
	source := mutableFuncrefBidirectionalCopySource()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitMutableFunctionGlobalBidirectionalCopy(m) {
		t.Fatal("exact mutable funcref bidirectional copy source rejected")
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, SyncHostSlots: 64})
	if err != nil || image == nil {
		t.Fatal("mutable funcref bidirectional copy compilation", err)
	}
	wantDigest := [sha256.Size]byte{0xb2, 0xb7, 0x62, 0xca, 0x55, 0x2d, 0x4f, 0x9b, 0x4b, 0x69, 0xaa, 0x27, 0x86, 0x30, 0xca, 0xe0, 0xe9, 0x39, 0x2d, 0x86, 0x73, 0x70, 0xd9, 0xeb, 0x85, 0x61, 0xfa, 0x98, 0x7f, 0x25, 0x23, 0x30}
	if len(image.Code) != 812 || sha256.Sum256(image.Code) != wantDigest || !slices.Equal(image.Entry, []int{0, 288}) || !slices.Equal(image.InternalEntry, []int{0, 288}) {
		t.Fatalf("mutable funcref bidirectional copy compilation drifted: code=%d digest=%x entries=%v/%v", len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry)
	}
}

func TestMutableFuncrefBidirectionalCopyProductionRejectsBroaderSources(t *testing.T) {
	module := func(exports, first, second []byte) []byte {
		return wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(2, wasmtest.Vec(
				wasmtest.GlobalImportEntry("env", "source", wasm.FuncRef, true),
				wasmtest.GlobalImportEntry("env", "target", wasm.FuncRef, true),
			)),
			wasmtest.Section(3, []byte{2, 0, 0}),
			wasmtest.Section(7, exports),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(first), wasmtest.Code(second))),
		)
	}
	exactExports := wasmtest.Vec(
		wasmtest.ExportEntry("copy_to_target", 0, 0),
		wasmtest.ExportEntry("copy_to_source", 0, 1),
	)
	forward := []byte{0x23, 0, 0x24, 1, 0x0b}
	reverse := []byte{0x23, 1, 0x24, 0, 0x0b}
	for name, source := range map[string][]byte{
		"two-forward":       module(exactExports, forward, forward),
		"two-reverse":       module(exactExports, reverse, reverse),
		"swapped-functions": module(exactExports, reverse, forward),
		"wrong-first-export": module(wasmtest.Vec(
			wasmtest.ExportEntry("other", 0, 0),
			wasmtest.ExportEntry("copy_to_source", 0, 1),
		), forward, reverse),
		"swapped-exports": module(wasmtest.Vec(
			wasmtest.ExportEntry("copy_to_source", 0, 1),
			wasmtest.ExportEntry("copy_to_target", 0, 0),
		), forward, reverse),
	} {
		t.Run(name, func(t *testing.T) {
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal("fixture decoding", err)
			}
			if admitMutableFunctionGlobalBidirectionalCopy(m) {
				t.Fatal("broader bidirectional copy source matched exact profile")
			}
			staged := false
			image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, SyncHostSlots: 64, MemoryPressure: func() { staged = true }})
			if image != nil || !errors.Is(err, ErrUnsupported) || staged {
				t.Fatal("broader bidirectional copy source reached staging", image, err, staged)
			}
		})
	}
}

func TestMutableFuncrefMoveProduction(t *testing.T) {
	source := mutableFuncrefMoveSource()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitMutableFunctionGlobalMove(m) {
		t.Fatal("exact mutable funcref move source rejected")
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, SyncHostSlots: 64})
	if err != nil || image == nil {
		t.Fatal("mutable funcref move compilation", err)
	}
	wantDigest := [sha256.Size]byte{0xfb, 0x01, 0xdd, 0x72, 0x40, 0xc5, 0x09, 0x91, 0x19, 0x19, 0xe9, 0x0d, 0x46, 0xc0, 0xf2, 0x00, 0x45, 0xe5, 0xf6, 0x15, 0x8b, 0xc9, 0x11, 0x53, 0x18, 0xf0, 0x2a, 0xff, 0xbb, 0xf0, 0xf0, 0x16}
	if len(image.Code) != 764 || sha256.Sum256(image.Code) != wantDigest || len(image.Entry) != 1 || image.Entry[0] != 0 || len(image.InternalEntry) != 1 || image.InternalEntry[0] != 0 {
		t.Fatalf("mutable funcref move compilation drifted: code=%d digest=%x entries=%v/%v", len(image.Code), sha256.Sum256(image.Code), image.Entry, image.InternalEntry)
	}
}

func TestMutableFuncrefMoveProductionRejectsBroaderSources(t *testing.T) {
	module := func(exports, body []byte) []byte {
		return wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(2, wasmtest.Vec(
				wasmtest.GlobalImportEntry("env", "source", wasm.FuncRef, true),
				wasmtest.GlobalImportEntry("env", "target", wasm.FuncRef, true),
			)),
			wasmtest.Section(3, []byte{1, 0}),
			wasmtest.Section(7, exports),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
		)
	}
	exactExports := wasmtest.Vec(wasmtest.ExportEntry("move", 0, 0))
	for name, source := range map[string][]byte{
		"copy-only":    module(exactExports, []byte{0x23, 0, 0x24, 1, 0x0b}),
		"clear-only":   module(exactExports, []byte{0xd0, 0x70, 0x24, 0, 0x0b}),
		"clear-first":  module(exactExports, []byte{0xd0, 0x70, 0x24, 0, 0x23, 0, 0x24, 1, 0x0b}),
		"wrong-clear":  module(exactExports, []byte{0x23, 0, 0x24, 1, 0xd0, 0x70, 0x24, 1, 0x0b}),
		"wrong-export": module(wasmtest.Vec(wasmtest.ExportEntry("other", 0, 0)), []byte{0x23, 0, 0x24, 1, 0xd0, 0x70, 0x24, 0, 0x0b}),
	} {
		t.Run(name, func(t *testing.T) {
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal("fixture decoding", err)
			}
			if admitMutableFunctionGlobalMove(m) {
				t.Fatal("broader mutable funcref move source matched exact profile")
			}
			staged := false
			image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, SyncHostSlots: 64, MemoryPressure: func() { staged = true }})
			if image != nil || !errors.Is(err, ErrUnsupported) || staged {
				t.Fatal("broader mutable funcref move source reached staging", image, err, staged)
			}
		})
	}
}

func TestMutableFuncrefStoreSelfProduction(t *testing.T) {
	source := mutableFuncrefStoreSelfSource()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitMutableFunctionGlobalSetSelf(m) {
		t.Fatal("exact mutable funcref self-store source rejected")
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, SyncHostSlots: 64})
	if err != nil || image == nil {
		t.Fatal("mutable funcref self-store compilation", err)
	}
	wantDigest := [sha256.Size]byte{0xef, 0xf3, 0x3e, 0xca, 0xb0, 0x53, 0x9a, 0xa8, 0xfd, 0x85, 0xc6, 0x7d, 0x62, 0xb2, 0xd8, 0x72, 0x59, 0x04, 0xca, 0x2c, 0x42, 0x24, 0x95, 0xed, 0xe1, 0x9e, 0x1f, 0x36, 0x72, 0xa5, 0x56, 0x91}
	if len(image.Code) != 556 || sha256.Sum256(image.Code) != wantDigest || len(image.Entry) != 1 || image.Entry[0] != 0 || len(image.InternalEntry) != 1 || image.InternalEntry[0] != 0 {
		t.Fatal("mutable funcref self-store compilation drifted")
	}
}

func TestMutableFuncrefStoreSelfProductionRejectsBroaderSources(t *testing.T) {
	module := func(exports, body []byte, functions ...[]byte) []byte {
		if len(functions) == 0 {
			functions = [][]byte{body}
		}
		funcTypes := make([]byte, 1+len(functions))
		funcTypes[0] = byte(len(functions))
		return wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
			wasmtest.Section(3, funcTypes),
			wasmtest.Section(7, exports),
			wasmtest.Section(10, wasmtest.Vec(functions...)),
		)
	}
	exactExports := wasmtest.Vec(wasmtest.ExportEntry("store", 0, 0), wasmtest.ExportEntry("cell", 3, 0))
	differentTarget := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(
			wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true),
			wasmtest.GlobalImportEntry("env", "other", wasm.FuncRef, true),
		)),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, exactExports),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd2, 0, 0x24, 1, 0x0b}))),
	)
	for name, source := range map[string][]byte{
		"copy-existing": module(exactExports, wasmtest.Code([]byte{0x23, 0, 0x24, 0, 0x0b})),
		"mixed-null":    module(exactExports, wasmtest.Code([]byte{0xd2, 0, 0x1a, 0xd0, 0x70, 0x24, 0, 0x0b})),
		"other-target":  differentTarget,
		"wrong-export": module(
			wasmtest.Vec(wasmtest.ExportEntry("other", 0, 0), wasmtest.ExportEntry("cell", 3, 0)),
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}),
		),
		"extra-function": module(
			exactExports,
			nil,
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0x0b}),
		),
	} {
		t.Run(name, func(t *testing.T) {
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal("fixture decoding", err)
			}
			if admitMutableFunctionGlobalSetSelf(m) {
				t.Fatal("broader mutable funcref self-store source matched exact profile")
			}
			staged := false
			image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, SyncHostSlots: 64, MemoryPressure: func() { staged = true }})
			if image != nil || !errors.Is(err, ErrUnsupported) || staged {
				t.Fatal("broader mutable funcref self-store source reached staging", image, err, staged)
			}
		})
	}
}

func TestMutableFuncrefClearProduction(t *testing.T) {
	source := mutableFuncrefClearSource()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitMutableFunctionGlobalClear(m) {
		t.Fatal("exact mutable funcref clear source rejected")
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, SyncHostSlots: 64})
	if err != nil || image == nil {
		t.Fatal("mutable funcref clear compilation", err)
	}
	wantDigest := [sha256.Size]byte{0x66, 0x4e, 0x06, 0x68, 0xf4, 0xa4, 0x78, 0x6c, 0x56, 0xef, 0xdd, 0x55, 0x1e, 0xba, 0x69, 0x4e, 0x22, 0x39, 0x04, 0xb5, 0x75, 0x0b, 0x58, 0x2f, 0x60, 0x34, 0x73, 0xae, 0xe7, 0xaf, 0xf8, 0xf8}
	if len(image.Code) != 476 || sha256.Sum256(image.Code) != wantDigest || len(image.Entry) != 1 || image.Entry[0] != 0 || len(image.InternalEntry) != 1 || image.InternalEntry[0] != 0 {
		t.Fatal("mutable funcref clear compilation drifted")
	}
}

func TestMutableFuncrefClearProductionRejectsBroaderSources(t *testing.T) {
	module := func(exports, body []byte, functions ...[]byte) []byte {
		if len(functions) == 0 {
			functions = [][]byte{body}
		}
		funcTypes := make([]byte, 1+len(functions))
		funcTypes[0] = byte(len(functions))
		return wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
			wasmtest.Section(3, funcTypes),
			wasmtest.Section(7, exports),
			wasmtest.Section(10, wasmtest.Vec(functions...)),
		)
	}
	exactExports := wasmtest.Vec(wasmtest.ExportEntry("clear", 0, 0), wasmtest.ExportEntry("cell", 3, 0))
	for name, source := range map[string][]byte{
		"copy-existing":  module(exactExports, wasmtest.Code([]byte{0x23, 0, 0x24, 0, 0x0b})),
		"store-non-null": module(exactExports, wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x0b})),
		"wrong-export": module(
			wasmtest.Vec(wasmtest.ExportEntry("other", 0, 0), wasmtest.ExportEntry("cell", 3, 0)),
			wasmtest.Code([]byte{0xd0, 0x70, 0x24, 0, 0x0b}),
		),
		"extra-function": module(
			exactExports,
			nil,
			wasmtest.Code([]byte{0xd0, 0x70, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0x0b}),
		),
	} {
		t.Run(name, func(t *testing.T) {
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal("fixture decoding", err)
			}
			if admitMutableFunctionGlobalClear(m) {
				t.Fatal("broader mutable funcref clear source matched exact profile")
			}
			staged := false
			image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, SyncHostSlots: 64, MemoryPressure: func() { staged = true }})
			if image != nil || !errors.Is(err, ErrUnsupported) || staged {
				t.Fatal("broader mutable funcref clear source reached staging", image, err, staged)
			}
		})
	}
}

func TestMutableFuncrefGetProduction(t *testing.T) {
	if err := AdmitProductionSource(mutableFuncrefGetSource()); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(mutableFuncrefGetSource())
	if err != nil {
		t.Fatal(err)
	}
	if !admitMutableFunctionGlobalGet(m) {
		t.Fatal("exact mutable funcref get source rejected")
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true})
	if err != nil || image == nil {
		t.Fatal("mutable funcref get compilation", err)
	}
	wantDigest := [sha256.Size]byte{0x4d, 0x22, 0xfb, 0x69, 0xe1, 0x32, 0x2a, 0xe2, 0x30, 0xc0, 0x3c, 0x22, 0x84, 0x14, 0x88, 0xe8, 0x76, 0xcb, 0xb4, 0x90, 0xf9, 0x33, 0xbc, 0x57, 0xe1, 0x7b, 0xa4, 0xdd, 0xaf, 0x4f, 0x86, 0xd0}
	if len(image.Code) != 244 || sha256.Sum256(image.Code) != wantDigest || len(image.Entry) != 1 || image.Entry[0] != 0 || len(image.InternalEntry) != 1 || image.InternalEntry[0] != 0 {
		t.Fatal("mutable funcref get compilation drifted")
	}
}

func TestMutableFuncrefGetProductionRejectsBroaderSources(t *testing.T) {
	funcrefResult := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("is_null", 0, 0), wasmtest.ExportEntry("cell", 3, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0, 0x0b}))),
	)
	globalSet := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("is_null", 0, 0), wasmtest.ExportEntry("cell", 3, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd0, 0x70, 0x24, 0, 0x41, 0, 0x0b}))),
	)
	extraFunction := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
		wasmtest.Section(3, []byte{2, 0, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("is_null", 0, 0), wasmtest.ExportEntry("cell", 3, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0, 0xd1, 0x0b}), wasmtest.Code([]byte{0x41, 0, 0x0b}))),
	)
	extraGlobal := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, 0, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("is_null", 0, 0), wasmtest.ExportEntry("cell", 3, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0, 0xd1, 0x0b}))),
	)
	wrongExports := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("other", 0, 0), wasmtest.ExportEntry("cell", 3, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0, 0xd1, 0x0b}))),
	)
	withTable := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(4, []byte{1, 0x70, 0, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("is_null", 0, 0), wasmtest.ExportEntry("cell", 3, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0, 0xd1, 0x0b}))),
	)
	withStart := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("is_null", 0, 0), wasmtest.ExportEntry("cell", 3, 0))),
		wasmtest.Section(8, []byte{0}),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0, 0xd1, 0x0b}))),
	)
	for name, source := range map[string][]byte{
		"global-set":     globalSet,
		"raw-funcref":    funcrefResult,
		"extra-function": extraFunction,
		"extra-global":   extraGlobal,
		"wrong-exports":  wrongExports,
		"table":          withTable,
		"start":          withStart,
	} {
		t.Run(name, func(t *testing.T) {
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal("fixture decoding", err)
			}
			if admitMutableFunctionGlobalGet(m) {
				t.Fatal("broader mutable funcref source matched exact profile")
			}
			staged := false
			image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, MemoryPressure: func() { staged = true }})
			if image != nil || !errors.Is(err, ErrUnsupported) || staged {
				t.Fatal("broader mutable funcref source reached staging", image, err, staged)
			}
		})
	}
}

func TestStateOnlyProductionStillValidatesState(t *testing.T) {
	for _, source := range [][]byte{
		wasmtest.Module(wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("bad", 0, 0)))),
		wasmtest.Module(wasmtest.Section(8, []byte{0})),
		wasmtest.Module(wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{0x23, 0, 0x0b})))),
	} {
		m, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal("fixture decoding", err)
		}
		staged := false
		image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, MemoryPressure: func() { staged = true }})
		if err == nil || image != nil || staged {
			t.Fatal("invalid functionless state reached staging", err)
		}
	}
}

func stateOnlyMutableFuncrefImportSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("cell", 3, 0))),
	)
}

func TestStateOnlyMutableFuncrefImportProduction(t *testing.T) {
	source := stateOnlyMutableFuncrefImportSource()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true})
	if err != nil || image == nil {
		t.Fatal("state-only mutable funcref compilation", err)
	}
	if image.CodeImage != nil || len(image.Code) != 0 || len(image.Entry) != 0 || len(image.InternalEntry) != 0 {
		t.Fatal("state-only mutable funcref allocated executable code")
	}

	for name, bad := range map[string][]byte{
		"no-export": wasmtest.Module(
			wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
		),
		"extra-import": wasmtest.Module(
			wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true), wasmtest.GlobalImportEntry("env", "count", wasm.I32, true))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("cell", 3, 0))),
		),
		"table": wasmtest.Module(
			wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "cell", wasm.FuncRef, true))),
			wasmtest.Section(4, []byte{1, 0x70, 1, 1, 1}),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("cell", 3, 0))),
		),
	} {
		t.Run(name, func(t *testing.T) {
			decoded, err := wasm.DecodeModule(bad)
			if err != nil {
				t.Fatal(err)
			}
			image, err := CompileModuleWith(decoded, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true})
			if image != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("unsupported mutable funcref state source admitted", err)
			}
		})
	}
}
