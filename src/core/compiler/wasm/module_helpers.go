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

// GlobalValueType returns the canonical value type from legacy Val and current
// Type fields. Decoders/tests may populate either form; callers should not
// duplicate that compatibility decision.
func GlobalValueType(gt GlobalType) ValType {
	if gt.Type != (ValType{}) {
		return gt.Type
	}
	return gt.Val
}

// TableRefType returns the canonical table element reference type from legacy
// Elem and current Ref fields.
func TableRefType(tt TableType) RefType {
	if tt.Ref != (RefType{}) {
		return tt.Ref
	}
	if tt.Elem.Kind == ValRef {
		return tt.Elem.Ref
	}
	return FuncRef.Ref
}

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

// LocalFuncType returns the function signature for a local (non-imported)
// function index.
func (m *Module) LocalFuncType(localIdx int) (*CompType, bool) {
	if localIdx < 0 || localIdx >= len(m.FuncTypes) {
		return nil, false
	}
	return m.typeFunc(m.FuncTypes[localIdx])
}

func (m *Module) subtypeByTypeIdx(idx TypeIdx) (*SubType, bool) {
	if idx.Rec {
		return nil, false
	}
	want := int(idx.Index)
	for gi := range m.Types {
		rt := &m.Types[gi]
		if want < len(rt.SubTypes) {
			return &rt.SubTypes[want], true
		}
		want -= len(rt.SubTypes)
	}
	return nil, false
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

// TypeFunc returns the function type at a flattened module type index.
func (m *Module) TypeFunc(typeIdx uint32) (*CompType, bool) {
	return m.typeFunc(TypeIdx{Index: typeIdx})
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
