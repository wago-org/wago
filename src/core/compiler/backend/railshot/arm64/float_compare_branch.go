//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Keep float operands through an immediate branch or select, which can consume FCMP's
// NaN-correct flags directly instead of a CSET followed by an integer compare.
// memAddr reconciles earlier trapping operands before introducing a load, including
// comparison loads that will be materialized before a float/vector select fallback.
func (f *fn) fcmpNext(r *wasm.Reader, kind wOp, f64 bool) {
	if f.opt(optSTFlags) {
		look := *r
		next, ok := look.Peek()
		if ok && next == 0x45 {
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
			f.boundPendingPacket()
			rightID := f.s.head.prev
			leftID := f.s.baseOfValentBlock(f.s.node(rightID)).prev
			id, node := f.s.alloc()
			node.st.typ = mtI32
			node.setElemKind(ekDeferred)
			node.setDeferredOp(op)
			node.setChildren(leftID, rightID)
			node.setDeferredDepth(1)
			if f.opt(optValueFacts) {
				node.st.setValueFacts(deferredResultFacts(op, mtI32))
			}
			if profileEnabled && f.stats != nil && f.stats.RecordSources {
				f.rememberProfileNode(node)
			}
			f.s.push(id, node)
			f.stats.peep("float-branch-defer")
			return
		}
	}
	f.fcmp(kind, f64)
}

// Reconciliation runs before this final flag producer. No GP scratch is needed.
func (f *fn) emitFloatCompareFlags(node *elem) Cond {
	left, right := f.s.arg0(node), f.s.arg1(node)
	f64 := left.st.typ == mtF64
	xa, ownA := f.operandRegF(left)
	saved := f.fpinned
	f.fpinned = saved.add(xa)
	xb, ownB := f.operandRegF(right)
	f.fpinned = saved
	var cc Cond
	cmpA, cmpB := xa, xb
	switch node.deferredOp() {
	case opFEq:
		cc = condE
	case opFNe:
		cc = condNE
	case opFLt:
		cmpA, cmpB = xb, xa
		cc = condG
	case opFGt:
		cc = condG
	case opFLe:
		cmpA, cmpB = xb, xa
		cc = condGE
	case opFGe:
		cc = condGE
	default:
		panic("arm64: invalid deferred float comparison")
	}
	f.a.Fcmp(cmpA, cmpB, f64)
	if ownA {
		f.releaseF(xa)
	}
	if ownB {
		f.releaseF(xb)
	}
	f.consumeBlockBelow(node)
	return cc
}

func (f *fn) condenseFloatCompare(node *elem, dest Reg) Reg {
	if dest == regNone {
		dest = f.allocReg(0)
	}
	saved := f.reserved
	f.reserved = saved.add(dest)
	cc := f.emitFloatCompareFlags(node)
	f.reserved = saved
	f.a.Cset32(dest, cc)
	f.occupy(node, dest)
	node.setDeferredOp(opNone)
	f.stats.peep("float-branch-materialize")
	return dest
}
