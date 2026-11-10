//go:build ((linux || darwin || windows) && (amd64 || arm64)) || (linux && riscv64 && !tinygo && !wago_target_tinygo)

package runtime

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestTrapNativeOriginConsumedOnceConcurrently(t *testing.T) {
	err := &TrapError{nativeOrigin: 0x12345678, nativeOriginPresent: true}
	var winners atomic.Int32
	var workers sync.WaitGroup
	for range 16 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			identity, present, pending := err.TakeNativeOrigin()
			if pending {
				winners.Add(1)
				if identity != 0x12345678 || !present {
					t.Errorf("winning provenance = %#x, %t", identity, present)
				}
			} else if identity != 0 || present {
				t.Error("loser retained provenance")
			}
		}()
	}
	workers.Wait()
	if winners.Load() != 1 || err.nativeOrigin != 0 || err.nativeOriginPresent {
		t.Fatal("origin was not consumed exactly once")
	}
	if identity, present, pending := err.TakeNativeOrigin(); identity != 0 || present || pending {
		t.Fatal("rethrow reacquired provenance")
	}
}
