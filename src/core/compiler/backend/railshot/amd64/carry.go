//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// tryTeeAddCarry recognizes the bounded producer/consumer sequence
//
//	iN.add; local.tee $sum; local.get $operand; iN.lt_u
//
// where $operand is one of the add's original operands and the comparison is
// immediately widened with i64.extend_i32_u. The comparison is exactly ADD's
// carry flag, and SETcc already provides the required zero extension. Requiring
// the widening is also a cost gate: an unextended comparison commonly feeds a
// branch, where the existing CMP/Jcc fusion is already cheaper. The reader is
// rewound on every miss, leaving the ordinary Valent path untouched.
func (f *fn) tryTeeAddCarry(r *wasm.Reader, local int, add *elem) bool {
	if r == nil || add == nil || add.kind != ekDeferred || add.op != opAdd ||
		(add.typ != mtI32 && add.typ != mtI64) {
		return false
	}
	// Nested additions may already be covered as one associative accumulator.
	// Disabling that cover merely to expose the root CF can add instructions.
	if add.arg0 != nil && add.arg0.kind == ekDeferred && add.arg0.op == opAdd ||
		add.arg1 != nil && add.arg1.kind == ekDeferred && add.arg1.op == opAdd {
		return false
	}
	save := r.Offset()
	rewind := func() bool {
		_ = r.JumpTo(save)
		return false
	}
	op, err := r.Byte()
	if err != nil || op != 0x20 { // local.get
		return rewind()
	}
	x, err := r.U32()
	operand := int(x) + f.localBase
	if err != nil || operand == local || !addHasLocalOperand(add, operand) {
		return rewind()
	}
	op, err = r.Byte()
	wantCompare := byte(0x49) // i32.lt_u
	if add.typ == mtI64 {
		wantCompare = 0x54 // i64.lt_u
	}
	if err != nil || op != wantCompare {
		return rewind()
	}

	op, err = r.Byte()
	if err != nil || op != 0xad { // i64.extend_i32_u
		return rewind()
	}
	f.stats.peep("tee-add-carry-candidate")
	if !f.opt(optTeeAddCarry) {
		return rewind()
	}

	// The mode excludes every flag-neutral add alternative and INC. Local homing
	// is MOV-only, so CF remains live until SETB materializes the carry.
	if localReg, isFloat, pinned := f.pinReg(local); pinned {
		if isFloat {
			return rewind()
		}
		result := f.allocReg(0)
		f.pinned = f.pinned.add(result)
		f.condenseBinaryMode(add, localReg, true)
		f.release(localReg) // the pin owns it now, not the operand-stack result.
		f.markLocalDirty(local)
		f.a.SetccReg(condB, result)
		f.pinned = f.pinned.remove(result)
		f.occupy(add, result)
		add.st.typ = mtI64
	} else {
		reg := f.condenseBinaryMode(add, regNone, true)
		f.storeFrameInt(f.localAddr(local), reg, f.localType[local])
		f.locals[local].state = lsMem
		f.a.SetccReg(condB, reg)
		f.replaceStorage(add, storage{kind: stReg, typ: mtI64, reg: reg})
	}
	if f.opt(optValueFacts) {
		add.st.facts = factUpper32Zero | factBoolean
	}
	f.stats.peep("tee-add-carry")
	return true
}

func addHasLocalOperand(add *elem, local int) bool {
	for _, operand := range []*elem{add.arg0, add.arg1} {
		if operand == nil || operand.kind != ekValue || operand.st.idx != local {
			continue
		}
		if operand.st.kind == stLocalRef || operand.st.kind == stLocalReg {
			return true
		}
	}
	return false
}
