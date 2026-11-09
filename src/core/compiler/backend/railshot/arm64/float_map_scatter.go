//go:build arm64

package arm64

// Complete bounds guards have already excluded memory32 wrap. Equal-stride
// streams are disjoint when their phase separation leaves an entire scalar word
// between lanes. Equal starts are ordinary independent in-place maps. A zero
// phase at a different start remains scalar unless the whole spans are disjoint.
// The memory-size lease is dead here and supplies the quotient scratch.
func (f *fn) floatMapScatterAlias(d, s Reg, dOff, sOff uint32, span, scratch Reg, stride, width uint32, guard func(Cond)) {
	f.floatMapOffset(X16, d, uint64(dOff), true, X16)
	f.floatMapOffset(X17, s, uint64(sOff), true, X17)
	f.cmpRR(X16, X17, true)
	equal := f.a.Bcond(condE)
	above := f.a.Bcond(condA)
	f.a.Sub64(X16, X17, X16)
	joined := f.a.Branch()
	f.patchBranch19(above, f.a.Len())
	f.a.Sub64(X16, X16, X17)
	f.patchBranch26(joined, f.a.Len())
	f.a.MovImm64(X17, uint64(stride))
	f.a.Mul64(scratch, span, X17)
	f.cmpRR(X16, scratch, true)
	separate := f.a.Bcond(condAE)
	laneStride := stride * width
	f.a.MovImm64(X17, uint64(laneStride))
	f.a.Udiv32(scratch, X16, X17)
	f.a.Msub32(X16, scratch, X17, X16)
	f.cmpImm(X16, width, false)
	guard(condB)
	f.a.MovImm64(X17, uint64(laneStride-width))
	f.cmpRR(X16, X17, false)
	guard(condA)
	f.patchBranch19(equal, f.a.Len())
	f.patchBranch19(separate, f.a.Len())
}
