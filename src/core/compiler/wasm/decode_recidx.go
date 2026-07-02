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
			markRecursiveValTypeIndexes(&ct.Params[i], base, limit)
		}
		for i := range ct.Results {
			markRecursiveValTypeIndexes(&ct.Results[i], base, limit)
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
	if ft.Storage.Packed {
		return
	}
	markRecursiveValTypeIndexes(&ft.Storage.Val, base, limit)
}

func markRecursiveValTypeIndexes(vt *ValType, base, limit uint32) {
	if vt.Kind != ValRef {
		return
	}
	markRecursiveRefTypeIndexes(&vt.Ref, base, limit)
}

func markRecursiveRefTypeIndexes(rt *RefType, base, limit uint32) {
	if rt.Heap.Kind != HeapTypeIndex {
		return
	}
	rt.Heap.Type = markRecursiveTypeIdx(rt.Heap.Type, base, limit)
}

func markRecursiveTypeIdx(idx TypeIdx, base, limit uint32) TypeIdx {
	if idx.Rec || idx.Index < base || idx.Index >= limit {
		return idx
	}
	return TypeIdx{Index: idx.Index - base, Rec: true}
}
