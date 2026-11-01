//go:build !tinygo && !wago_target_tinygo

package gc

import (
	"bytes"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
)

func TestParallelStage6PolicyOracle(t *testing.T) {
	parallelTestProcessors(t)
	for _, shift := range []uint8{0, 6, 12} {
		for _, workers := range []int{1, 2, 8} {
			t.Run(fmt.Sprintf("region%d/workers%d", shift, workers), func(t *testing.T) {
				c, parent, refs := newParallelTestGraph(t, workers, false, shift)
				roots := RefSliceRoots{parent}
				for _, minor := range []bool{false, false, true, true, false} {
					var err error
					if minor {
						err = c.CollectMinor(roots)
					} else {
						err = c.CollectFull(roots)
					}
					if err != nil {
						t.Fatal(err)
					}
					want, err := shadowTraceLive(c, roots)
					if err != nil {
						t.Fatal(err)
					}
					for h := 1; h < len(c.handles); h++ {
						if (c.handles[h].space != spaceFree) != want[h] {
							t.Fatalf("oracle mismatch %d", h)
						}
					}
				}
				if err := c.CollectFull(nil); err != nil {
					t.Fatal(err)
				}
				if c.Stats().LiveObjects != 0 {
					t.Fatal("stale helper marks retained graph")
				}
				// Reuse formerly high marked handles; newly installed edges must be scanned.
				a, err := c.NewStructDefault(0)
				if err != nil {
					t.Fatal(err)
				}
				b, err := c.NewStructDefault(0)
				if err != nil {
					t.Fatal(err)
				}
				if err = c.StructSet(a, 0, RefValue(b)); err != nil {
					t.Fatal(err)
				}
				if err = c.CollectFull(RefSliceRoots{a}); err != nil {
					t.Fatal(err)
				}
				if c.Stats().LiveObjects != 2 {
					t.Fatal("reuse lost newly reachable child", refs[len(refs)-1])
				}
			})
		}
	}
}

func TestParallelStage6FailureMatrix(t *testing.T) {
	parallelTestProcessors(t)
	for _, shift := range []uint8{0, 6, 12} {
		for _, phase := range []uint8{parallelTrace, parallelSweep, parallelCopy} {
			t.Run(fmt.Sprintf("region%d/phase%d", shift, phase), func(t *testing.T) {
				c, parent, refs := newParallelTestGraph(t, 4, true, shift)
				roots := append(RefSliceRoots{parent}, refs...)
				p := c.parallelPool()
				var fired atomic.Bool
				p.taskHook = func(ph uint8, w int) {
					if ph == phase && w == 1 {
						fired.Store(true)
						panic(parallelInjectedFailure{})
					}
				}
				before := append([]handleEntry(nil), c.handles...)
				payloads := make(map[uint32][]byte)
				for h, e := range before {
					if e.space != spaceFree {
						payloads[uint32(h)] = bytes.Clone(c.bytes(makeObjRef(uint32(h))))
					}
				}
				var err error
				if phase == parallelCopy {
					err = c.CollectMinor(roots)
				} else {
					err = c.CollectFull(roots)
				}
				if err == nil || !fired.Load() {
					t.Fatal("fault was not contained on a real helper", err)
				}
				for h, e := range before {
					if c.handles[h] != e {
						t.Fatalf("failed phase published handle %d", h)
					}
				}
				for h, b := range payloads {
					if !bytes.Equal(b, c.bytes(makeObjRef(h))) {
						t.Fatalf("failed copy changed source %d", h)
					}
				}
				if p.active != 0 || len(p.queue) != 0 {
					t.Fatal("phase not drained")
				}
				if err = c.Verify(roots); err != nil {
					t.Fatal(err)
				}
				p.taskHook = nil
				if phase == parallelCopy {
					err = c.CollectMinor(roots)
				} else {
					err = c.CollectFull(roots)
				}
				if err != nil {
					t.Fatal("retry", err)
				}
				if err = c.CollectFull(nil); err != nil {
					t.Fatal(err)
				}
				if c.Stats().LiveObjects != 0 {
					t.Fatal("failed phase leaked marks")
				}
			})
		}
	}
}

func TestParallelStage6DelayedCompletionAndWrap(t *testing.T) {
	parallelTestProcessors(t)
	c := newTestCollector(t, Config{Workers: 8})
	defer c.Close()
	p := c.parallelPool()
	p.phase = parallelSweep
	for _, seq := range []uint64{0xfffffffe, 0xffffffff, 1 << 32, ^uint64(0)} {
		p.sequence = seq
		entered, release := make(chan struct{}), make(chan struct{})
		p.hook = func(_ uint8, w int) {
			if w == 1 {
				close(entered)
				<-release
			}
		}
		done := make(chan struct{})
		p.selectParticipants(1)
		go func() { p.run(); close(done) }()
		<-entered
		select {
		case <-done:
			t.Fatal("join returned before delayed participant")
		default:
		}
		close(release)
		<-done
		if p.current.sequence != seq+1 || !p.current.done[1] {
			t.Fatal("completion identity")
		}
		p.hook = nil
		p.selectParticipants(parallelMinObjects * 8)
		p.run()
		if p.participants != 8 {
			t.Fatal("active set failed to grow")
		}
	}
	c.Close()
	c.Close()
}

func TestParallelStage6Poison(t *testing.T) {
	parallelTestProcessors(t)
	c, parent, _ := newParallelTestGraph(t, 4, false)
	p := c.parallelPool()
	p.hook = func(_ uint8, w int) {
		if w == 1 {
			panic("unexpected helper invariant")
		}
	}
	if err := c.CollectFull(RefSliceRoots{parent}); err == nil {
		t.Fatal("panic not contained")
	}
	if p.poison == nil {
		t.Fatal("missing sticky poison")
	}
	p.hook = nil
	if err := c.CollectFull(nil); err == nil {
		t.Fatal("poisoned collector reused")
	}
	if _, err := c.NewStructDefault(0); err == nil {
		t.Fatal("poisoned allocator reused")
	}
	c.Close()
}

func TestParallelStage6HelperExitDrainsPeers(t *testing.T) {
	parallelTestProcessors(t)
	c, parent, _ := newParallelTestGraph(t, 4, false)
	p := c.parallelPool()
	delayed, release := make(chan struct{}), make(chan struct{})
	p.hook = func(_ uint8, w int) {
		if w == 2 {
			close(delayed)
			<-release
		}
		if w == 1 {
			<-delayed
			runtime.Goexit()
		}
	}
	done := make(chan error)
	go func() { done <- c.CollectFull(RefSliceRoots{parent}) }()
	<-delayed
	select {
	case <-done:
		t.Fatal("reused phase while peer blocked")
	default:
	}
	close(release)
	if err := <-done; err == nil {
		t.Fatal("helper exit accepted")
	}
	if p.poison == nil {
		t.Fatal("dead helper did not poison")
	}
	c.Close()
}

func TestParallelStage6RootPanicBetweenPhases(t *testing.T) {
	parallelTestProcessors(t)
	c, parent, _ := newParallelTestGraph(t, 4, false)
	if err := c.CollectFull(RefSliceRoots{parent}); err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("root callback did not panic")
			}
		}()
		_ = c.CollectFull(panickingQueuedRoots{parent})
	}()
	if err := c.CollectMinor(RefSliceRoots{parent}); err != nil {
		t.Fatal(err)
	}
	if err := c.CollectFull(nil); err != nil {
		t.Fatal(err)
	}
	if c.Stats().LiveObjects != 0 {
		t.Fatal("interrupted roots retained helper marks")
	}
}

func TestParallelStage6QueueOverflowRetained(t *testing.T) {
	parallelTestProcessors(t)
	c := newTestCollector(t, Config{Workers: 4})
	defer c.Close()
	p := c.parallelPool()
	w := &p.workers[0]
	p.queue = make([]traceTask, parallelQueueLimit)
	for i := range p.queue {
		p.queue[i].handle = uint32(i + 1)
	}
	w.local = []traceTask{{handle: 9001}, {handle: 9002}}
	w.share()
	if len(p.queue) != parallelQueueLimit || len(w.local) != 2 || w.local[0].handle != 9001 || w.local[1].handle != 9002 {
		t.Fatal("full shared queue lost private overflow")
	}
	p.queue = p.queue[:0]
	w.share()
	if len(p.queue) != 1 || len(w.local) != 1 || p.queue[0].handle != 9002 {
		t.Fatal("overflow cannot resume sharing")
	}
}

func TestParallelStage6DuplicateCompletionPoisons(t *testing.T) {
	parallelTestProcessors(t)
	c := newTestCollector(t, Config{Workers: 2})
	defer c.Close()
	p := c.parallelPool()
	p.phase = parallelSweep
	p.run()
	old := p.current
	p.workers[0].execute(old)
	if p.poison == nil {
		t.Fatal("duplicate dispatch accepted")
	}
}

func TestParallelStage6NativeCancelBeforeDispatch(t *testing.T) {
	parallelTestProcessors(t)
	for _, shift := range []uint8{0, 6, 12} {
		t.Run(fmt.Sprint(shift), func(t *testing.T) {
			c, parent, _ := newParallelTestGraph(t, 4, false, shift)
			if !c.PrepareNativeAllocation(32) {
				t.Fatal("reservation refused")
			}
			epoch := c.nativeAllocEpoch
			if err := c.CollectFull(RefSliceRoots{parent}); err != nil {
				t.Fatal(err)
			}
			if c.nativeStructAlloc.Count != 0 || c.nativeAllocEpoch == epoch {
				t.Fatal("helpers started with pending reservation")
			}
			if c.parallel == nil {
				t.Fatal("missed helpers")
			}
			if err := c.CollectMinor(RefSliceRoots{parent}); err != nil {
				t.Fatal(err)
			}
			if err := c.CollectFull(nil); err != nil {
				t.Fatal(err)
			}
			if c.Stats().LiveObjects != 0 {
				t.Fatal("cancel retained handles")
			}
		})
	}
}
