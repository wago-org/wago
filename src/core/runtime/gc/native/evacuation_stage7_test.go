//go:build !tinygo && !wago_target_tinygo

package gc

import (
	"bytes"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"
)

func TestEvacuationStage7Moves(t *testing.T) {
	parallelTestProcessors(t)
	for _, workers := range []int{1, 2, 4, 8} {
		for _, shift := range []uint8{0, 6, 12} {
			t.Run(fmt.Sprintf("workers%d/region%d", workers, shift), func(t *testing.T) {
				c, roots := stage7Heap(t, workers, 8448, shift, shift != 0)
				before := append([]handleEntry(nil), c.handles...)
				payload := bytes.Clone(c.bytes(roots[0]))
				if c.throughput.mature != nil {
					c.throughput.mature.pressure = true
				}
				if err := c.CollectFull(roots); err != nil {
					t.Fatal(err)
				}
				d := c.CollectionDiagnostics()
				if shift != 0 {
					if d.Evacuation != "moved" || d.MovedBytes != uint64(len(roots))*32 || d.CopyWorkers != workers {
						t.Fatalf("nonvacuous movement/participants: %+v", d)
					}
					for _, r := range roots {
						if c.entry(r).off == before[handleOf(r)].off {
							t.Fatal("did not move")
						}
					}
				} else {
					for _, r := range roots {
						if *c.entry(r) != before[handleOf(r)] {
							t.Fatal("default changed")
						}
					}
				}
				oracle := map[Ref]uint32{}
				for _, r := range roots {
					oracle[r] = 32
				}
				stage4Oracle(t, c, oracle)
				if !bytes.Equal(payload, c.bytes(roots[0])) {
					t.Fatal("payload changed")
				}
				if err := c.CollectFull(nil); err != nil {
					t.Fatal(err)
				}
				if c.Stats().LiveObjects != 0 {
					t.Fatal("retained identities")
				}
			})
		}
	}
}

func TestEvacuationStage7Rollback(t *testing.T) {
	parallelTestProcessors(t)
	for _, workers := range []int{2, 4, 8} {
		t.Run(fmt.Sprint(workers), func(t *testing.T) {
			c, roots := stage7Heap(t, workers, 8448, 6, true)
			h := &c.throughput
			before := append([]handleEntry(nil), c.handles...)
			payloads := make([][]byte, len(roots))
			for i, r := range roots {
				payloads[i] = bytes.Clone(c.bytes(r))
			}
			free := h.freeSpans()
			bump := h.bump
			p := c.parallelPool()
			var fired atomic.Bool
			p.taskHook = func(phase uint8, w int) {
				if phase == parallelCopy && w == 1 {
					fired.Store(true)
					panic(parallelInjectedFailure{})
				}
			}
			h.mature.pressure = true
			if err := c.CollectFull(roots); err == nil || !fired.Load() {
				t.Fatal("vacuous helper failure", err)
			}
			if !reflect.DeepEqual(before, c.handles) || h.bump != bump || !reflect.DeepEqual(free, h.freeSpans()) {
				t.Fatal("rollback ownership changed")
			}
			for i, r := range roots {
				if !bytes.Equal(payloads[i], c.bytes(r)) {
					t.Fatal("source changed before commit")
				}
			}
			if err := c.Verify(roots); err != nil {
				t.Fatal(err)
			}
			p.taskHook = nil
			if err := c.CollectFull(roots); err != nil {
				t.Fatal(err)
			}
			if c.CollectionDiagnostics().Evacuation != "moved" {
				t.Fatal("retry did not move")
			}
		})
	}
}

func TestEvacuationStage7YoungRunsAndZeroDemand(t *testing.T) {
	for _, workers := range []int{1, 2, 4, 8} {
		c := newTestCollector(t, Config{Workers: workers, NurseryRegionShift: 6, NurseryBytes: 1 << 20, SurvivorBytes: 1 << 20, MatureEvacuation: true, ThroughputHeapBytes: 8 << 20, VerifyAfterCollect: true})
		roots := make(RefSliceRoots, 4096)
		for i := range roots {
			r, err := c.NewStructDefault(0)
			if err != nil {
				t.Fatal(err)
			}
			roots[i] = r
		}
		if err := c.CollectMinor(roots); err != nil {
			t.Fatal(err)
		}
		if d := c.CollectionDiagnostics(); d.Demand != 0 || d.Runs != 0 {
			t.Fatal("survivor demanded old runs", d)
		}
		if err := c.CollectMinor(roots); err != nil {
			t.Fatal(err)
		}
		d := c.CollectionDiagnostics()
		if d.Demand != 4096*32 || d.Runs == 0 || d.ReservationWorkers != workers {
			t.Fatal(d)
		}
		if d.Need != d.Demand+uint64(workers)*32768 {
			t.Fatal("reservation depended on active copy", d)
		}
		oracle := map[Ref]uint32{}
		for _, r := range roots {
			oracle[r] = 32
		}
		stage4Oracle(t, c, oracle)
	}
}

func TestEvacuationStage7CardsLOSAndYoung(t *testing.T) {
	c := newTestCollector(t, Config{Workers: 4, NurseryRegionShift: 12, NurseryBytes: 1 << 20, SurvivorBytes: 1 << 20, MatureEvacuation: true, ThroughputHeapBytes: 8 << 20, ThroughputPageBytes: 4096, VerifyAfterCollect: true})
	parent, err := c.NewStructDefault(1)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.ForcePromote(parent); err != nil {
		t.Fatal(err)
	}
	// Place a nonmoving high-card LOS array in a different overlay block.
	gap, err := c.throughput.alloc(32768-32, spaceOld)
	if err != nil {
		t.Fatal(err)
	}
	large, err := c.NewArrayDefault(3, 20000)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.ForcePromote(large); err != nil {
		t.Fatal(err)
	}
	if err = c.throughput.free(gap); err != nil {
		t.Fatal(err)
	}
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.StructSet(child, 0, I32Value(197)); err != nil {
		t.Fatal(err)
	}
	if err = c.StructSet(parent, 0, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	for _, i := range []uint32{9999, 19999} {
		if err = c.ArraySet(large, i, RefValue(child)); err != nil {
			t.Fatal(err)
		}
	}
	// Self/cycle edges retain stable words through old relocation.
	if err = c.StructSet(parent, 1, RefValue(parent)); err != nil {
		t.Fatal(err)
	}
	beforeParent, beforeLarge, beforeChild := *c.entry(parent), *c.entry(large), *c.entry(child)
	cards := append([]objectCard(nil), c.objectCards...)
	nurseryBump := c.nurseryBump
	youngCopied := c.Stats().YoungBytesCopied
	c.throughput.mature.pressure = true
	if err = c.CollectFull(RefSliceRoots{parent, large}); err != nil {
		t.Fatal(err)
	}
	if c.entry(parent).off == beforeParent.off || *c.entry(large) != beforeLarge || *c.entry(child) != beforeChild || c.nurseryBump != nurseryBump || c.Stats().YoungBytesCopied != youngCopied {
		t.Fatal("old/LOS/young motion contract", c.CollectionDiagnostics())
	}
	if c.entry(parent).cardSlot != beforeParent.cardSlot || !reflect.DeepEqual(cards, c.objectCards) || c.lastCardBounds != (objectCardBounds{}) {
		t.Fatal("card identity")
	}
	if err = c.CollectMinor(RefSliceRoots{parent, large}); err != nil {
		t.Fatal(err)
	}
	if v, err := c.StructGet(child, 0); err != nil || v.Bits != 197 {
		t.Fatal("young child lost", v, err)
	}
	for _, i := range []uint32{9999, 19999} {
		v, err := c.ArrayGet(large, i)
		if err != nil || v.Ref != child {
			t.Fatal(v, err)
		}
	}
}

func TestEvacuationStage7DrainAndPoison(t *testing.T) {
	parallelTestProcessors(t)
	for _, destructive := range []bool{false, true} {
		t.Run(fmt.Sprint(destructive), func(t *testing.T) {
			c, roots := stage7Heap(t, 4, 8448, 6, true)
			before := append([]handleEntry(nil), c.handles...)
			blocked, release := make(chan struct{}), make(chan struct{})
			var once atomic.Bool
			p := c.parallelPool()
			p.taskHook = func(phase uint8, w int) {
				if phase == parallelCopy && w == 1 && once.CompareAndSwap(false, true) {
					close(blocked)
					<-release
					if destructive {
						panic("destructive invariant")
					}
					panic(parallelInjectedFailure{})
				}
			}
			c.throughput.mature.pressure = true
			done := make(chan error, 1)
			go func() { done <- c.CollectFull(roots) }()
			<-blocked
			if !reflect.DeepEqual(before, c.handles) {
				t.Fatal("publication before join")
			}
			select {
			case <-done:
				t.Fatal("coordinator passed held helper")
			default:
			}
			close(release)
			if err := <-done; err == nil {
				t.Fatal("missing abort")
			}
			if !reflect.DeepEqual(before, c.handles) {
				t.Fatal("failed copy published")
			}
			p.taskHook = nil
			if destructive {
				if _, err := c.NewStructDefault(0); err == nil {
					t.Fatal("poison admitted allocation")
				}
				v := c.NativeView()
				if v.StructAllocState != 0 || v.NurseryAllocBytes != 0 {
					t.Fatal("poisoned native admission")
				}
			} else {
				if err := c.CollectFull(roots); err != nil {
					t.Fatal(err)
				}
			}
			c.Close()
			c.Close()
		})
	}
}

func TestEvacuationStage7ActivePolicy(t *testing.T) {
	parallelTestProcessors(t)
	c, roots := stage7Heap(t, 8, 128, 6, true)
	_ = roots
	p := c.parallelPool()
	m := c.throughput.mature
	c.beginAttempt("minor")
	p.phase = parallelTrace
	p.selectParticipants(128)
	if p.participants != 1 {
		t.Fatal("small minor")
	}
	c.beginAttempt("major")
	m.diagnostics.Pressure = true
	p.selectParticipants(128)
	if p.participants != 8 {
		t.Fatal("pressure did not use admitted pool")
	}
	m.diagnostics.Pressure = false
	p.selectParticipants(128)
	if p.participants != 2 {
		t.Fatal("major tail collapsed")
	}
	p.phase = parallelSweep
	p.selectParticipants(128)
	if p.participants != 1 {
		t.Fatal("small old sweep")
	}
	for _, active := range []int{1, 8, 2, 1} {
		p.participants = active
		_, _, need, err := planEvacuationRuns([]throughputFreeSpan{{32768, 16 * 32768}}, nil, 4096, 32, c.cfg.Workers, 65536)
		if err != nil || need != 4096+8*32768 {
			t.Fatal("active count changed conservative reservation", need, err)
		}
	}
}

type stage7PanickingRoots struct{}

func (stage7PanickingRoots) RangeRoots(func(RootSlot) bool) { panic("root admission abort") }
func TestEvacuationStage7RootFailurePressure(t *testing.T) {
	c, roots := stage7Heap(t, 1, 1, 6, true)
	before := *c.entry(roots[0])
	c.throughput.mature.pressure = true
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("missing root panic")
			}
		}()
		_ = c.CollectFull(stage7PanickingRoots{})
	}()
	d := c.CollectionDiagnostics()
	if d.Result != "failed" || d.RequiredCause == nil || !d.Pressure || *c.entry(roots[0]) != before {
		t.Fatal(d)
	}
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	d = c.CollectionDiagnostics()
	if d.Result != "success" || d.RequiredCause != nil || d.Evacuation != "moved" {
		t.Fatal(d)
	}
}
