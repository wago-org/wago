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
	mu           sync.Mutex
	owner        *RecType
	groups       int
	flat         int
	keys         map[uint32]structuralTypeKeyResult
	groupDigests map[int][32]byte
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
	// Validated modules are immutable while compiling. Check the stable outer
	// type-slice identity before asking for the flattened count: a cache hit
	// should not re-enter the module type-directory lookup on every query.
	if cache.owner == owner && cache.groups == len(m.Types) {
		if result, ok := cache.keys[typeIdx]; ok {
			return result.key, result.ok
		}
	}
	flat := m.flattenedTypeCount()
	if cache.owner != owner || cache.groups != len(m.Types) || cache.flat != flat {
		cache.owner, cache.groups, cache.flat = owner, len(m.Types), flat
		cache.keys = nil
		cache.groupDigests = nil
	}
	if result, ok := cache.keys[typeIdx]; ok {
		return result.key, result.ok
	}
	// A completed group digest is enough to derive any member key. Avoid
	// rebuilding per-query graph scratch when the module cache already owns it.
	directory := m.typeIndex()
	if uint(typeIdx) < uint(len(directory.flat)) {
		ref := directory.flat[typeIdx]
		if ref.st.Comp.Kind == CompFunc {
			if groupDigest, ok := cache.groupDigests[ref.recGroup]; ok {
				key := structuralGroupMemberKey(groupDigest, len(m.Types[ref.recGroup].SubTypes), uint32(int(typeIdx)-directory.bases[ref.recGroup]))
				result := structuralTypeKeyResult{key: key, ok: true}
				if cache.keys == nil {
					cache.keys = make(map[uint32]structuralTypeKeyResult)
				}
				cache.keys[typeIdx] = result
				return result.key, true
			}
		}
	}

	// The serializer emits the compact member digest. Retain its bytes briefly
	// so the 64-bit key is derived without a second hash pass.
	canonical := make([]byte, 0, 32)
	ok := m.writeStructuralIndexedFuncTypeKey(typeIdx, func(b byte) {
		canonical = append(canonical, b)
	}, &cache.groupDigests)
	result := structuralTypeKeyResult{ok: ok}
	if ok && len(canonical) == 32 {
		result.key = binary.LittleEndian.Uint64(canonical[:8])
	} else if ok {
		result.ok = false
	}
	if cache.keys == nil {
		cache.keys = make(map[uint32]structuralTypeKeyResult)
	}
	cache.keys[typeIdx] = result
	return result.key, result.ok
}

func structuralGroupMemberKey(groupDigest [32]byte, groupSize int, member uint32) uint64 {
	digest := structuralGroupMemberDigest(groupDigest, groupSize, member)
	return binary.LittleEndian.Uint64(digest[:8])
}

// writeStructuralIndexedFuncType serializes a validated indexed function type
// in linear graph form. References within a recursive group use member positions;
// references to earlier groups use the complete canonical digest of that group's
// selected member. Structurally shared and duplicated subgraphs therefore encode
// identically without recursively expanding the same DAG at every use site.
func (m *Module) writeStructuralIndexedFuncTypeLinear(typeIdx uint32, mix func(byte)) bool {
	return m.writeStructuralIndexedFuncType(typeIdx, mix, nil, false)
}

// writeStructuralIndexedFuncTypeKey emits the collision-resistant digest for
// one indexed function type. Recursive-group bytes are hashed once, and each
// member key is derived from that digest and its ordinal. The module cache
// shares completed group digests across all type-key queries.
func (m *Module) writeStructuralIndexedFuncTypeKey(typeIdx uint32, mix func(byte), groupDigests *map[int][32]byte) bool {
	return m.writeStructuralIndexedFuncType(typeIdx, mix, groupDigests, true)
}

// structuralEncodingBudget bounds the complete graph, including dependencies.
// Keep these operations out of line so each field does not duplicate slice
// growth and limit checks in the minimal runtime.
type structuralEncodingBudget int

//go:noinline
func (budget *structuralEncodingBudget) appendByte(dst *[]byte, b byte) bool {
	*budget++
	if *budget > 1<<20 {
		return false
	}
	*dst = append(*dst, b)
	return true
}

//go:noinline
func (budget *structuralEncodingBudget) appendU32(dst *[]byte, value uint32) bool {
	if *budget > 1<<20-4 {
		return false
	}
	*budget += 4
	*dst = binary.LittleEndian.AppendUint32(*dst, value)
	return true
}

func (m *Module) writeStructuralIndexedFuncType(typeIdx uint32, mix func(byte), groupDigests *map[int][32]byte, compactKey bool) bool {
	directory := m.typeIndex()
	flatCount := len(directory.flat)
	if uint(typeIdx) >= uint(flatCount) {
		return false
	}
	root, ok := m.TypeFunc(typeIdx)
	if !ok || root == nil {
		return false
	}

	groupBytes := make(map[int][]byte)
	memberDigests := make(map[uint32][32]byte)
	visiting := make(map[int]bool)
	var budget structuralEncodingBudget

	var buildGroup func(int) ([]byte, bool)
	var ensureGroupDigest func(int) ([32]byte, bool)
	var memberDigest func(uint32) ([32]byte, bool)
	var writeValue func(*[]byte, ValType, int) bool
	var writeField func(*[]byte, FieldType, int) bool
	resolveType := func(idx TypeIdx, currentGroup int) (int, bool) {
		if !idx.Rec {
			if uint64(idx.Index) >= uint64(flatCount) {
				return 0, false
			}
			return int(idx.Index), true
		}
		if currentGroup < 0 || currentGroup >= len(m.Types) || idx.Index >= uint32(len(m.Types[currentGroup].SubTypes)) {
			return 0, false
		}
		return directory.bases[currentGroup] + int(idx.Index), true
	}
	writeRef := func(dst *[]byte, idx TypeIdx, currentGroup int) bool {
		resolved, ok := resolveType(idx, currentGroup)
		if !ok || resolved < 0 || resolved >= flatCount {
			return false
		}
		target := uint32(resolved)
		targetGroup := directory.flat[target].recGroup
		if targetGroup == currentGroup {
			position := uint32(resolved - directory.bases[targetGroup])
			return budget.appendByte(dst, 0xf2) && budget.appendU32(dst, position)
		}
		digest, ok := memberDigest(target)
		if !ok || !budget.appendByte(dst, 0xf4) {
			return false
		}
		for _, b := range digest {
			if !budget.appendByte(dst, b) {
				return false
			}
		}
		return true
	}

	writeValue = func(dst *[]byte, value ValType, currentGroup int) bool {
		if !budget.appendByte(dst, byte(value.Kind())) {
			return false
		}
		switch value.Kind() {
		case ValNum:
			return budget.appendByte(dst, byte(value.Num()))
		case ValVec, ValBot:
			return true
		case ValRef:
			rt := value.Ref()
			for _, flag := range []bool{rt.Nullable(), rt.Exact()} {
				b := byte(0)
				if flag {
					b = 1
				}
				if !budget.appendByte(dst, b) {
					return false
				}
			}
			heap := rt.Heap()
			if !budget.appendByte(dst, byte(heap.Kind())) {
				return false
			}
			switch heap.Kind() {
			case HeapAbs:
				return budget.appendByte(dst, byte(heap.Abs()))
			case HeapTypeIndex:
				return writeRef(dst, heap.Type(), currentGroup)
			case HeapDefType:
				group, member, _, valid := heap.Def()
				if !valid || uint(group) >= uint(len(directory.bases)-1) || uint(member) >= uint(len(m.Types[group].SubTypes)) {
					return false
				}
				return writeRef(dst, TypeIdx{Index: uint32(directory.bases[group]) + member}, currentGroup)
			default:
				return false
			}
		default:
			return false
		}
	}

	writeField = func(dst *[]byte, field FieldType, currentGroup int) bool {
		storage := field.Storage()
		if storage.Packed() {
			if !budget.appendByte(dst, 1) || !budget.appendByte(dst, byte(storage.Pack())) {
				return false
			}
		} else if !budget.appendByte(dst, 0) || !writeValue(dst, storage.Val(), currentGroup) {
			return false
		}
		return budget.appendByte(dst, byte(field.Mut()))
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
		if !budget.appendU32(&encoded, uint32(len(m.Types[group].SubTypes))) {
			return nil, false
		}
		for member := range m.Types[group].SubTypes {
			st := &m.Types[group].SubTypes[member]
			if !budget.appendByte(&encoded, 0xf1) {
				return nil, false
			}
			final := byte(0)
			if st.Final {
				final = 1
			}
			if !budget.appendByte(&encoded, final) || !budget.appendU32(&encoded, uint32(len(st.Supers))) {
				return nil, false
			}
			for _, super := range st.Supers {
				if !writeRef(&encoded, super, group) {
					return nil, false
				}
			}
			for _, metadata := range []OptionalTypeIdx{st.Metadata.Describes, st.Metadata.Descriptor} {
				idx, present := metadata.Get()
				if !present {
					if !budget.appendByte(&encoded, 0) {
						return nil, false
					}
				} else if !budget.appendByte(&encoded, 1) || !writeRef(&encoded, idx, group) {
					return nil, false
				}
			}
			if !budget.appendByte(&encoded, byte(st.Comp.Kind)) {
				return nil, false
			}
			switch st.Comp.Kind {
			case CompFunc:
				if !budget.appendU32(&encoded, uint32(len(st.Comp.Params))) {
					return nil, false
				}
				for _, param := range st.Comp.Params {
					if !writeValue(&encoded, param, group) {
						return nil, false
					}
				}
				if !budget.appendU32(&encoded, uint32(len(st.Comp.Results))) {
					return nil, false
				}
				for _, result := range st.Comp.Results {
					if !writeValue(&encoded, result, group) {
						return nil, false
					}
				}
			case CompStruct:
				if !budget.appendU32(&encoded, uint32(len(st.Comp.Fields))) {
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
		// ensureGroupDigest hashes and publishes these bytes exactly once.
		return encoded, true
	}
	ensureGroupDigest = func(group int) ([32]byte, bool) {
		var zero [32]byte
		if compactKey && groupDigests != nil && *groupDigests != nil {
			if digest, ok := (*groupDigests)[group]; ok {
				return digest, true
			}
		}
		encoded, ok := buildGroup(group)
		if !ok {
			return zero, false
		}
		digest := sha256.Sum256(encoded)
		if compactKey && groupDigests != nil {
			if *groupDigests == nil {
				*groupDigests = make(map[int][32]byte)
			}
			(*groupDigests)[group] = digest
		}
		return digest, true
	}

	memberDigest = func(index uint32) ([32]byte, bool) {
		var zero [32]byte
		if digest, ok := memberDigests[index]; ok {
			return digest, true
		}
		if uint(index) >= uint(flatCount) {
			return zero, false
		}
		group := directory.flat[index].recGroup
		if compactKey {
			groupDigest, ok := ensureGroupDigest(group)
			if !ok {
				return zero, false
			}
			digest := structuralGroupMemberDigest(groupDigest, len(m.Types[group].SubTypes), index-uint32(directory.bases[group]))
			memberDigests[index] = digest
			return digest, true
		}
		var prefix [9]byte
		prefix[0] = 0xf3
		binary.LittleEndian.PutUint32(prefix[1:5], uint32(len(m.Types[group].SubTypes)))
		binary.LittleEndian.PutUint32(prefix[5:9], index-uint32(directory.bases[group]))
		h := sha256.New()
		_, _ = h.Write(prefix[:])
		encoded, ok := buildGroup(group)
		if !ok {
			return zero, false
		}
		_, _ = h.Write(encoded)
		var digest [32]byte
		copy(digest[:], h.Sum(nil))
		memberDigests[index] = digest
		return digest, true
	}

	group := directory.flat[typeIdx].recGroup
	if compactKey {
		digest, ok := memberDigest(typeIdx)
		if !ok {
			return false
		}
		for _, b := range digest {
			mix(b)
		}
		return true
	}
	encoded, ok := buildGroup(group)
	if !ok {
		return false
	}
	prefix := [9]byte{0xf3}
	binary.LittleEndian.PutUint32(prefix[1:5], uint32(len(m.Types[group].SubTypes)))
	binary.LittleEndian.PutUint32(prefix[5:9], typeIdx-uint32(directory.bases[group]))
	for _, b := range prefix {
		mix(b)
	}
	for _, b := range encoded {
		mix(b)
	}
	return true
}

func structuralGroupMemberDigest(groupDigest [32]byte, groupSize int, member uint32) [32]byte {
	var input [41]byte
	input[0] = 0xf3
	binary.LittleEndian.PutUint32(input[1:5], uint32(groupSize))
	binary.LittleEndian.PutUint32(input[5:9], member)
	copy(input[9:], groupDigest[:])
	return sha256.Sum256(input[:])
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
		mix(byte(v.Kind()))
		switch v.Kind() {
		case ValNum:
			mix(byte(v.Num()))
		case ValVec, ValBot:
			// The kind fully identifies these value types.
		case ValRef:
			rt := v.Ref()
			if rt.Nullable() {
				mix(1)
			} else {
				mix(0)
			}
			if rt.Exact() {
				mix(1)
			} else {
				mix(0)
			}
			heap := rt.Heap()
			mix(byte(heap.Kind()))
			switch heap.Kind() {
			case HeapAbs:
				mix(byte(heap.Abs()))
			case HeapTypeIndex:
				idx, ok := flatIndex(heap.Type(), recGroup)
				return ok && writeType(idx)
			case HeapDefType:
				group, member, _, valid := heap.Def()
				if !valid || uint(group) >= uint(len(m.Types)) || uint(member) >= uint(len(m.Types[group].SubTypes)) {
					return false
				}
				idx := uint32(0)
				for gi := uint32(0); gi < group; gi++ {
					idx += uint32(len(m.Types[gi].SubTypes))
				}
				return writeType(idx + member)
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
		storage := field.Storage()
		if storage.Packed() {
			mix(1)
			mix(byte(storage.Pack()))
		} else {
			mix(0)
			if !writeValue(storage.Val(), recGroup) {
				return false
			}
		}
		mix(byte(field.Mut()))
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
		if describes, present := st.Metadata.Describes.Get(); present {
			mix(1)
			idx, ok := flatIndex(describes, recGroup)
			if !ok || !writeType(idx) {
				return false
			}
		} else {
			mix(0)
		}
		if descriptor, present := st.Metadata.Descriptor.Get(); present {
			mix(1)
			idx, ok := flatIndex(descriptor, recGroup)
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
			if value.Kind() == ValRef && value.Ref().Heap().Kind() != HeapAbs {
				return true
			}
		}
	}
	return false
}
