//go:build arm64

package arm64

// Bound proof work as well as copied code. Pending exits and relocations can
// include an arbitrarily large prefix from earlier loops in the same function.
// Exhaustion retains ordinary lowering without modifying code or metadata.
const scalarLoopMetadataLimit = 256

type scalarLoopMetadataBudget struct {
	remaining int
	stats     *CodegenStats
}

func (f *fn) newScalarLoopMetadataBudget() scalarLoopMetadataBudget {
	return scalarLoopMetadataBudget{remaining: scalarLoopMetadataLimit, stats: f.stats}
}

func (b *scalarLoopMetadataBudget) take(n int) bool {
	if n > b.remaining {
		b.stats.peep("scalar-loop-metadata-limit")
		return false
	}
	b.remaining -= n
	return true
}
