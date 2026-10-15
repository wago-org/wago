//go:build amd64

package amd64

import gc "github.com/wago-org/wago/src/core/runtime/gc/native"

// A single numeric element can be loaded before the store without an overlap
// branch, payload buffer, or loop. Larger copies retain the Go copy helper.
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
	if count > 1 {
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
	dst, doneDst := f.emitSharedCheckedGCObject(dstValue, dstType, gc.PayloadOffset, gcSharedStubResolveBoundedObject)
	f.stats.addGCHandleResolution()
	f.a.Store64(RSP, f.spillOff(savedDst), dst)
	doneDst()
	src, doneSrc := f.emitSharedCheckedGCObject(srcValue, srcType, gc.PayloadOffset, gcSharedStubResolveBoundedObject)
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
	if count != 0 {
		rangeTrap = condAE
	}
	for _, pair := range [2][2]Reg{{dst, dstIndex}, {src, srcIndex}} {
		f.a.Load32(tmp, pair[0], 8)
		f.a.Cmp32(pair[1], tmp)
		f.trapIf(rangeTrap, trapBuiltin)
	}
	if count != 0 {
		required := int32(gc.PayloadOffset) + int32(dstScalar.size)
		for _, pair := range [2][2]Reg{{dst, dstIndex}, {src, srcIndex}} {
			switch dstScalar.size {
			case 2:
				f.a.ShiftImm(4, pair[1], 1, true)
			case 4, 8:
				f.a.ImulRI(pair[1], int32(dstScalar.size), true)
			}
			f.a.Load32(tmp, pair[0], 4)
			f.a.AluRI(cmpDigit, tmp, required, false)
			f.trapIf(condB, trapCastFailure)
			f.a.AluRI(5, tmp, required, true)
			f.a.Cmp64(pair[1], tmp)
			f.trapIf(condA, trapCastFailure)
		}
		// Address registers are separate from the cached base-object certificate.
		f.a.Add64(dstIndex, dst)
		f.a.Add64(srcIndex, src)
		f.a.LoadIdx(tmp, srcIndex, RSP, int32(gc.PayloadOffset), dstScalar.size, false, dstScalar.size == 8)
		f.a.StoreIdx(dstIndex, RSP, tmp, int32(gc.PayloadOffset), dstScalar.size)
	}
	f.pinned = f.pinned.remove(tmp).remove(srcIndex).remove(dstIndex).remove(dst)
	f.release(tmp)
	f.release(srcIndex)
	f.release(dstIndex)
	f.release(dst)
	doneSrc()
	f.spillFloor = oldFloor
	f.stats.peep("gc-array-native-copy")
	return true
}
