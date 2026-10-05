// Package riscv64 encodes fixed-width little-endian RV64I/M/F/D instructions.
// It does not allocate registers, publish executable memory, or implement Wasm.
package riscv64

import "fmt"

// Reg is a physical integer register number; aliases never change its encoding.
type Reg uint8

const (
	X0 Reg = iota
	X1
	X2
	X3
	X4
	X5
	X6
	X7
	X8
	X9
	X10
	X11
	X12
	X13
	X14
	X15
	X16
	X17
	X18
	X19
	X20
	X21
	X22
	X23
	X24
	X25
	X26
	X27
	X28
	X29
	X30
	X31
)

const (
	Zero = X0
	RA   = X1
	SP   = X2
	GP   = X3
	TP   = X4
	FP   = X8
	// Scratch0 and Scratch1 are permanently excluded from guest allocation.
	Scratch0 = X30
	Scratch1 = X31
	// ReservedMask also protects Go CTXT/g and provisional guest context pins.
	// This does not define the completed guest calling convention.
	ReservedMask uint64 = 1<<Zero | 1<<RA | 1<<SP | 1<<GP | 1<<TP | 1<<FP |
		1<<X24 | 1<<X25 | 1<<X26 | 1<<X27 | 1<<Scratch0 | 1<<Scratch1
)

// Cond is the architectural branch funct3. Inversion toggles its low bit.
type Cond uint8

const (
	EQ  Cond = 0
	NE  Cond = 1
	LT  Cond = 4
	GE  Cond = 5
	LTU Cond = 6
	GEU Cond = 7
)

func (c Cond) valid() bool { return c == EQ || c == NE || c >= LT && c <= GEU }

// Asm accumulates code. Its zero value is usable. Check Err before consuming B.
// Operand errors stop subsequent emission; fixup errors leave bytes unchanged.
// B may be an existing aligned writable tail, but must not be executable yet.
type Asm struct {
	regallocState
	B   []byte
	err error
}

// Err returns the first emission error. Start a new Asm to clear an error.
func (a *Asm) Err() error { return a.err }

// Len returns the code length in bytes.
func (a *Asm) Len() int { return len(a.B) }

func (a *Asm) fail(message string) {
	if a.err == nil {
		a.err = fmt.Errorf("riscv64: %s", message)
	}
}

func (a *Asm) registers(regs ...Reg) bool {
	if a.err != nil {
		return false
	}
	for _, r := range regs {
		if r > X31 {
			a.fail("register outside x0..x31")
			return false
		}
	}
	return true
}

func (a *Asm) word(w uint32) {
	if len(a.B)&3 != 0 {
		a.fail("unaligned code buffer")
	}
	if a.err == nil {
		a.B = append(a.B, byte(w), byte(w>>8), byte(w>>16), byte(w>>24))
	}
}

func (a *Asm) r(op, f3, f7 uint32, dst, left, right Reg) {
	if a.registers(dst, left, right) {
		if regallocCheckEnabled && a.err == nil && len(a.B)&3 == 0 {
			a.regallocDefinition(dst, false)
		}
		a.word(op | uint32(dst)<<7 | f3<<12 | uint32(left)<<15 | uint32(right)<<20 | f7<<25)
	}
}

func (a *Asm) i(op, f3 uint32, dst, src Reg, imm int32) {
	if !a.registers(dst, src) {
		return
	}
	if imm < -2048 || imm > 2047 {
		a.fail("immediate outside signed 12-bit range")
		return
	}
	if regallocCheckEnabled && a.err == nil && len(a.B)&3 == 0 {
		a.regallocImmediate(op, f3, dst, src, imm)
	}
	a.word(op | uint32(dst)<<7 | f3<<12 | uint32(src)<<15 | (uint32(imm)&0xfff)<<20)
}

func (a *Asm) u(op uint32, dst Reg, imm20 uint32) {
	if !a.registers(dst) {
		return
	}
	if imm20 > 0xfffff {
		a.fail("upper immediate outside 20-bit field")
		return
	}
	if regallocCheckEnabled && a.err == nil && len(a.B)&3 == 0 {
		a.regallocDefinition(dst, false)
	}
	a.word(op | uint32(dst)<<7 | imm20<<12)
}

func (a *Asm) Add(dst, left, right Reg)  { a.r(0x33, 0, 0, dst, left, right) }
func (a *Asm) Sub(dst, left, right Reg)  { a.r(0x33, 0, 0x20, dst, left, right) }
func (a *Asm) Mul(dst, left, right Reg)  { a.r(0x33, 0, 1, dst, left, right) }
func (a *Asm) Div(dst, left, right Reg)  { a.r(0x33, 4, 1, dst, left, right) }
func (a *Asm) Divu(dst, left, right Reg) { a.r(0x33, 5, 1, dst, left, right) }
func (a *Asm) Rem(dst, left, right Reg)  { a.r(0x33, 6, 1, dst, left, right) }
func (a *Asm) Remu(dst, left, right Reg) { a.r(0x33, 7, 1, dst, left, right) }
func (a *Asm) And(dst, left, right Reg)  { a.r(0x33, 7, 0, dst, left, right) }
func (a *Asm) Or(dst, left, right Reg)   { a.r(0x33, 6, 0, dst, left, right) }
func (a *Asm) Xor(dst, left, right Reg)  { a.r(0x33, 4, 0, dst, left, right) }
func (a *Asm) Slt(dst, left, right Reg)  { a.r(0x33, 2, 0, dst, left, right) }
func (a *Asm) Sltu(dst, left, right Reg) { a.r(0x33, 3, 0, dst, left, right) }
func (a *Asm) Sll(dst, left, right Reg)  { a.r(0x33, 1, 0, dst, left, right) }
func (a *Asm) Srl(dst, left, right Reg)  { a.r(0x33, 5, 0, dst, left, right) }
func (a *Asm) Sra(dst, left, right Reg)  { a.r(0x33, 5, 0x20, dst, left, right) }

// Word instructions sign-extend their result; shared i32 lowering must normalize.
func (a *Asm) Addw(dst, left, right Reg)  { a.r(0x3b, 0, 0, dst, left, right) }
func (a *Asm) Subw(dst, left, right Reg)  { a.r(0x3b, 0, 0x20, dst, left, right) }
func (a *Asm) Mulw(dst, left, right Reg)  { a.r(0x3b, 0, 1, dst, left, right) }
func (a *Asm) Divw(dst, left, right Reg)  { a.r(0x3b, 4, 1, dst, left, right) }
func (a *Asm) Divuw(dst, left, right Reg) { a.r(0x3b, 5, 1, dst, left, right) }
func (a *Asm) Remw(dst, left, right Reg)  { a.r(0x3b, 6, 1, dst, left, right) }
func (a *Asm) Remuw(dst, left, right Reg) { a.r(0x3b, 7, 1, dst, left, right) }
func (a *Asm) Sllw(dst, left, right Reg)  { a.r(0x3b, 1, 0, dst, left, right) }
func (a *Asm) Srlw(dst, left, right Reg)  { a.r(0x3b, 5, 0, dst, left, right) }
func (a *Asm) Sraw(dst, left, right Reg)  { a.r(0x3b, 5, 0x20, dst, left, right) }

func (a *Asm) Addi(dst, src Reg, imm int32)  { a.i(0x13, 0, dst, src, imm) }
func (a *Asm) Addiw(dst, src Reg, imm int32) { a.i(0x1b, 0, dst, src, imm) }
func (a *Asm) Xori(dst, src Reg, imm int32)  { a.i(0x13, 4, dst, src, imm) }
func (a *Asm) Sltiu(dst, src Reg, imm int32) { a.i(0x13, 3, dst, src, imm) }

const (
	FenceWrite  uint8 = 1
	FenceRead   uint8 = 2
	FenceOutput uint8 = 4
	FenceInput  uint8 = 8
)

// Fence emits the ordinary fm=0 memory/I/O ordering instruction. It neither
// changes GP registers nor substitutes for process-wide instruction-cache sync.
func (a *Asm) Fence(predecessor, successor uint8) {
	if predecessor > 15 || successor > 15 {
		a.fail("fence set outside four-bit field")
		return
	}
	a.word(0x0f | uint32(predecessor)<<24 | uint32(successor)<<20)
}

// Lui and Auipc take the unsigned encoded high-20 field; it is sign-extended
// after shifting by 12. They do not accept an unshifted 32-bit byte displacement.
func (a *Asm) Lui(dst Reg, imm20 uint32)   { a.u(0x37, dst, imm20) }
func (a *Asm) Auipc(dst Reg, imm20 uint32) { a.u(0x17, dst, imm20) }

func (a *Asm) shift(dst, src Reg, count uint8, right, arithmetic, word bool) {
	limit, op, f3, imm := uint8(64), uint32(0x13), uint32(1), int32(count)
	if word {
		limit, op = 32, 0x1b
	}
	if count >= limit {
		a.fail("shift count outside instruction width")
		return
	}
	if right {
		f3 = 5
	}
	if arithmetic {
		imm |= 0x400
	}
	a.i(op, f3, dst, src, imm)
}

func (a *Asm) Slli(dst, src Reg, count uint8)  { a.shift(dst, src, count, false, false, false) }
func (a *Asm) Srli(dst, src Reg, count uint8)  { a.shift(dst, src, count, true, false, false) }
func (a *Asm) Srai(dst, src Reg, count uint8)  { a.shift(dst, src, count, true, true, false) }
func (a *Asm) Slliw(dst, src Reg, count uint8) { a.shift(dst, src, count, false, false, true) }
func (a *Asm) Srliw(dst, src Reg, count uint8) { a.shift(dst, src, count, true, false, true) }
func (a *Asm) Sraiw(dst, src Reg, count uint8) { a.shift(dst, src, count, true, true, true) }

// ZeroExtend32 copies the low word and clears the upper word, even if dst==src.
func (a *Asm) ZeroExtend32(dst, src Reg) {
	if a.registers(dst, src) {
		a.Slli(dst, src, 32)
		a.Srli(dst, dst, 32)
	}
}

// Ld/Lwu/Lw and Sd/Sw require signed-12-bit byte offsets. Larger frame offsets
// must be materialized by the adapter into a permanently reserved address reg.
func (a *Asm) Ld(dst, base Reg, offset int32)  { a.i(0x03, 3, dst, base, offset) }
func (a *Asm) Lbu(dst, base Reg, offset int32) { a.i(0x03, 4, dst, base, offset) }
func (a *Asm) Lwu(dst, base Reg, offset int32) { a.i(0x03, 6, dst, base, offset) }
func (a *Asm) Lw(dst, base Reg, offset int32)  { a.i(0x03, 2, dst, base, offset) }

func (a *Asm) store(f3 uint32, src, base Reg, offset int32) {
	if !a.registers(src, base) {
		return
	}
	if offset < -2048 || offset > 2047 {
		a.fail("store offset outside signed 12-bit range")
		return
	}
	u := uint32(offset) & 0xfff
	if regallocCheckEnabled && a.err == nil && len(a.B)&3 == 0 {
		a.regallocStore(src, base, offset, f3)
	}
	a.word(0x23 | (u&31)<<7 | f3<<12 | uint32(base)<<15 | uint32(src)<<20 | (u>>5)<<25)
}

func (a *Asm) Sd(src, base Reg, offset int32)    { a.store(3, src, base, offset) }
func (a *Asm) Sw(src, base Reg, offset int32)    { a.store(2, src, base, offset) }
func (a *Asm) Sb(src, base Reg, offset int32)    { a.store(0, src, base, offset) }
func (a *Asm) Jalr(link, base Reg, offset int32) { a.i(0x67, 0, link, base, offset) }
func (a *Asm) Ret()                              { a.Jalr(Zero, RA, 0) }
func (a *Asm) Nop()                              { a.Addi(Zero, Zero, 0) }
