//go:build arm64 && !tinygo

package arm64

import (
	"errors"
	"testing"

	plugincodegen "github.com/wago-org/wago/codegen/arm64"
	railcore "github.com/wago-org/wago/src/core/compiler/backend/railshot"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/plugins"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func customPluginInputModuleARM64(t *testing.T) *wasm.Module {
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

func TestCustomPluginInputsRejectScalarAccessARM64(t *testing.T) {
	typ, err := plugins.PrepareCustomType(plugins.CustomTypeSpec{Name: "test.vector", Size: 16, Carrier: plugins.WasmI32})
	if err != nil {
		t.Fatal(err)
	}
	producer := &plugincodegen.Lowering{Compatibility: plugincodegen.CompatibilityFullAccess, Emit: func(ctx plugincodegen.Context) error {
		return ctx.OutputCustom(ctx.AllocVector())
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
		{"CheckedMemory", &plugincodegen.Lowering{Compatibility: plugincodegen.CompatibilityFullAccess, Emit: func(ctx plugincodegen.Context) error {
			if _, index, _, err := ctx.CheckedMemory(0, 0, 4); err == nil {
				ctx.ReleaseGP(index)
				return errors.New("CheckedMemory accepted a custom input")
			}
			return consumeValidInputs(ctx)
		}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cm, err := CompileModuleWith(customPluginInputModuleARM64(t), CompileOptions{
				DeferCodeMapping: true,
				CustomInstructions: map[uint32]railcore.CustomInstruction{
					0: {Codegen: producer, ResultWidth: 128, CustomOutput: &typ},
					1: {Codegen: tc.consumer, InputWidths: []int32{128, 32}, CustomInputs: []plugins.CustomType{typ, {}}},
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
