//go:build linux && amd64

package amd64

import (
	"bytes"
	"strings"
	"testing"
)

// TestMemOpSIBBaseEncoding locks the encoder fix that makes memory-base pinning
// possible: a base whose low 3 bits are 100 (RSP or R12) needs an explicit SIB
// byte in the [base+disp32] encoding, while every other base encodes directly.
func TestMemOpSIBBaseEncoding(t *testing.T) {
	cases := []struct {
		name string
		emit func(*Asm)
		want []byte
	}{
		// 49 8B /r with ModRM rm=100 (SIB) + SIB base=100 (r12, via REX.B).
		{"Load64 r12 base", func(a *Asm) { a.Load64(RAX, R12, 0x10) }, []byte{0x49, 0x8B, 0x84, 0x24, 0x10, 0, 0, 0}},
		// 48 8B with the same SIB, no REX.B (rsp < 8).
		{"Load64 rsp base", func(a *Asm) { a.Load64(RAX, RSP, 0x10) }, []byte{0x48, 0x8B, 0x84, 0x24, 0x10, 0, 0, 0}},
		// Control: r13 (rm=101) encodes directly, no SIB byte.
		{"Load64 r13 base", func(a *Asm) { a.Load64(RAX, R13, 0x10) }, []byte{0x49, 0x8B, 0x85, 0x10, 0, 0, 0}},
		// Store path (0x89) through the same SIB base.
		{"Store64 r12 base", func(a *Asm) { a.Store64(R12, 0x10, RAX) }, []byte{0x49, 0x89, 0x84, 0x24, 0x10, 0, 0, 0}},
	}
	for _, c := range cases {
		a := &Asm{}
		c.emit(a)
		if !bytes.Equal(a.B, c.want) {
			t.Errorf("%s = % x, want % x", c.name, a.B, c.want)
		}
	}
}

// TestMemoryBasePinning proves that with CompileOptions.PinMemoryBase a memory-
// accessing function keeps the linear-memory base in r12 (primed once) instead of
// reloading it from the frame slot before every access, and that the unpinned
// build does neither.
func TestMemoryBasePinning(t *testing.T) {
	m := watToModule(t, `(module (memory 1) (func (export "f") (param i32) (result i32)
		local.get 0 i32.load
		local.get 0 i32.const 4 i32.add i32.load
		i32.add))`)
	on, err := CompileFunctionWith(m, 0, CompileOptions{PinMemoryBase: true})
	if err != nil {
		t.Fatal(err)
	}
	off, err := CompileFunctionWith(m, 0, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	disOn, disOff := disasm(t, on), disasm(t, off)

	if !strings.Contains(disOn, "r12") {
		t.Errorf("pinned code should use r12 as the linMem base:\n%s", disOn)
	}
	if strings.Contains(disOff, "r12") {
		t.Errorf("unpinned code should not mention r12:\n%s", disOff)
	}
	// The frame-slot linMem reload ([rbp-0x10]) appears once per access when
	// unpinned; when pinned it appears only in the prologue store + the single
	// prime, independent of the number of accesses. Two accesses ⇒ strictly fewer.
	onReloads, offReloads := strings.Count(disOn, "rbp-0x10"), strings.Count(disOff, "rbp-0x10")
	if onReloads >= offReloads {
		t.Errorf("pinning should cut [rbp-0x10] reloads: pinned=%d unpinned=%d\npinned:\n%s", onReloads, offReloads, disOn)
	}
}

// TestMemoryBasePinningSkippedForComputeOnly proves the prime is gated: a function
// that never touches memory or globals must not reserve/prime r12, so pure compute
// (e.g. recursion) pays no prologue tax.
func TestMemoryBasePinningSkippedForComputeOnly(t *testing.T) {
	m := watToModule(t, `(module (func (export "f") (param i32 i32) (result i32)
		local.get 0 local.get 1 i32.add))`)
	code, err := CompileFunctionWith(m, 0, CompileOptions{PinMemoryBase: true})
	if err != nil {
		t.Fatal(err)
	}
	if dis := disasm(t, code); strings.Contains(dis, "r12") {
		t.Errorf("compute-only function must not prime r12:\n%s", dis)
	}
}
