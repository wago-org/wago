//go:build (linux || darwin) && arm64

package arm64spike

import (
	"testing"
	"unsafe"

	enc "github.com/wago-org/wago/src/core/encoder/arm64"
)

// TestSpikeAddExec is the P1 go/no-go: encode `X0 = X0 + X1; ret` with the arm64
// encoder, map it executable, and call it through the foreign-stack trampoline
// with no cgo. If X0+X1 comes back correct, the no-cgo native-exec technique
// works on arm64 and the port is viable.
func TestSpikeAddExec(t *testing.T) {
	var a enc.Asm
	a.Add64(enc.X0, enc.X0, enc.X1) // x0 = x0 + x1
	a.Ret()

	code, err := mmapExec(a.B)
	if err != nil {
		t.Fatalf("mmapExec: %v", err)
	}
	stack, err := mmapRW(64 * 1024)
	if err != nil {
		t.Fatalf("mmapRW stack: %v", err)
	}
	entry := uintptr(unsafe.Pointer(&code[0]))
	top := uintptr(unsafe.Pointer(&stack[0])) + uintptr(len(stack)) // 16-aligned (page-aligned)

	for _, tc := range []struct{ a, b, want uintptr }{
		{40, 2, 42},
		{0, 0, 0},
		{1000000, 337, 1000337},
	} {
		if got := enterNativeSpike(entry, tc.a, tc.b, top); got != tc.want {
			t.Fatalf("enterNativeSpike(add, %d, %d) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestSpikeGSurvives calls native code that clobbers x19-x27, then does Go work
// after the trampoline restores them.
func TestSpikeGSurvives(t *testing.T) {
	var a enc.Asm
	// Clobber several callee-saved regs, then return x0+x1.
	a.MovImm64(enc.X19, 0x1111)
	a.MovImm64(enc.X26, 0xdeadbeef)
	a.MovImm64(enc.X27, 0x2222)
	a.Add64(enc.X0, enc.X0, enc.X1)
	a.Ret()

	code, err := mmapExec(a.B)
	if err != nil {
		t.Fatalf("mmapExec: %v", err)
	}
	stack, err := mmapRW(64 * 1024)
	if err != nil {
		t.Fatalf("mmapRW: %v", err)
	}
	entry := uintptr(unsafe.Pointer(&code[0]))
	top := uintptr(unsafe.Pointer(&stack[0])) + uintptr(len(stack))

	got := enterNativeSpike(entry, 7, 35, top)
	if got != 42 {
		t.Fatalf("got %d want 42", got)
	}
	// Force Go work that dereferences g (allocation + goroutine scheduling).
	sum := 0
	for i := 0; i < 10000; i++ {
		sum += i
	}
	if sum != 49995000 {
		t.Fatalf("post-call Go work wrong: %d", sum)
	}
}
