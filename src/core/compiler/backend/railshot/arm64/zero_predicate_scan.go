//go:build arm64

package arm64

import (
	"os"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var zeroPredicateScanEnabled = os.Getenv("WAGO_ARM64_NO_ZERO_PREDICATE_SCAN") != "1"

type zeroPredicateScan struct {
	pointer, counter int
	offset           uint64
}

// Recognize a descending word predicate scan. The only unconditional effects
// are pointer-=4 and counter-=1. All other work belongs to the block skipped
// when the loaded word is zero. That work may mark future words, so a nonzero
// packet must return to the original scalar sequence.
func (f *fn) inspectZeroPredicateScan(source *wasm.Reader) (p zeroPredicateScan, ok bool) {
	r := *source
	valid := true
	op := func(want byte) {
		got, err := r.Byte()
		valid = valid && err == nil && got == want
	}
	local := func(code byte) int {
		op(code)
		x, err := r.U32()
		valid = valid && err == nil && x < uint32(f.nLocals)
		return int(x)
	}
	constant := func(want int32) {
		op(0x41)
		x, err := r.I32()
		valid = valid && err == nil && x == want
	}
	op(0x02)
	op(0x40)
	p.pointer = local(0x20)
	op(0x28)
	memory, offset, err := f.readMemArg(&r)
	valid = valid && err == nil && memory == 0 && offset <= uint64(^uint32(0))
	p.offset = offset
	op(0x45)
	op(0x0d)
	depth, err := r.U32()
	if !valid || err != nil || depth != 0 || f.nLocals > 64 {
		return p, false
	}
	var writes uint64
	var imm wasm.InstructionImmediate
	ended := false
	for instructions := 0; instructions < 192 && r.HasNext(); instructions++ {
		code, err := r.Byte()
		if err != nil {
			return p, false
		}
		if code == 0x0b {
			ended = true
			break
		}
		// No nested control, calls, memory growth or other observer can bypass
		// the predicate. This bounded straight-line arm keeps its exact code.
		if !(code == 0x01 || code == 0x1a || code == 0x1b || code >= 0x20 && code <= 0x22 ||
			code >= 0x28 && code <= 0x3e || code == 0x41 || code == 0x42 ||
			code >= 0x45 && code <= 0x5a || code >= 0x67 && code <= 0x8a) {
			return p, false
		}
		if f.classifier.ClassifyInto(&r, code, &imm) != nil {
			return p, false
		}
		if code == 0x21 || code == 0x22 {
			if imm.Index >= uint32(f.nLocals) {
				return p, false
			}
			writes |= uint64(1) << imm.Index
		}
	}
	if !ended {
		return p, false
	}
	valid = (local(0x20) == p.pointer) && valid
	constant(-4)
	op(0x6a)
	valid = (local(0x21) == p.pointer) && valid
	p.counter = local(0x20)
	constant(-1)
	op(0x6a)
	valid = (local(0x22) == p.counter) && valid
	op(0x0d)
	depth, err = r.U32()
	valid = valid && err == nil && depth == 0
	op(0x0b)
	if !valid || p.pointer == p.counter || p.pointer < f.nParams || p.counter < f.nParams ||
		f.localType[p.pointer] != mtI32 || f.localType[p.counter] != mtI32 ||
		writes&(uint64(1)<<uint(p.pointer)|uint64(1)<<uint(p.counter)) != 0 {
		return p, false
	}
	return p, true
}

// Called after the loop header is recorded, so every backedge checks the next
// packet. Leave at least one scalar iteration for the original exit and final
// locals. Failed guards do not trap, read memory or change locals.
func (f *fn) emitZeroPredicateScan(source *wasm.Reader) {
	if !f.opt(optZeroPredicateScan) || f.usesCalls || f.preserveCallerPins || f.interruptible || f.moduleEH ||
		f.gcFrameRoots != nil || f.localBase != 0 || f.inlineDepth != 0 ||
		len(f.customInstructions) != 0 || len(f.intervalLast) != 0 ||
		f.depth() != 0 || f.unreachable || f.memoryAddr64(0) || f.threadedMemory0 {
		return
	}
	p, ok := f.inspectZeroPredicateScan(source)
	if !ok {
		return
	}
	pointer, fp, pp := f.pinReg(p.pointer)
	counter, fc, pc := f.pinReg(p.counter)
	if !pp || !pc || fp || fc || !f.canonicalI32Local(p.pointer) || !f.canonicalI32Local(p.counter) {
		return
	}
	f.recoverLocal(p.pointer)
	f.recoverLocal(p.counter)
	oldPinned := f.pinned
	a := f.allocRegOrNone(0)
	if a == regNone {
		return
	}
	f.pinned = f.pinned.add(a)
	b := f.allocRegOrNone(0)
	defer func() {
		f.pinned = oldPinned
		f.release(a)
		if b != regNone {
			f.release(b)
		}
	}()
	if b == regNone {
		return
	}
	f.pinned = f.pinned.add(b)
	// The upper bound stays valid while the pointer descends without wrapping.
	f.a.CmpImm32(counter, 5)
	count := f.a.Bcond(condB)
	f.a.CmpImm32(pointer, 12)
	underflow := f.a.Bcond(condB)
	f.a.MovReg32(a, pointer)
	f.a.MovImm64(b, p.offset+4)
	f.a.Add64(b, a, b)
	if f.memSizeReg != regNone {
		f.a.CmpReg64(b, f.memSizeReg)
	} else {
		f.ld64(X16, linMemReg, -int32(bdCurBytes))
		f.a.CmpReg64(b, X16)
	}
	end := f.a.Bcond(condA)
	f.a.SubImm64(a, b, 16)
	f.a.Add64(a, linMemReg, a)
	f.a.MovReg64(X17, a)
	header := f.a.Len()
	f.ld64(b, X17, 0)
	f.ld64(a, X17, 8)
	f.a.Orr64(a, a, b)
	active := f.a.Cbnz64(a)
	// Test the old pointer before updating it: even a valid packet at
	// addresses 0..12 must fall back after the update wraps the i32 pointer.
	f.a.CmpImm32(pointer, 28)
	f.a.SubImm32(pointer, pointer, 16)
	f.a.SubImm32(counter, counter, 4)
	last := f.a.Bcond(condB)
	f.a.CmpImm32(counter, 5)
	tail := f.a.Bcond(condB)
	f.a.SubImm64(X17, X17, 16)
	f.patchBranch26(f.a.Branch(), header)
	for _, branch := range []int{count, underflow, end, active, last, tail} {
		f.patchBranch19(branch, f.a.Len())
	}
	for _, x := range []int{p.pointer, p.counter} {
		f.markLocalDirty(x)
		f.invalidateLocalWriteFacts(x)
		f.setFactsForLocal(x, 0)
	}
	f.invalidateGlobalsCache()
	f.invalidateBoundsCert()
	f.invalidateStoreForward()
	f.stats.peep("zero-predicate-scan")
}
