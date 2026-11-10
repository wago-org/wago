//go:build amd64

package amd64

import (
	"testing"

	plugincodegen "github.com/wago-org/wago/codegen/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/plugins"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func managedPluginBoundaryModuleAMD64(t *testing.T) *wasm.Module {
	t.Helper()
	importFunc := func(name string, typeIndex uint32) []byte {
		out := append(wasmtest.Name("env"), wasmtest.Name(name)...)
		return append(append(out, 0), wasmtest.ULEB(typeIndex)...)
	}
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
		)),
		wasmtest.Section(2, wasmtest.Vec(
			importFunc("normal", 0),
			importFunc("produce", 1),
			importFunc("consume", 2),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x10, 0, // call normal
			0x10, 1, // call produce
			0x10, 2, // call consume
			0x0b,
		}))),
	)
	m, err := wasm.DecodeModule(b)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestManagedPluginContextDoesNotExposeFullAMD64Context(t *testing.T) {
	typ, err := plugins.PrepareCustomType(plugins.CustomTypeSpec{Name: "test.boundary", Size: 16, Carrier: plugins.WasmI32})
	if err != nil {
		t.Fatal(err)
	}
	var normalCalled, customCalled, exposed bool
	normal := &plugincodegen.Lowering{
		Compatibility: plugincodegen.CompatibilityManaged,
		Managed: func(ctx plugincodegen.ManagedContext) error {
			normalCalled = true
			_, exposed = ctx.(plugincodegen.Context)
			return nil
		},
	}
	producer := &plugincodegen.Lowering{
		Compatibility: plugincodegen.CompatibilityFullAccess,
		Emit: func(ctx plugincodegen.Context) error {
			return ctx.OutputCustom(ctx.AllocYMM())
		},
	}
	consumer := &plugincodegen.Lowering{
		Compatibility: plugincodegen.CompatibilityManaged,
		Managed: func(ctx plugincodegen.ManagedContext) error {
			customCalled = true
			if _, ok := ctx.(plugincodegen.Context); ok {
				exposed = true
			}
			regs, err := ctx.InputCustom(0)
			if err != nil {
				return err
			}
			for _, reg := range regs {
				ctx.ReleaseVector(reg)
			}
			return nil
		},
	}
	cm, err := CompileModuleWith(managedPluginBoundaryModuleAMD64(t), CompileOptions{Workers: 1, CustomInstructions: map[uint32]CustomInstruction{
		0: {Codegen: normal},
		1: {Codegen: producer, ResultWidth: 128, CustomOutput: &typ},
		2: {Codegen: consumer, InputWidths: []int32{128}, CustomInputs: []plugins.CustomType{typ}},
	}})
	if cm != nil && cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	if !normalCalled || !customCalled {
		t.Fatalf("managed callbacks called: normal=%v custom=%v", normalCalled, customCalled)
	}
	if exposed {
		t.Fatal("managed plugin context exposes the full AMD64 encoder context")
	}
}
