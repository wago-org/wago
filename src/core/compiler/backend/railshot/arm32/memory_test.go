package arm32

import (
	"os/exec"
	"testing"

	a32 "github.com/wago-org/wago/src/core/encoder/arm32"
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
	a.MovImm32(a32.R12, 0)
	a.Str(a32.R12, a32.SP, 32)
}
func armExit(a *a32.Asm)       { a.MovImm32(a32.R7, 1); a.Svc(0); a.Align4() }
func armContextArg(a *a32.Asm) { a.MovImm32(a32.R12, 16); a.Add(a32.R0, a32.SP, a32.R12) }

func TestI64MemoryThunksExecuteUnderQEMU(t *testing.T) {
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
		runARM32Exit(t, qemu, append(a.B, fn...), 1)
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
		runARM32Exit(t, qemu, append(a.B, fn...), 1)
	})
}
