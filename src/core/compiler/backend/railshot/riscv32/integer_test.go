package riscv32

import (
	"os/exec"
	"testing"

	rv "github.com/wago-org/wago/src/core/encoder/riscv32"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

func TestI32DivRemThunksExecuteUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
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
			var a rv.Asm
			rvMemoryContext(&a)
			a.Addi(rv.A0, rv.SP, 16)
			a.MovImm32(rv.A1, tc.left)
			a.MovImm32(rv.A2, tc.right)
			call := a.Jal(rv.RA)
			a.MovImm32(rv.A7, 93)
			a.Ecall()
			a.PatchJAL21(call, len(a.B))
			runRV32Exit(t, qemu, append(a.B, fn...), tc.want)
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
			var a rv.Asm
			rvMemoryContext(&a)
			a.Addi(rv.A0, rv.SP, 16)
			a.MovImm32(rv.A1, tc.left)
			a.MovImm32(rv.A2, tc.right)
			call := a.Jal(rv.RA)
			a.Lw(rv.A0, rv.SP, 32)
			a.MovImm32(rv.A7, 93)
			a.Ecall()
			a.PatchJAL21(call, len(a.B))
			runRV32Exit(t, qemu, append(a.B, fn...), tc.want)
		})
	}
	if _, err := CompileI32DivRemThunk(255); err == nil {
		t.Fatal("invalid div/rem operation accepted")
	}
}
