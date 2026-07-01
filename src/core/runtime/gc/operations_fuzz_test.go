package gc

import (
	"strings"
	"testing"
)

func FuzzCollectorOperations(f *testing.F) {
	// Seeds cover the large-parent remembered-set regression for both large
	// structs and large ref arrays: allocate a nursery child, allocate a large
	// parent, store the child, run minor collection, then verify heap metadata.
	f.Add([]byte{0, 0, 0, 2, 0, 0, 4, 1, 0, 6, 0, 0, 11, 0, 0})
	f.Add([]byte{0, 0, 0, 3, 0, 0, 5, 1, 0, 6, 0, 0, 11, 0, 0})
	f.Add([]byte{0, 0, 0, 1, 0, 0, 4, 1, 0, 8, 1, 0, 6, 0, 0, 7, 0, 0, 11, 0, 0})
	promotionFailureSeed := []byte{0x80, 0, 0}
	for i := 0; i < 48; i++ {
		promotionFailureSeed = append(promotionFailureSeed, 2, 0, 0)
	}
	promotionFailureSeed = append(promotionFailureSeed, 10, 0, 0, 11, 0, 0)
	f.Add(promotionFailureSeed)

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 384 {
			data = data[:384]
		}
		types := fuzzCollectorTypes(t)
		cfg := Config{
			NurseryBytes:        256,
			LargeObjectBytes:    64,
			ThroughputHeapBytes: 1 << 20,
			VerifyAfterCollect:  true,
		}
		if len(data) != 0 && data[0]&0x80 != 0 {
			cfg.NurseryBytes = 8192
			cfg.LargeObjectBytes = 1024
			cfg.ThroughputHeapBytes = 4096
			cfg.ThroughputPageBytes = 4096
		}
		c, err := NewCollector(cfg, types)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()

		var refs []Ref
		var roots RefSliceRoots
		rootSet := func() RootSet {
			roots = pruneLiveRefs(c, roots)
			if len(roots) == 0 {
				return nil
			}
			return roots
		}

		for pc := 0; pc+2 < len(data); pc += 3 {
			op, a, b := data[pc]%12, data[pc+1], data[pc+2]
			switch op {
			case 0:
				if r, err := c.NewStructDefaultWithRoots(0, rootSet()); err == nil {
					refs = append(refs, r)
				}
			case 1:
				if r, err := c.NewStructDefaultWithRoots(1, rootSet()); err == nil {
					refs = append(refs, r)
				}
			case 2:
				if r, err := c.NewStructDefaultWithRoots(2, rootSet()); err == nil {
					refs = append(refs, r)
				}
			case 3:
				if r, err := c.NewArrayDefaultWithRoots(3, 16, rootSet()); err == nil {
					refs = append(refs, r)
				}
			case 4:
				parent, child, ok := fuzzPickTwo(c, refs, a, b)
				if ok {
					_ = c.StructSet(parent, uint32(b)%20, RefValue(child))
				}
			case 5:
				parent, child, ok := fuzzPickTwo(c, refs, a, b)
				if ok {
					if ln, err := c.ArrayLen(parent); err == nil && ln != 0 {
						_ = c.ArraySet(parent, uint32(a+b)%ln, RefValue(child))
					}
				}
			case 6:
				fuzzCollectMinor(t, c, rootSet())
			case 7:
				if err := c.CollectFull(rootSet()); err != nil {
					t.Fatal(err)
				}
			case 8:
				if r, ok := fuzzPick(c, refs, a); ok && len(roots) < 16 {
					roots = append(roots, r)
				}
			case 9:
				if len(roots) != 0 {
					idx := int(a) % len(roots)
					roots = append(roots[:idx], roots[idx+1:]...)
				}
			case 10:
				allRefs := RefSliceRoots(pruneLiveRefs(c, append([]Ref(nil), refs...)))
				fuzzCollectMinor(t, c, allRefs)
			case 11:
				if err := c.Verify(rootSet()); err != nil {
					t.Fatal(err)
				}
			}
			refs = pruneLiveRefs(c, refs)
			if err := c.Verify(rootSet()); err != nil {
				t.Fatal(err)
			}
		}
	})
}

func fuzzCollectorTypes(t *testing.T) []TypeDesc {
	t.Helper()
	child, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := NewStructDesc(1, []StorageKind{StorageRefNull, StorageRefNull})
	if err != nil {
		t.Fatal(err)
	}
	largeFields := make([]StorageKind, 20)
	for i := range largeFields {
		largeFields[i] = StorageRefNull
	}
	largeStruct, err := NewStructDesc(2, largeFields)
	if err != nil {
		t.Fatal(err)
	}
	largeArray, err := NewArrayDesc(3, StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	return []TypeDesc{child, pair, largeStruct, largeArray}
}

func fuzzCollectMinor(t *testing.T, c *Collector, roots RootSet) {
	t.Helper()
	if err := c.CollectMinor(roots); err != nil {
		if !strings.Contains(err.Error(), "throughput heap exhausted") {
			t.Fatal(err)
		}
		if err := c.Verify(roots); err != nil {
			t.Fatalf("heap inconsistent after expected promotion failure: %v", err)
		}
	}
}

func pruneLiveRefs(c *Collector, refs []Ref) []Ref {
	out := refs[:0]
	for _, r := range refs {
		if r.IsObj() && c.validObjectRef(r) {
			out = append(out, r)
		}
	}
	return out
}

func fuzzPick(c *Collector, refs []Ref, idx byte) (Ref, bool) {
	refs = pruneLiveRefs(c, refs)
	if len(refs) == 0 {
		return Null(), false
	}
	r := refs[int(idx)%len(refs)]
	return r, true
}

func fuzzPickTwo(c *Collector, refs []Ref, a, b byte) (Ref, Ref, bool) {
	refs = pruneLiveRefs(c, refs)
	if len(refs) == 0 {
		return Null(), Null(), false
	}
	parent := refs[int(a)%len(refs)]
	child := refs[int(b)%len(refs)]
	return parent, child, true
}
