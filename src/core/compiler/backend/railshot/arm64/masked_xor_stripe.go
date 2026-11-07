//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
)

var maskedXorStripeEnabled = os.Getenv("WAGO_ARM64_NO_MASKED_XOR_STRIPE") != "1"

type maskedXorStripePlan struct {
	pointer, index, temporary int
	bit, base, limit          uint32
}

// Match two adjacent conditional word XOR updates. The source index clears
// one stripe bit; all local roles and constants are inferred from the body.
// The private reader never consumes the original scalar fallback or tail.
func (f *fn) inspectMaskedXorStripe(source *wasm.Reader) (p maskedXorStripePlan, ok bool) {
	r := *source
	valid := true
	op := func(want byte) { got, err := r.Byte(); valid = valid && err == nil && got == want }
	local := func(code byte) int {
		op(code)
		x, err := r.U32()
		valid = valid && err == nil && x < uint32(f.nLocals) && f.localType[x] == mtI32
		return int(x)
	}
	same := func(code byte, want int) { valid = (local(code) == want) && valid }
	c := func() uint32 { op(0x41); x, err := r.I32(); valid = valid && err == nil; return uint32(x) }
	constant := func(want uint32) { valid = (c() == want) && valid }
	memory := func(code byte) {
		op(code)
		m, o, err := f.readMemArg(&r)
		valid = valid && err == nil && m == 0 && o == 0
	}
	branch := func() { op(0x0d); x, err := r.U32(); valid = valid && err == nil && x == 0 }
	op(0x02)
	op(0x40)
	p.index = local(0x20)
	p.bit = c()
	op(0x71)
	p.temporary = local(0x22)
	op(0x45)
	branch()
	p.pointer = local(0x20)
	same(0x20, p.pointer)
	memory(0x28)
	same(0x20, p.index)
	constant(0x3fffffff &^ (p.bit | 1))
	op(0x71)
	constant(2)
	op(0x74)
	p.base = c()
	op(0x6a)
	memory(0x28)
	op(0x73)
	memory(0x36)
	op(0x0b)
	op(0x02)
	op(0x40)
	same(0x20, p.temporary)
	op(0x45)
	branch()
	same(0x20, p.pointer)
	constant(4)
	op(0x6a)
	same(0x22, p.temporary)
	same(0x20, p.temporary)
	memory(0x28)
	same(0x20, p.index)
	constant(1)
	op(0x6a)
	constant(0x3fffffff &^ p.bit)
	op(0x71)
	constant(2)
	op(0x74)
	constant(p.base)
	op(0x6a)
	memory(0x28)
	op(0x73)
	memory(0x36)
	op(0x0b)
	same(0x20, p.pointer)
	constant(8)
	op(0x6a)
	same(0x21, p.pointer)
	same(0x20, p.index)
	constant(2)
	op(0x6a)
	same(0x22, p.index)
	p.limit = c()
	op(0x47)
	branch()
	op(0x0b)
	if !valid || p.pointer == p.index || p.pointer == p.temporary || p.index == p.temporary || p.bit < 4 || p.bit > 1<<29 || p.bit&(p.bit-1) != 0 || p.limit < 6 || p.limit > 1<<30 || p.limit&1 != 0 || uint64(p.base)+uint64(p.limit)*4 > 1<<32 {
		return p, false
	}
	return p, true
}

// A nonwrapping full-span guard proves the relationship between the pointer
// and index. Four aligned indices share the stripe predicate. Active source
// and destination packets are disjoint (the bit is at least four), so loading
// both packets before the store preserves the original scalar dependencies.
// Leave two or four words to the unchanged scalar loop for its final temporary,
// final pointer wrap, and original exit. Failed guards change no source local.
func (f *fn) emitMaskedXorStripe(source *wasm.Reader) {
	if !f.opt(optMaskedXorStripe) || f.usesCalls || f.preserveCallerPins || f.interruptible || f.moduleEH || f.gcFrameRoots != nil || f.localBase != 0 || f.inlineDepth != 0 || len(f.customInstructions) != 0 || len(f.intervalLast) != 0 || f.depth() != 0 || f.unreachable || f.memoryAddr64(0) || f.threadedMemory0 {
		return
	}
	p, ok := f.inspectMaskedXorStripe(source)
	if !ok {
		return
	}
	available := 0
	blocked := f.blockedFRegs(0)
	for _, r := range fpAllocRegs {
		if !(r >= 8 && r < 16) && !blocked.has(r) && f.fregUser[r] == nil {
			available++
		}
	}
	if available < 2 {
		return
	}
	oldPinned, oldFPinned := f.pinned, f.fpinned
	a := f.allocRegOrNone(0)
	if a == regNone {
		return
	}
	f.pinned = f.pinned.add(a)
	b := f.allocRegOrNone(0)
	defer func() {
		f.pinned, f.fpinned = oldPinned, oldFPinned
		f.release(a)
		if b != regNone {
			f.release(b)
		}
	}()
	if b == regNone {
		return
	}
	f.pinned = f.pinned.add(b)
	f.materializePendingTraps()
	for _, x := range []int{p.pointer, p.index} {
		_, _, pin := f.pinReg(x)
		if f.localConstZero(x) {
			f.materializeZeroLocal(x, !pin)
		}
		if pin {
			f.recoverLocal(x)
		}
	}
	read := func(x int, dst Reg) {
		if r, _, pin := f.pinReg(x); pin {
			f.a.MovReg32(dst, r)
		} else {
			f.ld32(dst, SP, f.localOff(x))
		}
	}
	read(p.pointer, a)
	read(p.index, b)
	var guards []int
	guard := func(cc Cond) { guards = append(guards, f.a.Bcond(cc)) }
	compare := func() {
		if p.limit-6 <= 4095 {
			f.a.CmpImm32(b, p.limit-6)
		} else {
			f.a.MovImm64(X16, uint64(p.limit-6))
			f.a.CmpReg32(b, X16)
		}
	}
	compare()
	guard(condA)
	f.a.TstImm32(b, 3)
	guard(condNE)
	f.a.MovImm64(X16, uint64(p.base))
	f.a.AddShifted(X16, X16, b, 2, false)
	f.a.CmpReg64(a, X16)
	guard(condNE)
	f.a.MovImm64(X16, uint64(p.base)+uint64(p.limit)*4)
	if f.memSizeReg != regNone {
		f.a.CmpReg64(X16, f.memSizeReg)
	} else {
		f.ld64(X17, linMemReg, -int32(bdCurBytes))
		f.a.CmpReg64(X16, X17)
	}
	guard(condA)
	for r := Reg(8); r < 16; r++ {
		f.fpinned = f.fpinned.add(r)
	}
	low := f.allocFReg(0)
	f.fpinned = f.fpinned.add(low)
	high := f.allocFReg(0)
	f.fpinned = f.fpinned.add(high)
	defer func() { f.releaseF(low); f.releaseF(high) }()
	f.a.Add64(a, a, linMemReg)
	header := f.a.Len()
	f.a.TstImm32(b, p.bit)
	inactive := f.a.Bcond(condE)
	f.a.MovImm64(X16, uint64(p.bit)*4)
	f.a.Sub64(X17, a, X16)
	f.a.LdrQ(low, a, 0)
	f.a.LdrQ(high, X17, 0)
	f.a.NeonEor16b(low, low, high)
	f.a.StrQ(a, 0, low)
	f.patchBranch19(inactive, f.a.Len())
	f.a.AddImm64(a, a, 16)
	f.a.AddImm32(b, b, 4)
	compare()
	f.patchBranch19(f.a.Bcond(condBE), header)
	f.a.Sub64(a, a, linMemReg)
	write := func(local int, reg Reg) {
		if r, _, pin := f.pinReg(local); pin {
			f.a.MovReg32(r, reg)
			f.markLocalDirty(local)
		} else {
			f.storeLocalReg(local, reg, false)
			f.locals[local].state = lsMem
		}
		f.invalidateLocalWriteFacts(local)
		f.setFactsForLocal(local, 0)
	}
	write(p.pointer, a)
	write(p.index, b)
	for _, site := range guards {
		f.patchBranch19(site, f.a.Len())
	}
	f.invalidateGlobalsCache()
	f.invalidateBoundsCert()
	f.invalidateStoreForward()
	f.stats.peep("masked-xor-stripe")
}
