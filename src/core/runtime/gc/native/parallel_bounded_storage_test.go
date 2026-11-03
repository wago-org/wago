//go:build !tinygo && !wago_target_tinygo

package gc

import (
	"sync"
	"testing"
	"time"
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
	p.queue = make([]traceTask, parallelQueueLimit)
	w := &p.workers[0]
	w.localLen = parallelPrivateLimit
	h := handleOf(r)
	for chain := uint32(1); chain <= 4; chain++ {
		w.enqueue(traceTask{handle: h, first: chain * parallelArrayChunk, end: (chain + 1) * parallelArrayChunk})
	}
	if got := p.outstanding.Load(); got != 4 {
		t.Fatalf("outstanding before consumption=%d want=4", got)
	}
	p.mu.Lock()
	p.queue = p.queue[:0]
	p.mu.Unlock()
	seen := map[uint32]bool{}
	for range 4 {
		task, ok := w.pop()
		if !ok || task.end == 0 {
			t.Fatalf("missing overflow continuation: %+v %v", task, ok)
		}
		seen[task.first] = true
		p.outstanding.Add(-1)
	}
	for chain := uint32(1); chain <= 4; chain++ {
		if !seen[chain*parallelArrayChunk] {
			t.Fatalf("lost array chain %d", chain)
		}
	}
	if task, ok := w.pop(); ok {
		t.Fatalf("phantom task after continuations drained: %+v", task)
	}
	if got := p.outstanding.Load(); got != 0 {
		t.Fatalf("outstanding after exact completion=%d want=0", got)
	}
}

func TestParallelOverflowPublicationIsAtomic(t *testing.T) {
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
	r, err := c.NewArray(0, 8*parallelArrayChunk, RefValue(Ref(0)))
	if err != nil {
		t.Fatal(err)
	}
	p := c.parallelPool()
	p.participants = 4
	p.queue = make([]traceTask, parallelQueueLimit)
	w := &p.workers[0]
	w.localLen = parallelPrivateLimit
	installed, resume := make(chan struct{}), make(chan struct{})
	var resumeOnce sync.Once
	release := func() { resumeOnce.Do(func() { close(resume) }) }
	p.overflowHook = func() {
		// The old protocol allowed this pre-existing queue token to expose a
		// separately stored cursor. The corrected protocol holds mu across both.
		p.queue = p.queue[:0]
		close(installed)
		<-resume
	}
	defer func() { p.overflowHook = nil }()
	producerDone := make(chan struct{})
	defer func() {
		release()
		select {
		case <-producerDone:
		case <-time.After(2 * time.Second):
			t.Error("producer remained blocked during cleanup")
		}
	}()
	go func() {
		w.enqueue(traceTask{handle: handleOf(r), first: parallelArrayChunk, end: 2 * parallelArrayChunk})
		close(producerDone)
	}()
	select {
	case <-installed:
	case <-time.After(2 * time.Second):
		t.Fatal("producer did not install overflow state")
	}
	popResult := make(chan traceTask, 1)
	go func() {
		task, ok := w.pop()
		if ok {
			popResult <- task
		} else {
			close(popResult)
		}
	}()
	select {
	case task := <-popResult:
		t.Fatalf("consumer observed partial publication: %+v", task)
	case <-time.After(10 * time.Millisecond):
	}
	release()
	select {
	case <-producerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("producer did not complete")
	}
	select {
	case task, ok := <-popResult:
		if !ok || task.first != parallelArrayChunk || task.end != 2*parallelArrayChunk {
			t.Fatalf("published continuation=%+v ok=%v", task, ok)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("consumer did not receive publication")
	}
	if task, ok := w.pop(); ok {
		t.Fatalf("stale publication produced task: %+v", task)
	}
	if got := p.outstanding.Load(); got != 1 {
		t.Fatalf("outstanding before completion=%d want=1", got)
	}
	p.outstanding.Add(-1)
}

func TestParallelOverflowActiveParticipantsAndTaskKinds(t *testing.T) {
	parallelTestProcessors(t)
	array, _ := NewArrayDesc(0, StorageRefNull)
	c, err := NewCollector(Config{Workers: 8, ThroughputHeapBytes: 8 << 20}, []TypeDesc{array})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r, err := c.NewArray(0, 8*parallelArrayChunk, RefValue(Ref(0)))
	if err != nil {
		t.Fatal(err)
	}
	p := c.parallelPool()
	p.participants = 2
	p.queue = make([]traceTask, parallelQueueLimit)
	w := &p.workers[0]
	w.localLen = parallelPrivateLimit
	h := handleOf(r)
	w.enqueue(traceTask{handle: h})
	w.enqueue(traceTask{handle: h, first: parallelArrayChunk, end: 2 * parallelArrayChunk})
	w.enqueue(traceTask{handle: h, first: 2 * parallelArrayChunk, end: 3 * parallelArrayChunk})
	p.mu.Lock()
	p.queue = p.queue[:0]
	p.mu.Unlock()
	want := []traceTask{{handle: h}, {handle: h, first: 2 * parallelArrayChunk, end: 3 * parallelArrayChunk}, {handle: h, first: parallelArrayChunk, end: 2 * parallelArrayChunk}}
	for i, expected := range want {
		task, ok := w.pop()
		if !ok || task != expected {
			t.Fatalf("task %d=%+v ok=%v want=%+v", i, task, ok, expected)
		}
		p.outstanding.Add(-1)
	}
	if task, ok := w.pop(); ok {
		t.Fatalf("extra task with two active/eight configured workers: %+v", task)
	}
	if got := p.outstanding.Load(); got != 0 {
		t.Fatalf("outstanding=%d want=0", got)
	}
}
