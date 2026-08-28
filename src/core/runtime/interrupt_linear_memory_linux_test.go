//go:build linux && (amd64 || arm64) && !tinygo && !wago_target_tinygo

package runtime

import (
	"sync/atomic"
	"testing"
)

func TestBindTrapCellRegistersInterruptLinearMemory(t *testing.T) {
	jm, err := NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	base := jm.LinMemBase()
	if err := jm.BindTrapCell(make([]byte, TrapBufferBytes)); err != nil {
		t.Fatal(err)
	}
	if !interruptLinearMemoryRegistered(base) {
		t.Fatal("bound linear memory was not published to interrupt handler")
	}
	if err := jm.Close(); err != nil {
		t.Fatal(err)
	}
	if interruptLinearMemoryRegistered(base) {
		t.Fatal("closed linear memory remained published")
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
