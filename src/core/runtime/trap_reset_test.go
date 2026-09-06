//go:build (linux || darwin || windows) && (amd64 || arm64)

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

func TestTrapResetPreservesConcurrentInterruption(t *testing.T) {
	for _, initial := range []TrapCode{TrapNone, TrapBuiltin} {
		for i := 0; i < 1000; i++ {
			trap := make([]byte, TrapBufferBytes)
			storeTrap(trap, uint32(initial))
			done := make(chan struct{})
			go func() { storeTrap(trap, uint32(TrapInterrupted)); close(done) }()
			clearTrapUnlessInterrupted(trap)
			<-done
			if got := TrapCode(loadTrap(trap)); got != TrapInterrupted {
				t.Fatalf("initial %v: interruption changed to %v", initial, got)
			}
		}
	}
}
