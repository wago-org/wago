//go:build linux && amd64

package x64

import "testing"

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
	// linMem (RBX), frame ptr (RBP), stack ptr (RSP) are not allocatable.
	for _, r := range []Reg{RBX, RBP, RSP} {
		if gpAllocPos(r) != -1 {
			t.Errorf("%v must not be in the allocation pool", r)
		}
	}
	if gpAllocPos(RDI) != 0 {
		t.Errorf("RDI should be first in the pool, got pos %d", gpAllocPos(RDI))
	}
}

// TestStackValentBlock builds `local.get 0; local.get 1; i32.add` as WARP would:
// two operand leaves with a deferred add node on top, and checks the tree
// navigation (the add's operands are the two leaves; the block base is the
// deepest-left leaf).
func TestStackValentBlock(t *testing.T) {
	s := newStack()
	a := s.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 0})
	b := s.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 1})

	// push a deferred add over the top two operands (mirrors pushDeferredAction).
	add := s.alloc()
	add.kind, add.op, add.typ = ekDeferred, opAdd, mtI32
	// wire children: b is top (right), a is below it (left).
	b.parent, a.parent = add, add
	add.sib = a.sib // node inherits the sibling below the arg group
	a.sib = nil     // left arg terminates the sibling chain
	b.sib = a       // right sibling → left
	s.push(add)

	if s.back() != add {
		t.Fatal("add should be on top")
	}
	if fo := firstOperand(add); fo != a {
		t.Fatalf("firstOperand(add) = %p, want a=%p", fo, a)
	}
	if base := baseOfValentBlock(add); base != a {
		t.Fatalf("baseOfValentBlock(add) = %p, want a=%p", base, a)
	}
}
