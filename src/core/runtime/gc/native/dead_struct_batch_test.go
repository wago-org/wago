package gc

import "testing"

func TestReserveDeadStructAllocationsMatchesBoundedSequence(t *testing.T) {
	desc, err := NewStructDesc(0, []StorageKind{StorageI32, StorageI32})
	if err != nil {
		t.Fatal(err)
	}
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
			if _, wantErr = sequential.ReserveDeadStructAllocation(0, EmptyRoots{}); wantErr != nil {
				break
			}
		}
		gotErr := batched.ReserveDeadStructAllocations(0, 1000, EmptyRoots{})
		if (gotErr == nil) != (wantErr == nil) {
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

func TestReserveDeadStructAllocationsPreservesRoots(t *testing.T) {
	desc, err := NewStructDesc(0, []StorageKind{StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{StressNurseryBytes: 128, VerifyAfterCollect: true}, []TypeDesc{desc})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ref, err := c.NewStructWithRoots(0, []Value{I32Value(987)}, EmptyRoots{})
	if err != nil {
		t.Fatal(err)
	}
	roots := RefSliceRoots{ref}
	for i := 0; i < 3; i++ {
		if err := c.ReserveDeadStructAllocations(0, 1000, roots); err != nil {
			t.Fatal(err)
		}
		value, err := c.StructGet(roots[0], 0)
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
