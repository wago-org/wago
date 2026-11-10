//go:build windows && arm64 && wago_guardpage && !tinygo

package runtime

import (
	"encoding/binary"
	"testing"

	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func TestWindowsARM64GuardCommitFailureReturnsTrap(t *testing.T) {
	if err := InstallGuardTrapHandler(); err != nil {
		t.Fatal(err)
	}
	memory, err := NewJobMemoryGuarded(0, wasmPageBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	// Model native memory.grow: logical size increases before pages are touched.
	memory.putGuardedSizeCaches(wasmPageBytes, wasmPageBytes)
	var code a64.Asm
	code.MovReg64(a64.X26, a64.X1)
	code.Ldrb(a64.X0, a64.X26, 0)
	code.Ret()
	image, entry, err := MapCode(code.B)
	if err != nil {
		t.Fatal(err)
	}
	defer Unmap(image)
	// A bounded native stub models VirtualAlloc returning NULL.
	var fail a64.Asm
	fail.MovImm64(a64.X0, 0)
	fail.Ret()
	failureImage, failurePC, err := MapCode(fail.B)
	if err != nil {
		t.Fatal(err)
	}
	defer Unmap(failureImage)
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	previous := guardVirtualAllocPC
	guardVirtualAllocPC = failurePC
	defer func() { guardVirtualAllocPC = previous }()
	trap := make([]byte, TrapBufferBytes)
	err = engine.CallGuarded(entry, nil, memory.LinMemBase(), trap, nil, memory)
	if got := TrapCode(binary.LittleEndian.Uint32(trap)); err == nil || got != TrapLinMemCouldNotExtend {
		t.Fatalf("commit failure = %v, trap %v; want allocation failure trap", err, got)
	}
}
