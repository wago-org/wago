package wasm

import (
	"crypto/sha256"
	"encoding/binary"
	"sync"
)

// structuralIndexedFuncTypeID preserves the compact historical discriminator
// used by metadata and diagnostics. Native calls additionally consume the
// collision-resistant StructuralTypeKey.
func (m *Module) structuralIndexedFuncTypeID(typeIdx uint32) (uint32, bool) {
	const offset32 = 2166136261
	const prime32 = 16777619
	h := uint32(offset32)
	ok := m.writeStructuralIndexedFuncType(typeIdx, func(b byte) {
		h ^= uint32(b)
		h *= prime32
	})
	return h, ok
}

type structuralTypeKeyResult struct {
	key uint64
	ok  bool
}

var structuralTypeCacheInitMu sync.Mutex

type structuralTypeKeyCache struct {
	mu     sync.Mutex
	owner  *RecType
	groups int
	flat   int
	keys   map[uint32]structuralTypeKeyResult
}

func (m *Module) structuralIndexedFuncTypeKey(typeIdx uint32) (uint64, bool) {
	structuralTypeCacheInitMu.Lock()
	if m.structuralTypeCache == nil {
		m.structuralTypeCache = &structuralTypeKeyCache{}
	}
	cache := m.structuralTypeCache
	structuralTypeCacheInitMu.Unlock()
	cache.mu.Lock()
	defer cache.mu.Unlock()

	var owner *RecType
	if len(m.Types) != 0 {
		owner = &m.Types[0]
	}
	flat := m.flattenedTypeCount()
	if cache.owner != owner || cache.groups != len(m.Types) || cache.flat != flat {
		cache.owner, cache.groups, cache.flat = owner, len(m.Types), flat
		cache.keys = nil
	}
	if result, ok := cache.keys[typeIdx]; ok {
		return result.key, result.ok
	}

	// Buffer the canonical stream and hash it in one chunk. This avoids a fresh
	// hash state plus one hash.Write call per byte, and the cached result makes
	// repeated compiler/backend queries constant work with no graph allocations.
	canonical := make([]byte, 0, 256)
	ok := m.writeStructuralIndexedFuncType(typeIdx, func(b byte) {
		canonical = append(canonical, b)
	})
	result := structuralTypeKeyResult{ok: ok}
	if ok {
		sum := sha256.Sum256(canonical)
		result.key = binary.LittleEndian.Uint64(sum[:8])
	}
	if cache.keys == nil {
		cache.keys = make(map[uint32]structuralTypeKeyResult)
	}
	cache.keys[typeIdx] = result
	return result.key, result.ok
}

func (m *Module) writeStructuralIndexedFuncType(typeIdx uint32, mix func(byte)) bool {
	// Bound adversarial recursive/DAG expansion without retaining canonical bytes.
	// Exceeding this budget fails closed through StructuralTypeKeyChecked.
	const maxStructuralTypeIdentityBytes = 1 << 20
	emitted := 0
	overflow := false
	rawMix := mix
	mix = func(b byte) {
		if overflow {
			return
		}
		emitted++
		if emitted > maxStructuralTypeIdentityBytes {
			overflow = true
			return
		}
		rawMix(b)
	}

	st, rootGroup, ok := m.subtypeByTypeIdxWithRecGroup(TypeIdx{Index: typeIdx})
	if !ok || st.Comp.Kind != CompFunc {
		return false
	}
	rootStart := uint32(0)
	for gi := 0; gi < rootGroup; gi++ {
		rootStart += uint32(len(m.Types[gi].SubTypes))
	}
	rootCount := uint32(len(m.Types[rootGroup].SubTypes))
	if typeIdx < rootStart || typeIdx >= rootStart+rootCount {
		return false
	}

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
	var writeTypeDefinition func(uint32) bool
	var flatIndex = func(idx TypeIdx, recGroup int) (uint32, bool) {
		flat, ok := m.flatTypeIdxInRecGroup(idx, recGroup)
		return uint32(flat), ok
	}

	writeValue = func(v ValType, recGroup int) bool {
		if overflow {
			return false
		}
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
		if overflow {
			return false
		}
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
		if overflow {
			return false
		}
		if rootCount > 1 && index >= rootStart && index < rootStart+rootCount {
			mix(0xf2)
			mixU32(index - rootStart)
			return true
		}
		return writeTypeDefinition(index)
	}

	writeTypeDefinition = func(index uint32) bool {
		if overflow {
			return false
		}
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

	if rootCount == 1 {
		return writeType(typeIdx) && !overflow
	}
	mix(0xf3)
	mixU32(rootCount)
	mixU32(typeIdx - rootStart)
	for i := uint32(0); i < rootCount; i++ {
		if !writeTypeDefinition(rootStart + i) {
			return false
		}
	}
	return !overflow
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
