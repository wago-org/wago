package riscv32

import (
	"os/exec"
	"testing"

	rv "github.com/wago-org/wago/src/core/encoder/riscv32"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

func rvMemoryContext(a *rv.Asm) {
	a.Addi(rv.SP, rv.SP, -64)
	a.MovReg(rv.T0, rv.SP)
	a.Sw(rv.T0, rv.SP, 16)
	a.MovImm32(rv.T1, 16)
	a.Sw(rv.T1, rv.SP, 20)
	a.Addi(rv.T1, rv.SP, 32)
	a.Sw(rv.T1, rv.SP, 24)
	a.Addi(rv.T1, rv.SP, 40)
	a.Sw(rv.T1, rv.SP, 28)
	a.MovImm32(rv.T1, 0)
	a.Sw(rv.T1, rv.SP, 32)
	a.Sw(rv.T1, rv.SP, 40)
	a.MovImm32(rv.T1, 16)
	a.Sw(rv.T1, rv.SP, 36)
}

func TestScalarMemoryThunkCoverage(t *testing.T) {
	loads := []embedded32.ScalarLoadOp{
		embedded32.ScalarI32Load, embedded32.ScalarI32Load8S, embedded32.ScalarI32Load8U,
		embedded32.ScalarI32Load16S, embedded32.ScalarI32Load16U, embedded32.ScalarI64Load,
		embedded32.ScalarI64Load8S, embedded32.ScalarI64Load8U, embedded32.ScalarI64Load16S,
		embedded32.ScalarI64Load16U, embedded32.ScalarI64Load32S, embedded32.ScalarI64Load32U,
		embedded32.ScalarF32Load, embedded32.ScalarF64Load,
	}
	for _, op := range loads {
		if code, err := CompileScalarLoadThunk(op, 0); err != nil || len(code) == 0 {
			t.Fatalf("load %d: code=%d err=%v", op, len(code), err)
		}
	}
	stores := []embedded32.ScalarStoreOp{
		embedded32.ScalarI32Store, embedded32.ScalarI32Store8, embedded32.ScalarI32Store16,
		embedded32.ScalarI64Store, embedded32.ScalarI64Store8, embedded32.ScalarI64Store16,
		embedded32.ScalarI64Store32, embedded32.ScalarF32Store, embedded32.ScalarF64Store,
	}
	for _, op := range stores {
		if code, err := CompileScalarStoreThunk(op, 0); err != nil || len(code) == 0 {
			t.Fatalf("store %d: code=%d err=%v", op, len(code), err)
		}
	}
	if _, err := CompileScalarLoadThunk(255, 0); err == nil {
		t.Fatal("invalid load accepted")
	}
	if _, err := CompileScalarStoreThunk(255, 0); err == nil {
		t.Fatal("invalid store accepted")
	}
}

func TestScalarMemoryThunksExecuteUnderQEMU(t *testing.T) {
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
	t.Run("load8-signed-i64", func(t *testing.T) {
		fn, err := CompileScalarLoadThunk(embedded32.ScalarI64Load8S, 0)
		if err != nil {
			t.Fatal(err)
		}
		var a rv.Asm
		rvMemoryContext(&a)
		a.MovImm32(rv.T1, 0x80)
		a.Sb(rv.T1, rv.SP, 0)
		a.Addi(rv.A0, rv.SP, 16)
		a.MovImm32(rv.A1, 0)
		call := a.Jal(rv.RA)
		a.MovReg(rv.A0, rv.A1)
		a.MovImm32(rv.A7, 93)
		a.Ecall()
		a.PatchJAL21(call, len(a.B))
		runRV32Exit(t, qemu, append(a.B, fn...), 0xff)
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
		runRV32Exit(t, qemu, append(a.B, fn...), 3)
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
	t.Run("store16-unaligned", func(t *testing.T) {
		fn, err := CompileScalarStoreThunk(embedded32.ScalarI64Store16, 0)
		if err != nil {
			t.Fatal(err)
		}
		var a rv.Asm
		rvMemoryContext(&a)
		a.Addi(rv.A0, rv.SP, 16)
		a.MovImm32(rv.A1, 1)
		a.MovImm32(rv.A2, 0x1234)
		a.MovImm32(rv.A3, 0xdeadbeef)
		call := a.Jal(rv.RA)
		a.Lbu(rv.A0, rv.SP, 1)
		a.MovImm32(rv.A7, 93)
		a.Ecall()
		a.PatchJAL21(call, len(a.B))
		runRV32Exit(t, qemu, append(a.B, fn...), 0x34)
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
	t.Run("static-offset-overflow", func(t *testing.T) {
		fn, err := CompileScalarLoadThunk(embedded32.ScalarI32Load8U, ^uint32(0))
		if err != nil {
			t.Fatal(err)
		}
		var a rv.Asm
		rvMemoryContext(&a)
		a.Addi(rv.A0, rv.SP, 16)
		a.MovImm32(rv.A1, 1)
		call := a.Jal(rv.RA)
		a.Lw(rv.A0, rv.SP, 32)
		a.MovImm32(rv.A7, 93)
		a.Ecall()
		a.PatchJAL21(call, len(a.B))
		runRV32Exit(t, qemu, append(a.B, fn...), 3)
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
		runRV32Exit(t, qemu, append(a.B, fn...), 3)
	})
}
