//go:build !tinygo && !wago_target_tinygo

package gc

import (
	"bytes"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
)

// Uses only APIs/fields present in both revisions: baseline must fail behavior,
// never compilation. Run with go test -timeout to bound collector deadlocks.
func TestProcessHelperBudgetRealCollections(t *testing.T) {
	old := runtime.GOMAXPROCS(8)
	t.Cleanup(func() { runtime.GOMAXPROCS(old) })
	for _, heldWorkers := range []int{8, 4} {
		want := 9 - heldWorkers // holder owns 7 or 3, leaving 0 or 4 helpers
		t.Run(fmt.Sprintf("active%d", want), func(t *testing.T) {
			holder := newTestCollector(t, Config{Workers: heldWorkers})
			holder.parallelPool()
			for _, kind := range []string{"graph", "mature"} {
				t.Run(kind, func(t *testing.T) {
					var c *Collector
					var roots RefSliceRoots
					if kind == "graph" {
						var parent Ref
						c, parent, _ = newParallelTestGraph(t, 8, true)
						roots = RefSliceRoots{parent}
					} else {
						c, roots = stage7Heap(t, 8, 8448, 6, true)
						c.throughput.mature.pressure = true
					}
					defer c.Close()
					p := c.parallelPool()
					var seen [3][8]atomic.Bool
					p.hook = func(phase uint8, worker int) { seen[phase][worker].Store(true) }
					payload := bytes.Clone(c.bytes(roots[0]))
					var err error
					if kind == "graph" {
						err = c.CollectMinor(roots)
					} else {
						err = c.CollectFull(roots)
					}
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(payload, c.bytes(roots[0])) {
						t.Fatal("collection changed payload")
					}
					if err = c.Verify(roots); err != nil {
						t.Fatal(err)
					}
					if kind == "graph" {
						if err = c.CollectFull(roots); err != nil {
							t.Fatal(err)
						}
					} else {
						d := c.CollectionDiagnostics()
						if d.Evacuation != "moved" || d.CopyWorkers != want || d.ReservationWorkers != 8 || d.Need != d.Demand+8*32768 {
							t.Fatalf("reservation/actual copy mismatch: %+v; want active=%d reservation=8", d, want)
						}
					}
					for _, phase := range []uint8{parallelTrace, parallelSweep, parallelCopy} {
						for worker := 0; worker < 8; worker++ {
							// Default graph policy selects at most workload-derived count, so
							// require actual owner work and forbid workers beyond the granted set.
							if worker == 0 && !seen[phase][worker].Load() {
								t.Errorf("phase %d owner did not execute", phase)
							}
							if worker >= want && seen[phase][worker].Load() {
								t.Errorf("phase %d dispatched denied worker %d (active limit %d)", phase, worker, want)
							}
						}
					}
					if kind == "mature" && !seen[parallelCopy][want-1].Load() {
						t.Fatal("partial grant copy did not dispatch last participant")
					}
					if err = c.CollectFull(nil); err != nil {
						t.Fatal(err)
					}
					if c.Stats().LiveObjects != 0 {
						t.Fatal("sweep retained unreachable objects")
					}
				})
			}
			holder.Close()
		})
	}
}
