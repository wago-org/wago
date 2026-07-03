package wasm

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

// GlobalValueType returns the canonical global value type.
func GlobalValueType(gt GlobalType) ValType { return gt.Type }

// TableRefType returns the canonical table element reference type.
func TableRefType(tt TableType) RefType { return tt.Ref }

func TableAddrType(tt TableType) ValType {
	if tt.Limits.Addr64 {
		return I64
	}
	return I32
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

// EncodeValType returns the canonical one-byte encoding for MVP numeric/vector
// value types and bare nullable reference aliases used by the current tests and
// wasm builders.
func EncodeValType(t ValType) (byte, bool) {
	switch {
	case equalValType(t, I32):
		return 0x7f, true
	case equalValType(t, I64):
		return 0x7e, true
	case equalValType(t, F32):
		return 0x7d, true
	case equalValType(t, F64):
		return 0x7c, true
	case equalValType(t, V128):
		return 0x7b, true
	case equalValType(t, FuncRef):
		return 0x70, true
	case equalValType(t, ExternRef):
		return 0x6f, true
	default:
		return 0, false
	}
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

// FuncSignature returns the function signature for a global function index.
func (m *Module) FuncSignature(idx uint32) (*CompType, bool) {
	i := uint32(0)
	for j := range m.Imports {
		if m.Imports[j].Type.Kind != ExternFunc {
			continue
		}
		if i == idx {
			return m.typeFunc(m.Imports[j].Type.Type)
		}
		i++
	}
	local := int(idx - i)
	if idx < i || local < 0 || local >= len(m.FuncTypes) {
		return nil, false
	}
	return m.typeFunc(m.FuncTypes[local])
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
	if localIdx < 0 || localIdx >= len(m.FuncTypes) {
		return nil, false
	}
	return m.resolvedTypeFunc(m.FuncTypes[localIdx])
}

func (m *Module) subtypeByTypeIdx(idx TypeIdx) (*SubType, bool) {
	st, _, ok := m.subtypeByTypeIdxWithRecGroup(idx)
	return st, ok
}

func (m *Module) subtypeByTypeIdxWithRecGroup(idx TypeIdx) (*SubType, int, bool) {
	if idx.Rec {
		return nil, 0, false
	}
	want := uint64(idx.Index)
	for gi := range m.Types {
		rt := &m.Types[gi]
		if want < uint64(len(rt.SubTypes)) {
			return &rt.SubTypes[int(want)], gi, true
		}
		want -= uint64(len(rt.SubTypes))
	}
	return nil, 0, false
}

func (m *Module) flattenedTypeCount() int {
	n := 0
	for i := range m.Types {
		n += len(m.Types[i].SubTypes)
	}
	return n
}

func (m *Module) typeFunc(idx TypeIdx) (*CompType, bool) {
	st, ok := m.subtypeByTypeIdx(idx)
	if !ok || st.Comp.Kind != CompFunc {
		return nil, false
	}
	return &st.Comp, true
}

func (m *Module) resolvedTypeFunc(idx TypeIdx) (*CompType, bool) {
	st, recGroup, ok := m.subtypeByTypeIdxWithRecGroup(idx)
	if !ok || st.Comp.Kind != CompFunc {
		return nil, false
	}
	ct := m.resolveCompTypeRecIndexes(st.Comp, recGroup)
	return &ct, true
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

func (m *Module) flatTypeIdxInRecGroup(idx TypeIdx, recGroup int) (int, bool) {
	if !idx.Rec {
		if _, ok := m.subtypeByTypeIdx(idx); !ok {
			return 0, false
		}
		return int(idx.Index), true
	}
	if recGroup < 0 || recGroup >= len(m.Types) || idx.Index >= uint32(len(m.Types[recGroup].SubTypes)) {
		return 0, false
	}
	abs := 0
	for gi := 0; gi < recGroup; gi++ {
		abs += len(m.Types[gi].SubTypes)
	}
	return abs + int(idx.Index), true
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
	if ft.Storage.Packed {
		return ft
	}
	ft.Storage.Val = m.resolveValTypeRecIndexes(ft.Storage.Val, recGroup)
	return ft
}

func (m *Module) resolveValTypeRecIndexes(t ValType, recGroup int) ValType {
	if t.Kind != ValRef {
		return t
	}
	t.Ref = m.resolveRefTypeRecIndexes(t.Ref, recGroup)
	return t
}

func (m *Module) resolveRefTypeRecIndexes(rt RefType, recGroup int) RefType {
	if rt.Heap.Kind != HeapTypeIndex {
		return rt
	}
	rt.Heap.Type = m.resolveTypeIdxRecIndex(rt.Heap.Type, recGroup)
	return rt
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
	i := uint32(0)
	for j := range m.Imports {
		if m.Imports[j].Type.Kind != ExternGlobal {
			continue
		}
		if i == idx {
			return m.Imports[j].Type.Global, true
		}
		i++
	}
	local := int(idx - i)
	if idx < i || local < 0 || local >= len(m.Globals) {
		return GlobalType{}, false
	}
	return m.Globals[local].Type, true
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
	target, ok := m.TypeFunc(typeIdx)
	if !ok {
		return typeIdx
	}
	for j := 0; j < m.flattenedTypeCount(); j++ {
		ft, ok := m.TypeFunc(uint32(j))
		if ok && FuncTypeEqual(ft, target) {
			return uint32(j)
		}
	}
	return typeIdx
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
	return StructuralFuncTypeID(ft)
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
		c, _ := EncodeValType(t)
		mix(c)
	}
	mix(0xfe) // params/results separator
	mix(byte(len(ft.Results)))
	for _, t := range ft.Results {
		c, _ := EncodeValType(t)
		mix(c)
	}
	return h
}
