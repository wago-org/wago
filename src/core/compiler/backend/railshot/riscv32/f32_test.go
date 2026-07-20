package riscv32

import (
	"os/exec"
	"testing"

	rv "github.com/wago-org/wago/src/core/encoder/riscv32"
)

func TestF32BitFunctionExecutesUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	body := []byte{0, 0x43, 0x2a, 0, 0, 0, 0x43, 0, 0, 0, 0x80, 0x98, 0x8b, 0x0b}
	fn, err := CompileF32BitFunction(0, body)
	if err != nil {
		t.Fatal(err)
	}
	var entry rv.Asm
	call := entry.Jal(rv.RA)
	entry.MovImm32(rv.A7, 93)
	entry.Ecall()
	entry.PatchJAL21(call, len(entry.B))
	runRV32Exit(t, qemu, append(entry.B, fn...), 42)
	if _, err := CompileF32BitFunction(0, []byte{0, 0x92, 0x0b}); err == nil {
		t.Fatal("unsupported f32.sqrt accepted")
	}
}
