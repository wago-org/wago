//go:build linux && amd64 && tinygo

package runtime

import (
	"encoding/binary"
	grt "runtime"
	"testing"
)

// TestTinyGoBoundedRunStability exercises the TinyGo trampoline
// (trampoline_tinygo_amd64.go) over many native enter/exit cycles, forcing a GC
// between runs. This is the supported TinyGo pattern: native code allocates
// nothing, so a collection can only occur between bounded runs — never while a
// thread is switched onto the foreign stack. A botched foreign-stack switch or a
// clobbered callee-saved register would corrupt results or crash here.
//
// (The standard-Go suite additionally storms runtime.GC() *concurrently* with
// native execution; see stress_test.go. That is unsafe under TinyGo's
// conservative collector with a threaded scheduler, which would scan a thread
// stopped mid-run with RSP on the foreign stack — a documented limitation.)
func TestTinyGoBoundedRunStability(t *testing.T) {
	eng, jm, ar := fixture(t)

	code, err := mmapExec(stubLoop)
	if err != nil {
		t.Skipf("exec mapping denied: %v", err)
	}
	defer munmap(code)

	serArgs := ar.Alloc(16)
	results := ar.Alloc(16)
	trap := ar.Alloc(8)
	lin := jm.LinearMemory()

	const iters = 50000
	for i := 0; i < iters; i++ {
		binary.LittleEndian.PutUint32(serArgs, uint32(i%64)) // bounded loop count
		if err := eng.Call(slicePtr(code), serArgs, lin, trap, results); err != nil {
			t.Fatalf("run %d: Call: %v", i, err)
		}
		if got := binary.LittleEndian.Uint32(results); got != loopSentinel {
			t.Fatalf("run %d: results = %#x, want sentinel %#x", i, got, loopSentinel)
		}
		if got := binary.LittleEndian.Uint32(lin); got != loopSentinel {
			t.Fatalf("run %d: linMem = %#x, want sentinel %#x", i, got, loopSentinel)
		}
		if i%2000 == 0 {
			grt.GC() // collect between (never during) native runs
		}
	}
}
