//go:build amd64 || arm64

package gc

import (
	"fmt"
	"testing"
)

func TestDefaultNurseryGrowthCumulativeBoundary(t *testing.T) {
	c, err := NewCollector(Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	chunk := uint64(defaultNursery / 8)
	for range 8 {
		c.growDefaultNursery(chunk, 0)
	}
	if c.nurseryGrowthSurvived != defaultNursery || c.cfg.NurseryBytes != defaultNursery {
		t.Fatal("survival at capacity must not grow the nursery")
	}
	c.growDefaultNursery(chunk, 0)
	if c.cfg.NurseryBytes != 2*defaultNursery {
		t.Fatal("cumulative survival above capacity did not grow the nursery")
	}
}

func TestDefaultNurseryGrowthDecaysSparseSurvival(t *testing.T) {
	c, err := NewCollector(Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	threshold := uint64(defaultNursery / 8)
	c.growDefaultNursery(threshold, 0)
	for range 7 {
		c.growDefaultNursery(threshold-1, 0)
	}
	if c.nurseryGrowthSurvived != uint32(threshold) || c.nurseryGrowthQuiet != 7 {
		t.Fatal("seven sparse collections cleared cumulative survival")
	}
	c.growDefaultNursery(threshold, 0)
	if c.nurseryGrowthSurvived != uint32(2*threshold) || c.nurseryGrowthQuiet != 0 {
		t.Fatal("dense survival did not reset the quiet count")
	}
	for range 8 {
		c.growDefaultNursery(threshold-1, 0)
	}
	if c.nurseryGrowthSurvived != 0 || c.nurseryGrowthQuiet != 0 || c.cfg.NurseryBytes != defaultNursery {
		t.Fatal("eight sparse collections did not clear cumulative survival")
	}
	for range 256 {
		c.growDefaultNursery(threshold-1, 0)
	}
	if c.cfg.NurseryBytes != defaultNursery {
		t.Fatal("persistent sparse survival grew the nursery")
	}
}

func TestDefaultNurseryGrowthKeepsDensePolicyAboveOneMiB(t *testing.T) {
	c, err := NewCollector(Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	for c.cfg.NurseryBytes < 1<<20 {
		c.nurseryGrowthSurvived = c.cfg.NurseryBytes
		c.growDefaultNursery(uint64(c.cfg.NurseryBytes/8), 0)
	}
	threshold := uint64(c.cfg.NurseryBytes / 4)
	c.growDefaultNursery(threshold-1, 0)
	if c.nurseryGrowthSurvived != 0 || c.cfg.NurseryBytes != 1<<20 {
		t.Fatal("sparse survival counted as dense growth pressure")
	}
	c.growDefaultNursery(threshold, 0)
	if c.nurseryGrowthSurvived != 1 || c.cfg.NurseryBytes != 1<<20 {
		t.Fatal("one dense event must not grow the nursery")
	}
	for range 8 {
		c.growDefaultNursery(threshold-1, 0)
	}
	if c.nurseryGrowthSurvived != 0 || c.nurseryGrowthQuiet != 0 || c.cfg.NurseryBytes != 1<<20 {
		t.Fatal("eight sparse events did not clear dense growth pressure")
	}
	c.growDefaultNursery(threshold, 0)
	c.growDefaultNursery(threshold, 0)
	if c.cfg.NurseryBytes != 2<<20 {
		t.Fatal("two dense events did not grow the nursery")
	}
}

func TestDefaultNurseryDensePregrowAdmission(t *testing.T) {
	newCollector := func() *Collector {
		c, err := NewCollector(Config{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(c.Close)
		c.nurseryGrowthSurvived = c.cfg.NurseryBytes
		return c
	}

	dense := newCollector()
	dense.growDefaultNursery(uint64(defaultNursery/8), defaultNursery/32)
	wantDense := uint32(denseNurseryPregrowBytes)
	if !denseNurseryPregrowEnabled {
		wantDense = 2 * defaultNursery
	}
	if dense.cfg.NurseryBytes != wantDense {
		t.Fatalf("dense Eden = %d, want %d", dense.cfg.NurseryBytes, wantDense)
	}
	if denseNurseryPregrowEnabled && len(dense.nursery) != int(wantDense+2*(512<<10)) {
		t.Fatalf("dense backing = %d, want %d", len(dense.nursery), wantDense+2*(512<<10))
	}

	wide := newCollector()
	wide.growDefaultNursery(uint64(defaultNursery/8), defaultNursery/80)
	if wide.cfg.NurseryBytes != 2*defaultNursery {
		t.Fatalf("wide Eden = %d, want ordinary doubling to %d", wide.cfg.NurseryBytes, 2*defaultNursery)
	}
}

func TestDefaultNurseryGrowthRejectsInvalidSurvivorExtent(t *testing.T) {
	for _, invalid := range []string{"before span", "after span", "size exceeds span", "bump exceeds space"} {
		t.Run(invalid, func(t *testing.T) {
			desc, err := NewArrayDesc(0, StorageI32)
			if err != nil {
				t.Fatal(err)
			}
			c, err := NewCollector(Config{}, []TypeDesc{desc})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(c.Close)
			r, err := c.NewArrayDefault(0, 4)
			if err != nil {
				t.Fatal(err)
			}
			root := Root(r)
			roots := Slots{&root}
			if err := c.CollectMinor(roots); err != nil {
				t.Fatal(err)
			}
			e := c.entry(r)
			valid, bump := *e, c.survivorBump
			base := c.survivorBase(c.survivorFrom)
			switch invalid {
			case "before span":
				e.off = base - 1
			case "after span":
				e.off = base + bump + 1
			case "size exceeds span":
				e.size = bump + 1
			case "bump exceeds space":
				c.survivorBump = c.survivorBytes + 1
			}
			invalidEntry := *e
			backing := sliceData(c.nursery)
			c.nurseryGrowthSurvived = defaultNursery
			c.growDefaultNursery(uint64(defaultNursery/8), 0)
			if c.cfg.NurseryBytes != defaultNursery || sliceData(c.nursery) != backing || *e != invalidEntry {
				t.Fatal("growth changed backing or handle with an invalid survivor extent")
			}
			*e, c.survivorBump = valid, bump
			if err := c.Verify(roots); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDefaultNurseryGrowthRelocatesLiveSurvivors(t *testing.T) {
	for _, from := range []uint8{0, 1} {
		t.Run(fmt.Sprint(from), func(t *testing.T) {
			vector, err := NewStructDesc(0, []StorageKind{StorageV128})
			if err != nil {
				t.Fatal(err)
			}
			refs, err := NewArrayDesc(1, StorageRefNull)
			if err != nil {
				t.Fatal(err)
			}
			numbers, err := NewArrayDesc(2, StorageI32)
			if err != nil {
				t.Fatal(err)
			}
			c, err := NewCollector(Config{VerifyAfterCollect: true, PoisonFreed: true}, []TypeDesc{vector, refs, numbers})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(c.Close)
			parent, err := c.NewArrayDefault(1, 1)
			if err != nil {
				t.Fatal(err)
			}
			if err := c.ForcePromote(parent); err != nil {
				t.Fatal(err)
			}
			const lo, hi = uint64(0x123456789abcdef0), uint64(0xfedcba9876543210)
			child, err := c.NewStructWithRoots(0, []Value{V128Value(lo, hi)}, EmptyRoots{})
			if err != nil {
				t.Fatal(err)
			}
			if err := c.ArraySet(parent, 0, RefValue(child)); err != nil {
				t.Fatal(err)
			}
			large, err := c.NewArrayDefault(2, c.cfg.LargeObjectBytes/4)
			if err != nil {
				t.Fatal(err)
			}
			largeOffset := c.entry(large).off
			filler, err := c.NewArrayDefault(2, 4096)
			if err != nil {
				t.Fatal(err)
			}
			refsRoot := []Root{Root(parent), Root(large), Root(filler)}
			roots := Slots{&refsRoot[0], &refsRoot[1], &refsRoot[2]}
			liveBytes := uint64(c.entry(child).size) + uint64(c.entry(filler).size)
			c.tenuringThreshold = maxTenuringThreshold
			cycles := 2
			if from == 1 {
				cycles = 1
			}
			c.nurseryGrowthSurvived = defaultNursery - uint32(cycles)*uint32(liveBytes) + 1
			view := c.NativeView()
			oldBacking := view.Spaces[NativeSpaceNursery].Base
			before := c.Stats()
			for range cycles {
				if !c.PrepareNativeAllocation(32) {
					t.Fatal("failed to reserve an unpublished native batch")
				}
				if err := c.CollectMinor(roots); err != nil {
					t.Fatal(err)
				}
			}
			if c.cfg.NurseryBytes != 2*defaultNursery || c.survivorBump == 0 || c.survivorFrom != from {
				t.Fatalf("grown Eden/live/index = %d/%d/%d", c.cfg.NurseryBytes, c.survivorBump, c.survivorFrom)
			}
			if view.Spaces[NativeSpaceNursery].Base == oldBacking || view.Spaces[NativeSpaceNursery].Base != sliceData(c.nursery) || view.NurseryAllocBytes != c.cfg.NurseryBytes {
				t.Fatal("collection did not refresh the native view after live relocation")
			}
			if c.nativeStructAlloc.Count != 0 || !c.inActiveSurvivor(*c.entry(child)) || c.entry(child).off%16 != 0 {
				t.Fatal("growth retained reservations or invalid vector placement")
			}
			if got, err := c.ArrayGet(parent, 0); err != nil || got.Ref != child {
				t.Fatalf("remembered child = %+v, %v", got, err)
			}
			if got, err := c.StructGet(child, 0); err != nil || got != V128Value(lo, hi) {
				t.Fatalf("relocated vector = %+v, %v", got, err)
			}
			if c.entry(large).space != spaceLarge || !c.entry(large).young() || c.entry(large).off != largeOffset {
				t.Fatal("nursery growth moved or tenured the young large object")
			}
			wantCopied := uint64(cycles)*liveBytes + uint64(c.survivorBump)
			if got := c.Stats().YoungBytesCopied - before.YoungBytesCopied; got != wantCopied {
				t.Fatalf("copied bytes = %d, want %d including growth copy", got, wantCopied)
			}
			if from == 1 {
				c.tenuringThreshold = maxTenuringThreshold
			}
			if err := c.CollectMinor(roots); err != nil {
				t.Fatal(err)
			}
			if from == 1 && (!c.inActiveSurvivor(*c.entry(child)) || c.survivorFrom != 0) {
				t.Fatal("relocated child did not copy into the other grown survivor space")
			}
			if from == 0 && c.entry(child).space != spaceOld {
				t.Fatal("relocated child did not promote after growth")
			}
			if err := c.Verify(roots); err != nil {
				t.Fatal(err)
			}
			if got, err := c.ArrayGet(parent, 0); err != nil || got.Ref != child {
				t.Fatalf("remembered child after another collection = %+v, %v", got, err)
			}
			if got, err := c.StructGet(child, 0); err != nil || got != V128Value(lo, hi) {
				t.Fatalf("vector after another collection = %+v, %v", got, err)
			}
		})
	}
}

func TestDenseNurseryPregrowthProgresses(t *testing.T) {
	if !denseNurseryPregrowEnabled {
		t.Skip("dense pregrowth disabled")
	}
	c, err := NewCollector(Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.nurseryGrowthSurvived = c.cfg.NurseryBytes
	c.growDefaultNursery(uint64(c.cfg.NurseryBytes), 80000)
	if c.cfg.NurseryBytes != denseNurseryPregrowBytes {
		t.Fatalf("pregrowth=%d", c.cfg.NurseryBytes)
	}
	for range 2 {
		c.growDefaultNursery(uint64(c.cfg.NurseryBytes), 80000)
	}
	if c.cfg.NurseryBytes != c.nurseryGrowthLimit {
		t.Fatalf("dense nursery stuck at %d; limit %d", c.cfg.NurseryBytes, c.nurseryGrowthLimit)
	}
}
