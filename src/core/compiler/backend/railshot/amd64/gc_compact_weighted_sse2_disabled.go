//go:build amd64 && !(wago_gc_compact_weighted_sse2 || wago_gc_direct_weighted_sse2)

package amd64

const gcCompactWeightedSSE2 = false

func (*fn) emitCompactWeightedSSE2(directLoopLease, Reg, Reg, Reg, Reg, int32) int { return -1 }
