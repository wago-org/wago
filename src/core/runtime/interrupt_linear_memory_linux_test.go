//go:build linux && (amd64 || arm64) && !tinygo && !wago_target_tinygo

package runtime

import (
	"sync/atomic"
	"testing"
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
