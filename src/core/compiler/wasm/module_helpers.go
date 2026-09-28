package wasm

import (
	"crypto/sha256"
	"encoding/binary"
	"sort"
	"strings"
	"sync"
)

type moduleTypeIndexDirectory struct {
	owner         *RecType
	groups        int
	bases         []int
	flat          []moduleSubTypeRef
	canonical     sync.Once
	canonicalIDs  []uint32
	superEdges    sync.Once
	superOffsets  []int
	superChildren []uint32
	superEdgesOK  bool
}

type moduleImportIndexDirectory struct {
	owner    *Import
	count    int
	funcs    []uint32
	tables   []uint32
	memories []uint32
	globals  []uint32
	tags     int
}

var moduleTypeIndexDirectoryMu sync.RWMutex
var moduleImportIndexDirectoryMu sync.RWMutex

// invalidateTypeAnalysisCaches starts a fresh validation view. Modules are
// immutable while a validation result is consumed, but callers may edit a
// module and validate it again; clear pointer-based indexes at that boundary so
// same-slice edits cannot leave stale subtype pointers or structural keys.
func (m *Module) invalidateTypeAnalysisCaches() {
	if m == nil {
		return
	}
	moduleTypeIndexDirectoryMu.Lock()
	m.typeIndexDirectory = nil
	moduleTypeIndexDirectoryMu.Unlock()
	moduleImportIndexDirectoryMu.Lock()
	m.importIndexDirectory = nil
	moduleImportIndexDirectoryMu.Unlock()

	structuralTypeCacheInitMu.Lock()
	if cache := m.structuralTypeCache; cache != nil {
		cache.mu.Lock()
		cache.owner, cache.groups, cache.flat = nil, 0, 0
		cache.keys = nil
		cache.groupDigests = nil
		cache.mu.Unlock()
	}
	structuralTypeCacheInitMu.Unlock()
}

func (m *Module) typeIndex() *moduleTypeIndexDirectory {
	if m == nil {
		return nil
	}
	var owner *RecType
	if len(m.Types) != 0 {
		owner = &m.Types[0]
	}
	moduleTypeIndexDirectoryMu.RLock()
	directory := m.typeIndexDirectory
	if directory != nil && directory.owner == owner && directory.groups == len(m.Types) {
		moduleTypeIndexDirectoryMu.RUnlock()
		return directory
	}
	moduleTypeIndexDirectoryMu.RUnlock()

	moduleTypeIndexDirectoryMu.Lock()
	defer moduleTypeIndexDirectoryMu.Unlock()
	directory = m.typeIndexDirectory
	if directory != nil && directory.owner == owner && directory.groups == len(m.Types) {
		return directory
	}
	directory = &moduleTypeIndexDirectory{owner: owner, groups: len(m.Types), bases: make([]int, len(m.Types)+1)}
	total := 0
	for group := range m.Types {
		total += len(m.Types[group].SubTypes)
	}
	directory.flat = make([]moduleSubTypeRef, 0, total)
	total = 0
	for group := range m.Types {
		directory.bases[group] = total
		for member := range m.Types[group].SubTypes {
			directory.flat = append(directory.flat, moduleSubTypeRef{st: &m.Types[group].SubTypes[member], recGroup: group})
		}
		total += len(m.Types[group].SubTypes)
	}
	directory.bases[len(m.Types)] = total
	m.typeIndexDirectory = directory
	return directory
}

func (m *Module) importIndex() *moduleImportIndexDirectory {
	if m == nil {
		return nil
	}
	var owner *Import
	if len(m.Imports) != 0 {
		owner = &m.Imports[0]
	}
	moduleImportIndexDirectoryMu.RLock()
	directory := m.importIndexDirectory
	if directory != nil && directory.owner == owner && directory.count == len(m.Imports) {
		moduleImportIndexDirectoryMu.RUnlock()
		return directory
	}
	moduleImportIndexDirectoryMu.RUnlock()

	moduleImportIndexDirectoryMu.Lock()
	defer moduleImportIndexDirectoryMu.Unlock()
	directory = m.importIndexDirectory
	if directory != nil && directory.owner == owner && directory.count == len(m.Imports) {
		return directory
	}
	directory = &moduleImportIndexDirectory{owner: owner, count: len(m.Imports)}
	for i := range m.Imports {
		importType := &m.Imports[i].Type
		switch importType.Kind {
		case ExternFunc:
			directory.funcs = append(directory.funcs, uint32(i))
		case ExternTable:
			directory.tables = append(directory.tables, uint32(i))
		case ExternMem:
			directory.memories = append(directory.memories, uint32(i))
		case ExternGlobal:
			directory.globals = append(directory.globals, uint32(i))
		case ExternTag:
			directory.tags++
		}
	}
	m.importIndexDirectory = directory
	return directory
}

// importEntry resolves an index in one import kind, or returns its local index.
// The measured crossover for cold directories is between 32 and 64 imports.
func (m *Module) importEntry(kind ExternKind, idx uint32) (entry int, local uint64) {
	if len(m.Imports) <= 32 {
		remaining := uint64(idx)
		for i := range m.Imports {
			if m.Imports[i].Type.Kind == kind {
				if remaining == 0 {
					return i, 0
				}
				remaining--
			}
		}
		return -1, remaining
	}
	directory := m.importIndex()
	var indexes []uint32
	switch kind {
	case ExternFunc:
		indexes = directory.funcs
	case ExternTable:
		indexes = directory.tables
	case ExternMem:
		indexes = directory.memories
	case ExternGlobal:
		indexes = directory.globals
	}
	if uint64(idx) < uint64(len(indexes)) {
		return int(indexes[idx]), 0
	}
	return -1, uint64(idx) - uint64(len(indexes))
}

// LocalCount returns the size of the wasm local index space for parameters plus
// compact declared-local runs. The overflow result is true only if the uint64
// count wrapped; callers with smaller frame limits must still enforce them.
func LocalCount(params []ValType, runs []LocalRun) (count uint64, overflow bool) {
	count = uint64(len(params))
	for _, run := range runs {
		if ^uint64(0)-count < uint64(run.Count) {
			return 0, true
		}
		count += uint64(run.Count)
	}
	return count, false
}

// LocalType resolves a wasm local index without expanding run-length encoded
// declared locals, keeping validation/build/codegen memory proportional to local
// runs rather than potentially enormous local counts.
func LocalType(params []ValType, runs []LocalRun, idx uint32) (ValType, bool) {
	if uint64(idx) < uint64(len(params)) {
		return params[idx], true
	}
	rem := uint64(idx) - uint64(len(params))
	for _, run := range runs {
		if rem < uint64(run.Count) {
			return run.Type, true
		}
		rem -= uint64(run.Count)
	}
	return ValType{}, false
}

// LocalTypeIndexed resolves an index using absolute run ends prepared once by
// the containing function. It preserves compact run-length declarations while
// making repeated late-local lookups logarithmic in the number of runs.
func LocalTypeIndexed(params []ValType, runs []LocalRun, runEnds []uint64, idx uint32) (ValType, bool) {
	if uint64(idx) < uint64(len(params)) {
		return params[idx], true
	}
	// Measured late-local lookup crosses over around four runs. One or two
	// runs are cheaper to scan and do not need an index allocation.
	if len(runs) <= 2 || len(runEnds) != len(runs) {
		return LocalType(params, runs, idx)
	}
	i := sort.Search(len(runEnds), func(i int) bool { return runEnds[i] > uint64(idx) })
	if i == len(runs) {
		return ValType{}, false
	}
	return runs[i].Type, true
}

// GlobalValueType returns the canonical global value type.
func GlobalValueType(gt GlobalType) ValType { return gt.Type }

// TableRefType returns the canonical table element reference type.
func TableRefType(tt TableType) RefType { return tt.Ref }

// TableType resolves a table index across imported tables followed by local
// definitions without materializing a parallel index. The module validator,
// frontend support pass, and direct backend use the same declaration order.
func (m *Module) TableType(idx uint32) (TableType, bool) {
	if m == nil {
		return TableType{}, false
	}
	entry, local := m.importEntry(ExternTable, idx)
	if entry >= 0 {
		return m.Imports[entry].Type.TableType(), true
	}
	if local >= uint64(len(m.Tables)) {
		return TableType{}, false
	}
	return m.Tables[int(local)].Type, true
}

func TableAddrType(tt TableType) ValType {
	if tt.Limits.Addr64 {
		return I64
	}
	return I32
}

// MemoryType resolves a memory index across imported memories followed by local
// definitions without materializing a parallel index.
func (m *Module) MemoryType(idx uint32) (MemType, bool) {
	if m == nil {
		return MemType{}, false
	}
	entry, local := m.importEntry(ExternMem, idx)
	if entry >= 0 {
		return m.Imports[entry].Type.MemType(), true
	}
	if local >= uint64(len(m.Memories)) {
		return MemType{}, false
	}
	return m.Memories[int(local)], true
}

func MemoryAddrType(mt MemType) ValType {
	if mt.Limits.Addr64 {
		return I64
	}
	return I32
}

// IsNumericGlobalType reports whether wago's runtime/backend currently support
// the value type for global storage and global.get/global.set codegen.
func IsNumericGlobalType(t ValType) bool {
	return equalValType(t, I32) || equalValType(t, I64) || equalValType(t, F32) || equalValType(t, F64)
}

// EncodeValType returns the one-byte encoding for MVP numeric/vector value
// types and bare nullable reference aliases. Unlike semantic equality, this
// helper intentionally observes RefType.Bare so an explicit (ref null ...)
// value type is not silently rewritten as shorthand during a binary round trip.
func EncodeValType(t ValType) (byte, bool) {
	switch t.Kind() {
	case ValNum:
		switch t.Num() {
		case NumI32, NumI64, NumF32, NumF64:
			return byte(t.Num()), true
		}
	case ValVec:
		return 0x7b, true
	case ValRef:
		rt := t.Ref()
		if rt.Bare() && rt.Nullable() && !rt.Exact() && rt.Heap().Kind() == HeapAbs {
			return byte(rt.Heap().Abs()), true
		}
	}
	return 0, false
}

// MustEncodeValType is EncodeValType for test/build helpers where unsupported
// value-type encodings are programmer errors.
func MustEncodeValType(t ValType) byte {
	b, ok := EncodeValType(t)
	if !ok {
		panic("wasm: value type has no one-byte encoding: " + t.String())
	}
	return b
}

// FuncTypeIndex returns the declared type index for a global function index.
func (m *Module) FuncTypeIndex(idx uint32) (TypeIdx, bool) {
	if m == nil {
		return TypeIdx{}, false
	}
	entry, local := m.importEntry(ExternFunc, idx)
	if entry >= 0 {
		return m.Imports[entry].Type.FuncType(), true
	}
	if local >= uint64(len(m.FuncTypes)) {
		return TypeIdx{}, false
	}
	return m.FuncTypes[int(local)], true
}

// FuncSignature returns the function signature for a global function index.
func (m *Module) FuncSignature(idx uint32) (*CompType, bool) {
	typeIdx, ok := m.FuncTypeIndex(idx)
	if !ok {
		return nil, false
	}
	return m.typeFunc(typeIdx)
}

// ImportFuncType returns the stored signature for one import-section entry.
// importIndex is the index in Imports, not the function index space. The
// returned pointer aliases immutable module type storage.
func (m *Module) ImportFuncType(importIndex int) (*CompType, bool) {
	if importIndex < 0 || importIndex >= len(m.Imports) || m.Imports[importIndex].Type.Kind != ExternFunc {
		return nil, false
	}
	return m.typeFunc(m.Imports[importIndex].Type.FuncType())
}

// LocalFuncType returns the stored function signature for a local
// (non-imported) function index. The returned pointer aliases module storage and
// may contain recursive-local TypeIdx values for signatures decoded inside
// recursive type groups. Use ResolvedLocalFuncType when callers need flattened
// module type indexes.
func (m *Module) LocalFuncType(localIdx int) (*CompType, bool) {
	if localIdx < 0 || localIdx >= len(m.FuncTypes) {
		return nil, false
	}
	return m.typeFunc(m.FuncTypes[localIdx])
}

// ResolvedLocalFuncType returns a copy of the local function signature with any
// recursive-local type indexes resolved to flattened absolute module indexes.
func (m *Module) ResolvedLocalFuncType(localIdx int) (*CompType, bool) {
	var ct CompType
	if !m.ResolveLocalFuncType(localIdx, &ct) {
		return nil, false
	}
	return &ct, true
}

// ResolveLocalFuncType writes the local function signature to dst with any
// recursive-local type indexes resolved. Unlike ResolvedLocalFuncType, callers
// that already own scratch storage need not allocate a CompType wrapper. When
// no rewrite is needed, the immutable parameter/result slices alias the module.
func (m *Module) ResolveLocalFuncType(localIdx int, dst *CompType) bool {
	if localIdx < 0 || localIdx >= len(m.FuncTypes) {
		return false
	}
	st, recGroup, ok := m.subtypeByTypeIdxWithRecGroup(m.FuncTypes[localIdx])
	if !ok || st.Comp.Kind != CompFunc {
		return false
	}
	if funcTypeHasRecIndexes(&st.Comp) {
		*dst = m.resolveCompTypeRecIndexes(st.Comp, recGroup)
	} else {
		*dst = st.Comp
	}
	return true
}

func (m *Module) subtypeByTypeIdx(idx TypeIdx) (*SubType, bool) {
	st, _, ok := m.subtypeByTypeIdxWithRecGroup(idx)
	return st, ok
}

func (m *Module) subtypeByTypeIdxWithRecGroup(idx TypeIdx) (*SubType, int, bool) {
	if idx.Rec {
		return nil, 0, false
	}
	if len(m.Types) <= 8 {
		remaining := uint64(idx.Index)
		for group := range m.Types {
			if remaining < uint64(len(m.Types[group].SubTypes)) {
				return &m.Types[group].SubTypes[remaining], group, true
			}
			remaining -= uint64(len(m.Types[group].SubTypes))
		}
		return nil, 0, false
	}
	directory := m.typeIndex()
	if uint64(idx.Index) >= uint64(len(directory.flat)) {
		return nil, 0, false
	}
	ref := directory.flat[idx.Index]
	return ref.st, ref.recGroup, true
}

func (m *Module) flattenedTypeCount() int {
	return len(m.typeIndex().flat)
}

func (m *Module) typeFunc(idx TypeIdx) (*CompType, bool) {
	st, ok := m.subtypeByTypeIdx(idx)
	if !ok || st.Comp.Kind != CompFunc {
		return nil, false
	}
	return &st.Comp, true
}

func (m *Module) resolvedTypeFunc(idx TypeIdx) (*CompType, bool) {
	var ct CompType
	if !m.resolveTypeFunc(idx, &ct, false) {
		return nil, false
	}
	return &ct, true
}

// ResolveTypeFunc writes the function signature at a flattened module type
// index to dst. Non-recursive parameter and result slices alias immutable
// module storage; recursive-local indexes are resolved into exact owned slices.
// Callers must not mutate aliased slices and must not retain them beyond m.
func (m *Module) ResolveTypeFunc(typeIdx uint32, dst *CompType) bool {
	return m.resolveTypeFunc(TypeIdx{Index: typeIdx}, dst, true)
}

func (m *Module) resolveTypeFunc(idx TypeIdx, dst *CompType, aliasNonRecursive bool) bool {
	st, recGroup, ok := m.subtypeByTypeIdxWithRecGroup(idx)
	if !ok || st.Comp.Kind != CompFunc || dst == nil {
		return false
	}
	if aliasNonRecursive && !funcTypeHasRecIndexes(&st.Comp) {
		*dst = st.Comp
	} else {
		*dst = m.resolveCompTypeRecIndexes(st.Comp, recGroup)
	}
	return true
}

func funcTypeHasRecIndexes(ct *CompType) bool {
	for _, t := range ct.Params {
		if t.Kind() == ValRef && t.Ref().Heap().Kind() == HeapTypeIndex && t.Ref().Heap().Type().Rec {
			return true
		}
	}
	for _, t := range ct.Results {
		if t.Kind() == ValRef && t.Ref().Heap().Kind() == HeapTypeIndex && t.Ref().Heap().Type().Rec {
			return true
		}
	}
	return false
}

// TypeFunc returns the stored function type at a flattened module type index.
// The returned pointer aliases module storage and may contain recursive-local
// TypeIdx values for signatures decoded inside recursive type groups. Use
// ResolvedTypeFunc when callers need flattened module type indexes.
func (m *Module) TypeFunc(typeIdx uint32) (*CompType, bool) {
	return m.typeFunc(TypeIdx{Index: typeIdx})
}

// ResolvedTypeFunc returns a copy of the function type at a flattened module
// type index with any recursive-local type indexes resolved to flattened
// absolute module indexes.
func (m *Module) ResolvedTypeFunc(typeIdx uint32) (*CompType, bool) {
	return m.resolvedTypeFunc(TypeIdx{Index: typeIdx})
}

// ReferenceTypeSubtype reports the validated Core reference subtyping relation
// within this module, including structural equivalence and declared super chains.
func (m *Module) ReferenceTypeSubtype(actual, required RefType) bool {
	return (&moduleValidator{m: m}).refSubtype(actual, required)
}

func (m *Module) flatTypeIdxInRecGroup(idx TypeIdx, recGroup int) (int, bool) {
	directory := m.typeIndex()
	if !idx.Rec {
		if uint64(idx.Index) >= uint64(len(directory.flat)) {
			return 0, false
		}
		return int(idx.Index), true
	}
	if recGroup < 0 || recGroup >= len(m.Types) || idx.Index >= uint32(len(m.Types[recGroup].SubTypes)) {
		return 0, false
	}
	return directory.bases[recGroup] + int(idx.Index), true
}

func (m *Module) resolveCompTypeRecIndexes(ct CompType, recGroup int) CompType {
	switch ct.Kind {
	case CompFunc:
		if len(ct.Params) > 0 {
			params := make([]ValType, len(ct.Params))
			for i, t := range ct.Params {
				params[i] = m.resolveValTypeRecIndexes(t, recGroup)
			}
			ct.Params = params
		}
		if len(ct.Results) > 0 {
			results := make([]ValType, len(ct.Results))
			for i, t := range ct.Results {
				results[i] = m.resolveValTypeRecIndexes(t, recGroup)
			}
			ct.Results = results
		}
	case CompStruct:
		if len(ct.Fields) > 0 {
			fields := make([]FieldType, len(ct.Fields))
			for i, f := range ct.Fields {
				fields[i] = m.resolveFieldTypeRecIndexes(f, recGroup)
			}
			ct.Fields = fields
		}
	case CompArray:
		ct.Array = m.resolveFieldTypeRecIndexes(ct.Array, recGroup)
	}
	return ct
}

func (m *Module) resolveFieldTypeRecIndexes(ft FieldType, recGroup int) FieldType {
	storage := ft.Storage()
	if storage.Packed() {
		return ft
	}
	return NewFieldType(StorageVal(m.resolveValTypeRecIndexes(storage.Val(), recGroup)), ft.Mut())
}

func (m *Module) resolveValTypeRecIndexes(t ValType, recGroup int) ValType {
	if t.Kind() != ValRef {
		return t
	}
	return RefVal(m.resolveRefTypeRecIndexes(t.Ref(), recGroup))
}

func (m *Module) resolveRefTypeRecIndexes(rt RefType, recGroup int) RefType {
	heap := rt.Heap()
	if heap.Kind() != HeapTypeIndex {
		return rt
	}
	return rt.WithHeap(IndexedHeap(m.resolveTypeIdxRecIndex(heap.Type(), recGroup)))
}

func (m *Module) resolveTypeIdxRecIndex(idx TypeIdx, recGroup int) TypeIdx {
	flat, ok := m.flatTypeIdxInRecGroup(idx, recGroup)
	if !ok {
		return idx
	}
	return TypeIdx{Index: uint32(flat)}
}

// GlobalTypeByIndex returns the declared type for a wasm global index.
func (m *Module) GlobalTypeByIndex(idx uint32) (GlobalType, bool) {
	if m == nil {
		return GlobalType{}, false
	}
	entry, local := m.importEntry(ExternGlobal, idx)
	if entry >= 0 {
		return m.Imports[entry].Type.GlobalType(), true
	}
	if local >= uint64(len(m.Globals)) {
		return GlobalType{}, false
	}
	return m.Globals[int(local)].Type, true
}

// FuncTypeEqual compares function signatures.
func FuncTypeEqual(a, b *CompType) bool {
	if a == nil || b == nil || a.Kind != CompFunc || b.Kind != CompFunc || len(a.Params) != len(b.Params) || len(a.Results) != len(b.Results) {
		return false
	}
	for i := range a.Params {
		if !equalValType(a.Params[i], b.Params[i]) {
			return false
		}
	}
	for i := range a.Results {
		if !equalValType(a.Results[i], b.Results[i]) {
			return false
		}
	}
	return true
}

// CanonicalTypeID returns the stable signature id used by call_indirect checks.
func (m *Module) CanonicalTypeID(typeIdx uint32) uint32 {
	// Type zero is its own first matching signature, including the invalid-type
	// fallback. Avoid counting or indexing the section for this common query.
	if typeIdx == 0 && m != nil {
		return 0
	}
	if len(m.Types) <= 8 {
		count := 0
		for _, group := range m.Types {
			count += len(group.SubTypes)
		}
		if count <= 8 {
			target, ok := m.TypeFunc(typeIdx)
			if !ok {
				return typeIdx
			}
			for i := 0; i < count; i++ {
				if ft, ok := m.TypeFunc(uint32(i)); ok && FuncTypeEqual(ft, target) {
					return uint32(i)
				}
			}
			return typeIdx
		}
	}
	directory := m.typeIndex()
	if uint64(typeIdx) >= uint64(len(directory.flat)) || directory.flat[typeIdx].st.Comp.Kind != CompFunc {
		return typeIdx
	}
	directory.canonical.Do(func() {
		ids := make([]uint32, len(directory.flat))
		// A prefix of identical function signatures has canonical ID zero.
		// Prove it directly before allocating keys; duplicate-only sections need
		// no signature map, while mostly unique sections stop at the first miss.
		prefix := 0
		first := &directory.flat[0].st.Comp
		if first.Kind == CompFunc {
			prefix = 1
			for prefix < len(directory.flat) && FuncTypeEqual(first, &directory.flat[prefix].st.Comp) {
				prefix++
			}
		}
		if prefix == len(directory.flat) {
			directory.canonicalIDs = ids
			return
		}
		firstBySignature := make(map[string]uint32)
		if prefix != 0 {
			firstBySignature[canonicalFuncTypeKey(first)] = 0
		}
		for i := prefix; i < len(directory.flat); i++ {
			idx := uint32(i)
			ids[i] = idx
			ct := &directory.flat[i].st.Comp
			if ct.Kind != CompFunc {
				continue
			}
			key := canonicalFuncTypeKey(ct)
			if first, ok := firstBySignature[key]; ok {
				ids[i] = first
				continue
			}
			firstBySignature[key] = idx
		}
		directory.canonicalIDs = ids
	})
	return directory.canonicalIDs[typeIdx]
}

// canonicalFuncTypeKey encodes exactly the fields observed by equalValType.
// Keeping the complete normalized signature as a string makes map equality
// collision-safe while assigning all module IDs in one pass.
func canonicalFuncTypeKey(ft *CompType) string {
	var key strings.Builder
	key.Grow(16 + 16*(len(ft.Params)+len(ft.Results)))
	var count [8]byte
	binary.LittleEndian.PutUint64(count[:], uint64(len(ft.Params)))
	_, _ = key.Write(count[:])
	for _, t := range ft.Params {
		writeCanonicalValTypeKey(&key, t)
	}
	binary.LittleEndian.PutUint64(count[:], uint64(len(ft.Results)))
	_, _ = key.Write(count[:])
	for _, t := range ft.Results {
		writeCanonicalValTypeKey(&key, t)
	}
	return key.String()
}

func writeCanonicalValTypeKey(dst *strings.Builder, t ValType) {
	var key [16]byte
	key[0] = byte(t.Kind())
	key[1] = byte(t.Num())
	if t.Kind() == ValRef {
		rt := t.Ref()
		heap := rt.Heap()
		typeIdx := heap.Type()
		if rt.Nullable() {
			key[2] = 1
		}
		if rt.Exact() {
			key[3] = 1
		}
		key[4] = byte(heap.Kind())
		key[5] = byte(heap.Abs())
		if typeIdx.Rec {
			key[6] = 1
		}
		binary.LittleEndian.PutUint32(key[8:12], typeIdx.Index)
		if heap.Kind() == HeapDefType {
			_, member, _, valid := heap.Def()
			if valid {
				key[7] = 1
				binary.LittleEndian.PutUint32(key[12:16], member)
			}
		}
	}
	_, _ = dst.Write(key[:])
}

// StructuralTypeID returns a call_indirect signature id derived only from the
// structure of the function type at typeIdx, so the same signature yields the
// same id in every module. This is required for cross-instance call_indirect,
// where a table entry's home instance and the caller are different modules with
// unrelated type sections; a per-module canonical id would not match.
func (m *Module) StructuralTypeID(typeIdx uint32) uint32 {
	ft, ok := m.TypeFunc(typeIdx)
	if !ok {
		return typeIdx
	}
	if compTypeHasIndexedReferences(ft) || m.typeInNonSingletonRecGroup(typeIdx) {
		if id, ok := m.structuralIndexedFuncTypeID(typeIdx); ok {
			return id
		}
	}
	return StructuralFuncTypeID(ft)
}

// StructuralTypeKey returns the collision-resistant native call discriminator
// for a function type. Validated callers should use StructuralTypeKeyChecked so
// an over-complex indexed graph is rejected rather than assigned a fallback key.
func (m *Module) StructuralTypeKey(typeIdx uint32) uint64 {
	key, _ := m.StructuralTypeKeyChecked(typeIdx)
	return key
}

// StructuralTypeKeyChecked is stable across modules with equivalent indexed or
// recursive graphs. It bounds canonicalization work and shares completed group
// digests in the module-local type-key cache. The boolean is false for a
// non-function type, malformed graph, or a graph exceeding the work limit.
func (m *Module) StructuralTypeKeyChecked(typeIdx uint32) (uint64, bool) {
	ft, ok := m.TypeFunc(typeIdx)
	if !ok {
		return 0, false
	}
	if compTypeHasIndexedReferences(ft) || m.typeInNonSingletonRecGroup(typeIdx) {
		return m.structuralIndexedFuncTypeKey(typeIdx)
	}
	return StructuralFuncTypeKey(ft), true
}

// FunctionSubtypeTypeIndexes returns every function type declared by this
// module that is a subtype of targetType. Dynamic indexed-function ref.test uses
// these exact local identities after a funcref has passed through storage and
// no longer carries compile-time ref.func provenance.
func (m *Module) FunctionSubtypeTypeIndexes(targetType uint32) ([]uint32, bool) {
	if _, ok := m.TypeFunc(targetType); !ok {
		return nil, false
	}
	// Measured subtype selection crosses over between two and four types.
	// Keep tiny sections on the direct relation; there is no useful graph index
	// to build for one identity, and at most two candidates for the small scan.
	if len(m.Types) <= 2 {
		count := 0
		for _, group := range m.Types {
			count += len(group.SubTypes)
		}
		if count == 1 {
			return []uint32{targetType}, true
		}
		if count == 2 {
			validator := &moduleValidator{m: m}
			required := Ref(false, IndexedHeap(TypeIdx{Index: targetType}), false)
			indexes := make([]uint32, 0, 1)
			for i := uint32(0); i < 2; i++ {
				if _, ok := m.TypeFunc(i); !ok {
					continue
				}
				actual := Ref(false, IndexedHeap(TypeIdx{Index: i}), false)
				if validator.refSubtype(actual, required) {
					indexes = append(indexes, i)
				}
			}
			return indexes, len(indexes) != 0
		}
	}
	directory := m.typeIndex()
	flat := directory.flat
	validator := &moduleValidator{m: m}
	state := make(map[moduleTypePair]uint8)
	equivalent := make([]bool, len(flat))
	for typeIndex, ref := range flat {
		if ref.st.Comp.Kind == CompFunc {
			equivalent[typeIndex] = validator.typeIdxEquivalentWithState(
				TypeIdx{Index: uint32(typeIndex)}, TypeIdx{Index: targetType}, state,
			)
		}
	}

	// Reverse the declared-super edges once, then walk descendants from every
	// structurally equivalent target. Calling ReferenceTypeSubtype for each
	// candidate repeatedly walks the same super chains; a long subtype chain
	// otherwise costs quadratic time.
	directory.superEdges.Do(func() {
		maxInt := int(^uint(0) >> 1)
		if len(flat) > maxInt-1 || uint64(len(flat)) > uint64(^uint32(0)) {
			return
		}
		offsets := make([]int, len(flat)+1)
		edgeCount := 0
		for _, ref := range flat {
			for _, super := range ref.st.Supers {
				superIndex, ok := m.flatTypeIdxInRecGroup(super, ref.recGroup)
				if !ok {
					continue
				}
				if edgeCount == maxInt {
					return
				}
				offsets[superIndex+1]++
				edgeCount++
			}
		}
		for i := 1; i < len(offsets); i++ {
			offsets[i] += offsets[i-1]
		}
		children := make([]uint32, edgeCount)
		cursor := append([]int(nil), offsets[:len(flat)]...)
		for typeIndex, ref := range flat {
			for _, super := range ref.st.Supers {
				superIndex, ok := m.flatTypeIdxInRecGroup(super, ref.recGroup)
				if !ok {
					continue
				}
				position := cursor[superIndex]
				children[position] = uint32(typeIndex)
				cursor[superIndex]++
			}
		}
		directory.superOffsets = offsets
		directory.superChildren = children
		directory.superEdgesOK = true
	})
	if !directory.superEdgesOK {
		return nil, false
	}

	reachable := make([]bool, len(flat))
	queue := make([]uint32, 0, len(flat))
	for typeIndex, isEquivalent := range equivalent {
		if isEquivalent {
			reachable[typeIndex] = true
			queue = append(queue, uint32(typeIndex))
		}
	}
	for head := 0; head < len(queue); head++ {
		parent := int(queue[head])
		for edge := directory.superOffsets[parent]; edge < directory.superOffsets[parent+1]; edge++ {
			child := directory.superChildren[edge]
			if reachable[child] {
				continue
			}
			reachable[child] = true
			queue = append(queue, child)
		}
	}

	indexes := make([]uint32, 0, 1)
	for typeIndex, ref := range flat {
		if reachable[typeIndex] && ref.st.Comp.Kind == CompFunc {
			indexes = append(indexes, uint32(typeIndex))
		}
	}
	return indexes, len(indexes) != 0
}

func (m *Module) typeInNonSingletonRecGroup(typeIdx uint32) bool {
	_, group, ok := m.subtypeByTypeIdxWithRecGroup(TypeIdx{Index: typeIdx})
	return ok && len(m.Types[group].SubTypes) > 1
}

// StructuralFuncTypeID hashes a function type's encoded params/results (FNV-1a)
// into a signature id that is identical across modules for identical signatures.
func StructuralFuncTypeID(ft *CompType) uint32 {
	const offset32 = 2166136261
	const prime32 = 16777619
	h := uint32(offset32)
	mix := func(b byte) { h ^= uint32(b); h *= prime32 }
	mix(byte(len(ft.Params)))
	for _, t := range ft.Params {
		mix(structuralLegacyValTypeByte(t))
	}
	mix(0xfe) // params/results separator
	mix(byte(len(ft.Results)))
	for _, t := range ft.Results {
		mix(structuralLegacyValTypeByte(t))
	}
	return h
}

// StructuralFuncTypeKey hashes the complete non-indexed value shape with
// SHA-256 and uses 64 bits in native descriptors. The full structural metadata
// remains authoritative at public/storage boundaries; this wider independent
// key prevents collisions in the legacy 32-bit FNV discriminator from admitting
// a wrong native target.
func structuralLegacyValTypeByte(t ValType) byte {
	switch t.Kind() {
	case ValNum:
		return byte(t.Num())
	case ValVec:
		return 0x7b
	case ValRef:
		// Canonicalize shorthand and explicit nullable abstract references to
		// the same legacy byte. Wider identity is provided by StructuralTypeKey.
		rt := t.Ref()
		if rt.Nullable() && !rt.Exact() && rt.Heap().Kind() == HeapAbs {
			return byte(rt.Heap().Abs())
		}
	}
	return 0
}

func StructuralFuncTypeKey(ft *CompType) uint64 {
	encoded := make([]byte, 0, 16+8*(len(ft.Params)+len(ft.Results)))
	mixU32 := func(v uint32) {
		encoded = append(encoded, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
	}
	writeValue := func(t ValType) {
		encoded = append(encoded, byte(t.Kind()))
		switch t.Kind() {
		case ValNum:
			encoded = append(encoded, byte(t.Num()))
		case ValRef:
			flags := byte(0)
			rt := t.Ref()
			if rt.Nullable() {
				flags |= 1
			}
			if rt.Exact() {
				flags |= 2
			}
			encoded = append(encoded, flags, byte(rt.Heap().Kind()), byte(rt.Heap().Abs()))
		}
	}
	mixU32(uint32(len(ft.Params)))
	for _, t := range ft.Params {
		writeValue(t)
	}
	encoded = append(encoded, 0xfe)
	mixU32(uint32(len(ft.Results)))
	for _, t := range ft.Results {
		writeValue(t)
	}
	sum := sha256.Sum256(encoded)
	return binary.LittleEndian.Uint64(sum[:8])
}
