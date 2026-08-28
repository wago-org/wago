//go:build linux && (amd64 || arm64) && !tinygo && !wago_target_tinygo

package runtime

import (
	goruntime "runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestJobMemoryLifecycleRegistersInterruptLinearMemory(t *testing.T) {
	jm, err := NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	base := jm.LinMemBase()
	if !interruptLinearMemoryRegistered(base) {
		t.Fatal("created linear memory was not published to interrupt handler")
	}
	if err := jm.Close(); err != nil {
		t.Fatal(err)
	}
	if interruptLinearMemoryRegistered(base) {
		t.Fatal("closed linear memory remained published")
	}
}

func TestUnregisterWaitsForSignalReaders(t *testing.T) {
	const base = uintptr(3)
	if err := registerInterruptLinearMemory(base); err != nil {
		t.Fatal(err)
	}
	// Model a handler that acquired its reader slot before Close took the gate.
	atomic.StoreUint32(&interruptLinearMemoryState, 1)
	done := make(chan struct{})
	go func() {
		unregisterInterruptLinearMemory(base)
		close(done)
	}()
	deadline := time.Now().Add(time.Second)
	for atomic.LoadUint32(&interruptLinearMemoryState)&interruptLinearMemoryWriter == 0 && time.Now().Before(deadline) {
		goruntime.Gosched()
	}
	if atomic.LoadUint32(&interruptLinearMemoryState)&interruptLinearMemoryWriter == 0 {
		atomic.AddUint32(&interruptLinearMemoryState, ^uint32(0))
		<-done
		t.Fatal("unregister did not close the signal-reader gate")
	}
	select {
	case <-done:
		t.Fatal("unregister returned while a signal reader was active")
	default:
	}
	atomic.AddUint32(&interruptLinearMemoryState, ^uint32(0))
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("unregister did not resume after the signal reader left")
	}
	if interruptLinearMemoryRegistered(base) {
		t.Fatal("base remained registered after reader quiescence")
	}
}

func TestInterruptLinearMemoryRegistrationScansPastHoles(t *testing.T) {
	const first, second = uintptr(1), uintptr(2)
	if err := registerInterruptLinearMemory(first); err != nil {
		t.Fatal(err)
	}
	if err := registerInterruptLinearMemory(second); err != nil {
		t.Fatal(err)
	}
	unregisterInterruptLinearMemory(first)
	if err := registerInterruptLinearMemory(second); err != nil {
		t.Fatal(err)
	}
	count := 0
	for i := uint32(0); i < atomic.LoadUint32(&interruptLinearMemoryLimit); i++ {
		if atomic.LoadUintptr(&interruptLinearMemories[i]) == second {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("second base registrations = %d, want 1", count)
	}
	unregisterInterruptLinearMemory(second)
	if interruptLinearMemoryRegistered(second) {
		t.Fatal("second base remained registered")
	}
}

func interruptLinearMemoryRegistered(base uintptr) bool {
	for i := uint32(0); i < atomic.LoadUint32(&interruptLinearMemoryLimit); i++ {
		if atomic.LoadUintptr(&interruptLinearMemories[i]) == base {
			return true
		}
	}
	return false
}
