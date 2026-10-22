//go:build !tinygo

package wago

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func sharedInstructionRecipe(steps int, calls *int) instructionRecipeExt {
	return instructionRecipeExt{name: "shared-budget", input: []int32{32}, output: 32, portableCalls: calls,
		lower: func(c LoweringContext) error {
			value := c.Input(0)
			for range steps {
				value = c.Add(value, value)
			}
			c.Output(0, value)
			return nil
		},
		eval: func(v []uint32) uint32 { return v[0] << uint(steps) },
	}
}

func TestCustomInstructionRecipeBudgetARM64(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("ARM64 recipe admission budget")
	}
	for _, steps := range []int{1, 4, 7, 8, 12} {
		t.Run(fmt.Sprintf("shared%d", steps), func(t *testing.T) {
			calls := 0
			ext := sharedInstructionRecipe(steps, &calls)
			in := instantiateInstructionRecipe(t, ext)
			defer in.Close()
			for _, value := range []uint32{0, 1, 0x12345678, 0xffffffff} {
				out, err := in.Invoke("run", I32(int32(value)))
				if err != nil || len(out) != 1 || uint32(out[0]) != value<<uint(steps) {
					t.Fatalf("input %#x: %v, %v", value, out, err)
				}
			}
			wantCalls := 0
			if steps >= 8 {
				wantCalls = 4
			}
			if calls != wantCalls {
				t.Fatalf("handler calls=%d, want %d", calls, wantCalls)
			}
			t.Logf("code=%d bytes, handler calls=%d", in.c.CodeSize(), calls)
		})
	}
	for _, shape := range []string{"depth", "nodes"} {
		t.Run(shape, func(t *testing.T) {
			calls := 0
			ext := instructionRecipeExt{name: "budget-" + shape, input: []int32{32}, output: 32, portableCalls: &calls}
			ext.lower = func(c LoweringContext) error {
				value := c.Input(0)
				if shape == "depth" {
					for range 40 {
						value = c.Add(value, c.Const(32, 1))
					}
				} else {
					for range 128 {
						c.Const(32, 0)
					}
				}
				c.Output(0, value)
				return nil
			}
			ext.eval = func(v []uint32) uint32 {
				if shape == "depth" {
					return v[0] + 40
				}
				return v[0]
			}
			in := instantiateInstructionRecipe(t, ext)
			defer in.Close()
			out, err := in.Invoke("run", I32(-1))
			if want := ext.eval([]uint32{0xffffffff}); err != nil || len(out) != 1 || uint32(out[0]) != want {
				t.Fatalf("result %v, %v; want %#x", out, err, want)
			}
			if calls != 1 {
				t.Fatalf("handler calls=%d, want 1", calls)
			}
		})
	}
}

func BenchmarkInstructionRecipeBudgetCompile(b *testing.B) {
	if runtime.GOARCH != "arm64" {
		b.Skip("ARM64 recipe admission budget")
	}
	for _, steps := range []int{1, 4, 8, 12} {
		b.Run(fmt.Sprintf("shared%d", steps), func(b *testing.B) {
			calls := 0
			rt := NewRuntime()
			defer rt.Close()
			if err := rt.Use(sharedInstructionRecipe(steps, &calls)); err != nil {
				b.Fatal(err)
			}
			module := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
				wasmtest.Section(2, wasmtest.Vec(instructionFuncImport("wago:instr/recipe", "shared-budget", 0))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x10, 0, 0x0b}))),
			)
			var size int
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				mod, err := rt.Compile(module)
				if err != nil {
					b.Fatal(err)
				}
				size = mod.Compiled().CodeSize()
				mod.Close()
			}
			b.StopTimer()
			b.ReportMetric(float64(size), "code-B")
		})
	}
}
