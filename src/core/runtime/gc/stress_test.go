package gc

import (
	"math/rand"
	"testing"
)

type stressObj struct {
	ref   Ref
	edges []int
}

func stressRootSlots(roots []Root) Slots {
	slots := make([]RootSlot, len(roots))
	for i := range roots {
		slots[i] = &roots[i]
	}
	return Slots(slots)
}

func stressReachable(objs []stressObj, roots []Root) map[int]bool {
	byRef := make(map[Ref]int, len(objs))
	for i, o := range objs {
		byRef[o.ref] = i
	}
	seen := make(map[int]bool)
	var visit func(int)
	visit = func(i int) {
		if i < 0 || i >= len(objs) || seen[i] {
			return
		}
		seen[i] = true
		for _, j := range objs[i].edges {
			visit(j)
		}
	}
	for _, r := range roots {
		if i, ok := byRef[Ref(r)]; ok {
			visit(i)
		}
	}
	return seen
}

func stressBuildGraph(t *testing.T, c *Collector, seed int64, n int) ([]stressObj, []Root) {
	t.Helper()
	rng := rand.New(rand.NewSource(seed))
	objs := make([]stressObj, 0, n)
	roots := make([]Root, 0, n/4)
	for i := 0; i < n; i++ {
		var r Ref
		var err error
		slots := stressRootSlots(roots)
		switch rng.Intn(4) {
		case 0:
			r, err = c.NewStructDefaultWithRoots(0, slots)
		case 1:
			r, err = c.NewStructDefaultWithRoots(1, slots)
		default:
			r, err = c.NewArrayDefaultWithRoots(3, uint32(rng.Intn(5)), slots)
		}
		if err != nil {
			t.Fatalf("alloc %d seed %d: %v", i, seed, err)
		}
		objs = append(objs, stressObj{ref: r})
		if rng.Intn(4) == 0 {
			roots = append(roots, Root(r))
		}
	}
	if len(roots) == 0 && len(objs) > 0 {
		roots = append(roots, Root(objs[0].ref))
	}
	for i := range objs {
		d, err := c.refDesc(objs[i].ref)
		if err != nil {
			t.Fatal(err)
		}
		switch d.Kind {
		case KindStruct:
			if !d.HasRefs {
				// Store ref-looking bits in pointer-free objects. Exact scanning must
				// ignore this even when it names another live handle.
				if len(objs) > 1 {
					_ = c.StructSet(objs[i].ref, 0, I32Value(int32(objs[rng.Intn(len(objs))].ref)))
				}
				continue
			}
			for f := range d.Fields {
				if rng.Intn(3) == 0 {
					_ = c.StructSet(objs[i].ref, uint32(f), RefValue(Null()))
					continue
				}
				j := rng.Intn(len(objs))
				if rng.Intn(12) == 0 {
					_ = c.StructSet(objs[i].ref, uint32(f), RefValue(I31New(int32(j))))
					continue
				}
				if err := c.StructSet(objs[i].ref, uint32(f), RefValue(objs[j].ref)); err != nil {
					t.Fatal(err)
				}
				objs[i].edges = append(objs[i].edges, j)
			}
		case KindArray:
			ln, _ := c.ArrayLen(objs[i].ref)
			for k := uint32(0); k < ln; k++ {
				if rng.Intn(3) == 0 {
					_ = c.ArraySet(objs[i].ref, k, RefValue(Null()))
					continue
				}
				j := rng.Intn(len(objs))
				if rng.Intn(12) == 0 {
					_ = c.ArraySet(objs[i].ref, k, RefValue(I31New(int32(j))))
					continue
				}
				if err := c.ArraySet(objs[i].ref, k, RefValue(objs[j].ref)); err != nil {
					t.Fatal(err)
				}
				objs[i].edges = append(objs[i].edges, j)
			}
		}
	}
	return objs, roots
}

func stressAssertReachability(t *testing.T, c *Collector, objs []stressObj, roots []Root) {
	t.Helper()
	want := stressReachable(objs, roots)
	if err := c.CollectFull(stressRootSlots(roots)); err != nil {
		t.Fatal(err)
	}
	for i, o := range objs {
		live := c.entry(o.ref).space != spaceFree
		if live != want[i] {
			t.Fatalf("object %d live=%v want %v ref=%v", i, live, want[i], o.ref)
		}
	}
	if err := c.Verify(stressRootSlots(roots)); err != nil {
		t.Fatal(err)
	}
}

func TestTinyExactGraphHammer(t *testing.T) {
	for seed := int64(1); seed <= 25; seed++ {
		c := newTinyTestCollector(t, Config{TinyHeapBytes: 16 << 10, TinyBlockBytes: 16, VerifyAfterCollect: true})
		objs, roots := stressBuildGraph(t, c, seed, 120)
		stressAssertReachability(t, c, objs, roots)
		c.Close()
	}
}

func TestThroughputExactGraphHammer(t *testing.T) {
	for seed := int64(101); seed <= 125; seed++ {
		c := newTestCollector(t, Config{NurseryBytes: 64 << 10, ThroughputHeapBytes: 1 << 20, ThroughputPageBytes: 4096, VerifyAfterCollect: true})
		objs, roots := stressBuildGraph(t, c, seed, 160)
		stressAssertReachability(t, c, objs, roots)
		c.Close()
	}
}

func TestTinyIncrementalMutationHammer(t *testing.T) {
	c := newTinyTestCollector(t, Config{TinyHeapBytes: 32 << 10, TinyBlockBytes: 16})
	rng := rand.New(rand.NewSource(0x51a7))
	roots := []Root{}
	for i := 0; i < 8; i++ {
		r, err := c.NewStructDefaultWithRoots(1, stressRootSlots(roots))
		if err != nil {
			t.Fatal(err)
		}
		roots = append(roots, Root(r))
	}
	if err := c.Step(stressRootSlots(roots)); err != nil {
		t.Fatal(err)
	}
	objects := make([]Ref, len(roots))
	for i := range roots {
		objects[i] = Ref(roots[i])
	}
	for step := 0; step < 400; step++ {
		slots := stressRootSlots(roots)
		switch rng.Intn(4) {
		case 0:
			parent := objects[rng.Intn(len(objects))]
			child := objects[rng.Intn(len(objects))]
			_ = c.StructSet(parent, uint32(rng.Intn(2)), RefValue(child))
		case 1:
			roots = append(roots, Root(objects[rng.Intn(len(objects))]))
		case 2:
			r, err := c.NewStructDefaultWithRoots(1, slots)
			if err == nil {
				objects = append(objects, r)
				roots = append(roots, Root(r))
			}
		case 3:
			arr, err := c.NewArrayWithRoots(3, uint32(1+rng.Intn(4)), RefValue(objects[rng.Intn(len(objects))]), slots)
			if err == nil {
				objects = append(objects, arr)
				roots = append(roots, Root(arr))
			}
		}
		if err := c.Step(stressRootSlots(roots)); err != nil {
			t.Fatal(err)
		}
		if step%17 == 0 {
			if err := c.Verify(stressRootSlots(roots)); err != nil {
				t.Fatal(err)
			}
		}
	}
	for c.tinyGC.state != tinyIdle {
		if err := c.Step(stressRootSlots(roots)); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Verify(stressRootSlots(roots)); err != nil {
		t.Fatal(err)
	}
}

func TestThroughputAllocatorReuseHammer(t *testing.T) {
	c := newTestCollector(t, Config{StressNurseryBytes: 1024, LargeObjectBytes: 256, ThroughputHeapBytes: 1 << 20, ThroughputPageBytes: 4096})
	roots := []Root{}
	var highWater uint32
	for cycle := 0; cycle < 60; cycle++ {
		for i := 0; i < 80; i++ {
			var r Ref
			var err error
			if i%7 == 0 {
				r, err = c.NewArrayWithRoots(2, uint32(80+i%31), I32Value(int32(i)), stressRootSlots(roots))
			} else {
				r, err = c.NewStructDefaultWithRoots(1, stressRootSlots(roots))
			}
			if err != nil {
				t.Fatal(err)
			}
			roots = append(roots, Root(r))
		}
		if err := c.CollectMinor(stressRootSlots(roots)); err != nil {
			t.Fatal(err)
		}
		if c.throughput.bump > highWater {
			highWater = c.throughput.bump
		}
		for i := range roots {
			if (i+cycle)%3 != 0 {
				roots[i] = Root(Null())
			}
		}
		if err := c.CollectFull(stressRootSlots(roots)); err != nil {
			t.Fatal(err)
		}
		if err := c.Verify(stressRootSlots(roots)); err != nil {
			t.Fatal(err)
		}
		compact := roots[:0]
		for _, r := range roots {
			if Ref(r).IsObj() && c.entry(Ref(r)).space != spaceFree {
				compact = append(compact, r)
			}
		}
		roots = compact
	}
	if c.throughput.bump > highWater+(256<<10) {
		t.Fatalf("throughput allocator grew unexpectedly despite reuse: bump=%d highWater=%d", c.throughput.bump, highWater)
	}
}
