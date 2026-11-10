//go:build amd64 || arm64

package gc

import (
	"testing"
	"unsafe"
)

func TestSparseScanPlanUsesDescriptorPadding(t *testing.T) {
	if unsafe.Sizeof(FieldDesc{}) != 8 || unsafe.Sizeof(TypeDesc{}) != 64 {
		t.Fatal("scan plan enlarged descriptors")
	}
	for _, count := range []int{8, 64, 65535, 65536} {
		fields := make([]StorageKind, count)
		for i := range fields {
			fields[i] = StorageI64
		}
		fields[1], fields[count-1] = StorageRefNull, StorageRefNull
		d, err := NewStructDesc(0, fields)
		if err != nil {
			t.Fatal(err)
		}
		if count <= 65535 {
			if d.firstRef != 2 || d.Fields[1].nextRef != uint16(count) || d.Fields[count-1].nextRef != 0 {
				t.Fatalf("%d fields: invalid scan links", count)
			}
		} else if d.firstRef != 0 {
			t.Fatal("oversized struct did not use linear scan")
		}
	}
}

func TestSparseScanPlanDiscardsChangedDescriptorCache(t *testing.T) {
	fields := make([]StorageKind, 64)
	for i := range fields {
		fields[i] = StorageI64
	}
	fields[63] = StorageRefNull
	d, err := NewStructDesc(0, fields)
	if err != nil {
		t.Fatal(err)
	}
	// The caller changes an otherwise valid layout before collector admission.
	// The cache no longer covers every reference field.
	d.Fields[1].Kind = StorageRefNull
	for _, appendTypes := range []bool{false, true} {
		var initial []TypeDesc
		if !appendTypes {
			initial = []TypeDesc{d}
		}
		c, err := NewCollector(Config{}, initial)
		if err != nil {
			t.Fatal(err)
		}
		if appendTypes {
			if err := c.AddTypes([]TypeDesc{d}); err != nil {
				t.Fatal(err)
			}
		}
		if c.types[0].firstRef != 0 {
			t.Fatal("stale scan plan accepted")
		}
		parent, err := c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		child, err := c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.StructSet(parent, 1, RefValue(child)); err != nil {
			t.Fatal(err)
		}
		if err := c.CollectFull(RefSliceRoots{parent}); err != nil {
			t.Fatal(err)
		}
		if c.Stats().LiveObjects != 2 {
			t.Fatal("reference omitted by stale scan plan")
		}
		c.Close()
	}
}

func TestSparseScanPlanKeepsAllLinkedChildren(t *testing.T) {
	fields := make([]StorageKind, 64)
	for i := range fields {
		fields[i] = StorageI64
	}
	indices := []uint32{1, 17, 31, 63}
	for _, i := range indices {
		fields[i] = StorageRefNull
	}
	d, err := NewStructDesc(0, fields)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{}, []TypeDesc{d})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.types[0].firstRef == 0 {
		t.Fatal("sparse scan plan was not installed")
	}
	parent, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range indices {
		child, err := c.NewStructDefault(0)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.StructSet(parent, i, RefValue(child)); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.CollectMinor(RefSliceRoots{parent}); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 2; n++ {
		if err := c.CollectFull(RefSliceRoots{parent}); err != nil {
			t.Fatal(err)
		}
		if got := c.Stats().LiveObjects; got != uint32(1+len(indices)) {
			t.Fatalf("live objects = %d, want %d", got, 1+len(indices))
		}
	}
}

func TestSparseScanPlanRememberedParent(t *testing.T) {
	fields := make([]StorageKind, 64)
	for i := range fields {
		fields[i] = StorageI64
	}
	fields[63] = StorageRefNull
	d, err := NewStructDesc(0, fields)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{VerifyAfterCollect: true}, []TypeDesc{d})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	parent, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ForcePromote(parent); err != nil {
		t.Fatal(err)
	}
	child, err := c.NewStructDefault(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.StructSet(parent, 63, RefValue(child)); err != nil {
		t.Fatal(err)
	}
	// Exercise the complete fallback scan rather than the separate card-range loop.
	c.cardFallback = true
	if err := c.CollectMinor(RefSliceRoots{parent}); err != nil {
		t.Fatal(err)
	}
	if !c.validObjectRef(child) || c.Stats().LiveObjects != 2 {
		t.Fatal("lost nursery child")
	}
}

func TestSparseScanPlanAdmissionAndFallback(t *testing.T) {
	for _, count := range []int{7, 8, 12} {
		for _, refs := range []int{1, 2, 3, 4} {
			fields := make([]StorageKind, count)
			for i := range fields {
				fields[i] = StorageI64
			}
			for i := 0; i < refs; i++ {
				fields[i] = StorageRefNull
			}
			d, err := NewStructDesc(0, fields)
			if err != nil {
				t.Fatal(err)
			}
			want := count >= 8 && refs*4 <= count
			if (d.firstRef != 0) != want {
				t.Fatalf("count=%d refs=%d cache=%d", count, refs, d.firstRef)
			}
			if !want {
				continue
			}
			c, err := NewCollector(Config{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := c.AddTypes([]TypeDesc{d}); err != nil {
				t.Fatal(err)
			}
			if c.types[0].firstRef == 0 {
				t.Fatal("valid cache discarded")
			}
			c.Close()
			// Changing the last reference to a scalar must invalidate the cached tail.
			d.Fields[refs-1].Kind = StorageI64
			d.checkScanPlan()
			if d.firstRef != 0 {
				t.Fatal("stale tail accepted")
			}
		}
	}
}
