package gc

import (
	"fmt"
	"testing"
)

func TestThroughputDoesNotAllocateTinyColors(t *testing.T) {
	desc, err := NewStructDesc(0, []StorageKind{StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	for _, checked := range []bool{false, true} {
		for _, disabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("generic/checked=%t/disabled=%t", checked, disabled), func(t *testing.T) {
				c, err := NewCollector(Config{Profile: ProfileThroughput, ThroughputHeapBytes: 1 << 20, StressNurseryBytes: 1 << 20, DisableCollection: disabled}, []TypeDesc{desc})
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				if checked {
					if err := c.EnableCheckedHandles(); err != nil {
						t.Fatal(err)
					}
				}
				for i := 0; i < 2048; i++ {
					if _, err := c.NewStructWithRoots(0, []Value{{Kind: StorageI32, Bits: uint64(i)}}, EmptyRoots{}); err != nil {
						t.Fatal(err)
					}
				}
				if len(c.handles) <= 1 {
					t.Fatal("fixture did not grow handles")
				}
				if len(c.tinyGC.color) != 0 || cap(c.tinyGC.color) != 0 {
					t.Fatalf("Throughput retains Tiny color storage: len%d cap%d", len(c.tinyGC.color), cap(c.tinyGC.color))
				}
			})
		}
		for _, batch := range []int{nativeArrayHandleBatch, nativeStructHandleBatch} {
			t.Run(fmt.Sprintf("native/checked=%t/batch=%d", checked, batch), func(t *testing.T) {
				c, err := NewCollector(Config{}, []TypeDesc{desc})
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				if checked {
					if err := c.EnableCheckedHandles(); err != nil {
						t.Fatal(err)
					}
				}
				c.reserveNativeHandles(batch)
				if c.nativeStructAlloc.Count != uint32(batch) || len(c.handles) != batch+1 {
					t.Fatal("native reservation did not complete")
				}
				c.discardNativeStructHandles()
				before := len(c.handles)
				c.reserveNativeHandles(batch)
				if c.nativeStructAlloc.Count != uint32(batch) || len(c.handles) != before {
					t.Fatal("native reservation did not reuse canceled identities")
				}
				if len(c.tinyGC.color) != 0 || cap(c.tinyGC.color) != 0 {
					t.Fatalf("native Throughput retains Tiny color storage: len%d cap%d", len(c.tinyGC.color), cap(c.tinyGC.color))
				}
			})
		}
		t.Run(fmt.Sprintf("large-array/checked=%t", checked), func(t *testing.T) {
			c, err := NewCollector(Config{}, []TypeDesc{desc})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if checked {
				if err := c.EnableCheckedHandles(); err != nil {
					t.Fatal(err)
				}
			}
			if !c.PrepareNativeArrayAllocation(16 << 10) {
				t.Fatal("large reservation did not complete")
			}
			if c.nativeLargeArrayAlloc.Count != 1 || len(c.handles) <= 1 {
				t.Fatal("large array did not reserve a handle")
			}
			if len(c.tinyGC.color) != 0 || cap(c.tinyGC.color) != 0 {
				t.Fatalf("large-array Throughput retains Tiny color storage: len%d cap%d", len(c.tinyGC.color), cap(c.tinyGC.color))
			}
		})
	}
}

func TestTinyColorsStayAlignedAfterFreshAndReusedHandles(t *testing.T) {
	desc, err := NewStructDesc(0, []StorageKind{StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{desc})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.EnableCheckedHandles(); err != nil {
		t.Fatal(err)
	}
	for pass := 0; pass < 3; pass++ {
		r, err := c.NewStructWithRoots(0, []Value{{Kind: StorageI32, Bits: uint64(11 + pass)}}, EmptyRoots{})
		if err != nil {
			t.Fatal(err)
		}
		h := handleOf(r)
		if len(c.tinyGC.color) != len(c.handles) || c.tinyGC.color[h] != tinyMarkState(c.tinyGC.markEpoch) {
			t.Fatal("fresh or reused identity lost its current-epoch color")
		}
		generation := (*c.checkedHandles)[h]
		if generation != uint64(pass+1) {
			t.Fatalf("generation%d want%d", generation, pass+1)
		}
		root := RefSliceRoots{r}
		if err := c.CollectFull(root); err != nil {
			t.Fatal(err)
		}
		value, err := c.StructGet(root[0], 0)
		if err != nil || value.Bits != uint64(11+pass) {
			t.Fatalf("rooted object lost: %+v,%v", value, err)
		}
		if err := c.CollectFull(EmptyRoots{}); err != nil {
			t.Fatal(err)
		}
		if err := c.Verify(EmptyRoots{}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTinyColorsStayAlignedAfterCheckedHandleRetirement(t *testing.T) {
	desc, err := NewStructDesc(0, []StorageKind{StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollector(Config{Profile: ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}, []TypeDesc{desc})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.EnableCheckedHandles(); err != nil {
		t.Fatal(err)
	}
	old, err := c.NewStructWithRoots(0, []Value{{Kind: StorageI32, Bits: 11}}, EmptyRoots{})
	if err != nil {
		t.Fatal(err)
	}
	retired := handleOf(old)
	if err := c.CollectFull(EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	if len(c.freeHandles) != 1 || c.freeHandles[0] != retired {
		t.Fatal("collection did not free the identity for retirement")
	}
	(*c.checkedHandles)[retired] = ^uint64(0)
	beforeHandles := len(c.handles)
	r, err := c.NewStructWithRoots(0, []Value{{Kind: StorageI32, Bits: 29}}, EmptyRoots{})
	if err != nil {
		t.Fatal(err)
	}
	h := handleOf(r)
	if int(h) != beforeHandles || len(c.handles) != beforeHandles+1 || len(c.freeHandles) != 0 {
		t.Fatal("Tiny allocation did not retire the exhausted identity")
	}
	if (*c.checkedHandles)[retired] != ^uint64(0) || (*c.checkedHandles)[h] != 1 {
		t.Fatal("Tiny handle generation wrapped or lost its initial value")
	}
	if len(c.tinyGC.color) != len(c.handles) || c.tinyGC.color[h] != tinyMarkState(c.tinyGC.markEpoch) {
		t.Fatal("replacement identity lost its current-epoch color")
	}
	root := RefSliceRoots{r}
	if err := c.CollectFull(root); err != nil {
		t.Fatal(err)
	}
	value, err := c.StructGet(root[0], 0)
	if err != nil || value.Bits != 29 {
		t.Fatalf("replacement object lost: %+v, %v", value, err)
	}
	if err := c.Verify(root); err != nil {
		t.Fatal(err)
	}
}
