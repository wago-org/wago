//go:build arm64

package arm64

var bulkCopyVectorEnabled = true
var bulkMemoryLoopsEnabled = true

// Load the complete source before writing any destination bytes. Overlapping
// final vectors stay inside the checked span, including for unaligned lengths.
func (f *fn) emitBulkCopyVector(n int, srcBase, src, dstBase, dst Reg) bool {
	if n < 16 || !f.opt(optBulkCopyVector) {
		return false
	}
	count := (n + 15) / 16
	avoid := f.blockedFRegs(0)
	free := 0
	for _, r := range fpAllocRegs {
		if !avoid.has(r) {
			free++
		}
	}
	if free < count {
		return false
	}
	var regs [4]Reg
	var offsets [4]int32
	for i := 0; i < count; i++ {
		off := i * 16
		if i == count-1 {
			off = n - 16
		}
		offsets[i] = int32(off)
		regs[i] = f.allocFReg(avoid)
		avoid = avoid.add(regs[i])
		f.a.LdrQIdx(regs[i], srcBase, src, offsets[i])
	}
	for i := 0; i < count; i++ {
		f.a.StrQIdx(dstBase, dst, regs[i], offsets[i])
		f.releaseF(regs[i])
	}
	f.stats.peep("memcopy-vector")
	return true
}
