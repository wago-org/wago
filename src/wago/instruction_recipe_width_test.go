//go:build !tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestCustomInstructionInputRecipeWidth(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("AMD64 portable recipe lowering")
	}
	for _, width := range []int32{1, 3, 4, 5, 31, 32} {
		t.Run(fmt.Sprintf("i%d", width), func(t *testing.T) {
			calls := 0
			ext := instructionRecipeExt{
				name: fmt.Sprintf("input.i%d", width), input: []int32{width}, output: width,
				portableCalls: &calls,
				lower:         func(c LoweringContext) error { c.Output(0, c.Input(0)); return nil },
				eval:          func(v []uint32) uint32 { return v[0] },
			}
			in := instantiateInstructionRecipe(t, ext)
			defer in.Close()
			mask := uint32((uint64(1) << uint(width)) - 1)
			for _, value := range []uint32{0, 1, 2, 15, 31, 0x80000000, 0x80000001, 0x7fffffff, 0xfffffffe, 0xffffffff} {
				out, err := in.Invoke("run", I32(int32(value)))
				if err != nil || len(out) != 1 || uint32(out[0]) != value&mask {
					t.Errorf("physical i32=%#x: got %v, %v; want %#x", value, out, err, value&mask)
				}
			}
			if calls != 0 {
				t.Fatalf("portable handler calls=%d, want 0", calls)
			}
		})
	}
}

func TestCustomInstructionInputRecipeTrapOrder(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("AMD64 portable recipe lowering")
	}
	calls := 0
	rt := NewRuntime()
	defer rt.Close()
	if err := rt.Use(instructionRecipeExt{
		name: "input.trap-order", input: []int32{4}, output: 4, portableCalls: &calls,
		lower: func(c LoweringContext) error { c.Output(0, c.Input(0)); return nil },
		eval:  func(v []uint32) uint32 { return v[0] },
	}); err != nil {
		t.Fatal(err)
	}
	// The prefix must overflow before the argument divides by zero. Five xor
	// nodes put the deferred argument one operation below its materialization limit.
	body := []byte{0x20, 0, 0x41, 0x7f, 0x6d, 0x20, 1, 0x41, 0, 0x6d}
	for i := 0; i < 5; i++ {
		body = append(body, 0x41, 3, 0x73)
	}
	body = append(body, 0x10, 0, 0x0f, 0x0b)
	mod, err := rt.Compile(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(instructionFuncImport("wago:instr/recipe", "input.trap-order", 0))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	))
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	_, err = in.Invoke("run", I32(-2147483648), I32(7))
	var trap *TrapError
	if !errors.As(err, &trap) || trap.Code != TrapDivOverflow {
		t.Fatalf("got %v, want the earlier integer overflow", err)
	}
	if calls != 0 {
		t.Fatalf("portable handler calls=%d, want 0", calls)
	}
}
