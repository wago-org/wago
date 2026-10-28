//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	rv "github.com/wago-org/wago/codegen/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestRV64PublicPluginMachineCode(t *testing.T) {
	calls := 0
	ext := instructionMachineExt{name: "i4.add", input: []int32{4, 4}, output: []int32{4}, portableCalls: &calls, lowering: &rv.Lowering{Compatibility: rv.CompatibilityFullAccess, Emit: func(ctx rv.Context) error {
		a, err := ctx.InputI32(0)
		if err != nil {
			return err
		}
		b, err := ctx.InputI32(1)
		if err != nil {
			return err
		}
		ctx.Encoder().Add(a, a, b)
		ctx.ReleaseGP(b)
		return ctx.OutputI32(a)
	}}}
	in := instantiateMachineInstruction(t, ext)
	for _, args := range [][2]uint64{{0, 0}, {15, 15}, {0xfeedface0000000e, 0xa5a5a5a500000007}} {
		got, err := in.Invoke("run", args[0], args[1])
		want := (args[0] + args[1]) & 15
		if err != nil || len(got) != 1 || got[0] != want {
			t.Fatal("native i4 plugin", args, got, err)
		}
	}
	if calls != 0 {
		t.Fatal("native plugin called portable handler", calls)
	}
}
func TestRV64PublicPluginManagedIdentity(t *testing.T) {
	calls := 0
	ext := instructionMachineExt{name: "i5.identity", input: []int32{5}, output: []int32{5}, portableCalls: &calls, lowering: &rv.Lowering{Compatibility: rv.CompatibilityManaged, Managed: func(ctx rv.ManagedContext) error {
		r, err := ctx.InputI32(0)
		if err != nil {
			return err
		}
		return ctx.OutputI32(r)
	}}}
	in := instantiateMachineInstruction(t, ext)
	got, err := in.Invoke("run", 0xfffffffffffffff7)
	if err != nil || len(got) != 1 || got[0] != 23 || calls != 0 {
		t.Fatal("native managed identity", got, calls, err)
	}
}

type rv64CustomCodegenExtension struct{}

func (rv64CustomCodegenExtension) Info() ExtensionInfo {
	return ExtensionInfo{ID: "test.rv64-custom-codegen", Version: "1.0.0", Stability: Experimental}
}
func (rv64CustomCodegenExtension) Register(reg *Registry) error {
	reg.Capability(CapCompilerCodegen)
	compiler := reg.Compiler()
	typ, err := compiler.Type(CustomTypeSpec{Name: "test.rv64-native16", Size: 16, Carrier: WasmV128})
	if err != nil {
		return err
	}
	if err := compiler.Instruction(InstructionSpec{Module: "rv64-custom", Name: "new", Output: []int32{128}, Custom: &CustomSignature{Output: &typ}, Codegen: &rv.Lowering{Compatibility: rv.CompatibilityFullAccess, Emit: func(ctx rv.Context) error {
		a, b := ctx.AllocGP(), ctx.AllocGP()
		ctx.Encoder().Constant64(a, 0x1122334455667788)
		ctx.Encoder().Constant64(b, 0x123456789abcdef0)
		return ctx.OutputCustom(a, b)
	}}}); err != nil {
		return err
	}
	return compiler.Instruction(InstructionSpec{Module: "rv64-custom", Name: "low", Input: []int32{128}, Output: []int32{32}, Custom: &CustomSignature{Inputs: []CustomType{typ}}, Codegen: &rv.Lowering{Compatibility: rv.CompatibilityFullAccess, Emit: func(ctx rv.Context) error {
		regs, err := ctx.InputCustom(0)
		if err != nil {
			return err
		}
		ctx.ReleaseGP(regs[1])
		return ctx.OutputI32(regs[0])
	}}})
}
func TestRV64PublicPluginCustomV128Carrier(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	if err := rt.Use(rv64CustomCodegenExtension{}); err != nil {
		t.Fatal(err)
	}
	source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}), wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(2, wasmtest.Vec(instructionFuncImport("rv64-custom", "new", 0), instructionFuncImport("rv64-custom", "low", 1))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x10, 1, 0x0b}))))
	mod, err := rt.Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	got, err := in.Invoke("run")
	if err != nil || len(got) != 1 || got[0] != 0x55667788 {
		t.Fatal("complete custom carrier", got, err)
	}
}
