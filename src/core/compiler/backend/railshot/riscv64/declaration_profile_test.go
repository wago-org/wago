//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func authenticateI31TestModule(t testing.TB, m *wasm.Module, opts CompileOptions) CompileOptions {
	t.Helper()
	opts.I31CoreSource = admitRV64I31Core(m)
	opts.GeneralI31Source = AdmitGeneralI31Module(m)
	opts.GCRefEqSource = admitRV64GCRefEq(m)
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	opts.Codegen.Module = codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}
	return opts
}

// Former private-profile near misses can be valid public modules. Keep their
// validation and publication checks rather than retaining private feature caps.
func checkPublicPrimitiveModule(t *testing.T, m *wasm.Module, opts CompileOptions) {
	t.Helper()
	opts = authenticateI31TestModule(t, m, opts)
	invalid := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{CompactImports: true, ExtendedConstGlobals: true, GCConstExpr: opts.I31CoreSource, MultiMemory: true}, 1, wasm.ValidationLimits{MaxFunctionLocals: publicFunctionLocalLimit(opts)}) != nil
	for _, element := range m.Elements {
		invalid = invalid || len(element.Kind.Funcs) != 0 && len(element.Kind.Exprs) != 0 || len(element.Mode.Offset.Instrs) != 0 || element.Mode.Kind != wasm.ElemActive && len(element.Mode.Offset.BodyBytes) != 0
		for _, expr := range element.Kind.Exprs {
			invalid = invalid || len(expr.Instrs) != 0
		}
	}
	for _, global := range m.Globals {
		invalid = invalid || len(global.Init.Instrs) != 0
	}
	if len(opts.ImportBindings) == 0 && m.ImportedFuncCount() != 0 {
		opts.ImportBindings = hostImportBindings(m.ImportedFuncCount())
		opts.SyncHostCalls = true
	}
	pressure := 0
	opts.DeferCodeMapping = true
	opts.MemoryPressure = func() { pressure++ }
	image, err := CompileModuleWith(m, opts)
	if invalid {
		if image != nil || err == nil || pressure != 0 {
			t.Fatal("invalid public module reached staging", err, pressure)
		}
		return
	}
	if err != nil || image == nil || image.CodeImage != nil || len(image.Entry) != len(m.Code) || len(image.InternalEntry) != len(m.Code) || pressure != 1 {
		t.Fatal("valid public module failed compilation", err, pressure)
	}
	for i, entry := range image.Entry {
		if entry < 0 || entry >= len(image.Code) || entry&3 != 0 || image.InternalEntry[i] != entry {
			t.Fatal("invalid public function entry", i, entry)
		}
	}
}

// Exact fixture recognizers remain separate from general production support.
// Source admission is a policy for validated modules, not another decoder.
func checkGeneralDeclarationWire(t *testing.T, source []byte) {
	t.Helper()
	m, err := wasm.DecodeModule(source)
	if err != nil {
		return
	}
	if err = wasm.ValidateModule(m); err != nil {
		return
	}
	if err = AdmitProductionSource(source); err != nil {
		t.Fatal("valid declaration source rejected", err)
	}
}

func checkGeneralDeclarationModule(t *testing.T, m *wasm.Module) {
	t.Helper()
	// Public compilation validates before entering this low-level backend.
	if err := wasm.ValidateModule(m); err != nil {
		return
	}
	canonicalInitializers := true
	for _, element := range m.Elements {
		if len(element.Mode.Offset.Instrs) != 0 || element.Mode.Kind != wasm.ElemActive && len(element.Mode.Offset.BodyBytes) != 0 {
			canonicalInitializers = false
		}
	}
	pressure := 0
	opts := closedReferenceProductionOptions(false, true)
	opts.MemoryPressure = func() { pressure++ }
	for _, im := range m.Imports {
		if im.Type.Kind == wasm.ExternFunc {
			opts.ImportBindings = append(opts.ImportBindings, ImportBinding{Dynamic: true, ImportIndex: uint32(len(opts.ImportBindings))})
		}
	}
	opts.SyncHostCalls = len(opts.ImportBindings) != 0
	image, err := CompileModuleWith(m, opts)
	if err != nil {
		if image != nil || pressure != 0 {
			t.Fatal("failed declaration compile published an image", err, pressure)
		}
		// Collector-bearing declarations need the separate exact-root contract.
		if canonicalInitializers && (admitPublicPrimitiveReferenceModule(m, false) || admitPublicPrimitiveReferenceModule(m, true)) {
			t.Fatal("valid primitive declaration rejected", err)
		}
		return
	}
	if image == nil || len(image.Code) == 0 || image.CodeImage != nil || pressure != 1 || len(image.Entry) != len(m.Code) || len(image.InternalEntry) != len(m.Code) {
		t.Fatal("invalid deferred declaration image", pressure)
	}
	for i, entry := range image.Entry {
		if entry < 0 || entry >= len(image.Code) || entry&3 != 0 || image.InternalEntry[i] != entry {
			t.Fatal("invalid declaration function entry", i, entry)
		}
	}
}

func checkGeneralI31Module(t *testing.T, m *wasm.Module, opts CompileOptions) {
	t.Helper()
	checkPublicPrimitiveModule(t, m, opts)
}

func checkGeneralI31Wire(t *testing.T, source []byte) {
	t.Helper()
	m, err := wasm.DecodeModule(source)
	valid := err == nil && wasm.ValidateModuleWithFeatures(m, wasm.ValidationFeatures{ExtendedConstGlobals: true, GCConstExpr: true, MultiMemory: true}) == nil
	if !valid {
		if AdmitI31CoreSource(source) {
			t.Fatal("invalid i31 source was admitted")
		}
		return
	}
	if !AdmitI31CoreSource(source) {
		t.Fatal("valid immediate-reference source was not admitted")
	}
	checkGeneralI31Module(t, m, CompileOptions{Interruptible: true, InvocationContext: true, ForeignTableCalls: true})
}
