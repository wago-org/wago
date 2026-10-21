//go:build wago_regalloccheck

package amd64

// ObserveFPWrites observes possible writes to the low 128 bits of XMM/YMM/ZMM
// aliases, independently of transfer windows. Calls and unclassified generic
// forms conservatively report all sixteen registers. Upper-only VZEROUPPER is
// excluded. Restore the returned observer on normal completion and panic.
func (a *Asm) ObserveFPWrites(fn func(uint32)) func(uint32) {
	old := a.fpWriteObserver
	a.fpWriteObserver = fn
	return old
}

func (a *Asm) regallocFPWrite(mask uint32) {
	if mask != 0 && a.fpWriteObserver != nil {
		a.fpWriteObserver(mask)
	}
}

// Match ModRM plus the encoder's extension-bit selection, even for aliased Reg
// arguments. VEX.vvvv destinations instead use the literal low four bits.
func regallocFPRegMask(reg Reg) uint32 {
	n := reg & 7
	if reg >= 8 {
		n |= 8
	}
	return uint32(1) << n
}

// This classifies destinations, not arithmetic semantics. Unknown generic
// encodings cannot establish preservation of any active FP reservation.
func regallocFPSSEMask(prefix, opcodeMap, op byte, reg, rm Reg, memory bool) uint32 {
	if prefix != 0 && prefix != 0x66 && prefix != 0xf2 && prefix != 0xf3 {
		return 0xffff
	}
	regMask, rmMask := regallocFPRegMask(reg), regallocFPRegMask(rm)
	if memory {
		rmMask = 0
	}
	switch opcodeMap {
	case 0:
		switch op {
		case 0x11, 0x29, 0x7f: // register destination or memory store
			return rmMask
		case 0x13, 0x17, 0x2b, 0xe7: // memory-only stores
			if memory {
				return 0
			}
		case 0x2c, 0x2d, 0x2e, 0x2f, 0x50, 0xc5, 0xd7:
			// GP/MMX conversion/extraction or flag-only comparison.
			return 0
		case 0x7e:
			if prefix == 0xf3 {
				return regMask
			} // MOVQ xmm, xmm/m64
			if prefix == 0x66 || prefix == 0 {
				return 0
			} // MOVD/Q to GP/memory
		case 0xd6:
			if prefix == 0x66 {
				return rmMask
			} // MOVQ xmm/m64, xmm
			if prefix == 0xf3 {
				return regMask
			} // MOVQ2DQ
			if prefix == 0xf2 {
				return 0
			} // MOVDQ2Q writes MMX
		case 0x71, 0x72, 0x73:
			if !memory && prefix == 0x66 && regallocFPShiftGroup(op, byte(reg)&7) {
				return rmMask
			} // legacy immediate shift
		case 0x10, 0x12, 0x14, 0x15, 0x16, 0x28, 0x2a,
			0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5a, 0x5b, 0x5c, 0x5d, 0x5e, 0x5f,
			0x60, 0x61, 0x62, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68, 0x69, 0x6a, 0x6b, 0x6c, 0x6d, 0x6e, 0x6f, 0x70,
			0x74, 0x75, 0x76, 0x7c, 0x7d, 0xc2, 0xc4, 0xc6,
			0xd0, 0xd1, 0xd2, 0xd3, 0xd4, 0xd5, 0xd8, 0xd9, 0xda, 0xdb, 0xdc, 0xdd, 0xde, 0xdf,
			0xe0, 0xe1, 0xe2, 0xe3, 0xe4, 0xe5, 0xe6, 0xe8, 0xe9, 0xea, 0xeb, 0xec, 0xed, 0xee, 0xef,
			0xf1, 0xf2, 0xf3, 0xf4, 0xf5, 0xf6, 0xf8, 0xf9, 0xfa, 0xfb, 0xfc, 0xfd, 0xfe:
			return regMask
		}
	case 0x38:
		if prefix != 0x66 {
			break
		}
		switch op {
		case 0x17: // PTEST: flags only
			return 0
		case 0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b,
			0x10, 0x14, 0x15, 0x18, 0x19, 0x1a, 0x1c, 0x1d, 0x1e,
			0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x28, 0x29, 0x2a, 0x2b,
			0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x37, 0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e, 0x3f, 0x40, 0x41,
			0x45, 0x46, 0x47, 0x50, 0x51, 0x58, 0x59, 0x78, 0x79:
			return regMask
		}
	case 0x3a:
		if prefix != 0x66 {
			break
		}
		switch op {
		case 0x14, 0x15, 0x16, 0x17: // PEXTR/EXTRACTPS writes GP or memory
			return 0
		case 0x19, 0x39: // VEX extract F/I128 writes r/m
			return rmMask
		case 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
			0x18, 0x20, 0x21, 0x22, 0x25, 0x38, 0x40, 0x41, 0x42, 0x44, 0x4a, 0x4b, 0x4c:
			return regMask
		}
	}
	return 0xffff
}

func (a *Asm) regallocFPSSE(prefix, opcodeMap, op byte, reg, rm Reg, memory bool) {
	a.regallocFPWrite(regallocFPSSEMask(prefix, opcodeMap, op, reg, rm, memory))
}

func (a *Asm) regallocFPVEX(opcodeMap, pp, op byte, reg, vvvv, rm Reg, memory bool) {
	if pp&^3 != 0 {
		a.regallocFPWrite(0xffff)
		return
	}
	if opcodeMap == vexMap0F3A && pp == 3 && op == 0xf0 {
		return
	} // RORX writes GP
	if opcodeMap == vexMap0F && pp == 1 && regallocFPShiftGroup(op, byte(reg)&7) && !memory {
		a.regallocFPWrite(uint32(1) << (vvvv & 15))
		return
	}
	var m byte
	switch opcodeMap {
	case vexMap0F:
	case vexMap0F38:
		m = 0x38
	case vexMap0F3A:
		m = 0x3a
	default:
		a.regallocFPWrite(0xffff)
		return
	}
	a.regallocFPSSE([...]byte{0, 0x66, 0xf3, 0xf2}[pp&3], m, op, reg, rm, memory)
}

// regallocFPShiftGroup recognizes the supported packed immediate shift groups.
// The caller passes the physical three-bit opcode extension.
func regallocFPShiftGroup(op, ext byte) bool {
	switch op {
	case 0x71, 0x72:
		return ext == 2 || ext == 4 || ext == 6
	case 0x73:
		return ext == 2 || ext == 3 || ext == 6 || ext == 7
	}
	return false
}

func (a *Asm) regallocFPShift(op, ext byte, dst Reg) {
	if regallocFPShiftGroup(op, ext&7) {
		a.regallocFPWrite(uint32(1) << (dst & 15))
	} else {
		a.regallocGPWrite(0xffff)
		a.regallocFPWrite(0xffff)
	}
}
