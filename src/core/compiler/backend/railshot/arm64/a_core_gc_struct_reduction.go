//go:build arm64

package arm64

// Reduced functions have no native loop or guest call. Publish their segment
// bound after ordinary call-graph resolution: callers of the original looping
// body must not inherit this separate reduced-entry contract.
func mergeGCReducedBounds(ordinary, reduced []uint64) []uint64 {
	if len(ordinary) == 0 {
		return reduced
	}
	for i, bits := range reduced {
		ordinary[i] |= bits
	}
	return ordinary
}
