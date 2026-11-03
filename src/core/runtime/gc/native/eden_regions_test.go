package gc

import (
	"testing"
	"unsafe"
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

func TestEdenRegionsReuseWithoutEvacuatingLiveRegion(t *testing.T) {
	c := regionCollector(t)
	// Four 32-byte objects occupy two 64-byte regions. Retain only the final one.
	for i := 0; i < 3; i++ {
		regionStruct(t, c, 0, EmptyRoots{})
	}
	live := regionStruct(t, c, 0, EmptyRoots{})
	root := live
	roots := Slots{(*Root)(&root)}
	before := *c.entry(live)
	if err := c.StructSet(live, 1, I64Value(0x123456789)); err != nil {
		t.Fatal(err)
	}
	for cycle := 0; cycle < 20000; cycle++ {
		regionStruct(t, c, 0, roots)
		regionStruct(t, c, 0, roots)
		if *c.entry(live) != before || root != live {
			t.Fatal("full tracing moved live identity/payload")
		}
		value, err := c.StructGet(live, 1)
		if err != nil || value.I64() != 0x123456789 {
			t.Fatalf("payload=%v err=%v", value, err)
		}
	}
	if c.Stats().FullCollections == 0 || c.Stats().MinorCollections != 0 || c.Stats().YoungBytesCopied != 0 {
		t.Fatalf("not region reuse: %+v", c.Stats())
	}
	if c.nativeAllocationAllowed(32) || c.PrepareNativeArrayAllocationImmediate(5000) || c.PrepareDeadAllocationBatch(32, 8).TryReserve(1) {
		t.Fatal("native allocator bypassed region bounds")
	}
	if c.NativeView().NurseryAllocBytes != 0 {
		t.Fatal("native Eden admission exposed")
	}
	if len(c.handles) > 5 || len(c.edenRegions) != 2 {
		t.Fatal("region reuse grew handle/region metadata")
	}
	if err := c.Verify(roots); err != nil {
		t.Fatal(err)
	}
}

func TestEdenRegionsCrossRegionEdgeAndEvacuation(t *testing.T) {
	c := regionCollector(t)
	parent := regionStruct(t, c, 1, EmptyRoots{})
	regionStruct(t, c, 0, Slots{(*Root)(&parent)})
	child := regionStruct(t, c, 0, Slots{(*Root)(&parent)})
	if err := c.StructSet(parent, 0, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	regionStruct(t, c, 0, Slots{(*Root)(&parent)})
	roots := Slots{(*Root)(&parent)}
	// Both full regions remain live, so exact full tracing cannot reset either.
	regionStruct(t, c, 0, roots)
	if c.Stats().MinorCollections != 1 || c.Stats().FullCollections != 1 {
		t.Fatalf("missing fallback: %+v", c.Stats())
	}
	value, err := c.StructGet(parent, 0)
	if err != nil || value.Ref != child {
		t.Fatalf("lost cross-region child: %v %v", value, err)
	}
	if err := c.Verify(roots); err != nil {
		t.Fatal(err)
	}
	// Promote the parent, store a new region object, and exercise old-to-young cards.
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

func TestEdenRegionsExhaustionFeedbackRecoveryAndSizes(t *testing.T) {
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

	// The first retained-live exhaustion probes full and falls back to minor.
	allocateChain(9)
	first := c.Stats()
	if first.FullCollections != 1 || first.MinorCollections != 1 {
		t.Fatalf("first exhaustion did not probe full then minor: %+v", first)
	}
	// The equivalent exhaustion uses the learned minor path.
	allocateChain(8)
	steady := c.Stats()
	if steady.FullCollections != first.FullCollections || steady.MinorCollections != first.MinorCollections+1 {
		t.Fatalf("known futile full repeated: before=%+v after=%+v", first, steady)
	}

	// An explicit cleanup full must neither clear nor refresh allocation feedback.
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	before := c.Stats()
	allocateChain(8)
	after := c.Stats()
	if after.FullCollections != before.FullCollections || after.MinorCollections != before.MinorCollections+1 {
		t.Fatalf("explicit full changed exhaustion feedback: before=%+v after=%+v", before, after)
	}

	// A different allocation shape is not covered by the 32-byte failure.
	before = c.Stats()
	for i := 0; i < 8 && c.Stats().FullCollections == before.FullCollections; i++ {
		if _, err := c.NewArrayDefaultWithRoots(3, 4, roots); err != nil {
			t.Fatal(err)
		}
	}
	after = c.Stats()
	if after.FullCollections != before.FullCollections+1 {
		t.Fatalf("failure for another size suppressed full probe: before=%+v after=%+v", before, after)
	}

	// Drop the graph. The finite skip window must eventually re-probe and learn
	// that exact tracing can reclaim regions again.
	root = Root(Null())
	fullBefore := c.Stats().FullCollections
	for i := 0; i < 80 && c.Stats().FullCollections == fullBefore; i++ {
		regionStruct(t, c, 1, roots)
	}
	if c.Stats().FullCollections == fullBefore {
		t.Fatal("finite feedback window did not re-probe full")
	}
	minorBefore := c.Stats().MinorCollections
	for i := 0; i < 32; i++ {
		regionStruct(t, c, 1, roots)
	}
	if c.Stats().MinorCollections != minorBefore {
		t.Fatalf("recovered low-survival workload kept bypassing profitable full: %+v", c.Stats())
	}

	// Returning to a retained graph probes again, then relearns the futile-full
	// result for the same request shape.
	before = c.Stats()
	allocateChain(9)
	after = c.Stats()
	if after.FullCollections == before.FullCollections || after.MinorCollections == before.MinorCollections {
		t.Fatalf("high-survival return did not probe and fall back: before=%+v after=%+v", before, after)
	}
	before = after
	allocateChain(8)
	after = c.Stats()
	if after.FullCollections != before.FullCollections || after.MinorCollections != before.MinorCollections+1 {
		t.Fatalf("high-survival return was not relearned: before=%+v after=%+v", before, after)
	}
	if err := c.Verify(roots); err != nil {
		t.Fatal(err)
	}
}

func TestEdenRegionsLargeAndNoRoots(t *testing.T) {
	c := regionCollector(t)
	a, err := c.NewArrayDefault(2, 32)
	if err != nil {
		t.Fatal(err)
	}
	if c.entry(a).space != spaceLarge {
		t.Fatal("object spanned regions")
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

func TestEdenRegionsAdmissionAndLayout(t *testing.T) {
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
	if unsafe.Sizeof(uintptr(0)) == 8 && unsafe.Sizeof(Config{}) != 80 {
		t.Fatal("unexpected config layout")
	}
	if unsafe.Offsetof(Collector{}.edenRegions) <= unsafe.Offsetof(Collector{}.nativeLargeArrayNurseryIndex) {
		t.Fatal("region state disturbed existing fields")
	}
	c, err := NewCollector(Config{}, testTypes(t))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if len(c.edenRegions) != 0 {
		t.Fatal("default enabled regions")
	}
}

func TestEdenRegionsCollectionPolicies(t *testing.T) {
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

func TestEdenRegionsArrayCycleAndInitializerOracle(t *testing.T) {
	// Same observable graph contract with the explicit baseline and region policy.
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
			// across the exhaustion collection. Small arrays stay inside one region;
			// the oversized case uses the existing large-object span contract.
			a, err := c.NewArrayWithRoots(3, length, RefValue(child), EmptyRoots{})
			if err != nil {
				t.Fatal(err)
			}
			if shift != 0 && length <= 5 && c.entry(a).space != spaceNursery {
				t.Fatal("small array did not enter region storage")
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
