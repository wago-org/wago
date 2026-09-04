//go:build amd64

package amd64

import (
	"fmt"
	"sort"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
)

// GC helpers reuse the synchronous parked-Go dispatcher. The high dispatch bit
// separates internal helpers from real Wasm function imports and public host
// funcref dispatch. These values are mirrored at the src/wago dispatcher
// boundary; they are compile-only ABI constants, not serialized product data.
const (
	gcStructDispatchBit       uint32 = 1 << 30
	gcStructAllocDefault             = 1
	gcStructGet                      = 2
	gcStructSet                      = 3
	gcStructGetS                     = 4
	gcStructGetU                     = 5
	gcStructRefTest                  = 6
	gcStructTableSet                 = 7
	gcAnyConvertExtern               = 8
	gcExternConvertAny               = 9
	gcStructRefCast                  = 10
	gcStructAllocOne                 = 11
	gcStructFinalCastGet             = 12
	gcStructFinalCastArrayLen        = 13
	gcFuncRefTest                    = 14
	gcStructReserveDead              = 15
)

func (f *fn) emitFB(r *wasm.Reader) error {
	before := f.a.Len()
	sub, err := r.U32()
	if err != nil {
		return err
	}
	f.prepareGCResolvedFB(sub)
	f.gcOpcodeBarrier = false
	defer func() { f.recordGCOpcodeBytes(sub, f.a.Len()-before) }()
	if sub >= 6 && sub <= 19 {
		return f.emitGCArray(sub, r)
	}
	if sub == 20 || sub == 21 {
		return f.emitGCI31Test(sub, r)
	}
	if sub == 22 || sub == 23 {
		return f.emitGCI31Cast(sub, r)
	}
	if sub == 24 || sub == 25 {
		return f.emitGCBranchCast(sub, r)
	}
	if sub >= 28 && sub <= 30 {
		return f.emitGCI31(sub)
	}
	if sub == 26 || sub == 27 {
		if !f.gcStructHelpers {
			return fmt.Errorf("amd64: unsupported staged extern conversion opcode %d without GC helpers", sub)
		}
		if sub == 26 {
			return f.callGCStructHelper(gcAnyConvertExtern, []wasm.ValType{wasm.ExternRef}, []wasm.ValType{wasm.AnyRef})
		}
		return f.callGCStructHelper(gcExternConvertAny, []wasm.ValType{wasm.AnyRef}, []wasm.ValType{wasm.ExternRef})
	}
	if !f.gcStructHelpers {
		return fmt.Errorf("amd64: unsupported 0xfb opcode %d without staged GC struct helpers", sub)
	}
	switch sub {
	case 0: // struct.new typeidx
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		st, ok := f.stagedStructType(typeIndex)
		if !ok {
			return fmt.Errorf("amd64: struct.new type %d is unavailable", typeIndex)
		}
		fieldN := len(st.Comp.Fields)
		nestedPayloadSafe := true
		for _, field := range st.Comp.Fields {
			if field.Storage().Val().Kind() == wasm.ValRef {
				nestedPayloadSafe = false
				break
			}
		}
		if deadUse := f.checkedDeadGCConstructorUse(r, nestedPayloadSafe); deadUse != checkedDeadGCNone {
			if err := f.reserveDeadGCStructConstructor(typeIndex, fieldN, deadUse); err != nil {
				return err
			}
			f.finishCheckedDeadGCConstructor(r, deadUse)
			return nil
		}
		params := make([]wasm.ValType, 0, fieldN+1)
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
		if _, native := f.nativeGCStructAllocLayout(typeIndex); native {
			f.nativeStructAllocType = typeIndex + 1
		}
		if err := f.callGCStructHelper(gcStructAllocOne, params, []wasm.ValType{result}); err != nil {
			return err
		}
		f.markTopGCReference()
		return nil
	case 1: // struct.new_default typeidx
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		if _, ok := f.stagedStructType(typeIndex); !ok {
			return fmt.Errorf("amd64: struct.new_default type %d is unavailable", typeIndex)
		}
		if deadUse := f.checkedDeadGCConstructorUse(r, true); deadUse != checkedDeadGCNone {
			if err := f.reserveDeadGCStructConstructor(typeIndex, 0, deadUse); err != nil {
				return err
			}
			f.finishCheckedDeadGCConstructor(r, deadUse)
			return nil
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		if err := f.callGCStructHelper(gcStructAllocDefault, []wasm.ValType{wasm.I32}, []wasm.ValType{result}); err != nil {
			return err
		}
		f.markTopGCReference()
		return nil
	case 2, 3, 4: // struct.get / struct.get_s / struct.get_u typeidx fieldidx
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
			return fmt.Errorf("amd64: struct.get type %d field %d is unavailable", typeIndex, fieldIndex)
		}
		helper := uint32(gcStructGet)
		resultType := field.Storage().Val()
		if sub == 3 || sub == 4 {
			if !field.Storage().Packed() {
				return fmt.Errorf("amd64: struct.get_s/u type %d field %d is not packed", typeIndex, fieldIndex)
			}
			resultType = wasm.I32
			if sub == 3 {
				helper = gcStructGetS
			} else {
				helper = gcStructGetU
			}
		} else if field.Storage().Packed() {
			return fmt.Errorf("amd64: plain struct.get cannot access packed type %d field %d", typeIndex, fieldIndex)
		}
		if f.emitDirectGCStructGet(typeIndex, fieldIndex, helper) {
			return nil
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(fieldIndex)})
		object := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		if err := f.callGCStructHelper(helper, []wasm.ValType{object, wasm.I32, wasm.I32}, []wasm.ValType{resultType}); err != nil {
			return err
		}
		return nil
	case 5: // struct.set typeidx fieldidx
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
			return fmt.Errorf("amd64: struct.set type %d field %d is unavailable", typeIndex, fieldIndex)
		}
		if field.Mut() != wasm.Var {
			return fmt.Errorf("amd64: struct.set type %d field %d is immutable", typeIndex, fieldIndex)
		}
		valueType := field.Storage().Val()
		if field.Storage().Packed() {
			valueType = wasm.I32
		}
		if f.emitDirectGCStructSet(typeIndex, fieldIndex) {
			return nil
		}
		if field.Storage().Val().Kind() == wasm.ValRef {
			if layout, final, layoutOK := f.gcStructFieldLayout(typeIndex, fieldIndex); layoutOK && final && layout.CollectorRef && layout.Size == 4 {
				f.gcOpcodeBarrier = true
				f.stats.peep("gc-barrier-slow")
				return f.emitNativeBarrierSafeStructRefSet(typeIndex, fieldIndex, layout.Offset, field.Storage().Val())
			}
			f.gcOpcodeBarrier = true
			f.stats.peep("gc-barrier-slow")
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(fieldIndex)})
		object := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		return f.callGCStructHelper(gcStructSet, []wasm.ValType{object, valueType, wasm.I32, wasm.I32}, nil)
	default:
		return fmt.Errorf("amd64: unsupported staged 0xfb opcode %d", sub)
	}
}

func (f *fn) emitGCI31Test(sub uint32, r *wasm.Reader) error {
	heap, err := r.S33()
	if err != nil {
		return err
	}
	nullable := sub == 21
	if heap >= 0 {
		top := f.s.back()
		if top != nil && top.kind == ekValue && top.st.kind == stConst && top.st.cval == 0 {
			f.popValue()
			matched := int64(0)
			if nullable {
				matched = 1
			}
			f.pushValue(storage{kind: stConst, typ: mtI32, cval: matched})
			return nil
		}
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
	if f.gcStructHelpers && heap >= 0 {
		if target, ok := f.stagedGCType(uint32(heap)); ok && (target.Comp.Kind == wasm.CompStruct || target.Comp.Kind == wasm.CompArray) {
			return f.emitNativeDefinedTest(uint32(heap), nullable)
		}
	}
	if heap == -16 || heap == -17 || heap == -13 || heap == -14 { // func, extern, nofunc, noextern
		value := f.materialize(f.popValue())
		if heap == -16 || heap == -17 {
			if nullable {
				f.a.MovImm32(value, 1)
			} else {
				f.a.TestSelf(value, true)
				f.a.SetccReg(condNE, value)
			}
		} else if nullable {
			f.a.TestSelf(value, true)
			f.a.SetccReg(condE, value)
		} else {
			f.a.AluRR(aluTable[opXor].rr, value, value, false)
		}
		f.pushReg(value, mtI32)
		return nil
	}
	if f.gcStructHelpers {
		value := f.materialize(f.popValue())
		f.pushReg(value, mtI64)
		f.pushValue(storage{kind: stConst, typ: mtI64, cval: heap})
		if nullable {
			f.pushValue(storage{kind: stConst, typ: mtI32, cval: 1})
		} else {
			f.pushValue(storage{kind: stConst, typ: mtI32})
		}
		f.pushValue(storage{kind: stConst, typ: mtI32}) // ref.test does not admit exact heap markers
		anyref := wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(wasm.HeapAny), false))
		return f.callGCStructHelper(gcStructRefTest, []wasm.ValType{anyref, wasm.I64, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32})
	}
	value := f.materialize(f.popValue())
	switch heap {
	case -20, -19, -18: // i31, eq, any: this exact product contains only null or tagged i31 values.
		tag := f.allocReg(maskOf(value))
		f.a.MovRegReg32(tag, value)
		f.a.AluRI(4, tag, 1, false)
		if nullable {
			f.a.TestSelf(value, true)
			f.a.SetccReg(condE, value)
			f.a.AluRR(aluTable[opOr].rr, value, tag, false)
		} else {
			f.a.MovRegReg32(value, tag)
		}
		f.release(tag)
	case -21, -22, -15: // struct, array, none: null matches only the nullable form; i31 never matches.
		if nullable {
			f.a.TestSelf(value, true)
			f.a.SetccReg(condE, value)
		} else {
			f.a.AluRR(aluTable[opXor].rr, value, value, false)
		}
	default:
		return fmt.Errorf("amd64: staged ref.test heap %d is outside the null/i31 slice", heap)
	}
	f.pushReg(value, mtI32)
	return nil
}

func (f *fn) emitGCI31Cast(sub uint32, r *wasm.Reader) error {
	heap, exactTarget, err := readRefHeapTypeImmediate(r)
	if err != nil {
		return err
	}
	sourceLocal, hasSourceLocal := gcLocalProvenance(f.s.back())
	finalTarget := false
	if heap >= 0 {
		if target, ok := f.stagedGCType(uint32(heap)); ok && target.Final {
			finalTarget = true
		}
	}
	if f.gcStructHelpers && heap >= 0 {
		if fused, err := f.tryFuseFinalCastStructGet(uint32(heap), sub == 23, r); fused || err != nil {
			return err
		}
		if fused, err := f.tryFuseFinalCastArrayLen(uint32(heap), sub == 23, r); fused || err != nil {
			return err
		}
		if target, ok := f.stagedGCType(uint32(heap)); ok && (target.Comp.Kind == wasm.CompStruct || target.Comp.Kind == wasm.CompArray) {
			if err := f.emitNativeDefinedCast(uint32(heap), sub == 23, exactTarget); err != nil {
				return err
			}
			if sub == 22 {
				if finalTarget || exactTarget {
					f.markTopGCReference()
				}
				if hasSourceLocal {
					markGCLocalProvenance(f.s.back(), sourceLocal)
				}
			}
			return nil
		}
	}
	if f.gcTypeSubtypingRefTest && heap >= 0 {
		if _, targetIsFunc := f.m.TypeFunc(uint32(heap)); targetIsFunc {
			value := f.materialize(f.popValue())
			f.emitLocalFunctionSubtypeIdentityCheck(value, uint32(heap), sub == 23, exactTarget, trapCastFailure)
			f.pushReg(value, mtI64)
			return nil
		}
	}
	if f.gcStructHelpers {
		value := f.materialize(f.popValue())
		f.pushReg(value, mtI64)
		f.pushValue(storage{kind: stConst, typ: mtI64, cval: heap})
		if sub == 23 {
			f.pushValue(storage{kind: stConst, typ: mtI32, cval: 1})
		} else {
			f.pushValue(storage{kind: stConst, typ: mtI32})
		}
		exact := int64(0)
		if exactTarget {
			exact = 1
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: exact})
		anyref := wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(wasm.HeapAny), false))
		if err := f.callGCStructHelper(gcStructRefCast, []wasm.ValType{anyref, wasm.I64, wasm.I32, wasm.I32}, []wasm.ValType{anyref}); err != nil {
			return err
		}
		if finalTarget && sub == 22 {
			f.markTopGCReference()
			if hasSourceLocal {
				markGCLocalProvenance(f.s.back(), sourceLocal)
			}
		}
		return nil
	}
	if heap != -20 { // i31
		return fmt.Errorf("amd64: staged ref.cast heap %d is not i31", heap)
	}
	value := f.materialize(f.popValue())
	var nullableDone int
	if sub == 23 {
		f.a.TestSelf(value, true)
		nullableDone = f.a.JccPlaceholder(condE)
	} else {
		f.a.TestSelf(value, true)
		f.trapIf(condE, trapCastFailure)
	}
	tag := f.allocReg(maskOf(value))
	f.a.MovRegReg32(tag, value)
	f.a.AluRI(4, tag, 1, false)
	f.a.TestSelf(tag, false)
	f.trapIf(condE, trapCastFailure)
	f.release(tag)
	if sub == 23 {
		f.a.PatchRel32(nullableDone, f.a.Len())
	}
	f.pushReg(value, mtI64)
	return nil
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
	if _, direct := directGCScalarStorage(field.Storage()); direct {
		// Let the shared native final-cast stub and existing direct scalar access
		// run independently; this removes the Go transition at a measured code-size cost.
		_ = r.JumpTo(start)
		return false, nil
	}
	if layout, final, layoutOK := f.gcStructFieldLayout(typeIndex, fieldIndex); layoutOK && layout.CollectorRef {
		if final && layout.Size == 4 {
			f.stats.peep("final-cast-struct-get-fuse")
			if err := f.emitNativeFinalCastStructRefGet(typeIndex, layout.Offset, nullable); err != nil {
				return true, err
			}
			return true, nil
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
	if err := f.callGCStructHelper(gcStructFinalCastGet, []wasm.ValType{anyref, wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{resultType}); err != nil {
		return true, err
	}
	return true, nil
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
	f.stats.peep("final-cast-array-len-fuse")
	if err := f.emitNativeFinalCastArrayLen(typeIndex, nullable); err != nil {
		return true, err
	}
	return true, nil
}

func (f *fn) emitDynamicFunctionSubtypeTest(targetType uint32, nullable bool) error {
	subtypes, ok := f.m.FunctionSubtypeTypeIndexes(targetType)
	if !ok {
		return fmt.Errorf("amd64: function ref.test target type %d is unavailable", targetType)
	}
	var savedLocals [16]locState
	if len(f.pinnedLocals) > len(savedLocals) {
		return fmt.Errorf("amd64: %d pinned locals exceed conditional function ref.test bound", len(f.pinnedLocals))
	}
	for i, local := range f.pinnedLocals {
		savedLocals[i] = f.locals[local].state
	}
	f.flush()
	value := f.s.back()
	if value == f.s.head || value.kind != ekValue || value.st.kind != stSlot {
		return fmt.Errorf("amd64: dynamic function ref.test lost canonical operand")
	}
	f.a.Load64(RAX, RSP, f.spillOff(value.st.slotIndex()))
	f.a.TestSelf(RAX, true)
	nullSite := f.a.JccPlaceholder(condE)
	f.a.Load64(RCX, RBX, -int32(offFuncRefDescPtr))
	f.cmpRR(RAX, RCX, true)
	unknownSites := []int{f.a.JccPlaceholder(condBE)}
	f.a.MovReg64(R8, RCX)
	f.a.LeaDisp(R8, R8, int32((f.m.ImportedFuncCount()+len(f.m.FuncTypes)+1)*runtime.FuncRefDescBytes))
	f.cmpRR(RAX, R8, true)
	unknownSites = append(unknownSites, f.a.JccPlaceholder(condAE))
	f.a.AluRR(aluTable[opSub].rr, RAX, RCX, true)
	f.a.XorSelf32(RDX)
	f.a.MovImm32(RSI, runtime.FuncRefDescBytes)
	f.a.Div(RSI, true)
	f.a.TestSelf(RDX, true)
	unknownSites = append(unknownSites, f.a.JccPlaceholder(condNE))
	f.a.AluRI(aluTable[opSub].digit, RAX, 1, true)
	f.a.Load64(RCX, RCX, runtime.TableEntryCodePtrOffset)
	f.a.TestSelf(RCX, true)
	unknownSites = append(unknownSites, f.a.JccPlaceholder(condE))
	f.a.LeaScaled(RDX, RCX, RAX, 2, 0)
	f.a.Load32(RAX, RDX, 0)
	trueSites := make([]int, 0, len(subtypes)+1)
	for _, typeIndex := range subtypes {
		f.a.AluRI(cmpDigit, RAX, int32(typeIndex), false)
		trueSites = append(trueSites, f.a.JccPlaceholder(condE))
	}
	f.a.AluRI(cmpDigit, RAX, -1, false)
	unknownSites = append(unknownSites, f.a.JccPlaceholder(condE))
	falseSite := f.a.JmpPlaceholder()
	trueLabel := f.a.Len()
	if nullable {
		f.a.PatchRel32(nullSite, trueLabel)
	}
	for _, site := range trueSites {
		f.a.PatchRel32(site, trueLabel)
	}
	f.a.MovImm32(RAX, 1)
	classified := f.a.JmpPlaceholder()
	falseLabel := f.a.Len()
	if !nullable {
		f.a.PatchRel32(nullSite, falseLabel)
	}
	f.a.PatchRel32(falseSite, falseLabel)
	f.a.MovImm32(RAX, 0)
	classifiedFalse := f.a.JmpPlaceholder()
	unknownLabel := f.a.Len()
	for _, site := range unknownSites {
		f.a.PatchRel32(site, unknownLabel)
	}
	f.a.MovImm32(RAX, 2)
	classDone := f.a.Len()
	f.a.PatchRel32(classified, classDone)
	f.a.PatchRel32(classifiedFalse, classDone)
	f.a.AluRI(cmpDigit, RAX, 2, false)
	known := f.a.JccPlaceholder(condNE)

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
	f.reloadConditionalGCPinnedLocals(savedLocals[:len(f.pinnedLocals)])
	f.flush()
	result := f.s.back()
	if result == f.s.head || result.kind != ekValue || result.st.kind != stSlot {
		return fmt.Errorf("amd64: dynamic function ref.test lost canonical result")
	}
	done := f.a.JmpPlaceholder()
	f.a.PatchRel32(known, f.a.Len())
	f.a.Store32(RSP, f.spillOff(result.st.slotIndex()), RAX)
	f.a.PatchRel32(done, f.a.Len())
	return nil
}

func (f *fn) emitLocalFunctionSubtypeIdentityCheck(value Reg, targetType uint32, nullable, exactTarget bool, trapCode uint32) {
	success := make([]int, 0, f.m.ImportedFuncCount()+len(f.m.FuncTypes)+1)
	if nullable {
		f.a.TestSelf(value, true)
		success = append(success, f.a.JccPlaceholder(condE))
	}
	base := f.allocReg(maskOf(value))
	f.a.Load64(base, RBX, -int32(offFuncRefDescPtr))
	candidate := f.allocReg(maskOf(value, base))
	required := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: targetType}), exactTarget)
	total := f.m.ImportedFuncCount() + len(f.m.FuncTypes)
	for functionIndex := 0; functionIndex < total; functionIndex++ {
		sourceType, ok := f.m.FuncTypeIndex(uint32(functionIndex))
		if !ok || !f.m.ReferenceTypeSubtype(wasm.Ref(false, wasm.IndexedHeap(sourceType), false), required) {
			continue
		}
		f.a.MovReg64(candidate, base)
		f.a.LeaDisp(candidate, candidate, int32((functionIndex+1)*runtime.FuncRefDescBytes))
		f.cmpRR(value, candidate, true)
		success = append(success, f.a.JccPlaceholder(condE))
		if functionIndex < f.m.ImportedFuncCount() {
			f.a.Load64(candidate, candidate, runtime.TableEntryRefSlotOffset)
			f.cmpRR(value, candidate, true)
			success = append(success, f.a.JccPlaceholder(condE))
		}
	}
	f.release(candidate)
	f.release(base)
	f.trapAlways(trapCode)
	done := f.a.Len()
	for _, site := range success {
		f.a.PatchRel32(site, done)
	}
}

func (f *fn) emitGCBranchCast(sub uint32, r *wasm.Reader) error {
	if !f.gcStructHelpers {
		return fmt.Errorf("amd64: unsupported staged branch cast without GC helpers")
	}
	flags, err := r.Byte()
	if err != nil {
		return err
	}
	if flags > 3 {
		return fmt.Errorf("amd64: invalid staged branch-cast flags %d", flags)
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
	value := f.materialize(f.popValue())
	copyReg := f.allocReg(maskOf(value))
	f.a.MovReg64(copyReg, value)
	original := f.pushReg(value, mtI64) // original identity for either selected edge
	markGCReference(original)
	copyValue := f.pushReg(copyReg, mtI64) // copied helper operand
	markGCReference(copyValue)
	f.pushValue(storage{kind: stConst, typ: mtI64, cval: target})
	if flags&2 != 0 {
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: 1})
	} else {
		f.pushValue(storage{kind: stConst, typ: mtI32})
	}
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

func (f *fn) emitGCI31(sub uint32) error {
	value := f.materialize(f.popValue())
	switch sub {
	case 28: // ref.i31
		f.a.ShiftImm(4, value, 1, false) // low 31 bits << 1; 32-bit write clears the upper half
		f.a.AluRI(1, value, 1, false)    // tag immediate with low bit 1
		result := f.pushReg(value, mtI64)
		markGCReference(result)
	case 29: // i31.get_s
		f.a.TestSelf(value, true)
		f.trapIf(condE, trapNullReference)
		f.a.ShiftImm(7, value, 1, false) // arithmetic shift sign-extends bit 30
		f.pushReg(value, mtI32)
	case 30: // i31.get_u
		f.a.TestSelf(value, true)
		f.trapIf(condE, trapNullReference)
		f.a.ShiftImm(5, value, 1, false)
		f.pushReg(value, mtI32)
	default:
		return fmt.Errorf("amd64: unsupported staged i31 opcode %d", sub)
	}
	return nil
}

func (f *fn) callGCStructHelper(helper uint32, params, results []wasm.ValType) error {
	// Every parked helper may run collector work, so the transient raw resolver
	// certificate does not survive it.
	f.invalidateGCResolvedObject()
	before := f.a.Len()
	defer func() {
		n := f.a.Len() - before
		f.stats.addGCHelperCallBytes(n)
		if gcHelperMayAllocate(helper) {
			f.stats.addGCAllocationBytes(n)
		}
		switch helper {
		case gcStructSet, gcStructTableSet, gcArraySet, gcArrayFill, gcArrayCopy, gcArrayInitData, gcArrayInitElem:
			f.stats.addGCBarrierBytes(n)
		}
	}()
	safepoint := uint32(0)
	if f.gcFrameRoots != nil && f.gcFrameRoots.Candidate && gcHelperMayAllocate(helper) {
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
	if siteIndex >= plan.AllocationMaskCount() {
		plan.Exact = false
		return id
	}
	builder := plan.BeginSafepoint()
	if !plan.VisitLiveLocals(siteIndex, false, func(root int) {
		builder.AppendOffset(plan.Locals[root].Offset)
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
	for _, off := range plan.FixedOffsets() {
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

func gcFrameRefType(m *wasm.Module, t wasm.ValType) bool {
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

func (f *fn) gcFrameLocal(index int) bool {
	if index < 0 || f.gcFrameRoots == nil {
		return false
	}
	return f.gcFrameRoots.TracksLocal(uint32(index))
}

func gcHelperMayAllocate(helper uint32) bool {
	switch helper {
	case gcStructAllocDefault, gcStructAllocOne, gcStructReserveDead,
		gcArrayAllocDefault, gcArrayAllocFixed, gcArrayAllocUniform,
		gcArrayAllocData, gcArrayAllocElem, gcArrayAllocFixedV128Spill,
		gcArrayAllocDefaultNative, gcArrayAllocUniformNative, gcArrayAllocFixedNative,
		gcArrayCheckDefault, gcArrayCheckUniform, gcArrayCheckData, gcArrayCheckFixed:
		return true
	default:
		return false
	}
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
	sub, ok := stagedStructType(m, typeIndex)
	if !ok || fieldIndex >= uint32(len(sub.Comp.Fields)) {
		return wasm.FieldType{}, false
	}
	return sub.Comp.Fields[fieldIndex], true
}
