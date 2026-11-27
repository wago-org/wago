// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wasm

func (v *funcValidator) stepGC(in Instruction) error {
	switch in.Kind {
	case InstrRefEq:
		if err := v.popExpect(EqRef); err != nil {
			return err
		}
		if err := v.popExpect(EqRef); err != nil {
			return err
		}
		v.push(I32)
		return nil
	case InstrStringConst:
		if uint(in.Index) >= uint(len(v.m.StringRefs)) {
			return v.verr(ErrTypeMismatch, "string.const index")
		}
		v.push(StringRef)
		return nil
	case InstrRefI31:
		if err := v.popExpect(I32); err != nil {
			return err
		}
		v.push(RefVal(Ref(false, AbsHeap(HeapI31), false)))
		return nil
	case InstrI31GetS, InstrI31GetU:
		if err := v.popExpect(I31Ref); err != nil {
			return err
		}
		v.push(I32)
		return nil
	case InstrAnyConvertExtern, InstrExternConvertAny:
		operand, result := ExternRef, AnyRef
		if in.Kind == InstrExternConvertAny {
			operand, result = AnyRef, ExternRef
		}
		x, err := v.pop()
		if err != nil {
			return err
		}
		if !x.unknown() && !v.subtype(x.t, operand) {
			return v.verr(ErrTypeMismatch, x.t.String()+" is not "+operand.String())
		}
		// An unreachable stack operand can use the non-null input type.
		v.push(RefVal(result.Ref().WithNullable(!x.unknown() && x.t.Ref().Nullable())))
		return nil
	case InstrRefTest, InstrRefTestDesc:
		x, err := v.pop()
		if err != nil {
			return err
		}
		if !x.unknown() && x.t.Kind() != ValRef {
			return v.verr(ErrTypeMismatch, in.Kind.String()+" expects a reference operand")
		}
		target, ok := v.descriptorTargetRefType(in.Cast.TargetNullable, in.HeapType(), false)
		if !ok {
			return v.verr(ErrUnknownType, "invalid descriptor target reftype")
		}
		if !x.unknown() {
			compatible := v.refTestCompatible(x.t.Ref(), target.Ref())
			if in.Kind == InstrRefTestDesc {
				compatible = v.descriptorCompatible(x.t.Ref(), target.Ref())
			}
			if !compatible {
				return v.verr(ErrTypeMismatch, "target does not match operand type")
			}
		}
		v.push(I32)
		return nil
	case InstrRefCast, InstrRefCastDescEq:
		target, ok := v.descriptorTargetRefType(in.Cast.TargetNullable, in.HeapType(), in.Cast.SourceNullable)
		if !ok {
			return v.verr(ErrUnknownType, "invalid descriptor target reftype")
		}
		if in.Kind == InstrRefCastDescEq {
			if in.HeapType().Kind() != HeapTypeIndex {
				return v.verr(ErrTypeMismatch, "cast target must have descriptor")
			}
			st, recGroup, ok := v.subtypeByTypeIdxWithRecGroup(in.HeapType().Type())
			if !ok {
				return v.verr(ErrUnknownType, "invalid descriptor target reftype")
			}
			descriptor, present := st.Metadata.Descriptor.Get()
			if st.Comp.Kind != CompStruct || !present {
				return v.verr(ErrTypeMismatch, "cast target must have descriptor")
			}
			descriptorFlat, ok := v.flatTypeIdxInRecGroup(descriptor, recGroup)
			if !ok {
				return v.verr(ErrUnknownType, "invalid descriptor type")
			}
			// Descriptor equality is sound only for the descriptor declared by the
			// target; an exact target likewise requires an exact descriptor value.
			want := RefVal(Ref(true, IndexedHeap(TypeIdx{Index: uint32(descriptorFlat)}), in.Cast.SourceNullable))
			if err := v.popExpect(want); err != nil {
				return err
			}
		}
		x, err := v.pop()
		if err != nil {
			return err
		}
		if !x.unknown() && x.t.Kind() != ValRef {
			return v.verr(ErrTypeMismatch, "ref.cast expects a reference operand")
		}
		if !x.unknown() {
			compatible := v.refTestCompatible(x.t.Ref(), target.Ref())
			if in.Kind == InstrRefCastDescEq {
				compatible = v.descriptorCompatible(x.t.Ref(), target.Ref())
			}
			if !compatible {
				return v.verr(ErrTypeMismatch, "target does not match operand type")
			}
		}
		v.push(target)
		return nil
	case InstrBrOnCast, InstrBrOnCastFail:
		return v.stepBrOnCast(in)
	case InstrRefGetDesc:
		_, st, recGroup, ok := v.structFields(TypeIdx{Index: in.Index})
		if !ok {
			return v.verr(ErrUnknownType, "ref.get_desc")
		}
		descriptor, present := st.Metadata.Descriptor.Get()
		if !present {
			return v.verr(ErrTypeMismatch, "type without descriptor")
		}
		descriptorFlat, ok := v.flatTypeIdxInRecGroup(descriptor, recGroup)
		if !ok {
			return v.verr(ErrUnknownType, "invalid descriptor type")
		}
		x, err := v.pop()
		if err != nil {
			return err
		}
		if !x.unknown() && x.t.Kind() != ValRef {
			return v.verr(ErrTypeMismatch, "expected a reference operand")
		}
		if !x.unknown() && !v.refSubtype(x.t.Ref(), Ref(true, IndexedHeap(TypeIdx{Index: in.Index}), false)) {
			return v.verr(ErrTypeMismatch, "ref.get_desc target")
		}
		v.push(RefVal(Ref(false, IndexedHeap(TypeIdx{Index: uint32(descriptorFlat)}), true)))
		return nil
	case InstrStructNew, InstrStructNewDefault, InstrStructNewDesc, InstrStructNewDefaultDesc:
		return v.stepStructNew(in)
	case InstrStructGet, InstrStructGetS, InstrStructGetU, InstrStructAtomicGet, InstrStructAtomicGetS, InstrStructAtomicGetU:
		fields, _, recGroup, ok := v.structFields(TypeIdx{Index: in.Index})
		if !ok {
			return v.verr(ErrUnknownType, "struct.get")
		}
		if uint(in.Index2) >= uint(len(fields)) {
			return v.verr(ErrTypeMismatch, "unknown field")
		}
		f := v.resolveFieldTypeRecIndexes(fields[in.Index2], recGroup)
		packedGet := packedFieldGet(in.Kind)
		if f.Storage().Packed() != packedGet {
			return v.verr(ErrTypeMismatch, "field storage does not match struct.get variant")
		}
		if err := v.popExpect(RefVal(Ref(true, IndexedHeap(TypeIdx{Index: in.Index}), false))); err != nil {
			return err
		}
		v.push(storageValType(f.Storage(), packedGet))
		return nil
	case InstrStructSet:
		fields, _, recGroup, ok := v.structFields(TypeIdx{Index: in.Index})
		if !ok {
			return v.verr(ErrUnknownType, "struct.set")
		}
		if uint(in.Index2) >= uint(len(fields)) {
			return v.verr(ErrTypeMismatch, "unknown field")
		}
		f := v.resolveFieldTypeRecIndexes(fields[in.Index2], recGroup)
		if f.Mut() != Var {
			return v.verr(ErrTypeMismatch, "immutable field")
		}
		if err := v.popExpect(storageValType(f.Storage(), false)); err != nil {
			return err
		}
		return v.popExpect(RefVal(Ref(true, IndexedHeap(TypeIdx{Index: in.Index}), false)))
	case InstrArrayNew, InstrArrayNewDefault, InstrArrayNewFixed, InstrArrayNewData, InstrArrayNewElem:
		return v.stepArrayNew(in)
	case InstrArrayGet, InstrArrayGetS, InstrArrayGetU:
		f, _, ok := v.arrayField(TypeIdx{Index: in.Index})
		if !ok {
			return v.verr(ErrUnknownType, "array.get")
		}
		packedGet := packedFieldGet(in.Kind)
		if f.Storage().Packed() != packedGet {
			return v.verr(ErrTypeMismatch, "field storage does not match array.get variant")
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		if err := v.popExpect(RefVal(Ref(true, IndexedHeap(TypeIdx{Index: in.Index}), false))); err != nil {
			return err
		}
		v.push(storageValType(f.Storage(), packedGet))
		return nil
	case InstrArraySet:
		f, _, ok := v.arrayField(TypeIdx{Index: in.Index})
		if !ok {
			return v.verr(ErrUnknownType, "array.set")
		}
		if f.Mut() != Var {
			return v.verr(ErrTypeMismatch, "immutable array")
		}
		if err := v.popExpect(storageValType(f.Storage(), false)); err != nil {
			return err
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		return v.popExpect(RefVal(Ref(true, IndexedHeap(TypeIdx{Index: in.Index}), false)))
	case InstrArrayLen:
		x, err := v.pop()
		if err != nil {
			return err
		}
		if !x.unknown() && (x.t.Kind() != ValRef || !v.heapSubtype(x.t.Ref().Heap(), AbsHeap(HeapArray))) {
			return v.verr(ErrTypeMismatch, "array.len")
		}
		v.push(I32)
		return nil
	case InstrArrayFill:
		f, _, ok := v.arrayField(TypeIdx{Index: in.Index})
		if !ok {
			return v.verr(ErrUnknownType, "array.fill")
		}
		if f.Mut() != Var {
			return v.verr(ErrTypeMismatch, "immutable array")
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		if err := v.popExpect(storageValType(f.Storage(), false)); err != nil {
			return err
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		return v.popExpect(RefVal(Ref(true, IndexedHeap(TypeIdx{Index: in.Index}), false)))
	case InstrArrayCopy:
		dst, _, okDst := v.arrayField(TypeIdx{Index: in.Index})
		src, _, okSrc := v.arrayField(TypeIdx{Index: in.Index2})
		if !okDst || !okSrc {
			return v.verr(ErrUnknownType, "array.copy")
		}
		if dst.Mut() != Var {
			return v.verr(ErrTypeMismatch, "immutable array")
		}
		storageMatches := false
		if dst.Storage().Packed() || src.Storage().Packed() {
			storageMatches = dst.Storage().Packed() && src.Storage().Packed() && dst.Storage().Pack() == src.Storage().Pack()
		} else {
			storageMatches = v.subtype(src.Storage().Val(), dst.Storage().Val())
		}
		if !storageMatches {
			return v.verr(ErrTypeMismatch, "array types do not match")
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		if err := v.popExpect(RefVal(Ref(true, IndexedHeap(TypeIdx{Index: in.Index2}), false))); err != nil {
			return err
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		return v.popExpect(RefVal(Ref(true, IndexedHeap(TypeIdx{Index: in.Index}), false)))
	case InstrArrayInitData:
		field, _, ok := v.arrayField(TypeIdx{Index: in.Index})
		if !ok {
			return v.verr(ErrUnknownType, "array.init_data")
		}
		if field.Mut() != Var {
			return v.verr(ErrTypeMismatch, "immutable array")
		}
		if !field.Storage().Packed() && field.Storage().Val().Kind() == ValRef {
			return v.verr(ErrTypeMismatch, "array type is not numeric or vector")
		}
		if err := v.checkDataIndex(in.Index2, "array.init_data"); err != nil {
			return err
		}
		for range 3 {
			if err := v.popExpect(I32); err != nil {
				return err
			}
		}
		return v.popExpect(RefVal(Ref(true, IndexedHeap(TypeIdx{Index: in.Index}), false)))
	case InstrArrayInitElem:
		field, _, ok := v.arrayField(TypeIdx{Index: in.Index})
		if !ok {
			return v.verr(ErrUnknownType, "array.init_elem")
		}
		if field.Mut() != Var {
			return v.verr(ErrTypeMismatch, "immutable array")
		}
		if field.Storage().Packed() || field.Storage().Val().Kind() != ValRef {
			return v.verr(ErrTypeMismatch, "array.init_elem destination is not a reference array")
		}
		elemRef, err := v.elemRefType(in.Index2)
		if err != nil {
			return err
		}
		if !v.refSubtype(elemRef, field.Storage().Val().Ref()) {
			return v.verr(ErrTypeMismatch, "array.init_elem element type")
		}
		for range 3 {
			if err := v.popExpect(I32); err != nil {
				return err
			}
		}
		return v.popExpect(RefVal(Ref(true, IndexedHeap(TypeIdx{Index: in.Index}), false)))
	}
	return v.verr(ErrUnsupportedValidationOpcode, in.Kind.String())
}

func (v *funcValidator) stepStructNew(in Instruction) error {
	fields, st, recGroup, ok := v.structFields(TypeIdx{Index: in.Index})
	if !ok {
		return v.verr(ErrUnknownType, "struct.new")
	}
	if (in.Kind == InstrStructNew || in.Kind == InstrStructNewDefault) && st.Metadata.Descriptor.Present() {
		return v.verr(ErrTypeMismatch, "use struct.new_desc for descriptor-bearing struct")
	}
	if in.Kind == InstrStructNewDefault || in.Kind == InstrStructNewDefaultDesc {
		// Defaultability depends on nullability, not resolved heap indexes.
		for _, f := range fields {
			if !f.Storage().Packed() && !valTypeDefaultable(storageValType(f.Storage(), false)) {
				return v.verr(ErrTypeMismatch, "field not defaultable")
			}
		}
	} else {
		for i := len(fields) - 1; i >= 0; i-- {
			f := v.resolveFieldTypeRecIndexes(fields[i], recGroup)
			if err := v.popExpect(storageValType(f.Storage(), false)); err != nil {
				return err
			}
		}
	}
	if in.Kind == InstrStructNewDesc || in.Kind == InstrStructNewDefaultDesc {
		descriptor, present := st.Metadata.Descriptor.Get()
		if !present {
			return v.verr(ErrTypeMismatch, "type without descriptor")
		}
		descriptorFlat, ok := v.flatTypeIdxInRecGroup(descriptor, recGroup)
		if !ok {
			return v.verr(ErrUnknownType, "invalid descriptor type")
		}
		want := RefVal(Ref(false, IndexedHeap(TypeIdx{Index: uint32(descriptorFlat)}), true))
		if err := v.popExpect(want); err != nil {
			return err
		}
	}
	v.push(RefVal(Ref(false, IndexedHeap(TypeIdx{Index: in.Index}), false)))
	return nil
}

func (v *funcValidator) stepArrayNew(in Instruction) error {
	f, _, ok := v.arrayField(TypeIdx{Index: in.Index})
	if !ok {
		return v.verr(ErrUnknownType, "array.new")
	}
	elem := storageValType(f.Storage(), false)
	switch in.Kind {
	case InstrArrayNew:
		if err := v.popExpect(I32); err != nil {
			return err
		}
		if err := v.popExpect(elem); err != nil {
			return err
		}
	case InstrArrayNewDefault:
		if !f.Storage().Packed() && !valTypeDefaultable(elem) {
			return v.verr(ErrTypeMismatch, "element not defaultable")
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
	case InstrArrayNewFixed:
		available := len(v.vals) - v.top().height
		if uint64(in.Index2) > uint64(available) && !v.top().unreachable {
			return v.verr(ErrTypeMismatch, "stack underflow")
		}
		count := available
		if uint64(in.Index2) < uint64(count) {
			count = int(in.Index2)
		} else if uint64(in.Index2) > uint64(available) {
			// The remaining operands are polymorphic bottom values. Validate only
			// the concrete stack suffix so an untrusted u32 count cannot turn a
			// tiny unreachable body into billions of validation iterations.
			count = available
		}
		for range count {
			if err := v.popExpect(elem); err != nil {
				return err
			}
		}
	case InstrArrayNewData:
		if !f.Storage().Packed() && f.Storage().Val().Kind() == ValRef {
			return v.verr(ErrTypeMismatch, "array type is not numeric or vector")
		}
		if err := v.checkDataIndex(in.Index2, "array.new_data"); err != nil {
			return err
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
	case InstrArrayNewElem:
		if f.Storage().Packed() || f.Storage().Val().Kind() != ValRef {
			return v.verr(ErrTypeMismatch, "array.new_elem destination is not a reference array")
		}
		elemRef, err := v.elemRefType(in.Index2)
		if err != nil {
			return err
		}
		if !v.refSubtype(elemRef, f.Storage().Val().Ref()) {
			return v.verr(ErrTypeMismatch, "array.new_elem element type")
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
	}
	v.push(RefVal(Ref(false, IndexedHeap(TypeIdx{Index: in.Index}), false)))
	return nil
}

func (v *funcValidator) stepBrOnCast(in Instruction) error {
	lt, err := v.label(in.Index)
	if err != nil {
		return err
	}
	if len(lt) == 0 {
		return v.verr(ErrTypeMismatch, "label type too short")
	}
	labelRef := lt[len(lt)-1]
	if labelRef.Kind() != ValRef {
		return v.verr(ErrTypeMismatch, "label must end with a reftype")
	}
	rt1 := Ref(in.Cast.SourceNullable, in.HeapType(), false)
	rt2 := Ref(in.Cast.TargetNullable, in.HeapType2(), false)
	if !v.refSubtype(rt2, rt1) {
		return v.verr(ErrTypeMismatch, "rt2 does not match rt1")
	}
	x, err := v.pop()
	if err != nil {
		return err
	}
	if !x.unknown() && (x.t.Kind() != ValRef || !v.refSubtype(x.t.Ref(), rt1)) {
		return v.verr(ErrTypeMismatch, "br_on_cast operand")
	}
	// A nullable target consumes null on the successful cast edge. The failed
	// edge is therefore known non-null even when the declared source is nullable.
	// When the target is non-null, null remains a possible failed value and the
	// source nullability is preserved.
	failed := rt1
	if rt2.Nullable() {
		failed = failed.WithNullable(false)
	}
	prefix := lt[:len(lt)-1]
	if in.Kind == InstrBrOnCastFail {
		if !v.subtype(RefVal(failed), labelRef) {
			return v.verr(ErrTypeMismatch, "failed source does not match label rt")
		}
		if err := v.popAll(prefix); err != nil {
			return err
		}
		v.pushAll(prefix)
		v.push(RefVal(rt2))
		return nil
	}
	if !v.subtype(RefVal(rt2), labelRef) {
		return v.verr(ErrTypeMismatch, "rt2 does not match label rt")
	}
	if err := v.popAll(prefix); err != nil {
		return err
	}
	v.pushAll(prefix)
	v.push(RefVal(failed))
	return nil
}
