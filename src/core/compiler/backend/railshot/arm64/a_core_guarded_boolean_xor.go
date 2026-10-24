//go:build arm64

package arm64

// Deliberately narrow, architecturally pure integer encodings. Register31 is
// zero here; SP forms, extended arithmetic, division and unknown words decline.
func guardedIntegerWord(w uint32) (x guardedIntegerEffects, ok bool) {
	x.wide = w&0x80000000 != 0
	reg := func(r uint32) uint32 { return (uint32(1) << r) & 0x7fffffff }
	rd, rn, rm := w&31, w>>5&31, w>>16&31
	x.writes = reg(rd)
	switch {
	case w == nopWord:
		x.writes = 0
	case w&0x1f800000 == 0x12800000:
		op := w >> 29 & 3
		if op == 1 || !x.wide && w>>21&3 > 1 {
			return x, false
		}
		if op == 3 { // MOVK preserves the other bits of Rd.
			x.reads = reg(rd)
		}
	case w&0x1f000000 == 0x0a000000:
		if !x.wide && w>>10&63 >= 32 {
			return x, false
		}
		x.reads, x.flags = reg(rn)|reg(rm), w>>29&3 == 3
		x.copy = w&0x7fe0ffe0 == 0x2a0003e0 // ORR Rd,ZR,Rm,LSL0 (MOV)
	case w&0x1f800000 == 0x12000000:
		if rd == 31 && w>>29&3 != 3 { // Nonflag logical-immediate Rd31 is SP.
			return x, false
		}
		if !x.wide && w&(1<<22) != 0 {
			return x, false
		}
		encoded := (w>>22&1)<<6 | (^w >> 10 & 63)
		level := uint32(0)
		for encoded > 1 {
			encoded >>= 1
			level++
		}
		if level < 1 || w>>10&((1<<level)-1) == (1<<level)-1 {
			return x, false
		}
		x.reads, x.flags = reg(rn), w>>29&3 == 3
	case w&0x1f800000 == 0x11000000:
		if rn == 31 || rd == 31 && w&(1<<29) == 0 {
			return x, false
		}
		x.reads, x.flags = reg(rn), w&(1<<29) != 0
	case w&0x1f200000 == 0x0b000000:
		if w>>22&3 == 3 || !x.wide && w>>10&63 >= 32 {
			return x, false
		}
		x.reads, x.flags = reg(rn)|reg(rm), w&(1<<29) != 0
	case w&0x7fe00000 == 0x1b000000:
		x.reads = reg(rn) | reg(rm) | reg(w>>10&31)
	case w&0x1f800000 == 0x13000000:
		if w>>29&3 == 3 || (w>>22&1 != w>>31) || !x.wide && (w>>16&63 >= 32 || w>>10&63 >= 32) {
			return x, false
		}
		x.reads = reg(rn)
		if w>>29&3 == 1 { // BFM preserves destination bits.
			x.reads |= reg(rd)
		}
	case w&0x7fff0fe0 == 0x1a9f07e0: // CSET has no GPR inputs.
		x.readsFlags = true
	case w&0x1fe00000 == 0x1a800000: // Other integer conditional selects.
		if w&(1<<29) != 0 || w&(1<<11) != 0 {
			return x, false
		}
		x.readsFlags = true
		x.reads = reg(rn) | reg(rm)
	default:
		return x, false
	}
	return x, true
}
