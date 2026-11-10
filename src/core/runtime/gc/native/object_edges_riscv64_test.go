//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_tiny_nonincremental

package gc

import (
	"fmt"
	"testing"
)

type testObjectEdges map[Ref][]Ref

func (s testObjectEdges) ObjectEdgeCount(ref Ref) uint32         { return uint32(len(s[ref])) }
func (s testObjectEdges) ObjectEdgeAt(ref Ref, index uint32) Ref { return s[ref][index] }

type testConditionalObjectEdges struct {
	testObjectEdges
	conditional map[SlotKind]map[uint32]bool
}

type testObjectRefVisitor struct {
	refs  []Ref
	count uint32
}

func (v *testObjectRefVisitor) VisitObjectRef(ref Ref) {
	v.count++
	if v.refs != nil {
		v.refs = append(v.refs, ref)
	}
}

func TestObjectTraceBoundedReadOnlyAllocationFree(t *testing.T) {
	c := newTestCollectorWithTypes(t, Config{}, tinyScanTypes(t))
	parent, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	source := testConditionalObjectEdges{
		testObjectEdges: testObjectEdges{parent: {child, child, child}},
		conditional:     map[SlotKind]map[uint32]bool{},
	}
	c.SetObjectEdgeSource(source)
	global := c.NewGlobalSlot(child)
	c.NewGlobalSlot(parent)
	source.conditional[SlotGlobal] = map[uint32]bool{global: true}
	visitor := &testObjectRefVisitor{}
	var cursor ObjectTraceCursor
	for i := 0; i < 3; i++ {
		used, done, err := c.TraceObjectChildren(parent, &cursor, 1, visitor)
		if err != nil || used != 1 || done != (i == 2) {
			t.Fatalf("step%d: used=%d done=%t err=%v", i, used, done, err)
		}
	}
	if visitor.count != 3 {
		t.Fatalf("children=%d", visitor.count)
	}
	next, done, err := c.TracePersistentRoots(SlotGlobal, 0, 1, visitor)
	if err != nil || next != 1 || done || visitor.count != 3 {
		t.Fatalf("conditional slot step: next=%d done=%t count=%d err=%v", next, done, visitor.count, err)
	}
	next, done, err = c.TracePersistentRoots(SlotGlobal, next, 1, visitor)
	if err != nil || next != 2 || !done || visitor.count != 4 {
		t.Fatalf("strong slot step: next=%d done=%t count=%d err=%v", next, done, visitor.count, err)
	}
	marks := append([]bool(nil), c.mark...)
	allocs := testing.AllocsPerRun(1000, func() {
		cursor = ObjectTraceCursor{}
		if _, _, err := c.TraceObjectChildren(parent, &cursor, 1, visitor); err != nil {
			panic(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("bounded traversal allocations=%v", allocs)
	}
	for i := range marks {
		if c.mark[i] != marks[i] {
			t.Fatal("read-only trace changed collector marks")
		}
	}
}

func (s testConditionalObjectEdges) PersistentRootIsStrong(kind SlotKind, index uint32) bool {
	return !s.conditional[kind][index]
}

func TestObjectEdgesConditionalPersistentCycle(t *testing.T) {
	for _, profile := range []Profile{ProfileThroughput, ProfileTiny} {
		t.Run(fmt.Sprint(profile), func(t *testing.T) {
			c := newTestCollectorWithTypes(t, Config{Profile: profile, TinyHeapBytes: 1 << 20, TinyBlockBytes: 16, VerifyAfterCollect: true}, tinyScanTypes(t))
			env, err := c.NewStructDefault(0)
			if err != nil {
				t.Fatal(err)
			}
			exn, err := c.NewStructDefault(0)
			if err != nil {
				t.Fatal(err)
			}
			global := c.NewGlobalSlot(exn)
			table := c.NewTableSlot(exn)
			source := testConditionalObjectEdges{
				testObjectEdges: testObjectEdges{env: {exn}, exn: {env}},
				conditional:     map[SlotKind]map[uint32]bool{SlotGlobal: {global: true}, SlotTable: {table: true}},
			}
			c.SetObjectEdgeSource(source)
			root := Root(env)
			roots := &tinyDirectRoot{root: &root}
			if err := c.CollectFull(roots); err != nil {
				t.Fatal(err)
			}
			if _, err := c.ObjectType(exn); err != nil {
				t.Fatalf("reachable conditional child lost: %v", err)
			}
			root = Root(Null())
			if err := c.CollectFull(roots); err != nil {
				t.Fatal(err)
			}
			if _, err := c.ObjectType(env); err == nil {
				t.Fatal("closed Env cycle retained")
			}
			if _, err := c.ObjectType(exn); err == nil {
				t.Fatal("closed exception cycle retained")
			}
		})
	}
}

func TestReclaimObserverIndependentRemoval(t *testing.T) {
	c := newTestCollectorWithTypes(t, Config{}, tinyScanTypes(t))
	var primary, extra, removed int
	c.SetReclaimObserver(func(Ref) { primary++ })
	remove := c.AddReclaimObserver(func(Ref) { removed++ })
	c.AddReclaimObserver(func(Ref) { extra++ })
	remove()
	remove()
	if _, err := c.NewStructDefault(0); err != nil {
		t.Fatal(err)
	}
	if err := c.CollectFull(EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	if primary != 1 || extra != 1 || removed != 0 {
		t.Fatalf("observers primary=%d extra=%d removed=%d", primary, extra, removed)
	}
	c.SetReclaimObserver(nil)
	c.Close()
	if primary != 1 || extra != 2 || removed != 0 {
		t.Fatalf("close observers primary=%d extra=%d removed=%d", primary, extra, removed)
	}
}

func TestObjectEdgesRetainAndReclaim(t *testing.T) {
	for _, profile := range []Profile{ProfileThroughput, ProfileTiny} {
		t.Run(fmt.Sprint(profile), func(t *testing.T) {
			c := newTestCollectorWithTypes(t, Config{Profile: profile, NurseryBytes: 4096, ThroughputHeapBytes: 1 << 20, TinyHeapBytes: 1 << 20, TinyBlockBytes: 16, VerifyAfterCollect: true}, tinyScanTypes(t))
			parent, err := c.NewStructDefault(0)
			if err != nil {
				t.Fatal(err)
			}
			child, err := c.NewStructDefault(0)
			if err != nil {
				t.Fatal(err)
			}
			edges := testObjectEdges{parent: {child}}
			c.SetObjectEdgeSource(edges)
			root := Root(parent)
			roots := &tinyDirectRoot{root: &root}
			if err := c.CollectFull(roots); err != nil {
				t.Fatal(err)
			}
			if _, err := c.ObjectType(child); err != nil {
				t.Fatalf("child lost: %v", err)
			}
			edges[parent][0] = Null()
			if err := c.CollectFull(roots); err != nil {
				t.Fatal(err)
			}
			if _, err := c.ObjectType(child); err == nil {
				t.Fatal("removed child retained")
			}
			if err := c.CollectFull(EmptyRoots{}); err != nil {
				t.Fatal(err)
			}
			if _, err := c.ObjectType(parent); err == nil {
				t.Fatal("unreachable parent retained")
			}
		})
	}
}

func TestObjectEdgesTinyBoundedScan(t *testing.T) {
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 1 << 20, TinyBlockBytes: 16}, tinyScanTypes(t))
	parent, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	edges := testObjectEdges{parent: make([]Ref, 4096)}
	for i := range edges[parent] {
		edges[parent][i] = child
	}
	c.SetObjectEdgeSource(edges)
	root := Root(parent)
	roots := &tinyDirectRoot{root: &root}
	if err := c.Step(roots); err != nil {
		t.Fatal(err)
	}
	steps := drainTinyWithWorkChecks(t, c, roots)
	if steps < 2 {
		t.Fatalf("large virtual edge scan used %d steps", steps)
	}
	if _, err := c.ObjectType(child); err != nil {
		t.Fatal(err)
	}
}

func TestObjectEdgesOldToYoungBarrier(t *testing.T) {
	c := newTestCollectorWithTypes(t, Config{NurseryBytes: 4096, ThroughputHeapBytes: 1 << 20, SurvivorBytes: 4096, VerifyAfterCollect: true}, tinyScanTypes(t))
	parent, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ForcePromote(parent); err != nil {
		t.Fatal(err)
	}
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	edges := testObjectEdges{parent: {child}}
	c.SetObjectEdgeSource(edges)
	c.WriteBarrierObject(parent, child)
	root := Root(parent)
	roots := &tinyDirectRoot{root: &root}
	for i := 0; i < 3; i++ {
		if err := c.CollectMinor(roots); err != nil {
			t.Fatal(err)
		}
		if _, err := c.ObjectType(child); err != nil {
			t.Fatal(err)
		}
	}
}

func TestObjectEdgesMixedBoundedCursor(t *testing.T) {
	c := newTestCollectorWithTypes(t, Config{}, tinyScanTypes(t))
	physical, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	shadow, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := c.NewStructDefault(1)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.StructSet(parent, 0, RefValue(physical)); err != nil {
		t.Fatal(err)
	}
	c.SetObjectEdgeSource(testObjectEdges{parent: {shadow, shadow}})
	var cursor objectScanCursor
	var got []Ref
	for steps := 0; ; steps++ {
		work, done := c.scanObjectRefsRange(handleOf(parent), &cursor, objectScanBudget{ObjectRanges: 1, ScanEntries: 1, RefSlots: 1, PayloadBytes: 4}, objectScanVisitor{fn: func(ref Ref) { got = append(got, ref) }})
		if work.ScanEntries > 1 || work.RefSlots > 1 || work.PayloadBytes > 4 {
			t.Fatalf("work exceeded budget: %+v", work)
		}
		if done {
			break
		}
		if steps > 4 {
			t.Fatal("cursor did not finish")
		}
	}
	if len(got) != 3 || got[0] != physical || got[1] != shadow || got[2] != shadow {
		t.Fatalf("mixed trace=%v", got)
	}
}

func TestObjectEdgesTinyMutationBehindCursor(t *testing.T) {
	c := newTestCollectorWithTypes(t, Config{Profile: ProfileTiny, TinyHeapBytes: 1 << 20, TinyBlockBytes: 16}, tinyScanTypes(t))
	parent, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	edges := testObjectEdges{parent: make([]Ref, 8192)}
	c.SetObjectEdgeSource(edges)
	root := Root(parent)
	roots := &tinyDirectRoot{root: &root}
	for i := 0; ; i++ {
		if err := c.Step(roots); err != nil {
			t.Fatal(err)
		}
		if c.tinyGC.scan.handle == handleOf(parent) && c.tinyGC.scan.scan.index > 0 {
			break
		}
		if i > 100 {
			t.Fatal("no partial parent scan")
		}
	}
	c.WriteBarrierObject(parent, child)
	edges[parent][0] = child
	drainTinyWithWorkChecks(t, c, roots)
	if _, err := c.ObjectType(child); err != nil {
		t.Fatalf("new child behind cursor was lost: %v", err)
	}
}

func BenchmarkObjectEdgeScan(b *testing.B) {
	for _, extra := range []bool{false, true} {
		b.Run(fmt.Sprint(extra), func(b *testing.B) {
			desc, err := NewStructDesc(0, []StorageKind{StorageI64})
			if err != nil {
				b.Fatal(err)
			}
			c, err := NewCollector(Config{}, []TypeDesc{desc})
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			parent, err := c.NewStructDefault(0)
			if err != nil {
				b.Fatal(err)
			}
			if extra {
				c.SetObjectEdgeSource(testObjectEdges{parent: {Null(), Null(), Null(), Null()}})
			}
			visit := func(Ref) {}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c.scanObjectRefs(handleOf(parent), visit)
			}
		})
	}
}
