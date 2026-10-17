//go:build amd64

package amd64

import gc "github.com/wago-org/wago/src/core/runtime/gc/native"

// Up to four numeric elements can be loaded before the stores without an overlap
// branch, payload buffer, or loop. Other copies use the bounded wide lowering
// when eligible, or retain the Go copy helper.
func (f *fn) emitDirectGCArrayCopy(dstType, srcType uint32) bool {
	if !gcSharedStubsEnabled {
		return false
	}
	dstScalar, dstFinal, dstOK := f.directGCArrayLayout(dstType)
	srcScalar, srcFinal, srcOK := f.directGCArrayLayout(srcType)
	countRoot := f.s.back()
	if !dstOK || !srcOK || !dstFinal || !srcFinal || dstScalar != srcScalar ||
		countRoot == nil || countRoot.elemKind() != ekValue || countRoot.st.kind != stConst || countRoot.st.typ != mtI32 {
		return false
	}
	count := uint32(countRoot.st.cval)
	if count > 4 {
		return false
	}
	f.flush()
	oldFloor := f.spillFloor
	savedDst := f.allocSpillSlots(1)
	f.spillFloor = savedDst + 1
	f.popValue() // constant count
	srcIndexValue := f.popValue()
	srcValue := f.popValue()
	dstIndexValue := f.popValue()
	dstValue := f.popValue()

	// Resolve both typed objects before range checks, matching helper trap order.
	// The copy-only resolver also proves that ObjHeader.Size fits the handle.
	dst := f.emitSharedCheckedGCObjectPinned(dstValue, dstType, gc.PayloadOffset, gcSharedStubResolveBoundedObject)
	f.stats.addGCHandleResolution()
	f.a.Store64(RSP, f.spillOff(savedDst), dst)
	f.pinned = f.pinned.remove(dst)
	src := f.emitSharedCheckedGCObjectPinned(srcValue, srcType, gc.PayloadOffset, gcSharedStubResolveBoundedObject)
	f.stats.addGCHandleResolution()
	dst = f.allocReg(maskOf(src))
	f.a.Load64(dst, RSP, f.spillOff(savedDst))
	f.pinned = f.pinned.add(dst)
	dstIndex := f.materialize(dstIndexValue)
	f.a.MovRegReg32(dstIndex, dstIndex)
	f.pinned = f.pinned.add(dstIndex)
	srcIndex := f.materialize(srcIndexValue)
	f.a.MovRegReg32(srcIndex, srcIndex)
	f.pinned = f.pinned.add(srcIndex)
	tmp := f.allocReg(maskOf(dst, src, dstIndex, srcIndex))
	f.pinned = f.pinned.add(tmp)
	rangeTrap := condA
	if count == 1 {
		rangeTrap = condAE
	}
	for _, pair := range [2][2]Reg{{dst, dstIndex}, {src, srcIndex}} {
		if count >= 2 {
			// The zero-extended i32 index plus the small count cannot overflow 64 bits.
			// Keep the exclusive end for both logical and physical bounds.
			f.a.AluRI(0, pair[1], int32(count), true)
			f.a.Load32(tmp, pair[0], 8)
			f.a.Cmp64(pair[1], tmp)
			f.trapIf(condA, trapBuiltin)
		} else {
			f.a.Load32(tmp, pair[0], 8)
			f.a.Cmp32(pair[1], tmp)
			f.trapIf(rangeTrap, trapBuiltin)
		}
	}
	if count != 0 {
		required := int32(gc.PayloadOffset) + int32(count)*int32(dstScalar.size)
		if count >= 2 {
			required = int32(gc.PayloadOffset)
		}
		for _, pair := range [2][2]Reg{{dst, dstIndex}, {src, srcIndex}} {
			switch dstScalar.size {
			case 2:
				f.a.ShiftImm(4, pair[1], 1, true)
			case 4, 8:
				f.a.ImulRI(pair[1], int32(dstScalar.size), true)
			}
			f.a.AluRI(0, pair[1], required, true)
			f.a.Load32(tmp, pair[0], 4)
			f.a.Cmp64(pair[1], tmp)
			f.trapIf(condA, trapCastFailure)
		}
		// Address registers are separate from the cached base-object certificate.
		f.a.Add64(dstIndex, dst)
		f.a.Add64(srcIndex, src)
		firstDisp := -int32(count) * int32(dstScalar.size)
		if int32(count)*int32(dstScalar.size) == 16 {
			x := f.allocFReg(0)
			f.mov128LoadIdx(x, srcIndex, RSP, firstDisp)
			f.mov128StoreIdx(dstIndex, RSP, x, firstDisp)
			f.releaseF(x)
		} else {
			values := [4]Reg{tmp}
			used := maskOf(dst, src, dstIndex, srcIndex, tmp)
			for i := 1; i < int(count); i++ {
				values[i] = f.allocReg(used)
				used = used.add(values[i])
			}
			for i := range int(count) {
				disp := firstDisp + int32(i)*int32(dstScalar.size)
				f.a.LoadIdx(values[i], srcIndex, RSP, disp, dstScalar.size, false, dstScalar.size == 8)
			}
			for i := range int(count) {
				disp := firstDisp + int32(i)*int32(dstScalar.size)
				f.a.StoreIdx(dstIndex, RSP, values[i], disp, dstScalar.size)
			}
			for i := 1; i < int(count); i++ {
				f.release(values[i])
			}
		}
	}
	f.pinned = f.pinned.remove(tmp).remove(srcIndex).remove(dstIndex).remove(dst)
	f.release(tmp)
	f.release(srcIndex)
	f.release(dstIndex)
	f.release(dst)
	f.pinned = f.pinned.remove(src)
	f.spillFloor = oldFloor
	f.stats.peep("gc-array-native-copy")
	return true
}
