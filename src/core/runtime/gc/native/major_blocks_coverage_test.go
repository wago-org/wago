//go:build amd64

package gc

import (
	"testing"
	"unsafe"
)

func TestMajorBlocksMixedLiveThenDead(t *testing.T) {
	c := newMajorBlocksEmptyCollector(t, 16383, false)
	c.tenuringThreshold = 1
	old, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.CollectMinor(RefSliceRoots{old}); err != nil {
		t.Fatal(err)
	}
	large, err := c.NewArrayDefault(2, 4096)
	if err != nil {
		t.Fatal(err)
	}
	live, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	oh, lh, keep := uint32(old)>>1, uint32(large)>>1, uint32(live)>>1
	if c.handles[oh].space != spaceOld || c.handles[lh].space != spaceLarge || keep >= lh || lh >= oh || keep>>majorBlockShift != oh>>majorBlockShift {
		t.Fatal("fixture does not put dead old/large entries after live entry")
	}
	if err = c.CollectFull(RefSliceRoots{live}); err != nil {
		t.Fatal(err)
	}
	if len(c.majorBlocks) == 0 || c.Stats().LiveObjects != 1 || c.handles[oh].space != spaceFree || c.handles[lh].space != spaceFree {
		t.Fatal("second block loop skipped dead old/large entries")
	}
	if err = c.CollectFull(EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	if c.Stats().LiveObjects != 0 {
		t.Fatal("survivor not reclaimed")
	}
}

func TestMajorBlocksCheckedAndThroughputNativeReservations(t *testing.T) {
	c := newMajorBlocksEmptyCollector(t, 16383, true)
	if !c.PrepareNativeStructHandles() {
		t.Fatal("checked native reservation failed")
	}
	for _, h := range c.nativeStructAlloc.Handles[:c.nativeStructAlloc.Count] {
		if c.majorBlocks[h>>majorWordShift]&(uint64(1)<<((h>>majorBlockShift)&63)) == 0 {
			t.Fatal("checked batch omitted handle")
		}
	}
	c.CancelNativeAllocationBatch()
	if err := c.CollectFull(EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	for _, word := range c.majorBlocks {
		if word != 0 {
			t.Fatal("checked cancel left occupied block")
		}
	}
	if !c.PrepareNativeArrayAllocationImmediate(4096 + HeaderSize) {
		t.Fatal("throughput native reservation failed")
	}
	s := c.nativeLargeArrayAlloc
	if spaceKind((s.ChunkBump>>16)&0xff) != spaceLarge {
		t.Fatal("fixture did not reserve throughput-backed array")
	}
	h := s.Handle
	if c.majorBlocks[h>>majorWordShift]&(uint64(1)<<((h>>majorBlockShift)&63)) == 0 {
		t.Fatal("throughput reservation omitted handle")
	}
	if err := c.CollectFull(EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	if len(c.majorBlocks) == 0 || c.Stats().LiveObjects != 0 {
		t.Fatal("throughput reservation not canceled with index active")
	}
	for _, word := range c.majorBlocks {
		if word != 0 {
			t.Fatal("throughput cancellation left occupied block")
		}
	}
}

func TestMajorBlocksReservationWordRevisits(t *testing.T) {
	c := &Collector{majorBlocks: []uint64{uint64(1) << 63}}
	c.noteMajorHandles([]uint32{2, 16385, 3, 32770, 16641})
	want := []uint64{uint64(1)<<63 | 1, 3, 1}
	if len(c.majorBlocks) != len(want) {
		t.Fatalf("words=%x want=%x", c.majorBlocks, want)
	}
	for i, word := range want {
		if c.majorBlocks[i] != word {
			t.Fatalf("words=%x want=%x", c.majorBlocks, want)
		}
	}
}

func TestMajorBlocksFlagUsesExistingPadding(t *testing.T) {
	if unsafe.Offsetof(Collector{}.checkedHandles) != 3088 || unsafe.Offsetof(Collector{}.majorBlocks) != 3104 || unsafe.Sizeof(Collector{}) != 3128 {
		t.Fatal("native index changed existing collector offsets or grew beyond the slice descriptor")
	}
	if unsafe.Offsetof(Collector{}.majorYoungDirty) >= unsafe.Offsetof(Collector{}.checkedHandles) {
		t.Fatal("flag does not use existing padding")
	}
}

func publishMajorBlocksNative(c *Collector, count int) RefSliceRoots {
	roots := make(RefSliceRoots, count)
	off := c.nurseryBump
	for i := range roots {
		h := c.nativeStructAlloc.Handles[i]
		c.handles[h] = handleEntry{off: off + uint32(i)*16, size: 16, space: spaceNursery}
		roots[i] = makeObjRef(h)
		c.writeHeader(roots[i], ObjHeader{TypeID: 0, Size: 16, Flags: FlagPointerFree})
	}
	c.nurseryBump = off + uint32(count)*16
	c.nativeStructAlloc.Cursor = uint32(count)
	return roots
}

func TestMajorBlocksDeferredNativeGrowth(t *testing.T) {
	c := newMajorBlocksEmptyCollector(t, 16383, false)
	free := append([]uint32(nil), c.freeHandles...)
	c.freeHandles = free[:1]
	if !c.PrepareNativeStructHandles() {
		t.Fatal("mixed growth reservation failed")
	}
	c.freeHandles = free[1:]
	publishMajorBlocksNative(c, 2)
	if len(c.majorBlocks) != 1 || !c.majorYoungDirty {
		t.Fatal("native batch was eagerly indexed")
	}
	if err := c.CollectFull(EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	if len(c.majorBlocks) != 2 || c.majorYoungDirty || c.Stats().LiveObjects != 0 {
		t.Fatal("deferred native growth lost dead objects or disabled the map")
	}
}

func TestMajorBlocksDeferredNativeMinor(t *testing.T) {
	for _, mode := range []string{"immediate", "captured", "dense", "survivor"} {
		t.Run(mode, func(t *testing.T) {
			c := newMajorBlocksEmptyCollector(t, 16383, false)
			c.tenuringThreshold = 1
			if mode == "immediate" {
				c.survivorBytes = 0
			}
			if mode == "survivor" {
				c.tenuringThreshold = 2
			}
			if !c.PrepareNativeStructHandles() {
				t.Fatal("native reservation failed")
			}
			roots := publishMajorBlocksNative(c, 2)
			if mode != "dense" {
				roots = roots[:1]
			}
			if err := c.CollectMinor(roots); err != nil {
				t.Fatal(err)
			}
			h := uint32(roots[0]) >> 1
			want := spaceOld
			if mode == "survivor" {
				want = spaceNursery
			}
			if c.handles[h].space != want {
				t.Fatalf("%s did not reach intended space: %v", mode, c.handles[h].space)
			}
			if mode != "survivor" && c.majorBlocks[h>>majorWordShift]&(uint64(1)<<((h>>majorBlockShift)&63)) == 0 {
				t.Fatal("promotion removed an unindexed native object from nursery ledger")
			}
			if err := c.CollectFull(EmptyRoots{}); err != nil {
				t.Fatal(err)
			}
			if len(c.majorBlocks) == 0 || c.majorYoungDirty || c.Stats().LiveObjects != 0 {
				t.Fatal("deferred native object was not reclaimed by indexed major")
			}
		})
	}
}

func TestMajorBlocksNativeRefillFlatThenSparse(t *testing.T) {
	c := newMajorBlocksEmptyCollector(t, 8191, false)
	var root Ref
	for i := 0; i < 8; i++ {
		if !c.PrepareNativeStructHandles() {
			t.Fatal("native refill failed")
		}
		roots := publishMajorBlocksNative(c, 512)
		root = roots[len(roots)-1]
	}
	if len(c.freeHandles) >= len(c.handles)/2 {
		t.Fatal("fixture did not refill table")
	}
	if err := c.CollectFull(RefSliceRoots{root}); err != nil {
		t.Fatal(err)
	}
	if len(c.majorBlocks) == 0 || !c.majorYoungDirty || c.Stats().LiveObjects != 1 {
		t.Fatal("flat sweep lost pending nursery coverage")
	}
	if err := c.CollectFull(RefSliceRoots{root}); err != nil {
		t.Fatal(err)
	}
	if c.majorYoungDirty {
		t.Fatal("indexed sweep did not cover dirty nursery")
	}
	if err := c.CollectFull(EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	if c.Stats().LiveObjects != 0 {
		t.Fatal("native survivor was missing after flat-to-indexed transition")
	}
}
