//go:build amd64 && !wago_gc_compact_range_sse2

package amd64

const gcCompactRangeSSE2 = false

func (*fn) emitCompactRangeSSE2(directLoopLease, Reg, Reg, Reg, int, uint32) int { return -1 }
