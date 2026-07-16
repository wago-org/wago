//go:build amd64

package amd64

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
)

// GC helpers reuse the synchronous parked-Go dispatcher. The high dispatch bit
// separates internal helpers from real Wasm function imports and public host
// funcref dispatch. These values are mirrored at the src/wago dispatcher
// boundary; they are compile-only ABI constants, not serialized product data.
const (
	gcStructDispatchBit  uint32 = 1 << 30
	gcStructAllocDefault        = 1
	gcStructGet                 = 2
	gcStructSet                 = 3
	gcStructGetS                = 4
	gcStructGetU                = 5
	gcStructRefTest             = 6
	gcStructTableSet            = 7
	gcAnyConvertExtern          = 8
	gcExternConvertAny          = 9
	gcStructRefCast             = 10
	gcStructAllocOne            = 11
)

func (f *fn) emitFB(r *wasm.Reader) error {
	sub, err := r.U32()
	if err != nil {
		return err
	}
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
		st, ok := stagedStructType(f.m, typeIndex)
		if !ok || len(st.Comp.Fields) != 1 {
			return fmt.Errorf("amd64: staged struct.new type %d requires exactly one field", typeIndex)
		}
		field := st.Comp.Fields[0]
		valueType := field.Storage.Val
		if field.Storage.Packed {
			valueType = wasm.I32
		}
		if valueType.Kind == wasm.ValRef {
			return fmt.Errorf("amd64: reference struct.new remains outside the staged helper slice")
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		return f.callGCStructHelper(gcStructAllocOne, []wasm.ValType{valueType, wasm.I32}, []wasm.ValType{result})
	case 1: // struct.new_default typeidx
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		if _, ok := stagedStructType(f.m, typeIndex); !ok {
			return fmt.Errorf("amd64: struct.new_default type %d is unavailable", typeIndex)
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		return f.callGCStructHelper(gcStructAllocDefault, []wasm.ValType{wasm.I32}, []wasm.ValType{result})
	case 2, 3, 4: // struct.get / struct.get_s / struct.get_u typeidx fieldidx
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		fieldIndex, err := r.U32()
		if err != nil {
			return err
		}
		field, ok := stagedStructField(f.m, typeIndex, fieldIndex)
		if !ok {
			return fmt.Errorf("amd64: struct.get type %d field %d is unavailable", typeIndex, fieldIndex)
		}
		helper := uint32(gcStructGet)
		resultType := field.Storage.Val
		if sub == 3 || sub == 4 {
			if !field.Storage.Packed {
				return fmt.Errorf("amd64: struct.get_s/u type %d field %d is not packed", typeIndex, fieldIndex)
			}
			resultType = wasm.I32
			if sub == 3 {
				helper = gcStructGetS
			} else {
				helper = gcStructGetU
			}
		} else if field.Storage.Packed {
			return fmt.Errorf("amd64: plain struct.get cannot access packed type %d field %d", typeIndex, fieldIndex)
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(fieldIndex)})
		object := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		return f.callGCStructHelper(helper, []wasm.ValType{object, wasm.I32, wasm.I32}, []wasm.ValType{resultType})
	case 5: // struct.set typeidx fieldidx
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		fieldIndex, err := r.U32()
		if err != nil {
			return err
		}
		field, ok := stagedStructField(f.m, typeIndex, fieldIndex)
		if !ok {
			return fmt.Errorf("amd64: struct.set type %d field %d is unavailable", typeIndex, fieldIndex)
		}
		if field.Mut != wasm.Var {
			return fmt.Errorf("amd64: struct.set type %d field %d is immutable", typeIndex, fieldIndex)
		}
		if field.Storage.Val.Kind == wasm.ValRef {
			return fmt.Errorf("amd64: reference struct.set remains outside the staged helper slice")
		}
		valueType := field.Storage.Val
		if field.Storage.Packed {
			valueType = wasm.I32
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
	if f.gcTypeSubtypingRefTest && heap >= 0 {
		value := f.popValue()
		if value.kind != ekValue || value.st.kind != stFuncRef || value.st.idx < 0 || value.st.idx >= len(f.m.FuncTypes) {
			return fmt.Errorf("amd64: staged function ref.test lost exact local ref.func provenance")
		}
		actual := wasm.Ref(false, wasm.IndexedHeap(f.m.FuncTypes[value.st.idx]), false)
		required := wasm.Ref(nullable, wasm.IndexedHeap(wasm.TypeIdx{Index: uint32(heap)}), false)
		matched := int64(0)
		if f.m.ReferenceTypeSubtype(actual, required) {
			matched = 1
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: matched})
		return nil
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
		anyref := wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(wasm.HeapAny), false))
		return f.callGCStructHelper(gcStructRefTest, []wasm.ValType{anyref, wasm.I64, wasm.I32}, []wasm.ValType{wasm.I32})
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
	heap, err := r.S33()
	if err != nil {
		return err
	}
	if f.gcTypeSubtypingRefTest && heap >= 0 {
		value := f.materialize(f.popValue())
		f.emitLocalFunctionSubtypeIdentityCheck(value, uint32(heap), sub == 23, trapCastFailure)
		f.pushReg(value, mtI64)
		return nil
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
		anyref := wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(wasm.HeapAny), false))
		return f.callGCStructHelper(gcStructRefCast, []wasm.ValType{anyref, wasm.I64, wasm.I32}, []wasm.ValType{anyref})
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

func (f *fn) emitLocalFunctionSubtypeIdentityCheck(value Reg, targetType uint32, nullable bool, trapCode uint32) {
	var success [16]int
	nsuccess := 0
	if nullable {
		f.a.TestSelf(value, true)
		success[nsuccess] = f.a.JccPlaceholder(condE)
		nsuccess++
	}
	base := f.allocReg(maskOf(value))
	f.a.Load64(base, RBX, -int32(offFuncRefDescPtr))
	candidate := f.allocReg(maskOf(value, base))
	required := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: targetType}), false)
	for i, sourceType := range f.m.FuncTypes {
		actual := wasm.Ref(false, wasm.IndexedHeap(sourceType), false)
		if !f.m.ReferenceTypeSubtype(actual, required) {
			continue
		}
		if nsuccess == len(success) {
			f.trapAlways(trapCode)
			break
		}
		f.a.MovReg64(candidate, base)
		f.a.LeaDisp(candidate, candidate, int32((i+1)*runtime.FuncRefDescBytes))
		f.cmpRR(value, candidate, true)
		success[nsuccess] = f.a.JccPlaceholder(condE)
		nsuccess++
	}
	f.release(candidate)
	f.release(base)
	f.trapAlways(trapCode)
	done := f.a.Len()
	for i := 0; i < nsuccess; i++ {
		f.a.PatchRel32(success[i], done)
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
	if _, err := r.S33(); err != nil { // validated source heap type
		return err
	}
	target, err := r.S33()
	if err != nil {
		return err
	}
	value := f.materialize(f.popValue())
	copyReg := f.allocReg(maskOf(value))
	f.a.MovReg64(copyReg, value)
	f.pushReg(value, mtI64)   // original identity for either selected edge
	f.pushReg(copyReg, mtI64) // copied helper operand
	f.pushValue(storage{kind: stConst, typ: mtI64, cval: target})
	if flags&2 != 0 {
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: 1})
	} else {
		f.pushValue(storage{kind: stConst, typ: mtI32})
	}
	anyref := wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(wasm.HeapAny), false))
	if err := f.callGCStructHelper(gcStructRefTest, []wasm.ValType{anyref, wasm.I64, wasm.I32}, []wasm.ValType{wasm.I32}); err != nil {
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
		f.pushReg(value, mtI64)
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
	ft := &wasm.CompType{Kind: wasm.CompFunc, Params: params, Results: results}
	return f.callHostSync(int(gcStructDispatchBit|helper), ft)
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

func stagedStructField(m *wasm.Module, typeIndex, fieldIndex uint32) (wasm.FieldType, bool) {
	sub, ok := stagedStructType(m, typeIndex)
	if !ok || fieldIndex >= uint32(len(sub.Comp.Fields)) {
		return wasm.FieldType{}, false
	}
	return sub.Comp.Fields[fieldIndex], true
}
