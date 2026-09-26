//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

func packedPrefix(f64 bool) byte {
	if f64 {
		return 0x66
	}
	return 0
}

func (f *fn) legacySIMDImmediate(prefix, op byte, dst, left, right Reg, imm byte) {
	tmp := regNone
	slot := 0
	if dst == right && dst != left {
		tmp = 0
		for tmp == dst || tmp == left {
			tmp++
		}
		slot = f.allocSpillSlots(2)
		f.mov128StoreDisp(RSP, f.spillOff(slot), tmp)
		f.mov128(tmp, right)
		right = tmp
	}
	if dst != left {
		f.mov128(dst, left)
	}
	f.a.SseMapRRI(prefix, 0, op, dst, right, imm)
	if tmp != regNone {
		f.mov128LoadDisp(tmp, RSP, f.spillOff(slot))
	}
}

func (f *fn) emitVFCmpPacked(dst, left, right Reg, f64 bool, imm byte) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VFCmpPacked(dst, left, right, f64, imm)
		return
	}
	// Legacy comparisons have only eight predicates. Reverse operands for
	// ordered greater/greater-equal; unordered inputs must still yield false.
	switch imm {
	case vfcmpGtOQ:
		left, right, imm = right, left, 1
	case vfcmpGeOQ:
		left, right, imm = right, left, 2
	default:
		imm &= 7
	}
	f.legacySIMDImmediate(packedPrefix(f64), 0xc2, dst, left, right, imm)
}
func (f *fn) emitVShufps(dst, left, right Reg, imm byte) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VShufps(dst, left, right, imm)
		return
	}
	f.legacySIMDImmediate(0, 0xc6, dst, left, right, imm)
}
func (f *fn) emitVSseRRR(pp, op byte, dst, left, right Reg) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VSseRRR(pp, op, dst, left, right)
		return
	}
	prefix := byte(0)
	if pp == 1 {
		prefix = 0x66
	}
	f.legacySIMDBinary(prefix, 0, op, dst, left, right)
}
func (f *fn) emitVFPackedSqrt(dst, src Reg, f64 bool) {
	if f.cpuHas(shared.AMD64AVX) {
		f.a.VFPackedSqrt(dst, src, f64)
		return
	}
	f.a.SseRR(packedPrefix(f64), 0x51, dst, src, false)
}
func (f *fn) emitVFRoundPacked(dst, src Reg, f64 bool, imm byte) {
	if f.cpuHas(shared.AMD64AVX | shared.AMD64SSE41) {
		f.a.VFRoundPacked(dst, src, f64, imm)
		return
	}
	if f.cpuHas(shared.AMD64SSE41) {
		op := byte(8)
		if f64 {
			op = 9
		}
		f.a.SseMapRRI(0x66, 0x3a, op, dst, src, imm)
		return
	}
	old := f.fpinned
	f.fpinned = old.union(maskOf(dst, src))
	tmp := f.allocFReg(0)
	f.fpinned = f.fpinned.add(tmp)
	slot := f.allocSpillSlots(2)
	floor := f.spillFloor
	f.spillFloor = slot + 2
	off := f.spillOff(slot)
	f.mov128StoreDisp(RSP, off, src)
	width := 4
	if f64 {
		width = 8
	}
	for i := 0; i < 16; i += width {
		f.a.FLoadDisp(tmp, RSP, off+int32(i), f64)
		f.scalarRound(tmp, tmp, f64, imm)
		f.a.FStoreDisp(RSP, off+int32(i), tmp, f64)
	}
	f.mov128LoadDisp(dst, RSP, off)
	f.spillFloor = floor
	f.releaseF(tmp)
	f.fpinned = old
}

func (f *fn) extractSIMDLane(dst, src Reg, lane byte, width int) {
	if width == 1 {
		f.a.Pextrw(dst, src, lane/2)
		if lane&1 != 0 {
			f.a.ShiftImm(5, dst, 8, false)
		}
		f.a.AluRI(4, dst, 255, false)
		return
	}
	if lane == 0 {
		f.a.MovXmmToGpr(dst, src, width == 8)
		return
	}
	tmp := Reg(0)
	if tmp == src {
		tmp++
	}
	slot := f.allocSpillSlots(2)
	f.mov128StoreDisp(RSP, f.spillOff(slot), tmp)
	control := lane
	if width == 8 {
		control = 0x4e
	}
	f.a.Pshufd(tmp, src, control)
	f.a.MovXmmToGpr(dst, tmp, width == 8)
	f.mov128LoadDisp(tmp, RSP, f.spillOff(slot))
}
func (f *fn) insertSIMDLane(dst, src Reg, lane byte, width int) {
	tmp, other := RAX, RDX
	if src == tmp {
		tmp = R11
	}
	if src == other {
		other = R11
	}
	slot := f.allocSpillSlots(2)
	off := f.spillOff(slot)
	f.a.Store64(RSP, off, tmp)
	f.a.Store64(RSP, off+8, other)
	f.a.AluRR(0x89, tmp, src, width == 8)
	if width == 1 {
		f.a.Pextrw(other, dst, lane/2)
		f.a.AluRI(4, tmp, 255, false)
		keep := int32(0xff00)
		if lane&1 != 0 {
			f.a.ShiftImm(4, tmp, 8, false)
			keep = 255
		}
		f.a.AluRI(4, other, keep, false)
		f.a.AluRR(0x09, tmp, other, false)
		f.a.Pinsrw(dst, tmp, lane/2)
	} else {
		for i := 0; i < width/2; i++ {
			f.a.Pinsrw(dst, tmp, lane*byte(width/2)+byte(i))
			if i+1 < width/2 {
				f.a.ShiftImm(5, tmp, 16, width == 8)
			}
		}
	}
	f.a.Load64(tmp, RSP, off)
	f.a.Load64(other, RSP, off+8)
}

// simdTestZero supplies the ZF contract consumed by any_true fusion. No caller
// consumes PTEST's carry flag. Compare against all sixteen zero-byte matches.
func (f *fn) simdTestZero(left, right Reg) {
	oldF, oldG := f.fpinned, f.pinned
	f.fpinned = oldF.union(maskOf(left, right))
	x := f.allocFReg(0)
	f.fpinned = f.fpinned.add(x)
	z := f.allocFReg(0)
	f.fpinned = f.fpinned.add(z)
	r := f.allocReg(0)
	f.mov128(x, left)
	f.a.SseRR(0x66, 0xdb, x, right, false)
	f.a.SseRR(0x66, 0xef, z, z, false)
	f.a.SseRR(0x66, 0x74, x, z, false)
	f.a.Pmovmskb(r, x)
	f.a.AluRI(7, r, 65535, false)
	f.release(r)
	f.releaseF(x)
	f.releaseF(z)
	f.fpinned, f.pinned = oldF, oldG
}
