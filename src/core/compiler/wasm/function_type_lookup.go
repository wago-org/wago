package wasm

// FunctionTypeLookup is optional read-only compiler-pass scratch. It must be
// used while the source module's type section is immutable; it is not a cache
// stored on Module and must not be retained after that compilation or pass.
// Small type sections and indexes above the fixed scratch budget use the
// ordinary lookup without allocating.
type FunctionTypeLookup struct {
	module *Module
	ends   []uint64 // Cumulative type counts at the end of each block.
}

const (
	functionTypeLookupGroupStride = 16
	maxFunctionTypeLookupBytes    = 256 << 10
)

func NewFunctionTypeLookup(m *Module) FunctionTypeLookup {
	if m == nil || len(m.FuncTypes) <= 8 || len(m.Types) <= 64 {
		return FunctionTypeLookup{}
	}
	blocks := (len(m.Types)-1)/functionTypeLookupGroupStride + 1
	if blocks > maxFunctionTypeLookupBytes/8 {
		return FunctionTypeLookup{}
	}
	ends := make([]uint64, blocks)
	next := uint64(0)
	for i := range m.Types {
		next += uint64(len(m.Types[i].SubTypes))
		if i%functionTypeLookupGroupStride == functionTypeLookupGroupStride-1 {
			ends[i/functionTypeLookupGroupStride] = next
		}
	}
	ends[blocks-1] = next
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
	if lo > 0 {
		want -= lookup.ends[lo-1]
	}
	stop := (lo + 1) * functionTypeLookupGroupStride
	if stop > len(m.Types) {
		stop = len(m.Types)
	}
	for group := lo * functionTypeLookupGroupStride; group < stop; group++ {
		count := uint64(len(m.Types[group].SubTypes))
		if want < count {
			comp := &m.Types[group].SubTypes[int(want)].Comp
			if comp.Kind != CompFunc {
				return nil, false
			}
			return comp, true
		}
		want -= count
	}
	return nil, false
}
