package gc

import "testing"

func TestReserveDeadArrayAllocationsMatchesBoundedSequence(t *testing.T) {
	desc, err := NewArrayDesc(0, StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	for _, length := range []uint32{0, 1, 16, 64, 2048} {
		for _, cfg := range []Config{
			{StressNurseryBytes: 128, VerifyAfterCollect: true},
			{StressNurseryBytes: 128, CollectEveryAlloc: true, VerifyAfterCollect: true},
			{Profile: ProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, VerifyAfterCollect: true},
			{DisableCollection: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096},
			{Profile: ProfileTiny, TinyHeapBytes: 16, TinyBlockBytes: 16},
		} {
			sequential, err := NewCollector(cfg, []TypeDesc{desc})
			if err != nil {
				t.Fatal(err)
			}
			batched, err := NewCollector(cfg, []TypeDesc{desc})
			if err != nil {
				t.Fatal(err)
			}
			var wantErr error
			for i := 0; i < 1000; i++ {
				if _, wantErr = sequential.ReserveDeadArrayAllocation(0, length, EmptyRoots{}); wantErr != nil {
					break
				}
			}
			gotErr := batched.ReserveDeadArrayAllocations(0, length, 1000, EmptyRoots{})
			if (gotErr == nil) != (wantErr == nil) || gotErr != nil && gotErr.Error() != wantErr.Error() {
				t.Fatalf("config %+v: batch error=%v sequence=%v", cfg, gotErr, wantErr)
			}
			a, b := sequential.Stats(), batched.Stats()
			if a.Allocations != b.Allocations || a.MinorCollections+a.FullCollections != b.MinorCollections+b.FullCollections || (!failureInjectionEnabled && (a.MinorCollections != b.MinorCollections || a.FullCollections != b.FullCollections)) || a.LiveObjects != b.LiveObjects {
				t.Fatalf("config %+v: sequence %+v batch %+v", cfg, a, b)
			}
			if err := batched.Verify(EmptyRoots{}); err != nil {
				t.Fatal(err)
			}
			sequential.Close()
			batched.Close()
		}
	}
}

func TestReserveDeadArrayAllocationsPreservesRoots(t *testing.T) {
	desc, err := NewArrayDesc(0, StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{StressNurseryBytes: 128, VerifyAfterCollect: true}, []TypeDesc{desc})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ref, err := c.NewArrayWithRoots(0, 1, I32Value(987), EmptyRoots{})
	if err != nil {
		t.Fatal(err)
	}
	roots := RefSliceRoots{ref}
	for i := 0; i < 3; i++ {
		if err := c.ReserveDeadArrayAllocations(0, 16, 1000, roots); err != nil {
			t.Fatal(err)
		}
		value, err := c.ArrayGet(roots[0], 0)
		if err != nil || value.I32() != 987 {
			t.Fatalf("root value=%+v error=%v", value, err)
		}
		if err := c.Verify(roots); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.CollectFull(roots); err != nil {
		t.Fatal(err)
	}
	if c.Stats().LiveObjects != 1 {
		t.Fatalf("live objects=%d", c.Stats().LiveObjects)
	}
}

func TestReserveDeadArrayAllocationsRejectsUnprovenStorage(t *testing.T) {
	numeric, err := NewArrayDesc(0, StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	references, err := NewArrayDesc(1, StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{}, []TypeDesc{numeric, references})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, tc := range []struct {
		typ    TypeID
		length uint32
	}{{0, ^uint32(0)}, {1, 16}, {2, 16}} {
		if err := c.ReserveDeadArrayAllocations(tc.typ, tc.length, 10, EmptyRoots{}); err == nil {
			t.Fatalf("accepted invalid batch: %+v", tc)
		}
		if c.Stats().Allocations != 0 {
			t.Fatal("failed validation changed allocation count")
		}
	}
	if err := c.ReserveDeadArrayAllocations(0, 16, 0, EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	if c.Stats().Allocations != 0 {
		t.Fatal("empty batch allocated")
	}
}
