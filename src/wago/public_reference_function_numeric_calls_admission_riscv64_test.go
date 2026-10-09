//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64PublicReferenceFunctionNumericCallsMarkerAdmission(t *testing.T) {
	numeric := []wasm.ValType{wasm.I32}
	source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(numeric, numeric), wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{wasm.FuncRef}))), wasmtest.Section(3, []byte{3, 0, 0, 1}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))), wasmtest.Section(9, []byte{1, 3, 0, 1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0xd2, 0, 0x14, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x0b}))))
	c := rv64NativeTailCompile(t, "reference-numeric-call-only-marker", source, false)
	if !c.nativeReferenceCallABI || !c.nativeReferenceFunctionABI || c.nativeReferenceControlABI || c.dynamicFuncrefEscape || rv64NativeTailModule(c) != nil {
		t.Fatal("ordinary-call-only private witness")
	}
	for name, mutate := range map[string]func(*Compiled){
		"missing-call":      func(v *Compiled) { v.nativeReferenceCallABI = false },
		"missing-private":   func(v *Compiled) { v.nativeReferenceFunctionABI = false },
		"missing-typed":     func(v *Compiled) { v.requiredFeatures &^= CoreFeatureTypedFunctionReferences },
		"missing-reference": func(v *Compiled) { v.nativeTableReferenceABI = false },
		"missing-context":   func(v *Compiled) { v.nativeTableContextABI = false },
		"descriptor-extent": func(v *Compiled) { v.FuncTypeID = nil },
		"private-export":    func(v *Compiled) { v.Exports["run"] = 2 },
		"dynamic-escape":    func(v *Compiled) { v.dynamicFuncrefEscape = true },
		"canonical-binding": func(v *Compiled) { v.FuncTypeID[0] = 1 },
		"nonfinal":          func(v *Compiled) { v.Types[0].Final = false },
	} {
		t.Run(name, func(t *testing.T) {
			v := cloneCompiledMetadata(c)
			v.codeCache = nil
			v.validateMemo = &validateMemo{}
			v.ensureCodeCache()
			v.codeCache.stagedFeatures = CoreFeatureTypedFunctionReferences
			mutate(v)
			if err := railshotValidateCompiled(v); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("bad joint reference witness accepted", err)
			}
			blob, err := marshalCompiled(v)
			if err != nil {
				return
			}
			loaded, err := LoadTrustedArtifact(blob)
			if loaded != nil {
				loaded.Close()
			}
			if err == nil {
				t.Fatal("bad joint witness artifact loaded")
			}
		})
	}
	if allocs := testing.AllocsPerRun(5, func() {
		if err := railshotValidateReferenceCalls(c); err != nil {
			panic(err)
		}
		if err := rv64ValidateReferenceFunctions(c); err != nil {
			panic(err)
		}
	}); allocs != 0 {
		t.Fatal("joint reference admission allocation", allocs)
	}
	rt := NewRuntime()
	defer rt.Close()
	mod, err := rt.Module(c)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	in, err := rt.Instantiate(context.Background(), mod)
	if in != nil {
		in.Close()
	}
	if in != nil || !errors.Is(err, railshot.ErrUnsupported) || c.loadCodeCache().base != 0 || c.loadCodeCache().sealed || c.loadCodeCache().refs != 0 {
		t.Fatal("default destination published joint private reference image", err)
	}
}
