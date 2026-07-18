package riscv32

import (
	"os/exec"
	"testing"

	rv "github.com/wago-org/wago/src/core/encoder/riscv32"
)

func rvMemoryContext(a *rv.Asm) {
	a.Addi(rv.SP, rv.SP, -64)
	a.MovReg(rv.T0, rv.SP)
	a.Sw(rv.T0, rv.SP, 16)
	a.MovImm32(rv.T1, 16)
	a.Sw(rv.T1, rv.SP, 20)
	a.Addi(rv.T1, rv.SP, 32)
	a.Sw(rv.T1, rv.SP, 24)
	a.MovImm32(rv.T1, 0)
	a.Sw(rv.T1, rv.SP, 32)
}

func TestI64MemoryThunksExecuteUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	t.Run("load", func(t *testing.T) {
		fn, err := CompileI64LoadThunk(2)
		if err != nil {
			t.Fatal(err)
		}
		var a rv.Asm
		rvMemoryContext(&a)
		a.MovImm32(rv.T1, 0x44332211)
		a.Sw(rv.T1, rv.SP, 4)
		a.MovImm32(rv.T1, 0x88776655)
		a.Sw(rv.T1, rv.SP, 8)
		a.Addi(rv.A0, rv.SP, 16)
		a.MovImm32(rv.A1, 2)
		call := a.Jal(rv.RA)
		a.MovImm32(rv.A7, 93)
		a.Ecall()
		a.PatchJAL21(call, len(a.B))
		runRV32Exit(t, qemu, append(a.B, fn...), 0x11)
	})
	t.Run("load-oob", func(t *testing.T) {
		fn, _ := CompileI64LoadThunk(0)
		var a rv.Asm
		rvMemoryContext(&a)
		a.Addi(rv.A0, rv.SP, 16)
		a.MovImm32(rv.A1, 10)
		call := a.Jal(rv.RA)
		a.Lw(rv.A0, rv.SP, 32)
		a.MovImm32(rv.A7, 93)
		a.Ecall()
		a.PatchJAL21(call, len(a.B))
		runRV32Exit(t, qemu, append(a.B, fn...), 1)
	})
	t.Run("store", func(t *testing.T) {
		fn, _ := CompileI64StoreThunk(0)
		var a rv.Asm
		rvMemoryContext(&a)
		a.Addi(rv.A0, rv.SP, 16)
		a.MovImm32(rv.A1, 4)
		a.MovImm32(rv.A2, 42)
		a.MovImm32(rv.A3, 99)
		call := a.Jal(rv.RA)
		a.Lw(rv.A0, rv.SP, 4)
		a.MovImm32(rv.A7, 93)
		a.Ecall()
		a.PatchJAL21(call, len(a.B))
		runRV32Exit(t, qemu, append(a.B, fn...), 42)
	})
	t.Run("store-oob-atomic", func(t *testing.T) {
		fn, _ := CompileI64StoreThunk(0)
		var a rv.Asm
		rvMemoryContext(&a)
		a.MovImm32(rv.T1, 2)
		a.Sw(rv.T1, rv.SP, 12)
		a.Addi(rv.A0, rv.SP, 16)
		a.MovImm32(rv.A1, 12)
		a.MovImm32(rv.A2, 42)
		a.MovImm32(rv.A3, 99)
		call := a.Jal(rv.RA)
		a.Lw(rv.A0, rv.SP, 12)
		a.MovImm32(rv.A7, 93)
		a.Ecall()
		a.PatchJAL21(call, len(a.B))
		runRV32Exit(t, qemu, append(a.B, fn...), 2)
	})
	t.Run("store-oob", func(t *testing.T) {
		fn, _ := CompileI64StoreThunk(0)
		var a rv.Asm
		rvMemoryContext(&a)
		a.Addi(rv.A0, rv.SP, 16)
		a.MovImm32(rv.A1, 12)
		a.MovImm32(rv.A2, 42)
		a.MovImm32(rv.A3, 99)
		call := a.Jal(rv.RA)
		a.Lw(rv.A0, rv.SP, 32)
		a.MovImm32(rv.A7, 93)
		a.Ecall()
		a.PatchJAL21(call, len(a.B))
		runRV32Exit(t, qemu, append(a.B, fn...), 1)
	})
}
