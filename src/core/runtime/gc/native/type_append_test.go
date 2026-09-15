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

func TestWasmtimeIssue13417TypeMetadataIsolation(t *testing.T) {
	oldFields := make([]StorageKind, 17)
	for i := 0; i < 16; i++ {
		oldFields[i] = StorageI64
	}
	oldFields[16] = StorageRefNull
	oldType, err := NewStructDesc(0, oldFields)
	if err != nil {
		t.Fatal(err)
	}
	newType, err := NewStructDesc(1, []StorageKind{StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	configs := []Config{
		{ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096},
		{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 256},
	}
	for _, cfg := range configs {
		c, err := NewCollector(cfg, []TypeDesc{oldType})
		if err != nil {
			t.Fatal(err)
		}
		oldObject, err := c.NewStructDefaultWithRoots(0, EmptyRoots{})
		if err != nil {
			c.Close()
			t.Fatal(err)
		}
		root := Root(oldObject)
		if err := c.CollectFull(Slots{&root}); err != nil {
			c.Close()
			t.Fatal(err)
		}
		if err := c.AddTypes([]TypeDesc{newType}); err != nil {
			c.Close()
			t.Fatal(err)
		}
		root = Root(Null())
		if err := c.CollectFull(Slots{&root}); err != nil {
			c.Close()
			t.Fatal(err)
		}
		newObject, err := c.NewStructDefaultWithRoots(1, EmptyRoots{})
		if err != nil {
			c.Close()
			t.Fatal(err)
		}
		root = Root(newObject)
		if err := c.CollectFull(Slots{&root}); err != nil {
			c.Close()
			t.Fatal(err)
		}
		newObject = Ref(root)
		if typ, err := c.ObjectType(newObject); err != nil || typ != 1 {
			c.Close()
			t.Fatalf("replacement object type = %d, %v; want 1", typ, err)
		}
		if _, err := c.StructGet(newObject, 0); err != nil {
			c.Close()
			t.Fatalf("replacement object inherited stale large-struct trace metadata: %v", err)
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
