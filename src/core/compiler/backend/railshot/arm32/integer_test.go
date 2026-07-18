package arm32

import (
	"os/exec"
	"testing"

	a32 "github.com/wago-org/wago/src/core/encoder/arm32"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

func TestI32DivRemThunksExecuteUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	tests := []struct {
		name        string
		op          embedded32.I32DivRemOp
		left, right uint32
		want        int
	}{
		{"div_s", embedded32.I32DivS, ^uint32(20), 4, 251},
		{"div_u", embedded32.I32DivU, 21, 4, 5},
		{"rem_s", embedded32.I32RemS, ^uint32(20), 4, 255},
		{"rem_u", embedded32.I32RemU, 21, 4, 1},
		{"rem_s_overflow_defined", embedded32.I32RemS, 0x80000000, 0xffffffff, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fn, err := CompileI32DivRemThunk(tc.op)
			if err != nil {
				t.Fatal(err)
			}
			var a a32.Asm
			armMemoryContext(&a)
			armContextArg(&a)
			a.MovImm32(a32.R1, tc.left)
			a.MovImm32(a32.R2, tc.right)
			call := a.Call()
			armExit(&a)
			a.PatchCall(call, len(a.B))
			runARM32Exit(t, qemu, append(a.B, fn...), tc.want)
		})
	}
	traps := []struct {
		name        string
		op          embedded32.I32DivRemOp
		left, right uint32
		want        int
	}{
		{"divide_by_zero", embedded32.I32DivU, 1, 0, int(embedded32.TrapIntegerDivideByZero)},
		{"signed_overflow", embedded32.I32DivS, 0x80000000, 0xffffffff, int(embedded32.TrapIntegerOverflow)},
	}
	for _, tc := range traps {
		t.Run(tc.name, func(t *testing.T) {
			fn, err := CompileI32DivRemThunk(tc.op)
			if err != nil {
				t.Fatal(err)
			}
			var a a32.Asm
			armMemoryContext(&a)
			armContextArg(&a)
			a.MovImm32(a32.R1, tc.left)
			a.MovImm32(a32.R2, tc.right)
			call := a.Call()
			a.Ldr(a32.R0, a32.SP, 32)
			armExit(&a)
			a.PatchCall(call, len(a.B))
			runARM32Exit(t, qemu, append(a.B, fn...), tc.want)
		})
	}
	if _, err := CompileI32DivRemThunk(255); err == nil {
		t.Fatal("invalid div/rem operation accepted")
	}
}
