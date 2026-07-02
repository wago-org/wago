// Package amd64 is the x86-64 instruction encoder (the Asm type) that the code
// generator in backend/railshot drives to emit machine code. It holds only the
// encoder, the Reg/Cond vocabulary, and the CompiledModule result type; the
// wasm→native code generator itself lives in backend/railshot.
package amd64

import "encoding/binary"

type Reg byte

const (
	RAX Reg = 0
	RCX Reg = 1
	RDX Reg = 2
	RBX Reg = 3
	RSP Reg = 4
	RBP Reg = 5
	RSI Reg = 6
	RDI Reg = 7
	R8  Reg = 8
	R9  Reg = 9
	R10 Reg = 10
	R11 Reg = 11
	R12 Reg = 12
	R13 Reg = 13
	R14 Reg = 14
	R15 Reg = 15
)

type Asm struct{ B []byte }

func (a *Asm) emit(bs ...byte) { a.B = append(a.B, bs...) }
func (a *Asm) imm32(v int32) {
	var t [4]byte
	binary.LittleEndian.PutUint32(t[:], uint32(v))
	a.B = append(a.B, t[:]...)
}
func (a *Asm) Len() int                  { return len(a.B) }
func (a *Asm) PatchU32(at int, v uint32) { binary.LittleEndian.PutUint32(a.B[at:], v) }

func rex(w, r, x, b bool) byte {
	v := byte(0x40)
	if w {
		v |= 0x08
	}
	if r {
		v |= 0x04
	}
	if x {
		v |= 0x02
	}
	if b {
		v |= 0x01
	}
	return v
}

// memOp requires a base that does not need a SIB byte.
func (a *Asm) memOp(opcode byte, regField byte, base Reg, disp int32, w bool) {
	rb := base >= 8
	rr := regField >= 8
	if w || rr || rb {
		a.emit(rex(w, rr, false, rb))
	}
	a.emit(opcode)
	if base&7 == 4 { // RSP/R12 base: rm=100 means "SIB follows", so emit one
		a.emit(0x80 | ((regField & 7) << 3) | 0x04) // mod=10, rm=100
		a.emit(0x24)                                // SIB: scale=0, index=none(100), base=100
		a.imm32(disp)
		return
	}
	a.emit(0x80 | ((regField & 7) << 3) | byte(base&7)) // mod=10, disp32
	a.imm32(disp)
}

func (a *Asm) Push(r Reg) {
	if r >= 8 {
		a.emit(0x41)
	}
	a.emit(0x50 | byte(r&7))
}

func (a *Asm) Pop(r Reg) {
	if r >= 8 {
		a.emit(0x41)
	}
	a.emit(0x58 | byte(r&7))
}

func (a *Asm) MovImm32(r Reg, v int32) {
	if r >= 8 {
		a.emit(0x41)
	}
	a.emit(0xB8 | byte(r&7))
	a.imm32(v)
}

func (a *Asm) MovRegReg32(dst, src Reg) {
	rr := src >= 8
	rb := dst >= 8
	if rr || rb {
		a.emit(rex(false, rr, false, rb))
	}
	a.emit(0x89)
	a.emit(0xC0 | ((byte(src) & 7) << 3) | byte(dst&7))
}

func (a *Asm) sseBitOp(opcode byte, dst, src Reg, w bool) {
	a.emit(0xF3)
	if w || dst >= 8 || src >= 8 {
		a.emit(rex(w, dst >= 8, false, src >= 8))
	}
	a.emit(0x0F, opcode, 0xC0|((byte(dst)&7)<<3)|byte(src&7))
}

func (a *Asm) Lzcnt(dst, src Reg, w bool)  { a.sseBitOp(0xBD, dst, src, w) }
func (a *Asm) Tzcnt(dst, src Reg, w bool)  { a.sseBitOp(0xBC, dst, src, w) }
func (a *Asm) Popcnt(dst, src Reg, w bool) { a.sseBitOp(0xB8, dst, src, w) }

func (a *Asm) MovReg64(dst, src Reg) {
	a.emit(rex(true, src >= 8, false, dst >= 8), 0x89, 0xC0|((byte(src)&7)<<3)|byte(dst&7))
}

// Xchg64 exchanges the contents of two 64-bit registers (xchg r/m64, r64).
func (a *Asm) Xchg64(x, y Reg) {
	a.emit(rex(true, x >= 8, false, y >= 8), 0x87, 0xC0|((byte(x)&7)<<3)|byte(y&7))
}

func (a *Asm) Movsxd(dst, src Reg) {
	a.emit(rex(true, dst >= 8, false, src >= 8), 0x63, 0xC0|((byte(dst)&7)<<3)|byte(src&7))
}

// Movsx8 sign-extends the low byte of src into dst; w selects a 64-bit dest.
// A byte source of SPL/BPL/SIL/DIL (regs 4–7) requires a mandatory REX prefix to
// select the low-byte encoding instead of the legacy AH/CH/DH/BH.
func (a *Asm) Movsx8(dst, src Reg, w bool) {
	if w || dst >= 8 || src >= 4 {
		a.emit(rex(w, dst >= 8, false, src >= 8))
	}
	a.emit(0x0F, 0xBE, 0xC0|((byte(dst)&7)<<3)|byte(src&7))
}

// Movsx16 sign-extends the low word of src into dst; w selects a 64-bit dest.
func (a *Asm) Movsx16(dst, src Reg, w bool) {
	if w || dst >= 8 || src >= 8 {
		a.emit(rex(w, dst >= 8, false, src >= 8))
	}
	a.emit(0x0F, 0xBF, 0xC0|((byte(dst)&7)<<3)|byte(src&7))
}

func (a *Asm) Load32(dst Reg, base Reg, disp int32)  { a.memOp(0x8B, byte(dst), base, disp, false) }
func (a *Asm) Store32(base Reg, disp int32, src Reg) { a.memOp(0x89, byte(src), base, disp, false) }
func (a *Asm) Load64(dst Reg, base Reg, disp int32)  { a.memOp(0x8B, byte(dst), base, disp, true) }
func (a *Asm) Store64(base Reg, disp int32, src Reg) { a.memOp(0x89, byte(src), base, disp, true) }

func (a *Asm) StoreImm32Mem(base Reg, disp int32, v int32) {
	rb := base >= 8
	if rb {
		a.emit(rex(false, false, false, rb))
	}
	a.emit(0xC7)
	a.emit(0x80 | byte(base&7)) // mod=10, reg=0
	a.imm32(disp)
	a.imm32(v)
}

func (a *Asm) alu(opcode byte, dst, src Reg, w bool) {
	if w || src >= 8 || dst >= 8 {
		a.emit(rex(w, src >= 8, false, dst >= 8))
	}
	a.emit(opcode)
	a.emit(0xC0 | ((byte(src) & 7) << 3) | byte(dst&7))
}

func (a *Asm) Add32(dst, src Reg) { a.alu(0x01, dst, src, false) }
func (a *Asm) Sub32(dst, src Reg) { a.alu(0x29, dst, src, false) }
func (a *Asm) And32(dst, src Reg) { a.alu(0x21, dst, src, false) }
func (a *Asm) Or32(dst, src Reg)  { a.alu(0x09, dst, src, false) }
func (a *Asm) Xor32(dst, src Reg) { a.alu(0x31, dst, src, false) }
func (a *Asm) Cmp32(dst, src Reg) { a.alu(0x39, dst, src, false) }

func (a *Asm) IMul(dst, src Reg, w bool) {
	if w || dst >= 8 || src >= 8 {
		a.emit(rex(w, dst >= 8, false, src >= 8))
	}
	a.emit(0x0F, 0xAF)
	a.emit(0xC0 | ((byte(dst) & 7) << 3) | byte(src&7))
}

func (a *Asm) shiftCL(digit byte, r Reg, w bool) {
	if w || r >= 8 {
		a.emit(rex(w, false, false, r >= 8))
	}
	a.emit(0xD3)
	a.emit(0xC0 | (digit << 3) | byte(r&7))
}

func (a *Asm) TestSelf(r Reg, w bool) {
	if w || r >= 8 {
		a.emit(rex(w, r >= 8, false, r >= 8))
	}
	a.emit(0x85)
	a.emit(0xC0 | ((byte(r) & 7) << 3) | byte(r&7))
}

type Cond byte

const (
	CondE  Cond = 0x4 // ==
	CondNE Cond = 0x5
	CondB  Cond = 0x2 // unsigned <
	CondAE Cond = 0x3
	CondBE Cond = 0x6
	CondA  Cond = 0x7
	CondL  Cond = 0xC // signed <
	CondGE Cond = 0xD
	CondLE Cond = 0xE
	CondG  Cond = 0xF
	CondP  Cond = 0xA // parity (ucomis unordered: PF=1)
	CondNP Cond = 0xB // not parity (ordered: PF=0)
	CondS  Cond = 0x8 // sign flag set (negative / top bit set)
)

func (a *Asm) SetccAL(c Cond) {
	a.emit(0x0F, 0x90|byte(c), 0xC0) // setcc al (ModRM 11 000 000)
	a.emit(0x0F, 0xB6, 0xC0)         // movzx eax, al
}

func (a *Asm) Leave() { a.emit(0xC9) }
func (a *Asm) Ret()   { a.emit(0xC3) }

func (a *Asm) Prologue() { a.emit(0x55, 0x48, 0x89, 0xE5) } // push rbp; mov rbp,rsp

func (a *Asm) SubRsp(v int32) { a.emit(0x48, 0x81, 0xEC); a.imm32(v) }

func (a *Asm) AluRR(rrOpcode byte, dst, src Reg, w bool) { a.alu(rrOpcode, dst, src, w) }

func (a *Asm) AluRM(rmOpcode byte, dst, base Reg, disp int32, w bool) {
	a.memOp(rmOpcode, byte(dst), base, disp, w)
}

// AluIdx emits `dst = dst <op> [base + index + disp]` (reg,r/m form) — folding a
// bounds-checked memory operand into an ALU op. rmOpcode is the reg,r/m opcode.
func (a *Asm) AluIdx(rmOpcode byte, dst, base, index Reg, disp int32, w bool) {
	if w || dst >= 8 || index >= 8 || base >= 8 {
		a.emit(rex(w, dst >= 8, index >= 8, base >= 8))
	}
	a.emit(rmOpcode)
	a.sibAddr(dst, base, index, disp)
}

// ImulIdx emits `dst = dst * [base + index + disp]`.
func (a *Asm) ImulIdx(dst, base, index Reg, disp int32, w bool) {
	if w || dst >= 8 || index >= 8 || base >= 8 {
		a.emit(rex(w, dst >= 8, index >= 8, base >= 8))
	}
	a.emit(0x0F, 0xAF)
	a.sibAddr(dst, base, index, disp)
}

// digit selects add/or/and/sub/xor/cmp.
func (a *Asm) AluRI(digit byte, dst Reg, imm int32, w bool) {
	if w || dst >= 8 {
		a.emit(rex(w, false, false, dst >= 8))
	}
	if imm >= -128 && imm <= 127 {
		a.emit(0x83, 0xC0|(digit<<3)|byte(dst&7), byte(imm))
	} else {
		a.emit(0x81, 0xC0|(digit<<3)|byte(dst&7))
		a.imm32(imm)
	}
}

func (a *Asm) ImulRM(dst, base Reg, disp int32, w bool) {
	if w || dst >= 8 || base >= 8 {
		a.emit(rex(w, dst >= 8, false, base >= 8))
	}
	a.emit(0x0F, 0xAF)
	if base&7 == 4 { // RSP/R12 base: rm=100 means "SIB follows"
		a.emit(0x80|((byte(dst)&7)<<3)|0x04, 0x24)
	} else {
		a.emit(0x80 | ((byte(dst) & 7) << 3) | byte(base&7))
	}
	a.imm32(disp)
}

func (a *Asm) ImulRI(dst Reg, imm int32, w bool) {
	if w || dst >= 8 {
		a.emit(rex(w, dst >= 8, false, dst >= 8))
	}
	mod := byte(0xC0) | ((byte(dst) & 7) << 3) | byte(dst&7)
	if imm >= -128 && imm <= 127 {
		a.emit(0x6B, mod, byte(imm))
	} else {
		a.emit(0x69, mod)
		a.imm32(imm)
	}
}

func (a *Asm) ShiftImm(digit byte, dst Reg, count byte, w bool) {
	if w || dst >= 8 {
		a.emit(rex(w, false, false, dst >= 8))
	}
	a.emit(0xC1, 0xC0|(digit<<3)|byte(dst&7), count)
}

func (a *Asm) ShiftCL(digit byte, dst Reg, w bool) { a.shiftCL(digit, dst, w) }

func (a *Asm) MovImm64(r Reg, v uint64) {
	a.emit(rex(true, false, false, r >= 8), 0xB8|byte(r&7))
	var t [8]byte
	t[0] = byte(v)
	t[1] = byte(v >> 8)
	t[2] = byte(v >> 16)
	t[3] = byte(v >> 24)
	t[4] = byte(v >> 32)
	t[5] = byte(v >> 40)
	t[6] = byte(v >> 48)
	t[7] = byte(v >> 56)
	a.B = append(a.B, t[:]...)
}

func (a *Asm) AddRsp(v int32) { a.emit(0x48, 0x81, 0xC4); a.imm32(v) }

func (a *Asm) rspMem(opcode byte, reg byte, disp int32, w bool) {
	rr := reg >= 8
	if w || rr {
		a.emit(rex(w, rr, false, false))
	}
	a.emit(opcode)
	a.emit(0x80 | ((reg & 7) << 3) | 0x04) // mod=10, rm=100 (SIB)
	a.emit(0x24)                           // SIB: scale=0 index=none base=rsp
	a.imm32(disp)
}

func (a *Asm) StoreRsp32(disp int32, src Reg) { a.rspMem(0x89, byte(src), disp, false) }
func (a *Asm) LoadRsp32(dst Reg, disp int32)  { a.rspMem(0x8B, byte(dst), disp, false) }
func (a *Asm) StoreRsp64(disp int32, src Reg) { a.rspMem(0x89, byte(src), disp, true) }
func (a *Asm) LoadRsp64(dst Reg, disp int32)  { a.rspMem(0x8B, byte(dst), disp, true) }

func (a *Asm) LeaRsp(dst Reg, disp int32) { a.rspMem(0x8D, byte(dst), disp, true) }

func (a *Asm) MovFromRsp(dst Reg) {
	a.emit(rex(true, false, false, dst >= 8), 0x89, 0xC0|(4<<3)|byte(dst&7))
}

func (a *Asm) CallRel32() int { a.emit(0xE8); off := a.Len(); a.imm32(0); return off }

func (a *Asm) CallReg(r Reg) {
	if r >= 8 {
		a.emit(0x41)
	}
	a.emit(0xFF, 0xD0|byte(r&7))
}

func (a *Asm) LeaScaled(dst, base, index Reg, scaleLog uint8, disp int8) {
	a.LeaScaledW(dst, base, index, scaleLog, disp, true)
}

// LeaScaledW is LeaScaled with an explicit destination width. w=false yields a
// 32-bit result (the address is computed in 64-bit and truncated+zero-extended),
// which matches i32 wraparound arithmetic.
func (a *Asm) LeaScaledW(dst, base, index Reg, scaleLog uint8, disp int8, w bool) {
	if w || dst >= 8 || index >= 8 || base >= 8 {
		a.emit(rex(w, dst >= 8, index >= 8, base >= 8))
	}
	a.emit(0x8D, 0x40|((byte(dst)&7)<<3)|0x04) // mod=01 disp8, rm=100 (SIB)
	a.emit((scaleLog << 6) | ((byte(index) & 7) << 3) | byte(base&7))
	a.emit(byte(disp))
}

// LeaDispW is `lea dst, [base + disp]` with an explicit destination width.
func (a *Asm) LeaDispW(dst, base Reg, disp int32, w bool) { a.memOp(0x8D, byte(dst), base, disp, w) }

func (a *Asm) Add64(dst, src Reg) {
	a.emit(rex(true, src >= 8, false, dst >= 8), 0x01, 0xC0|((byte(src)&7)<<3)|byte(dst&7))
}

func (a *Asm) Cmp64(x, y Reg) {
	a.emit(rex(true, y >= 8, false, x >= 8), 0x39, 0xC0|((byte(y)&7)<<3)|byte(x&7))
}

func (a *Asm) LeaDisp(dst, base Reg, disp int32) { a.memOp(0x8D, byte(dst), base, disp, true) }

// String ops for bulk memory. In 64-bit mode these use RSI/RDI (64-bit
// pointers) and RCX (64-bit count); rep stosb stores AL. Direction is DF.
func (a *Asm) RepMovsb() { a.emit(0xF3, 0xA4) } // rep movs byte [RDI] <- [RSI], RCX times
func (a *Asm) RepStosb() { a.emit(0xF3, 0xAA) } // rep stos byte [RDI] <- AL, RCX times
func (a *Asm) Std()      { a.emit(0xFD) }       // set direction flag (decrement)
func (a *Asm) Cld()      { a.emit(0xFC) }       // clear direction flag (increment)

// LoadIdx loads `size` bytes from [base+index] into dst. signed selects sign-
// vs zero-extension; wide selects a 64-bit destination (i64), so signed
// sub-width loads sign-extend to all 64 bits instead of only 32. Unsigned loads
// zero-extend to 64 regardless of wide (x86 movzx/32-bit mov clear the top).
// sibAddr emits ModRM + SIB (+ disp32 when disp != 0) for a [base + index + disp]
// operand (scale 1) with the given reg field. disp == 0 uses the compact mod=00
// form; a folded wasm memarg offset uses mod=10 disp32.
func (a *Asm) sibAddr(reg, base, index Reg, disp int32) {
	mod := byte(0x00)
	if disp != 0 {
		mod = 0x80 // mod=10, disp32
	}
	a.emit(mod | ((byte(reg) & 7) << 3) | 0x04)     // ModRM rm=100 (SIB)
	a.emit(((byte(index) & 7) << 3) | byte(base&7)) // SIB scale=0 index base
	if disp != 0 {
		a.imm32(disp)
	}
}

func (a *Asm) LoadIdx(dst, base, index Reg, disp int32, size int, signed, wide bool) {
	var op []byte
	rexW := false
	switch {
	case size == 8:
		op, rexW = []byte{0x8B}, true // mov r64, m64
	case size == 4 && signed && wide:
		op, rexW = []byte{0x63}, true // movsxd r64, m32
	case size == 4:
		op = []byte{0x8B} // mov r32, m32 (zero-extends to 64)
	case size == 1 && signed:
		op, rexW = []byte{0x0F, 0xBE}, wide // movsx r, m8
	case size == 1:
		op = []byte{0x0F, 0xB6} // movzx r, m8 (zero-extends to 64)
	case size == 2 && signed:
		op, rexW = []byte{0x0F, 0xBF}, wide // movsx r, m16
	default: // size == 2 unsigned
		op = []byte{0x0F, 0xB7} // movzx r, m16 (zero-extends to 64)
	}
	if rexW || dst >= 8 || index >= 8 || base >= 8 {
		a.emit(rex(rexW, dst >= 8, index >= 8, base >= 8))
	}
	a.emit(op...)
	a.sibAddr(dst, base, index, disp)
}

func (a *Asm) StoreIdx(base, index, src Reg, disp int32, size int) {
	if size == 2 {
		a.emit(0x66) // operand-size prefix for 16-bit
	}
	w := size == 8
	// A byte store from SPL/BPL/SIL/DIL (regs 4–7) needs a mandatory REX to select
	// the low-byte encoding instead of the legacy AH/CH/DH/BH.
	if w || src >= 8 || index >= 8 || base >= 8 || (size == 1 && src >= 4) {
		a.emit(rex(w, src >= 8, index >= 8, base >= 8))
	}
	op := byte(0x89)
	if size == 1 {
		op = 0x88
	}
	a.emit(op)
	a.sibAddr(src, base, index, disp)
}

func (a *Asm) Cdq(w bool) {
	if w {
		a.emit(0x48)
	}
	a.emit(0x99)
}

func (a *Asm) Idiv(r Reg, w bool) {
	if w || r >= 8 {
		a.emit(rex(w, false, false, r >= 8))
	}
	a.emit(0xF7, 0xF8|byte(r&7)) // 0xF7 /7
}

func (a *Asm) Div(r Reg, w bool) {
	if w || r >= 8 {
		a.emit(rex(w, false, false, r >= 8))
	}
	a.emit(0xF7, 0xF0|byte(r&7)) // 0xF7 /6
}

// XorSelf32 zeroes r and clears the upper 32 bits.
func (a *Asm) XorSelf32(r Reg) { a.alu(0x31, r, r, false) }

func (a *Asm) JmpPlaceholder() int { a.emit(0xE9); off := a.Len(); a.imm32(0); return off }

func (a *Asm) JccPlaceholder(c Cond) int {
	a.emit(0x0F, 0x80|byte(c))
	off := a.Len()
	a.imm32(0)
	return off
}

func (a *Asm) PatchRel32(at, target int) { a.PatchU32(at, uint32(int32(target-(at+4)))) }

func (a *Asm) JmpBack(target int) { a.emit(0xE9); off := a.Len(); a.imm32(int32(target - (off + 4))) }

func (a *Asm) Cmovcc(cc Cond, dst, src Reg, w bool) {
	if w || dst >= 8 || src >= 8 {
		a.emit(rex(w, dst >= 8, false, src >= 8))
	}
	a.emit(0x0F, 0x40|byte(cc), 0xC0|((byte(dst)&7)<<3)|byte(src&7))
}

func (a *Asm) SetccReg(c Cond, dst Reg) {
	if dst >= 4 {
		a.emit(rex(false, false, false, dst >= 8))
	}
	a.emit(0x0F, 0x90|byte(c), 0xC0|byte(dst&7))
	if dst >= 4 {
		a.emit(rex(false, dst >= 8, false, dst >= 8))
	}
	a.emit(0x0F, 0xB6, 0xC0|((byte(dst)&7)<<3)|byte(dst&7))
}
