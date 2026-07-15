package wasm

// structuralIndexedFuncTypeID hashes a validated indexed function type without
// depending on its raw module type indexes. References expand their structural
// definitions; recursive edges are encoded by their distance to an ancestor on
// the current expansion path. Non-recursive sharing is deliberately expanded
// again, so one shared definition and two equivalent duplicate definitions
// produce the same id.
func (m *Module) structuralIndexedFuncTypeID(typeIdx uint32) (uint32, bool) {
	st, _, ok := m.subtypeByTypeIdxWithRecGroup(TypeIdx{Index: typeIdx})
	if !ok || st.Comp.Kind != CompFunc {
		return 0, false
	}

	const offset32 = 2166136261
	const prime32 = 16777619
	h := uint32(offset32)
	mix := func(b byte) { h ^= uint32(b); h *= prime32 }
	mixU32 := func(v uint32) {
		mix(byte(v))
		mix(byte(v >> 8))
		mix(byte(v >> 16))
		mix(byte(v >> 24))
	}

	path := make([]uint32, 0, 8)
	active := make(map[uint32]int)
	var writeValue func(ValType, int) bool
	var writeField func(FieldType, int) bool
	var writeType func(uint32) bool
	var flatIndex = func(idx TypeIdx, recGroup int) (uint32, bool) {
		flat, ok := m.flatTypeIdxInRecGroup(idx, recGroup)
		return uint32(flat), ok
	}

	writeValue = func(v ValType, recGroup int) bool {
		mix(byte(v.Kind))
		switch v.Kind {
		case ValNum:
			mix(byte(v.Num))
		case ValVec, ValBot:
			// The kind fully identifies these value types.
		case ValRef:
			if v.Ref.Nullable {
				mix(1)
			} else {
				mix(0)
			}
			if v.Ref.Exact {
				mix(1)
			} else {
				mix(0)
			}
			if v.Ref.Bare {
				mix(1)
			} else {
				mix(0)
			}
			mix(byte(v.Ref.Heap.Kind))
			switch v.Ref.Heap.Kind {
			case HeapAbs:
				mix(byte(v.Ref.Heap.Abs))
			case HeapTypeIndex:
				idx, ok := flatIndex(v.Ref.Heap.Type, recGroup)
				return ok && writeType(idx)
			case HeapDefType:
				def := v.Ref.Heap.Def
				if def == nil || int(def.GroupIndex) >= len(m.Types) || def.Index >= uint32(len(m.Types[def.GroupIndex].SubTypes)) {
					return false
				}
				idx := uint32(0)
				for gi := uint32(0); gi < def.GroupIndex; gi++ {
					idx += uint32(len(m.Types[gi].SubTypes))
				}
				return writeType(idx + def.Index)
			default:
				return false
			}
		default:
			return false
		}
		return true
	}

	writeField = func(field FieldType, recGroup int) bool {
		if field.Storage.Packed {
			mix(1)
			mix(byte(field.Storage.Pack))
		} else {
			mix(0)
			if !writeValue(field.Storage.Val, recGroup) {
				return false
			}
		}
		mix(byte(field.Mut))
		return true
	}

	writeType = func(index uint32) bool {
		if depth, ok := active[index]; ok {
			mix(0xf0)
			mixU32(uint32(len(path) - depth))
			return true
		}
		st, recGroup, ok := m.subtypeByTypeIdxWithRecGroup(TypeIdx{Index: index})
		if !ok {
			return false
		}
		active[index] = len(path)
		path = append(path, index)
		defer func() {
			path = path[:len(path)-1]
			delete(active, index)
		}()

		mix(0xf1)
		if st.Final {
			mix(1)
		} else {
			mix(0)
		}
		mixU32(uint32(len(st.Supers)))
		for _, super := range st.Supers {
			idx, ok := flatIndex(super, recGroup)
			if !ok || !writeType(idx) {
				return false
			}
		}
		if st.Metadata.Describes != nil {
			mix(1)
			idx, ok := flatIndex(*st.Metadata.Describes, recGroup)
			if !ok || !writeType(idx) {
				return false
			}
		} else {
			mix(0)
		}
		if st.Metadata.Descriptor != nil {
			mix(1)
			idx, ok := flatIndex(*st.Metadata.Descriptor, recGroup)
			if !ok || !writeType(idx) {
				return false
			}
		} else {
			mix(0)
		}

		mix(byte(st.Comp.Kind))
		switch st.Comp.Kind {
		case CompFunc:
			mixU32(uint32(len(st.Comp.Params)))
			for _, param := range st.Comp.Params {
				if !writeValue(param, recGroup) {
					return false
				}
			}
			mixU32(uint32(len(st.Comp.Results)))
			for _, result := range st.Comp.Results {
				if !writeValue(result, recGroup) {
					return false
				}
			}
		case CompStruct:
			mixU32(uint32(len(st.Comp.Fields)))
			for _, field := range st.Comp.Fields {
				if !writeField(field, recGroup) {
					return false
				}
			}
		case CompArray:
			if !writeField(st.Comp.Array, recGroup) {
				return false
			}
		default:
			return false
		}
		return true
	}

	if !writeType(typeIdx) {
		return 0, false
	}
	return h, true
}

func compTypeHasIndexedReferences(ft *CompType) bool {
	if ft == nil {
		return false
	}
	for _, values := range [][]ValType{ft.Params, ft.Results} {
		for _, value := range values {
			if value.Kind == ValRef && value.Ref.Heap.Kind != HeapAbs {
				return true
			}
		}
	}
	return false
}
