//go:build linux && amd64

package amd64

import "testing"

func TestNewStackArenaDefaultCapacity(t *testing.T) {
	s := newStack()
	if cap(s.arena) != defaultStackArenaCap {
		t.Fatalf("stack arena cap = %d, want %d", cap(s.arena), defaultStackArenaCap)
	}
}

func TestNewStackWithCapClamps(t *testing.T) {
	for _, tc := range []struct {
		hint int
		want int
	}{
		{0, minStackArenaCap},
		{minStackArenaCap - 1, minStackArenaCap},
		{minStackArenaCap + 7, minStackArenaCap + 7},
		{defaultStackArenaCap + 1, defaultStackArenaCap},
	} {
		s := newStackWithCap(tc.hint)
		if cap(s.arena) != tc.want {
			t.Fatalf("newStackWithCap(%d) cap = %d, want %d", tc.hint, cap(s.arena), tc.want)
		}
		if s.head == nil || s.head.next != s.head || s.head.prev != s.head {
			t.Fatalf("newStackWithCap(%d) did not initialize sentinel links", tc.hint)
		}
	}
}

func TestStackArenaCapForBodyTinyFunction(t *testing.T) {
	s := newStackWithCap(stackArenaCapForBody(0, 0))
	if cap(s.arena) != minStackArenaCap {
		t.Fatalf("tiny stack arena cap = %d, want %d", cap(s.arena), minStackArenaCap)
	}
}

func TestStackArenaCapForBodyMediumFunction(t *testing.T) {
	const bodyLen = 64
	const locals = 12
	want := bodyLen + locals/4 + 1
	s := newStackWithCap(stackArenaCapForBody(bodyLen, locals))
	if cap(s.arena) != want {
		t.Fatalf("medium stack arena cap = %d, want %d", cap(s.arena), want)
	}
}

func TestStackArenaCapForBodyLargeFunctionClamp(t *testing.T) {
	s := newStackWithCap(stackArenaCapForBody(1024, 128))
	if cap(s.arena) != defaultStackArenaCap {
		t.Fatalf("large stack arena cap = %d, want clamp %d", cap(s.arena), defaultStackArenaCap)
	}
}

func TestStackArenaPointerStabilityAcrossArenaAndHeapFallback(t *testing.T) {
	s := newStackWithCap(minStackArenaCap)
	first := s.pushValue(storage{kind: stConst, typ: mtI32, cval: 1})
	var last *elem
	for i := 2; i < minStackArenaCap; i++ { // fills the fixed arena after the sentinel.
		last = s.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(i)})
	}
	// The next allocation exceeds the fixed arena capacity and falls back to a
	// standalone heap node. Existing arena pointers must remain valid.
	heap := s.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(minStackArenaCap)})
	if first.st.cval != 1 || last.st.cval != minStackArenaCap-1 || heap.st.cval != minStackArenaCap {
		t.Fatalf("stack values changed across heap fallback: %d %d %d", first.st.cval, last.st.cval, heap.st.cval)
	}
	if s.head.next != first || last.next != heap || heap.next != s.head {
		t.Fatal("stack links changed across heap fallback")
	}
}

func TestRegMask(t *testing.T) {
	m := maskOf(RAX, R12, R15)
	for _, r := range []Reg{RAX, R12, R15} {
		if !m.has(r) {
			t.Fatalf("mask should contain %v", r)
		}
	}
	if m.has(RCX) {
		t.Fatal("mask should not contain RCX")
	}
	if m.count() != 3 {
		t.Fatalf("count = %d, want 3", m.count())
	}
	m = m.remove(R12)
	if m.has(R12) || m.count() != 2 {
		t.Fatal("remove failed")
	}
	if got, ok := m.union(maskOf(RCX)).firstIn([]Reg{RDX, RCX, RAX}); !ok || got != RCX {
		t.Fatalf("firstIn = %v,%v, want RCX,true", got, ok)
	}
}

func TestRegLayout(t *testing.T) {
	// Reserved scratch regs are the trailing pool entries and include the fixed
	// x86 roles (RAX/RDX/RCX) and the return registers.
	for _, r := range []Reg{RAX, RDX, RCX, R8} {
		if !isScratchGP(r) {
			t.Errorf("%v should be a scratch GP", r)
		}
	}
	for _, r := range []Reg{RDI, R12, R15} {
		if isScratchGP(r) {
			t.Errorf("%v should NOT be reserved scratch", r)
		}
	}
	// linMem (RBX) and stack ptr (RSP) are not allocatable. RBP IS (frameless).
	for _, r := range []Reg{RBX, RSP} {
		if gpAllocPos(r) != -1 {
			t.Errorf("%v must not be in the allocation pool", r)
		}
	}
	if gpAllocPos(RBP) == -1 {
		t.Errorf("RBP must be allocatable in the frameless backend")
	}
	if gpAllocPos(RDI) != 0 {
		t.Errorf("RDI should be first in the pool, got pos %d", gpAllocPos(RDI))
	}
}

func TestAssignPinnedLocalsUsesLocalDefs(t *testing.T) {
	f := &fn{
		nLocals:   3,
		localType: []machineType{mtI32, mtF64, mtI32},
	}
	f.assignPinnedLocals([]int64{1, 10, 5}, nil, nil, pinnedLocalRegs)

	r, isFloat, ok := f.pinReg(1)
	if !ok || !isFloat || r != pinnedFLocalRegs[0] {
		t.Fatalf("float local pin = %v,%v,%v", r, isFloat, ok)
	}
	r, isFloat, ok = f.pinReg(2)
	if !ok || isFloat || r != pinnedLocalRegs[0] {
		t.Fatalf("hot int local pin = %v,%v,%v", r, isFloat, ok)
	}
	if f.locals[2].state != lsReg {
		t.Fatalf("initial local state = %v, want lsReg", f.locals[2].state)
	}
}

// TestStackValentBlock builds `local.get 0; local.get 1; i32.add` and a nested
// `(a+b)+c`, checking the deferred-tree navigation: the add's operands, and the
// block base = the deepest-left leaf.
func TestStackValentBlock(t *testing.T) {
	f := &fn{s: newStack()}
	a := f.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 0})
	b := f.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 1})
	f.pushBinOp(opAdd, mtI32) // a + b
	add := f.s.back()
	if !add.isDeferred() || add.arg0 != a || add.arg1 != b {
		t.Fatalf("add node operands wrong: arg0=%p arg1=%p (want a=%p b=%p)", add.arg0, add.arg1, a, b)
	}
	if base := baseOfValentBlock(add); base != a {
		t.Fatalf("baseOfValentBlock(add) = %p, want a=%p", base, a)
	}
	// nest: (a+b) + c
	c := f.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 2})
	f.pushBinOp(opAdd, mtI32)
	outer := f.s.back()
	if outer.arg0 != add || outer.arg1 != c {
		t.Fatalf("outer operands wrong: arg0=%p arg1=%p (want add=%p c=%p)", outer.arg0, outer.arg1, add, c)
	}
	if base := baseOfValentBlock(outer); base != a {
		t.Fatalf("baseOfValentBlock(outer) = %p, want a=%p (deepest-left leaf)", base, a)
	}
}
