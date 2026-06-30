//go:build linux && amd64 && !tinygo

// These stress tests assert standard-Go runtime invariants — morestack stack
// relocation, the _Grunning contract, g-register restoration — and adversarially
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
	"time"
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
	trap := ar.Alloc(8)
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

// Test 6b: characterize the _Grunning stall. A long native run (in _Grunning,
// not _Gsyscall) cannot be async-preempted, so a concurrent grt.GC() cannot
// complete its stop-the-world until the native call returns. This is
// informational (proves "keep runs bounded" is a real requirement, and sets the
// cooperative-checkpoint budget for Phase 1) — not a hard correctness gate.
func TestGCStallCharacterization(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping characterization test in -short mode")
	}
	eng, jm, ar := fixture(t)
	code, err := mmapExec(stubLoopHeartbeat)
	if err != nil {
		t.Skipf("exec mapping denied: %v", err)
	}
	defer munmap(code)
	lin := jm.LinearMemory()
	serArgs := ar.Alloc(16)
	results := ar.Alloc(16)
	trap := ar.Alloc(8)
	binary.LittleEndian.PutUint32(serArgs, 2000000000) // ~2e9 iters: long native run

	// Observer: wait until native is demonstrably running (heartbeat != 0), then
	// time a grt.GC() requested *mid-run*. If the verified prediction holds,
	// the GC's stop-the-world mark-termination cannot complete until the native
	// loop returns a safepoint, so gcDur ~ remaining native time.
	gcDone := make(chan time.Duration, 1)
	go func() {
		for binary.LittleEndian.Uint32(lin) == 0 { // benign racy read of heartbeat
			grt.Gosched()
		}
		t0 := time.Now()
		grt.GC()
		gcDone <- time.Since(t0)
	}()

	t0 := time.Now()
	if err := eng.Call(slicePtr(code), serArgs, lin, trap, results); err != nil {
		t.Fatalf("Call: %v", err)
	}
	nativeDur := time.Since(t0)
	gcDur := <-gcDone

	t.Logf("native loop ran ~%v; grt.GC() requested mid-run took ~%v", nativeDur, gcDur)
	if got := binary.LittleEndian.Uint32(results); got != loopSentinel {
		t.Fatalf("corrupt result after long run: %#x", got)
	}
	switch {
	case gcDur > nativeDur/2:
		t.Logf("=> STALL CONFIRMED: GC blocked on the non-preemptible native run (Phase 1 must add cooperative checkpoints)")
	default:
		t.Logf("=> No significant stall on this host/GOMAXPROCS (GC made progress concurrently); still bound native runs in Phase 1")
	}
}
