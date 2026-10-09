//go:build !tinygo

package wago

import (
	"context"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

type exactInstructionPlugin struct{}

func (exactInstructionPlugin) Register(r *Registrar) error {
	instructions, err := r.CompilerInstructions()
	if err != nil {
		return err
	}
	return instructions.Define(InstructionSpec{
		Module: "a.b", Name: "c", Input: []int32{32}, Output: []int32{32},
		Handler: func(_ InstructionContext, args []Bits) ([]Bits, error) {
			value, _ := BitsFromUint32(32, args[0].Uint32()+1)
			return []Bits{value}, nil
		},
		Lower: func(c LoweringContext) error {
			c.Output(0, c.Add(c.Input(0), c.Const(32, 1)))
			return nil
		},
	})
}

func TestInstructionDoesNotReplaceDistinctImport(t *testing.T) {
	scope := AuthorityScope{Modules: []string{"a.b"}}
	definition := PluginDefinition{
		ID: "example.com/exact-instruction", Version: "1.0.0",
		Provenance:  PluginProvenance{Repository: "https://example.com/exact-instruction", License: "MIT"},
		Authorities: []AuthorityRequest{{Name: AuthorityCompilerInstructionDefine, Mode: AuthorityRequired, Reason: "test", Scope: scope}},
	}
	digest, err := DefinitionDigest(definition)
	if err != nil {
		t.Fatal(err)
	}
	set := PluginSet{
		Providers: []PluginProvider{{Definition: definition, New: func() Plugin { return exactInstructionPlugin{} }}},
		Selections: []PluginSelection{{ID: definition.ID, DefinitionDigest: digest, Direct: true,
			Grants: []AuthorityGrant{{Name: AuthorityCompilerInstructionDefine, Scope: scope}}}},
	}
	rt := NewRuntime()
	defer rt.CloseContext(context.Background())
	if err := rt.LoadPlugins(context.Background(), set); err != nil {
		t.Fatal(err)
	}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(instructionFuncImport("a", "b.c", 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x00, 0x0b}))),
	)
	compiled, err := rt.Compile(module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	calls := 0
	imports := NewImports()
	imports.HostFunc("a", "b.c", func(int32) int32 { calls++; return 99 })
	instance, err := rt.Instantiate(context.Background(), compiled, WithImports(imports))
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	out, err := instance.Invoke("f", I32(7))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || AsI32(out[0]) != 99 || calls != 1 {
		t.Fatalf("result=%v, host calls=%d; want 99 and one host call", out, calls)
	}
}
