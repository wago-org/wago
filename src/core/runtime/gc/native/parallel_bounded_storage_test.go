//go:build !tinygo && !wago_target_tinygo

package gc

import (
	"slices"
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
	pendingBacking := &p.pending[0]
	queueBacking := &p.queue[:cap(p.queue)][0]
	if err = c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	if &p.pending[0] != pendingBacking || &p.queue[:cap(p.queue)][0] != queueBacking {
		t.Fatal("repeated trace replaced fixed pending or queue backing storage")
	}
}

func TestParallelArrayQueueRefusalHasNoPublishedTask(t *testing.T) {
	parallelTestProcessors(t)
	array, err := NewArrayDesc(0, StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{Workers: 8, ThroughputHeapBytes: 8 << 20}, []TypeDesc{array})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r, err := c.NewArray(0, 4*parallelArrayChunk, RefValue(Ref(0)))
	if err != nil {
		t.Fatal(err)
	}
	p := c.parallelPool()
	p.participants = 2
	p.queue = make([]traceTask, parallelQueueLimit)
	w := &p.workers[0]
	task := traceTask{handle: handleOf(r), first: parallelArrayChunk, end: 2 * parallelArrayChunk}
	before := p.outstanding.Load()
	if w.enqueueArray(task) {
		t.Fatal("full fixed queue admitted array continuation")
	}
	if p.outstanding.Load() != before || p.pendingVisible() {
		t.Fatal("queue refusal published or counted phantom work")
	}
	p.mu.Lock()
	p.queue = p.queue[:parallelQueueLimit-1]
	p.mu.Unlock()
	if !w.enqueueArray(task) {
		t.Fatal("available fixed queue slot refused array continuation")
	}
	if got := p.outstanding.Load(); got != before+1 {
		t.Fatalf("outstanding after admission=%d want=%d", got, before+1)
	}
	p.mu.Lock()
	got := p.queue[len(p.queue)-1]
	p.queue = p.queue[:0]
	p.mu.Unlock()
	if got != task {
		t.Fatalf("admitted task=%+v want=%+v", got, task)
	}
	p.outstanding.Add(-1)
}

func TestParallelObjectOverflowFixedBitsExactTasks(t *testing.T) {
	parallelTestProcessors(t)
	desc, _ := NewStructDesc(0, nil)
	c, err := NewCollector(Config{Workers: 8, ThroughputHeapBytes: 8 << 20}, []TypeDesc{desc})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	refs := make([]Ref, 129)
	for i := range refs {
		refs[i], err = c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
	}
	p := c.parallelPool()
	p.pending = make([]uint64, (len(c.handles)+63)/64)
	p.participants = 2
	p.queue = make([]traceTask, parallelQueueLimit)
	w := &p.workers[0]
	w.localLen = parallelPrivateLimit
	wants := make([]uint32, len(refs))
	for i, r := range refs {
		wants[i] = handleOf(r)
		w.enqueue(traceTask{handle: wants[i]})
	}
	if got := p.overflowTasks.Load(); got != int64(len(wants)) {
		t.Fatalf("pending object tasks=%d want=%d", got, len(wants))
	}
	p.mu.Lock()
	p.queue = p.queue[:0]
	p.mu.Unlock()
	got := make([]uint32, 0, len(wants))
	for range wants {
		task, ok := w.pop()
		if !ok || task.end != 0 {
			t.Fatalf("missing exact object overflow task: %+v %v", task, ok)
		}
		got = append(got, task.handle)
		p.outstanding.Add(-1)
	}
	slices.Sort(got)
	slices.Sort(wants)
	if !slices.Equal(got, wants) {
		t.Fatalf("overflow identities=%v want=%v", got, wants)
	}
	if task, ok := w.pop(); ok {
		t.Fatalf("phantom task after pending bits drained: %+v", task)
	}
	if p.overflowTasks.Load() != 0 || p.outstanding.Load() != 0 {
		t.Fatalf("post-drain pending/outstanding=%d/%d", p.overflowTasks.Load(), p.outstanding.Load())
	}
}

func TestParallelLargeArraysKeepGraph(t *testing.T) {
	parallelTestProcessors(t)
	leaf, _ := NewStructDesc(0, nil)
	array, _ := NewArrayDesc(1, StorageRefNull)
	for _, workers := range []int{2, 4, 8} {
		t.Run(string(rune('0'+workers)), func(t *testing.T) {
			c, err := NewCollector(Config{Workers: workers, ThroughputHeapBytes: 32 << 20}, []TypeDesc{leaf, array})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			child, err := c.NewStructDefault(0)
			if err != nil {
				t.Fatal(err)
			}
			root, err := c.NewArray(1, 12*parallelArrayChunk+17, RefValue(child))
			if err != nil {
				t.Fatal(err)
			}
			p := c.parallelPool()
			p.taskHook = func(uint8, int) {}
			if err = c.CollectFull(RefSliceRoots{root}); err != nil {
				t.Fatal(err)
			}
			p.taskHook = nil
			if got := c.Stats().LiveObjects; got != 2 {
				t.Fatalf("live objects=%d want=2", got)
			}
			if p.outstanding.Load() != 0 || p.pendingVisible() || len(p.queue) != 0 {
				t.Fatal("large-array trace retained or lost task accounting")
			}
		})
	}
}

func TestParallelForcedInlineResidues(t *testing.T) {
	parallelTestProcessors(t)
	for _, participants := range []int{1, 2, 4, 8} {
		t.Run(string(rune('0'+participants)), func(t *testing.T) {
			leaf, _ := NewStructDesc(0, nil)
			array, _ := NewArrayDesc(1, StorageRefNull)
			c, err := NewCollector(Config{Workers: 8, NurseryBytes: 4 << 20, ThroughputHeapBytes: 32 << 20}, []TypeDesc{leaf, array})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			const count = 19*parallelArrayChunk + 17
			root, err := c.NewArray(1, count, RefValue(Ref(0)))
			if err != nil {
				t.Fatal(err)
			}
			refs := make([]Ref, count)
			for i := range refs {
				refs[i], err = c.NewStructDefault(0)
				if err != nil {
					t.Fatal(err)
				}
				if err = c.ArraySet(root, uint32(i), RefValue(refs[i])); err != nil {
					t.Fatal(err)
				}
			}
			p := c.parallelPool()
			p.participants = participants
			p.epoch = 1
			c.mark = make([]bool, len(c.handles))
			p.claims = make([]uint32, len(c.handles))
			p.pending = make([]uint64, (len(c.handles)+63)/64)
			p.queue = p.queue[:parallelQueueLimit]
			w := &p.workers[0]
			w.localLen = parallelPrivateLimit
			// No consumer can free queue space: every range and successor must scan inline.
			w.scan(traceTask{handle: handleOf(root)})
			for _, r := range refs {
				if atomic.LoadUint32(&p.claims[handleOf(r)]) != 1 {
					t.Fatalf("P=%d missed handle %d", participants, handleOf(r))
				}
			}
			if p.outstanding.Load() != count {
				t.Fatalf("P=%d counted=%d want=%d", participants, p.outstanding.Load(), count)
			}
			if p.overflowTasks.Load() != count {
				t.Fatalf("P=%d pending=%d", participants, p.overflowTasks.Load())
			}
			// Drain actual bits before any reset; no phantom, duplicate or missing identity.
			p.queue = p.queue[:0]
			w.localLen = 0
			for range count {
				task, ok := w.pop()
				if !ok || task.end != 0 {
					t.Fatal("missing object")
				}
				w.scan(task)
				p.outstanding.Add(-1)
			}
			if _, ok := w.pop(); ok || p.outstanding.Load() != 0 || p.overflowTasks.Load() != 0 {
				t.Fatal("unbalanced drain")
			}
			if w.scanned != count+1 {
				t.Fatalf("scanned=%d", w.scanned)
			}
		})
	}
}

func TestParallelQueuedSuccessors(t *testing.T) {
	parallelTestProcessors(t)
	for _, participants := range []int{1, 2, 4, 8} {
		t.Run(string(rune('0'+participants)), func(t *testing.T) {
			leaf, _ := NewStructDesc(0, nil)
			array, _ := NewArrayDesc(1, StorageRefNull)
			c, err := NewCollector(Config{Workers: 8, NurseryBytes: 4 << 20, ThroughputHeapBytes: 32 << 20}, []TypeDesc{leaf, array})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			const count = 19*parallelArrayChunk + 17
			root, err := c.NewArray(1, count, RefValue(Ref(0)))
			if err != nil {
				t.Fatal(err)
			}
			refs := make([]Ref, count)
			for i := range refs {
				refs[i], err = c.NewStructDefault(0)
				if err != nil {
					t.Fatal(err)
				}
				if err = c.ArraySet(root, uint32(i), RefValue(refs[i])); err != nil {
					t.Fatal(err)
				}
			}
			p := c.parallelPool()
			p.participants = participants
			p.epoch = 1
			c.mark = make([]bool, len(c.handles))
			p.claims = make([]uint32, len(c.handles))
			p.pending = make([]uint64, (len(c.handles)+63)/64)
			p.queue = p.queue[:0]
			w := &p.workers[0]
			w.localLen = parallelPrivateLimit
			// Drain admitted ranges and descendants through the actual queue and bit paths.
			w.scan(traceTask{handle: handleOf(root)})
			for p.outstanding.Load() != 0 {
				task, ok := w.pop()
				if !ok {
					t.Fatal("missing queued task")
				}
				w.scan(task)
				p.outstanding.Add(-1)
			}
			for _, r := range refs {
				if p.claims[handleOf(r)] != 1 {
					t.Fatal("missing edge")
				}
			}
			if _, ok := w.pop(); ok || p.overflowTasks.Load() != 0 || w.scanned != count+1 {
				t.Fatalf("phantom/count scanned=%d pending=%d", w.scanned, p.overflowTasks.Load())
			}
		})
	}
}
