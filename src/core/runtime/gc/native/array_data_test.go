//go:build amd64

package gc

import (
	"bytes"
	"testing"
)

func TestArrayDataSingleInitialization(t *testing.T) {
	for _, kind := range []StorageKind{StorageI8, StorageI16, StorageI32, StorageI64, StorageF32, StorageF64, StorageV128} {
		d, err := NewArrayDesc(0, kind)
		if err != nil {
			t.Fatal(err)
		}
		for _, profile := range []Profile{ProfileThroughput, ProfileTiny} {
			for _, poison := range []bool{false, true} {
				c, err := NewCollector(Config{Profile: profile, PoisonFreed: poison, TinyHeapBytes: 4096, TinyBlockBytes: 16, CollectEveryAlloc: true, VerifyAfterCollect: true}, []TypeDesc{d})
				if err != nil {
					t.Fatal(err)
				}
				source := make([]byte, 512)
				for i := range source {
					source[i] = byte(i*17 + 3)
				}
				roots := ClassifiedRoots{}
				for _, n := range []uint32{17, 1, 0, 7, 3, 17} {
					ref, err := c.NewArrayDataWithRoots(0, n, source, 3, roots)
					if err != nil {
						t.Fatal(err)
					}
					payload := c.bytes(ref)[PayloadOffset:]
					count := n * d.ElemSize
					if !bytes.Equal(payload[:count], source[3:3+count]) {
						t.Fatalf("kind%d profile%d length%d payload mismatch", kind, profile, n)
					}
					for _, v := range payload[count:] {
						if v != 0 {
							t.Fatalf("alignment padding not cleared: %x", payload[count:])
						}
					}
				}
				before := c.Stats().Allocations
				if _, err := c.NewArrayDataWithRoots(0, 1, source, uint32(len(source)), roots); err == nil {
					t.Fatal("accepted out of bounds source")
				}
				if _, err := c.NewArrayDataWithRoots(0, ^uint32(0), source, 0, roots); err == nil {
					t.Fatal("accepted oversized array")
				}
				if c.Stats().Allocations != before {
					t.Fatal("invalid source allocated")
				}
				c.Close()
			}
		}
	}
}

func TestArrayDataRejectsReferenceStorage(t *testing.T) {
	for _, kind := range []StorageKind{StorageRef, StorageRefNull, StorageFuncRef, StorageFuncRefNull, StorageExternRef, StorageExternRefNull} {
		d, err := NewArrayDesc(0, kind)
		if err != nil {
			t.Fatal(err)
		}
		c, err := NewCollector(Config{}, []TypeDesc{d})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.NewArrayDataWithRoots(0, 0, nil, 0, nil); err == nil {
			t.Fatalf("accepted reference storage %d", kind)
		}
		if c.Stats().Allocations != 0 {
			t.Fatal("reference array allocated")
		}
		c.Close()
	}
}

func TestArrayDataClearsThroughputClassPadding(t *testing.T) {
	d, err := NewArrayDesc(0, StorageI8)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{NurseryBytes: 1024, LargeObjectBytes: 32, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096, CollectEveryAlloc: true, PoisonFreed: true}, []TypeDesc{d})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	data := bytes.Repeat([]byte{0xa5}, 33)
	for _, n := range []uint32{33, 17, 33, 17} {
		ref, err := c.NewArrayDataWithRoots(0, n, data, 0, EmptyRoots{})
		if err != nil {
			t.Fatal(err)
		}
		e := c.entry(ref)
		if e.space != spaceLarge {
			t.Fatalf("space=%d, want large", e.space)
		}
		limit := e.off + c.throughput.handleAllocSize(*e)
		for _, v := range c.throughput.mem[e.off+PayloadOffset+n : limit] {
			if v != 0 {
				t.Fatal("stale size-class padding")
			}
		}
	}
}
