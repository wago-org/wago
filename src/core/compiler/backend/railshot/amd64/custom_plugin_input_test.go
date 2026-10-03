//go:build amd64 && !tinygo

package amd64

import (
	"errors"
	"testing"

	plugincodegen "github.com/wago-org/wago/codegen/amd64"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/plugins"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func customPluginInputModuleAMD64(t *testing.T) *wasm.Module {
	t.Helper()
	importFunc := func(name string, typeIndex uint32) []byte {
		out := append(wasmtest.Name("test"), wasmtest.Name(name)...)
		return append(append(out, 0), wasmtest.ULEB(typeIndex)...)
	}
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil),
			wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(importFunc("produce", 0), importFunc("consume", 1))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x41, 7, 0x10, 1, 0x0b}))),
	)
	m, err := wasm.DecodeModule(b)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCustomPluginInputsRejectScalarAccessAMD64(t *testing.T) {
	typ, err := plugins.PrepareCustomType(plugins.CustomTypeSpec{Name: "test.vector", Size: 32, Carrier: plugins.WasmI32})
	if err != nil {
		t.Fatal(err)
	}
	producer := &plugincodegen.Lowering{Compatibility: plugincodegen.CompatibilityFullAccess, Features: plugincodegen.FeatureAVX2, Emit: func(ctx plugincodegen.Context) error {
		return ctx.OutputCustom(ctx.AllocYMM())
	}}
	consumeValidInputs := func(ctx plugincodegen.ManagedContext) error {
		scalar, err := ctx.InputI32(1)
		if err != nil {
			return err
		}
		ctx.ReleaseGP(scalar)
		custom, err := ctx.InputCustom(0)
		if err != nil {
			return err
		}
		for _, reg := range custom {
			ctx.ReleaseVector(reg)
		}
		return nil
	}
	tests := []struct {
		name     string
		consumer *plugincodegen.Lowering
	}{
		{"InputI32", &plugincodegen.Lowering{Compatibility: plugincodegen.CompatibilityManaged, Managed: func(ctx plugincodegen.ManagedContext) error {
			if reg, err := ctx.InputI32(0); err == nil {
				ctx.ReleaseGP(reg)
				return errors.New("InputI32 accepted a custom input")
			}
			return consumeValidInputs(ctx)
		}}},
		{"LoadYMM", &plugincodegen.Lowering{Compatibility: plugincodegen.CompatibilityManaged, Managed: func(ctx plugincodegen.ManagedContext) error {
			if reg, err := ctx.LoadYMM(0, 0); err == nil {
				ctx.ReleaseVector(reg)
				return errors.New("LoadYMM accepted a custom input as a memory address")
			}
			return consumeValidInputs(ctx)
		}}},
		{"CheckedMemory", &plugincodegen.Lowering{Compatibility: plugincodegen.CompatibilityFullAccess, Features: plugincodegen.FeatureAVX2, Emit: func(ctx plugincodegen.Context) error {
			if _, index, _, err := ctx.CheckedMemory(0, 0, 4); err == nil {
				ctx.ReleaseGP(index)
				return errors.New("CheckedMemory accepted a custom input")
			}
			return consumeValidInputs(ctx)
		}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cm, err := CompileModuleWith(customPluginInputModuleAMD64(t), CompileOptions{
				AMD64FeaturesSet: true, AMD64Features: shared.AMD64AVX | shared.AMD64AVX2,
				DeferCodeMapping: true,
				CustomInstructions: map[uint32]CustomInstruction{
					0: {Codegen: producer, ResultWidth: 256, CustomOutput: &typ},
					1: {Codegen: tc.consumer, InputWidths: []int32{256, 32}, CustomInputs: []plugins.CustomType{typ, {}}},
				},
			})
			if cm != nil && cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
