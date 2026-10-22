//go:build linux && !tinygo

package riscv64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/plugins"
)

func TestRootedCustomSourcePreservesOrdinaryReferenceBindings(t *testing.T) {
	custom, err := plugins.PrepareCustomType(plugins.CustomTypeSpec{Name: "rooted-custom", Size: 32, Carrier: plugins.WasmFuncRef})
	if err != nil {
		t.Fatal(err)
	}
	module := func() *wasm.Module {
		m := widePluginTestModule(wasm.FuncRef, []byte{0x10, 0, 0x10, 1, 0x0b})
		m.Types = append(m.Types, wasm.RecType{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompStruct, Fields: []wasm.FieldType{wasm.NewFieldType(wasm.StorageVal(wasm.I32), wasm.Const)}}}}})
		return m
	}
	directory := widePluginTestInstructions(custom)
	m := module()
	if !AdmitGCRootedScalarCustomModule(m, directory) {
		t.Fatal("validated inline custom imports were rejected")
	}
	if !wasm.EqualValType(m.Types[0].SubTypes[0].Comp.Results[0], wasm.FuncRef) {
		t.Fatal("custom admission changed the original type graph")
	}
	ordinary := module()
	ordinary.FuncTypes = append(ordinary.FuncTypes, wasm.TypeIdx{Index: 0})
	ordinary.Code = append(ordinary.Code, wasm.Func{BodyBytes: []byte{0xd0, 0x70, 0x0b}})
	if !AdmitGCRootedScalarCustomModule(ordinary, directory) {
		t.Fatal("valid ordinary reference definition was rejected")
	}
	if rootedCustomImportType(ordinary, pluginInstructions(directory), 0) || !wasm.EqualValType(ordinary.Types[0].SubTypes[0].Comp.Results[0], wasm.FuncRef) {
		t.Fatal("ordinary reference definition inherited custom erasure")
	}
	invalid := widePluginTestInstructions(custom)
	producer := invalid[0]
	producer.ResultWidth--
	invalid[0] = producer
	if AdmitGCRootedScalarCustomModule(module(), invalid) {
		t.Fatal("invalid custom width was admitted")
	}
}
