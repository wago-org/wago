//go:build amd64

package amd64

var bulkFillVectorEnabled = true

// memoryFillVector retains the full pre-write bounds check. The final vector
// overlaps earlier stores so every byte written lies inside the requested span.
// Native vector support is independent of the guest SIMD feature selection.
func (f *fn) memoryFillVector(n int, memoryIndex uint32) {
	f.stats.peep("memfill-vector")
	f.materializePendingLoads()
	f.erase(f.s.back())
	v, owned := f.materializeRead(f.popValue())
	x := f.allocFReg(0)
	f.a.MovGprToXmm(x, v, false)
	opVPunpcklbw.emit(f, x, x, x)
	opVPunpcklwd.emit(f, x, x, x)
	f.a.Pshufd(x, x, 0)
	if owned {
		f.release(v)
	}
	addedPins := maskOf(x) &^ f.fpinned
	f.fpinned = f.fpinned.add(x)
	minimumAccess := f.minimumMemoryAccess(memoryIndex, 0, n)
	dst, dstOwned := f.materializeRead(f.popValue())
	if !f.memoryAddr64(memoryIndex) {
		f.a.MovRegReg32(dst, dst)
	}
	if minimumAccess {
		f.stats.peep("memory-min-bounds")
	} else {
		f.bulkBoundsCheck(dst, n, memoryIndex)
	}
	base, baseOwned := f.bulkMemory0Base(maskOf(dst))
	for off := 0; off+16 < n; off += 16 {
		f.mov128StoreIdx(base, dst, x, int32(off))
	}
	off := n - 16
	f.mov128StoreIdx(base, dst, x, int32(off))
	if baseOwned {
		f.release(base)
	}
	f.fpinned &^= addedPins
	f.releaseF(x)
	if dstOwned {
		f.release(dst)
	}
}
