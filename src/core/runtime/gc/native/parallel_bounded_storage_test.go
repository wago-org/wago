//go:build !tinygo && !wago_target_tinygo

package gc

import (
	"sync/atomic"
	"testing"
)

func TestParallelBoundedSeedOverflowPreservesWork(t *testing.T) {
	parallelTestProcessors(t)
	desc, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{Workers: 4, NurseryBytes: 1 << 20, ThroughputHeapBytes: 8 << 20}, []TypeDesc{desc})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	const count = parallelQueueLimit + 257
	roots := make(RefSliceRoots, count)
	for i := range roots {
		roots[i], err = c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
	}
	p := c.parallelPool()
	p.taskHook = func(uint8, int) {}
	if err = c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	p.taskHook = nil
	if c.Stats().LiveObjects != count {
		t.Fatalf("live objects=%d want=%d", c.Stats().LiveObjects, count)
	}
	for i := range p.workers {
		if p.workers[i].localLen > parallelPrivateLimit {
			t.Fatalf("worker %d private tasks=%d", i, p.workers[i].localLen)
		}
	}
	if len(p.queue) != 0 || p.outstanding.Load() != 0 || p.pendingVisible() {
		t.Fatal("parallel trace retained published work after join")
	}
}

func TestParallelBoundedArrayOverflowKeepsEveryChain(t *testing.T) {
	parallelTestProcessors(t)
	array, err := NewArrayDesc(0, StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{Workers: 4, ThroughputHeapBytes: 8 << 20}, []TypeDesc{array})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r, err := c.NewArray(0, 16*parallelArrayChunk, RefValue(Ref(0)))
	if err != nil {
		t.Fatal(err)
	}
	p := c.parallelPool()
	p.participants = 4
	p.pending = make([]atomic.Uint64, (len(c.handles)+63)/64)
	p.arrayNext = make([]atomic.Uint32, len(c.handles)*len(p.workers))
	p.queue = make([]traceTask, parallelQueueLimit)
	w := &p.workers[0]
	w.localLen = parallelPrivateLimit
	h := handleOf(r)
	for chain := uint32(1); chain <= 4; chain++ {
		w.enqueue(traceTask{handle: h, first: chain * parallelArrayChunk, end: (chain + 1) * parallelArrayChunk})
	}
	p.queue = p.queue[:0]
	seen := map[uint32]bool{}
	for range 4 {
		task, ok := w.pop()
		if !ok || task.end == 0 {
			t.Fatalf("missing overflow continuation: %+v %v", task, ok)
		}
		seen[task.first] = true
	}
	for chain := uint32(1); chain <= 4; chain++ {
		if !seen[chain*parallelArrayChunk] {
			t.Fatalf("lost array chain %d", chain)
		}
	}
}
