//go:build amd64 && wago_profile

package amd64

// These are static allocator decisions, not runtime spills or cycle counts.
// "available" means a cache reservation is the only reason its register cannot
// serve this request. It does not prove that revoking the cache would be safe
// across already-emitted loop backedges or profitable over the whole function.
func (f *fn) recordProfileFPressure(avoid regMask, outcome string) {
	if f.stats == nil || !f.stats.RecordSources {
		return
	}
	f.stats.peep(outcome)
	block := avoid.union(f.fpinned).union(f.fpinnedLocalMask)
	available := func(r Reg) bool { return !block.has(r) && f.fregUser[r] == nil }
	scalar, vector := false, false
	for _, c := range f.fconsts {
		scalar = scalar || available(c.reg)
	}
	for _, c := range f.vconsts {
		vector = vector || available(c.reg)
	}
	if scalar {
		f.stats.peep("fp-pressure-scalar-constant-available")
	}
	if vector {
		f.stats.peep("fp-pressure-vector-constant-available")
	}
	if scalar || vector {
		f.stats.peep("fp-pressure-constant-available")
	}
}
