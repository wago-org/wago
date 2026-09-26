//go:build amd64

package amd64

// legacySIMDBinary preserves the three-operand contract of vector lowering
// while selecting the destructive legacy SSE encoding at compile time.
func (f *fn) legacySIMDBinary(prefix, opcodeMap, op byte, dst, left, right Reg) {
	if dst == right && dst != left {
		tmp := Reg(0)
		for tmp == dst || tmp == left {
			tmp++
		}
		slot := f.allocSpillSlots(2)
		off := f.spillOff(slot)
		f.mov128StoreDisp(RSP, off, tmp)
		f.mov128(tmp, right)
		f.mov128(dst, left)
		f.a.SseMapRR(prefix, opcodeMap, op, dst, tmp)
		f.mov128LoadDisp(tmp, RSP, off)
		return
	}
	if dst != left {
		f.mov128(dst, left)
	}
	f.a.SseMapRR(prefix, opcodeMap, op, dst, right)
}

// simdFallback implements the optional 0F38 integer primitives used by
// Railshot. Sources are snapshotted before writing any result, including all
// alias combinations. Scratch belongs to the native frame, is bounded to 80
// bytes, and is reclaimed immediately; no runtime helper or heap is involved.
func (f *fn) simdFallback(op byte, dst, left, right Reg) {
	// Preserve fixed scratch registers because callers may have live
	// unowned temporaries that the operand allocator cannot see.
	x, y, z := RAX, RDX, R11
	slot := f.allocSpillSlots(10)
	off := f.spillOff(slot)
	f.a.Store64(RSP, off+56, x)
	f.a.Store64(RSP, off+64, y)
	f.a.Store64(RSP, off+72, z)
	f.mov128StoreDisp(RSP, off, left)
	f.mov128StoreDisp(RSP, off+16, right)
	load := func(r Reg, source, lane, width int, signed bool) {
		at := off + int32(source*16+lane*width)
		if width == 8 {
			f.a.Load64(r, RSP, at)
			return
		}
		f.a.Load32(r, RSP, at)
		if signed {
			f.a.ShiftImm(4, r, byte(64-width*8), true)
			f.a.ShiftImm(7, r, byte(64-width*8), true)
		} else if width < 4 {
			f.a.AluRI(4, r, int32((1<<uint(width*8))-1), false)
		}
	}
	store := func(lane, width int) {
		if width == 8 {
			f.a.Store64(RSP, off+32+int32(lane*width), x)
		} else {
			f.a.Store32(RSP, off+32+int32(lane*width), x)
		}
	}
	width := 4
	switch op {
	case 0x00, 0x1c, 0x38, 0x3c:
		width = 1
	case 0x04, 0x0b, 0x1d, 0x2b, 0x3a, 0x3e:
		width = 2
	case 0x28, 0x29, 0x37:
		width = 8
	}
	for i := 0; i < 16/width; i++ {
		switch op {
		case 0x00: // PSHUFB: low nibble selects, top bit zeros.
			load(y, 1, i, 1, false)
			f.a.AluRR(0x89, z, y, false)
			f.a.AluRI(4, y, 15, false)
			f.a.LoadIdx(x, RSP, y, off, 1, false, false)
			f.a.TestImm(z, 128, false)
			f.a.MovImm32(z, 0)
			f.a.Cmovcc(condNE, x, z, false)
		case 0x02: // PHADDD
			source, lane := i/2, (i%2)*2
			load(x, source, lane, 4, false)
			load(y, source, lane+1, 4, false)
			f.a.AluRR(0x01, x, y, false)
		case 0x04: // PMADDUBSW, unsigned*signed with signed saturation.
			load(x, 0, i*2, 1, false)
			load(y, 1, i*2, 1, true)
			f.a.IMul(x, y, false)
			load(z, 0, i*2+1, 1, false)
			load(y, 1, i*2+1, 1, true)
			f.a.IMul(z, y, false)
			f.a.AluRR(0x01, x, z, false)
			f.a.MovImm32(y, 32767)
			f.a.AluRR(0x39, x, y, false)
			f.a.Cmovcc(condG, x, y, false)
			f.a.MovImm32(y, -32768)
			f.a.AluRR(0x39, x, y, false)
			f.a.Cmovcc(condL, x, y, false)
		case 0x0b: // PMULHRSW, including the raw -32768* -32768 result.
			load(x, 0, i, 2, true)
			load(y, 1, i, 2, true)
			f.a.IMul(x, y, false)
			f.a.AluRI(0, x, 16384, false)
			f.a.ShiftImm(7, x, 15, false)
		case 0x1c, 0x1d, 0x1e: // PABS{B,W,D}
			load(x, 0, i, width, true)
			f.a.AluRR(0x89, y, x, true)
			f.a.Neg(y, true)
			f.a.TestSelf(x, true)
			f.a.Cmovcc(condL, x, y, true)
		case 0x28: // PMULDQ, signed products of even dwords.
			load(x, 0, i*2, 4, true)
			load(y, 1, i*2, 4, true)
			f.a.IMul(x, y, true)
		case 0x29, 0x37: // PCMPEQQ / PCMPGTQ
			load(x, 0, i, 8, false)
			load(y, 1, i, 8, false)
			f.a.AluRR(0x39, x, y, true)
			cc := condE
			if op == 0x37 {
				cc = condG
			}
			f.a.SetccReg(cc, x)
			f.a.Neg(x, true)
		case 0x2b: // PACKUSDW: signed dwords saturate into unsigned words.
			load(x, i/4, i%4, 4, true)
			f.a.MovImm32(y, 0)
			f.a.TestSelf(x, true)
			f.a.Cmovcc(condL, x, y, true)
			f.a.MovImm32(y, 65535)
			f.a.AluRR(0x39, x, y, true)
			f.a.Cmovcc(condG, x, y, true)
		case 0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e, 0x3f:
			signed := op == 0x38 || op == 0x39 || op == 0x3c || op == 0x3d
			load(x, 0, i, width, signed)
			load(y, 1, i, width, signed)
			f.a.AluRR(0x39, x, y, true)
			cc := condG
			if op >= 0x3c {
				cc = condL
			}
			f.a.Cmovcc(cc, x, y, true)
		case 0x40: // PMULLD
			load(x, 0, i, 4, false)
			load(y, 1, i, 4, false)
			f.a.IMul(x, y, false)
		default:
			panic("amd64: missing SIMD primitive fallback")
		}
		store(i, width)
	}
	f.mov128LoadDisp(dst, RSP, off+32)
	f.a.Load64(x, RSP, off+56)
	f.a.Load64(y, RSP, off+64)
	f.a.Load64(z, RSP, off+72)
}
