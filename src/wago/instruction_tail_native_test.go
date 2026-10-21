//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo

package wago

import (
	"context"
	"fmt"
	"runtime"
	"testing"

	"github.com/wago-org/wago/codegen"
	x64 "github.com/wago-org/wago/codegen/amd64"
	a64 "github.com/wago-org/wago/codegen/arm64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func instructionTailModule(module, name string, arity int, result, trapPrefix bool) []byte {
	params := make([]wasm.ValType, arity)
	for i := range params {
		params[i] = wasm.I32
	}
	var results []wasm.ValType
	if result {
		results = []wasm.ValType{wasm.I32}
	}
	args := []byte{}
	for i := range arity {
		args = append(args, 0x20)
		args = append(args, wasmtest.ULEB(uint32(i))...)
	}
	tail := []byte{0x41, 11} // the tail discards this live operand prefix
	if trapPrefix {
		tail = []byte{0x20, 0, 0x20, 1, 0x6e} // a discarded division must still trap
	}
	tail = append(tail, args...)
	tail = append(tail, 0x12, 0, 0x0b)
	nested := append(append([]byte{}, args...), 0x10, 1)
	if result {
		nested = append(nested, 0x41, 7, 0x6a)
	}
	nested = append(nested, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results))),
		wasmtest.Section(2, wasmtest.Vec(instructionFuncImport(module, name, 0))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1), wasmtest.ExportEntry("nested", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tail), wasmtest.Code(nested))),
	)
}

func instantiateTailInstruction(t testing.TB, ext Extension, module []byte) *Instance {
	t.Helper()
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)))
	t.Cleanup(func() { rt.Close() })
	if err := rt.Use(ext); err != nil {
		t.Fatal(err)
	}
	mod, err := rt.Compile(module)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { mod.Close() })
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { in.Close() })
	return in
}

func TestCustomImportedTailRecipe(t *testing.T) {
	for _, portable := range []bool{false, true} {
		t.Run(fmt.Sprintf("portable%t", portable), func(t *testing.T) {
			calls := 0
			ext := instructionRecipeExt{name: "tail", input: []int32{4, 4}, output: 4, portableCalls: &calls,
				lower: func(c LoweringContext) error { c.Output(0, c.Sub(c.Input(1), c.Input(0))); return nil },
				eval:  func(v []uint32) uint32 { return (v[1] - v[0]) & 15 },
			}
			if portable {
				ext.lower = nil
			}
			in := instantiateTailInstruction(t, ext, instructionTailModule("wago:instr/recipe", ext.name, 2, true, false))
			for _, export := range []string{"run", "nested"} {
				for _, values := range [][2]int32{{1, 7}, {-1, 3}, {0x7fffffff, -1}} {
					want := uint32(values[1]-values[0]) & 15
					if export == "nested" {
						want += 7
					}
					out, err := in.Invoke(export, I32(values[0]), I32(values[1]))
					if err != nil || len(out) != 1 || uint32(out[0]) != want {
						t.Fatalf("%s %v: %v, %v; want %d", export, values, out, err, want)
					}
				}
			}
			wantCalls := 0
			if portable {
				wantCalls = 6
			}
			if calls != wantCalls {
				t.Fatalf("handler calls=%d, want %d", calls, wantCalls)
			}
		})
	}
}

func TestCustomImportedTailPreservesPrefixTrap(t *testing.T) {
	calls := 0
	ext := instructionRecipeExt{name: "tail-trap", input: []int32{32, 32}, output: 32, portableCalls: &calls,
		lower: func(c LoweringContext) error { c.Output(0, c.Input(1)); return nil },
		eval:  func(v []uint32) uint32 { return v[1] },
	}
	in := instantiateTailInstruction(t, ext, instructionTailModule("wago:instr/recipe", ext.name, 2, true, true))
	if _, err := in.Invoke("run", I32(8), I32(0)); err == nil {
		t.Fatal("discarded division did not trap")
	}
	if calls != 0 {
		t.Fatal("handler ran before the prefix trap")
	}
	if out, err := in.Invoke("nested", I32(8), I32(2)); err != nil || len(out) != 1 || out[0] != 9 {
		t.Fatalf("nested nontrapping call: %v, %v; want 9", out, err)
	}
}

func tailMachineLowering(result bool) codegen.Lowering {
	if runtime.GOARCH == "amd64" {
		return &x64.Lowering{Compatibility: x64.CompatibilityManaged, Managed: func(c x64.ManagedContext) error {
			if !result {
				return nil
			}
			value, err := c.InputI32(0)
			if err != nil {
				return err
			}
			return c.OutputI32(value)
		}}
	}
	return &a64.Lowering{Compatibility: a64.CompatibilityManaged, Managed: func(c a64.ManagedContext) error {
		if !result {
			return nil
		}
		value, err := c.InputI32(0)
		if err != nil {
			return err
		}
		return c.OutputI32(value)
	}}
}

func TestCustomImportedTailMachineABI(t *testing.T) {
	for _, result := range []bool{false, true} {
		for _, arity := range []int{0, 1, 8, 17, 64} {
			if result && arity == 0 {
				continue
			}
			t.Run(fmt.Sprintf("arity%d/result%t", arity, result), func(t *testing.T) {
				calls := 0
				ext := instructionMachineExt{name: "tail-machine", input: make([]int32, arity), portableCalls: &calls, lowering: tailMachineLowering(result)}
				for i := range ext.input {
					ext.input[i] = 32
				}
				if result {
					ext.output = []int32{32}
				}
				in := instantiateTailInstruction(t, ext, instructionTailModule("wago:instr/machine", ext.name, arity, result, false))
				args := make([]uint64, arity)
				for i := range args {
					args[i] = uint64(i + 35)
				}
				for _, export := range []string{"run", "nested"} {
					out, err := in.Invoke(export, args...)
					if err != nil {
						t.Fatal(err)
					}
					if result {
						want := uint64(35)
						if export == "nested" {
							want += 7
						}
						if len(out) != 1 || out[0] != want {
							t.Fatalf("%s result=%v, want %d", export, out, want)
						}
					} else if len(out) != 0 {
						t.Fatalf("void result=%v", out)
					}
				}
				if calls != 0 {
					t.Fatalf("handler called %d times", calls)
				}
			})
		}
	}
}

func TestCustomImportedTailRecipeBudgetFallback(t *testing.T) {
	calls := 0
	ext := sharedInstructionRecipe(8, &calls)
	in := instantiateTailInstruction(t, ext, instructionTailModule("wago:instr/recipe", ext.name, 1, true, false))
	if out, err := in.Invoke("run", I32(3)); err != nil || len(out) != 1 || out[0] != 768 {
		t.Fatalf("result %v, %v; want 768", out, err)
	}
	wantCalls := 0
	if runtime.GOARCH == "arm64" {
		wantCalls = 1
	}
	if calls != wantCalls {
		t.Fatalf("handler calls=%d, want %d", calls, wantCalls)
	}
}

func TestCustomImportedTailPreservesExactImportIdentity(t *testing.T) {
	scope := AuthorityScope{Modules: []string{"a.b"}}
	definition := PluginDefinition{ID: "example.com/tail-exact", Version: "1.0.0",
		Provenance:  PluginProvenance{Repository: "https://example.com/tail-exact", License: "MIT"},
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
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)))
	defer rt.Close()
	if err := rt.LoadPlugins(context.Background(), set); err != nil {
		t.Fatal(err)
	}
	mod, err := rt.Compile(instructionTailModule("a", "b.c", 1, true, false))
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	calls := 0
	imports := NewImports()
	imports.HostFunc("a", "b.c", func(int32) int32 { calls++; return 99 })
	in, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if out, err := in.Invoke("run", I32(7)); err != nil || len(out) != 1 || out[0] != 99 || calls != 1 {
		t.Fatalf("result=%v, %v; host calls=%d; want99/one call", out, err, calls)
	}
}

func TestCustomImportedTailDiscardsGCFrame(t *testing.T) {
	calls := 0
	ext := instructionRecipeExt{name: "tail-gc", input: []int32{32}, output: 32, portableCalls: &calls,
		lower: func(c LoweringContext) error { c.Output(0, c.Add(c.Input(0), c.Const(32, 1))); return nil },
		eval:  func(v []uint32) uint32 { return v[0] + 1 },
	}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 0}, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(instructionFuncImport("wago:instr/recipe", ext.name, 1))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		// The allocated object is a discarded operand prefix at the proper tail.
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfb, 1, 0, 0x20, 0, 0x12, 0, 0x0b}))),
	)
	in := instantiateTailInstruction(t, ext, module)
	for i := uint64(0); i < 64; i++ {
		out, err := in.Invoke("run", i)
		if err != nil || len(out) != 1 || out[0] != i+1 {
			t.Fatalf("iteration%d: %v, %v", i, out, err)
		}
		if err := in.CollectGC(); err != nil {
			t.Fatal(err)
		}
		if live := in.gc.Stats().LiveObjects; live != 0 {
			t.Fatalf("tail frame retained %d objects", live)
		}
	}
	if calls != 0 || in.gc.Stats().Allocations != 64 {
		t.Fatalf("handler calls=%d, GC allocations=%d; want0/64", calls, in.gc.Stats().Allocations)
	}
}
