//go:build linux && amd64

package amd64

import "testing"

func TestNewStackArenaDefaultCapacity(t *testing.T) {
	s := newStack()
	if cap(s.chunks[0]) != defaultStackArenaCap {
		t.Fatalf("first chunk cap = %d, want %d", cap(s.chunks[0]), defaultStackArenaCap)
	}
}

func TestNewStackWithCapSizesFirstChunk(t *testing.T) {
	for _, tc := range []struct {
		hint int
		want int
	}{
		{0, minStackArenaCap},
		{minStackArenaCap - 1, minStackArenaCap},
		{minStackArenaCap + 7, minStackArenaCap + 7},
		{defaultStackArenaCap + 1, defaultStackArenaCap + 1}, // no upper clamp: chunked arena grows freely
	} {
		s := newStackWithCap(tc.hint)
		if cap(s.chunks[0]) != tc.want {
			t.Fatalf("newStackWithCap(%d) first chunk cap = %d, want %d", tc.hint, cap(s.chunks[0]), tc.want)
		}
		if s.head == nil || s.head.next != s.head || s.head.prev != s.head {
			t.Fatalf("newStackWithCap(%d) did not initialize sentinel links", tc.hint)
		}
	}
}

func TestStackArenaCapForBodyTinyFunction(t *testing.T) {
	s := newStackWithCap(stackArenaCapForBody(0, 0))
	if cap(s.chunks[0]) != minStackArenaCap {
		t.Fatalf("tiny stack first chunk cap = %d, want %d", cap(s.chunks[0]), minStackArenaCap)
	}
}

func TestStackArenaCapForBodyMediumFunction(t *testing.T) {
	const bodyLen = 64
	const locals = 12
	want := bodyLen + locals/4 + 1
	s := newStackWithCap(stackArenaCapForBody(bodyLen, locals))
	if cap(s.chunks[0]) != want {
		t.Fatalf("medium stack first chunk cap = %d, want %d", cap(s.chunks[0]), want)
	}
}

func TestStackArenaCapForHintsIgnoresLongImmediates(t *testing.T) {
	// A body with a few stack-producing opcodes and long immediates should reserve
	// from the opcode hint, not one arena elem per byte.
	const bodyLen = 64
	const nodes = 12
	want := nodes + nodes/2 + 1
	if got := stackArenaCapForHints(bodyLen, 0, nodes); got != want {
		t.Fatalf("stackArenaCapForHints(%d, 0, %d) = %d, want %d", bodyLen, nodes, got, want)
	}
}

func TestStackArenaPointerStabilityAcrossChunks(t *testing.T) {
	s := newStackWithCap(minStackArenaCap)
	first := s.pushValue(storage{kind: stConst, typ: mtI32, cval: 1})
	var last *elem
	// Push far past the first chunk so the arena advances through several
	// geometrically-grown chunks. Every earlier *elem must stay valid.
	const total = 4 * minStackArenaCap
	for i := 2; i <= total; i++ {
		last = s.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(i)})
	}
	if len(s.chunks) < 2 {
		t.Fatalf("expected the arena to advance past the first chunk, got %d chunk(s)", len(s.chunks))
	}
	if first.st.cval != 1 || last.st.cval != total {
		t.Fatalf("stack values changed across chunk growth: first=%d last=%d", first.st.cval, last.st.cval)
	}
	// Walk the whole physical list and confirm contiguous values 1..total.
	want := int64(1)
	for e := s.head.next; e != s.head; e = e.next {
		if e.st.cval != want {
			t.Fatalf("list value at position %d = %d, want %d", want, e.st.cval, want)
		}
		want++
	}
	if want != int64(total)+1 {
		t.Fatalf("walked %d nodes, want %d", want-1, total)
	}
}

func TestStackArenaReusesChunksAcrossReset(t *testing.T) {
	s := newStackWithCap(minStackArenaCap)
	for i := 0; i < 4*minStackArenaCap; i++ {
		s.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(i)})
	}
	grown := len(s.chunks)
	if grown < 2 {
		t.Fatalf("expected chunk growth, got %d", grown)
	}
	s.reset()
	// After reset the sentinel is back and every chunk is retained for reuse.
	if s.cur != 0 || len(s.chunks[0]) != 1 || s.head.next != s.head {
		t.Fatalf("reset did not rewind: cur=%d len0=%d", s.cur, len(s.chunks[0]))
	}
	for i := 0; i < 4*minStackArenaCap; i++ {
		s.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(i)})
	}
	if len(s.chunks) != grown {
		t.Fatalf("reuse allocated new chunks: %d, want %d retained", len(s.chunks), grown)
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
		sc:        &scratch{},
	}
	f.assignPinnedLocals([]int64{1, 10, 5}, nil, nil, pinnedLocalRegs, baseFPPins, false)

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
