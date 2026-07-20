package arm32

import (
	"os/exec"
	"testing"

	a32 "github.com/wago-org/wago/src/core/encoder/arm32"
)

func TestF32BitFunctionExecutesUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	body := []byte{0, 0x43, 0x2a, 0, 0, 0, 0x43, 0, 0, 0, 0x80, 0x98, 0x8b, 0x0b}
	fn, err := CompileF32BitFunction(0, body)
	if err != nil {
		t.Fatal(err)
	}
	var entry a32.Asm
	call := entry.Call()
	entry.MovImm32(a32.R7, 1)
	entry.Svc(0)
	entry.Align4()
	entry.PatchCall(call, len(entry.B))
	runARM32Exit(t, qemu, append(entry.B, fn...), 42)
	if _, err := CompileF32BitFunction(0, []byte{0, 0x92, 0x0b}); err == nil {
		t.Fatal("unsupported f32.sqrt accepted")
	}
}
