//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"crypto/sha256"
	"errors"
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
