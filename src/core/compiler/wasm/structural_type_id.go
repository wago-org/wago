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
	ok := m.writeStructuralIndexedFuncTypeLinear(typeIdx, func(b byte) {
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
	ok := m.writeStructuralIndexedFuncTypeLinear(typeIdx, func(b byte) {
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

// writeStructuralIndexedFuncType serializes a validated indexed function type
// in linear graph form. References within a recursive group use member positions;
// references to earlier groups use the complete canonical digest of that group's
// selected member. Structurally shared and duplicated subgraphs therefore encode
// identically without recursively expanding the same DAG at every use site.
func (m *Module) writeStructuralIndexedFuncTypeLinear(typeIdx uint32, mix func(byte)) bool {
	const maxCanonicalBytes = 1 << 20
	flatCount := m.flattenedTypeCount()
	if int(typeIdx) >= flatCount {
		return false
	}
	groupOf := make([]int, flatCount)
	position := make([]uint32, flatCount)
	starts := make([]uint32, len(m.Types))
	flat := uint32(0)
	for group := range m.Types {
		starts[group] = flat
		for member := range m.Types[group].SubTypes {
			if int(flat) >= flatCount {
				return false
			}
			groupOf[flat] = group
			position[flat] = uint32(member)
			flat++
		}
	}
	root, ok := m.TypeFunc(typeIdx)
	if !ok || root == nil {
		return false
	}

	groupBytes := make(map[int][]byte)
	memberDigests := make(map[uint32][32]byte)
	visiting := make(map[int]bool)
	totalBytes := 0
	appendByte := func(dst *[]byte, b byte) bool {
		totalBytes++
		if totalBytes > maxCanonicalBytes {
			return false
		}
		*dst = append(*dst, b)
		return true
	}
	appendU32 := func(dst *[]byte, value uint32) bool {
		return appendByte(dst, byte(value)) && appendByte(dst, byte(value>>8)) && appendByte(dst, byte(value>>16)) && appendByte(dst, byte(value>>24))
	}

	var buildGroup func(int) ([]byte, bool)
	var memberDigest func(uint32) ([32]byte, bool)
	var writeValue func(*[]byte, ValType, int) bool
	var writeField func(*[]byte, FieldType, int) bool
	writeRef := func(dst *[]byte, idx TypeIdx, currentGroup int) bool {
		resolved, ok := m.flatTypeIdxInRecGroup(idx, currentGroup)
		if !ok || resolved < 0 || resolved >= flatCount {
			return false
		}
		target := uint32(resolved)
		if groupOf[target] == currentGroup {
			return appendByte(dst, 0xf2) && appendU32(dst, position[target])
		}
		digest, ok := memberDigest(target)
		if !ok || !appendByte(dst, 0xf4) {
			return false
		}
		for _, b := range digest {
			if !appendByte(dst, b) {
				return false
			}
		}
		return true
	}

	writeValue = func(dst *[]byte, value ValType, currentGroup int) bool {
		if !appendByte(dst, byte(value.Kind)) {
			return false
		}
		switch value.Kind {
		case ValNum:
			return appendByte(dst, byte(value.Num))
		case ValVec, ValBot:
			return true
		case ValRef:
			for _, flag := range []bool{value.Ref.Nullable, value.Ref.Exact} {
				b := byte(0)
				if flag {
					b = 1
				}
				if !appendByte(dst, b) {
					return false
				}
			}
			if !appendByte(dst, byte(value.Ref.Heap.Kind)) {
				return false
			}
			switch value.Ref.Heap.Kind {
			case HeapAbs:
				return appendByte(dst, byte(value.Ref.Heap.Abs))
			case HeapTypeIndex:
				return writeRef(dst, value.Ref.Heap.Type, currentGroup)
			case HeapDefType:
				def := value.Ref.Heap.Def
				if def == nil || int(def.GroupIndex) >= len(starts) || def.Index >= uint32(len(m.Types[def.GroupIndex].SubTypes)) {
					return false
				}
				return writeRef(dst, TypeIdx{Index: starts[def.GroupIndex] + def.Index}, currentGroup)
			default:
				return false
			}
		default:
			return false
		}
	}

	writeField = func(dst *[]byte, field FieldType, currentGroup int) bool {
		if field.Storage.Packed {
			if !appendByte(dst, 1) || !appendByte(dst, byte(field.Storage.Pack)) {
				return false
			}
		} else if !appendByte(dst, 0) || !writeValue(dst, field.Storage.Val, currentGroup) {
			return false
		}
		return appendByte(dst, byte(field.Mut))
	}

	buildGroup = func(group int) ([]byte, bool) {
		if encoded, ok := groupBytes[group]; ok {
			return encoded, true
		}
		if group < 0 || group >= len(m.Types) || visiting[group] {
			return nil, false
		}
		visiting[group] = true
		defer delete(visiting, group)
		encoded := make([]byte, 0, 128)
		if !appendU32(&encoded, uint32(len(m.Types[group].SubTypes))) {
			return nil, false
		}
		for member := range m.Types[group].SubTypes {
			st := &m.Types[group].SubTypes[member]
			if !appendByte(&encoded, 0xf1) {
				return nil, false
			}
			final := byte(0)
			if st.Final {
				final = 1
			}
			if !appendByte(&encoded, final) || !appendU32(&encoded, uint32(len(st.Supers))) {
				return nil, false
			}
			for _, super := range st.Supers {
				if !writeRef(&encoded, super, group) {
					return nil, false
				}
			}
			for _, metadata := range []*TypeIdx{st.Metadata.Describes, st.Metadata.Descriptor} {
				if metadata == nil {
					if !appendByte(&encoded, 0) {
						return nil, false
					}
				} else if !appendByte(&encoded, 1) || !writeRef(&encoded, *metadata, group) {
					return nil, false
				}
			}
			if !appendByte(&encoded, byte(st.Comp.Kind)) {
				return nil, false
			}
			switch st.Comp.Kind {
			case CompFunc:
				if !appendU32(&encoded, uint32(len(st.Comp.Params))) {
					return nil, false
				}
				for _, param := range st.Comp.Params {
					if !writeValue(&encoded, param, group) {
						return nil, false
					}
				}
				if !appendU32(&encoded, uint32(len(st.Comp.Results))) {
					return nil, false
				}
				for _, result := range st.Comp.Results {
					if !writeValue(&encoded, result, group) {
						return nil, false
					}
				}
			case CompStruct:
				if !appendU32(&encoded, uint32(len(st.Comp.Fields))) {
					return nil, false
				}
				for _, field := range st.Comp.Fields {
					if !writeField(&encoded, field, group) {
						return nil, false
					}
				}
			case CompArray:
				if !writeField(&encoded, st.Comp.Array, group) {
					return nil, false
				}
			default:
				return nil, false
			}
		}
		groupBytes[group] = encoded
		return encoded, true
	}

	memberDigest = func(index uint32) ([32]byte, bool) {
		var zero [32]byte
		if digest, ok := memberDigests[index]; ok {
			return digest, true
		}
		if int(index) >= flatCount {
			return zero, false
		}
		group := groupOf[index]
		encoded, ok := buildGroup(group)
		if !ok {
			return zero, false
		}
		var prefix [9]byte
		prefix[0] = 0xf3
		binary.LittleEndian.PutUint32(prefix[1:5], uint32(len(m.Types[group].SubTypes)))
		binary.LittleEndian.PutUint32(prefix[5:9], position[index])
		h := sha256.New()
		_, _ = h.Write(prefix[:])
		_, _ = h.Write(encoded)
		var digest [32]byte
		copy(digest[:], h.Sum(nil))
		memberDigests[index] = digest
		return digest, true
	}

	group := groupOf[typeIdx]
	encoded, ok := buildGroup(group)
	if !ok {
		return false
	}
	prefix := [9]byte{0xf3}
	binary.LittleEndian.PutUint32(prefix[1:5], uint32(len(m.Types[group].SubTypes)))
	binary.LittleEndian.PutUint32(prefix[5:9], position[typeIdx])
	for _, b := range prefix {
		mix(b)
	}
	for _, b := range encoded {
		mix(b)
	}
	return true
}

// writeStructuralIndexedFuncTypeExpanded is retained as an exact baseline for recursive expansion.
func (m *Module) writeStructuralIndexedFuncTypeExpanded(typeIdx uint32, mix func(byte)) bool {
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
