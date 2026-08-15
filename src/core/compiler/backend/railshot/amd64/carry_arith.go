//go:build amd64

package amd64

// tryWidenedCarryArithmetic lowers the exact trees
//
//	x + i64.extend_i32_u(a <u b)
//	x - i64.extend_i32_u(a <u b)
//
// as CMP followed by ADC/SBB x,0. Only unsigned less-than is admitted because
// x86 CF represents it directly. Every subtree must be nontrapping so the add
// form may commute the carry to the right without changing Wasm trap order.
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
	if cc := f.condenseToFlagsMode(compare, false); cc != condB {
		panic("amd64: widened carry matcher admitted a non-CF comparison")
	}
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

func matchWidenedCarryArithmetic(node *elem) (other, ext, compare *elem, digit byte, ok bool) {
	if node == nil || node.kind != ekDeferred || node.typ != mtI64 ||
		(node.op != opAdd && node.op != opSub) {
		return nil, nil, nil, 0, false
	}
	isCarry := func(e *elem) (*elem, bool) {
		if e == nil || e.kind != ekDeferred || e.op != opZExt32 || e.typ != mtI64 ||
			e.arg0 == nil || e.arg0.kind != ekDeferred || e.arg0.op != opLtU {
			return nil, false
		}
		if !treeReorderSafe(e.arg0) {
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
