//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestZeroBranchIfArm64(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	// local.get 0; if (result i32) 11 else 22 end
	body := []byte{0x00, 0x20, 0x00, 0x04, 0x7f, 0x41, 0x0b, 0x05, 0x41, 0x16, 0x0b, 0x0b}
	m := mod1(t, i32, i32, body)

	for arg, want := range map[uint32]uint32{0: 22, 1: 11, ^uint32(0): 11} {
		if got := uint32(runArm64Internal2(t, m, uintptr(arg), 0)); got != want {
			t.Fatalf("if(%d) = %d, want %d", arg, got, want)
		}
	}

	before := zeroBranchEnabled
	t.Cleanup(func() { zeroBranchEnabled = before })
	zeroBranchEnabled = false
	long := compileWithStats(t, m, false).Funcs[0]
	zeroBranchEnabled = true
	short := compileWithStats(t, m, false).Funcs[0]
	if got := long.CodeBytes - short.CodeBytes; got != 4 {
		t.Fatalf("CMP+B.cond delta = %d bytes, want 4", got)
	}
	if got := short.Peephole["zero-branch"]; got != 1 {
		t.Fatalf("zero-branch hits = %d, want 1", got)
	}
}
