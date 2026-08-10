package gc

import "testing"

type directTestRoots []Ref

func (r *directTestRoots) RangeRoots(fn func(RootSlot) bool) {
	for i := range *r {
		if !fn((*Root)(&(*r)[i])) {
			return
		}
	}
}

func (r *directTestRoots) RangeRootRefs(sink RootRefSink) bool {
	for i := range *r {
		if !sink.VisitRootRef((*r)[i]) {
			return false
		}
	}
	return true
}

type panickingDirectRoots struct{}

func (panickingDirectRoots) RangeRoots(func(RootSlot) bool) {}
func (panickingDirectRoots) RangeRootRefs(RootRefSink) bool { panic("root walk") }

func TestDirectRootMarkingIsAllocationFreeAndResetsAfterPanic(t *testing.T) {
	c, err := NewCollector(Config{Profile: ProfileThroughput, NurseryBytes: 4096, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	roots := &directTestRoots{0, I31New(7)}
	if allocs := testing.AllocsPerRun(1000, func() { c.markRoots(roots) }); allocs != 0 {
		t.Fatalf("direct root marking allocations = %v, want 0", allocs)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("panicking direct root walk did not panic")
			}
		}()
		c.markRoots(panickingDirectRoots{})
	}()
	if c.rootMarkMode != 0 {
		t.Fatalf("root mark mode after panic = %d, want idle", c.rootMarkMode)
	}
}

func TestCollectorNativeRootMapContract(t *testing.T) {
	maps := []NativeRootMap{{
		LocalFunction: 0,
		FrameBytes:    336,
		Slots:         []NativeRootSlot{{Offset: 248, Kind: NativeRootFuncRef}},
	}}
	if err := ValidateNativeRootMaps(maps, 1); err != nil {
		t.Fatalf("collector native root map: %v", err)
	}
	if maps[0].Slots[0].Kind == NativeRootGCRef {
		t.Fatal("funcref lifecycle root was misclassified as a collector gc.Ref")
	}
}
