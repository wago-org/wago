//go:build amd64

package amd64

// tryWidenedCarryArithmetic lowers the exact trees
//
//	x + i64.extend_i32_u(a <u b)
//	x - i64.extend_i32_u(a <u b)
//
// as CMP followed by ADC/SBB x,0. Unsigned less-than maps directly to CF;
// unsigned greater-than swaps two simple comparands in the final CMP. Every
// subtree must be nontrapping so the add form may commute the carry to the right
// without changing Wasm trap order.
func (f *fn) tryWidenedCarryArithmetic(node *elem, dest Reg) Reg {
	other, ext, compare, digit, ok := matchWidenedCarryArithmetic(node)
	if !ok {
		return regNone
	}
	f.stats.peep("widened-carry-arith-candidate")
	if !f.opt(optWidenedCarryArith) {
		return regNone
	}

	result := f.materialize(other)
	f.pinned = f.pinned.add(result)
	f.condenseUnsignedCompareToCarry(compare)
	f.a.AluRI(digit, result, 0, true) // adc/sbb result,0
	f.pinned = f.pinned.remove(result)

	// condenseToFlags consumed the comparison subtree. Remove its now-dead
	// extension and transfer the other operand's register ownership to the root.
	f.erase(ext)
	f.erase(other)
	if dest != regNone && dest != result {
		f.moveInt(dest, result, mtI64)
		f.release(result)
		result = dest
	}
	f.occupy(node, result)
	node.op = opNone
	f.stats.peep("widened-carry-arith")
	return result
}

// condenseUnsignedCompareToCarry consumes compare and leaves its truth value in
// CF. The ordinary lt_u path already emits CMP left,right. For gt_u, materialize
// the simple operands in Wasm order and emit CMP right,left, making right<left
// (CF=1) exactly left>right without materializing a boolean.
func (f *fn) condenseUnsignedCompareToCarry(compare *elem) {
	if compare.op == opLtU {
		if cc := f.condenseToFlagsMode(compare, false); cc != condB {
			panic("amd64: widened carry matcher admitted a non-CF comparison")
		}
		return
	}
	left, right := compare.arg0, compare.arg1
	l := f.materialize(left)
	f.pinned = f.pinned.add(l)
	r := f.materialize(right)
	f.pinned = f.pinned.add(r)
	f.cmpRR(r, l, compare.typ.is64())
	f.pinned = f.pinned.remove(l).remove(r)
	f.release(l)
	f.release(r)
	f.erase(left)
	f.erase(right)
	f.erase(compare)
}

func simpleCarryComparand(e *elem) bool {
	if e == nil || e.kind != ekValue {
		return false
	}
	switch e.st.kind {
	case stReg, stSlot, stLocalRef, stLocalReg, stGlobalRef, stGlobReg:
		return true
	default:
		return false
	}
}

func matchWidenedCarryArithmetic(node *elem) (other, ext, compare *elem, digit byte, ok bool) {
	if node == nil || node.kind != ekDeferred || node.typ != mtI64 ||
		(node.op != opAdd && node.op != opSub) {
		return nil, nil, nil, 0, false
	}
	isCarry := func(e *elem) (*elem, bool) {
		if e == nil || e.kind != ekDeferred || e.op != opZExt32 || e.typ != mtI64 ||
			e.arg0 == nil || e.arg0.kind != ekDeferred ||
			(e.arg0.op != opLtU && e.arg0.op != opGtU) {
			return nil, false
		}
		if !treeReorderSafe(e.arg0) {
			return nil, false
		}
		if e.arg0.op == opGtU &&
			(!simpleCarryComparand(e.arg0.arg0) || !simpleCarryComparand(e.arg0.arg1)) {
			return nil, false
		}
		return e.arg0, true
	}

	if compare, ok = isCarry(node.arg1); ok {
		other, ext = node.arg0, node.arg1
	} else if node.op == opAdd {
		if compare, ok = isCarry(node.arg0); ok {
			other, ext = node.arg1, node.arg0
		}
	}
	if !ok || !treeReorderSafe(other) {
		return nil, nil, nil, 0, false
	}
	digit = 2 // ADC /2
	if node.op == opSub {
		digit = 3 // SBB /3
	}
	return other, ext, compare, digit, true
}
