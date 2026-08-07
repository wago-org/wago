package wasm

// markRecursiveTypeIndexes rolls absolute type indexes that point inside their
// own recursive group into the module's local recursive-index representation.
// WebAssembly binaries encode these references as flattened type indexes; the
// validator and GC descriptor lowerer keep them relative so recursive groups can
// be reasoned about independently after decode.
func markRecursiveTypeIndexes(types []RecType) {
	base := uint32(0)
	for gi := range types {
		rt := &types[gi]
		limit := base + uint32(len(rt.SubTypes))
		// Every rectype, including the compact singleton form, opens recursive
		// scope. The explicit 0x4e group form only changes the group cardinality.
		for si := range rt.SubTypes {
			markRecursiveSubTypeIndexes(&rt.SubTypes[si], base, limit)
		}
		base = limit
	}
}

func markRecursiveSubTypeIndexes(st *SubType, base, limit uint32) {
	for i := range st.Supers {
		st.Supers[i] = markRecursiveTypeIdx(st.Supers[i], base, limit)
	}
	if st.Metadata.Describes != nil {
		idx := markRecursiveTypeIdx(*st.Metadata.Describes, base, limit)
		st.Metadata.Describes = &idx
	}
	if st.Metadata.Descriptor != nil {
		idx := markRecursiveTypeIdx(*st.Metadata.Descriptor, base, limit)
		st.Metadata.Descriptor = &idx
	}
	markRecursiveCompTypeIndexes(&st.Comp, base, limit)
}

func markRecursiveCompTypeIndexes(ct *CompType, base, limit uint32) {
	switch ct.Kind {
	case CompFunc:
		for i := range ct.Params {
			ct.Params[i] = markRecursiveValTypeIndexes(ct.Params[i], base, limit)
		}
		for i := range ct.Results {
			ct.Results[i] = markRecursiveValTypeIndexes(ct.Results[i], base, limit)
		}
	case CompStruct:
		for i := range ct.Fields {
			markRecursiveFieldTypeIndexes(&ct.Fields[i], base, limit)
		}
	case CompArray:
		markRecursiveFieldTypeIndexes(&ct.Array, base, limit)
	}
}

func markRecursiveFieldTypeIndexes(ft *FieldType, base, limit uint32) {
	storage := ft.Storage()
	if storage.Packed() {
		return
	}
	*ft = NewFieldType(StorageVal(markRecursiveValTypeIndexes(storage.Val(), base, limit)), ft.Mut())
}

func markRecursiveValTypeIndexes(vt ValType, base, limit uint32) ValType {
	if vt.Kind() != ValRef {
		return vt
	}
	return RefVal(markRecursiveRefTypeIndexes(vt.Ref(), base, limit))
}

func markRecursiveRefTypeIndexes(rt RefType, base, limit uint32) RefType {
	heap := rt.Heap()
	if heap.Kind() != HeapTypeIndex {
		return rt
	}
	return rt.WithHeap(IndexedHeap(markRecursiveTypeIdx(heap.Type(), base, limit)))
}

func markRecursiveTypeIdx(idx TypeIdx, base, limit uint32) TypeIdx {
	if idx.Rec || idx.Index < base || idx.Index >= limit {
		return idx
	}
	return TypeIdx{Index: idx.Index - base, Rec: true}
}
