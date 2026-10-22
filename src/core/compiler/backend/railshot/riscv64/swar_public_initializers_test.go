//go:build linux && !tinygo

package riscv64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func swarImportedInitializerModule(t *testing.T) *wasm.Module {
	t.Helper()
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "vector", wasm.V128, false), wasmtest.GlobalImportEntry("env", "offset", wasm.I32, false))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(5, []byte{1, 1, 1, 1}),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.V128, true, []byte{0x23, 0, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 0))),
		wasmtest.Section(12, []byte{2}),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0, 0x41, 0, 0x41, 1, 0xfc, 8, 1, 0, 0x23, 2, 0x0b}))),
		wasmtest.Section(11, wasmtest.Vec([]byte{0, 0x23, 1, 0x0b, 1, 42}, []byte{1, 1, 43})),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestSWARPublicImportedInitializers(t *testing.T) {
	m := swarImportedInitializerModule(t)
	image, err := compileSWARDecodedModuleProfile(m, emissionMode{production: true, interruptible: true}, swarModuleHosts, swarStateProfile{publicInitializers: true, publicStarts: true, exports: true, importedGlobals: true, importedMemory: true})
	if err != nil {
		t.Fatal(err)
	}
	if !image.publicInitializers || len(image.globals) != 0 || len(image.data) != 0 || len(image.activeData) != 0 {
		t.Fatal("public initializer payload duplicated")
	}
	job := new(coreruntime.JobMemory)
	if owner, err := image.instantiateGlobals(job); owner != nil || err == nil {
		t.Fatal("public image bound private globals", err)
	}
	calls := 0
	if owner, err := image.instantiateDataWithArena(job, func(int) (*coreruntime.Arena, error) { calls++; return nil, nil }); owner != nil || err == nil || calls != 0 {
		t.Fatal("public image allocated private data", err, calls)
	}
	if image, err := compileSWARDecodedModule(m, emissionMode{production: true, interruptible: true}, swarModuleHosts); image != nil || err == nil {
		t.Fatal("private initializer gate widened")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*wasm.Module)
	}{
		{"mutable", func(m *wasm.Module) {
			m.Imports[0].Type = wasm.NewGlobalExternType(wasm.GlobalType{Type: wasm.V128, Mutable: true})
		}},
		{"wrong-type", func(m *wasm.Module) { m.Globals[0].Init.BodyBytes = []byte{0x23, 1, 0x0b} }},
		{"local", func(m *wasm.Module) { m.Globals[0].Init.BodyBytes = []byte{0x23, 2, 0x0b} }},
		{"extended", func(m *wasm.Module) { m.Globals[0].Init.BodyBytes = []byte{0x23, 0, 0x23, 0, 0xfd, 81, 0x0b} }},
		{"dual", func(m *wasm.Module) { m.Globals[0].Init.Instrs = []wasm.Instruction{{Kind: wasm.InstrGlobalGet}} }},
		{"truncated", func(m *wasm.Module) { m.Globals[0].Init.BodyBytes = []byte{0x23, 0x80} }},
		{"oversized", func(m *wasm.Module) {
			m.Globals[0].Init.BodyBytes = []byte{0x23, 0x80, 0x80, 0x80, 0x80, 0, 0x0b, 0x0b}
		}},
		{"data-wrong-type", func(m *wasm.Module) { m.Data[0].Mode.Offset.BodyBytes = []byte{0x23, 0, 0x0b} }},
		{"data-no-memory", func(m *wasm.Module) { m.Memories = nil }},
		{"passive-offset", func(m *wasm.Module) { m.Data[1].Mode.Offset.BodyBytes = []byte{0x23, 1, 0x0b} }},
		{"global-bound", func(m *wasm.Module) { m.Globals = make([]wasm.Global, 255) }},
		{"data-index", func(m *wasm.Module) { m.Data[0].Mode.Mem = 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := swarImportedInitializerModule(t)
			tc.mutate(bad)
			pressure := 0
			image, err := CompileModuleWith(bad, CompileOptions{DeferCodeMapping: true, Interruptible: true, MemoryPressure: func() { pressure++ }})
			if image != nil || err == nil || pressure != 0 {
				t.Fatal("unsupported initializer staged", err, pressure)
			}
		})
	}
}
