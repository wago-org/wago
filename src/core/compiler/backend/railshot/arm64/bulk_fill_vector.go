//go:build arm64

package arm64

var bulkFillVectorEnabled = true

// memoryFillVector retains the full pre-write bounds check. Full vectors and
// a scalar byte tail or overlapping final vector stay inside the requested span.
// Native vector support is independent of the guest SIMD feature selection.
func (f *fn) memoryFillVector(n int, memoryIndex uint32) {
	f.stats.peep("memfill-vector")
	f.materializePendingLoads()
	f.erase(f.s.back())
	v, owned := f.materializeRead(f.popValue())
	byteTail := n%16 == 1
	addedGPins := regMask(0)
	if byteTail {
		addedGPins = maskOf(v) &^ f.pinned
		f.pinned = f.pinned.add(v)
	}
	x := f.v128SplatScalar(v, 1)
	if owned && !byteTail {
		f.release(v)
	}
	addedPins := maskOf(x) &^ f.fpinned
	f.fpinned = f.fpinned.add(x)
	minimumAccess := f.minimumMemoryAccess(memoryIndex, 0, n)
	dst, dstOwned := f.materializeRead(f.popValue())
	if !f.memoryAddr64(memoryIndex) {
		f.a.MovReg32(dst, dst)
	}
	if minimumAccess {
		f.stats.peep("memory-min-bounds")
	} else {
		f.bulkBoundsCheck(dst, n, memoryIndex)
	}
	base, baseOwned := f.indexedMemoryBase(memoryIndex, maskOf(dst))
	for off := 0; off+16 < n; off += 16 {
		f.a.StrQIdx(base, dst, x, int32(off))
	}
	if byteTail {
		// The low byte is already the requested fill pattern. Preserve it
		// across address lowering and avoid a final overlapping vector store.
		f.a.StoreIdx(base, dst, v, int32(n-1), 1)
		f.stats.peep("memfill-byte-tail")
	} else {
		f.a.StrQIdx(base, dst, x, int32(n-16))
	}
	if baseOwned {
		f.release(base)
	}
	f.fpinned &^= addedPins
	f.releaseF(x)
	f.pinned &^= addedGPins
	if owned && byteTail {
		f.release(v)
	}
	if dstOwned {
		f.release(dst)
	}
}
