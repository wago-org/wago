//go:build (linux || darwin || windows) && (amd64 || arm64)

package runtime

import (
	"encoding/binary"
	"errors"
	"github.com/wago-org/wago/src/core/runtime/abi"
	goruntime "runtime"
	"testing"
)

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

// A native no-op leaves the trap untouched, so this checks the complete raw
// entry setup rather than relying on a stub that clears the trap itself.
func TestEngineCallTrapResetAndBinding(t *testing.T) {
	eng, jm, ar := fixture(t)
	nativeReturn := []byte{0xc3}
	if goruntime.GOARCH == "arm64" {
		nativeReturn = []byte{0xc0, 0x03, 0x5f, 0xd6}
	}
	code, entry, err := MapCode(nativeReturn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := Unmap(code); err != nil {
			t.Error(err)
		}
	}()
	trap, other := ar.Alloc(TrapBufferBytes), ar.Alloc(TrapBufferBytes)
	for _, initial := range []TrapCode{TrapNone, TrapBuiltin, TrapInterrupted} {
		storeTrap(trap, uint32(initial))
		binary.LittleEndian.PutUint64(trap[16:], ^uint64(0))
		jm.putU64(abi.TrapCellPtrOffset, uint64(slicePtr(other)))
		jm.putU64(abi.EHHandlerPtrOffset, 1234)
		err := eng.Call(entry, nil, jm.LinearMemory(), trap, nil)
		if initial == TrapInterrupted {
			var te *TrapError
			if !errors.As(err, &te) || te.Code != TrapInterrupted {
				t.Fatalf("interrupted entry: %v", err)
			}
		} else if err != nil {
			t.Fatalf("initial %v: %v", initial, err)
		}
		if !jm.HasTrapCell(trap) || jm.getU64(abi.EHHandlerPtrOffset) != 0 {
			t.Fatalf("initial %v: stale entry binding", initial)
		}
		if got := binary.LittleEndian.Uint64(trap[16:]); got != 0 {
			t.Fatalf("initial %v: stale trap payload %#x", initial, got)
		}
	}
	for length := 0; length < TrapBufferBytes; length++ {
		jm.putU64(abi.TrapCellPtrOffset, uint64(slicePtr(other)))
		jm.putU64(abi.EHHandlerPtrOffset, 1234)
		if err := eng.Call(entry, nil, jm.LinearMemory(), trap[:length], nil); err != errIncompleteTrapBuffer {
			t.Fatalf("short trap %d: %v", length, err)
		}
		if !jm.HasTrapCell(other) || jm.getU64(abi.EHHandlerPtrOffset) != 1234 {
			t.Fatalf("short trap %d changed entry binding", length)
		}
	}
}
