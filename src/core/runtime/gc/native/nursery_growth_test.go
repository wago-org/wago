package gc

import (
	"fmt"
	"runtime"
	"testing"
)

func TestDefaultNurseryGrowsForRollingLiveGraph(t *testing.T) {
	for _, replaceRoot := range []bool{false, true} {
		t.Run(fmt.Sprint("replace root=", replaceRoot), func(t *testing.T) {
			testDefaultNurseryRollingGraph(t, replaceRoot)
		})
	}
}

func testDefaultNurseryRollingGraph(t *testing.T, replaceRoot bool) {
	if runtime.GOARCH != "amd64" {
		t.Skip("automatic nursery sizing is measured on AMD64")
	}
	values, err := NewArrayDesc(0, StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := NewArrayDesc(1, StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{VerifyAfterCollect: true, PoisonFreed: true}, []TypeDesc{values, refs})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	parent, err := c.NewArrayDefault(1, 4096)
	if err != nil {
		t.Fatal(err)
	}
	root := Root(parent)
	roots := Slots{&root}
	const allocations = 64 << 10
	for i := 0; i < allocations; i++ {
		if replaceRoot && i != 0 && i%4096 == 0 {
			parent, err = c.NewArrayDefaultWithRoots(1, 4096, roots)
			if err != nil {
				t.Fatal(err)
			}
			root = Root(parent)
		}
		child, err := c.NewArrayWithRoots(0, 16, I32Value(int32(i)), roots)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.ArraySet(parent, uint32(i&4095), RefValue(child)); err != nil {
			t.Fatal(err)
		}
	}
	if c.cfg.NurseryBytes != 2<<20 || c.survivorBytes != 512<<10 || len(c.nursery) != 3<<20 {
		t.Fatalf("rolling graph nursery = %d/%d/%d, want 2 MiB Eden/512 KiB survivor/3 MiB backing", c.cfg.NurseryBytes, c.survivorBytes, len(c.nursery))
	}
	if err := c.CollectMinor(roots); err != nil {
		t.Fatal(err)
	}
	for i := uint32(0); i < 4096; i++ {
		child, err := c.ArrayGet(parent, i)
		if err != nil {
			t.Fatal(err)
		}
		value, err := c.ArrayGet(child.Ref, 15)
		if err != nil || value.I32() != allocations-4096+int32(i) {
			t.Fatalf("retained value %d = %v, %v", i, value, err)
		}
	}
	if err := c.Verify(roots); err != nil {
		t.Fatal(err)
	}
	view := c.NativeView()
	if view.NurseryAllocBytes != c.cfg.NurseryBytes || view.Spaces[NativeSpaceNursery].Bytes != uint32(len(c.nursery)) || view.Spaces[NativeSpaceNursery].Base != sliceData(c.nursery) {
		t.Fatal("native view did not publish grown nursery backing")
	}
}

func TestNurseryGrowthClearsOldPressureAfterQuietPeriod(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("automatic nursery sizing is measured on AMD64")
	}
	desc, err := NewArrayDesc(0, StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{}, []TypeDesc{desc})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.growDefaultNursery(defaultNursery, 0)
	for range 8 {
		c.growDefaultNursery(0, 0)
	}
	c.growDefaultNursery(defaultNursery, 0)
	if c.cfg.NurseryBytes != defaultNursery {
		t.Fatal("isolated pressure spikes grew the nursery")
	}
	c.growDefaultNursery(0, 0)
	c.growDefaultNursery(defaultNursery, 0)
	if c.cfg.NurseryBytes != 2*defaultNursery {
		t.Fatal("repeated pressure did not grow the empty nursery")
	}
}

func TestDefaultNurseryKeepsLowSurvivalFootprint(t *testing.T) {
	desc, err := NewArrayDesc(0, StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{}, []TypeDesc{desc})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for i := 0; i < 8192; i++ {
		if _, err := c.NewArrayWithRoots(0, 16, I32Value(int32(i)), EmptyRoots{}); err != nil {
			t.Fatal(err)
		}
	}
	if c.cfg.NurseryBytes != defaultNursery || len(c.nursery) != 2*defaultNursery {
		t.Fatalf("low-survival nursery grew to %d/%d", c.cfg.NurseryBytes, len(c.nursery))
	}
}

func TestExplicitNurseryPolicyDisablesGrowth(t *testing.T) {
	desc, err := NewArrayDesc(0, StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		cfg  Config
	}{
		{"fixed Eden equal to default", Config{NurseryBytes: defaultNursery}},
		{"fixed survivor equal to default", Config{SurvivorBytes: defaultNursery / 2}},
		{"fixed old heap", Config{ThroughputHeapBytes: 1 << 20}},
		{"fixed large object threshold", Config{LargeObjectBytes: defaultLarge}},
		{"fixed pause target", Config{MinorPauseTargetMicros: 1000}},
		{"stress nursery", Config{StressNurseryBytes: defaultNursery}},
		{"stress allocation", Config{CollectEveryAlloc: true}},
		{"forced major", Config{ForceMajorEveryMinor: true}},
		{"nonmoving", Config{DisableMovingNursery: true}},
		{"disabled", Config{DisableCollection: true}},
		{"tiny", Config{Profile: ProfileTiny}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewCollector(tc.cfg, []TypeDesc{desc})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if c.nurseryGrowthLimit != 0 {
				t.Fatal("explicit policy enabled automatic nursery growth")
			}
		})
	}
}

func TestLargeOnlyAgingDoesNotGrowNursery(t *testing.T) {
	desc, err := NewArrayDesc(0, StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{}, []TypeDesc{desc})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	root := Root(Null())
	roots := Slots{&root}
	for i := 0; i < 4; i++ {
		ref, err := c.NewArrayDefaultWithRoots(0, defaultLarge/4, roots)
		if err != nil {
			t.Fatal(err)
		}
		root = Root(ref)
		for range 2 {
			if err := c.CollectMinor(roots); err != nil {
				t.Fatal(err)
			}
		}
	}
	if c.stats.PromotedBytes == 0 || c.stats.YoungBytesCopied != 0 || c.cfg.NurseryBytes != defaultNursery {
		t.Fatalf("large-only policy = promoted %d copied %d Eden %d", c.stats.PromotedBytes, c.stats.YoungBytesCopied, c.cfg.NurseryBytes)
	}
}
