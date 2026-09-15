//go:build !wago_tiny_nonincremental

package gc

import (
	"bytes"
	"math"
	"testing"
)

type tinyDirectRoot struct{ root *Root }

func (r *tinyDirectRoot) RangeRoots(fn func(RootSlot) bool) {
	if r != nil && r.root != nil {
		fn(r.root)
	}
}

func (r *tinyDirectRoot) RangeRootRefs(sink RootRefSink) bool {
	return r == nil || r.root == nil || sink.VisitRootRef(Ref(*r.root))
}

func tinyScanTypes(t testing.TB) []TypeDesc {
	t.Helper()
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	back, err := NewStructDesc(1, []StorageKind{StorageRefNull})
	if err != nil {
		t.Fatal(err)
	}
	array, err := NewArrayDesc(2, StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	return []TypeDesc{leaf, back, array}
}

func assertTinyStepWorkBound(t testing.TB, work objectScanWork) {
	t.Helper()
	if work.ObjectRanges > tinyStepObjectRanges || work.ScanEntries > tinyStepScanEntries || work.RefSlots > tinyStepRefSlots || work.PayloadBytes > tinyStepPayloadBytes {
		t.Fatalf("Tiny Step work = %+v, bounds = objects:%d entries:%d refs:%d bytes:%d", work, tinyStepObjectRanges, tinyStepScanEntries, tinyStepRefSlots, tinyStepPayloadBytes)
	}
}

func drainTinyWithWorkChecks(t testing.TB, c *Collector, roots RootSet) int {
	t.Helper()
	steps := 0
	for c.tinyGC.state != tinyIdle {
		if err := c.Step(roots); err != nil {
			t.Fatal(err)
		}
		assertTinyStepWorkBound(t, c.tinyGC.lastStepWork.objectScanWork())
		steps++
		if steps > 1_000_000 {
			t.Fatal("Tiny collection did not converge")
		}
	}
	return steps
}

func TestTinyPartialMarkStepAllocationFree(t *testing.T) {
	refs, err := NewArrayDesc(0, StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 1 << 20, TinyBlockBytes: 16}, []TypeDesc{refs})
	array, err := c.NewArrayDefault(0, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	root := Root(array)
	roots := &tinyDirectRoot{root: &root}
	if err := c.Step(roots); err != nil {
		t.Fatal(err)
	}
	allocs := testing.AllocsPerRun(100, func() {
		if err := c.Step(roots); err != nil {
			panic(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("warmed partial mark Step allocations = %v, want 0", allocs)
	}
}

func TestTinyStepBoundsGiantReferenceArrays(t *testing.T) {
	const giant = uint32(4096)
	tests := []struct {
		name     string
		length   uint32
		populate func(*testing.T, *Collector, Ref) []Ref
		wantLive func([]Ref) uint32
		wantDead func([]Ref) []Ref
	}{
		{
			name: "all-null", length: giant,
			populate: func(*testing.T, *Collector, Ref) []Ref { return nil },
			wantLive: func([]Ref) uint32 { return 1 },
		},
		{
			name: "all-one-object", length: giant,
			populate: func(t *testing.T, c *Collector, array Ref) []Ref {
				child, err := c.NewStructDefault(0)
				if err != nil {
					t.Fatal(err)
				}
				for i := uint32(0); i < giant; i++ {
					if err := c.ArraySet(array, i, RefValue(child)); err != nil {
						t.Fatal(err)
					}
				}
				return []Ref{child}
			},
			wantLive: func([]Ref) uint32 { return 2 },
		},
		{
			name: "different-objects", length: 257,
			populate: func(t *testing.T, c *Collector, array Ref) []Ref {
				children := make([]Ref, 257)
				for i := range children {
					var err error
					children[i], err = c.NewStructDefault(0)
					if err != nil {
						t.Fatal(err)
					}
					if err := c.ArraySet(array, uint32(i), RefValue(children[i])); err != nil {
						t.Fatal(err)
					}
				}
				return children
			},
			wantLive: func(children []Ref) uint32 { return uint32(len(children) + 1) },
		},
		{
			name: "cycle", length: giant,
			populate: func(t *testing.T, c *Collector, array Ref) []Ref {
				child, err := c.NewStructDefault(1)
				if err != nil {
					t.Fatal(err)
				}
				if err := c.StructSet(child, 0, RefValue(array)); err != nil {
					t.Fatal(err)
				}
				for i := uint32(0); i < giant; i++ {
					if err := c.ArraySet(array, i, RefValue(child)); err != nil {
						t.Fatal(err)
					}
				}
				return []Ref{child}
			},
			wantLive: func([]Ref) uint32 { return 2 },
		},
		{
			name: "alternating-live-dead", length: 256,
			populate: func(t *testing.T, c *Collector, array Ref) []Ref {
				children := make([]Ref, 256)
				for i := range children {
					var err error
					children[i], err = c.NewStructDefault(0)
					if err != nil {
						t.Fatal(err)
					}
					if i%2 == 0 {
						if err := c.ArraySet(array, uint32(i), RefValue(children[i])); err != nil {
							t.Fatal(err)
						}
					}
				}
				return children
			},
			wantLive: func(children []Ref) uint32 { return uint32(len(children)/2 + 1) },
			wantDead: func(children []Ref) []Ref {
				dead := make([]Ref, 0, len(children)/2)
				for i := 1; i < len(children); i += 2 {
					dead = append(dead, children[i])
				}
				return dead
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 1 << 20, TinyBlockBytes: 16, VerifyAfterCollect: true}, tinyScanTypes(t))
			array, err := c.NewArrayDefault(2, test.length)
			if err != nil {
				t.Fatal(err)
			}
			children := test.populate(t, c, array)
			root := Root(array)
			roots := Slots{&root}
			if err := c.Step(roots); err != nil {
				t.Fatal(err)
			}
			if err := c.Step(roots); err != nil {
				t.Fatal(err)
			}
			assertTinyStepWorkBound(t, c.tinyGC.lastStepWork.objectScanWork())
			if test.length > tinyStepScanEntries {
				if c.tinyGC.scan.handle != handleOf(array) || c.tinyGC.scan.scan.index == 0 || c.tinyColorOf(handleOf(array)) != tinyGray {
					t.Fatalf("large array did not retain a gray partial cursor: cursor=%+v color=%v", c.tinyGC.scan, c.tinyColorOf(handleOf(array)))
				}
			}
			markSteps := 1
			for c.tinyColorOf(handleOf(array)) != tinyBlack {
				if err := c.Step(roots); err != nil {
					t.Fatal(err)
				}
				assertTinyStepWorkBound(t, c.tinyGC.lastStepWork.objectScanWork())
				markSteps++
			}
			if want := int((test.length + tinyStepScanEntries - 1) / tinyStepScanEntries); markSteps < want {
				t.Fatalf("array scan steps = %d, want at least %d", markSteps, want)
			}
			drainTinyWithWorkChecks(t, c, roots)
			if got, want := c.Stats().LiveObjects, test.wantLive(children); got != want {
				t.Fatalf("live objects = %d, want %d", got, want)
			}
			if test.wantDead != nil {
				for _, dead := range test.wantDead(children) {
					if c.validObjectRef(dead) {
						t.Fatalf("unreachable child %v survived", dead)
					}
				}
			}
		})
	}
}

func TestTinyStepBoundsSparseLargeStruct(t *testing.T) {
	const fields = 1025
	kinds := make([]StorageKind, fields)
	for i := range kinds {
		kinds[i] = StorageI32
	}
	for _, i := range []int{0, 63, 64, 65, fields - 1} {
		kinds[i] = StorageRefNull
	}
	large, err := NewStructDesc(1, kinds)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 1 << 20, TinyBlockBytes: 16}, []TypeDesc{leaf, large})
	parent, err := c.NewStructDefault(1)
	if err != nil {
		t.Fatal(err)
	}
	children := make([]Ref, 5)
	for i, field := range []uint32{0, 63, 64, 65, fields - 1} {
		children[i], err = c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.StructSet(parent, field, RefValue(children[i])); err != nil {
			t.Fatal(err)
		}
	}
	dead, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	root := Root(parent)
	roots := Slots{&root}
	if err := c.Step(roots); err != nil {
		t.Fatal(err)
	}
	var entries uint32
	var scanSteps int
	for c.tinyColorOf(handleOf(parent)) != tinyBlack {
		if err := c.Step(roots); err != nil {
			t.Fatal(err)
		}
		assertTinyStepWorkBound(t, c.tinyGC.lastStepWork.objectScanWork())
		entries += c.tinyGC.lastStepWork.objectScanWork().ScanEntries
		scanSteps++
	}
	if entries != fields {
		t.Fatalf("descriptor entries = %d, want %d", entries, fields)
	}
	if scanSteps < (fields+int(tinyStepScanEntries)-1)/int(tinyStepScanEntries) {
		t.Fatalf("struct scan steps = %d", scanSteps)
	}
	drainTinyWithWorkChecks(t, c, roots)
	for _, child := range children {
		if !c.validObjectRef(child) {
			t.Fatalf("reachable child %v was collected", child)
		}
	}
	if c.validObjectRef(dead) {
		t.Fatal("unreachable sparse-struct sibling survived")
	}
}

func TestTinyPayloadByteBudgetLimitsWideStruct(t *testing.T) {
	const fields = 257
	kinds := make([]StorageKind, fields)
	for i := range kinds {
		kinds[i] = StorageV128
	}
	kinds[fields-1] = StorageRefNull
	wide, err := NewStructDesc(1, kinds)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 1 << 20, TinyBlockBytes: 16}, []TypeDesc{leaf, wide})
	parent, err := c.NewStructDefault(1)
	if err != nil {
		t.Fatal(err)
	}
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.StructSet(parent, fields-1, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	root := Root(parent)
	roots := Slots{&root}
	if err := c.Step(roots); err != nil {
		t.Fatal(err)
	}
	if err := c.Step(roots); err != nil {
		t.Fatal(err)
	}
	work := c.tinyGC.lastStepWork.objectScanWork()
	if work.ScanEntries != tinyStepPayloadBytes/16 || work.PayloadBytes != tinyStepPayloadBytes || work.ScanEntries >= tinyStepScanEntries {
		t.Fatalf("wide struct work = %+v, want payload-byte-limited Step", work)
	}
	if c.tinyGC.scan.handle != handleOf(parent) || c.tinyColorOf(handleOf(parent)) != tinyGray {
		t.Fatal("wide struct did not retain a partial gray cursor")
	}
	drainTinyWithWorkChecks(t, c, roots)
	if !c.validObjectRef(child) {
		t.Fatal("wide struct lost final reference")
	}
}

func TestObjectScanCursorExactBoundaries(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	desc, err := NewStructDesc(1, []StorageKind{StorageI32, StorageRefNull, StorageI32, StorageRefNull})
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 1024, TinyBlockBytes: 16}, []TypeDesc{leaf, desc})
	parent, _ := c.NewStructDefault(1)
	first, _ := c.NewStructDefault(0)
	last, _ := c.NewStructDefault(0)
	if err := c.StructSet(parent, 1, RefValue(first)); err != nil {
		t.Fatal(err)
	}
	if err := c.StructSet(parent, 3, RefValue(last)); err != nil {
		t.Fatal(err)
	}

	var cursor objectScanCursor
	var visited []Ref
	visitor := objectScanVisitor{fn: func(r Ref) { visited = append(visited, r) }}
	work, complete := c.scanObjectRefsRange(handleOf(parent), &cursor, objectScanBudget{ObjectRanges: 1, ScanEntries: 1, PayloadBytes: 4}, visitor)
	if complete || cursor.index != 1 || work.ScanEntries != 1 || len(visited) != 0 {
		t.Fatalf("pause before ref: cursor=%d work=%+v complete=%v visited=%v", cursor.index, work, complete, visited)
	}
	work, complete = c.scanObjectRefsRange(handleOf(parent), &cursor, objectScanBudget{ObjectRanges: 1, ScanEntries: 1, RefSlots: 1, PayloadBytes: 4}, visitor)
	if complete || cursor.index != 2 || work.RefSlots != 1 || len(visited) != 1 || visited[0] != first {
		t.Fatalf("pause after ref: cursor=%d work=%+v complete=%v visited=%v", cursor.index, work, complete, visited)
	}
	work, complete = c.scanObjectRefsRange(handleOf(parent), &cursor, objectScanBudget{ObjectRanges: 1, ScanEntries: 2, RefSlots: 1, PayloadBytes: 8}, visitor)
	if !complete || cursor.index != 4 || work.ScanEntries != 2 || len(visited) != 2 || visited[1] != last {
		t.Fatalf("final field: cursor=%d work=%+v complete=%v visited=%v", cursor.index, work, complete, visited)
	}
}

func TestTinyVerifyRejectsPartialScanInvariantCorruption(t *testing.T) {
	newPartial := func(t *testing.T) (*Collector, Ref, RootSet) {
		t.Helper()
		refs, err := NewArrayDesc(0, StorageRefNull)
		if err != nil {
			t.Fatal(err)
		}
		c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{refs})
		array, err := c.NewArrayDefault(0, 512)
		if err != nil {
			t.Fatal(err)
		}
		root := Root(array)
		roots := Slots{&root}
		if err := c.Step(roots); err != nil {
			t.Fatal(err)
		}
		if err := c.Step(roots); err != nil {
			t.Fatal(err)
		}
		if c.tinyGC.scan.handle != handleOf(array) {
			t.Fatal("test did not establish a partial scan")
		}
		return c, array, roots
	}

	t.Run("active also queued", func(t *testing.T) {
		c, array, roots := newPartial(t)
		c.tinyGC.grayStack = append(c.tinyGC.grayStack, handleOf(array))
		if err := c.Verify(roots); err == nil {
			t.Fatal("Verify accepted an active scan duplicated on the gray stack")
		}
	})
	t.Run("cursor at end", func(t *testing.T) {
		c, array, roots := newPartial(t)
		c.tinyGC.scan.scan.index = c.header(array).Aux
		if err := c.Verify(roots); err == nil {
			t.Fatal("Verify accepted a completed active cursor")
		}
	})
	t.Run("active during sweep", func(t *testing.T) {
		c, _, roots := newPartial(t)
		c.tinyGC.state = tinySweep
		if err := c.Verify(roots); err == nil {
			t.Fatal("Verify accepted an active cursor during sweep")
		}
	})
	t.Run("recorded Step work exceeds bound", func(t *testing.T) {
		refs, err := NewArrayDesc(0, StorageRefNull)
		if err != nil {
			t.Fatal(err)
		}
		c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{refs})
		c.tinyGC.lastStepWork.scanEntries = uint16(tinyStepScanEntries + 1)
		if err := c.Verify(nil); err == nil {
			t.Fatal("Verify accepted recorded Step work beyond the bound")
		}
	})
	t.Run("gray object missing from work state", func(t *testing.T) {
		refs, err := NewArrayDesc(0, StorageRefNull)
		if err != nil {
			t.Fatal(err)
		}
		c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{refs})
		array, err := c.NewArrayDefault(0, 1)
		if err != nil {
			t.Fatal(err)
		}
		root := Root(array)
		roots := Slots{&root}
		if err := c.Step(roots); err != nil {
			t.Fatal(err)
		}
		c.tinyGC.grayStack = c.tinyGC.grayStack[:0]
		if err := c.Verify(roots); err == nil {
			t.Fatal("Verify accepted a gray object missing from queue/cursor state")
		}
	})
}

func TestTinyMutationDuringPartialScan(t *testing.T) {
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 1 << 20, TinyBlockBytes: 16, VerifyAfterCollect: true}, tinyScanTypes(t))
	parent, _ := c.NewArrayDefault(2, 512)
	before, _ := c.NewStructDefault(0)
	after, _ := c.NewStructDefault(0)
	blackChild, _ := c.NewStructDefault(0)
	rootChild, _ := c.NewStructDefault(0)
	back, _ := c.NewStructDefault(1)
	root := Root(parent)
	roots := Slots{&root}
	if err := c.Step(roots); err != nil {
		t.Fatal(err)
	}
	if err := c.Step(roots); err != nil {
		t.Fatal(err)
	}
	if c.tinyGC.scan.handle != handleOf(parent) || c.tinyGC.scan.scan.index != tinyStepScanEntries {
		t.Fatalf("partial cursor = %+v", c.tinyGC.scan)
	}
	stackBefore := len(c.tinyGC.grayStack)
	for i := 0; i < 8; i++ {
		c.tinyGrayHandle(handleOf(parent))
	}
	if len(c.tinyGC.grayStack) != stackBefore {
		t.Fatal("duplicate gray request queued active parent")
	}
	if err := c.ArraySet(parent, 0, RefValue(before)); err != nil {
		t.Fatal(err)
	}
	if err := c.ArraySet(parent, 400, RefValue(after)); err != nil {
		t.Fatal(err)
	}
	if err := c.ArraySet(parent, 350, RefValue(back)); err != nil {
		t.Fatal(err)
	}
	if err := c.StructSet(back, 0, RefValue(parent)); err != nil {
		t.Fatal(err)
	}
	for _, child := range []Ref{before, after, back} {
		if c.tinyColorOf(handleOf(child)) != tinyGray {
			t.Fatalf("child %v was not shaded from partial gray parent", child)
		}
	}
	for c.tinyColorOf(handleOf(parent)) != tinyBlack {
		if err := c.Step(roots); err != nil {
			t.Fatal(err)
		}
		assertTinyStepWorkBound(t, c.tinyGC.lastStepWork.objectScanWork())
	}
	if err := c.ArraySet(parent, 1, RefValue(blackChild)); err != nil {
		t.Fatal(err)
	}
	if c.tinyColorOf(handleOf(blackChild)) != tinyGray || c.tinyColorOf(handleOf(parent)) != tinyGray {
		t.Fatal("black-parent barrier did not preserve existing hybrid shading")
	}
	root = Root(rootChild) // ordinary transient roots are observed by remark.
	drainTinyWithWorkChecks(t, c, roots)
	// parent was already gray when the transient root changed, so this cycle must
	// finish its exact graph as well as retain the newly remarked root.
	for _, live := range []Ref{parent, before, after, back, blackChild, rootChild} {
		if !c.validObjectRef(live) {
			t.Fatalf("reachable object %v was collected", live)
		}
	}
}

func TestTinyBulkBarrierRangeValidationEveryPhase(t *testing.T) {
	phases := []struct {
		name    string
		advance func(*testing.T, *Collector, RootSet, Ref)
	}{
		{name: "mark", advance: func(t *testing.T, c *Collector, roots RootSet, parent Ref) {
			if err := c.Step(roots); err != nil {
				t.Fatal(err)
			}
			for c.tinyColorOf(handleOf(parent)) != tinyBlack {
				if err := c.Step(roots); err != nil {
					t.Fatal(err)
				}
			}
			if c.tinyGC.state != tinyMark {
				t.Fatalf("state=%v, want mark", c.tinyGC.state)
			}
		}},
		{name: "remark", advance: func(t *testing.T, c *Collector, roots RootSet, parent Ref) {
			if err := c.Step(roots); err != nil {
				t.Fatal(err)
			}
			for c.tinyColorOf(handleOf(parent)) != tinyBlack {
				if err := c.Step(roots); err != nil {
					t.Fatal(err)
				}
			}
			if err := c.Step(roots); err != nil {
				t.Fatal(err)
			}
			if c.tinyGC.state != tinyRemark {
				t.Fatalf("state=%v, want remark", c.tinyGC.state)
			}
		}},
		{name: "sweep", advance: func(t *testing.T, c *Collector, roots RootSet, parent Ref) {
			if err := c.Step(roots); err != nil {
				t.Fatal(err)
			}
			for c.tinyColorOf(handleOf(parent)) != tinyBlack {
				if err := c.Step(roots); err != nil {
					t.Fatal(err)
				}
			}
			for c.tinyGC.state != tinySweep {
				if err := c.Step(roots); err != nil {
					t.Fatal(err)
				}
			}
		}},
	}
	invalid := []struct {
		name          string
		start, length uint32
	}{
		{name: "max-start", start: math.MaxUint32, length: 1},
		{name: "wrapping-start-length", start: math.MaxUint32, length: 2},
		{name: "invalid-start", start: 4, length: 1},
		{name: "invalid-length", start: 3, length: 2},
		{name: "zero-length", start: math.MaxUint32, length: 0},
	}
	for _, phase := range phases {
		t.Run(phase.name+"/exact-last", func(t *testing.T) {
			c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, tinyScanTypes(t))
			parent, _ := c.NewArrayDefault(2, 4)
			child, _ := c.NewStructDefault(0)
			root := Root(parent)
			roots := Slots{&root}
			phase.advance(t, c, roots, parent)
			d, _ := c.refDesc(parent)
			if err := c.storeValue(parent, d, uint64(PayloadOffset)+3*uint64(d.ElemSize), d.Elem, RefValue(child)); err != nil {
				t.Fatal(err)
			}
			c.PostBulkWriteBarrier(parent, 3, 1)
			if c.tinyColorOf(handleOf(child)) == tinyWhite {
				t.Fatal("exact last valid element was not shaded")
			}
		})
		for _, test := range invalid {
			t.Run(phase.name+"/"+test.name, func(t *testing.T) {
				c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, tinyScanTypes(t))
				parent, _ := c.NewArrayDefault(2, 4)
				child, _ := c.NewStructDefault(0)
				root := Root(parent)
				roots := Slots{&root}
				phase.advance(t, c, roots, parent)
				d, _ := c.refDesc(parent)
				if err := c.storeValue(parent, d, uint64(PayloadOffset), d.Elem, RefValue(child)); err != nil {
					t.Fatal(err)
				}
				c.PostBulkWriteBarrier(parent, test.start, test.length)
				if c.tinyColorOf(handleOf(child)) != tinyWhite {
					t.Fatalf("invalid range shaded child: color=%v", c.tinyColorOf(handleOf(child)))
				}
			})
		}
	}
}

func TestTinyRemarkBulkBarrierResumesActivePartialScan(t *testing.T) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	large, err := NewArrayDesc(1, StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	holder, err := NewArrayDesc(2, StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 1 << 20, TinyBlockBytes: 16, VerifyAfterCollect: true}, []TypeDesc{leaf, large, holder})
	parent, err := c.NewArrayDefault(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	child, err := c.NewArrayDefault(1, 1024)
	if err != nil {
		t.Fatal(err)
	}
	root := Root(parent)
	roots := Slots{&root}
	if err := c.Step(roots); err != nil {
		t.Fatal(err)
	}
	for c.tinyColorOf(handleOf(parent)) != tinyBlack {
		if err := c.Step(roots); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Step(roots); err != nil {
		t.Fatal(err)
	}
	if c.tinyGC.state != tinyRemark {
		t.Fatalf("state=%v, want remark", c.tinyGC.state)
	}
	d, err := c.refDesc(parent)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.storeValue(parent, d, uint64(PayloadOffset), d.Elem, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	c.PostBulkWriteBarrier(parent, 0, 1)
	if c.tinyGC.scan.handle != handleOf(child) || len(c.tinyGC.grayStack) != 0 || c.tinyColorOf(handleOf(child)) != tinyGray {
		t.Fatalf("remark barrier partial state: scan=%+v stack=%v child=%v", c.tinyGC.scan, c.tinyGC.grayStack, c.tinyColorOf(handleOf(child)))
	}
	if err := c.Verify(roots); err != nil {
		t.Fatalf("legitimate remark cursor failed verification: %v", err)
	}
	if err := c.Step(roots); err != nil {
		t.Fatal(err)
	}
	if c.tinyGC.state != tinyMark || c.tinyGC.scan.handle != handleOf(child) {
		t.Fatalf("remark did not return active cursor to mark: state=%v scan=%+v", c.tinyGC.state, c.tinyGC.scan)
	}
	drainTinyWithWorkChecks(t, c, roots)
	if !c.validObjectRef(parent) || !c.validObjectRef(child) {
		t.Fatal("remark bulk barrier lost partially scanned graph")
	}
}

func TestTinyRootStoresDuringPartialMarkRemarkAndSweep(t *testing.T) {
	for _, targetState := range []tinyGCState{tinyMark, tinyRemark, tinySweep} {
		t.Run(targetStateName(targetState), func(t *testing.T) {
			c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 1 << 20, TinyBlockBytes: 16}, tinyScanTypes(t))
			parent, _ := c.NewArrayDefault(2, 512)
			child, _ := c.NewStructDefault(0)
			root := Root(parent)
			roots := Slots{&root}
			g := c.NewGlobalSlot(Null())
			if err := c.Step(roots); err != nil {
				t.Fatal(err)
			}
			for {
				if targetState == tinyMark && c.tinyGC.scan.handle == handleOf(parent) {
					break
				}
				if targetState == tinyRemark && c.tinyGC.state == tinyRemark {
					break
				}
				if targetState == tinySweep && c.tinyGC.state == tinySweep {
					break
				}
				if err := c.Step(roots); err != nil {
					t.Fatal(err)
				}
			}
			if err := c.SetGlobalSlot(g, child); err != nil {
				t.Fatal(err)
			}
			root = Root(Null())
			drainTinyWithWorkChecks(t, c, roots)
			if !c.validObjectRef(child) {
				t.Fatal("root store did not preserve child")
			}
		})
	}
}

func targetStateName(state tinyGCState) string {
	switch state {
	case tinyMark:
		return "mark"
	case tinyRemark:
		return "remark"
	case tinySweep:
		return "sweep"
	default:
		return "unknown"
	}
}

type tinyLiveSnapshot struct {
	live bool
	data []byte
}

func snapshotTinyLive(c *Collector) []tinyLiveSnapshot {
	out := make([]tinyLiveSnapshot, len(c.handles))
	for h := uint32(1); int(h) < len(c.handles); h++ {
		if c.handles[h].space != spaceTiny {
			continue
		}
		out[h] = tinyLiveSnapshot{live: true, data: append([]byte(nil), c.bytes(makeObjRef(h))...)}
	}
	return out
}

func TestTinyBoundedStepsMatchCollectFull(t *testing.T) {
	build := func(t *testing.T) (*Collector, Root) {
		c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 1 << 20, TinyBlockBytes: 16}, tinyScanTypes(t))
		array, _ := c.NewArrayDefault(2, 512)
		for i := uint32(0); i < 128; i++ {
			child, err := c.NewStructDefault(1)
			if err != nil {
				t.Fatal(err)
			}
			if err := c.StructSet(child, 0, RefValue(array)); err != nil {
				t.Fatal(err)
			}
			if err := c.ArraySet(array, i*4, RefValue(child)); err != nil {
				t.Fatal(err)
			}
		}
		for i := 0; i < 64; i++ {
			if _, err := c.NewStructDefault(0); err != nil {
				t.Fatal(err)
			}
		}
		return c, Root(array)
	}
	bounded, boundedRoot := build(t)
	full, fullRoot := build(t)
	want, err := shadowTraceLive(bounded, Slots{&boundedRoot})
	if err != nil {
		t.Fatal(err)
	}
	if err := bounded.Step(Slots{&boundedRoot}); err != nil {
		t.Fatal(err)
	}
	drainTinyWithWorkChecks(t, bounded, Slots{&boundedRoot})
	for h := uint32(1); int(h) < len(bounded.handles); h++ {
		if got := bounded.handles[h].space != spaceFree; got != want[h] {
			t.Fatalf("bounded handle %d live=%v, shadow wants %v", h, got, want[h])
		}
	}
	if err := full.CollectFull(Slots{&fullRoot}); err != nil {
		t.Fatal(err)
	}
	left, right := snapshotTinyLive(bounded), snapshotTinyLive(full)
	if len(left) != len(right) {
		t.Fatalf("handle lengths = %d/%d", len(left), len(right))
	}
	for i := range left {
		if left[i].live != right[i].live || !bytes.Equal(left[i].data, right[i].data) {
			t.Fatalf("handle %d differs: bounded=%+v full=%+v", i, left[i], right[i])
		}
	}
	if bounded.Stats().LiveObjects != full.Stats().LiveObjects {
		t.Fatalf("live counts differ: %d/%d", bounded.Stats().LiveObjects, full.Stats().LiveObjects)
	}
}
