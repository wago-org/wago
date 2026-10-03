//go:build amd64

package amd64

import (
	"os"
	"runtime"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Qualified on Linux AMD64. Set WAGO_AMD64_FLOAT_BRANCH=0 to restore eager
// boolean materialization before branches. Other platforms retain that path.
var floatBranchEnabled = runtime.GOOS == "linux" && os.Getenv("WAGO_AMD64_FLOAT_BRANCH") != "0"

// Retain only the two eager scalar operands through an immediate branch consumer.
// Equality keeps its existing unordered-aware boolean lowering.
func (f *fn) fcmpNext(r *wasm.Reader, kind wOp, f64 bool) {
	if floatBranchEnabled {
		look := *r
		next, ok := look.Peek()
		if ok && next == 0x45 { // One boolean inversion before the branch.
			_, _ = look.Byte()
			next, ok = look.Peek()
		}
		if ok && (next == 0x04 || next == 0x0d) {
			var op wOp
			switch kind {
			case opLtS:
				op = opFLt
			case opGtS:
				op = opFGt
			case opLeS:
				op = opFLe
			case opGeS:
				op = opFGe
			}
			if op != opNone {
				right := f.s.back()
				left := baseOfValentBlock(right).prev
				node := f.s.alloc()
				node.setElemKind(ekDeferred)
				node.setDeferredOp(op)
				node.setValueType(mtI32)
				node.arg0, node.arg1 = left, right
				labelDeferredNode(node)
				f.s.pushDeferred(node)
				f.stats.peep("float-branch-defer")
				return
			}
		}
	}
	f.fcmp(kind, f64)
}

// Emit the comparison last so all earlier reconciliation may safely precede it.
// Above/above-equal reject unordered inputs; swap operands for less/less-equal.
func (f *fn) emitFloatCompareFlags(node *elem) Cond {
	left, right := node.arg0, node.arg1
	f64 := left.st.typ == mtF64
	xa, ownA := f.operandRegF(left)
	old := f.fpinned
	f.fpinned = old.add(xa)
	xb, ownB := f.operandRegF(right)
	f.fpinned = old
	cc := condA
	switch node.deferredOp() {
	case opFLt:
		f.a.Ucomis(xb, xa, f64)
	case opFGt:
		f.a.Ucomis(xa, xb, f64)
	case opFLe:
		f.a.Ucomis(xb, xa, f64)
		cc = condAE
	case opFGe:
		f.a.Ucomis(xa, xb, f64)
		cc = condAE
	default:
		panic("amd64: invalid deferred float comparison")
	}
	if ownA {
		f.releaseF(xa)
	}
	if ownB {
		f.releaseF(xb)
	}
	f.consumeBlockBelow(node)
	return cc
}

// Local-reference reconciliation can materialize the boolean before its branch.
func (f *fn) condenseFloatCompare(node *elem, dest Reg) Reg {
	if dest == regNone {
		dest = f.allocReg(0)
	}
	saved := f.reserved
	f.reserved = saved.add(dest)
	cc := f.emitFloatCompareFlags(node)
	f.reserved = saved
	f.a.SetccReg(cc, dest)
	f.occupy(node, dest)
	node.setDeferredOp(opNone)
	f.stats.peep("float-branch-materialize")
	return dest
}
