package wasm

func (v *moduleValidator) subtypeByTypeIdx(idx TypeIdx) (*SubType, bool) {
	if idx.Rec {
		return nil, false
	}
	want := int(idx.Index)
	for gi := range v.m.Types {
		rt := &v.m.Types[gi]
		if want < len(rt.SubTypes) {
			return &rt.SubTypes[want], true
		}
		want -= len(rt.SubTypes)
	}
	return nil, false
}

func (v *moduleValidator) validTypeIdx(idx TypeIdx) bool {
	_, ok := v.subtypeByTypeIdx(idx)
	return ok
}

func (v *moduleValidator) funcTypeFromTypeIdx(idx TypeIdx) *CompType {
	st, ok := v.subtypeByTypeIdx(idx)
	if !ok || st.Comp.Kind != CompFunc {
		return nil
	}
	return &st.Comp
}

func (v *moduleValidator) compTypeFromTypeIdx(idx TypeIdx) (*CompType, bool) {
	st, ok := v.subtypeByTypeIdx(idx)
	if !ok {
		return nil, false
	}
	return &st.Comp, true
}

func (v *moduleValidator) structFields(idx TypeIdx) ([]FieldType, *SubType, bool) {
	st, ok := v.subtypeByTypeIdx(idx)
	if !ok || st.Comp.Kind != CompStruct {
		return nil, nil, false
	}
	return st.Comp.Fields, st, true
}

func (v *moduleValidator) arrayField(idx TypeIdx) (FieldType, *SubType, bool) {
	st, ok := v.subtypeByTypeIdx(idx)
	if !ok || st.Comp.Kind != CompArray {
		return FieldType{}, nil, false
	}
	return st.Comp.Array, st, true
}

func storageValType(st StorageType, signedGet bool) ValType {
	if !st.Packed {
		return st.Val
	}
	_ = signedGet
	return I32
}

func valTypeDefaultable(t ValType) bool {
	return t.Kind != ValRef || t.Ref.Nullable
}

func (v *moduleValidator) descriptorTargetRefType(nullable bool, ht HeapType, exact bool) (ValType, bool) {
	if ht.Kind == HeapTypeIndex {
		if _, ok := v.subtypeByTypeIdx(ht.Type); !ok {
			return ValType{}, false
		}
	}
	return RefVal(Ref(nullable, ht, exact)), true
}

func (v *moduleValidator) typeIdxSuperSubtype(a, b TypeIdx) bool {
	if a == b {
		return true
	}
	st, ok := v.subtypeByTypeIdx(a)
	if !ok {
		return false
	}
	for _, sup := range st.Supers {
		if v.typeIdxSuperSubtype(sup, b) {
			return true
		}
	}
	return false
}

func (v *moduleValidator) descriptorCompatible(a, b RefType) bool {
	if a.Heap.Kind == HeapAbs && b.Heap.Kind == HeapAbs {
		return absHeapSubtype(a.Heap.Abs, b.Heap.Abs) || absHeapSubtype(b.Heap.Abs, a.Heap.Abs)
	}
	if a.Exact && b.Exact && a.Heap.Kind == HeapTypeIndex && b.Heap.Kind == HeapTypeIndex {
		ac, aok := v.compTypeFromTypeIdx(a.Heap.Type)
		bc, bok := v.compTypeFromTypeIdx(b.Heap.Type)
		return aok && bok && equalCompType(*ac, *bc)
	}
	return v.refSubtype(a, b) || v.refSubtype(b, a)
}

func (v *moduleValidator) refSubtype(a, b RefType) bool {
	if !b.Nullable && a.Nullable {
		return false
	}
	if equalHeapType(a.Heap, b.Heap) {
		if b.Exact && !a.Exact {
			return false
		}
		return true
	}
	if a.Heap.Kind == HeapTypeIndex && b.Heap.Kind == HeapTypeIndex {
		return !b.Exact && v.typeIdxSuperSubtype(a.Heap.Type, b.Heap.Type)
	}
	if a.Heap.Kind == HeapTypeIndex && b.Heap.Kind == HeapAbs {
		ct, ok := v.compTypeFromTypeIdx(a.Heap.Type)
		if !ok {
			return false
		}
		switch ct.Kind {
		case CompStruct:
			return absHeapSubtype(HeapStruct, b.Heap.Abs)
		case CompArray:
			return absHeapSubtype(HeapArray, b.Heap.Abs)
		case CompFunc:
			return absHeapSubtype(HeapFunc, b.Heap.Abs)
		}
	}
	if a.Heap.Kind == HeapAbs && b.Heap.Kind == HeapAbs {
		return absHeapSubtype(a.Heap.Abs, b.Heap.Abs)
	}
	return false
}

func (v *moduleValidator) heapSubtype(a, b HeapType) bool {
	if equalHeapType(a, b) {
		return true
	}
	return v.refSubtype(Ref(false, a, false), Ref(false, b, false))
}
