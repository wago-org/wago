//go:build linux && riscv64 && !tinygo

package riscv64

import (
	"bytes"
	"runtime"
	"testing"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func probeRaw(code, low, top, frame, trace, trap uintptr) uint64

//go:nosplit
func probeCall(code, low, top, frame, trace, trap uintptr) uint64 {
	enterSyscall()
	mask := probeRaw(code, low, top, frame, trace, trap)
	exitSyscall()
	return mask
}

func TestNativeABIRegistersAndAlignment(t *testing.T) {
	e, err := NewEngine(MinNativeStackBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	trace, err := mapRW(64)
	if err != nil {
		t.Fatal(err)
	}
	defer linuxMemory.unmap(trace)
	var a enc.Asm
	a.Addi(enc.SP, enc.SP, -16)
	a.Sd(enc.RA, enc.SP, 0)
	for i, reg := range []enc.Reg{enc.GP, enc.TP, enc.X27, enc.SP, enc.RA, enc.X26} {
		a.Sd(reg, enc.X10, int32(i*8))
	}
	// The native guest may use these callee-saved/Go CTXT registers. g/gp/tp
	// stay valid throughout the snippet; their preservation is checked live.
	for _, reg := range []enc.Reg{enc.X8, enc.X9, enc.X18, enc.X19, enc.X20, enc.X21, enc.X22, enc.X23, enc.X24, enc.X25, enc.X26} {
		a.Constant64(reg, int64(uint64(0xdeadbeef00000000)+uint64(reg)))
	}
	a.Constant64(enc.X10, 42)
	a.Ld(enc.RA, enc.SP, 0)
	a.Addi(enc.SP, enc.SP, 16)
	a.Ret()
	if a.Err() != nil {
		t.Fatal(a.Err())
	}
	p, err := MapCode(a.B)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	for i := 0; i < 128; i++ {
		if mask := probeCall(pointer(p.mem), e.low, e.top, 16, pointer(trace), pointer(e.trap)); mask != 0 {
			t.Fatalf("ABI sentinel failure mask=%#x", mask)
		}
		if i%16 == 0 {
			runtime.GC()
			runtime.Gosched()
		}
	}
	runtime.KeepAlive(p)
	runtime.KeepAlive(e)
}

func TestRawStackBoundsBeforeWrites(t *testing.T) {
	stack, err := mapRW(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer linuxMemory.unmap(stack)
	for i := range stack {
		stack[i] = 0x5a
	}
	before := append([]byte(nil), stack...)
	top := pointer(stack) + uintptr(len(stack))
	for _, tc := range []struct{ low, top, frame uintptr }{
		{top - 128, top, 16}, // Save area fits, guest frame does not.
		{top - 127, top, 16},
		{top - 144, top - 1, 16},
		{top + 16, top, 16},
		{top - 256, top, 0},
		{top - 256, top, 17},
		{top - 256, top, ^uintptr(0) &^ 15},
	} {
		// code=0 must never be reached, and no foreign stack byte may change.
		result, status := enterRaw(0, 0, 0, tc.low, tc.top, tc.frame, 0)
		if result != 0 || status != 1 || !bytes.Equal(stack, before) {
			t.Fatalf("raw bounds=%+v result=%d status=%d", tc, result, status)
		}
	}
	// The exact 128+16 boundary succeeds with a framed encoder snippet.
	p, err := MapCode(snippet(0))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	trap, err := mapRW(trapBufferBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer linuxMemory.unmap(trap)
	result, status := nativeCall(pointer(p.mem), 19, 23, top-144, top, 16, pointer(trap))
	if result != 42 || status != 0 {
		t.Fatalf("exact frame boundary %d/%d", result, status)
	}
	if !bytes.Equal(stack[:len(stack)-144], before[:len(stack)-144]) {
		t.Fatal("entry wrote below allowed fence")
	}
}
