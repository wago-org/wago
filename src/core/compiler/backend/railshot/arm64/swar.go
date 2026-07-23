//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// trySWARPack4 recognizes utf-as's exact inverse of swar-widen4. The OR tree
// may be associated or ordered differently, but must contain exactly the four
// masked byte gathers from one local.
func (f *fn) trySWARPack4(root *elem) bool {
	if root != nil && root.kind == ekDeferred && root.op == opSWARPack4 {
		return true
	}
	if !swarIdiomsEnabled || root == nil || root.kind != ekDeferred || root.op != opOr || root.typ != mtI64 {
		return false
	}
	var terms [4]*elem
	n := 0
	var flatten func(*elem) bool
	flatten = func(e *elem) bool {
		if e.kind == ekDeferred && e.op == opOr && e.typ == mtI64 {
			return flatten(e.arg0) && flatten(e.arg1)
		}
		if n == len(terms) {
			return false
		}
		terms[n] = e
		n++
		return true
	}
	if !flatten(root) || n != len(terms) {
		return false
	}
	var source *elem
	local := -1
	seen := uint8(0)
	for _, term := range terms {
		if term == nil || term.kind != ekDeferred || term.op != opAnd || term.typ != mtI64 {
			return false
		}
		expr, mask := term.arg0, term.arg1
		if expr != nil && expr.kind == ekValue && expr.st.kind == stConst {
			expr, mask = mask, expr
		}
		if mask == nil || mask.kind != ekValue || mask.st.kind != stConst {
			return false
		}
		shift := uint64(0)
		leaf := expr
		if expr != nil && expr.kind == ekDeferred && expr.op == opShrU && expr.typ == mtI64 {
			leaf = expr.arg0
			if expr.arg1 == nil || expr.arg1.kind != ekValue || expr.arg1.st.kind != stConst {
				return false
			}
			shift = uint64(expr.arg1.st.cval)
		}
		if shift > 24 || shift&7 != 0 || uint64(mask.st.cval) != uint64(0xff)<<shift ||
			leaf == nil || leaf.kind != ekValue || leaf.st.typ != mtI64 ||
			(leaf.st.kind != stLocalRef && leaf.st.kind != stLocalReg) {
			return false
		}
		if local < 0 {
			local, source = leaf.st.idx, leaf
		} else if leaf.st.idx != local {
			return false
		}
		bit := uint8(1 << (shift / 8))
		if seen&bit != 0 {
			return false
		}
		seen |= bit
	}
	if seen != 0x0f {
		return false
	}
	root.op, root.arg0, root.arg1 = opSWARPack4, source, nil
	root.deferDepth = 1 + deferDepthOf(source)
	f.stats.peep("swar-pack4")
	return true
}

// tryMulHighU recognizes the exact straight-line unsigned 64x64 multiply-high
// expansion emitted by xjb-as/AssemblyScript. The expansion overwrites its two
// inputs and two scratch locals, so this first bounded form is deliberately
// restricted to a function-tail expression where those writes are unobservable.
func (f *fn) tryMulHighU(r *wasm.Reader, a1 int) (bool, error) {
	if !swarIdiomsEnabled || a1 < f.localBase {
		return false, nil
	}
	root := f.s.back()
	if root == nil || root.kind != ekDeferred || root.op != opShrU || root.typ != mtI64 ||
		root.arg1 == nil || root.arg1.kind != ekValue || root.arg1.st.kind != stConst || root.arg1.st.cval != 32 ||
		root.arg0 == nil || root.arg0.kind != ekValue ||
		(root.arg0.st.kind != stLocalRef && root.arg0.st.kind != stLocalReg) {
		return false, nil
	}
	a := root.arg0.st.idx
	if a < f.localBase {
		return false, nil
	}
	aRaw, a1Raw := uint32(a-f.localBase), uint32(a1-f.localBase)
	save := r.Offset()
	matchByte := func(want byte) bool { got, err := r.Byte(); return err == nil && got == want }
	matchU32 := func(want uint32) bool { got, err := r.U32(); return err == nil && got == want }
	matchI64 := func(want int64) bool { got, err := r.I64(); return err == nil && got == want }
	local := func(op byte, x uint32) bool { return matchByte(op) && matchU32(x) }
	constant := func(v int64) bool { return matchByte(0x42) && matchI64(v) }

	if !matchByte(0x20) {
		_ = r.JumpTo(save)
		return false, nil
	}
	bRaw, err := r.U32()
	if err != nil || !constant(0xffffffff) || !matchByte(0x83) || !matchByte(0x22) {
		_ = r.JumpTo(save)
		return false, nil
	}
	b0Raw, err := r.U32()
	matched := err == nil &&
		matchByte(0x7e) && // a1*b0
		local(0x20, aRaw) && constant(0xffffffff) && matchByte(0x83) && local(0x22, aRaw) &&
		local(0x20, b0Raw) && matchByte(0x7e) && constant(32) && matchByte(0x88) && matchByte(0x7c) && local(0x21, b0Raw) &&
		local(0x20, bRaw) && constant(32) && matchByte(0x88) && local(0x22, bRaw) &&
		local(0x20, a1Raw) && matchByte(0x7e) &&
		local(0x20, b0Raw) && constant(32) && matchByte(0x88) && matchByte(0x7c) &&
		local(0x20, aRaw) && local(0x20, bRaw) && matchByte(0x7e) &&
		local(0x20, b0Raw) && constant(0xffffffff) && matchByte(0x83) && matchByte(0x7c) &&
		constant(32) && matchByte(0x88) && matchByte(0x7c) &&
		aRaw != bRaw && aRaw != a1Raw && aRaw != b0Raw && bRaw != a1Raw && bRaw != b0Raw && a1Raw != b0Raw &&
		r.BytesLeft() == 1
	if next, ok := r.Peek(); !matched || !ok || next != 0x0b {
		if err := r.JumpTo(save); err != nil {
			return false, err
		}
		return false, nil
	}

	b := int(bRaw) + f.localBase
	if f.localConstZero(b) {
		f.replaceStorage(root.arg1, zeroStorage(mtI64))
	} else if pr, _, ok := f.pinReg(b); ok {
		f.recoverLocal(b)
		f.replaceStorage(root.arg1, storage{kind: stLocalReg, typ: mtI64, reg: pr, idx: b})
	} else {
		f.replaceStorage(root.arg1, storage{kind: stLocalRef, typ: mtI64, idx: b})
	}
	root.op = opMulHighU
	root.deferDepth = 1 + max(deferDepthOf(root.arg0), deferDepthOf(root.arg1))
	f.stats.peep("mul-high-u64")
	return true, nil
}

// trySWARWiden4 recognizes AssemblyScript's local.tee-separated four-byte
// widening idiom:
//
//	w = x & 0xffffffff
//	w = (w | w<<16) & 0x0000ffff0000ffff
//	w = (w | w<<8)  & 0x00ff00ff00ff00ff
//
// The two later expressions are encoded immediately after the first tee. They
// are pure and cannot trap; the liveness scan below proves that the omitted
// temporary-local writes cannot be observed before the local is overwritten.
func (f *fn) trySWARWiden4(r *wasm.Reader, x int) (bool, error) {
	if !swarIdiomsEnabled || x < f.localBase {
		return false, nil
	}
	root := f.s.back()
	if root == nil || root.kind != ekDeferred || root.op != opAnd || root.typ != mtI64 ||
		root.arg1 == nil || root.arg1.kind != ekValue || root.arg1.st.kind != stConst ||
		uint64(root.arg1.st.cval) != 0xffffffff {
		return false, nil
	}
	save := r.Offset()
	raw := uint32(x - f.localBase)
	matchByte := func(want byte) bool {
		got, err := r.Byte()
		return err == nil && got == want
	}
	matchU32 := func(want uint32) bool {
		got, err := r.U32()
		return err == nil && got == want
	}
	matchI64 := func(want int64) bool {
		got, err := r.I64()
		return err == nil && got == want
	}
	matchStage := func(shift int64, mask uint64, tee bool) bool {
		return matchByte(0x20) && matchU32(raw) && // local.get x
			matchByte(0x42) && matchI64(shift) && // i64.const shift
			matchByte(0x86) && // i64.shl
			matchByte(0x84) && // i64.or
			matchByte(0x42) && matchI64(int64(mask)) &&
			matchByte(0x83) && // i64.and
			(!tee || matchByte(0x22) && matchU32(raw)) // optional local.tee x
	}
	if !matchStage(16, 0x0000ffff0000ffff, true) ||
		!matchStage(8, 0x00ff00ff00ff00ff, false) ||
		!localDeadBeforeWrite(r, raw) {
		if err := r.JumpTo(save); err != nil {
			return false, err
		}
		return false, nil
	}

	f.erase(root.arg1)
	root.op = opSWARWiden4
	root.arg1 = nil
	root.deferDepth = 1 + deferDepthOf(root.arg0)
	f.stats.peep("swar-widen4")
	return true, nil
}

// localDeadBeforeWrite proves that the current value of local x cannot be read
// after the matched sequence. It scans only the current straight-line region:
// the next write or function end proves deadness; a read or control transfer
// rejects the rewrite. The reader is restored before returning.
func localDeadBeforeWrite(r *wasm.Reader, x uint32) bool {
	save := r.Offset()
	defer func() { _ = r.JumpTo(save) }()
	for {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		switch op {
		case 0x0b: // only the final function end proves deadness; a nested end does not
			if r.BytesLeft() == 0 {
				return true
			}
			continue
		case 0x20: // local.get
			y, err := r.U32()
			if err != nil || y == x {
				return false
			}
			continue
		case 0x21, 0x22: // local.set / local.tee overwrites without reading old x
			y, err := r.U32()
			if err != nil {
				return false
			}
			if y == x {
				return true
			}
			continue
		case 0x02, 0x03, 0x04, 0x05, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11:
			return false // do not prove liveness across control flow or calls
		}
		if err := wasm.SkipInstructionImmediate(r, op); err != nil {
			return false
		}
	}
}
