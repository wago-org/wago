//go:build arm64

package arm64

import (
	"fmt"
	"math"
	"sort"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/src/core/runtime/gc"
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
	gcStructReserveDead        uint32 = 15
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
	gcArrayCheckDefault        uint32 = 36
	gcArrayCheckUniform        uint32 = 37
	gcArrayCheckData           uint32 = 38
	gcArrayCheckFixed          uint32 = 39
)

func (f *fn) emitFB(r *wasm.Reader) error {
	before := f.a.Len()
	sub, err := r.U32()
	if err != nil {
		return err
	}
	f.prepareGCResolvedFB(sub)
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
			if _, targetIsFunc := f.m.TypeFunc(uint32(heap)); targetIsFunc && top != nil && top.kind == ekValue && top.st.kind == stFuncRef && top.st.idx >= f.m.ImportedFuncCount() && top.st.idx < len(f.m.FuncTypes) {
				f.popValue()
				actual := wasm.Ref(false, wasm.IndexedHeap(f.m.FuncTypes[top.st.idx]), false)
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
				gcRoot := value.st.gcRoot
				ref := f.materialize(value)
				f.emitLocalFunctionSubtypeIdentityCheck(ref, uint32(heap), sub == 23, exactTarget, trapCastFailure)
				f.pushReg(ref, mtI64).st.gcRoot = gcRoot
				return nil
			}
		}
		if !moduleHasCollectorTypes(f.m) {
			value := f.popValue()
			gcRoot := value.st.gcRoot
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
			f.pushReg(ref, mtI64).st.gcRoot = gcRoot
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
			f.pushReg(value, mtI64).st.gcRoot = f.tracksGCFrameRoots()
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
		nestedPayloadSafe := true
		for _, field := range st.Comp.Fields {
			if field.Storage().Val().Kind() == wasm.ValRef {
				nestedPayloadSafe = false
				break
			}
		}
		if deadUse := f.checkedDeadGCConstructorUse(r, nestedPayloadSafe); deadUse != checkedDeadGCNone {
			if err := f.reserveDeadGCStructConstructor(typeIndex, len(st.Comp.Fields), deadUse); err != nil {
				return err
			}
			f.finishCheckedDeadGCConstructor(r, deadUse)
			return nil
		}
		knownField, consumeField := f.consumeKnownGCStructGet(typeIndex, st, false, r)
		consumeCast := !consumeField && f.consumeExactGCConstructorCast(typeIndex, r)
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
		if err := f.callGCStructHelper(gcStructAllocOne, params, []wasm.ValType{result}); err != nil {
			return err
		}
		f.finishKnownGCStructGet(knownField, consumeField)
		f.finishExactGCConstructorCast(consumeCast)
		return nil
	case 1: // struct.new_default
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		st, ok := f.stagedStructType(typeIndex)
		if !ok {
			return fmt.Errorf("arm64: struct.new_default type %d is unavailable", typeIndex)
		}
		if deadUse := f.checkedDeadGCConstructorUse(r, true); deadUse != checkedDeadGCNone {
			if err := f.reserveDeadGCStructConstructor(typeIndex, 0, deadUse); err != nil {
				return err
			}
			f.finishCheckedDeadGCConstructor(r, deadUse)
			return nil
		}
		knownField, consumeField := f.consumeKnownGCStructGet(typeIndex, st, true, r)
		consumeCast := !consumeField && f.consumeExactGCConstructorCast(typeIndex, r)
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		if err := f.callGCStructHelper(gcStructAllocDefault, []wasm.ValType{wasm.I32}, []wasm.ValType{result}); err != nil {
			return err
		}
		f.finishKnownGCStructGet(knownField, consumeField)
		f.finishExactGCConstructorCast(consumeCast)
		return nil
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
		if f.policy.EnabledOption(optGCNativeScalarGet) && !f.policy.CompactNative {
			if layout, scalar, layoutOK := f.directGCStructLayout(typeIndex, fieldIndex); layoutOK {
				required := uint64(gc.PayloadOffset) + uint64(layout.Offset) + uint64(scalar.size)
				if required <= math.MaxInt32 {
					f.stats.peep("gc-native-final-struct-scalar-get")
					return f.emitNativeFinalStructScalarGet(typeIndex, layout.Offset, uint32(required), true, sub, scalar)
				}
			}
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
		if f.policy.EnabledOption(optGCNativeScalarSet) && !f.policy.CompactNative {
			if layout, scalar, layoutOK := f.directGCStructLayout(typeIndex, fieldIndex); layoutOK {
				required := uint64(gc.PayloadOffset) + uint64(layout.Offset) + uint64(scalar.size)
				if required <= math.MaxInt32 {
					f.stats.peep("gc-native-final-struct-scalar-set")
					return f.emitNativeFinalStructScalarSet(typeIndex, layout.Offset, uint32(required), scalar)
				}
			}
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
		if deadUse := f.checkedDeadGCConstructorUse(r, true); valueType.Kind() != wasm.ValRef && deadUse != checkedDeadGCNone {
			f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
			if err := f.callGCStructHelper(gcArrayCheckUniform, []wasm.ValType{valueType, wasm.I32, wasm.I32}, deadGCReservationResults(typeIndex, deadUse)); err != nil {
				return err
			}
			f.finishCheckedDeadGCConstructor(r, deadUse)
			return nil
		}
		knownLength, consumeLength := f.consumeKnownGCArrayLen(r)
		consumeCast := !consumeLength && f.consumeExactGCConstructorCast(typeIndex, r)
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		if err := f.callGCStructHelper(gcArrayAllocUniform, []wasm.ValType{valueType, wasm.I32, wasm.I32}, []wasm.ValType{result}); err != nil {
			return err
		}
		f.finishKnownGCArrayLen(knownLength, consumeLength)
		f.finishExactGCConstructorCast(consumeCast)
		return nil
	case 7: // array.new_default
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		if _, ok := f.stagedArrayType(typeIndex); !ok {
			return fmt.Errorf("arm64: array.new_default type %d is unavailable", typeIndex)
		}
		if deadUse := f.checkedDeadGCConstructorUse(r, true); deadUse != checkedDeadGCNone {
			f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
			if err := f.callGCStructHelper(gcArrayCheckDefault, []wasm.ValType{wasm.I32, wasm.I32}, deadGCReservationResults(typeIndex, deadUse)); err != nil {
				return err
			}
			f.finishCheckedDeadGCConstructor(r, deadUse)
			return nil
		}
		knownLength, consumeLength := f.consumeKnownGCArrayLen(r)
		consumeCast := !consumeLength && f.consumeExactGCConstructorCast(typeIndex, r)
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		if err := f.callGCStructHelper(gcArrayAllocDefault, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{result}); err != nil {
			return err
		}
		f.finishKnownGCArrayLen(knownLength, consumeLength)
		f.finishExactGCConstructorCast(consumeCast)
		return nil
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
		if deadUse := f.checkedDeadGCConstructorUse(r, valueType.Kind() != wasm.ValRef); deadUse != checkedDeadGCNone {
			if err := f.reserveDeadGCFixedArrayConstructor(typeIndex, count, deadUse); err != nil {
				return err
			}
			f.finishCheckedDeadGCConstructor(r, deadUse)
			return nil
		}
		valueSlots := funcTypeSlots([]wasm.ValType{valueType})
		if uint64(count)*uint64(valueSlots)+2 > maxSyncHostSlots {
			if wasm.EqualValType(valueType, wasm.V128) {
				result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
				return f.callGCArrayFixedV128Spill(typeIndex, count, result)
			}
			return fmt.Errorf("arm64: array.new_fixed count %d exceeds helper slot bound", count)
		}
		consumeLength := f.policy.EnabledOption(optGCFixedArrayLen) && consumeImmediateGCArrayLen(r)
		consumeCast := !consumeLength && f.consumeExactGCConstructorCast(typeIndex, r)
		params := make([]wasm.ValType, 0, int(count)+2)
		for i := uint32(0); i < count; i++ {
			params = append(params, valueType)
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(count)})
		params = append(params, wasm.I32, wasm.I32)
		result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		if err := f.callGCStructHelper(gcArrayAllocFixed, params, []wasm.ValType{result}); err != nil {
			return err
		}
		f.finishKnownGCArrayLen(count, consumeLength)
		f.finishExactGCConstructorCast(consumeCast)
		return nil
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
		knownLength, consumeLength := uint32(0), false
		if sub == 9 {
			knownLength, consumeLength = f.consumeKnownGCArrayLen(r)
		}
		consumeCast := !consumeLength && f.consumeExactGCConstructorCast(typeIndex, r)
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(segmentIndex)})
		deadUse := checkedDeadGCNone
		if sub == 9 {
			deadUse = f.checkedDeadGCConstructorUse(r, true)
		}
		if deadUse != checkedDeadGCNone {
			if err := f.callGCStructHelper(gcArrayCheckData, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, deadGCReservationResults(typeIndex, deadUse)); err != nil {
				return err
			}
			f.finishCheckedDeadGCConstructor(r, deadUse)
			return nil
		}
		result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		if err := f.callGCStructHelper(helper, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{result}); err != nil {
			return err
		}
		f.finishKnownGCArrayLen(knownLength, consumeLength)
		f.finishExactGCConstructorCast(consumeCast)
		return nil
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
		if f.policy.EnabledOption(optGCNativeArrayGet) && !f.policy.CompactNative {
			if scalar, ok := f.directGCArrayLayout(typeIndex); ok {
				f.stats.peep("gc-native-final-array-scalar-get")
				return f.emitNativeFinalArrayScalarGet(typeIndex, sub, scalar)
			}
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
		if f.policy.EnabledOption(optGCNativeArraySet) && !f.policy.CompactNative {
			if scalar, ok := f.directGCArrayLayout(typeIndex); ok {
				f.stats.peep("gc-native-final-array-scalar-set")
				return f.emitNativeFinalArrayScalarSet(typeIndex, scalar)
			}
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

// consumeImmediateGCArrayLen commits only the exact two-instruction producer /
// consumer shape. Reading through a copied Reader makes every mismatch and
// malformed suffix leave the production reader untouched for ordinary parsing.
func consumeImmediateGCArrayLen(r *wasm.Reader) bool {
	look := *r
	op, err := look.Byte()
	if err != nil || op != 0xfb {
		return false
	}
	sub, err := look.U32()
	if err != nil || sub != 15 {
		return false
	}
	if err := r.JumpTo(look.Offset()); err != nil {
		panic("arm64: copied GC lookahead produced an invalid reader offset")
	}
	return true
}

func (f *fn) consumeKnownGCArrayLen(r *wasm.Reader) (uint32, bool) {
	if !f.policy.EnabledOption(optGCFixedArrayLen) {
		return 0, false
	}
	length := f.s.back()
	if length == nil || length.kind != ekValue || length.st.kind != stConst || length.st.typ != mtI32 {
		return 0, false
	}
	if !consumeImmediateGCArrayLen(r) {
		return 0, false
	}
	return uint32(length.st.cval), true
}

func (f *fn) finishKnownGCArrayLen(length uint32, consume bool) {
	if !consume {
		return
	}
	f.dropValue()
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(length)})
	f.stats.peep("gc-known-array-len")
	f.stats.peep("gc-array-len-elide")
}

type knownGCStructField struct {
	value int64
	typ   machineType
}

func (f *fn) consumeKnownGCStructGet(typeIndex uint32, st wasm.SubType, defaulted bool, r *wasm.Reader) (knownGCStructField, bool) {
	if !f.policy.EnabledOption(optGCConstStructGet) {
		return knownGCStructField{}, false
	}
	look := *r
	op, err := look.Byte()
	if err != nil || op != 0xfb {
		return knownGCStructField{}, false
	}
	sub, err := look.U32()
	if err != nil || sub < 2 || sub > 4 {
		return knownGCStructField{}, false
	}
	accessType, err := look.U32()
	if err != nil || accessType != typeIndex {
		return knownGCStructField{}, false
	}
	fieldIndex, err := look.U32()
	if err != nil || fieldIndex >= uint32(len(st.Comp.Fields)) {
		return knownGCStructField{}, false
	}
	field := st.Comp.Fields[fieldIndex]
	storageType := field.Storage()
	if (sub == 2) == storageType.Packed() {
		return knownGCStructField{}, false
	}
	known := knownGCStructField{typ: mtI32}
	if !storageType.Packed() {
		valueType := storageType.Val()
		if !wasm.EqualValType(valueType, wasm.I32) && !wasm.EqualValType(valueType, wasm.I64) {
			return knownGCStructField{}, false
		}
		known.typ = mtOf(valueType)
	}
	if !defaulted {
		roots := f.rootsBottomToTop()
		firstField := len(roots) - len(st.Comp.Fields)
		if firstField < 0 {
			return knownGCStructField{}, false
		}
		initializer := roots[firstField+int(fieldIndex)]
		if initializer.kind != ekValue || initializer.st.kind != stConst {
			return knownGCStructField{}, false
		}
		known.value = initializer.st.cval
	}
	if storageType.Packed() {
		switch storageType.Pack() {
		case wasm.PackI8:
			if sub == 3 {
				known.value = int64(int32(int8(known.value)))
			} else {
				known.value = int64(uint8(known.value))
			}
		case wasm.PackI16:
			if sub == 3 {
				known.value = int64(int32(int16(known.value)))
			} else {
				known.value = int64(uint16(known.value))
			}
		default:
			return knownGCStructField{}, false
		}
	}
	if err := r.JumpTo(look.Offset()); err != nil {
		panic("arm64: copied GC struct lookahead produced an invalid reader offset")
	}
	return known, true
}

func (f *fn) finishKnownGCStructGet(known knownGCStructField, consume bool) {
	if !consume {
		return
	}
	f.dropValue()
	f.pushValue(storage{kind: stConst, typ: known.typ, cval: known.value})
	f.stats.peep("gc-known-struct-get")
	f.stats.peep("gc-struct-get-elide")
}

func (f *fn) consumeExactGCConstructorCast(typeIndex uint32, r *wasm.Reader) bool {
	if !f.policy.EnabledOption(optGCConstructorCast) {
		return false
	}
	look := *r
	op, err := look.Byte()
	if err != nil || op != 0xfb {
		return false
	}
	sub, err := look.U32()
	if err != nil || (sub != 22 && sub != 23) {
		return false
	}
	target, _, err := readRefHeapTypeImmediate(&look)
	if err != nil || target != int64(typeIndex) {
		return false
	}
	if err := r.JumpTo(look.Offset()); err != nil {
		panic("arm64: copied GC constructor-cast lookahead produced an invalid reader offset")
	}
	return true
}

func (f *fn) finishExactGCConstructorCast(consume bool) {
	if !consume {
		return
	}
	f.stats.peep("gc-ref-cast-elide")
	f.stats.peep("gc-constructor-cast")
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
	if f.policy.EnabledOption(optGCNativeScalarGet) && !f.policy.CompactNative {
		if layout, scalar, layoutOK := f.directGCStructLayout(typeIndex, fieldIndex); layoutOK {
			required := uint64(gc.PayloadOffset) + uint64(layout.Offset) + uint64(scalar.size)
			if required <= math.MaxInt32 {
				f.stats.peep("final-cast-struct-get-fuse")
				f.stats.peep("gc-native-final-struct-scalar-get")
				return true, f.emitNativeFinalStructScalarGet(typeIndex, layout.Offset, uint32(required), nullable, sub, scalar)
			}
		}
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
	if f.policy.EnabledOption(optGCNativeFinalArrayLen) && !f.policy.CompactNative {
		f.stats.peep("final-cast-array-len-fuse")
		f.stats.peep("gc-native-final-array-len")
		return true, f.emitNativeFinalCastArrayLen(typeIndex, nullable)
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

// emitNativeFinalCastArrayLen resolves one compact reference through the
// versioned collector native view, proves exact equality with the statically
// final array type, and loads its length. Instantiation validates the immutable
// view ABI and local type map; mutable collector backing is reloaded here.
//
// The checks deliberately mirror the Go helper's observable order: null first,
// then i31/handle/range/type validation. ref.cast_null accepts null, but the
// immediately fused array.len still reports a null-reference trap.
func (f *fn) emitNativeFinalCastArrayLen(typeIndex uint32, nullable bool) error {
	object, err := f.emitNativeFinalCastObject(typeIndex, gc.HeaderSize, nullable)
	if err != nil {
		return err
	}
	result := object
	if f.gcResolved.valid && f.gcResolved.reg == object {
		// The cached raw address must remain intact for a following read. Use a
		// separate result only in that bounded case; uncached single reads retain
		// the existing in-place load.
		result = f.allocReg(maskOf(object))
	}
	f.ld32(result, object, 8)
	if result != object {
		f.release(object)
	}
	f.pushReg(result, mtI32)
	return nil
}

func (f *fn) emitNativeFinalStructScalarGet(typeIndex, fieldOffset, required uint32, nullable bool, sub uint32, scalar directGCScalar) error {
	object, err := f.emitNativeFinalCastObject(typeIndex, required, nullable)
	if err != nil {
		return err
	}
	disp := int32(gc.PayloadOffset + fieldOffset)
	if scalar.typ.isFloat() {
		result := f.allocFReg(0)
		f.fld(result, object, disp, scalar.typ == mtF64)
		f.release(object)
		f.pushFReg(result, scalar.typ)
		return nil
	}
	result := f.allocReg(maskOf(object))
	f.a.LoadIdx(result, object, ZR, disp, scalar.size, sub == 3, scalar.typ == mtI64)
	f.release(object)
	f.pushReg(result, scalar.typ)
	return nil
}

func (f *fn) emitNativeFinalStructScalarSet(typeIndex, fieldOffset, required uint32, scalar directGCScalar) error {
	valueElem := f.popValue()
	var value Reg
	if scalar.typ.isFloat() {
		value = f.materializeF(valueElem)
		f.fpinned = f.fpinned.add(value)
	} else {
		value = f.materialize(valueElem)
		f.pinned = f.pinned.add(value)
	}
	object, err := f.emitNativeFinalCastObject(typeIndex, required, true)
	if err != nil {
		return err
	}
	disp := int32(gc.PayloadOffset + fieldOffset)
	if scalar.typ.isFloat() {
		f.fst(object, disp, value, scalar.typ == mtF64)
		f.fpinned = f.fpinned.remove(value)
		f.releaseF(value)
	} else {
		f.a.StoreIdx(object, ZR, value, disp, scalar.size)
		f.pinned = f.pinned.remove(value)
		f.release(value)
	}
	f.release(object)
	return nil
}

func (f *fn) emitNativeFinalArrayScalarGet(typeIndex, sub uint32, scalar directGCScalar) error {
	indexValue := f.popValue()
	index := f.materialize(indexValue)
	f.a.MovReg32(index, index)
	f.pinned = f.pinned.add(index)
	object, err := f.emitNativeFinalCastObject(typeIndex, gc.PayloadOffset, true)
	if err != nil {
		return err
	}

	end := f.emitNativeArrayElementChecks(object, index, scalar.size)

	if scalar.typ.isFloat() {
		result := f.allocFReg(0)
		f.a.LdrFIdx(result, object, index, int32(gc.PayloadOffset), scalar.typ == mtF64)
		f.release(end)
		f.pinned = f.pinned.remove(index)
		f.release(index)
		f.release(object)
		f.pushFReg(result, scalar.typ)
		return nil
	}
	result := end
	f.a.LoadIdx(result, object, index, int32(gc.PayloadOffset), scalar.size, sub == 12, scalar.typ == mtI64)
	f.pinned = f.pinned.remove(index)
	f.release(index)
	f.release(object)
	f.pushReg(result, scalar.typ)
	return nil
}

func (f *fn) emitNativeFinalArrayScalarSet(typeIndex uint32, scalar directGCScalar) error {
	valueElem := f.popValue()
	var value Reg
	if scalar.typ.isFloat() {
		value = f.materializeF(valueElem)
		f.fpinned = f.fpinned.add(value)
	} else {
		value = f.materialize(valueElem)
		f.pinned = f.pinned.add(value)
	}
	indexValue := f.popValue()
	index := f.materialize(indexValue)
	f.a.MovReg32(index, index)
	f.pinned = f.pinned.add(index)
	object, err := f.emitNativeFinalCastObject(typeIndex, gc.PayloadOffset, true)
	if err != nil {
		return err
	}
	end := f.emitNativeArrayElementChecks(object, index, scalar.size)
	f.release(end)
	if scalar.typ.isFloat() {
		f.a.StrFIdx(object, index, value, int32(gc.PayloadOffset), scalar.typ == mtF64)
		f.fpinned = f.fpinned.remove(value)
		f.releaseF(value)
	} else {
		f.a.StoreIdx(object, index, value, int32(gc.PayloadOffset), scalar.size)
		f.pinned = f.pinned.remove(value)
		f.release(value)
	}
	f.pinned = f.pinned.remove(index)
	f.release(index)
	f.release(object)
	return nil
}

// emitNativeArrayElementChecks validates the logical index and the physical
// object extent, then leaves index scaled by the element width for one load or
// store. The returned scratch register is allocator-owned.
func (f *fn) emitNativeArrayElementChecks(object, index Reg, size int) Reg {
	length := f.allocReg(maskOf(object, index))
	f.ld32(length, object, 8)
	f.cmpRR(index, length, false)
	f.trapIf(condAE, trapBuiltin)
	f.release(length)
	if size > 1 {
		f.a.LslImm64(index, index, uint8(log2u(uint64(size))))
	}
	objectSize := f.allocReg(maskOf(object, index))
	f.ld32(objectSize, object, 4)
	end := f.allocReg(maskOf(object, index, objectSize))
	f.leaDisp(end, index, int32(gc.PayloadOffset)+int32(size), true)
	f.cmpRR(end, objectSize, true)
	f.trapIf(condA, trapCastFailure)
	f.release(objectSize)
	return end
}

// emitNativeFinalCastObject validates one exact-final compact reference and
// returns its current object-header address. The caller must consume or release
// the returned allocator-owned register without crossing a safepoint.
func (f *fn) emitNativeFinalCastObject(typeIndex, required uint32, nullable bool) (Reg, error) {
	local, hasLocal := gcLocalProvenance(f.s.back())
	if f.opt(optGCNativeResolveReuse) && hasLocal && f.gcResolved.valid &&
		f.gcResolved.local == local && f.gcResolved.typeIndex == typeIndex &&
		required <= f.gcResolved.required {
		object := f.popValue()
		if object.st.kind == stReg {
			f.release(object.st.reg)
		}
		f.stats.peep("gc-native-resolve-reuse")
		return f.gcResolved.reg, nil
	}
	f.invalidateGCResolvedObject()
	checkedRequired := required
	if f.opt(optGCNativeResolveReuse) && hasLocal {
		// A cached struct address may feed a later, higher-offset field. Validate
		// the immutable final layout's complete extent once so every subsequent
		// scalar field covered by that layout can reuse the certificate.
		if layout, ok := f.gcTypeLayout(typeIndex, wasm.CompStruct); ok && layout.ObjectSize > checkedRequired {
			checkedRequired = layout.ObjectSize
		}
	}
	object := f.popValue()
	ref := f.materialize(object)
	if nullable {
		f.trapIfZero(ref, false, true, trapNullReference)
	} else {
		f.trapIfZero(ref, false, true, trapCastFailure)
	}
	if !f.a.TstImm32(ref, 1) {
		panic("arm64: compact-reference tag is not an encodable logical immediate")
	}
	f.trapIf(condNE, trapCastFailure)

	view := f.allocReg(maskOf(ref))
	f.ld64(view, linMemReg, -int32(abi.GCNativeViewPtrOffset))
	domain := f.allocReg(maskOf(ref, view))
	f.ld64(domain, view, gc.NativeInstanceViewLocalTypesOffset)
	tmp := f.allocReg(maskOf(ref, view, domain))
	f.a.MovImm64(tmp, uint64(typeIndex)*4)
	f.a.LoadIdx(domain, domain, tmp, 0, 4, false, false)
	f.release(tmp)

	// Convert the compact reference to its handle index and validate it before
	// following the current handle-table backing.
	f.ld64(view, view, gc.NativeInstanceViewCollectorOffset)
	f.a.LsrImm32(ref, ref, 1)
	tmp = f.allocReg(maskOf(ref, view, domain))
	f.ld32(tmp, view, gc.NativeViewHandleCountOffset)
	f.cmpRR(ref, tmp, false)
	f.trapIf(condAE, trapCastFailure)
	f.ld64(tmp, view, gc.NativeViewHandlesOffset)
	handle := f.allocReg(maskOf(ref, view, domain, tmp))
	f.a.MovImm64(handle, gc.NativeHandleStride)
	f.a.Mul64(ref, ref, handle)
	f.a.Add64(tmp, tmp, ref) // tmp = handle entry

	// Select the current heap space. Reading the packed word at offset 16 keeps
	// the access within the 20-byte handle entry while extracting byte 18.
	f.ld32(handle, tmp, 16)
	f.a.LsrImm32(handle, handle, 16)
	if !f.a.AndImm32(handle, handle, 0xff) {
		panic("arm64: native space mask is not an encodable logical immediate")
	}
	f.trapIfZero(handle, false, true, trapCastFailure)
	f.cmpImm(handle, gc.NativeSpaceCount-1, false)
	f.trapIf(condA, trapCastFailure)
	f.a.LslImm64(handle, handle, 4)
	f.a.Add64(view, view, handle)

	// Retain offset and size only; the handle-table entry is no longer needed.
	f.ld32(ref, tmp, gc.NativeHandleOffsetOffset)
	f.ld32(handle, tmp, gc.NativeHandleSizeOffset)
	f.release(tmp)
	tmp = f.allocReg(maskOf(ref, view, domain, handle))
	f.ld64(tmp, view, gc.NativeViewSpacesOffset+gc.NativeSpaceBaseOffset)
	f.ld32(view, view, gc.NativeViewSpacesOffset+gc.NativeSpaceBytesOffset)
	f.cmpRR(ref, view, false)
	f.trapIf(condA, trapCastFailure)
	f.a.Sub32(view, view, ref)
	f.cmpRR(handle, view, false)
	f.trapIf(condA, trapCastFailure)
	if checkedRequired <= 0xfff {
		f.cmpImm(handle, checkedRequired, false)
	} else {
		viewRequired := f.allocReg(maskOf(ref, view, domain, handle, tmp))
		f.a.MovImm64(viewRequired, uint64(checkedRequired))
		f.cmpRR(handle, viewRequired, false)
		f.release(viewRequired)
	}
	f.trapIf(condB, trapCastFailure)

	// Exact final-type equality proves the statically selected object layout.
	f.a.Add64(tmp, tmp, ref)
	f.ld32(view, tmp, 0)
	f.cmpRR(view, domain, false)
	f.trapIf(condNE, trapCastFailure)
	f.release(ref)
	f.release(view)
	f.release(domain)
	f.release(handle)
	if f.opt(optGCNativeResolveReuse) && hasLocal {
		f.pinned = f.pinned.add(tmp)
		f.gcResolved = gcResolvedObject{valid: true, local: local, typeIndex: typeIndex, required: checkedRequired, reg: tmp}
	}
	return tmp, nil
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
	f.ld64(value, SP, f.spillOff(valueElem.st.slot))
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
	f.st32(SP, f.spillOff(result.st.slot), resultReg)
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
	gcRoot := original.st.gcRoot
	value := f.materialize(original)
	copyReg := f.allocReg(maskOf(value))
	f.a.MovReg64(copyReg, value)
	f.pushReg(value, mtI64).st.gcRoot = gcRoot
	f.pushReg(copyReg, mtI64).st.gcRoot = gcRoot
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
	id := plan.SafepointBase + uint32(len(plan.Safepoints)+1)
	if id == 0 || id > shared.GCSafepointIDMax {
		plan.Exact = false
		return 0
	}
	roots := f.rootsBottomToTop()
	if paramCount < 0 || paramCount > len(roots) {
		plan.Exact = false
		return id
	}
	siteIndex := len(plan.Safepoints)
	if siteIndex >= len(plan.LiveLocalMasks) {
		plan.Exact = false
		return id
	}
	f.materializeGCFrameLocalsAt(siteIndex, false)
	offsets := make([]uint32, 0, len(plan.LocalOffsets))
	for i, off := range plan.LocalOffsets {
		if plan.LocalLiveAt(siteIndex, i) {
			offsets = append(offsets, off)
		}
	}
	hidden := len(roots) - paramCount
	slot := 0
	for i, root := range roots {
		if i < hidden && root.kind == ekValue && root.st.gcRoot {
			off := f.spillOff(slot)
			if off < 0 {
				plan.Exact = false
				return id
			}
			offsets = append(offsets, uint32(off))
		}
		slot += rootMachineType(root).stackSlots()
	}
	offsets = append(offsets, plan.FixedOffsets...)
	sort.Slice(offsets, func(i, j int) bool { return offsets[i] < offsets[j] })
	if len(offsets) > shared.GCFrameRootLimit {
		plan.Exact = false
	}
	plan.Safepoints = append(plan.Safepoints, shared.GCFrameSafepointPlan{ID: id, Offsets: offsets})
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
	if !f.tracksGCFrameRoots() {
		return false
	}
	for _, candidate := range f.gcFrameRoots.LocalIndexes {
		if int(candidate) == index {
			return true
		}
	}
	return false
}

func arm64GCHelperMayAllocate(helper uint32) bool {
	switch helper {
	case gcStructAllocDefault, gcStructAllocOne, gcStructReserveDead,
		gcArrayAllocDefault, gcArrayAllocFixed, gcArrayAllocUniform,
		gcArrayAllocData, gcArrayAllocElem, gcArrayAllocFixedV128Spill,
		gcArrayCheckDefault, gcArrayCheckUniform, gcArrayCheckData, gcArrayCheckFixed:
		return true
	default:
		return false
	}
}

func (f *fn) callGCArrayFixedV128Spill(typeIndex, count uint32, resultType wasm.ValType) error {
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
