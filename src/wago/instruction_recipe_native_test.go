//go:build !tinygo

package wago

import (
	"context"
	"fmt"
	"runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestCustomInstructionRecipeWidthBoundaries(t *testing.T) {
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Skip("scalar recipe lowering needs AMD64 or ARM64")
	}
	for _, width := range []int32{1, 3, 5, 31, 32} {
		for _, shift := range []bool{false, true} {
			t.Run(fmt.Sprintf("width%d/shift%t", width, shift), func(t *testing.T) {
				calls := 0
				mask := uint32((uint64(1) << uint(width)) - 1)
				ext := instructionRecipeExt{name: "boundary", input: []int32{width, 32, width}, output: width, portableCalls: &calls}
				if shift {
					ext.lower = func(c LoweringContext) error {
						c.Output(0, c.ShrS(c.Input(2), c.Input(1))) // omit input 0
						return nil
					}
					ext.eval = func(v []uint32) uint32 {
						signed := int32(v[2]<<uint(32-width)) >> uint(32-width)
						return uint32(signed>>uint(v[1]%uint32(width))) & mask
					}
				} else {
					ext.lower = func(c LoweringContext) error {
						a, b := c.Input(2), c.Input(0) // reorder and reuse inputs
						c.Output(0, c.Xor(c.Or(c.Mul(a, a), c.Not(b)), c.And(c.Sub(a, b), b)))
						return nil
					}
					ext.eval = func(v []uint32) uint32 {
						a, b := v[2], v[0]
						return ((a*a | ^b) ^ ((a - b) & b)) & mask
					}
				}
				in := instantiateInstructionRecipe(t, ext)
				defer in.Close()
				values := []uint32{0, 1, 2, 31, 32, 33, 0x7fffffff, 0x80000000, 0xffffffff}
				for _, a := range values {
					for _, b := range values {
						out, err := in.Invoke("run", I32(int32(a)), I32(int32(b)), I32(int32(a^0xa5a5a5a5)))
						if err != nil {
							t.Fatal(err)
						}
						want := ext.eval([]uint32{a & mask, b, (a ^ 0xa5a5a5a5) & mask})
						if got := uint32(out[0]); got != want {
							t.Fatalf("inputs %#x/%#x: got %#x, want %#x", a, b, got, want)
						}
					}
				}
				if calls != 0 {
					t.Fatalf("portable handler called %d times", calls)
				}
			})
		}
	}
}

func TestCustomInstructionRecipeOmittedArgumentTrap(t *testing.T) {
	calls := 0
	ext := instructionRecipeExt{name: "ignore-load", input: []int32{32}, output: 32, portableCalls: &calls,
		lower: func(c LoweringContext) error { c.Output(0, c.Const(32, 7)); return nil },
		eval:  func([]uint32) uint32 { return 7 },
	}
	rt := NewRuntime()
	defer rt.Close()
	if err := rt.Use(ext); err != nil {
		t.Fatal(err)
	}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(instructionFuncImport("wago:instr/recipe", ext.name, 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		// Keep a live prefix and perform a load that the recipe does not use.
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 11, 0x20, 0, 0x28, 2, 0, 0x10, 0, 0x6a, 0x0b}))),
	)
	mod, err := rt.Compile(module)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if out, err := in.Invoke("run", I32(0)); err != nil || len(out) != 1 || AsI32(out[0]) != 18 {
		t.Fatalf("valid load: %v, %v; want 18", out, err)
	}
	if _, err := in.Invoke("run", I32(65535)); err == nil {
		t.Fatal("omitted out-of-bounds argument did not trap")
	}
	if (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64") && calls != 0 {
		t.Fatalf("portable handler called %d times", calls)
	}
}

func TestCustomInstructionRecipeInputOnlyARM64(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("ARM64 recipe input canonicalization")
	}
	for _, width := range []int32{1, 3, 5, 31, 32} {
		t.Run(fmt.Sprintf("width%d", width), func(t *testing.T) {
			calls := 0
			ext := instructionRecipeExt{name: "input-only", input: []int32{width}, output: width, portableCalls: &calls,
				lower: func(c LoweringContext) error { c.Output(0, c.Input(0)); return nil },
				eval:  func(v []uint32) uint32 { return v[0] },
			}
			in := instantiateInstructionRecipe(t, ext)
			defer in.Close()
			out, err := in.Invoke("run", I32(-1))
			want := uint32((uint64(1) << uint(width)) - 1)
			if err != nil || len(out) != 1 || uint32(out[0]) != want {
				t.Fatalf("got %v, %v; want %#x", out, err, want)
			}
			if calls != 0 {
				t.Fatalf("portable handler called %d times", calls)
			}
		})
	}
}
