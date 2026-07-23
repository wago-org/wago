//go:build !tinygo

package wago

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

type customCarrierExtension struct{}

func (customCarrierExtension) Info() ExtensionInfo {
	return ExtensionInfo{ID: "test.custom-carriers", Version: "1.0.0", Stability: Experimental}
}

func (customCarrierExtension) Register(reg *Registry) error {
	reg.Capability(CapCompilerCodegen)
	compiler := reg.Compiler()
	for i, carrier := range []WasmType{WasmI32, WasmI64, WasmF32, WasmF64, WasmV128, WasmFuncRef, WasmExternRef} {
		name := fmt.Sprintf("test.value.%d", i)
		typ, err := compiler.Type(CustomTypeSpec{Name: name, Size: 32, Carrier: carrier})
		if err != nil {
			return err
		}
		again, err := compiler.Type(CustomTypeSpec{Name: name, Size: 32, Carrier: carrier})
		if err != nil || !again.Equal(typ) {
			return fmt.Errorf("idempotent registration for %s failed: %w", name, err)
		}
		if err := compiler.Instruction(InstructionSpec{
			Module: "test:custom", Name: name, Output: []int32{256},
			Custom: &CustomSignature{Output: &typ},
			AMD64: &AMD64InstructionLowering{
				Compatibility: AMD64CompatibilityFullAccess,
				Emit: func(ctx AMD64LoweringContext) error {
					reg := ctx.AllocYMM()
					ctx.Encoder().YPxor(reg, reg, reg)
					return ctx.OutputCustom(reg)
				},
			},
			ARM64: &ARM64InstructionLowering{
				Compatibility: ARM64CompatibilityFullAccess,
				Emit: func(ctx ARM64LoweringContext) error {
					a, b := ctx.AllocVector(), ctx.AllocVector()
					ctx.Encoder().Eor16b(a, a, a)
					ctx.Encoder().Eor16b(b, b, b)
					return ctx.OutputCustom(a, b)
				},
			},
		}); err != nil {
			return err
		}
		if err := compiler.Instruction(InstructionSpec{
			Module: "test:custom", Name: name + ".drop", Input: []int32{256},
			Custom: &CustomSignature{Inputs: []CustomType{typ}},
			AMD64: &AMD64InstructionLowering{
				Compatibility: AMD64CompatibilityFullAccess,
				Emit: func(ctx AMD64LoweringContext) error {
					regs, err := ctx.InputCustom(0)
					if err != nil {
						return err
					}
					for _, reg := range regs {
						ctx.ReleaseVector(reg)
					}
					return nil
				},
			},
			ARM64: &ARM64InstructionLowering{
				Compatibility: ARM64CompatibilityFullAccess,
				Emit: func(ctx ARM64LoweringContext) error {
					regs, err := ctx.InputCustom(0)
					if err != nil {
						return err
					}
					for _, reg := range regs {
						ctx.ReleaseVector(reg)
					}
					return nil
				},
			},
		}); err != nil {
			return err
		}
	}
	return nil
}

func TestCustomTypeCarriersCompileAndExecuteAsErasedValues(t *testing.T) {
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Skip("custom machine-code lowering is available on amd64 and arm64")
	}
	rt := NewRuntime()
	if err := rt.Use(customCarrierExtension{}); err != nil {
		t.Fatal(err)
	}
	carriers := []byte{0x7f, 0x7e, 0x7d, 0x7c, 0x7b, 0x70, 0x6f}
	for i, carrier := range carriers {
		t.Run(fmt.Sprintf("carrier-%x", carrier), func(t *testing.T) {
			mod, err := rt.Compile(customCarrierModule(fmt.Sprintf("test.value.%d", i), carrier))
			if err != nil {
				t.Fatal(err)
			}
			in, err := rt.Instantiate(context.Background(), mod)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if _, err := in.Invoke("run"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCustomTypeCarriersDeterminePhysicalSignatures(t *testing.T) {
	rt := NewRuntime()
	if err := rt.Use(customCarrierExtension{}); err != nil {
		t.Fatal(err)
	}
	want := []ValType{ValI32, ValI64, ValF32, ValF64, ValV128, ValFuncRef, ValExternRef}
	seen := make(map[string]ValType)
	for _, provided := range rt.ProvidedImports() {
		if provided.Module != "test:custom" || strings.HasSuffix(provided.Name, ".drop") {
			continue
		}
		if len(provided.Params) != 0 || len(provided.Results) != 1 {
			t.Fatalf("%s has signature %v -> %v", provided.Name, provided.Params, provided.Results)
		}
		seen[provided.Name] = provided.Results[0]
	}
	for i, carrier := range want {
		name := fmt.Sprintf("test.value.%d", i)
		if got, ok := seen[name]; !ok || got != carrier {
			t.Fatalf("%s carrier=%v, want %v", name, got, carrier)
		}
	}
}

func TestCustomTypeRegistrationRejectsConflictsAndForeignTokens(t *testing.T) {
	first := (&Registry{}).Compiler()
	typ, err := first.Type(CustomTypeSpec{Name: "test.value", Size: 32, Carrier: WasmI32})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Type(CustomTypeSpec{Name: "test.value", Size: 32, Carrier: WasmI64}); err == nil {
		t.Fatal("conflicting carrier accepted")
	}

	second := (&Registry{}).Compiler()
	if err := second.Instruction(InstructionSpec{
		Module: "test", Name: "foreign", Output: []int32{256},
		Custom: &CustomSignature{Output: &typ},
		AMD64: &AMD64InstructionLowering{
			Compatibility: AMD64CompatibilityFullAccess,
			Emit:          func(AMD64LoweringContext) error { return nil },
		},
	}); err == nil {
		t.Fatal("custom type token from another registry accepted")
	}
}

func customCarrierModule(name string, carrier byte) []byte {
	produce := []byte{0x60, 0, 1, carrier}
	consume := []byte{0x60, 1, carrier, 0}
	run := wasmtest.FuncType(nil, nil)
	importFunc := func(importName string, typeIndex uint32) []byte {
		out := append(wasmtest.Name("test:custom"), wasmtest.Name(importName)...)
		out = append(out, 0)
		return append(out, wasmtest.ULEB(typeIndex)...)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(produce, consume, run)),
		wasmtest.Section(2, wasmtest.Vec(importFunc(name, 0), importFunc(name+".drop", 1))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x10, 1, 0x0b}))),
	)
}
