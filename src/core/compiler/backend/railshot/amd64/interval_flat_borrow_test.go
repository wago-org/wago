//go:build linux && amd64

package amd64

import (
	"fmt"
	"testing"
)

func TestFlatIntervalBorrowMaskMatchesPhysicalForest(t *testing.T) {
	for depth := 1; depth <= 32; depth++ {
		f := fn{s: newStackWithCap(128), locals: []localDef{{reg: R12}, {reg: R13}, {reg: R14}}}
		root := f.pushValue(storage{kind: stLocalReg, typ: mtI64, reg: R12, idx: 0})
		for level := 0; level < depth; level++ {
			right := f.pushValue(memRefStorage(R13, int32(level*8), 8, false, true, 1))
			node := f.s.alloc()
			node.setElemKind(ekDeferred)
			node.setDeferredOp(opAdd)
			node.setValueType(mtI64)
			node.arg0, node.arg1 = root, right
			f.s.pushDeferred(node)
			root = node
		}
		// A frame reference is a canonical memory home, not a register borrow.
		f.pushValue(storage{kind: stLocalRef, typ: mtI64, idx: 2})
		check := func() {
			t.Helper()
			want := maskOf(R12, R13)
			if got := f.intervalBorrowedRegs(); got != want {
				t.Fatalf("depth=%d flat=%x want=%x", depth, got, want)
			}
			if got := f.intervalBorrowedRegsRecursive(); got != want {
				t.Fatalf("depth=%d recursive=%x want=%x", depth, got, want)
			}
		}
		check()
		// Consumers can detach a root before lowering its still-linked children.
		f.erase(root)
		check()
	}
}

// Register ownership follows the current resident home, not the historical
// register recorded in a pending operand. Inactive locals do not own registers.
func TestFlatIntervalBorrowMaskUsesCurrentHomes(t *testing.T) {
	f := fn{s: newStack(), locals: []localDef{{reg: R14}, {reg: regNone}}}
	f.pushValue(storage{kind: stLocalReg, typ: mtI64, reg: R12, idx: 0})
	f.pushValue(storage{kind: stLocalReg, typ: mtI64, reg: R13, idx: 1})
	f.pushValue(storage{kind: stLocalRef, typ: mtI64, idx: 0})
	if got := f.intervalBorrowedRegs(); got != maskOf(R14) {
		t.Fatalf("borrowed=%x want=%x", got, maskOf(R14))
	}
	if got := f.intervalBorrowedRegsRecursive(); got != maskOf(R14) {
		t.Fatalf("recursive=%x want=%x", got, maskOf(R14))
	}
}

// Keep the old recursive scan as an independent ownership oracle.
func (f *fn) intervalBorrowedRegsRecursive() regMask {
	var borrowed regMask
	var visit func(*elem)
	visit = func(e *elem) {
		if e == nil {
			return
		}
		if e.isDeferred() {
			visit(e.arg0)
			visit(e.arg1)
			return
		}
		if !e.isValue() {
			return
		}
		x := -1
		switch e.st.kind {
		case stLocalReg:
			x = e.st.index()
		case stMemRef:
			x = e.st.memBorrow()
		}
		if x >= 0 && x < len(f.locals) {
			if reg := f.locals[x].reg; reg != regNone {
				borrowed = borrowed.add(reg)
			}
		}
	}
	for e := f.s.head.next; e != f.s.head; e = e.next {
		visit(e)
	}
	return borrowed
}

func BenchmarkIntervalBorrowMask(b *testing.B) {
	for _, depth := range []int{1, 8, 32} {
		for _, recursive := range []bool{false, true} {
			b.Run(fmt.Sprintf("depth=%d/recursive=%v", depth, recursive), func(b *testing.B) {
				f := fn{s: newStackWithCap(128), locals: []localDef{{reg: R12}, {reg: R13}}}
				root := f.pushValue(storage{kind: stLocalReg, typ: mtI64, reg: R12, idx: 0})
				for level := 0; level < depth; level++ {
					right := f.pushValue(memRefStorage(R13, int32(level*8), 8, false, true, 1))
					node := f.s.alloc()
					node.setElemKind(ekDeferred)
					node.setDeferredOp(opAdd)
					node.setValueType(mtI64)
					node.arg0, node.arg1 = root, right
					f.s.pushDeferred(node)
					root = node
				}
				scan := f.intervalBorrowedRegs
				if recursive {
					scan = f.intervalBorrowedRegsRecursive
				}
				b.ReportAllocs()
				b.ResetTimer()
				var got regMask
				for i := 0; i < b.N; i++ {
					got = scan()
				}
				b.StopTimer()
				if got != maskOf(R12, R13) {
					b.Fatalf("borrowed=%x", got)
				}
			})
		}
	}
}
