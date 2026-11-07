//go:build arm64

package arm64

import (
	"math/bits"
	"os"
)

var minPlusRunsEnabled = os.Getenv("WAGO_ARM64_NO_MINPLUS_RUNS") != "1"

// The existing emitter has already checked whole memory32 ranges and proved
// source/destination rows equal or disjoint. Count complete packets that cannot
// write any byte of the pivot. A pivot behind the destination is safe except
// when its final three bytes overlap the next write. The original scalar tail
// remains outside the count. This calculation does not read guest memory.
func (f *fn) minPlusRunCount(p minPlusPlan, bytes uint32, d, pivot, counter, count Reg) {
	f.a.MovImm64(count, uint64(p.limit-8))
	f.a.Sub32(count, count, counter)
	shift := uint8(bits.TrailingZeros32(bytes))
	f.a.LsrImm(count, count, shift, true)
	f.a.Sub64(X16, pivot, d)
	f.a.AddImm64(X17, X16, 3)
	f.a.LsrImm(X16, X16, shift, false)
	f.a.CmpReg64(count, X16)
	f.a.Csel64(count, count, X16, condBE)
	f.a.CmpImm64(X17, 3)
	f.a.Csel64(count, ZR, count, condB)
}

// Hoist pivot loads and overlap checks over a counted run. Stop before the
// pivot, use the established sequential packet to cross it, and reload its
// possibly changed value before the next run. No new loop grammar or memory
// eligibility is introduced, and the original scalar tail restores temporaries.
func (f *fn) emitMinPlusRuns(p minPlusPlan, d, s, pivot, counter, count, vd, vs, vp Reg, compare func(uint32)) {
	dispatch := f.a.Len()
	compare(p.limit - 24)
	done := f.a.Bcond(condA)
	// Plan one run in 16-byte packets, then split that count into groups of
	// four and a remainder. All stores stay before the same proven boundary.
	f.minPlusRunCount(p, 16, d, pivot, counter, count)
	scalar := f.a.Cbz64(count)
	f.a.Load32(X16, pivot, 0)
	f.a.NeonDupGprS(vp, X16)
	// Directly encoded packet loads/stores and immediate updates do not use
	// X17. Keep the at-most-three narrow packets there across the wide loop.
	f.a.AndImm32(X17, count, 3)
	f.a.LsrImm(count, count, 2, true)
	packet := func(bytes uint32) {
		empty := f.a.Cbz64(count)
		header := f.a.Len()
		for off := int32(0); off < int32(bytes); off += 16 {
			f.a.LdrQ(vd, d, off)
			f.a.LdrQ(vs, s, off)
			f.a.NeonAddS(vs, vs, vp)
			f.a.NeonSminS(vd, vd, vs)
			f.a.StrQ(d, off, vd)
		}
		f.a.AddImm64(d, d, bytes)
		f.a.AddImm64(s, s, bytes)
		f.a.AddImm32(counter, counter, bytes)
		f.a.SubsImm64(count, count, 1)
		f.patchBranch19(f.a.Bcond(condNE), header)
		f.patchBranch19(empty, f.a.Len())
	}
	packet(64)
	f.a.MovReg64(count, X17)
	packet(16)
	f.patchBranch26(f.a.Branch(), dispatch)
	f.patchBranch19(scalar, f.a.Len())
	// This source grammar reloads the pivot between its two stores. Preserve
	// that order when a write can change even part of the pivot's four bytes.
	for off := uint32(0); off < 8; off += 4 {
		f.a.Load32(X16, s, off)
		f.a.Load32(X17, pivot, 0)
		f.a.Add32(X16, X16, X17)
		f.a.Load32(X17, d, off)
		f.a.CmpReg32(X16, X17)
		f.a.Csel32(X16, X16, X17, condL)
		f.a.Store32(X16, d, off)
	}
	f.a.AddImm64(d, d, 8)
	f.a.AddImm64(s, s, 8)
	f.a.AddImm32(counter, counter, 8)
	f.patchBranch26(f.a.Branch(), dispatch)
	f.patchBranch19(done, f.a.Len())
	f.stats.peep("minplus-loop-runs")
	f.stats.peep("minplus-loop-unroll")
}
