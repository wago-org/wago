//go:build linux && amd64 && tinygo

package runtime

import (
	"encoding/binary"
	grt "runtime"
	"testing"
)

// TestTinyGoBoundedRunStability exercises the TinyGo trampoline
// (trampoline_tinygo_amd64.go) over many native enter/exit cycles, forcing a GC
// between runs. Native code allocates nothing itself. With scheduler=threads,
// scripts/patch-tinygo-042.sh also supports concurrent collection through
// registered foreign mappings; TestTinyGoForeignStackConcurrentGC exercises
// that boundary with a deterministic guest/collector handshake.
func TestTinyGoBoundedRunStability(t *testing.T) {
	eng, jm, ar := fixture(t)

	code, err := mmapExec(stubLoop)
	if err != nil {
		t.Skipf("exec mapping denied: %v", err)
	}
	defer munmap(code)

	serArgs := ar.Alloc(16)
	results := ar.Alloc(16)
	trap := ar.Alloc(TrapBufferBytes)
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
