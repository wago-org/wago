package wasm

// FunctionTypeLookup is optional read-only compiler-pass scratch. It must be
// used while the source module's type section is immutable; it is not a cache
// stored on Module and must not be retained after that compilation or pass.
// Small type sections and indexes above the fixed scratch budget use the
// ordinary lookup without allocating.
type FunctionTypeLookup struct {
	module *Module
	ends   []uint64
}

const maxFunctionTypeLookupBytes = 256 << 10

func NewFunctionTypeLookup(m *Module) FunctionTypeLookup {
	if m == nil || len(m.FuncTypes) <= 8 || len(m.Types) <= 64 || len(m.Types) > maxFunctionTypeLookupBytes/8 {
		return FunctionTypeLookup{}
	}
	ends := make([]uint64, len(m.Types))
	next := uint64(0)
	for i := range m.Types {
		next += uint64(len(m.Types[i].SubTypes))
		ends[i] = next
	}
	return FunctionTypeLookup{module: m, ends: ends}
}

// LocalFuncType has Module.LocalFuncType semantics and returns the same stored
// signature pointer. A zero lookup or a different module uses direct lookup.
func (lookup FunctionTypeLookup) LocalFuncType(m *Module, local int) (*CompType, bool) {
	if m == nil {
		return nil, false
	}
	if lookup.module != m || len(lookup.ends) == 0 {
		return m.LocalFuncType(local)
	}
	if local < 0 || local >= len(m.FuncTypes) {
		return nil, false
	}
	idx := m.FuncTypes[local]
	if idx.Rec {
		return nil, false
	}
	want := uint64(idx.Index)
	lo, hi := 0, len(lookup.ends)
	for lo < hi {
		mid := lo + (hi-lo)/2
		if lookup.ends[mid] <= want {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo == len(lookup.ends) {
		return nil, false
	}
	start := uint64(0)
	if lo > 0 {
		start = lookup.ends[lo-1]
	}
	comp := &m.Types[lo].SubTypes[int(want-start)].Comp
	if comp.Kind != CompFunc {
		return nil, false
	}
	return comp, true
}
