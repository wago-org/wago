package gc

import "testing"

func TestCollectorAddTypesPreservesLiveObjects(t *testing.T) {
	base, err := NewStructDesc(0, []StorageKind{StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	added, err := NewArrayDesc(1, StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	for _, cfg := range []Config{{ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096}, {Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}} {
		c, err := NewCollector(cfg, []TypeDesc{base})
		if err != nil {
			t.Fatal(err)
		}
		ref, err := c.NewStructDefaultWithRoots(0, EmptyRoots{})
		if err != nil {
			c.Close()
			t.Fatal(err)
		}
		if err := c.AddTypes([]TypeDesc{added}); err != nil {
			c.Close()
			t.Fatal(err)
		}
		if typ, err := c.ObjectType(ref); err != nil || typ != 0 {
			c.Close()
			t.Fatalf("pre-existing object type = %d, %v", typ, err)
		}
		if _, err := c.NewArrayDefaultWithRoots(1, 1, EmptyRoots{}); err != nil {
			c.Close()
			t.Fatal(err)
		}
		c.Close()
	}
}

func TestCollectorAddTypesRejectsDuplicateAndAlignmentGrowth(t *testing.T) {
	base, err := NewStructDesc(0, []StorageKind{StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{Profile: ProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 8}, []TypeDesc{base})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.AddTypes([]TypeDesc{base}); err == nil {
		t.Fatal("duplicate appended type ID accepted")
	}
	vec, err := NewArrayDesc(1, StorageV128)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.AddTypes([]TypeDesc{vec}); err == nil {
		t.Fatal("v128 type admitted into 8-byte Tiny blocks")
	}
}
