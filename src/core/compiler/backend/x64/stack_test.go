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

// TestStackValentBlock builds `local.get 0; local.get 1; i32.add` and a nested
// `(a+b)+c`, checking the deferred-tree navigation: the add's operands, and the
// block base = the deepest-left leaf.
func TestStackValentBlock(t *testing.T) {
	f := &fn{s: newStack()}
	a := f.s.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 0})
	b := f.s.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 1})
	f.pushBinOp(opAdd, mtI32) // a + b
	add := f.s.back()
	if !add.isDeferred() || add.arg0 != a || add.arg1 != b {
		t.Fatalf("add node operands wrong: arg0=%p arg1=%p (want a=%p b=%p)", add.arg0, add.arg1, a, b)
	}
	if base := baseOfValentBlock(add); base != a {
		t.Fatalf("baseOfValentBlock(add) = %p, want a=%p", base, a)
	}
	// nest: (a+b) + c
	c := f.s.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 2})
	f.pushBinOp(opAdd, mtI32)
	outer := f.s.back()
	if outer.arg0 != add || outer.arg1 != c {
		t.Fatalf("outer operands wrong: arg0=%p arg1=%p (want add=%p c=%p)", outer.arg0, outer.arg1, add, c)
	}
	if base := baseOfValentBlock(outer); base != a {
		t.Fatalf("baseOfValentBlock(outer) = %p, want a=%p (deepest-left leaf)", base, a)
	}
}
