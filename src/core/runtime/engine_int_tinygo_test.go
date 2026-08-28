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

func TestPreparedIntThunkOwnedByEngine(t *testing.T) {
	e := &Engine{}
	first, err := preparedIntThunkFor(e)
	if err != nil {
		t.Fatal(err)
	}
	second, err := preparedIntThunkFor(e)
	if err != nil {
		t.Fatal(err)
	}
	if first == 0 || second != first {
		t.Fatalf("prepared integer entry: first=%#x second=%#x", first, second)
	}
	if len(e.preparedInt.mem) == 0 {
		t.Fatal("prepared integer entry has no owned mapping")
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	if e.preparedInt.entry != 0 || e.preparedInt.mem != nil {
		t.Fatal("prepared integer entry mapping retained after engine close")
	}
}
