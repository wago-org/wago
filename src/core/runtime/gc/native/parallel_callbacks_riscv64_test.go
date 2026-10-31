//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package gc

import "testing"

// Ownership callbacks are not promised safe for concurrent invocation.
func TestParallelOwnershipCallbacksKeepSerialTrace(t *testing.T) {
	parallelTestProcessors(t)
	c, parent, _ := newParallelTestGraph(t, 4, false)
	c.SetObjectEdgeSource(parallelEmptyEdges{})
	c.clearMarks()
	c.markRoots(RefSliceRoots{parent})
	if c.parallel != nil {
		t.Fatal("ownership callback trace started helpers")
	}
}

type parallelEmptyEdges struct{}

func (parallelEmptyEdges) ObjectEdgeCount(Ref) uint32   { return 0 }
func (parallelEmptyEdges) ObjectEdgeAt(Ref, uint32) Ref { return Null() }
