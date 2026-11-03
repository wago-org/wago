package gc

import (
	"testing"
)

func regionCollector(t *testing.T) *Collector {
	t.Helper()
	c, err := NewCollector(Config{NurseryBytes: 128, NurseryRegionShift: 6, SurvivorBytes: 128, VerifyAfterCollect: true}, testTypes(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}
func regionStruct(t *testing.T, c *Collector, typ TypeID, roots RootSet) Ref {
	t.Helper()
	r, err := c.NewStructDefaultWithRoots(typ, roots)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestContiguousEdenMinorFirstReusePreservesLiveIdentity(t *testing.T) {
	c := regionCollector(t)
	// Fill the 128-byte Eden and retain only the final object.
	for i := 0; i < 3; i++ {
		regionStruct(t, c, 0, EmptyRoots{})
	}
	live := regionStruct(t, c, 0, EmptyRoots{})
	root := live
	roots := Slots{(*Root)(&root)}
	if err := c.StructSet(live, 1, I64Value(0x123456789)); err != nil {
		t.Fatal(err)
	}
	for cycle := 0; cycle < 20000; cycle++ {
		regionStruct(t, c, 0, roots)
		regionStruct(t, c, 0, roots)
		if root != live {
			t.Fatal("minor collection changed stable identity")
		}
		value, err := c.StructGet(live, 1)
		if err != nil || value.I64() != 0x123456789 {
			t.Fatalf("payload=%v err=%v", value, err)
		}
	}
	if c.Stats().FullCollections != 0 || c.Stats().MinorCollections == 0 || c.Stats().YoungBytesCopied == 0 {
		t.Fatalf("not minor-first region reuse: %+v", c.Stats())
	}
	if c.NativeView().NurseryAllocBytes != c.edenBytes() {
		t.Fatal("contiguous native Eden admission hidden")
	}
	if len(c.handles) > nativeArrayHandleBatch+6 {
		t.Fatalf("contiguous reuse grew handle metadata: handles=%d", len(c.handles))
	}
	if err := c.Verify(roots); err != nil {
		t.Fatal(err)
	}
}

func TestContiguousEdenEdgeAndEvacuation(t *testing.T) {
	c := regionCollector(t)
	parent := regionStruct(t, c, 1, EmptyRoots{})
	regionStruct(t, c, 0, Slots{(*Root)(&parent)})
	child := regionStruct(t, c, 0, Slots{(*Root)(&parent)})
	if err := c.StructSet(parent, 0, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	regionStruct(t, c, 0, Slots{(*Root)(&parent)})
	roots := Slots{(*Root)(&parent)}
	// The live graph exhausts Eden; minor evacuation must preserve the edge.
	regionStruct(t, c, 0, roots)
	if c.Stats().MinorCollections != 1 || c.Stats().FullCollections != 0 {
		t.Fatalf("ordinary exhaustion was not minor-only: %+v", c.Stats())
	}
	value, err := c.StructGet(parent, 0)
	if err != nil || value.Ref != child {
		t.Fatalf("lost child edge: %v %v", value, err)
	}
	if err := c.Verify(roots); err != nil {
		t.Fatal(err)
	}
	// Promote the parent, store a new Eden object, and exercise old-to-young cards.
	if err := c.ForcePromote(parent); err != nil {
		t.Fatal(err)
	}
	next := regionStruct(t, c, 0, roots)
	if err := c.StructSet(parent, 1, RefValue(next)); err != nil {
		t.Fatal(err)
	}
	if err := c.CollectMinor(roots); err != nil {
		t.Fatal(err)
	}
	if _, err := c.StructGet(next, 0); err != nil {
		t.Fatal("lost remembered edge", err)
	}
	if err := c.CollectFull(EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	if c.Stats().LiveObjects != 0 {
		t.Fatal("dead graph retained")
	}
}

func TestContiguousEdenExhaustionIsMinorFirstAcrossChangingRootsAndSizes(t *testing.T) {
	c, err := NewCollector(Config{NurseryBytes: 256, NurseryRegionShift: 6, SurvivorBytes: 256, VerifyAfterCollect: true}, testTypes(t))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	var root Root
	roots := Slots{&root}
	allocateChain := func(count int) {
		t.Helper()
		for i := 0; i < count; i++ {
			r := regionStruct(t, c, 1, roots)
			if err := c.StructSet(r, 0, RefValue(Ref(root))); err != nil {
				t.Fatal(err)
			}
			root = Root(r)
		}
	}

	// Retained-live exhaustion starts with a minor and does not speculatively
	// trace the mature heap.
	for c.Stats().MinorCollections == 0 {
		allocateChain(1)
	}
	first := c.Stats()
	if first.FullCollections != 0 || first.MinorCollections != 1 {
		t.Fatalf("first exhaustion was not minor-only: %+v", first)
	}
	// The same request has the same schedule without failed-fit feedback.
	minorBefore := c.Stats().MinorCollections
	for c.Stats().MinorCollections == minorBefore {
		allocateChain(1)
	}
	steady := c.Stats()
	if steady.FullCollections != first.FullCollections || steady.MinorCollections != first.MinorCollections+1 {
		t.Fatalf("equivalent exhaustion schedule: before=%+v after=%+v", first, steady)
	}

	// An explicit full does not change the next exhaustion's minor-first order.
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	before := c.Stats()
	minorBefore = c.Stats().MinorCollections
	for c.Stats().MinorCollections == minorBefore {
		allocateChain(1)
	}
	after := c.Stats()
	if after.FullCollections != before.FullCollections || after.MinorCollections != before.MinorCollections+1 {
		t.Fatalf("explicit full changed exhaustion schedule: before=%+v after=%+v", before, after)
	}

	// A different fitting allocation shape also starts with a minor.
	before = c.Stats()
	for i := 0; i < 8 && c.Stats().MinorCollections == before.MinorCollections; i++ {
		if _, err := c.NewArrayDefaultWithRoots(3, 4, roots); err != nil {
			t.Fatal(err)
		}
	}
	after = c.Stats()
	if after.FullCollections != before.FullCollections || after.MinorCollections != before.MinorCollections+1 {
		t.Fatalf("different size did not use minor-only schedule: before=%+v after=%+v", before, after)
	}

	// Change the exact roots behind the same root-set object. Low survival must
	// immediately reclaim Eden with a minor; no durable negative-fit decision can
	// suppress collection or insert a full.
	root = Root(Null())
	before = c.Stats()
	for i := 0; i < 16 && c.Stats().MinorCollections == before.MinorCollections; i++ {
		regionStruct(t, c, 1, roots)
	}
	after = c.Stats()
	if after.FullCollections != before.FullCollections || after.MinorCollections != before.MinorCollections+1 {
		t.Fatalf("changed roots did not permit minor reclamation: before=%+v after=%+v", before, after)
	}
	if err := c.Verify(roots); err != nil {
		t.Fatal(err)
	}
}

func TestContiguousEdenLargeAndNoRoots(t *testing.T) {
	c := regionCollector(t)
	a, err := c.NewArrayDefault(2, 32)
	if err != nil {
		t.Fatal(err)
	}
	if c.entry(a).space != spaceLarge {
		t.Fatal("oversized object did not use large space")
	}
	for i := 0; i < 4; i++ {
		regionStruct(t, c, 0, nil)
	}
	before := c.Stats()
	if _, err := c.NewStructDefault(0); err == nil {
		t.Fatal("exhaustion collected without roots")
	}
	if c.Stats() != before {
		t.Fatal("failed admission changed counters")
	}
	if err := c.CollectFull(Slots{(*Root)(&a)}); err != nil {
		t.Fatal(err)
	}
	if n, err := c.ArrayLen(a); err != nil || n != 32 {
		t.Fatalf("large root=%d %v", n, err)
	}
}

func TestMatureRegionConfigAdmission(t *testing.T) {
	for _, cfg := range []Config{
		{NurseryRegionShift: 5, NurseryBytes: 128}, {NurseryRegionShift: 31, NurseryBytes: 128},
		{NurseryRegionShift: 6}, {NurseryRegionShift: 6, NurseryBytes: 127},
		{NurseryRegionShift: 6, NurseryBytes: 128, Profile: ProfileTiny},
		{NurseryRegionShift: 6, NurseryBytes: 128, DisableCollection: true},
		{NurseryRegionShift: 6, NurseryBytes: 128, MinorPauseTargetMicros: 1},
		{NurseryRegionShift: 6, NurseryBytes: 128, StressNurseryBytes: 128},
		{NurseryRegionShift: 6, NurseryBytes: 64 * 65537},
	} {
		if err := ValidateConfig(cfg); err == nil {
			t.Fatalf("admitted %+v", cfg)
		}
	}
	c, err := NewCollector(Config{}, testTypes(t))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
}

func TestContiguousEdenCollectionPolicies(t *testing.T) {
	for _, policy := range []struct {
		name      string
		configure func(*Config)
	}{
		{"poison", func(c *Config) { c.PoisonFreed = true }},
		{"immediate-promotion", func(c *Config) { c.DisableMovingNursery = true; c.SurvivorBytes = 0 }},
		{"every-allocation", func(c *Config) { c.CollectEveryAlloc = true }},
		{"major-after-minor", func(c *Config) { c.ForceMajorEveryMinor = true }},
	} {
		t.Run(policy.name, func(t *testing.T) {
			cfg := Config{NurseryBytes: 256, NurseryRegionShift: 6, SurvivorBytes: 128, VerifyAfterCollect: true}
			policy.configure(&cfg)
			c, err := NewCollector(cfg, testTypes(t))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			parent := regionStruct(t, c, 1, EmptyRoots{})
			roots := Slots{(*Root)(&parent)}
			for i := int64(0); i < 64; i++ {
				child, err := c.NewStructWithRoots(0, []Value{I32Value(int32(i)), I64Value(i * 12345)}, roots)
				if err != nil {
					t.Fatal(err)
				}
				if err := c.StructSet(parent, 0, RefValue(child)); err != nil {
					t.Fatal(err)
				}
				if i%3 == 0 {
					if err := c.CollectFull(roots); err != nil {
						t.Fatal(err)
					}
				}
				if i%7 == 0 {
					if err := c.CollectMinor(roots); err != nil {
						t.Fatal(err)
					}
				}
				value, err := c.StructGet(child, 1)
				if err != nil || value.I64() != i*12345 {
					t.Fatalf("iteration %d: %v %v", i, value, err)
				}
			}
			if err := c.Verify(roots); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestContiguousEdenArrayCycleAndInitializerOracle(t *testing.T) {
	// Same observable graph contract with and without mature descriptors.
	for _, shift := range []uint8{0, 6} {
		for _, length := range []uint32{2, 5, 32} {
			cfg := Config{NurseryBytes: 128, NurseryRegionShift: shift, SurvivorBytes: 128, VerifyAfterCollect: true}
			c, err := NewCollector(cfg, testTypes(t))
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 3; i++ {
				regionStruct(t, c, 0, EmptyRoots{})
			}
			child := regionStruct(t, c, 0, EmptyRoots{})
			if err := c.StructSet(child, 1, I64Value(1234567)); err != nil {
				t.Fatal(err)
			}
			// No explicit child root: constructor initializer staging must preserve it
			// across the exhaustion collection.
			a, err := c.NewArrayWithRoots(3, length, RefValue(child), EmptyRoots{})
			if err != nil {
				t.Fatal(err)
			}
			if err := c.ArraySet(a, 1, RefValue(a)); err != nil {
				t.Fatal(err)
			}
			original := a
			roots := Slots{(*Root)(&a)}
			for i := 0; i < 4; i++ {
				if err := c.CollectFull(roots); err != nil {
					t.Fatal(err)
				}
				if err := c.CollectMinor(roots); err != nil {
					t.Fatal(err)
				}
				edge, err := c.ArrayGet(a, 0)
				if err != nil || edge.Ref != child {
					t.Fatalf("child identity: %v %v", edge, err)
				}
				self, err := c.ArrayGet(a, 1)
				if err != nil || self.Ref != a || a != original {
					t.Fatal("cycle identity changed", err)
				}
				value, err := c.StructGet(child, 1)
				if err != nil || value.I64() != 1234567 {
					t.Fatal("child payload changed", err)
				}
				if c.Stats().LiveObjects != 2 {
					t.Fatalf("reachable graph differs from oracle: %+v", c.Stats())
				}
			}
			if err := c.CollectFull(EmptyRoots{}); err != nil {
				t.Fatal(err)
			}
			if c.Stats().LiveObjects != 0 {
				t.Fatal("unrooted cycle retained")
			}
			c.Close()
		}
	}
}
