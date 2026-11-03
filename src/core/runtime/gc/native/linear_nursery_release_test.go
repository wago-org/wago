//go:build linux && !tinygo && !wago_target_tinygo

package gc

import (
	"slices"
	"testing"
)

func linearNurseryReleaseFixture(t *testing.T) *Collector {
	t.Helper()
	c := newTestCollector(t, Config{
		NurseryBytes:         4096,
		ThroughputHeapBytes:  4096,
		ThroughputPageBytes:  4096,
		ThroughputClassLimit: 4096,
	})
	c.handles = []handleEntry{
		{},
		handleEntry{off: 0, size: 32, space: spaceNursery},
		handleEntry{off: 32, size: 32, space: spaceNursery},
		handleEntry{off: 64, size: 32, space: spaceNursery},
		handleEntry{off: 96, size: 32, space: spaceNursery},
	}
	c.mark = []bool{false, false, false, true, false}
	c.nurseryHandles = append(c.nurseryHandles, 3, 1, 4, 2)
	c.nurseryBump = 128
	c.lastCardBounds = objectCardBounds{head: 7, oppositeEdge: 9}
	return c
}

func TestLinearNurseryReleaseMatchesObserverForcedGenericPath(t *testing.T) {
	if !nativeCollectorFastPaths {
		t.Skip("native collector fast path unavailable")
	}
	fast := linearNurseryReleaseFixture(t)
	oracle := linearNurseryReleaseFixture(t)
	fastGenerations := []uint64{0, 3, 5, 7, 9}
	oracleGenerations := slices.Clone(fastGenerations)
	fast.checkedHandles = &fastGenerations
	oracle.checkedHandles = &oracleGenerations
	var reclaimed []Ref
	remove := oracle.AddReclaimObserver(func(r Ref) { reclaimed = append(reclaimed, r) })
	defer remove()

	fast.sweepAll()
	oracle.sweepAll()

	if !slices.Equal(reclaimed, []Ref{makeObjRef(1), makeObjRef(2), makeObjRef(4)}) {
		t.Fatalf("generic callback order = %v", reclaimed)
	}
	if !slices.Equal(fast.handles, oracle.handles) ||
		!slices.Equal(fast.freeHandles, oracle.freeHandles) ||
		!slices.Equal(fast.nurseryHandles, oracle.nurseryHandles) ||
		fast.nurseryBump != oracle.nurseryBump ||
		fast.survivorBump != oracle.survivorBump ||
		fast.lastCardBounds != oracle.lastCardBounds {
		t.Fatalf("fast path differs:\nfast=%+v free=%v nursery=%v bump=%d/%d bounds=%+v\noracle=%+v free=%v nursery=%v bump=%d/%d bounds=%+v",
			fast.handles, fast.freeHandles, fast.nurseryHandles, fast.nurseryBump, fast.survivorBump, fast.lastCardBounds,
			oracle.handles, oracle.freeHandles, oracle.nurseryHandles, oracle.nurseryBump, oracle.survivorBump, oracle.lastCardBounds)
	}
	if !slices.Equal(fast.freeHandles, []uint32{1, 2, 4}) || !slices.Equal(fast.nurseryHandles, []uint32{3}) {
		t.Fatalf("identity orders changed: free=%v nursery=%v", fast.freeHandles, fast.nurseryHandles)
	}
	for i := range 3 {
		e := handleEntry{off: uint32(256 + 32*i), size: 32, space: spaceNursery}
		if got, want := fast.newHandle(e), oracle.newHandle(e); got != want {
			t.Fatalf("reuse %d identity = %d, want %d", i, got, want)
		}
	}
	if !slices.Equal(fast.handles, oracle.handles) || !slices.Equal(fastGenerations, oracleGenerations) ||
		!slices.Equal(fast.freeHandles, oracle.freeHandles) {
		t.Fatalf("native reuse differs: handles=%+v/%+v generations=%v/%v free=%v/%v",
			fast.handles, oracle.handles, fastGenerations, oracleGenerations, fast.freeHandles, oracle.freeHandles)
	}
}

func TestLinearNurseryReleaseObserverPanicPrecedesMutation(t *testing.T) {
	c := linearNurseryReleaseFixture(t)
	beforeHandles := slices.Clone(c.handles)
	beforeFree := slices.Clone(c.freeHandles)
	beforeNursery := slices.Clone(c.nurseryHandles)
	remove := c.AddReclaimObserver(func(Ref) { panic("reclaim sentinel") })
	defer remove()

	func() {
		defer func() {
			if got := recover(); got != "reclaim sentinel" {
				t.Fatalf("panic = %v", got)
			}
		}()
		c.sweepAll()
	}()

	if !slices.Equal(c.handles, beforeHandles) || !slices.Equal(c.freeHandles, beforeFree) ||
		!slices.Equal(c.nurseryHandles, beforeNursery) {
		t.Fatalf("observer panic mutated collection state")
	}
}

func TestLinearNurseryReleaseExceptionalMetadataFallsBack(t *testing.T) {
	t.Run("remembered card", func(t *testing.T) {
		c := linearNurseryReleaseFixture(t)
		c.handles[1].remembered = true
		c.handles[1].cardSlot = 1
		c.remembered = []uint32{1}
		c.objectCards = []objectCard{{handle: 1, index: 0, end: 7}}
		c.sweepAll()
		if c.handles[1] != (handleEntry{}) || c.objectCards[0] != (objectCard{}) ||
			c.lastCardBounds != (objectCardBounds{}) {
			t.Fatalf("generic metadata release incomplete: entry=%+v card=%+v bounds=%+v",
				c.handles[1], c.objectCards[0], c.lastCardBounds)
		}
	})

	t.Run("poison", func(t *testing.T) {
		c := linearNurseryReleaseFixture(t)
		c.cfg.PoisonFreed = true
		clear(c.nursery)
		c.sweepAll()
		for _, off := range []uint32{0, 32, 96} {
			for i, b := range c.nursery[off : off+32] {
				if b != 0xdd {
					t.Fatalf("dead extent %d byte %d = %#x", off, i, b)
				}
			}
		}
		for i, b := range c.nursery[64:96] {
			if b != 0 {
				t.Fatalf("live extent byte %d poisoned: %#x", i, b)
			}
		}
	})
}
