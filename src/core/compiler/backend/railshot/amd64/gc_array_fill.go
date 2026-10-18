//go:build amd64

package amd64

import gc "github.com/wago-org/wago/src/core/runtime/gc/native"

// Bound both emitted code and compile work for the common short numeric fill.
const directGCArrayFillMaxStores = 16
const directGCArrayFillMaxBytes = 64

func directGCArrayFillCount(count uint32, size int) bool {
	return count <= directGCArrayFillMaxStores && uint64(count)*uint64(size) <= directGCArrayFillMaxBytes
}

func (f *fn) emitDirectGCArrayFill(typeIndex uint32) bool {
	scalar, final, ok := f.directGCArrayLayout(typeIndex)
	countRoot := f.s.back()
	if !ok || !final || countRoot == nil || countRoot.elemKind() != ekValue || countRoot.st.kind != stConst || countRoot.st.typ != mtI32 {
		return false
	}
	count := uint32(countRoot.st.cval)
	if !directGCArrayFillCount(count, scalar.size) {
		return false
	}
	valueRoot := baseOfValentBlock(countRoot).prev
	indexRoot := baseOfValentBlock(valueRoot).prev
	local, hasLocal := gcLocalProvenance(baseOfValentBlock(indexRoot).prev)
	f.flush()
	oldFloor := f.spillFloor
	f.spillFloor = f.curSpillSlot()
	f.popValue() // constant count
	value := f.popValue()
	indexValue := f.popValue()
	object := f.popValue()
	bytes := count * uint32(scalar.size)
	required := uint32(gc.PayloadOffset) + bytes
	obj, done := f.emitDirectGCObject(object, typeIndex, uint32(gc.PayloadOffset), local, hasLocal)
	index := f.materialize(indexValue)
	f.a.MovRegReg32(index, index)
	f.pinned = f.pinned.add(index)
	tmp := f.allocReg(maskOf(obj, index))
	f.pinned = f.pinned.add(tmp)
	// Subtract before comparing so an i32 index/count sum cannot wrap.
	f.a.Load32(tmp, obj, 8)
	f.a.AluRI(cmpDigit, tmp, int32(count), false)
	f.trapIf(condB, trapBuiltin)
	f.a.AluRI(5, tmp, int32(count), false)
	f.a.Cmp32(index, tmp)
	f.trapIf(condA, trapBuiltin)
	if count != 0 {
		f.a.ImulRI(index, int32(scalar.size), true)
		// Prove the complete physical range before writing. Check the header
		// minimum explicitly: the inline resolver checks the handle extent.
		f.a.Load32(tmp, obj, 4)
		f.a.AluRI(cmpDigit, tmp, int32(required), false)
		f.trapIf(condB, trapCastFailure)
		f.a.AluRI(5, tmp, int32(required), true)
		f.a.Cmp64(index, tmp)
		f.trapIf(condA, trapCastFailure)
	}
	f.pinned = f.pinned.remove(tmp)
	f.release(tmp)
	if count != 0 {
		if scalar.typ.isFloat() {
			x := f.materializeF(value)
			for i := uint32(0); i < count; i++ {
				f.a.FStoreIdx(obj, index, x, int32(gc.PayloadOffset+i*uint32(scalar.size)), scalar.typ == mtF64)
			}
			f.releaseF(x)
		} else {
			r := f.materialize(value)
			for i := uint32(0); i < count; i++ {
				f.a.StoreIdx(obj, index, r, int32(gc.PayloadOffset+i*uint32(scalar.size)), scalar.size)
			}
			f.release(r)
		}
	} else {
		f.releaseDroppedValue(value)
	}
	f.pinned = f.pinned.remove(index)
	f.release(index)
	done()
	f.spillFloor = oldFloor
	f.stats.peep("gc-array-native-fill")
	return true
}
