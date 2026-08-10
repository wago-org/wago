//go:build amd64

package amd64

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

const (
	gcArrayAllocDefault        uint32 = 16
	gcArrayGet                 uint32 = 17
	gcArrayGetS                uint32 = 18
	gcArrayGetU                uint32 = 19
	gcArraySet                 uint32 = 20
	gcArrayLen                 uint32 = 21
	gcArrayAllocFixed          uint32 = 22
	gcArrayAllocUniform        uint32 = 23
	gcArrayAllocData           uint32 = 24
	gcArrayAllocElem           uint32 = 25
	gcArrayDropElem            uint32 = 26
	gcArrayFill                uint32 = 27
	gcArrayCopy                uint32 = 28
	gcArrayInitData            uint32 = 29
	gcArrayInitElem            uint32 = 30
	gcArrayAllocFixedV128Spill uint32 = 31
	gcArrayAllocDefaultNative  uint32 = 32
	gcArrayAllocUniformNative  uint32 = 33
	gcArrayAllocFixedNative    uint32 = 34
	gcArrayFillNoBarrier       uint32 = 35
)

func (f *fn) emitGCArray(sub uint32, r *wasm.Reader) error {
	if !f.gcArrayHelpers {
		return fmt.Errorf("amd64: unsupported staged array opcode %d without GC array helpers", sub)
	}
	switch sub {
	case 6: // array.new typeidx
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		field, ok := f.stagedArrayType(typeIndex)
		if !ok {
			return fmt.Errorf("amd64: array.new type %d is unavailable", typeIndex)
		}
		valueType := field.Storage().Val()
		if field.Storage().Packed() {
			valueType = wasm.I32
		}
		length, lengthKnown := gcKnownI32Const(f.s.back())
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		helper := uint32(gcArrayAllocUniform)
		if layout, native := nativeGCArrayLayout(f.m, typeIndex); native {
			if length, bytes, static := nativeGCStaticArraySize(f, layout); static && bytes <= uint64(gc.NativeArrayAllocMaxBytes) {
				f.nativeArrayAlloc = gcArrayAllocStubSite{typeIndex: typeIndex, count: length, mode: gcArrayNativeUniform}
				helper = gcArrayAllocUniformNative
			}
		}
		if err := f.callGCStructHelper(helper, []wasm.ValType{valueType, wasm.I32, wasm.I32}, []wasm.ValType{result}); err != nil {
			return err
		}
		if lengthKnown {
			f.markTopConstructorGCRefFact(typeIndex, &length)
		} else {
			f.markTopConstructorGCRefFact(typeIndex, nil)
		}
		return nil
	case 10: // array.new_elem typeidx elemidx
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		elemIndex, err := r.U32()
		if err != nil {
			return err
		}
		field, ok := f.stagedArrayType(typeIndex)
		if !ok || field.Storage().Val().Kind() != wasm.ValRef {
			return fmt.Errorf("amd64: array.new_elem type %d is not a reference array", typeIndex)
		}
		length, lengthKnown := gcKnownI32Const(f.s.back())
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(elemIndex)})
		result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		if err := f.callGCStructHelper(gcArrayAllocElem, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{result}); err != nil {
			return err
		}
		if lengthKnown {
			f.markTopConstructorGCRefFact(typeIndex, &length)
		} else {
			f.markTopConstructorGCRefFact(typeIndex, nil)
		}
		return nil
	case 9: // array.new_data typeidx dataidx
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		dataIndex, err := r.U32()
		if err != nil {
			return err
		}
		field, ok := f.stagedArrayType(typeIndex)
		if !ok {
			return fmt.Errorf("amd64: array.new_data type %d is unavailable", typeIndex)
		}
		if field.Storage().Val().Kind() == wasm.ValRef || (field.Storage().Packed() && field.Storage().Pack() != wasm.PackI8 && field.Storage().Pack() != wasm.PackI16) {
			return fmt.Errorf("amd64: array.new_data type %d has unsupported storage", typeIndex)
		}
		length, lengthKnown := gcKnownI32Const(f.s.back())
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(dataIndex)})
		result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		if err := f.callGCStructHelper(gcArrayAllocData, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{result}); err != nil {
			return err
		}
		if lengthKnown {
			f.markTopConstructorGCRefFact(typeIndex, &length)
		} else {
			f.markTopConstructorGCRefFact(typeIndex, nil)
		}
		return nil
	case 8: // array.new_fixed typeidx length
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		count, err := r.U32()
		if err != nil {
			return err
		}
		field, ok := f.stagedArrayType(typeIndex)
		if !ok {
			return fmt.Errorf("amd64: array.new_fixed type %d is unavailable", typeIndex)
		}
		valueType := field.Storage().Val()
		if field.Storage().Packed() {
			valueType = wasm.I32
		}
		valueSlots := funcTypeSlots([]wasm.ValType{valueType})
		if uint64(count)*uint64(valueSlots)+2 > maxSyncHostSlots {
			if wasm.EqualValType(valueType, wasm.V128) {
				if f.skipDroppedGCConstructor(r, int(count)) || f.deferGCConstructorForDroppedStruct(r, int(count)) {
					return nil
				}
				result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
				return f.callGCArrayFixedV128Spill(typeIndex, count, result)
			}
			maxValues := (maxSyncHostSlots - 2) / valueSlots
			return fmt.Errorf("amd64: array.new_fixed count %d of %s exceeds helper slot bound %d values", count, valueType, maxValues)
		}
		if f.skipDroppedGCConstructor(r, int(count)) || f.deferGCConstructorForDroppedStruct(r, int(count)) {
			return nil
		}
		params := make([]wasm.ValType, 0, int(count)+2)
		for i := uint32(0); i < count; i++ {
			params = append(params, valueType)
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(count)})
		params = append(params, wasm.I32, wasm.I32)
		result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		helper := uint32(gcArrayAllocFixed)
		if layout, native := nativeGCArrayLayout(f.m, typeIndex); native {
			bytes := uint64(gc.PayloadOffset) + uint64(count)*uint64(layout.elemSize)
			bytes = (bytes + 7) &^ 7
			if bytes <= uint64(gc.NativeArrayAllocMaxBytes) {
				f.nativeArrayAlloc = gcArrayAllocStubSite{typeIndex: typeIndex, count: count, mode: gcArrayNativeFixed}
				helper = gcArrayAllocFixedNative
			}
		}
		if err := f.callGCStructHelper(helper, params, []wasm.ValType{result}); err != nil {
			return err
		}
		f.markTopConstructorGCRefFact(typeIndex, &count)
		return nil
	case 7: // array.new_default typeidx
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		if _, ok := f.stagedArrayType(typeIndex); !ok {
			return fmt.Errorf("amd64: array.new_default type %d is unavailable", typeIndex)
		}
		length, lengthKnown := gcKnownI32Const(f.s.back())
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		helper := uint32(gcArrayAllocDefault)
		if layout, native := nativeGCArrayLayout(f.m, typeIndex); native && (!layout.ref || layout.nullable) {
			if length, bytes, static := nativeGCStaticArraySize(f, layout); static && bytes <= uint64(gc.NativeArrayAllocMaxBytes) {
				f.nativeArrayAlloc = gcArrayAllocStubSite{typeIndex: typeIndex, count: length, mode: gcArrayNativeDefault}
				helper = gcArrayAllocDefaultNative
			}
		}
		if err := f.callGCStructHelper(helper, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{result}); err != nil {
			return err
		}
		if lengthKnown {
			f.markTopConstructorGCRefFact(typeIndex, &length)
		} else {
			f.markTopConstructorGCRefFact(typeIndex, nil)
		}
		return nil
	case 11, 12, 13: // array.get / array.get_s / array.get_u
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		field, ok := f.stagedArrayType(typeIndex)
		if !ok {
			return fmt.Errorf("amd64: array.get type %d is unavailable", typeIndex)
		}
		helper := uint32(gcArrayGet)
		resultType := field.Storage().Val()
		if sub == 12 || sub == 13 {
			if !field.Storage().Packed() {
				return fmt.Errorf("amd64: array.get_s/u type %d is not packed", typeIndex)
			}
			resultType = wasm.I32
			if sub == 12 {
				helper = gcArrayGetS
			} else {
				helper = gcArrayGetU
			}
		} else if field.Storage().Packed() {
			return fmt.Errorf("amd64: plain array.get cannot access packed type %d", typeIndex)
		}
		if f.emitDirectGCArrayGet(typeIndex, helper) {
			return nil
		}
		indexRoot := f.s.back()
		f.refineGCDereferencedObject(baseOfValentBlock(indexRoot).prev)
		if sub == 11 && field.Storage().Val().Kind() == wasm.ValRef && gcFrameRefType(f.m, field.Storage().Val()) {
			if target, ok := f.stagedGCType(typeIndex); ok && target.Final {
				return f.emitNativeFinalArrayRefGet(typeIndex)
			}
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		object := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		return f.callGCStructHelper(helper, []wasm.ValType{object, wasm.I32, wasm.I32}, []wasm.ValType{resultType})
	case 14: // array.set typeidx
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		field, ok := f.stagedArrayType(typeIndex)
		if !ok {
			return fmt.Errorf("amd64: array.set type %d is unavailable", typeIndex)
		}
		if field.Mut() != wasm.Var {
			return fmt.Errorf("amd64: array.set type %d is immutable", typeIndex)
		}
		valueType := field.Storage().Val()
		if field.Storage().Packed() {
			valueType = wasm.I32
		}
		valueRoot := f.s.back()
		indexRoot := baseOfValentBlock(valueRoot).prev
		objectRoot := baseOfValentBlock(indexRoot).prev
		f.refineGCDereferencedObject(objectRoot)
		if f.emitDirectGCArraySet(typeIndex) {
			return nil
		}
		if target, found := f.stagedGCType(typeIndex); found && target.Final && field.Storage().Val().Kind() == wasm.ValRef && gcFrameRefType(f.m, field.Storage().Val()) {
			barrierState := shared.SelectGCStoreBarrier(gcRefFact(objectRoot), gcRefFact(valueRoot))
			f.publishGCStoredChild(objectRoot, valueRoot)
			if !barrierState.NeedsBarrier() && f.emitDirectGCArrayRefSetNoBarrier(typeIndex, barrierState) {
				return nil
			}
			f.gcOpcodeBarrier = true
			f.recordGCBarrierState(shared.GCBarrierSlowBarrier)
			return f.emitNativeCardSafeArrayRefSet(typeIndex, valueType)
		}
		if field.Storage().Val().Kind() == wasm.ValRef {
			f.publishGCStoredChild(objectRoot, valueRoot)
			f.gcOpcodeBarrier = true
			f.recordGCBarrierState(shared.GCBarrierSlowBarrier)
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		object := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		return f.callGCStructHelper(gcArraySet, []wasm.ValType{object, wasm.I32, valueType, wasm.I32}, nil)
	case 15: // array.len
		f.refineGCDereferencedObject(f.s.back())
		fact := gcRefFact(f.s.back())
		if length, known := fact.KnownArrayLength(); known && fact.Nullability() == shared.GCKnownNonNull {
			f.dropValue()
			f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(length)})
			f.stats.peep("gc-known-array-len")
			f.stats.peep("gc-array-len-elide")
			return nil
		}
		observed := false
		if _, typeIndex, exact := f.topExactGCLocal(); exact {
			if f.tryForwardGCArrayLen(typeIndex) {
				return nil
			}
			f.observeGCArrayLen(typeIndex)
			observed = true
			if f.emitDirectGCArrayLen(typeIndex) {
				f.recordGCArrayLenResult()
				return nil
			}
		}
		object := wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(wasm.HeapArray), false))
		if err := f.callGCStructHelper(gcArrayLen, []wasm.ValType{object}, []wasm.ValType{wasm.I32}); err != nil {
			return err
		}
		if observed {
			f.recordGCArrayLenResult()
		}
		return nil
	case 16: // array.fill typeidx
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		field, ok := f.stagedArrayType(typeIndex)
		if !ok || field.Mut() != wasm.Var {
			return fmt.Errorf("amd64: array.fill type %d is unavailable or immutable", typeIndex)
		}
		valueType := field.Storage().Val()
		if field.Storage().Packed() {
			valueType = wasm.I32
		}
		lengthRoot := f.s.back()
		valueRoot := baseOfValentBlock(lengthRoot).prev
		startRoot := baseOfValentBlock(valueRoot).prev
		objectRoot := baseOfValentBlock(startRoot).prev
		f.refineGCDereferencedObject(objectRoot)
		helper := uint32(gcArrayFill)
		if field.Storage().Val().Kind() == wasm.ValRef && gcFrameRefType(f.m, field.Storage().Val()) {
			barrierState := shared.SelectGCStoreBarrier(gcRefFact(objectRoot), gcRefFact(valueRoot))
			f.recordGCBarrierState(barrierState)
			f.publishGCStoredChild(objectRoot, valueRoot)
			if !barrierState.NeedsBarrier() {
				helper = gcArrayFillNoBarrier
				f.stats.peep("gc-bulk-barrier-elide")
			} else {
				f.gcOpcodeBarrier = true
			}
		} else {
			f.recordGCBarrierState(shared.GCBarrierNoBarrier)
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		object := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		return f.callGCStructHelper(helper, []wasm.ValType{object, wasm.I32, valueType, wasm.I32, wasm.I32}, nil)
	case 18: // array.init_data typeidx dataidx
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		dataIndex, err := r.U32()
		if err != nil {
			return err
		}
		field, ok := f.stagedArrayType(typeIndex)
		if !ok || field.Mut() != wasm.Var || (!field.Storage().Packed() && field.Storage().Val().Kind() == wasm.ValRef) {
			return fmt.Errorf("amd64: array.init_data type %d is unavailable", typeIndex)
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(dataIndex)})
		object := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		return f.callGCStructHelper(gcArrayInitData, []wasm.ValType{object, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}, nil)
	case 19: // array.init_elem typeidx elemidx
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		elemIndex, err := r.U32()
		if err != nil {
			return err
		}
		field, ok := f.stagedArrayType(typeIndex)
		if !ok || field.Mut() != wasm.Var || field.Storage().Val().Kind() != wasm.ValRef {
			return fmt.Errorf("amd64: array.init_elem type %d is unavailable", typeIndex)
		}
		if gcFrameRefType(f.m, field.Storage().Val()) {
			f.gcOpcodeBarrier = true
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(elemIndex)})
		object := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		return f.callGCStructHelper(gcArrayInitElem, []wasm.ValType{object, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}, nil)
	case 17: // array.copy dsttype srcType
		dstType, err := r.U32()
		if err != nil {
			return err
		}
		srcType, err := r.U32()
		if err != nil {
			return err
		}
		dstField, ok := f.stagedArrayType(dstType)
		if !ok || dstField.Mut() != wasm.Var {
			return fmt.Errorf("amd64: array.copy destination type %d is unavailable or immutable", dstType)
		}
		if _, ok := f.stagedArrayType(srcType); !ok {
			return fmt.Errorf("amd64: array.copy source type %d is unavailable", srcType)
		}
		if dstField.Storage().Val().Kind() == wasm.ValRef && gcFrameRefType(f.m, dstField.Storage().Val()) {
			f.gcOpcodeBarrier = true
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(dstType)})
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(srcType)})
		dst := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: dstType}), false))
		src := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: srcType}), false))
		return f.callGCStructHelper(gcArrayCopy, []wasm.ValType{dst, wasm.I32, src, wasm.I32, wasm.I32, wasm.I32, wasm.I32}, nil)
	default:
		return fmt.Errorf("amd64: unsupported staged array opcode %d", sub)
	}
}

// callGCArrayFixedV128Spill handles vector fixed constructors whose flattened
// values do not fit in the 64-slot synchronous control frame. The values are
// already resident in contiguous canonical foreign-stack spill slots. Pass one
// off-heap pointer to those slots, then discard the logical operands after the
// helper returns the constructed reference. The parked helper copies the bytes
// before native execution resumes; no Go pointer enters native state.
func (f *fn) callGCArrayFixedV128Spill(typeIndex, count uint32, resultType wasm.ValType) error {
	roots := f.rootsBottomToTop()
	if uint64(count) > uint64(len(roots)) {
		return fmt.Errorf("amd64: array.new_fixed count %d exceeds operand depth %d", count, len(roots))
	}
	first := len(roots) - int(count)
	firstSlot := 0
	for i := 0; i < first; i++ {
		typ := roots[i].st.typ
		if roots[i].kind == ekDeferred && roots[i].typ != mtNone {
			typ = roots[i].typ
		}
		firstSlot += typ.stackSlots()
	}
	f.flush()
	ptr := f.allocReg(0)
	f.a.LeaRsp(ptr, f.spillOff(firstSlot))
	f.pushReg(ptr, mtI64)
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(count)})
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
	if err := f.callGCStructHelper(gcArrayAllocFixedV128Spill, []wasm.ValType{wasm.I64, wasm.I32, wasm.I32}, []wasm.ValType{resultType}); err != nil {
		return err
	}
	result := f.materialize(f.popValue())
	for i := uint32(0); i < count; i++ {
		f.popValue()
	}
	f.pushReg(result, mtI64)
	return nil
}

func nativeGCStaticArraySize(f *fn, layout nativeGCArrayAllocLayout) (length uint32, objectBytes uint64, ok bool) {
	if f == nil || f.s == nil {
		return 0, 0, false
	}
	value := f.s.back()
	if value != nil {
		value = value.prev // trailing type-index constant was pushed before admission
	}
	if value == nil || value.kind != ekValue || value.st.kind != stConst || value.st.typ != mtI32 {
		return 0, 0, false
	}
	length = uint32(value.st.cval)
	objectBytes = uint64(gc.PayloadOffset) + uint64(length)*uint64(layout.elemSize)
	objectBytes = (objectBytes + 7) &^ 7
	return length, objectBytes, objectBytes <= uint64(^uint32(0))
}

func stagedArrayType(m *wasm.Module, typeIndex uint32) (wasm.FieldType, bool) {
	if m == nil {
		return wasm.FieldType{}, false
	}
	index := typeIndex
	for _, group := range m.Types {
		if index < uint32(len(group.SubTypes)) {
			sub := group.SubTypes[index]
			return sub.Comp.Array, sub.Comp.Kind == wasm.CompArray
		}
		index -= uint32(len(group.SubTypes))
	}
	return wasm.FieldType{}, false
}
