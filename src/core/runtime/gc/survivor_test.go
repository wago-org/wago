package gc

import (
	"testing"
)

func TestSurvivorConfigRejectsContradictoryProfiles(t *testing.T) {
	for _, cfg := range []Config{
		{Profile: ProfileTiny, SurvivorBytes: 32},
		{Profile: ProfileTiny, MinorPauseTargetMicros: 1},
		{DisableMovingNursery: true, SurvivorBytes: 32},
		{DisableMovingNursery: true, MinorPauseTargetMicros: 1},
	} {
		if err := ValidateConfig(cfg); err == nil {
			t.Fatalf("ValidateConfig accepted contradictory survivor config %+v", cfg)
		}
	}
}

func TestThroughputSurvivorAgesBeforePromotion(t *testing.T) {
	c := newTestCollector(t, Config{NurseryBytes: 1024, SurvivorBytes: 512, VerifyAfterCollect: true, PoisonFreed: true})
	ref, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	h := handleOf(ref)
	originalOff := c.handles[h].off
	root := Root(ref)
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	first := c.handles[h]
	if !first.young() || first.space != spaceNursery || first.age() != 1 || !c.inActiveSurvivor(first) {
		t.Fatalf("first survivor = %+v age=%d", first, first.age())
	}
	if first.off == originalOff {
		t.Fatal("first survivor was not copied out of Eden")
	}
	for _, b := range c.nursery[originalOff : originalOff+first.size] {
		if b != 0xdd {
			t.Fatal("evacuated Eden bytes were not poisoned")
		}
	}
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if got := c.handles[h]; got.space != spaceOld || got.young() {
		t.Fatalf("second survivor = %+v, want tenured old object", got)
	}
	if Ref(root) != ref {
		t.Fatalf("stable handle changed: got %v want %v", Ref(root), ref)
	}
}

func TestThroughputSurvivorCapacityFallsBackToPromotion(t *testing.T) {
	c := newTestCollector(t, Config{NurseryBytes: 1024, SurvivorBytes: 32, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096, VerifyAfterCollect: true})
	roots := make([]Root, 3)
	slots := make(Slots, len(roots))
	for i := range roots {
		ref, err := c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		roots[i], slots[i] = Root(ref), &roots[i]
	}
	if err := c.CollectMinor(slots); err != nil {
		t.Fatal(err)
	}
	young, old := 0, 0
	for _, root := range roots {
		e := c.entry(Ref(root))
		if e.young() {
			young++
		} else if e.space == spaceOld {
			old++
		}
	}
	if young != 1 || old != 2 {
		t.Fatalf("survivor overflow result young/old=%d/%d entries=%+v/%+v/%+v, want 1/2", young, old, *c.entry(Ref(roots[0])), *c.entry(Ref(roots[1])), *c.entry(Ref(roots[2])))
	}
}

func TestThroughputLargeYoungObjectAgesInPlace(t *testing.T) {
	c := newTestCollector(t, Config{NurseryBytes: 1024, SurvivorBytes: 512, LargeObjectBytes: 64, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096, VerifyAfterCollect: true})
	array, err := c.NewArrayDefault(2, 64)
	if err != nil {
		t.Fatal(err)
	}
	h := handleOf(array)
	before := c.handles[h]
	if before.space != spaceLarge || !before.young() || before.age() != 0 {
		t.Fatalf("new large young entry = %+v", before)
	}
	root := Root(array)
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	first := c.handles[h]
	if first.space != spaceLarge || !first.young() || first.age() != 1 || first.off != before.off {
		t.Fatalf("first large survivor = %+v, before=%+v", first, before)
	}
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	second := c.handles[h]
	if second.space != spaceLarge || second.young() || second.off != before.off || c.header(array).Flags&FlagOld == 0 {
		t.Fatalf("tenured large survivor = %+v flags=%x", second, c.header(array).Flags)
	}
}

func TestNewlyTenuredParentCardsYoungerChild(t *testing.T) {
	c := newTestCollector(t, Config{NurseryBytes: 1024, SurvivorBytes: 512, VerifyAfterCollect: true})
	parent, err := c.NewStructDefault(1)
	if err != nil {
		t.Fatal(err)
	}
	root := Root(parent)
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.StructSet(parent, 0, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	if c.RememberedCount() != 0 {
		t.Fatal("young parent unexpectedly recorded a card")
	}
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if c.entry(parent).space != spaceOld || !c.entry(child).young() || c.RememberedCount() != 1 || c.CardCount() != 1 {
		t.Fatalf("tenured parent/younger child metadata parent=%+v child=%+v remembered=%d cards=%d", *c.entry(parent), *c.entry(child), c.RememberedCount(), c.CardCount())
	}
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if !c.validObjectRef(child) || c.entry(child).space != spaceOld {
		t.Fatal("carded younger child was lost after parent tenuring")
	}
}

func TestThroughputSurvivorRetainsAuthoritativeCards(t *testing.T) {
	c := newTestCollector(t, Config{NurseryBytes: 1024, SurvivorBytes: 512, VerifyAfterCollect: true})
	parent, err := c.NewStructDefault(1)
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
	if err := c.StructSet(parent, 0, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	root := Root(parent)
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if !c.entry(child).young() || c.RememberedCount() != 1 || c.CardCount() != 1 {
		t.Fatalf("first minor child/metadata = %+v remembered=%d cards=%d", *c.entry(child), c.RememberedCount(), c.CardCount())
	}
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if c.entry(child).young() || c.entry(child).space != spaceOld || c.RememberedCount() != 0 || c.CardCount() != 0 {
		t.Fatalf("second minor child/metadata = %+v remembered=%d cards=%d", *c.entry(child), c.RememberedCount(), c.CardCount())
	}
}

func TestThroughputAdaptiveTenuringRespondsToPressureAndPauseTarget(t *testing.T) {
	c := newTestCollector(t, Config{NurseryBytes: 1024, SurvivorBytes: 512, LargeObjectBytes: 16, ThroughputHeapBytes: 8192, ThroughputPageBytes: 4096, MinorPauseTargetMicros: 1000})
	for i := 0; i < 190; i++ {
		ref, err := c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.ForcePromote(ref); err != nil {
			t.Fatal(err)
		}
	}
	ref, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	root := Root(ref)
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if c.tenuringThreshold != 3 {
		t.Fatalf("old-pressure threshold=%d, want 3", c.tenuringThreshold)
	}
	c.adaptTenuring(uint64(c.survivorBytes), 0, uint64(c.cfg.MinorPauseTargetMicros)*1000+1)
	if c.tenuringThreshold != 2 {
		t.Fatalf("pause-pressure threshold=%d, want 2", c.tenuringThreshold)
	}
}

func TestThroughputFullCollectionHandlesActiveSurvivor(t *testing.T) {
	c := newTestCollector(t, Config{NurseryBytes: 1024, SurvivorBytes: 512, VerifyAfterCollect: true})
	ref, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	root := Root(ref)
	if err := c.CollectMinor(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if !c.entry(ref).young() {
		t.Fatal("first minor did not retain survivor")
	}
	if err := c.CollectFull(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if !c.validObjectRef(ref) || !c.entry(ref).young() {
		t.Fatal("full collection lost active survivor")
	}
	root = Root(Null())
	if err := c.CollectFull(Slots{&root}); err != nil {
		t.Fatal(err)
	}
	if c.validObjectRef(ref) || len(c.nurseryHandles) != 0 {
		t.Fatal("full collection retained unreachable survivor")
	}
}
