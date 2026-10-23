//go:build amd64

package amd64

import (
	"os"
	"runtime"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Qualified on Linux AMD64. Set WAGO_AMD64_FLOAT_BRANCH=0 to restore eager
// boolean materialization before branches/selects. Other platforms retain that path.
var floatBranchEnabled = runtime.GOOS == "linux" && os.Getenv("WAGO_AMD64_FLOAT_BRANCH") != "0"

// Retain scalar operands through an immediate branch or integer-select consumer.
func (f *fn) fcmpNext(r *wasm.Reader, kind wOp, f64 bool) {
	if floatBranchEnabled && f.opt(optSTFlags) {
		look := *r
		next, ok := look.Peek()
		if ok && next == 0x45 { // One boolean inversion before the consumer.
			_, _ = look.Byte()
			next, ok = look.Peek()
		}
		if ok && (next == 0x04 || next == 0x0d || next == 0x1b || next == 0x1c) {
			var op wOp
			switch kind {
			case opEq:
				op = opFEq
			case opNe:
				op = opFNe
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
				if f.opt(optValueFacts) {
					node.st.setValueFacts(deferredResultFacts(op, mtI32))
				}
				if profileEnabled && f.stats != nil && f.stats.RecordSources {
					f.rememberProfileNode(node)
				}
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
	f.fpinned = f.fpinned.add(xb)
	cc := condA
	switch node.deferredOp() {
	case opFEq, opFNe:
		if xa == xb {
			f.a.Ucomis(xa, xb, f64)
			cc = condNP
			if node.deferredOp() == opFNe {
				cc = condP
			}
		} else {
			// Scalar EQ produces zero for unordered; NE produces all ones. MOVMSK's
			// low bit ignores upper lanes, and TEST gives a single NaN-correct flag.
			tmp := f.allocReg(0)
			mask := f.allocFReg(maskOf(xa, xb))
			pred := byte(0) // EQ_OQ
			if node.deferredOp() == opFNe {
				pred = 4
			} // NEQ_UQ
			if f.cpuHas(shared.AMD64AVX) {
				f.a.VFCmpScalar(mask, xa, xb, f64, pred)
				if f64 {
					f.a.VMovmskpd(tmp, mask)
				} else {
					f.a.VMovmskps(tmp, mask)
				}
			} else {
				f.a.FMov(mask, xa, f64)
				f.a.SseMapRRI(scalarFloatPrefix(f64), 0, 0xC2, mask, xb, pred)
				prefix := byte(0)
				if f64 {
					prefix = 0x66
				}
				f.a.SseRR(prefix, 0x50, tmp, mask, false)
			}
			f.a.TestImm(tmp, 1, false)
			f.release(tmp)
			f.releaseF(mask)
			cc = condNE
		}
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
	f.fpinned = old
	if ownA {
		f.releaseF(xa)
	}
	if ownB {
		f.releaseF(xb)
	}
	f.consumeBlockBelow(node)
	return cc
}

// Local-reference reconciliation can materialize the boolean before its consumer.
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
