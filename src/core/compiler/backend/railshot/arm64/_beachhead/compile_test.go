package arm64

import (
	"bytes"
	"testing"

	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

// TestCompileAddBytes checks the beachhead compiler lowers
// `local.get 0; local.get 1; i32.add` to the expected AArch64 instruction bytes.
// Byte-level, so it runs on the amd64 host (no qemu needed).
func TestCompileAddBytes(t *testing.T) {
	// body: 0 local decls; local.get 0; local.get 1; i32.add; end
	body := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b}
	got, err := Compile(2, body)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	// Prologue moves params into local regs (X19,X20); each local.get copies to a
	// scratch (X9,X10); add into X11; move result to X0; ret.
	var want a64.Asm
	want.MovReg32(a64.X19, a64.X0)
	want.MovReg32(a64.X20, a64.X1)
	want.MovReg32(a64.X9, a64.X19)
	want.MovReg32(a64.X10, a64.X20)
	want.Add32(a64.X11, a64.X9, a64.X10)
	want.MovReg32(a64.X0, a64.X11)
	want.Ret()
	if !bytes.Equal(got, want.B) {
		t.Errorf("compiled bytes mismatch\n got: % x\nwant: % x", got, want.B)
	}
}

// TestCompileConstSub checks const materialization + subtraction:
// `i32.const 10; local.get 0; i32.sub`  →  10 - x0. (10 < 64, so the LEB byte
// 0x0a is unambiguously positive and MovImm64 emits a single movz.)
func TestCompileConstSub(t *testing.T) {
	body := []byte{0x00, 0x41, 0x0a, 0x20, 0x00, 0x6b, 0x0b} // const 10; local.get 0; sub
	got, err := Compile(1, body)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	// prologue mov w19,w0; local.get 0 -> mov w9,w19; const 10 -> movz x10,#10;
	// sub w11,w10,w9 (10 - x0); mov w0,w11; ret.
	var want a64.Asm
	want.MovReg32(a64.X19, a64.X0)
	want.MovReg32(a64.X9, a64.X19)
	want.MovImm64(a64.X10, 10)
	want.Sub32(a64.X11, a64.X10, a64.X9)
	want.MovReg32(a64.X0, a64.X11)
	want.Ret()
	if !bytes.Equal(got, want.B) {
		t.Errorf("compiled bytes mismatch\n got: % x\nwant: % x", got, want.B)
	}
}

// TestCompileUnsupported ensures unknown opcodes are rejected, not miscompiled.
func TestCompileUnsupported(t *testing.T) {
	body := []byte{0x00, 0x20, 0x00, 0x28, 0x02, 0x00, 0x0b} // i32.load (memory not supported yet)
	if _, err := Compile(1, body); err == nil {
		t.Fatal("expected error for unsupported opcode 0x28")
	}
}
