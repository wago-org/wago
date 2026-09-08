package gc

import "testing"

func BenchmarkGCReferenceStoreBarrier(b *testing.B) {
	field, err := NewStructDesc(0, []StorageKind{StorageRefNull})
	if err != nil {
		b.Fatal(err)
	}
	leaf, err := NewStructDesc(1, nil)
	if err != nil {
		b.Fatal(err)
	}
	newCollector := func(b *testing.B) *Collector {
		c, err := NewCollector(Config{NurseryBytes: 1 << 20}, []TypeDesc{field, leaf})
		if err != nil {
			b.Fatal(err)
		}
		b.Cleanup(c.Close)
		return c
	}
	b.Run("nursery-parent", func(b *testing.B) {
		c := newCollector(b)
		parent, _ := c.NewStructDefault(0)
		child, _ := c.NewStructDefault(1)
		value := RefValue(child)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := c.StructSet(parent, 0, value); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("old-parent-old-child", func(b *testing.B) {
		c := newCollector(b)
		parent, _ := c.NewStructDefault(0)
		child, _ := c.NewStructDefault(1)
		if err := c.ForcePromote(parent); err != nil {
			b.Fatal(err)
		}
		if err := c.ForcePromote(child); err != nil {
			b.Fatal(err)
		}
		value := RefValue(child)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := c.StructSet(parent, 0, value); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("old-parent-young-same-card", func(b *testing.B) {
		c := newCollector(b)
		parent, _ := c.NewStructDefault(0)
		child, _ := c.NewStructDefault(1)
		if err := c.ForcePromote(parent); err != nil {
			b.Fatal(err)
		}
		value := RefValue(child)
		if err := c.StructSet(parent, 0, value); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := c.StructSet(parent, 0, value); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkGCArrayReferenceStoreBarrier(b *testing.B) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		b.Fatal(err)
	}
	refs, err := NewArrayDesc(1, StorageRefNull)
	if err != nil {
		b.Fatal(err)
	}
	c, err := NewCollector(Config{NurseryBytes: 1 << 20}, []TypeDesc{leaf, refs})
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	array, err := c.NewArrayDefault(1, 128)
	if err != nil {
		b.Fatal(err)
	}
	if err := c.ForcePromote(array); err != nil {
		b.Fatal(err)
	}
	child, err := c.NewStructDefault(0)
	if err != nil {
		b.Fatal(err)
	}
	value := RefValue(child)
	if err := c.ArraySet(array, 17, value); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := c.ArraySet(array, 17, value); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGCArrayReferenceStoreBarrierNonHeadCard(b *testing.B) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		b.Fatal(err)
	}
	refs, err := NewArrayDesc(1, StorageRefNull)
	if err != nil {
		b.Fatal(err)
	}
	c, err := NewCollector(Config{NurseryBytes: 1 << 20}, []TypeDesc{leaf, refs})
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	array, err := c.NewArrayDefault(1, 65)
	if err != nil {
		b.Fatal(err)
	}
	if err := c.ForcePromote(array); err != nil {
		b.Fatal(err)
	}
	child, err := c.NewStructDefault(0)
	if err != nil {
		b.Fatal(err)
	}
	value := RefValue(child)
	if err := c.ArraySet(array, 0, value); err != nil {
		b.Fatal(err)
	}
	if err := c.ArraySet(array, 64, value); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := c.ArraySet(array, 0, value); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGCPersistentRootBarrier(b *testing.B) {
	leaf, err := NewStructDesc(0, nil)
	if err != nil {
		b.Fatal(err)
	}
	c, err := NewCollector(Config{NurseryBytes: 1 << 20}, []TypeDesc{leaf})
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	child, err := c.NewStructDefault(0)
	if err != nil {
		b.Fatal(err)
	}
	slot := c.NewGlobalSlot(Null())
	if err := c.SetGlobalSlot(slot, child); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := c.SetGlobalSlot(slot, child); err != nil {
			b.Fatal(err)
		}
	}
}
