package gc

import (
	"errors"
	"fmt"
	"slices"
	"testing"
)

type allocationScheduleRoots struct {
	c        *Collector
	roots    Slots
	events   []string
	last     string
	fail     string
	sentinel error
}

func (r *allocationScheduleRoots) RangeRoots(f func(RootSlot) bool) {
	key := fmt.Sprintf("%d/%d", r.c.stats.MinorCollections, r.c.stats.FullCollections)
	if key != r.last {
		r.events = append(r.events, key)
		r.last = key
	}
	if key == r.fail {
		panic(r.sentinel)
	}
	r.roots.RangeRoots(f)
}
func TestContiguousEdenFirstSchedule(t *testing.T) {
	for _, live := range []bool{false, true} {
		t.Run(fmt.Sprint(live), func(t *testing.T) {
			c := newTestCollector(t, Config{NurseryBytes: 128, NurseryRegionShift: 6, SurvivorBytes: 128})
			var root Root
			r := &allocationScheduleRoots{c: c, roots: Slots{&root}}
			for i := 0; i < 4; i++ {
				v, e := c.NewStructDefaultWithRoots(0, EmptyRoots{})
				if e != nil {
					t.Fatal(e)
				}
				if live {
					root = Root(v)
				}
			}
			if _, e := c.NewStructDefaultWithRoots(0, r); e != nil {
				t.Fatal(e)
			}
			if !slices.Equal(r.events, []string{"1/0"}) {
				t.Fatalf("order %v", r.events)
			}
		})
	}
}
func TestContiguousEdenPromotionFallbackOrder(t *testing.T) {
	for _, productive := range []bool{false, true} {
		t.Run(fmt.Sprint(productive), func(t *testing.T) {
			c := newTestCollector(t, Config{NurseryBytes: 8192, NurseryRegionShift: 6, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096})
			var held []Root
			var survivor Ref
			for {
				v, e := c.NewStructDefault(1)
				if e != nil {
					t.Fatal(e)
				}
				e = c.ForcePromote(v)
				if errors.Is(e, errThroughputHeapExhausted) {
					survivor = v
					break
				}
				if e != nil {
					t.Fatal(e)
				}
				if !productive {
					held = append(held, Root(v))
				}
			}
			held = append(held, Root(survivor))
			slots := stressRootSlots(held)
			r := &allocationScheduleRoots{c: c, roots: slots}
			c.tenuringThreshold = 1
			// Fill without adding survivors so the next checked allocation must collect.
			for c.nurseryBump+32 <= c.edenBytes() {
				if _, e := c.NewStructDefault(0); e != nil {
					t.Fatal(e)
				}
			}
			_, e := c.NewStructDefaultWithRoots(0, r)
			if productive && e != nil {
				t.Fatal(e)
			}
			if !productive && !errors.Is(e, errThroughputHeapExhausted) {
				t.Fatalf("unproductive error %v", e)
			}
			if !slices.Equal(r.events, []string{"1/0", "1/1", "2/1"}) {
				t.Fatalf("order %v", r.events)
			}
			if e := c.Verify(slots); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestContiguousEdenLargeFullFirst(t *testing.T) {
	c := newTestCollector(t, Config{NurseryBytes: 128, NurseryRegionShift: 6, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096, LargeObjectBytes: 64})
	for {
		_, e := c.NewArrayDefault(2, 128)
		if errors.Is(e, errThroughputHeapExhausted) || e != nil {
			break
		}
	}
	r := &allocationScheduleRoots{c: c}
	if _, e := c.NewArrayDefaultWithRoots(2, 128, r); e != nil {
		t.Fatal(e)
	}
	if !slices.Equal(r.events, []string{"0/1"}) {
		t.Fatalf("order %v", r.events)
	}
}
func TestContiguousEdenAboveRegionWidthClass(t *testing.T) {
	c := newTestCollector(t, Config{NurseryBytes: 1024, NurseryRegionShift: 6, LargeObjectBytes: 2048})
	v, e := c.NewArrayDefault(2, 32)
	if e != nil {
		t.Fatal(e)
	}
	if c.entry(v).space != spaceNursery {
		t.Fatal("above-region object not nursery")
	}
}
func TestContiguousEdenFirstRootPanicOrder(t *testing.T) {
	for _, major := range []bool{false, true} {
		t.Run(fmt.Sprint(major), func(t *testing.T) {
			c := newTestCollector(t, Config{NurseryBytes: 128, NurseryRegionShift: 6, SurvivorBytes: 128})
			live := regionStruct(t, c, 0, EmptyRoots{})
			r := &allocationScheduleRoots{c: c, roots: Slots{(*Root)(&live)}, sentinel: errors.New("root sentinel")}
			var got any
			if major {
				r.fail = "0/1"
				func() {
					defer func() { got = recover() }()
					_ = c.retryAllocationMinorAfterPromotionFailure(r, errThroughputHeapExhausted)
				}()
				if c.stats.MinorCollections != 0 || c.stats.FullCollections != 1 {
					t.Fatal("retried after failed major")
				}
			} else {
				for i := 0; i < 3; i++ {
					regionStruct(t, c, 0, EmptyRoots{})
				}
				r.fail = "1/0"
				func() { defer func() { got = recover() }(); _, _ = c.NewStructDefaultWithRoots(0, r) }()
				if c.stats.MinorCollections != 1 || c.stats.FullCollections != 0 {
					t.Fatal("full replaced root panic")
				}
			}
			if got != r.sentinel {
				t.Fatalf("first panic %v", got)
			}
		})
	}
}
