//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// The wide kernel has fixed per-site cost. Bound expansion in functions with
// many copies; additional sites retain the existing Go helper.
const maxDynamicGCArrayCopySites = 16

// emitDynamicGCArrayCopy validates both complete ranges before writing. The
// direction-specific kernels preserve overlapping numeric payloads without a
// temporary buffer or a change to the processor's direction flag.
func (f *fn) emitDynamicGCArrayCopy(dstType, srcType uint32) bool {
	if f.gcDynamicCopySites >= maxDynamicGCArrayCopySites {
		return false
	}
	dstScalar, dstFinal, dstOK := f.directGCArrayLayout(dstType)
	srcScalar, srcFinal, srcOK := f.directGCArrayLayout(srcType)
	if !gcSharedStubsEnabled || !dstOK || !srcOK || !dstFinal || !srcFinal || dstScalar != srcScalar {
		return false
	}
	f.gcDynamicCopySites++
	// array.copy's hintHasCall excludes RSI/RDI loop constants and interval
	// leases. Preserve that planning invariant: this kernel uses fixed scratch
	// registers, including RSI, and leaves R9-R11 untouched.
	f.flush()
	oldFloor := f.spillFloor
	savedDst := f.allocSpillSlots(1)
	f.spillFloor = savedDst + 1
	count := f.popValue()
	srcIndex := f.popValue()
	srcValue := f.popValue()
	dstIndex := f.popValue()
	dstValue := f.popValue()
	countSlot, srcSlot, dstSlot := count.st.slotIndex(), srcIndex.st.slotIndex(), dstIndex.st.slotIndex()

	// Preserve destination/source resolution and trap order. The bounded
	// resolver checks that the header fits in its handle's physical extent.
	dst := f.emitSharedCheckedGCObjectPinned(dstValue, dstType, gc.PayloadOffset, gcSharedStubResolveBoundedObject)
	f.stats.addGCHandleResolution()
	f.a.Store64(RSP, f.spillOff(savedDst), dst)
	f.pinned = f.pinned.remove(dst)
	src := f.emitSharedCheckedGCObjectPinned(srcValue, srcType, gc.PayloadOffset, gcSharedStubResolveBoundedObject)
	f.stats.addGCHandleResolution()
	// Both resolvers return RAX. Spill vector storage before any conditional
	// exit, and keep the existing fixed-scratch contract (R9 remains untouched).
	var vec [4]Reg
	used := regMask(0)
	for i := range vec {
		vec[i] = f.allocFReg(used)
		used = used.add(vec[i])
	}
	f.a.Load64(R8, RSP, f.spillOff(savedDst))
	f.a.Load32(RCX, RSP, f.spillOff(countSlot))
	f.a.Load32(RDI, RSP, f.spillOff(dstSlot))
	f.a.Load32(RSI, RSP, f.spillOff(srcSlot))
	for _, pair := range [2][2]Reg{{R8, RDI}, {src, RSI}} {
		// The sum of two zero-extended i32 values cannot overflow 64 bits.
		f.a.Add64(pair[1], RCX)
		f.a.Load32(RDX, pair[0], 8)
		f.a.Cmp64(pair[1], RDX)
		f.trapIf(condA, trapBuiltin)
	}
	f.a.TestSelf(RCX, false)
	zero := f.a.JccPlaceholder(condE)
	for _, pair := range [2][2]Reg{{R8, RDI}, {src, RSI}} {
		f.a.ImulRI(pair[1], int32(dstScalar.size), true)
		f.a.AluRI(0, pair[1], int32(gc.PayloadOffset), true)
		f.a.Load32(RDX, pair[0], 4)
		f.a.Cmp64(pair[1], RDX)
		f.trapIf(condA, trapCastFailure)
		f.a.Add64(pair[1], pair[0])
	}
	f.a.ImulRI(RCX, int32(dstScalar.size), true)
	f.pinned = f.pinned.remove(src)
	// Pointers denote exclusive ends of equally long ranges. Comparing ends
	// selects the same safe direction as comparing starts, including disjoint
	// arrays. Equal pointers still undergo all preceding bounds checks.
	f.a.Cmp64(RDI, RSI)
	same := f.a.JccPlaceholder(condE)
	forward := f.a.JccPlaceholder(condB)
	f.emitGCArrayCopyLoop(vec, true)
	done := f.a.JmpPlaceholder()
	f.a.PatchRel32(forward, f.a.Len())
	f.a.AluRR(0x29, RDI, RCX, true)
	f.a.AluRR(0x29, RSI, RCX, true)
	f.emitGCArrayCopyLoop(vec, false)
	f.a.PatchRel32(done, f.a.Len())
	f.a.PatchRel32(same, f.a.Len())
	f.a.PatchRel32(zero, f.a.Len())
	for _, reg := range vec {
		f.releaseF(reg)
	}
	f.pinned = f.pinned.remove(src)
	f.releaseDroppedValue(count)
	f.releaseDroppedValue(srcIndex)
	f.releaseDroppedValue(dstIndex)
	f.spillFloor = oldFloor
	f.stats.peep("gc-array-native-copy-dynamic")
	return true
}

// RCX holds the remaining byte count, RSI/RDI source/destination pointers.
// Load a whole wide block before its stores. This preserves overlap and avoids
// the short-distance store/load dependencies of interleaved vector copies.
func (f *fn) emitGCArrayCopyLoop(vec [4]Reg, backward bool) {
	f.a.AluRI(cmpDigit, RCX, 1024, true)
	medium := f.a.JccPlaceholder(condB)
	// Align destination stores to vector width without assuming object or
	// element alignment. At least 1024 validated bytes remain here, so
	// peeling at most 31 bytes leaves room for a complete vector block.
	f.a.MovReg64(RAX, RDI)
	if !backward {
		f.a.Neg(RAX, true)
	}
	alignment := int32(16)
	if f.cpuHas(shared.AMD64AVX) {
		alignment = 32
	}
	f.a.AluRI(4, RAX, alignment-1, true)
	aligned := f.a.JccPlaceholder(condE)
	peel := f.a.Len()
	peelDisp, peelStep := int32(0), int32(1)
	if backward {
		peelDisp, peelStep = -1, -1
	}
	f.a.LoadIdx(RDX, RSI, RSP, peelDisp, 1, false, false)
	f.a.StoreIdx(RDI, RSP, RDX, peelDisp, 1)
	f.a.AluRI(0, RSI, peelStep, true)
	f.a.AluRI(0, RDI, peelStep, true)
	f.a.AluRI(5, RCX, 1, true)
	f.a.AluRI(5, RAX, 1, true)
	f.a.PatchRel32(f.a.JccPlaceholder(condNE), peel)
	f.a.PatchRel32(aligned, f.a.Len())
	if f.cpuHas(shared.AMD64AVX) {
		wide := f.a.Len()
		for i, reg := range vec {
			disp := int32(i) * 32
			if backward {
				disp = -int32(i+1) * 32
			}
			f.a.YMovdquLoadIdx(reg, RSI, RSP, disp)
		}
		for i, reg := range vec {
			disp := int32(i) * 32
			if backward {
				disp = -int32(i+1) * 32
			}
			f.a.YMovdquStoreIdx(RDI, RSP, reg, disp)
		}
		step := int32(128)
		if backward {
			step = -128
		}
		f.a.AluRI(0, RSI, step, true)
		f.a.AluRI(0, RDI, step, true)
		f.a.AluRI(5, RCX, 128, true)
		f.a.AluRI(cmpDigit, RCX, 128, true)
		f.a.PatchRel32(f.a.JccPlaceholder(condAE), wide)
		f.a.VZeroUpper()
	}
	f.a.PatchRel32(medium, f.a.Len())
	f.a.AluRI(cmpDigit, RCX, 64, true)
	short := f.a.JccPlaceholder(condB)
	wide := f.a.Len()
	for i, reg := range vec {
		disp := int32(i) * 16
		if backward {
			disp = -int32(i+1) * 16
		}
		f.mov128LoadIdx(reg, RSI, RSP, disp)
	}
	for i, reg := range vec {
		disp := int32(i) * 16
		if backward {
			disp = -int32(i+1) * 16
		}
		f.mov128StoreIdx(RDI, RSP, reg, disp)
	}
	wideStep := int32(64)
	if backward {
		wideStep = -64
	}
	f.a.AluRI(0, RSI, wideStep, true)
	f.a.AluRI(0, RDI, wideStep, true)
	f.a.AluRI(5, RCX, 64, true)
	f.a.AluRI(cmpDigit, RCX, 64, true)
	f.a.PatchRel32(f.a.JccPlaceholder(condAE), wide)
	f.a.PatchRel32(short, f.a.Len())
	f.a.AluRI(cmpDigit, RCX, 16, true)
	tail := f.a.JccPlaceholder(condB)
	vector := f.a.Len()
	disp := int32(0)
	step := int32(16)
	if backward {
		disp, step = -16, -16
	}
	f.mov128LoadIdx(vec[0], RSI, RSP, disp)
	f.mov128StoreIdx(RDI, RSP, vec[0], disp)
	f.a.AluRI(0, RSI, step, true)
	f.a.AluRI(0, RDI, step, true)
	f.a.AluRI(5, RCX, 16, true)
	f.a.AluRI(cmpDigit, RCX, 16, true)
	f.a.PatchRel32(f.a.JccPlaceholder(condAE), vector)
	f.a.PatchRel32(tail, f.a.Len())
	// Peeling can leave an arbitrary byte count. Decompose the <16-byte tail
	// into raw 8/4/2/1-byte moves, independent of the array's element width.
	for _, size := range []int{8, 4, 2, 1} {
		f.a.AluRI(cmpDigit, RCX, int32(size), true)
		skip := f.a.JccPlaceholder(condB)
		disp, step = 0, int32(size)
		if backward {
			disp, step = -int32(size), -int32(size)
		}
		f.a.LoadIdx(RDX, RSI, RSP, disp, size, false, size == 8)
		f.a.StoreIdx(RDI, RSP, RDX, disp, size)
		f.a.AluRI(0, RSI, step, true)
		f.a.AluRI(0, RDI, step, true)
		f.a.AluRI(5, RCX, int32(size), true)
		f.a.PatchRel32(skip, f.a.Len())
	}
}
