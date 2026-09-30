package gc

import "testing"

func FuzzTinySpanAllocator(f *testing.F) {
	for _, seed := range [][]byte{
		{1, 2, 3, 0x80, 4, 0x81},
		{64, 64, 64, 64, 0x80, 0x81, 127},
		{1, 1, 1, 1, 0x82, 2, 0x80, 3},
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, operations []byte) {
		if len(operations) > 256 {
			operations = operations[:256]
		}
		const blocks = uint32(257)
		h := newTinyHeap(make([]byte, blocks*16), blocks, 16, false)
		live := make([]tinyTestAllocation, 0, blocks)
		for step, op := range operations {
			if op&0x80 != 0 {
				if len(live) != 0 {
					i := int(op&0x7f) % len(live)
					if err := h.free(live[i].off * h.blockBytes); err != nil {
						t.Fatalf("step %d free %+v: %v", step, live[i], err)
					}
					live[i] = live[len(live)-1]
					live = live[:len(live)-1]
				}
			} else {
				need := uint32(op%96) + 1
				offBytes, spanBytes, err := h.alloc(need * h.blockBytes)
				if err != nil {
					if maxFreeGap(live, blocks) >= need {
						t.Fatalf("step %d rejected %d blocks with a fitting interval", step, need)
					}
				} else {
					a := tinyTestAllocation{off: offBytes / h.blockBytes, blocks: spanBytes / h.blockBytes}
					for _, other := range live {
						if a.off < other.off+other.blocks && other.off < a.off+a.blocks {
							t.Fatalf("step %d overlap: %+v and %+v", step, a, other)
						}
					}
					live = append(live, a)
				}
			}
			assertTinyHeapMetadata(t, &h)
		}
	})
}

func FuzzTinyAllocationBounds(f *testing.F) {
	maxRounded := ^uint32(0) - 7
	maxI8ArrayLength := ^uint32(0) - HeaderSize - 7
	for _, seed := range []struct {
		length uint32
		size   uint32
	}{
		{0, 0},
		{1, HeaderSize},
		{15, 15},
		{16, 16},
		{112, 128},
		{113, 129},
		{maxI8ArrayLength - 1, maxRounded - 8},
		{maxI8ArrayLength, maxRounded},
		{maxI8ArrayLength + 1, maxRounded + 1},
		{^uint32(0), ^uint32(0)},
	} {
		f.Add(seed.length, seed.size)
	}

	f.Fuzz(func(t *testing.T, length uint32, rawSize uint32) {
		types := testTypes(t)
		i8, err := NewArrayDesc(4, StorageI8)
		if err != nil {
			t.Fatal(err)
		}
		types = append(types, i8)

		c, err := NewCollector(Config{Profile: ProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 16, VerifyAfterCollect: true}, types)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()

		beforeSize, beforeSummary := c.tiny.spanSize(0), c.tiny.binSummary
		off, span, err := c.tiny.alloc(rawSize)
		if err != nil {
			if c.tiny.findFreeSpan(8) != 0 || c.tiny.spanSize(0) != beforeSize || c.tiny.binSummary != beforeSummary {
				t.Fatalf("failed raw tiny allocation corrupted metadata: size=%d start=%d before=%d after=%d", rawSize, c.tiny.findFreeSpan(8), beforeSize, c.tiny.spanSize(0))
			}
		} else {
			if span == 0 || off+span > uint32(len(c.tiny.mem)) {
				t.Fatalf("raw tiny allocation returned invalid extent: size=%d off=%d span=%d", rawSize, off, span)
			}
			if err := c.tiny.free(off); err != nil {
				t.Fatalf("freeing raw tiny allocation failed: %v", err)
			}
		}

		ref, err := c.NewArrayDefault(4, length)
		if err != nil {
			if len(c.handles) != 1 {
				t.Fatalf("failed tiny array allocation leaked handle: length=%d handles=%d", length, len(c.handles))
			}
			if err := c.Verify(nil); err != nil {
				t.Fatalf("heap did not verify after failed tiny array allocation length=%d: %v", length, err)
			}
			return
		}
		if c.entry(ref).space != spaceTiny || c.entry(ref).size == 0 {
			t.Fatalf("tiny array allocation returned invalid entry: length=%d entry=%+v", length, c.entry(ref))
		}
		root := Root(ref)
		if err := c.Verify(Slots{&root}); err != nil {
			t.Fatalf("heap did not verify after tiny array allocation length=%d: %v", length, err)
		}
	})
}
