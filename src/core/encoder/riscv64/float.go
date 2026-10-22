package riscv64

// FReg names the separate physical floating-point register file.
type FReg uint8

const (
	F0 FReg = iota
	F1
	F2
	F3
	F4
	F5
	F6
	F7
	F8
	F9
	F10
	F11
	F12
	F13
	F14
	F15
	F16
	F17
	F18
	F19
	F20
	F21
	F22
	F23
	F24
	F25
	F26
	F27
	F28
	F29
	F30
	F31
)

// RoundingMode is the architectural instruction rm field. Dynamic depends on
// the caller's frm; Wasm lowering must choose a static mode instead.
type RoundingMode uint8

const (
	RNE RoundingMode = iota
	RTZ
	RDN
	RUP
	RMM
	Dynamic RoundingMode = 7
)

func (a *Asm) rounding(mode RoundingMode) bool {
	if mode > RMM && mode != Dynamic {
		a.fail("reserved floating-point rounding mode")
	}
	return a.err == nil
}

// floatWord validates both register files before reporting any effect. rs2 can
// be a fixed conversion selector rather than a register; public methods select
// that field themselves. Every FP definition kills the full FLEN=64 identity,
// including the NaN-boxed upper word of a single-precision result.
func (a *Asm) floatWord(f3, f7 uint32, dst, left, right uint8, floatingDst bool) {
	if !a.registers(Reg(dst), Reg(left), Reg(right)) {
		return
	}
	if regallocCheckEnabled && len(a.B)&3 == 0 {
		if floatingDst {
			a.regallocFloatDefinition(FReg(dst))
		} else {
			a.regallocDefinition(Reg(dst), false)
		}
	}
	a.word(0x53 | uint32(dst)<<7 | f3<<12 | uint32(left)<<15 | uint32(right)<<20 | f7<<25)
}

func (a *Asm) FaddS(dst, left, right FReg, mode RoundingMode) {
	if a.rounding(mode) {
		a.floatWord(uint32(mode), 0x0, uint8(dst), uint8(left), uint8(right), true)
	}
}

func (a *Asm) FsubS(dst, left, right FReg, mode RoundingMode) {
	if a.rounding(mode) {
		a.floatWord(uint32(mode), 0x4, uint8(dst), uint8(left), uint8(right), true)
	}
}

func (a *Asm) FmulS(dst, left, right FReg, mode RoundingMode) {
	if a.rounding(mode) {
		a.floatWord(uint32(mode), 0x8, uint8(dst), uint8(left), uint8(right), true)
	}
}

func (a *Asm) FdivS(dst, left, right FReg, mode RoundingMode) {
	if a.rounding(mode) {
		a.floatWord(uint32(mode), 0xc, uint8(dst), uint8(left), uint8(right), true)
	}
}

func (a *Asm) FsqrtS(dst, src FReg, mode RoundingMode) {
	if a.rounding(mode) {
		a.floatWord(uint32(mode), 0x2c, uint8(dst), uint8(src), 0, true)
	}
}

func (a *Asm) FsgnjS(dst, left, right FReg) {
	a.floatWord(0, 0x10, uint8(dst), uint8(left), uint8(right), true)
}

func (a *Asm) FsgnjnS(dst, left, right FReg) {
	a.floatWord(1, 0x10, uint8(dst), uint8(left), uint8(right), true)
}

func (a *Asm) FsgnjxS(dst, left, right FReg) {
	a.floatWord(2, 0x10, uint8(dst), uint8(left), uint8(right), true)
}

func (a *Asm) FminS(dst, left, right FReg) {
	a.floatWord(0, 0x14, uint8(dst), uint8(left), uint8(right), true)
}

func (a *Asm) FmaxS(dst, left, right FReg) {
	a.floatWord(1, 0x14, uint8(dst), uint8(left), uint8(right), true)
}

func (a *Asm) FeqS(dst Reg, left, right FReg) {
	a.floatWord(2, 0x50, uint8(dst), uint8(left), uint8(right), false)
}

func (a *Asm) FltS(dst Reg, left, right FReg) {
	a.floatWord(1, 0x50, uint8(dst), uint8(left), uint8(right), false)
}

func (a *Asm) FleS(dst Reg, left, right FReg) {
	a.floatWord(0, 0x50, uint8(dst), uint8(left), uint8(right), false)
}

func (a *Asm) FclassS(dst Reg, src FReg) { a.floatWord(1, 0x70, uint8(dst), uint8(src), 0, false) }

func (a *Asm) FcvtWS(dst Reg, src FReg, mode RoundingMode) {
	if a.rounding(mode) {
		a.floatWord(uint32(mode), 0x60, uint8(dst), uint8(src), 0, false)
	}
}

func (a *Asm) FcvtSW(dst FReg, src Reg, mode RoundingMode) {
	if a.rounding(mode) {
		a.floatWord(uint32(mode), 0x68, uint8(dst), uint8(src), 0, true)
	}
}

func (a *Asm) FcvtWuS(dst Reg, src FReg, mode RoundingMode) {
	if a.rounding(mode) {
		a.floatWord(uint32(mode), 0x60, uint8(dst), uint8(src), 1, false)
	}
}

func (a *Asm) FcvtSWu(dst FReg, src Reg, mode RoundingMode) {
	if a.rounding(mode) {
		a.floatWord(uint32(mode), 0x68, uint8(dst), uint8(src), 1, true)
	}
}

func (a *Asm) FcvtLS(dst Reg, src FReg, mode RoundingMode) {
	if a.rounding(mode) {
		a.floatWord(uint32(mode), 0x60, uint8(dst), uint8(src), 2, false)
	}
}

func (a *Asm) FcvtSL(dst FReg, src Reg, mode RoundingMode) {
	if a.rounding(mode) {
		a.floatWord(uint32(mode), 0x68, uint8(dst), uint8(src), 2, true)
	}
}

func (a *Asm) FcvtLuS(dst Reg, src FReg, mode RoundingMode) {
	if a.rounding(mode) {
		a.floatWord(uint32(mode), 0x60, uint8(dst), uint8(src), 3, false)
	}
}

func (a *Asm) FcvtSLu(dst FReg, src Reg, mode RoundingMode) {
	if a.rounding(mode) {
		a.floatWord(uint32(mode), 0x68, uint8(dst), uint8(src), 3, true)
	}
}

func (a *Asm) FaddD(dst, left, right FReg, mode RoundingMode) {
	if a.rounding(mode) {
		a.floatWord(uint32(mode), 0x1, uint8(dst), uint8(left), uint8(right), true)
	}
}

func (a *Asm) FsubD(dst, left, right FReg, mode RoundingMode) {
	if a.rounding(mode) {
		a.floatWord(uint32(mode), 0x5, uint8(dst), uint8(left), uint8(right), true)
	}
}

func (a *Asm) FmulD(dst, left, right FReg, mode RoundingMode) {
	if a.rounding(mode) {
		a.floatWord(uint32(mode), 0x9, uint8(dst), uint8(left), uint8(right), true)
	}
}

func (a *Asm) FdivD(dst, left, right FReg, mode RoundingMode) {
	if a.rounding(mode) {
		a.floatWord(uint32(mode), 0xd, uint8(dst), uint8(left), uint8(right), true)
	}
}

func (a *Asm) FsqrtD(dst, src FReg, mode RoundingMode) {
	if a.rounding(mode) {
		a.floatWord(uint32(mode), 0x2d, uint8(dst), uint8(src), 0, true)
	}
}

func (a *Asm) FsgnjD(dst, left, right FReg) {
	a.floatWord(0, 0x11, uint8(dst), uint8(left), uint8(right), true)
}

func (a *Asm) FsgnjnD(dst, left, right FReg) {
	a.floatWord(1, 0x11, uint8(dst), uint8(left), uint8(right), true)
}

func (a *Asm) FsgnjxD(dst, left, right FReg) {
	a.floatWord(2, 0x11, uint8(dst), uint8(left), uint8(right), true)
}

func (a *Asm) FminD(dst, left, right FReg) {
	a.floatWord(0, 0x15, uint8(dst), uint8(left), uint8(right), true)
}

func (a *Asm) FmaxD(dst, left, right FReg) {
	a.floatWord(1, 0x15, uint8(dst), uint8(left), uint8(right), true)
}

func (a *Asm) FeqD(dst Reg, left, right FReg) {
	a.floatWord(2, 0x51, uint8(dst), uint8(left), uint8(right), false)
}

func (a *Asm) FltD(dst Reg, left, right FReg) {
	a.floatWord(1, 0x51, uint8(dst), uint8(left), uint8(right), false)
}

func (a *Asm) FleD(dst Reg, left, right FReg) {
	a.floatWord(0, 0x51, uint8(dst), uint8(left), uint8(right), false)
}

func (a *Asm) FclassD(dst Reg, src FReg) { a.floatWord(1, 0x71, uint8(dst), uint8(src), 0, false) }

func (a *Asm) FcvtWD(dst Reg, src FReg, mode RoundingMode) {
	if a.rounding(mode) {
		a.floatWord(uint32(mode), 0x61, uint8(dst), uint8(src), 0, false)
	}
}

func (a *Asm) FcvtDW(dst FReg, src Reg, mode RoundingMode) {
	if a.rounding(mode) {
		a.floatWord(uint32(mode), 0x69, uint8(dst), uint8(src), 0, true)
	}
}

func (a *Asm) FcvtWuD(dst Reg, src FReg, mode RoundingMode) {
	if a.rounding(mode) {
		a.floatWord(uint32(mode), 0x61, uint8(dst), uint8(src), 1, false)
	}
}

func (a *Asm) FcvtDWu(dst FReg, src Reg, mode RoundingMode) {
	if a.rounding(mode) {
		a.floatWord(uint32(mode), 0x69, uint8(dst), uint8(src), 1, true)
	}
}

func (a *Asm) FcvtLD(dst Reg, src FReg, mode RoundingMode) {
	if a.rounding(mode) {
		a.floatWord(uint32(mode), 0x61, uint8(dst), uint8(src), 2, false)
	}
}

func (a *Asm) FcvtDL(dst FReg, src Reg, mode RoundingMode) {
	if a.rounding(mode) {
		a.floatWord(uint32(mode), 0x69, uint8(dst), uint8(src), 2, true)
	}
}

func (a *Asm) FcvtLuD(dst Reg, src FReg, mode RoundingMode) {
	if a.rounding(mode) {
		a.floatWord(uint32(mode), 0x61, uint8(dst), uint8(src), 3, false)
	}
}

func (a *Asm) FcvtDLu(dst FReg, src Reg, mode RoundingMode) {
	if a.rounding(mode) {
		a.floatWord(uint32(mode), 0x69, uint8(dst), uint8(src), 3, true)
	}
}

// FmvXW transfers raw bits; FMV.X.W sign-extends, FMV.W.X NaN-boxes.
func (a *Asm) FmvXW(dst Reg, src FReg) { a.floatWord(0, 0x70, uint8(dst), uint8(src), 0, false) }

// FmvWX transfers raw bits; FMV.X.W sign-extends, FMV.W.X NaN-boxes.
func (a *Asm) FmvWX(dst FReg, src Reg) { a.floatWord(0, 0x78, uint8(dst), uint8(src), 0, true) }

// FmvXD transfers all 64 raw bits from an FP register to a GP register.
func (a *Asm) FmvXD(dst Reg, src FReg) { a.floatWord(0, 0x71, uint8(dst), uint8(src), 0, false) }

// FmvDX transfers all 64 raw bits from a GP register to an FP register.
func (a *Asm) FmvDX(dst FReg, src Reg) { a.floatWord(0, 0x79, uint8(dst), uint8(src), 0, true) }

func (a *Asm) FcvtSD(dst, src FReg, mode RoundingMode) {
	if a.rounding(mode) {
		a.floatWord(uint32(mode), 0x20, uint8(dst), uint8(src), 1, true)
	}
}

func (a *Asm) FcvtDS(dst, src FReg, mode RoundingMode) {
	if a.rounding(mode) {
		a.floatWord(uint32(mode), 0x21, uint8(dst), uint8(src), 0, true)
	}
}

// Only the floating-point CSRs are exposed, rather than arbitrary system state.
func (a *Asm) floatCSR(csr uint32, dst, src Reg, write bool) {
	if !a.registers(dst, src) {
		return
	}
	if regallocCheckEnabled && len(a.B)&3 == 0 {
		a.regallocDefinition(dst, false)
	}
	f3 := uint32(2) // CSRRS with rs1=x0 reads without writing.
	if write {
		f3 = 1
	} // CSRRW with rd=x0 discards the old value.
	a.word(0x73 | uint32(dst)<<7 | f3<<12 | uint32(src)<<15 | csr<<20)
}

func (a *Asm) ReadFcsr(dst Reg)    { a.floatCSR(3, dst, Zero, false) }
func (a *Asm) WriteFcsr(src Reg)   { a.floatCSR(3, Zero, src, true) }
func (a *Asm) ReadFflags(dst Reg)  { a.floatCSR(1, dst, Zero, false) }
func (a *Asm) WriteFflags(src Reg) { a.floatCSR(1, Zero, src, true) }
