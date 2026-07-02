package gc

import "testing"

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

		before := c.tiny.blocks[0]
		off, span, err := c.tiny.alloc(rawSize)
		if err != nil {
			if c.tiny.freeHead != 0 || c.tiny.blocks[0] != before {
				t.Fatalf("failed raw tiny allocation corrupted metadata: size=%d head=%d before=%+v after=%+v", rawSize, c.tiny.freeHead, before, c.tiny.blocks[0])
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
