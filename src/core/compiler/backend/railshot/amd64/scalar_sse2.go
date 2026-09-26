//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

func (f *fn) cpuHas(features shared.AMD64Features) bool {
	if f.sc == nil {
		return (shared.AMD64ModernBaseline | shared.AMD64BMI2).Has(features)
	}
	if !f.sc.amd64Features.Has(features) {
		return false
	}
	f.sc.usedAMD64Features |= features
	return true
}

func (f *fn) scalarBinary(vop func(Reg, Reg, Reg, bool), op byte, dst, s1, s2 Reg, f64 bool) {
	if f.cpuHas(shared.AMD64AVX) {
		vop(dst, s1, s2, f64)
		return
	}
	if dst == s2 && dst != s1 {
		tmp := f.allocFReg(maskOf(dst, s1, s2))
		f.a.FMov(tmp, s2, f64)
		f.a.FMov(dst, s1, f64)
		f.a.SseRR(scalarFloatPrefix(f64), op, dst, tmp, false)
		f.releaseF(tmp)
		return
	}
	if dst != s1 {
		f.a.FMov(dst, s1, f64)
	}
	f.a.SseRR(scalarFloatPrefix(f64), op, dst, s2, false)
}

// All scalar sign operations here are commutative bitwise AND/XOR.
func (f *fn) scalarLogic(pp, op byte, dst, s1, s2 Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VSseRRR(pp, op, dst, s1, s2)
		return
	}
	if dst == s2 {
		s1, s2 = s2, s1
	}
	if dst != s1 {
		f.a.SseRR(0, 0x28, dst, s1, false)
	} // MOVAPS register form
	prefix := byte(0)
	if pp == 1 {
		prefix = 0x66
	}
	f.a.SseRR(prefix, op, dst, s2, false)
}

func (f *fn) scalarZero(x Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VPxor(x, x, x)
	} else {
		f.a.SseRR(0x66, 0xef, x, x, false)
	}
}

func (f *fn) scalarRound(dst, src Reg, f64 bool, mode byte) {
	if f.cpuHas(shared.AMD64SSE41) {
		f.a.Round(dst, src, f64, mode)
		return
	}
	// GPR allocation can materialize a pending floating load into an XMM
	// scratch register. The popped rounding operands have no allocator owner.
	oldF := f.fpinned
	f.fpinned = oldF.union(maskOf(dst, src))
	// RCX is fixed by variable shifts. Preserve allocator ownership before
	// claiming it, and pin it until all temporary GPRs have been released.
	f.spillIfUsed(RCX)
	old := f.pinned
	f.pinned = f.pinned.add(RCX)
	bits := f.allocReg(0)
	magnitude := f.allocReg(maskOf(bits))
	mask := f.allocReg(maskOf(bits, magnitude))
	tmp := f.allocReg(maskOf(bits, magnitude, mask))
	emitRoundSSE2(f.a, dst, src, bits, magnitude, mask, tmp, f64, mode)
	f.release(tmp)
	f.release(mask)
	f.release(magnitude)
	f.release(bits)
	f.pinned = old
	f.fpinned = oldF
}

func (f *fn) mov128LoadDisp(dst, base Reg, disp int32) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VMovdquLoadDisp(dst, base, disp)
	} else {
		f.a.MovdquLoadDisp(dst, base, disp)
	}
}

func (f *fn) mov128StoreDisp(base Reg, disp int32, src Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VMovdquStoreDisp(base, disp, src)
	} else {
		f.a.MovdquStoreDisp(base, disp, src)
	}
}

func (f *fn) mov128LoadIdx(dst, base, index Reg, disp int32) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VMovdquLoadIdx(dst, base, index, disp)
	} else {
		f.a.MovdquLoadIdx(dst, base, index, disp)
	}
}

func (f *fn) mov128StoreIdx(base, index, src Reg, disp int32) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VMovdquStoreIdx(base, index, src, disp)
	} else {
		f.a.MovdquStoreIdx(base, index, src, disp)
	}
}

func (f *fn) mov128(dst, src Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VMovdqu(dst, src)
	} else {
		f.a.SseRR(0xf3, 0x6f, dst, src, false)
	}
}
