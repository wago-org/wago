//go:build arm64

package arm64

import "math/bits"

// Match two deferred operations only. The operand arena already gives these
// nodes a single consumer; no use lists or extra compiler storage are needed.
func (f *fn) tryBitfieldPair(node *elem, dest Reg) Reg {
	op := node.deferredOp()
	if op != opAnd && op != opShl && op != opShrS && op != opShrU {
		return regNone
	}
	left, right := f.s.arg0(node), f.s.arg1(node)
	if op == opAnd && left.elemKind() == ekValue && left.st.kind == stConst {
		left, right = right, left
	}
	if right.elemKind() != ekValue || right.st.kind != stConst || left.elemKind() != ekDeferred || left.st.typ != node.st.typ {
		return regNone
	}
	innerOp := left.deferredOp()
	if innerOp != opAnd && innerOp != opShl && innerOp != opShrU {
		return regNone
	}
	src, constant := f.s.arg0(left), f.s.arg1(left)
	if innerOp == opAnd && src.elemKind() == ekValue && src.st.kind == stConst {
		src, constant = constant, src
	}
	if constant.elemKind() != ekValue || constant.st.kind != stConst {
		return regNone
	}
	width := uint64(32)
	if node.st.typ.is64() {
		width = 64
	}
	countMask := width - 1
	var immr, imms uint64
	signed := false
	lowMaskWidth := func(v int64) int {
		m := uint64(v)
		if width == 32 {
			m = uint64(uint32(v))
		}
		if m == 0 || m&(m+1) != 0 {
			return 0
		}
		return bits.OnesCount64(m)
	}
	switch {
	case op == opAnd && innerOp == opShrU:
		n := lowMaskWidth(right.st.cval)
		shift := uint64(constant.st.cval) & countMask
		if n == 0 || shift == 0 {
			return regNone
		}
		immr = shift
		imms = min(width-1, shift+uint64(n)-1)
	case op == opShl && innerOp == opAnd:
		n := lowMaskWidth(constant.st.cval)
		shift := uint64(right.st.cval) & countMask
		if n == 0 || shift == 0 {
			return regNone
		}
		immr = (width - shift) & countMask
		imms = min(width-shift, uint64(n)) - 1
	case (op == opShrS || op == opShrU) && innerOp == opShl:
		l := uint64(constant.st.cval) & countMask
		r := uint64(right.st.cval) & countMask
		if l == 0 || r == 0 {
			return regNone
		}
		immr = (r - l) & countMask
		imms = width - l - 1
		signed = op == opShrS
	default:
		return regNone
	}
	saved := f.pinned
	source, owned := f.materializeRead(src)
	f.pinned = f.pinned.add(source)
	if dest == regNone {
		if owned {
			dest = source
		} else {
			dest = f.allocReg(maskOf(source))
		}
	}
	f.a.Bitfield(dest, source, uint8(immr), uint8(imms), signed, width == 32)
	f.pinned = saved
	if owned && source != dest {
		f.release(source)
	}
	f.consumeBlockBelow(node)
	f.occupy(node, dest)
	f.stats.peep("bitfield-pair")
	return dest
}
