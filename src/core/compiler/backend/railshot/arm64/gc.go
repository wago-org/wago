//go:build arm64

package arm64

import (
	"fmt"
	"sort"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
)

// GC helpers share the synchronous parked-host dispatch ABI with amd64. Keep
// these values stable: src/wago dispatches them before ordinary imports.
const (
	gcStructDispatchBit        uint32 = 1 << 30
	gcStructAllocDefault       uint32 = 1
	gcStructGet                uint32 = 2
	gcStructSet                uint32 = 3
	gcStructGetS               uint32 = 4
	gcStructGetU               uint32 = 5
	gcStructRefTest            uint32 = 6
	gcStructTableSet           uint32 = 7
	gcAnyConvertExtern         uint32 = 8
	gcExternConvertAny         uint32 = 9
	gcStructRefCast            uint32 = 10
	gcStructAllocOne           uint32 = 11
	gcStructFinalCastGet       uint32 = 12
	gcStructFinalCastArrayLen  uint32 = 13
	gcFuncRefTest              uint32 = 14
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
)

func (f *fn) emitFB(r *wasm.Reader) error {
	before := f.a.Len()
	sub, err := r.U32()
	if err != nil {
		return err
	}
	defer func() { f.recordGCOpcodeBytes(sub, f.a.Len()-before) }()
	if sub >= 6 && sub <= 19 {
		return f.emitGCArray(sub, r)
	}
	if sub == 20 || sub == 21 { // ref.test / ref.test_null
		heap, err := r.S33()
		if err != nil {
			return err
		}
		nullable := sub == 21
		if heap >= 0 {
			top := f.s.back()
			if _, targetIsFunc := f.m.TypeFunc(uint32(heap)); targetIsFunc && top != nil && top.kind == ekValue && top.st.kind == stFuncRef && top.st.idx >= uint32(f.m.ImportedFuncCount()) && top.st.idx < uint32(len(f.m.FuncTypes)) {
				f.popValue()
				actual := wasm.Ref(false, wasm.IndexedHeap(f.m.FuncTypes[top.st.index()]), false)
				required := wasm.Ref(nullable, wasm.IndexedHeap(wasm.TypeIdx{Index: uint32(heap)}), false)
				matched := int64(0)
				if f.m.ReferenceTypeSubtype(actual, required) {
					matched = 1
				}
				f.pushValue(storage{kind: stConst, typ: mtI32, cval: matched})
				return nil
			}
		}
		_, targetIsFunc := f.m.TypeFunc(uint32(heap))
		if f.gcTypeSubtypingRefTest && heap >= 0 && targetIsFunc {
			return f.emitDynamicFunctionSubtypeTest(uint32(heap), nullable)
		}
		if heap == -16 || heap == -17 || heap == -13 || heap == -14 { // func, extern, nofunc, noextern
			value := f.materialize(f.popValue())
			switch {
			case heap == -16 || heap == -17:
				if nullable {
					f.a.MovImm32(value, 1)
				} else {
					f.cmpImm(value, 0, true)
					f.a.Cset32(value, condNE)
				}
			case nullable:
				f.cmpImm(value, 0, true)
				f.a.Cset32(value, condE)
			default:
				f.a.MovImm32(value, 0)
			}
			f.pushReg(value, mtI32)
			return nil
		}
		if !f.gcStructHelpers {
			return fmt.Errorf("arm64: ref.test requires GC helpers")
		}
		f.pushValue(storage{kind: stConst, typ: mtI64, cval: heap})
		nullableFlag := int64(0)
		if nullable {
			nullableFlag = 1
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: nullableFlag})
		f.pushValue(storage{kind: stConst, typ: mtI32}) // ref.test does not admit exact heap markers
		anyref := wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(wasm.HeapAny), false))
		return f.callGCStructHelper(gcStructRefTest, []wasm.ValType{anyref, wasm.I64, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32})
	}
	if sub == 24 || sub == 25 { // br_on_cast / br_on_cast_fail
		return f.emitGCBranchCast(sub, r)
	}
	if sub == 22 || sub == 23 { // ref.cast / ref.cast_null
		heap, exactTarget, err := readRefHeapTypeImmediate(r)
		if err != nil {
			return err
		}
		if f.gcStructHelpers && heap >= 0 {
			if fused, err := f.tryFuseFinalCastStructGet(uint32(heap), sub == 23, r); fused || err != nil {
				return err
			}
			if fused, err := f.tryFuseFinalCastArrayLen(uint32(heap), sub == 23, r); fused || err != nil {
				return err
			}
		}
		if f.gcTypeSubtypingRefTest && heap >= 0 {
			if _, targetIsFunc := f.m.TypeFunc(uint32(heap)); targetIsFunc {
				value := f.popValue()
				gcRoot := value.st.hasGCRoot()
				ref := f.materialize(value)
				f.emitLocalFunctionSubtypeIdentityCheck(ref, uint32(heap), sub == 23, exactTarget, trapCastFailure)
				f.pushReg(ref, mtI64).st.setGCRoot(gcRoot)
				return nil
			}
		}
		if !moduleHasCollectorTypes(f.m) {
			value := f.popValue()
			gcRoot := value.st.hasGCRoot()
			ref := f.materialize(value)
			nullable := sub == 23
			var done int
			if nullable {
				done = f.zeroBranch(ref, true, true)
			} else {
				f.trapIfZero(ref, true, true, trapCastFailure)
			}
			switch heap {
			case -20, -19, -18: // i31, eq, any
				tag := f.allocReg(maskOf(ref))
				f.a.AndImm64(tag, ref, 1)
				f.cmpImm(tag, 0, true)
				f.release(tag)
				f.trapIf(condE, trapCastFailure)
			case -15, -21, -22: // none, struct, array: no non-null inhabitant without collector types
				f.trapAlways(trapCastFailure)
			default:
				return fmt.Errorf("arm64: ref.cast heap %d requires a live collector", heap)
			}
			if nullable {
				f.a.PatchBranch19(done, f.a.Len())
			}
			f.pushReg(ref, mtI64).st.setGCRoot(gcRoot)
			return nil
		}
		if !f.gcStructHelpers {
			return fmt.Errorf("arm64: ref.cast requires GC helpers")
		}
		f.pushValue(storage{kind: stConst, typ: mtI64, cval: heap})
		nullable := int64(0)
		if sub == 23 {
			nullable = 1
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: nullable})
		exact := int64(0)
		if exactTarget {
			exact = 1
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: exact})
		anyref := wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(wasm.HeapAny), false))
		return f.callGCStructHelper(gcStructRefCast, []wasm.ValType{anyref, wasm.I64, wasm.I32, wasm.I32}, []wasm.ValType{anyref})
	}
	if sub == 26 || sub == 27 {
		if !f.gcStructHelpers {
			return fmt.Errorf("arm64: extern conversion requires GC helpers")
		}
		if sub == 26 {
			return f.callGCStructHelper(gcAnyConvertExtern, []wasm.ValType{wasm.ExternRef}, []wasm.ValType{wasm.AnyRef})
		}
		return f.callGCStructHelper(gcExternConvertAny, []wasm.ValType{wasm.AnyRef}, []wasm.ValType{wasm.ExternRef})
	}
	if sub >= 28 && sub <= 30 {
		value := f.materialize(f.popValue())
		switch sub {
		case 28: // ref.i31
			f.a.LslImm(value, value, 1, true)
			if !f.a.OrrImm32(value, value, 1) {
				panic("arm64: i31 tag immediate is not encodable")
			}
			f.pushReg(value, mtI64).st.setGCRoot(f.tracksGCFrameRoots())
		case 29: // i31.get_s
			f.trapIfZero(value, false, true, trapNullReference)
			f.a.AsrImm(value, value, 1, true)
			f.pushReg(value, mtI32)
		case 30: // i31.get_u
			f.trapIfZero(value, false, true, trapNullReference)
			f.a.LsrImm(value, value, 1, true)
			f.pushReg(value, mtI32)
		}
		return nil
	}
	if !f.gcStructHelpers {
		return fmt.Errorf("arm64: unsupported 0xfb opcode %d without GC struct helpers", sub)
	}
	switch sub {
	case 0: // struct.new
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		st, ok := f.stagedStructType(typeIndex)
		if !ok {
			return fmt.Errorf("arm64: struct.new type %d is unavailable", typeIndex)
		}
		params := make([]wasm.ValType, 0, len(st.Comp.Fields)+1)
		for _, field := range st.Comp.Fields {
			valueType := field.Storage().Val()
			if field.Storage().Packed() {
				valueType = wasm.I32
			}
			params = append(params, valueType)
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		params = append(params, wasm.I32)
		result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		return f.callGCStructHelper(gcStructAllocOne, params, []wasm.ValType{result})
	case 1: // struct.new_default
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		if _, ok := f.stagedStructType(typeIndex); !ok {
			return fmt.Errorf("arm64: struct.new_default type %d is unavailable", typeIndex)
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		return f.callGCStructHelper(gcStructAllocDefault, []wasm.ValType{wasm.I32}, []wasm.ValType{result})
	case 2, 3, 4:
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		fieldIndex, err := r.U32()
		if err != nil {
			return err
		}
		field, ok := f.stagedStructField(typeIndex, fieldIndex)
		if !ok {
			return fmt.Errorf("arm64: struct.get type %d field %d is unavailable", typeIndex, fieldIndex)
		}
		helper, resultType := uint32(gcStructGet), field.Storage().Val()
		if sub == 3 || sub == 4 {
			if !field.Storage().Packed() {
				return fmt.Errorf("arm64: struct.get_s/u field is not packed")
			}
			resultType = wasm.I32
			if sub == 3 {
				helper = gcStructGetS
			} else {
				helper = gcStructGetU
			}
		} else if field.Storage().Packed() {
			return fmt.Errorf("arm64: plain struct.get cannot access a packed field")
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(fieldIndex)})
		object := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		return f.callGCStructHelper(helper, []wasm.ValType{object, wasm.I32, wasm.I32}, []wasm.ValType{resultType})
	case 5:
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		fieldIndex, err := r.U32()
		if err != nil {
			return err
		}
		field, ok := f.stagedStructField(typeIndex, fieldIndex)
		if !ok || field.Mut() != wasm.Var {
			return fmt.Errorf("arm64: struct.set type %d field %d is unavailable or immutable", typeIndex, fieldIndex)
		}
		valueType := field.Storage().Val()
		if field.Storage().Packed() {
			valueType = wasm.I32
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(fieldIndex)})
		object := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		return f.callGCStructHelper(gcStructSet, []wasm.ValType{object, valueType, wasm.I32, wasm.I32}, nil)
	default:
		return fmt.Errorf("arm64: unsupported staged 0xfb opcode %d", sub)
	}
}

func (f *fn) emitGCArray(sub uint32, r *wasm.Reader) error {
	if !f.gcArrayHelpers {
		return fmt.Errorf("arm64: unsupported array opcode %d without GC array helpers", sub)
	}
	switch sub {
	case 6: // array.new
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		field, ok := f.stagedArrayType(typeIndex)
		if !ok {
			return fmt.Errorf("arm64: array.new type %d is unavailable", typeIndex)
		}
		valueType := field.Storage().Val()
		if field.Storage().Packed() {
			valueType = wasm.I32
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		return f.callGCStructHelper(gcArrayAllocUniform, []wasm.ValType{valueType, wasm.I32, wasm.I32}, []wasm.ValType{result})
	case 7: // array.new_default
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		if _, ok := f.stagedArrayType(typeIndex); !ok {
			return fmt.Errorf("arm64: array.new_default type %d is unavailable", typeIndex)
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		return f.callGCStructHelper(gcArrayAllocDefault, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{result})
	case 8: // array.new_fixed
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
			return fmt.Errorf("arm64: array.new_fixed type %d is unavailable", typeIndex)
		}
		valueType := field.Storage().Val()
		if field.Storage().Packed() {
			valueType = wasm.I32
		}
		valueSlots := funcTypeSlots([]wasm.ValType{valueType})
		if uint64(count)*uint64(valueSlots)+2 > maxSyncHostSlots {
			result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
			return f.callGCArrayFixedSpill(typeIndex, count, result)
		}
		params := make([]wasm.ValType, 0, int(count)+2)
		for i := uint32(0); i < count; i++ {
			params = append(params, valueType)
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(count)})
		params = append(params, wasm.I32, wasm.I32)
		result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		return f.callGCStructHelper(gcArrayAllocFixed, params, []wasm.ValType{result})
	case 9, 10: // array.new_data / array.new_elem
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		segmentIndex, err := r.U32()
		if err != nil {
			return err
		}
		field, ok := f.stagedArrayType(typeIndex)
		if !ok {
			return fmt.Errorf("arm64: array constructor type %d is unavailable", typeIndex)
		}
		helper := uint32(gcArrayAllocData)
		if sub == 10 {
			if field.Storage().Val().Kind() != wasm.ValRef {
				return fmt.Errorf("arm64: array.new_elem type %d is not a reference array", typeIndex)
			}
			helper = gcArrayAllocElem
		} else if field.Storage().Val().Kind() == wasm.ValRef {
			return fmt.Errorf("arm64: array.new_data type %d has reference storage", typeIndex)
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(segmentIndex)})
		result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		return f.callGCStructHelper(helper, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{result})
	case 11, 12, 13:
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		field, ok := f.stagedArrayType(typeIndex)
		if !ok {
			return fmt.Errorf("arm64: array.get type %d is unavailable", typeIndex)
		}
		helper, resultType := uint32(gcArrayGet), field.Storage().Val()
		if sub == 12 || sub == 13 {
			if !field.Storage().Packed() {
				return fmt.Errorf("arm64: array.get_s/u type %d is not packed", typeIndex)
			}
			resultType = wasm.I32
			if sub == 12 {
				helper = gcArrayGetS
			} else {
				helper = gcArrayGetU
			}
		} else if field.Storage().Packed() {
			return fmt.Errorf("arm64: plain array.get cannot access packed storage")
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		object := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		return f.callGCStructHelper(helper, []wasm.ValType{object, wasm.I32, wasm.I32}, []wasm.ValType{resultType})
	case 14:
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		field, ok := f.stagedArrayType(typeIndex)
		if !ok || field.Mut() != wasm.Var {
			return fmt.Errorf("arm64: array.set type %d is unavailable or immutable", typeIndex)
		}
		valueType := field.Storage().Val()
		if field.Storage().Packed() {
			valueType = wasm.I32
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		object := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		return f.callGCStructHelper(gcArraySet, []wasm.ValType{object, wasm.I32, valueType, wasm.I32}, nil)
	case 15:
		object := wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(wasm.HeapArray), false))
		return f.callGCStructHelper(gcArrayLen, []wasm.ValType{object}, []wasm.ValType{wasm.I32})
	case 16:
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		field, ok := f.stagedArrayType(typeIndex)
		if !ok || field.Mut() != wasm.Var {
			return fmt.Errorf("arm64: array.fill type %d is unavailable or immutable", typeIndex)
		}
		valueType := field.Storage().Val()
		if field.Storage().Packed() {
			valueType = wasm.I32
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		object := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		return f.callGCStructHelper(gcArrayFill, []wasm.ValType{object, wasm.I32, valueType, wasm.I32, wasm.I32}, nil)
	case 17:
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
			return fmt.Errorf("arm64: array.copy destination is unavailable")
		}
		if _, ok := f.stagedArrayType(srcType); !ok {
			return fmt.Errorf("arm64: array.copy source is unavailable")
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(dstType)})
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(srcType)})
		dst := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: dstType}), false))
		src := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: srcType}), false))
		return f.callGCStructHelper(gcArrayCopy, []wasm.ValType{dst, wasm.I32, src, wasm.I32, wasm.I32, wasm.I32, wasm.I32}, nil)
	case 18, 19:
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		segmentIndex, err := r.U32()
		if err != nil {
			return err
		}
		field, ok := f.stagedArrayType(typeIndex)
		if !ok || field.Mut() != wasm.Var {
			return fmt.Errorf("arm64: array.init type %d is unavailable", typeIndex)
		}
		helper := uint32(gcArrayInitData)
		if sub == 19 {
			helper = gcArrayInitElem
			if field.Storage().Val().Kind() != wasm.ValRef {
				return fmt.Errorf("arm64: array.init_elem requires reference storage")
			}
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(segmentIndex)})
		object := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		return f.callGCStructHelper(helper, []wasm.ValType{object, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}, nil)
	default:
		return fmt.Errorf("arm64: unsupported array opcode %d", sub)
	}
}

func (f *fn) tryFuseFinalCastStructGet(typeIndex uint32, nullable bool, r *wasm.Reader) (bool, error) {
	st, ok := f.stagedStructType(typeIndex)
	if !ok || !st.Final {
		return false, nil
	}
	start := r.Offset()
	op, err := r.Byte()
	if err != nil {
		return false, nil
	}
	if op != 0xfb {
		_ = r.JumpTo(start)
		return false, nil
	}
	sub, err := r.U32()
	if err != nil {
		return false, err
	}
	if sub < 2 || sub > 4 {
		_ = r.JumpTo(start)
		return false, nil
	}
	accessType, err := r.U32()
	if err != nil {
		return false, err
	}
	fieldIndex, err := r.U32()
	if err != nil {
		return false, err
	}
	field, ok := f.stagedStructField(accessType, fieldIndex)
	if !ok || accessType != typeIndex || (sub == 2) == field.Storage().Packed() {
		_ = r.JumpTo(start)
		return false, nil
	}
	resultType := field.Storage().Val()
	if field.Storage().Packed() {
		resultType = wasm.I32
	}
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(fieldIndex)})
	if nullable {
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: 1})
	} else {
		f.pushValue(storage{kind: stConst, typ: mtI32})
	}
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(sub - 2)})
	anyref := wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(wasm.HeapAny), false))
	f.stats.peep("final-cast-struct-get-fuse")
	return true, f.callGCStructHelper(gcStructFinalCastGet, []wasm.ValType{anyref, wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{resultType})
}

func (f *fn) tryFuseFinalCastArrayLen(typeIndex uint32, nullable bool, r *wasm.Reader) (bool, error) {
	st, ok := f.stagedArraySubtype(typeIndex)
	if !ok || !st.Final {
		return false, nil
	}
	start := r.Offset()
	op, err := r.Byte()
	if err != nil {
		return false, nil
	}
	if op != 0xfb {
		_ = r.JumpTo(start)
		return false, nil
	}
	sub, err := r.U32()
	if err != nil {
		return false, err
	}
	if sub != 15 {
		_ = r.JumpTo(start)
		return false, nil
	}
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
	if nullable {
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: 1})
	} else {
		f.pushValue(storage{kind: stConst, typ: mtI32})
	}
	anyref := wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(wasm.HeapAny), false))
	f.stats.peep("final-cast-array-len-fuse")
	return true, f.callGCStructHelper(gcStructFinalCastArrayLen, []wasm.ValType{anyref, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32})
}

func (f *fn) emitDynamicFunctionSubtypeTest(targetType uint32, nullable bool) error {
	subtypes, ok := f.m.FunctionSubtypeTypeIndexes(targetType)
	if !ok {
		return fmt.Errorf("arm64: function ref.test target type %d is unavailable", targetType)
	}
	savedLocals := append([]localDef(nil), f.locals...)
	f.flush()
	valueElem := f.s.back()
	if valueElem == f.s.head || valueElem.kind != ekValue || valueElem.st.kind != stSlot {
		return fmt.Errorf("arm64: dynamic function ref.test lost canonical operand")
	}
	value := f.allocReg(0)
	f.ld64(value, SP, f.spillOff(valueElem.st.slotIndex()))
	nullSite := f.zeroBranch(value, true, true)
	base := f.allocReg(maskOf(value))
	f.ld64(base, linMemReg, -int32(offFuncRefDescPtr))
	f.cmpRR(value, base, true)
	unknownSites := []int{f.a.Bcond(condBE)}
	end := f.allocReg(maskOf(value, base))
	f.a.MovImm64(end, uint64((f.m.ImportedFuncCount()+len(f.m.FuncTypes)+1)*runtime.FuncRefDescBytes))
	f.a.Add64(end, base, end)
	f.cmpRR(value, end, true)
	unknownSites = append(unknownSites, f.a.Bcond(condAE))
	f.a.Sub64(value, value, base)
	f.a.MovImm64(end, runtime.FuncRefDescBytes)
	quotient := f.allocReg(maskOf(value, base, end))
	f.a.Udiv64(quotient, value, end)
	remainder := f.allocReg(maskOf(value, base, end, quotient))
	f.a.Msub64(remainder, quotient, end, value)
	unknownSites = append(unknownSites, f.zeroBranch(remainder, true, false))
	f.a.SubImm64(quotient, quotient, 1)
	f.ld64(base, base, runtime.TableEntryCodePtrOffset)
	unknownSites = append(unknownSites, f.zeroBranch(base, true, true))
	f.a.LslImm64(end, quotient, 2)
	f.a.Add64(end, base, end)
	f.ld32(quotient, end, 0)
	trueSites := make([]int, 0, len(subtypes)+1)
	for _, typeIndex := range subtypes {
		f.a.MovImm32(end, int32(typeIndex))
		f.cmpRR(quotient, end, false)
		trueSites = append(trueSites, f.a.Bcond(condE))
	}
	f.a.MovImm32(end, -1)
	f.cmpRR(quotient, end, false)
	unknownSites = append(unknownSites, f.a.Bcond(condE))
	falseSite := f.a.Branch()
	trueLabel := f.a.Len()
	if nullable && !f.a.PatchBranch19(nullSite, trueLabel) {
		return fmt.Errorf("arm64: dynamic function ref.test nullable edge exceeds conditional branch range")
	}
	for _, site := range trueSites {
		if !f.a.PatchBranch19(site, trueLabel) {
			return fmt.Errorf("arm64: dynamic function ref.test subtype set exceeds conditional branch range")
		}
	}
	f.a.MovImm32(quotient, 1)
	classifiedTrue := f.a.Branch()
	falseLabel := f.a.Len()
	if !nullable && !f.a.PatchBranch19(nullSite, falseLabel) {
		return fmt.Errorf("arm64: dynamic function ref.test null edge exceeds conditional branch range")
	}
	if !f.a.PatchBranch26(falseSite, falseLabel) {
		return fmt.Errorf("arm64: dynamic function ref.test false edge exceeds branch range")
	}
	f.a.MovImm32(quotient, 0)
	classifiedFalse := f.a.Branch()
	unknownLabel := f.a.Len()
	for _, site := range unknownSites {
		if !f.a.PatchBranch19(site, unknownLabel) {
			return fmt.Errorf("arm64: dynamic function ref.test unknown edge exceeds conditional branch range")
		}
	}
	f.a.MovImm32(quotient, 2)
	classDone := f.a.Len()
	if !f.a.PatchBranch26(classifiedTrue, classDone) || !f.a.PatchBranch26(classifiedFalse, classDone) {
		return fmt.Errorf("arm64: dynamic function ref.test classifier join exceeds branch range")
	}
	f.release(value)
	f.release(base)
	f.release(end)
	f.release(remainder)
	f.cmpImm(quotient, 2, false)
	known := f.a.Bcond(condNE)
	resultReg := quotient
	f.release(quotient)

	f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(targetType)})
	if nullable {
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: 1})
	} else {
		f.pushValue(storage{kind: stConst, typ: mtI32})
	}
	funcref := wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(wasm.HeapFunc), false))
	if err := f.callGCStructHelper(gcFuncRefTest, []wasm.ValType{funcref, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}); err != nil {
		return err
	}
	if f.usesCalls {
		for x := 0; x < f.nLocals; x++ {
			local := savedLocals[x]
			if local.reg != regNone && (local.state == lsReg || local.state == lsStackReg) {
				f.loadLocalReg(x, local.reg, local.isFloat)
			}
		}
		f.locals = savedLocals
	}
	f.flush()
	result := f.s.back()
	if result == f.s.head || result.kind != ekValue || result.st.kind != stSlot {
		return fmt.Errorf("arm64: dynamic function ref.test lost canonical result")
	}
	done := f.a.Branch()
	if !f.a.PatchBranch19(known, f.a.Len()) {
		return fmt.Errorf("arm64: dynamic function ref.test known edge exceeds conditional branch range")
	}
	f.st32(SP, f.spillOff(result.st.slotIndex()), resultReg)
	if !f.a.PatchBranch26(done, f.a.Len()) {
		return fmt.Errorf("arm64: dynamic function ref.test result join exceeds branch range")
	}
	return nil
}

func (f *fn) emitLocalFunctionSubtypeIdentityCheck(value Reg, targetType uint32, nullable, exactTarget bool, trapCode uint32) {
	success := make([]int, 0, f.m.ImportedFuncCount()+len(f.m.FuncTypes)+1)
	if nullable {
		success = append(success, f.zeroBranch(value, true, true))
	}
	base := f.allocReg(maskOf(value))
	f.ld64(base, linMemReg, -int32(offFuncRefDescPtr))
	candidate := f.allocReg(maskOf(value).add(base))
	required := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: targetType}), exactTarget)
	total := f.m.ImportedFuncCount() + len(f.m.FuncTypes)
	for functionIndex := 0; functionIndex < total; functionIndex++ {
		sourceType, ok := f.m.FuncTypeIndex(uint32(functionIndex))
		if !ok || !f.m.ReferenceTypeSubtype(wasm.Ref(false, wasm.IndexedHeap(sourceType), false), required) {
			continue
		}
		f.a.MovReg64(candidate, base)
		f.leaDisp(candidate, candidate, int32((functionIndex+1)*runtime.FuncRefDescBytes), true)
		f.cmpRR(value, candidate, true)
		success = append(success, f.a.Bcond(condE))
		if functionIndex < f.m.ImportedFuncCount() {
			f.ld64(candidate, candidate, runtime.TableEntryRefSlotOffset)
			f.cmpRR(value, candidate, true)
			success = append(success, f.a.Bcond(condE))
		}
	}
	f.release(candidate)
	f.release(base)
	f.trapAlways(trapCode)
	done := f.a.Len()
	for _, site := range success {
		f.a.PatchBranch19(site, done)
	}
}

func moduleHasCollectorTypes(m *wasm.Module) bool {
	for i := range m.Types {
		for j := range m.Types[i].SubTypes {
			switch m.Types[i].SubTypes[j].Comp.Kind {
			case wasm.CompStruct, wasm.CompArray:
				return true
			}
		}
	}
	return false
}

func (f *fn) emitGCBranchCast(sub uint32, r *wasm.Reader) error {
	if !f.gcStructHelpers {
		return fmt.Errorf("arm64: unsupported staged branch cast without GC helpers")
	}
	flags, err := r.Byte()
	if err != nil {
		return err
	}
	if flags > 3 {
		return fmt.Errorf("arm64: invalid staged branch-cast flags %d", flags)
	}
	depth, err := r.U32()
	if err != nil {
		return err
	}
	if _, _, err := readRefHeapTypeImmediate(r); err != nil { // validated source reference type
		return err
	}
	target, exactTarget, err := readRefHeapTypeImmediate(r)
	if err != nil {
		return err
	}
	original := f.popValue()
	gcRoot := original.st.hasGCRoot()
	value := f.materialize(original)
	copyReg := f.allocReg(maskOf(value))
	f.a.MovReg64(copyReg, value)
	f.pushReg(value, mtI64).st.setGCRoot(gcRoot)
	f.pushReg(copyReg, mtI64).st.setGCRoot(gcRoot)
	f.pushValue(storage{kind: stConst, typ: mtI64, cval: target})
	nullable := int64(0)
	if flags&2 != 0 {
		nullable = 1
	}
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: nullable})
	exact := int64(0)
	if exactTarget {
		exact = 1
	}
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: exact})
	anyref := wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(wasm.HeapAny), false))
	if err := f.callGCStructHelper(gcStructRefTest, []wasm.ValType{anyref, wasm.I64, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}); err != nil {
		return err
	}
	return f.brOnCastResult(depth, sub == 24)
}

func (f *fn) callGCStructHelper(helper uint32, params, results []wasm.ValType) error {
	before := f.a.Len()
	defer func() {
		n := f.a.Len() - before
		f.stats.addGCHelperCallBytes(n)
		if arm64GCHelperMayAllocate(helper) {
			f.stats.addGCAllocationBytes(n)
		}
		switch helper {
		case gcStructSet, gcStructTableSet, gcArraySet, gcArrayFill, gcArrayCopy, gcArrayInitData, gcArrayInitElem:
			f.stats.addGCBarrierBytes(n)
		}
	}()
	safepoint := uint32(0)
	if f.gcFrameRoots != nil && f.gcFrameRoots.Candidate && arm64GCHelperMayAllocate(helper) {
		safepoint = f.recordGCFrameSafepoint(len(params))
	}
	payload, ok := shared.EncodeGCDispatch(helper, safepoint)
	if !ok {
		if f.gcFrameRoots != nil {
			f.gcFrameRoots.Exact = false
		}
		payload = helper
	}
	ft := &wasm.CompType{Kind: wasm.CompFunc, Params: params, Results: results}
	return f.callHostSync(int(gcStructDispatchBit|payload), ft)
}

func (f *fn) recordGCFrameSafepoint(paramCount int) uint32 {
	plan := f.gcFrameRoots
	id := plan.SafepointBase + uint32(plan.SafepointCount()+1)
	if id == 0 || id > shared.GCSafepointIDMax {
		plan.Exact = false
		return 0
	}
	roots := f.rootsBottomToTop()
	if paramCount < 0 || paramCount > len(roots) {
		plan.Exact = false
		return id
	}
	siteIndex := plan.SafepointCount()
	if siteIndex >= len(plan.LiveLocalMasks) {
		plan.Exact = false
		return id
	}
	f.materializeGCFrameLocalsAt(siteIndex, false)
	builder := plan.BeginSafepoint()
	if !plan.VisitLiveLocals(siteIndex, false, func(root int) {
		builder.AppendOffset(plan.LocalOffsets[root])
	}) {
		builder.Abort()
		plan.Exact = false
		return id
	}
	hidden := len(roots) - paramCount
	slot := 0
	for i, root := range roots {
		if i < hidden && root.kind == ekValue && root.st.hasGCRoot() {
			off := f.spillOff(slot)
			if off < 0 {
				builder.Abort()
				plan.Exact = false
				return id
			}
			builder.AppendOffset(uint32(off))
		}
		slot += rootMachineType(root).stackSlots()
	}
	for _, off := range plan.FixedOffsets {
		builder.AppendOffset(off)
	}
	offsets, ok := builder.Offsets()
	if !ok {
		builder.Abort()
		plan.Exact = false
		return id
	}
	sort.Slice(offsets, func(i, j int) bool { return offsets[i] < offsets[j] })
	if !builder.Commit() {
		builder.Abort()
		plan.Exact = false
		return id
	}
	f.stats.addGCRootMapBytes(8 + len(offsets)*4)
	return id
}

func arm64GCFrameRefType(m *wasm.Module, t wasm.ValType) bool {
	if t.Kind() != wasm.ValRef {
		return false
	}
	heap := t.Ref().Heap()
	switch heap.Kind() {
	case wasm.HeapAbs:
		switch heap.Abs() {
		case wasm.HeapAny, wasm.HeapEq, wasm.HeapI31, wasm.HeapStruct, wasm.HeapArray, wasm.HeapNone:
			return true
		default:
			return false
		}
	case wasm.HeapDefType:
		kind, valid := heap.DefCompKind()
		if !valid {
			return true
		}
		return kind == wasm.CompStruct || kind == wasm.CompArray
	case wasm.HeapTypeIndex:
		index := heap.Type().Index
		for _, group := range m.Types {
			if index < uint32(len(group.SubTypes)) {
				kind := group.SubTypes[index].Comp.Kind
				return kind == wasm.CompStruct || kind == wasm.CompArray
			}
			index -= uint32(len(group.SubTypes))
		}
		return true
	default:
		return true
	}
}

func (f *fn) tracksGCFrameRoots() bool {
	return f.gcFrameRoots != nil && f.gcFrameRoots.Candidate
}

func (f *fn) gcFrameLocal(index int) bool {
	if index < 0 || f.gcFrameRoots == nil {
		return false
	}
	return f.gcFrameRoots.TracksLocal(uint32(index))
}

func arm64GCHelperMayAllocate(helper uint32) bool {
	switch helper {
	case gcStructAllocDefault, gcStructAllocOne,
		gcArrayAllocDefault, gcArrayAllocFixed, gcArrayAllocUniform,
		gcArrayAllocData, gcArrayAllocElem, gcArrayAllocFixedV128Spill:
		return true
	default:
		return false
	}
}

func (f *fn) callGCArrayFixedSpill(typeIndex, count uint32, resultType wasm.ValType) error {
	roots := f.rootsBottomToTop()
	if uint64(count) > uint64(len(roots)) {
		return fmt.Errorf("arm64: array.new_fixed count %d exceeds operand depth %d", count, len(roots))
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
	f.a.LeaSP(ptr, f.spillOff(firstSlot))
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

func stagedStructType(m *wasm.Module, typeIndex uint32) (wasm.SubType, bool) {
	if m == nil {
		return wasm.SubType{}, false
	}
	index := typeIndex
	for _, group := range m.Types {
		if index < uint32(len(group.SubTypes)) {
			sub := group.SubTypes[index]
			return sub, sub.Comp.Kind == wasm.CompStruct
		}
		index -= uint32(len(group.SubTypes))
	}
	return wasm.SubType{}, false
}

func stagedArraySubtype(m *wasm.Module, typeIndex uint32) (wasm.SubType, bool) {
	if m == nil {
		return wasm.SubType{}, false
	}
	index := typeIndex
	for _, group := range m.Types {
		if index < uint32(len(group.SubTypes)) {
			sub := group.SubTypes[index]
			return sub, sub.Comp.Kind == wasm.CompArray
		}
		index -= uint32(len(group.SubTypes))
	}
	return wasm.SubType{}, false
}

func stagedStructField(m *wasm.Module, typeIndex, fieldIndex uint32) (wasm.FieldType, bool) {
	st, ok := stagedStructType(m, typeIndex)
	if !ok || fieldIndex >= uint32(len(st.Comp.Fields)) {
		return wasm.FieldType{}, false
	}
	return st.Comp.Fields[fieldIndex], true
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
