package gc

import "testing"

type panickingMarkRoots struct{ root Root }

func (r *panickingMarkRoots) RangeRoots(visit func(RootSlot) bool) {
	visit(&r.root)
	panic("root enumeration failed")
}

type directPanickingMarkRoots struct{ panickingMarkRoots }

func (r *directPanickingMarkRoots) RangeRootRefs(sink RootRefSink) bool {
	sink.VisitRootRef(r.root.GetRef())
	panic("root enumeration failed")
}

func TestFullMarkResetAfterRootPanicAndMinorCollection(t *testing.T) {
	for _, direct := range []bool{false, true} {
		name := "slots"
		if direct {
			name = "direct"
		}
		t.Run(name, func(t *testing.T) {
			c := newTestCollector(t, Config{NurseryBytes: 1 << 20})
			child, err := c.NewStructDefault(0)
			if err != nil {
				t.Fatal(err)
			}
			parent, err := c.NewStructDefault(1)
			if err != nil {
				t.Fatal(err)
			}
			if err := c.StructSet(parent, 0, RefValue(child)); err != nil {
				t.Fatal(err)
			}
			roots := RefSliceRoots{parent}
			if err := c.CollectMinor(roots); err != nil {
				t.Fatal(err)
			}
			if c.entry(parent).space != spaceOld || c.entry(child).space != spaceOld {
				t.Fatal("parent and child were not promoted")
			}

			var panickingRoots RootSet = &panickingMarkRoots{root: Root(parent)}
			if direct {
				panickingRoots = &directPanickingMarkRoots{panickingMarkRoots{root: Root(parent)}}
			}
			func() {
				defer func() {
					if got := recover(); got != "root enumeration failed" {
						t.Fatalf("root walk panic = %v", got)
					}
				}()
				if err := c.CollectFull(panickingRoots); err != nil {
					t.Fatal(err)
				}
			}()
			if !c.mark[handleOf(parent)] || c.mark[handleOf(child)] || len(c.markStack) != 1 || c.markHighWater != 0 {
				t.Fatal("panicking root walk did not leave the parent queued and child unmarked")
			}
			// This collection discards the queued parent without tracing it. Its
			// old mark must still be covered by the next full mark reset.
			if err := c.CollectMinor(nil); err != nil {
				t.Fatal(err)
			}
			if len(c.markStack) != 0 {
				t.Fatal("minor collection retained the queued parent")
			}
			if err := c.CollectFull(roots); err != nil {
				t.Fatal(err)
			}
			if !c.validObjectRef(parent) || !c.validObjectRef(child) {
				t.Fatal("full collection freed a reachable object after root panic and minor collection")
			}
			if err := c.Verify(roots); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMarkResetCoversQueuedAndDrainedRoots(t *testing.T) {
	for _, mode := range []string{"queued", "minor", "captured-minor", "full"} {
		t.Run(mode, func(t *testing.T) {
			c := newTestCollector(t, Config{NurseryBytes: 1 << 20})
			var ref Ref
			for range 65 {
				var err error
				ref, err = c.NewStructDefault(0)
				if err != nil {
					t.Fatal(err)
				}
			}
			c.clearNurseryMarks()
			if mode == "full" {
				c.markRef(ref)
				c.drainMarkStack()
			} else {
				c.markNurseryRef(ref)
				switch mode {
				case "minor":
					c.drainNurseryMarkStack()
				case "captured-minor":
					c.drainNurseryMarkStackForPromotion()
				}
			}
			c.clearMarks()
			for _, marked := range c.mark {
				if marked {
					t.Fatal("reset left a stale mark")
				}
			}
			if c.markHighWater != 0 || len(c.markStack) != 0 {
				t.Fatal("reset left tracing state")
			}
		})
	}
}

func TestFullMarkResetTracksReachableHighWater(t *testing.T) {
	d, err := NewStructDesc(0, []StorageKind{StorageRefNull})
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{NurseryBytes: 1 << 20, VerifyAfterCollect: true}, []TypeDesc{d})
	refs := make([]Ref, 1025)
	for i := range refs {
		refs[i], err = c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
	}
	low, high := refs[0], refs[1023]
	if err := c.StructSet(low, 0, RefValue(high)); err != nil {
		t.Fatal(err)
	}
	roots := RefSliceRoots{low}
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	if c.markHighWater != handleOf(high)+1 || c.Stats().LiveObjects != 2 {
		t.Fatalf("transitive full mark: high-water=%d live=%d", c.markHighWater, c.Stats().LiveObjects)
	}
	if err := c.StructSet(low, 0, RefValue(Null())); err != nil {
		t.Fatal(err)
	}
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	if c.validObjectRef(high) || c.markHighWater != handleOf(low)+1 || len(c.mark) < len(refs) {
		t.Fatalf("sparse full mark: high valid=%v high-water=%d marks=%d", c.validObjectRef(high), c.markHighWater, len(c.mark))
	}
	// Minor tracing clears its own marks even when it reuses a high identity.
	fresh, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.CollectMinor(RefSliceRoots{low, fresh}); err != nil {
		t.Fatal(err)
	}
	if c.mark[handleOf(fresh)] {
		t.Fatal("minor marking left a stale mark on a reused identity")
	}
	for range 2 {
		if err := c.CollectFull(nil); err != nil {
			t.Fatal(err)
		}
		if c.Stats().LiveObjects != 0 || c.markHighWater != 0 {
			t.Fatal("empty full collection retained old marks")
		}
	}
}

func TestEvacuatedCardListsReuseScalarTails(t *testing.T) {
	c, parent, _ := sparseCardBoundsFixture(t, 64)
	children := make([]Ref, 64)
	for i := 0; i <= 4096; i++ {
		c.NewGlobalSlot(Null())
		c.NewTableSlot(Null())
	}
	for cycle := range 3 {
		fillSparseWholeMinor(t, c, parent, children)
		for _, index := range []uint32{0, 63, 64, 4096} {
			if err := c.SetGlobalSlot(index, children[0]); err != nil {
				t.Fatal(err)
			}
			if err := c.SetTableSlot(index, children[1]); err != nil {
				t.Fatal(err)
			}
		}
		if err := c.CollectMinor(RefSliceRoots{parent}); err != nil {
			t.Fatal(err)
		}
		if c.CardCount() != 0 || c.RememberedCount() != 0 || c.entry(parent).cardSlot != 0 || c.freeObjectCardSlot != 0 {
			t.Fatalf("cycle %d left card metadata", cycle)
		}
		for _, bitmap := range [][]uint64{c.globalCardBits, c.tableCardBits} {
			for _, word := range bitmap {
				if word != 0 {
					t.Fatalf("cycle %d left persistent card bits", cycle)
				}
			}
		}
		for _, child := range children {
			if !c.validObjectRef(child) || c.entry(child).young() {
				t.Fatalf("cycle %d lost promoted child", cycle)
			}
		}
		if err := c.Verify(RefSliceRoots{parent}); err != nil {
			t.Fatal(err)
		}
	}
}
