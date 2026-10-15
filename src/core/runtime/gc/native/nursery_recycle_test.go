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
			if err := c.CollectMinor(EmptyRoots{}); err != nil {
				t.Fatal(err)
			}
			for _, ref := range dead {
				if c.handles[handleOf(ref)] != (handleEntry{}) {
					t.Fatalf("dead handle %v was not cleared", ref)
				}
			}
			for i, ref := range dead {
				if got := c.freeHandles[len(c.freeHandles)-len(dead)+i]; got != handleOf(ref) {
					t.Fatalf("free order %d = %d, want %d", i, got, handleOf(ref))
				}
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
