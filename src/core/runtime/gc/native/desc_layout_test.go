package gc

import (
	"errors"
	"strings"
	"testing"
)

func TestCheckArrayAllocationPreservesDeterministicCapacityAndOverflowTraps(t *testing.T) {
	wide, err := NewArrayDesc(0, StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	throughput, err := NewCollector(Config{ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096}, []TypeDesc{wide})
	if err != nil {
		t.Fatal(err)
	}
	defer throughput.Close()
	if err := throughput.CheckArrayAllocation(0, 2000); err != errThroughputHeapExhausted {
		t.Fatalf("throughput impossible array = %v, want %v", err, errThroughputHeapExhausted)
	}
	if err := throughput.CheckArrayAllocation(0, 1_073_741_817); !errors.Is(err, ErrAllocationTooLarge) {
		t.Fatalf("throughput physical overflow = %v, want ErrAllocationTooLarge", err)
	}

	tiny, err := NewCollector(Config{Profile: ProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 16}, []TypeDesc{wide})
	if err != nil {
		t.Fatal(err)
	}
	defer tiny.Close()
	if err := tiny.CheckArrayAllocation(0, 100); err != errTinyHeapExhausted {
		t.Fatalf("tiny impossible array = %v, want %v", err, errTinyHeapExhausted)
	}
}

func TestReserveDeadArrayAllocationUsesCurrentBoundedHeapState(t *testing.T) {
	d, err := NewArrayDesc(0, StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		config Config
		first  uint32
		next   uint32
	}{
		{name: "throughput", config: Config{DisableCollection: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096}, first: 600, next: 600},
		{name: "tiny", config: Config{Profile: ProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 16}, first: 24, next: 40},
	} {
		t.Run(tc.name, func(t *testing.T) {
			makeCollector := func() (*Collector, RefSliceRoots) {
				c, err := NewCollector(tc.config, []TypeDesc{d})
				if err != nil {
					t.Fatal(err)
				}
				first, err := c.NewArrayDefault(0, tc.first)
				if err != nil {
					c.Close()
					t.Fatalf("occupy heap: %v", err)
				}
				return c, RefSliceRoots{first}
			}
			actual, actualRoots := makeCollector()
			defer actual.Close()
			reserved, reserveRoots := makeCollector()
			defer reserved.Close()

			if err := reserved.CheckArrayAllocation(0, tc.next); err != nil {
				t.Fatalf("size-only check unexpectedly failed: %v", err)
			}
			_, actualErr := actual.NewArrayDefaultWithRoots(0, tc.next, actualRoots)
			_, reserveErr := reserved.ReserveDeadDefaultArrayAllocation(0, tc.next, reserveRoots)
			if (actualErr == nil) != (reserveErr == nil) {
				t.Fatalf("allocation/reservation outcome mismatch: actual=%v reserve=%v", actualErr, reserveErr)
			}
			actualStats, reserveStats := actual.Stats(), reserved.Stats()
			if actualStats.Allocations != reserveStats.Allocations || actualStats.LiveObjects != reserveStats.LiveObjects {
				t.Fatalf("allocation/reservation stats mismatch: actual=%+v reserve=%+v", actualStats, reserveStats)
			}
		})
	}
}

func TestReserveDeadStructAllocationUsesCurrentBoundedHeapState(t *testing.T) {
	d, err := NewStructDesc(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	makeCollector := func() (*Collector, RefSliceRoots) {
		c, err := NewCollector(Config{Profile: ProfileTiny, TinyHeapBytes: 16, TinyBlockBytes: 16}, []TypeDesc{d})
		if err != nil {
			t.Fatal(err)
		}
		first, err := c.NewStructDefault(0)
		if err != nil {
			c.Close()
			t.Fatalf("occupy heap: %v", err)
		}
		return c, RefSliceRoots{first}
	}
	actual, actualRoots := makeCollector()
	defer actual.Close()
	reserved, reserveRoots := makeCollector()
	defer reserved.Close()

	_, actualErr := actual.NewStructUninitializedWithRoots(0, actualRoots)
	_, reserveErr := reserved.ReserveDeadStructAllocation(0, reserveRoots)
	if (actualErr == nil) != (reserveErr == nil) || (actualErr != nil && actualErr.Error() != reserveErr.Error()) {
		t.Fatalf("allocation/reservation outcome mismatch: actual=%v reserve=%v", actualErr, reserveErr)
	}
	actualStats, reserveStats := actual.Stats(), reserved.Stats()
	if actualStats.Allocations != reserveStats.Allocations || actualStats.LiveObjects != reserveStats.LiveObjects {
		t.Fatalf("allocation/reservation stats mismatch: actual=%+v reserve=%+v", actualStats, reserveStats)
	}
}

func TestDescriptorsAndLayout(t *testing.T) {
	pf, err := NewStructDesc(1, []StorageKind{StorageI32, StorageI64, StorageI8})
	if err != nil {
		t.Fatal(err)
	}
	if pf.HasRefs || !pf.PointerFree() {
		t.Fatal("pointer-free struct has refs")
	}
	if pf.Fields[0].Offset != 0 || pf.Fields[1].Offset != 8 || pf.Fields[2].Offset != 16 || pf.Size != 24 {
		t.Fatalf("bad struct offsets/size: %+v", pf)
	}
	mixed, _ := NewStructDesc(2, []StorageKind{StorageI8, StorageRef, StorageI64, StorageRefNull})
	if !mixed.HasRefs || mixed.PointerFree() {
		t.Fatal("mixed refs not detected")
	}
	off := mixed.StructRefOffsets()
	if len(off) != 2 || off[0] != 4 || off[1] != 16 {
		t.Fatalf("bad ref offsets %v", off)
	}
	arr, _ := NewArrayDesc(3, StorageI16)
	if arr.HasRefs || arr.ElemSize != 2 || arr.Align != 2 {
		t.Fatalf("bad packed array %+v", arr)
	}
	rarr, _ := NewArrayDesc(4, StorageRef)
	if !rarr.ArrayElementsAreRefs() || rarr.ElemSize != 4 {
		t.Fatalf("bad ref array %+v", rarr)
	}
	varr, err := NewArrayDesc(5, StorageV128)
	if err != nil {
		t.Fatal(err)
	}
	if varr.HasRefs || !varr.PointerFree() || varr.ElemSize != 16 || varr.Align != 16 {
		t.Fatalf("bad v128 array %+v", varr)
	}
	vstruct, err := NewStructDesc(6, []StorageKind{StorageI8, StorageV128, StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	if vstruct.Align != 16 || vstruct.Size != 48 || vstruct.Fields[1].Offset != 16 || vstruct.Fields[2].Offset != 32 {
		t.Fatalf("bad v128 struct layout %+v", vstruct)
	}
	sz, _ := StructSize(pf)
	if sz != Align8(HeaderSize+pf.Size) || sz%8 != 0 {
		t.Fatalf("bad struct size %d", sz)
	}
	asz, _ := ArraySize(arr, 3)
	if asz != Align8(HeaderSize+6) || asz%8 != 0 {
		t.Fatalf("bad array size %d", asz)
	}
	if _, err := ArraySize(arr, ^uint32(0)); !errors.Is(err, ErrAllocationTooLarge) {
		t.Fatalf("array size overflow = %v, want ErrAllocationTooLarge", err)
	}
	wide, err := NewArrayDesc(0, StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	collector, err := NewCollector(Config{}, []TypeDesc{wide})
	if err != nil {
		t.Fatal(err)
	}
	defer collector.Close()
	// This payload fits the eight-byte object-size rounding but not the
	// throughput allocator's sixteen-byte physical extent. It previously wrapped
	// Align16 and panicked while publishing the object header.
	if _, err := collector.NewArrayDefault(0, 1_073_741_817); !errors.Is(err, ErrAllocationTooLarge) {
		t.Fatalf("near-u32 array allocation = %v, want ErrAllocationTooLarge", err)
	}
	if got, err := NewStructDesc(5, []StorageKind{StorageI8, StorageRef, StorageI64, StorageRefNull}); err != nil {
		t.Fatalf("mixed layout rejected: %v", err)
	} else if got.Size != 24 || got.Align != 8 || got.Fields[0].Offset != 0 || got.Fields[1].Offset != 4 || got.Fields[2].Offset != 8 || got.Fields[3].Offset != 16 {
		t.Fatalf("mixed layout changed: %+v", got)
	}
	for _, tc := range []struct {
		name   string
		start  uint32
		fields []StorageKind
	}{
		{name: "field align wraps", start: ^uint32(0) - 2, fields: []StorageKind{StorageI64}},
		{name: "field add wraps", start: ^uint32(0) - 1, fields: []StorageKind{StorageI32}},
		{name: "final align wraps", start: ^uint32(0) - 6, fields: []StorageKind{StorageI32, StorageI8}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := newStructDescLayout(99, tc.fields, tc.start); err == nil || !strings.Contains(err.Error(), "struct layout overflow") {
				t.Fatalf("newStructDescLayout err = %v, want struct layout overflow", err)
			}
		})
	}
	overflowStruct := TypeDesc{
		ID:      0,
		Kind:    KindStruct,
		Fields:  []FieldDesc{{Kind: StorageI8, Offset: ^uint32(0) - 1}},
		Size:    ^uint32(0),
		Align:   1,
		Final:   true,
		HasRefs: false,
	}
	if _, err := StructSize(overflowStruct); err == nil || !strings.Contains(err.Error(), "struct size overflow") {
		t.Fatalf("StructSize overflow error = %v, want struct size overflow", err)
	}
	if err := ValidateTypeDescs([]TypeDesc{overflowStruct}); err == nil || !strings.Contains(err.Error(), "struct size overflow") {
		t.Fatalf("ValidateTypeDescs overflow error = %v, want struct size overflow", err)
	}
	if _, err := NewCollector(Config{}, []TypeDesc{overflowStruct}); err == nil || !strings.Contains(err.Error(), "struct size overflow") {
		t.Fatalf("NewCollector overflow error = %v, want struct size overflow", err)
	}
	if HeaderSize != 16 || PayloadOffset != 16 {
		t.Fatalf("header layout changed: %d %d", HeaderSize, PayloadOffset)
	}
}

func TestStructDescBuilderRequiresExactFieldCount(t *testing.T) {
	b := NewStructDescBuilder(7, 1)
	if _, err := b.Finish(); err == nil || !strings.Contains(err.Error(), "got 0 struct fields, want 1") {
		t.Fatalf("unfinished builder error = %v", err)
	}
	if err := b.Add(StorageI32); err != nil {
		t.Fatal(err)
	}
	if err := b.Add(StorageI64); err == nil || !strings.Contains(err.Error(), "too many struct fields") {
		t.Fatalf("overfilled builder error = %v", err)
	}
	d, err := b.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != 7 || d.Size != 4 || len(d.Fields) != 1 || d.Fields[0] != (FieldDesc{Kind: StorageI32, Offset: 0}) {
		t.Fatalf("built descriptor = %+v", d)
	}
}
