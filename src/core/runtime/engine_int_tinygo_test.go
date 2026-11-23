//go:build tinygo && ((linux && amd64) || ((linux || darwin) && arm64))

package runtime

import (
	"testing"
	"unsafe"
)

func TestTinyGoTrampolineFailurePreservesInterrupt(t *testing.T) {
	trap := uint32(TrapInterrupted)
	markTinyGoTrampolineFailure(uintptr(unsafe.Pointer(&trap)))
	if got := TrapCode(trap); got != TrapInterrupted {
		t.Fatalf("pending interrupt changed to %v", got)
	}
	trap = 0
	markTinyGoTrampolineFailure(uintptr(unsafe.Pointer(&trap)))
	if got := TrapCode(trap); got != TrapBuiltin {
		t.Fatalf("ordinary mapping failure = %v, want builtin trap", got)
	}
}
