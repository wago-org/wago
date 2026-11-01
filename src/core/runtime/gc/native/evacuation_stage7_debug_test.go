//go:build wagodebug && !tinygo && !wago_target_tinygo

package gc

import "testing"

func TestEvacuationStage7PartialAdmissionRollback(t *testing.T) {
	for _, point := range []failurePoint{failMatureMetadata, failBackingGrowth} {
		c, roots := stage7Heap(t, 8, 1, 6, true)
		h := &c.throughput
		before := *c.entry(roots[0])
		bump := h.bump
		backing := len(h.mem)
		h.mature.pressure = true
		disarm := armFailure(h, point, 1)
		err := c.CollectFull(roots)
		disarm()
		if err != nil || c.CollectionDiagnostics().Evacuation != "metadata" {
			t.Fatal("second run fault not reached", point, err, c.CollectionDiagnostics())
		}
		if *c.entry(roots[0]) != before || h.bump != bump || len(h.mem) != backing {
			t.Fatal("partial run rollback")
		}
		if err := c.Verify(roots); err != nil {
			t.Fatal(err)
		}
	}
}
