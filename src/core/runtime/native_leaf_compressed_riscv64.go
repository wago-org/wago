//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package runtime

// Expand the arithmetic subset of RVC into standard RV64G instructions. This
// recognises Go's compact source body without requiring C in copied guest code.
// Stack, memory, branches, calls and PC-relative instructions are never expanded.
func scalarLeafCompressedRV64(h uint16) (word uint32, ret, ok bool) {
	if h == 0x8082 {
		return 0, true, true
	} // C.JR RA
	w := uint32(h)
	q, f3 := w&3, w>>13
	rd, rs2 := w>>7&31, w>>2&31
	imm := int32((w>>12&1)<<5 | w>>2&31)
	imm = imm << 26 >> 26
	i := func(op, r, source, funct uint32, value int32) uint32 {
		return uint32(value)&4095<<20 | source<<15 | funct<<12 | r<<7 | op
	}
	r := func(op, d, left, right, funct, f7 uint32) uint32 {
		return f7<<25 | right<<20 | left<<15 | funct<<12 | d<<7 | op
	}
	switch {
	case q == 1 && f3 == 0:
		return i(0x13, rd, rd, 0, imm), false, true // C.ADDI/NOP
	case q == 1 && f3 == 1 && rd != 0:
		return i(0x1b, rd, rd, 0, imm), false, true // C.ADDIW
	case q == 1 && f3 == 2 && rd != 0:
		return i(0x13, rd, 0, 0, imm), false, true // C.LI
	case q == 1 && f3 == 3 && rd != 0 && rd != 2 && imm != 0:
		return uint32(imm)<<12 | rd<<7 | 0x37, false, true // C.LUI; excludes ADDI16SP
	case q == 2 && f3 == 0 && rd != 0:
		shamt := (w>>12&1)<<5 | w>>2&31
		if shamt == 0 {
			return 0, false, false
		}
		return i(0x13, rd, rd, 1, int32(shamt)), false, true
	case q == 2 && f3 == 4 && rd != 0 && rs2 != 0:
		if w>>12&1 == 0 {
			return r(0x33, rd, 0, rs2, 0, 0), false, true
		}
		return r(0x33, rd, rd, rs2, 0, 0), false, true // C.MV/ADD
	case q == 1 && f3 == 4:
		d, right := 8+(w>>7&7), 8+(w>>2&7)
		sub := w >> 10 & 3
		switch sub {
		case 0, 1:
			shamt := (w>>12&1)<<5 | w>>2&31
			if shamt == 0 {
				return 0, false, false
			}
			if sub == 1 {
				shamt |= 0x400
			}
			return i(0x13, d, d, 5, int32(shamt)), false, true
		case 2:
			return i(0x13, d, d, 7, imm), false, true
		case 3:
			action := w >> 5 & 3
			if w>>12&1 != 0 {
				if action == 0 {
					return r(0x3b, d, d, right, 0, 32), false, true
				}
				if action == 1 {
					return r(0x3b, d, d, right, 0, 0), false, true
				}
				return 0, false, false
			}
			funct := []uint32{0, 4, 6, 7}[action]
			f7 := uint32(0)
			if action == 0 {
				f7 = 32
			}
			return r(0x33, d, d, right, funct, f7), false, true
		}
	}
	return 0, false, false
}
