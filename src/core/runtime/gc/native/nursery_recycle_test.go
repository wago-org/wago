package gc

import (
	"fmt"
	"testing"
)

func TestMinorNurseryRecyclingPreservesMixedHeap(t *testing.T) {
	for _, poison := range []bool{false, true} {
		t.Run(fmt.Sprint("poison=", poison), func(t *testing.T) {
			leaf, err := NewStructDesc(0, []StorageKind{StorageI32})
			if err != nil {
				t.Fatal(err)
			}
			parentType, err := NewStructDesc(1, []StorageKind{StorageRefNull})
			if err != nil {
				t.Fatal(err)
			}
			array, err := NewArrayDesc(2, StorageI32)
			if err != nil {
				t.Fatal(err)
			}
			c, err := NewCollector(Config{NurseryBytes: 4096, SurvivorBytes: 2048, LargeObjectBytes: 1024, VerifyAfterCollect: true, PoisonFreed: poison}, []TypeDesc{leaf, parentType, array})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			parent, err := c.NewStructDefault(1)
			if err != nil {
				t.Fatal(err)
			}
			c.NewGlobalSlot(parent)
			if err := c.ForcePromote(parent); err != nil {
				t.Fatal(err)
			}
			child, err := c.NewStructDefault(0)
			if err != nil {
				t.Fatal(err)
			}
			if err := c.StructSet(child, 0, I32Value(137)); err != nil {
				t.Fatal(err)
			}
			if err := c.StructSet(parent, 0, RefValue(child)); err != nil {
				t.Fatal(err)
			}
			if c.RememberedCount() != 1 || c.CardCount() == 0 {
				t.Fatalf("old edge lacks cards: %d/%d", c.RememberedCount(), c.CardCount())
			}
			dead := make([]Ref, 0, 65)
			large, err := c.NewArrayDefault(2, 512)
			if err != nil {
				t.Fatal(err)
			}
			dead = append(dead, large)
			for range 64 {
				ref, err := c.NewStructDefault(0)
				if err != nil {
					t.Fatal(err)
				}
				dead = append(dead, ref)
			}
			deadPayload := c.bytes(dead[1])
			entries := len(c.handles)
			if !c.PrepareNativeStructHandles() {
				t.Fatal("no native batch")
			}
			// Cancellation first returns the unused reservation. Collection must
			// then recycle every dead object exactly once, in any order.
			freeBefore := len(c.freeHandles) + int(c.nativeStructAlloc.Count)
			if err := c.CollectMinor(EmptyRoots{}); err != nil {
				t.Fatal(err)
			}
			for i, ref := range dead {
				e := c.handles[handleOf(ref)]
				if e.space != spaceFree {
					t.Fatalf("dead handle %v space = %v, want free", ref, e.space)
				}
				// Poisoning and allocator-owned large objects use the general release
				// path, which continues to clear the complete handle entry.
				if (poison || i == 0) && e != (handleEntry{}) {
					t.Fatalf("released handle %v was not cleared", ref)
				}
			}
			if got, want := len(c.freeHandles), freeBefore+len(dead); got != want {
				t.Fatalf("free identities = %d, want %d", got, want)
			}
			wantDead := make(map[uint32]bool, len(dead))
			for _, ref := range dead {
				wantDead[handleOf(ref)] = true
			}
			for _, h := range c.freeHandles[freeBefore:] {
				if !wantDead[h] {
					t.Fatalf("unexpected or duplicate recycled identity %d", h)
				}
				delete(wantDead, h)
			}
			if poison {
				for i, b := range deadPayload {
					if b != 0xdd {
						t.Fatalf("poison byte %d = %#x", i, b)
					}
				}
			}
			for range 2 {
				if err := c.CollectMinor(EmptyRoots{}); err != nil {
					t.Fatal(err)
				}
			}
			got, err := c.StructGet(parent, 0)
			if err != nil || got.Ref != child {
				t.Fatalf("old edge = %v, %v", got, err)
			}
			value, err := c.StructGet(child, 0)
			if err != nil || value.I32() != 137 {
				t.Fatalf("child payload = %v, %v", value, err)
			}
			for range 64 {
				if _, err := c.NewStructDefault(0); err != nil {
					t.Fatal(err)
				}
			}
			if len(c.handles) > entries+nativeStructHandleBatch {
				t.Fatal("recycling grew the identity table")
			}
			if err := c.Verify(EmptyRoots{}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMinorSparseRecyclingSurvivorPositions(t *testing.T) {
	for _, positions := range [][]int{{0}, {63}, {0, 63}, {31, 32}, {0, 1, 2, 3, 60, 61, 62, 63}} {
		t.Run(fmt.Sprint(positions), func(t *testing.T) {
			desc, err := NewStructDesc(0, []StorageKind{StorageI32})
			if err != nil {
				t.Fatal(err)
			}
			c, err := NewCollector(Config{NurseryBytes: 16384, SurvivorBytes: 4096, VerifyAfterCollect: true}, []TypeDesc{desc})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			refs := make([]Ref, 64)
			for i := range refs {
				refs[i], err = c.NewStructDefault(0)
				if err != nil {
					t.Fatal(err)
				}
				if err := c.StructSet(refs[i], 0, I32Value(int32(i+100))); err != nil {
					t.Fatal(err)
				}
			}
			roots := make(RefSliceRoots, len(positions))
			live := make(map[uint32]bool, len(positions))
			for i, pos := range positions {
				roots[i] = refs[pos]
				live[handleOf(refs[pos])] = true
			}
			if err := c.CollectMinor(roots); err != nil {
				t.Fatal(err)
			}
			if len(c.freeHandles) != len(refs)-len(roots) || len(c.nurseryHandles) != len(roots) {
				t.Fatalf("free/young counts = %d/%d", len(c.freeHandles), len(c.nurseryHandles))
			}
			dead := make(map[uint32]bool, len(c.freeHandles))
			for _, h := range c.freeHandles {
				if h == 0 || int(h) >= len(c.handles) || live[h] || dead[h] || c.handles[h].space != spaceFree {
					t.Fatalf("invalid or duplicate free identity %d", h)
				}
				dead[h] = true
			}
			for i, h := range c.nurseryHandles {
				if h != handleOf(roots[i]) {
					t.Fatalf("survivor %d = %d, want %d", i, h, handleOf(roots[i]))
				}
			}
			for range len(refs) - len(roots) {
				r, err := c.NewStructDefault(0)
				if err != nil {
					t.Fatal(err)
				}
				if !dead[handleOf(r)] {
					t.Fatalf("allocation did not reuse a unique dead identity: %v", r)
				}
				delete(dead, handleOf(r))
			}
			if err := c.CollectMinor(roots); err != nil {
				t.Fatal(err)
			}
			if got, want := len(c.freeHandles), len(refs)-len(roots); got != want {
				t.Fatalf("second collection free identities = %d, want %d", got, want)
			}
			for _, h := range c.freeHandles {
				if h == 0 || int(h) >= len(c.handles) || live[h] || dead[h] || c.handles[h].space != spaceFree {
					t.Fatalf("second collection invalid or duplicate free identity %d", h)
				}
				dead[h] = true
			}
			for i, pos := range positions {
				got, err := c.StructGet(roots[i], 0)
				if err != nil || got.I32() != int32(pos+100) {
					t.Fatalf("survivor %d payload = %v, %v", pos, got, err)
				}
			}
			if !c.PrepareNativeStructHandles() {
				t.Fatal("no native batch after sparse recycling")
			}
			batch := &c.nativeStructAlloc
			reserved := make(map[uint32]bool, batch.Count)
			for i, h := range batch.Handles[:batch.Count] {
				if h == 0 || int(h) >= len(c.handles) || live[h] || reserved[h] || c.handles[h].space != spaceFree {
					t.Fatalf("invalid or duplicate reserved identity %d", h)
				}
				if batch.HandleBase != 0 && h != batch.HandleBase+uint32(i) {
					t.Fatalf("non-contiguous native batch at identity %d", h)
				}
				reserved[h] = true
			}
		})
	}
}
