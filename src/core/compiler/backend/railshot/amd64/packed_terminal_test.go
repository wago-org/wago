//go:build amd64

package amd64

import (
	"bytes"
	"fmt"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	"testing"
)

func packedTestEnabled(t *testing.T) {
	t.Helper()
	old := packedTerminalEnabled
	packedTerminalEnabled = true
	t.Cleanup(func() { packedTerminalEnabled = old })
}
func packedTestFn() *fn {
	return &fn{s: newStackWithCap(16), a: &encoder.Asm{}, policy: currentCodegenPolicy()}
}
func TestPackedTerminalRoundTrip(t *testing.T) {
	packedTestEnabled(t)
	for _, typ := range []machineType{mtI32, mtI64} {
		for _, op := range []wOp{opAdd, opSub, opAnd, opOr, opXor} {
			for _, pair := range [][2]storage{
				{{kind: stLocalRef, idx: 0xffffffff}, {kind: stConst, cval: -2147483648}},
				{{kind: stConst, cval: 2147483647}, {kind: stLocalReg, idx: 17, reg: R15}},
				{{kind: stLocalReg, idx: 7, reg: R12}, {kind: stLocalReg, idx: 9, reg: R8}},
				{{kind: stLocalRef, idx: 13}, {kind: stLocalReg, idx: 13, reg: R13}},
			} {
				f := packedTestFn()
				outer := f.s.pushValue(storage{kind: stConst, typ: typ, cval: 91})
				f.s.pendingPrefix = outer
				pair[0].typ = typ
				pair[1].typ = typ
				left := f.s.pushValue(pair[0])
				right := f.s.pushValue(pair[1])
				ls, rs := left.st, right.st
				depth := f.s.logicalDepth
				effects := f.s.pendingEffects
				if !f.tryPackTerminal(op, typ, left, right) {
					t.Fatalf("not packed typ=%v op=%v pair=%+v", typ, op, pair)
				}
				n := f.s.back()
				if n != left || !n.isPackedTerminal() || n.st.cold != 0 || n.arg0 != nil || n.arg1 != nil || f.s.logicalDepth != depth-1 {
					t.Fatal("packed shape")
				}
				if outer.next != n || n.prev != outer || n.next != f.s.head || f.s.pendingPrefix != outer || f.s.pendingEffects != effects {
					t.Fatal("outer state changed")
				}
				facts := n.st.meta
				for len(*f.s.activeChunk) < cap(*f.s.activeChunk) {
					f.s.alloc()
				} // Force expansion across an arena chunk boundary.
				f.expandPackedTerminal(n)
				if n.isPackedTerminal() || !n.isDeferred() || n.deferredOp() != op || n.valueType() != typ || n.st.meta != facts || n.st.cold != 0 || f.s.logicalDepth != depth-1 {
					t.Fatal("expanded parent state")
				}
				ls.setLogicalRoot(false)
				rs.setLogicalRoot(false)
				if n.arg0.st != ls || n.arg1.st != rs {
					t.Fatalf("roundtrip got=%+v/%+v want=%+v/%+v", n.arg0.st, n.arg1.st, ls, rs)
				}
				if outer.next != n.arg0 || n.arg0.prev != outer || n.arg0.next != n.arg1 || n.arg1.prev != n.arg0 || n.arg1.next != n || n.prev != n.arg1 || n.next != f.s.head || f.s.head.prev != n {
					t.Fatal("expanded physical order")
				}
				if f.s.pendingPrefix != outer || f.s.pendingEffects != effects {
					t.Fatal("expanded pending state")
				}
				a, b := n.arg0, n.arg1
				f.expandPackedTerminal(n)
				if n.arg0 != a || n.arg1 != b {
					t.Fatal("expansion not idempotent")
				}
			}
		}
	}
}
func TestPackedTerminalEligibilityRejectsWithoutMutation(t *testing.T) {
	packedTestEnabled(t)
	cases := []struct {
		name string
		edit func(*fn, *elem, *elem)
		op   wOp
		typ  machineType
	}{
		{"interval", func(f *fn, l, r *elem) { f.intervalReg = []Reg{R12} }, opAdd, mtI32},
		{"owned", func(f *fn, l, r *elem) { l.st.kind = stReg }, opAdd, mtI32},
		{"memory", func(f *fn, l, r *elem) { l.st.kind = stMemRef }, opAdd, mtI32},
		{"root", func(f *fn, l, r *elem) { l.st.setGCRoot(true) }, opAdd, mtI32},
		{"facts", func(f *fn, l, r *elem) { l.st.setValueFacts(factUpper32Zero) }, opAdd, mtI32},
		{"slot", func(f *fn, l, r *elem) { l.st.slot = 8 }, opAdd, mtI32},
		{"cold", func(f *fn, l, r *elem) { l.st.cold = 1 }, opAdd, mtI32},
		{"provenance", func(f *fn, l, r *elem) { l.st.cval = 7 }, opAdd, mtI32},
		{"two-constants", func(f *fn, l, r *elem) { l.st.kind = stConst }, opAdd, mtI32},
		{"large-i64", func(f *fn, l, r *elem) { r.st.cval = 1 << 40 }, opAdd, mtI64},
		{"div", func(f *fn, l, r *elem) {}, opDivU, mtI32},
		{"float", func(f *fn, l, r *elem) {}, opAdd, mtF32},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := packedTestFn()
			l := f.s.pushValue(storage{kind: stLocalRef, typ: c.typ})
			r := f.s.pushValue(storage{kind: stConst, typ: c.typ, cval: 7})
			c.edit(f, l, r)
			a, b, d := *l, *r, f.s.logicalDepth
			if f.tryPackTerminal(c.op, c.typ, l, r) {
				t.Fatal("accepted")
			}
			if *l != a || *r != b || f.s.logicalDepth != d {
				t.Fatal("rejection mutated state")
			}
		})
	}
}
func TestPackedTerminalDoesNotRewindUnrelatedAllocation(t *testing.T) {
	packedTestEnabled(t)
	f := packedTestFn()
	l := f.s.pushValue(storage{kind: stLocalRef, typ: mtI32})
	r := f.s.pushValue(storage{kind: stConst, typ: mtI32, cval: 3})
	unused := f.s.alloc()
	unused.st.cval = 777
	if !f.tryPackTerminal(opAdd, mtI32, l, r) {
		t.Fatal("not packed")
	}
	next := f.s.alloc()
	next.st.cval = 888
	if unused.st.cval != 777 || next == unused {
		t.Fatal("unrelated allocation rewound")
	}
	f.expandPackedTerminal(l)
	if unused.st.cval != 777 {
		t.Fatal("unrelated allocation overwritten")
	}
}
func TestPackedTerminalExpansionPreservesOverwriteCapture(t *testing.T) {
	packedTestEnabled(t)
	oldCaptured := capturedBoundsEnabled
	capturedBoundsEnabled = true
	t.Cleanup(func() { capturedBoundsEnabled = oldCaptured })
	makeFn := func(pack, rhsLocal, bounds bool) (*fn, *elem) {
		f := packedTestFn()
		f.localType = []machineType{mtI32}
		f.localSlot = []uint32{0}
		f.locals = []localDef{{typ: mtI32, reg: R12, state: lsReg}}
		f.pinnedLocals = []int{0}
		f.pinnedLocalMask = maskOf(R12)
		f.boundsFacts = bounds
		if bounds {
			f.boundsCertUpdate(1, 0, 16)
		}
		l := f.s.pushValue(storage{kind: stLocalReg, typ: mtI32, reg: R12, idx: 0})
		r := f.s.pushValue(storage{kind: stConst, typ: mtI32, cval: 7})
		if rhsLocal {
			r.st = storage{kind: stLocalReg, typ: mtI32, reg: R12, idx: 0, meta: uint8(storageLogicalRoot)}
		}
		var n *elem
		if pack {
			if !f.tryPackTerminal(opAdd, mtI32, l, r) {
				t.Fatal("not packed")
			}
			n = l
		} else {
			n = f.s.alloc()
			n.setElemKind(ekDeferred)
			n.setDeferredOp(opAdd)
			n.setValueType(mtI32)
			n.arg0, n.arg1 = l, r
			labelDeferredNode(n)
			f.s.pushDeferred(n)
		}
		return f, n
	}
	for _, bounds := range []bool{false, true} {
		for _, both := range []bool{false, true} {
			a, an := makeFn(false, both, bounds)
			b, bn := makeFn(true, both, bounds)
			a.realizeLocalRefs(0, nil)
			b.realizeLocalRefs(0, nil)
			if !bytes.Equal(a.a.B, b.a.B) || an.arg0.st != bn.arg0.st || an.arg1.st != bn.arg1.st || a.s.logicalDepth != b.s.logicalDepth {
				t.Fatal("overwrite capture differs from original tree")
			}
			if bn.arg0.st.kind != stReg || both && bn.arg1.st.kind != stReg {
				t.Fatal("old local value not captured")
			}
			if bounds && (bn.arg0.st.cval != 16 || bn.arg0.st.meta&uint8(storageCapturedBounds) == 0 || both && (bn.arg1.st.cval != 16 || bn.arg1.st.meta&uint8(storageCapturedBounds) == 0)) {
				t.Fatal("captured bounds not retained")
			}
		}
	}
}

// Compare the activated typed path with the original tree at identical allocator
// state. Exact bytes also guard i32 zero extension and fixed/RHS alias handling.
func TestPackedTerminalTypedMaterializerParity(t *testing.T) {
	packedTestEnabled(t)
	for _, typ := range []machineType{mtI32, mtI64} {
		for _, op := range []wOp{opAdd, opSub, opAnd, opOr, opXor} {
			pairs := [][2]storage{
				{{kind: stLocalRef, idx: 0}, {kind: stConst, cval: 7}},
				{{kind: stConst, cval: 7}, {kind: stLocalReg, idx: 1, reg: R12}},
				{{kind: stConst, cval: 0}, {kind: stLocalReg, idx: 1, reg: R12}},
				{{kind: stLocalReg, idx: 0, reg: R8}, {kind: stLocalReg, idx: 1, reg: R12}},
				{{kind: stLocalReg, idx: 1, reg: R12}, {kind: stLocalReg, idx: 1, reg: R12}},
				{{kind: stLocalReg, idx: 0, reg: R8}, {kind: stConst, cval: -2147483648}},
				{{kind: stLocalRef, idx: 0}, {kind: stLocalRef, idx: 1}},
				{{kind: stLocalReg, idx: 0, reg: R8}, {kind: stLocalRef, idx: 1}},
			}
			for pi, pair := range pairs {
				for _, dest := range []Reg{regNone, RAX, R8, R12} {
					t.Run(fmt.Sprintf("type%d-op%d-pair%d-dest%d", typ, op, pi, dest), func(t *testing.T) {
						makeFn := func(pack bool) (*fn, *elem) {
							f := packedTestFn()
							f.localType = []machineType{typ, typ}
							f.localSlot = []uint32{0, 8}
							f.locals = []localDef{{typ: typ, reg: R8, state: lsReg}, {typ: typ, reg: R12, state: lsReg}}
							f.pinnedLocals = []int{0, 1}
							f.pinnedLocalMask = maskOf(R8).union(maskOf(R12))
							a, b := pair[0], pair[1]
							a.typ = typ
							b.typ = typ
							l, r := f.s.pushValue(a), f.s.pushValue(b)
							if pack {
								if !f.tryPackTerminal(op, typ, l, r) {
									t.Fatal("not admitted")
								}
								return f, l
							}
							n := f.s.alloc()
							n.setElemKind(ekDeferred)
							n.setDeferredOp(op)
							n.setValueType(typ)
							n.arg0, n.arg1 = l, r
							labelDeferredNode(n)
							f.s.pushDeferred(n)
							if f.opt(optValueFacts) {
								n.st.setValueFacts(deferredResultFacts(op, typ))
							}
							return f, n
						}
						a, an := makeFn(false)
						b, bn := makeFn(true)
						ar := a.condenseBinary(an, dest)
						br := b.condensePackedTerminal(bn, dest)
						if ar != br || !bytes.Equal(a.a.B, b.a.B) {
							t.Fatalf("native mismatch regs %v/%v bytes %x/%x", ar, br, a.a.B, b.a.B)
						}
						if an.st != bn.st || a.s.logicalDepth != b.s.logicalDepth || b.s.packedCount != 0 {
							t.Fatalf("state mismatch: %+v/%+v depth %d/%d packed %d", an.st, bn.st, a.s.logicalDepth, b.s.logicalDepth, b.s.packedCount)
						}
						if b.regUser[br] != bn {
							t.Fatal("result ownership lost")
						}
					})
				}
			}
		}
	}
}
func TestPackedTerminalLifecycle(t *testing.T) {
	packedTestEnabled(t)
	for _, action := range []string{"drop", "replace", "occupy", "expand-reset"} {
		t.Run(action, func(t *testing.T) {
			f := packedTestFn()
			outer := f.s.pushValue(storage{kind: stConst, typ: mtI32, cval: 99})
			l := f.s.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 0})
			r := f.s.pushValue(storage{kind: stConst, typ: mtI32, cval: 7})
			if !f.tryPackTerminal(opAdd, mtI32, l, r) {
				t.Fatal("not packed")
			}
			switch action {
			case "drop":
				f.dropValue()
				if f.s.back() != outer || f.s.logicalDepth != 1 || len(f.a.B) != 0 {
					t.Fatal("pure drop changed outer state or emitted code")
				}
			case "replace":
				f.replaceStorage(l, storage{kind: stConst, typ: mtI32, cval: 8})
				f.erase(l)
			case "occupy":
				f.occupy(l, RAX)
				if f.regUser[RAX] != l {
					t.Fatal("lost register owner")
				}
				f.release(RAX)
				f.erase(l)
			case "expand-reset":
				f.expandPackedTerminal(l)
			}
			if f.s.packedCount != 0 {
				t.Fatal("stale packed count")
			}
			f.s.reset()
			if f.s.packedCount != 0 || f.s.logicalDepth != 0 {
				t.Fatal("reset stale state")
			}
			for _, e := range f.regUser {
				if e == r {
					t.Fatal("erased RHS retained as register owner")
				}
			}
		})
	}
}

func TestPackedTerminalOpcodeBoundaryPreservesPrefix(t *testing.T) {
	packedTestEnabled(t)
	for _, op := range []byte{0x04, 0x45, 0x1b, 0x23, 0x28, 0x10} {
		f := packedTestFn()
		outer := f.s.pushValue(storage{kind: stConst, typ: mtI32, cval: 99})
		f.s.pendingPrefix = outer
		l := f.s.pushValue(storage{kind: stLocalRef, typ: mtI32, idx: 0})
		r := f.s.pushValue(storage{kind: stConst, typ: mtI32, cval: 7})
		if !f.tryPackTerminal(opAdd, mtI32, l, r) {
			t.Fatal("not packed")
		}
		effects := f.s.pendingEffects
		f.packedOpcodeBoundary(op)
		if l.isPackedTerminal() || f.s.packedCount != 0 || l.arg0 == nil || l.arg1 == nil {
			t.Fatal("boundary failed expansion")
		}
		if f.s.pendingPrefix != outer || f.s.pendingEffects != effects || f.s.logicalDepth != 2 || len(f.a.B) != 0 {
			t.Fatal("boundary changed prefix/effects/order")
		}
	}
}

func TestPackedTerminalLargerExpressionParity(t *testing.T) {
	packedTestEnabled(t)
	for _, outerOp := range []wOp{opMul, opSub, opXor} {
		makeFn := func(enabled bool) (*fn, *elem) {
			packedTerminalEnabled = enabled
			f := packedTestFn()
			f.localType = []machineType{mtI32}
			f.localSlot = []uint32{0}
			f.locals = []localDef{{typ: mtI32, reg: R12, state: lsReg}}
			f.pinnedLocals = []int{0}
			f.pinnedLocalMask = maskOf(R12)
			f.s.pushValue(storage{kind: stLocalReg, typ: mtI32, idx: 0, reg: R12})
			f.s.pushValue(storage{kind: stConst, typ: mtI32, cval: 7})
			f.pushBinOpWithPacking(opAdd, mtI32, true)
			if f.s.back().isPackedTerminal() != enabled {
				t.Fatal("admission not exercised")
			}
			f.s.pushValue(storage{kind: stConst, typ: mtI32, cval: 3})
			f.pushBinOp(outerOp, mtI32)
			if f.s.packedCount != 0 {
				t.Fatal("nested matcher saw packed child")
			}
			n := f.s.back()
			f.condense(n, regNone)
			return f, n
		}
		a, an := makeFn(false)
		b, bn := makeFn(true)
		if !bytes.Equal(a.a.B, b.a.B) || an.st != bn.st || a.s.logicalDepth != b.s.logicalDepth {
			t.Fatal("larger expression output differs")
		}
	}
}
