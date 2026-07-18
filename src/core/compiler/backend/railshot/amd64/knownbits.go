//go:build amd64

package amd64

// knownBits is a bounded, allocation-free bit estimator over Railshot's existing
// deferred tree. It is deliberately smaller than a general optimizer: only facts
// that are immediate from constants, unsigned narrow loads, bitwise operations,
// and constant shifts are propagated. The tree is already capped at
// maxDeferDepth, so this adds constant compile-time and stack bounds.
type knownBits struct {
	zero uint64
	one  uint64
}

func bitWidthMask(typ machineType) uint64 {
	if typ == mtI32 {
		return uint64(^uint32(0))
	}
	return ^uint64(0)
}

func estimateKnownBits(e *elem, typ machineType) knownBits {
	mask := bitWidthMask(typ)
	if e == nil {
		return knownBits{}
	}
	if e.kind == ekValue {
		switch e.st.kind {
		case stConst:
			one := uint64(e.st.cval) & mask
			return knownBits{zero: (^one) & mask, one: one}
		case stMemRef:
			if !e.st.memSigned() && e.st.memSize()*8 < 64 {
				valueMask := uint64(1)<<(e.st.memSize()*8) - 1
				return knownBits{zero: mask &^ valueMask}
			}
		}
		return knownBits{}
	}
	if e.kind != ekDeferred {
		return knownBits{}
	}

	switch e.op {
	case opAnd, opOr, opXor:
		a := estimateKnownBits(e.arg0, typ)
		b := estimateKnownBits(e.arg1, typ)
		var k knownBits
		switch e.op {
		case opAnd:
			k = knownBits{zero: a.zero | b.zero, one: a.one & b.one}
		case opOr:
			k = knownBits{zero: a.zero & b.zero, one: a.one | b.one}
		case opXor:
			k = knownBits{
				zero: (a.zero & b.zero) | (a.one & b.one),
				one:  (a.zero & b.one) | (a.one & b.zero),
			}
		}
		k.zero &= mask
		k.one &= mask
		return k
	case opShl, opShrU:
		if e.arg1.kind != ekValue || e.arg1.st.kind != stConst {
			return knownBits{}
		}
		width := uint(64)
		if typ == mtI32 {
			width = 32
		}
		s := uint(e.arg1.st.cval) & (width - 1)
		a := estimateKnownBits(e.arg0, typ)
		if s == 0 {
			return a
		}
		if e.op == opShl {
			return knownBits{
				zero: ((a.zero << s) | (uint64(1)<<s - 1)) & mask,
				one:  (a.one << s) & mask,
			}
		}
		highZero := mask &^ (mask >> s)
		return knownBits{zero: (a.zero >> s) | highZero, one: a.one >> s}
	case opWrap:
		k := estimateKnownBits(e.arg0, mtI64)
		return knownBits{zero: k.zero & mask, one: k.one & mask}
	case opZExt32:
		k := estimateKnownBits(e.arg0, mtI32)
		return knownBits{zero: k.zero | 0xffffffff00000000, one: k.one}
	case opEq, opNe, opLtS, opLtU, opGtS, opGtU, opLeS, opLeU, opGeS, opGeU, opEqz:
		// Wasm comparisons are canonical 0/1 i32 values.
		return knownBits{zero: mask &^ 1}
	}
	return knownBits{}
}

// simplifyKnownBitsRHS removes a constant mask operation when the deferred
// producer already proves it cannot change any bit. This subsumes the common
// unsigned-load masks (load8_u & 0xff, load16_u & 0xffff) and nested SWAR masks.
func (f *fn) simplifyKnownBitsRHS(op wOp, typ machineType, left, right *elem) bool {
	if !knownBitsEnabled || right.kind != ekValue || right.st.kind != stConst {
		return false
	}
	mask := bitWidthMask(typ)
	c := uint64(right.st.cval) & mask
	k := estimateKnownBits(left, typ)
	switch op {
	case opAnd:
		if (mask&^c)&^k.zero != 0 {
			return false
		}
	case opOr:
		if c&^k.one != 0 {
			return false
		}
	default:
		return false
	}
	f.erase(right)
	return true
}
