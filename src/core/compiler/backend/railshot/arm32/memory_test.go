package arm32

import (
	"os/exec"
	"testing"

	a32 "github.com/wago-org/wago/src/core/encoder/arm32"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

func armMemoryContext(a *a32.Asm) {
	a.MovImm32(a32.R12, 64)
	a.Sub(a32.SP, a32.SP, a32.R12)
	a.MovReg(a32.R4, a32.SP)
	a.Str(a32.R4, a32.SP, 16)
	a.MovImm32(a32.R12, 16)
	a.Str(a32.R12, a32.SP, 20)
	a.MovImm32(a32.R12, 32)
	a.Add(a32.R5, a32.SP, a32.R12)
	a.Str(a32.R5, a32.SP, 24)
	a.MovImm32(a32.R12, 52)
	a.Add(a32.R5, a32.SP, a32.R12)
	a.Str(a32.R5, a32.SP, 28)
	a.MovImm32(a32.R12, 48)
	a.Add(a32.R5, a32.SP, a32.R12)
	a.Str(a32.R5, a32.SP, 44)
	a.MovImm32(a32.R12, 0)
	a.Str(a32.R12, a32.SP, 32)
	a.Str(a32.R12, a32.SP, 40)
	a.Str(a32.R12, a32.SP, 48)
	a.Str(a32.R12, a32.SP, 52)
	a.MovImm32(a32.R12, 16)
	a.Str(a32.R12, a32.SP, 36)
}
func armExit(a *a32.Asm)       { a.MovImm32(a32.R7, 1); a.Svc(0); a.Align4() }
func armContextArg(a *a32.Asm) { a.MovImm32(a32.R12, 16); a.Add(a32.R0, a32.SP, a32.R12) }

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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	t.Run("load", func(t *testing.T) {
		fn, err := CompileI64LoadThunk(2)
		if err != nil {
			t.Fatal(err)
		}
		var a a32.Asm
		armMemoryContext(&a)
		a.MovImm32(a32.R12, 0x44332211)
		a.Str(a32.R12, a32.SP, 4)
		a.MovImm32(a32.R12, 0x88776655)
		a.Str(a32.R12, a32.SP, 8)
		armContextArg(&a)
		a.MovImm32(a32.R1, 2)
		call := a.Call()
		armExit(&a)
		a.PatchCall(call, len(a.B))
		runARM32Exit(t, qemu, append(a.B, fn...), 0x11)
	})
	t.Run("load8-signed-i64", func(t *testing.T) {
		fn, err := CompileScalarLoadThunk(embedded32.ScalarI64Load8S, 0)
		if err != nil {
			t.Fatal(err)
		}
		var a a32.Asm
		armMemoryContext(&a)
		a.MovImm32(a32.R12, 0x80)
		a.Strb(a32.R12, a32.SP, 0)
		armContextArg(&a)
		a.MovImm32(a32.R1, 0)
		call := a.Call()
		a.MovReg(a32.R0, a32.R1)
		armExit(&a)
		a.PatchCall(call, len(a.B))
		runARM32Exit(t, qemu, append(a.B, fn...), 0xff)
	})
	t.Run("load-oob", func(t *testing.T) {
		fn, _ := CompileI64LoadThunk(0)
		var a a32.Asm
		armMemoryContext(&a)
		armContextArg(&a)
		a.MovImm32(a32.R1, 10)
		call := a.Call()
		a.Ldr(a32.R0, a32.SP, 32)
		armExit(&a)
		a.PatchCall(call, len(a.B))
		runARM32Exit(t, qemu, append(a.B, fn...), 3)
	})
	t.Run("store", func(t *testing.T) {
		fn, _ := CompileI64StoreThunk(0)
		var a a32.Asm
		armMemoryContext(&a)
		armContextArg(&a)
		a.MovImm32(a32.R1, 4)
		a.MovImm32(a32.R2, 42)
		a.MovImm32(a32.R3, 99)
		call := a.Call()
		a.Ldr(a32.R0, a32.SP, 4)
		armExit(&a)
		a.PatchCall(call, len(a.B))
		runARM32Exit(t, qemu, append(a.B, fn...), 42)
	})
	t.Run("store16-unaligned", func(t *testing.T) {
		fn, err := CompileScalarStoreThunk(embedded32.ScalarI64Store16, 0)
		if err != nil {
			t.Fatal(err)
		}
		var a a32.Asm
		armMemoryContext(&a)
		armContextArg(&a)
		a.MovImm32(a32.R1, 1)
		a.MovImm32(a32.R2, 0x1234)
		a.MovImm32(a32.R3, 0xdeadbeef)
		call := a.Call()
		a.Ldrb(a32.R0, a32.SP, 1)
		armExit(&a)
		a.PatchCall(call, len(a.B))
		runARM32Exit(t, qemu, append(a.B, fn...), 0x34)
	})
	t.Run("store-oob-atomic", func(t *testing.T) {
		fn, _ := CompileI64StoreThunk(0)
		var a a32.Asm
		armMemoryContext(&a)
		a.MovImm32(a32.R12, 2)
		a.Str(a32.R12, a32.SP, 12)
		armContextArg(&a)
		a.MovImm32(a32.R1, 12)
		a.MovImm32(a32.R2, 42)
		a.MovImm32(a32.R3, 99)
		call := a.Call()
		a.Ldr(a32.R0, a32.SP, 12)
		armExit(&a)
		a.PatchCall(call, len(a.B))
		runARM32Exit(t, qemu, append(a.B, fn...), 2)
	})
	t.Run("static-offset-overflow", func(t *testing.T) {
		fn, err := CompileScalarLoadThunk(embedded32.ScalarI32Load8U, ^uint32(0))
		if err != nil {
			t.Fatal(err)
		}
		var a a32.Asm
		armMemoryContext(&a)
		armContextArg(&a)
		a.MovImm32(a32.R1, 1)
		call := a.Call()
		a.Ldr(a32.R0, a32.SP, 32)
		armExit(&a)
		a.PatchCall(call, len(a.B))
		runARM32Exit(t, qemu, append(a.B, fn...), 3)
	})
	t.Run("store-oob", func(t *testing.T) {
		fn, _ := CompileI64StoreThunk(0)
		var a a32.Asm
		armMemoryContext(&a)
		armContextArg(&a)
		a.MovImm32(a32.R1, 12)
		a.MovImm32(a32.R2, 42)
		a.MovImm32(a32.R3, 99)
		call := a.Call()
		a.Ldr(a32.R0, a32.SP, 32)
		armExit(&a)
		a.PatchCall(call, len(a.B))
		runARM32Exit(t, qemu, append(a.B, fn...), 3)
	})
}
