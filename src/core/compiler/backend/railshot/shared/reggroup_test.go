package shared

import "testing"

func TestGroupAllocatorAtomicOwnership(t *testing.T) {
	a := NewGroupAllocator([]uint8{0, 1, 2, 3, 4, 5})
	pair, ok := a.Alloc(2)
	if !ok || pair.Width != 2 || pair.Regs[0] != 0 || pair.Regs[1] != 1 {
		t.Fatalf("pair=%+v ok=%v", pair, ok)
	}
	quad, ok := a.Alloc(4)
	if !ok || quad.Regs[0] != 2 || quad.Regs[3] != 5 {
		t.Fatalf("quad=%+v ok=%v", quad, ok)
	}
	if _, ok := a.Alloc(1); ok {
		t.Fatal("allocated from full pool")
	}
	forged := pair
	forged.Regs[1] = 2
	if a.Release(forged) {
		t.Fatal("released forged partial pair")
	}
	if !a.Owns(pair) || !a.Owns(quad) {
		t.Fatal("failed release changed ownership")
	}
	if !a.Release(pair) || a.FreeRegisters() != 2 {
		t.Fatal("pair release failed")
	}
	one, ok := a.Alloc(1)
	if !ok || one.Regs[0] != 0 {
		t.Fatalf("one=%+v", one)
	}
}

func TestGroupAllocatorExactAcquireIsTransactional(t *testing.T) {
	a := NewGroupAllocator([]uint8{4, 5, 6, 7})
	pair, ok := a.Acquire([4]uint8{5, 7}, 2)
	if !ok {
		t.Fatal("exact pair acquire")
	}
	if _, ok := a.Acquire([4]uint8{4, 5}, 2); ok {
		t.Fatal("overlapping acquire succeeded")
	}
	if a.FreeRegisters() != 2 || !a.Owns(pair) {
		t.Fatal("failed acquire changed ownership")
	}
	if _, ok := a.Acquire([4]uint8{4, 4}, 2); ok {
		t.Fatal("duplicate register acquire succeeded")
	}
}

func TestGroupAllocatorVictimReturnsWholeLRUGroup(t *testing.T) {
	a := NewGroupAllocator([]uint8{0, 1, 2, 3, 4, 5})
	old, _ := a.Alloc(2)
	newer, _ := a.Alloc(4)
	a.Touch(old)
	victim, ok := a.Victim(0)
	if !ok || victim != newer {
		t.Fatalf("victim=%+v want=%+v", victim, newer)
	}
	victim, ok = a.Victim((1 << newer.Regs[2]))
	if !ok || victim != old {
		t.Fatalf("excluded victim=%+v want=%+v", victim, old)
	}
	if _, ok := a.Victim((1 << old.Regs[0]) | (1 << newer.Regs[0])); ok {
		t.Fatal("victim intersects exclusion")
	}
}

func TestGroupAllocatorRejectsInvalidWidths(t *testing.T) {
	a := NewGroupAllocator([]uint8{0, 1, 2, 3})
	for _, w := range []uint8{0, 3, 5} {
		if _, ok := a.Alloc(w); ok {
			t.Fatalf("allocated width %d", w)
		}
	}
}
