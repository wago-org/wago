//go:build linux && (amd64 || arm64) && !tinygo

// These stress tests assert standard-Go runtime invariants — morestack stack
// relocation, the syscall boundary, g-register restoration — and adversarially
// storm runtime.GC() concurrently with native execution. None of that maps onto
// TinyGo (no morestack, a conservative non-moving collector, a different
// scheduler), so the file is excluded from the TinyGo build. See
// stress_tinygo_test.go for the TinyGo-appropriate bounded-run stability test.

package runtime

import (
	"encoding/binary"
	grt "runtime"
	"sync"
	"sync/atomic"
	"testing"
)

// deepGo forces real Go stack growth (morestack) so we can confirm that, right
// after returning from native code, g/RSP/stack are healthy and the Go stack is
// still relocatable. A botched g restore in the trampoline corrupts this.
//
//go:noinline
func deepGo(n int) int {
	if n == 0 {
		return 0
	}
	var buf [512]byte
	buf[0] = byte(n)
	return int(buf[0]) + deepGo(n-1)
}

// Test 6a: bounded-run GC/preemption stress (item f). Hammer enter/return
// transitions while another goroutine forces GC and others allocate. Must pass
// cleanly, including under -race (checkptr).
func TestGCPreemptStressBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in -short mode")
	}
	eng, jm, ar := fixture(t)
	code, err := mmapExec(stubLoop)
	if err != nil {
		t.Skipf("exec mapping denied: %v", err)
	}
	defer munmap(code)
	lin := jm.LinearMemory()
	serArgs := ar.Alloc(16)
	results := ar.Alloc(16)
	trap := ar.Alloc(TrapBufferBytes)
	binary.LittleEndian.PutUint32(serArgs, 2000) // iterations per native call

	var stop atomic.Bool
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for !stop.Load() {
			grt.GC()
		}
	}()
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var sink [][]byte
			for !stop.Load() {
				sink = append(sink, make([]byte, 1024))
				if len(sink) > 256 {
					sink = sink[:0]
				}
			}
		}()
	}

	const iterations = 200000
	for i := 0; i < iterations; i++ {
		if err := eng.Call(slicePtr(code), serArgs, lin, trap, results); err != nil {
			stop.Store(true)
			t.Fatalf("iter %d: %v", i, err)
		}
		if got := binary.LittleEndian.Uint32(results); got != loopSentinel {
			stop.Store(true)
			t.Fatalf("iter %d: corrupt result %#x (want %#x)", i, got, loopSentinel)
		}
		if got := binary.LittleEndian.Uint32(lin); got != loopSentinel {
			stop.Store(true)
			t.Fatalf("iter %d: corrupt linMem[0] %#x (want %#x)", i, got, loopSentinel)
		}
		if i%2000 == 0 {
			_ = deepGo(40) // force a real Go stack move after returning from native
		}
	}

	stop.Store(true)
	wg.Wait()
	grt.GC()
}
