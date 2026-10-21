//go:build !tinygo

package wago

import (
	"fmt"
	"runtime"
	"testing"
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
