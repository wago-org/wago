package gc

import "testing"

func TestNativeResolutionEpochPromotionWithoutGrowth(t *testing.T) {
	d, _ := NewArrayDesc(0, StorageI32)
	c, err := NewCollector(Config{NurseryBytes: 16384, ThroughputHeapBytes: 65536, ThroughputPageBytes: 4096}, []TypeDesc{d})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	// Prime an old-space slab so the second promotion only changes the entry.
	a, err := c.NewArrayDefault(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.ForcePromote(a); err != nil {
		t.Fatal(err)
	}
	b, err := c.NewArrayDefault(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	oldLen := len(c.throughput.mem)
	generation := c.NativeView().RefreshGeneration
	if err = c.ForcePromote(b); err != nil {
		t.Fatal(err)
	}
	if len(c.throughput.mem) != oldLen {
		t.Fatal("fixture unexpectedly grew old heap")
	}
	if c.NativeView().RefreshGeneration <= generation {
		t.Fatal("promotion left cached nursery address valid")
	}
}

func TestNativeResolutionEpochTinySweep(t *testing.T) {
	d, _ := NewStructDesc(0, nil)
	c, err := NewCollector(Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{d})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 100 && c.validObjectRef(r); n++ {
		generation := c.NativeView().RefreshGeneration
		if err = c.Step(nil); err != nil {
			t.Fatal(err)
		}
		if c.NativeView().RefreshGeneration <= generation {
			t.Fatal("Tiny step failed to invalidate resolution")
		}
	}
	if c.validObjectRef(r) {
		t.Fatal("unrooted object did not sweep")
	}
}

func TestNativeResolutionEpochSaturates(t *testing.T) {
	d, _ := NewStructDesc(0, nil)
	c, err := NewCollector(Config{}, []TypeDesc{d})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	v := c.NativeView()
	v.RefreshGeneration = NativeResolutionGenerationDisabled - 1
	for _, f := range []func(){c.refreshNativeView, c.refreshNativeHandles, c.refreshNativeCards, c.invalidateNativeResolution} {
		f()
		if v.RefreshGeneration != NativeResolutionGenerationDisabled {
			t.Fatal("epoch wrapped or remained cacheable")
		}
	}
	if err = c.CollectFull(nil); err != nil {
		t.Fatal(err)
	}
	if v.RefreshGeneration != NativeResolutionGenerationDisabled {
		t.Fatal("collection reenabled saturated epoch")
	}
	c.Close()
	if v.RefreshGeneration != NativeResolutionGenerationDisabled || v.Handles != 0 {
		t.Fatal("close failed to invalidate saturated cache")
	}
}
