//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package runtime

import "testing"

func TestClearTrapUnlessInterrupted(t *testing.T) {
	trap := make([]byte, 4)
	storeTrap(trap, uint32(TrapBuiltin))
	clearTrapUnlessInterrupted(trap)
	if got := TrapCode(loadTrap(trap)); got != TrapNone {
		t.Fatalf("ordinary trap reset = %v, want none", got)
	}
	storeTrap(trap, uint32(TrapInterrupted))
	clearTrapUnlessInterrupted(trap)
	if got := TrapCode(loadTrap(trap)); got != TrapInterrupted {
		t.Fatalf("close interruption reset = %v, want interrupted", got)
	}
}
