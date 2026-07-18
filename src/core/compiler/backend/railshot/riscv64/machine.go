//go:build riscv64

package riscv64

import (
	"encoding/binary"
	"fmt"

	rv "github.com/wago-org/wago/src/core/encoder/riscv64"
)

// machine is the RISC-V-specific instruction-selection boundary. Higher-level
// railshot files express wasm semantics through this type; only this file knows
// how those semantics expand into RV64 instructions. Keeping it here (rather
// than teaching the architectural encoder about flags or ARM addressing modes)
// makes every synthesized sequence visible and auditable as backend lowering.
type machine struct {
	rv.Asm
	DenseIdxDisp bool
	pending      pendingFlags
}

type pendingKind uint8

const (
	pendingNone pendingKind = iota
	pendingCmp
	pendingAdd
	pendingSub
	pendingFloat
)

type pendingFlags struct {
	kind        pendingKind
	left, right Reg
	result      Reg
	flag        Reg
	imm         uint64
	immediate   bool
	wide        bool
	f64         bool
}

const (
	addrScratch          = X16
	machineBranchScratch = X17
)

func imm32(v any) (int32, bool) {
	switch x := v.(type) {
	case int:
		return int32(x), int64(x) == int64(int32(x))
	case int32:
		return x, true
	case uint32:
		return int32(x), true
	case int64:
		return int32(x), x == int64(int32(x))
	case uint64:
		return int32(x), x <= 0xffffffff
	default:
		return 0, false
	}
}

func (a *machine) fail(op string) { panic("riscv64 backend: cannot encode " + op) }
func (a *machine) must(ok bool, op string) {
	if !ok {
		a.fail(op)
	}
}

func (a *machine) MovImm64(dst Reg, value uint64) { a.Asm.MovImm64(dst, value) }
func (a *machine) MovImm32(dst Reg, value any) {
	imm, ok := imm32(value)
	if !ok {
		a.fail("i32 immediate")
	}
	a.Asm.MovImm32(dst, imm)
}
func (a *machine) MovReg64(dst, src Reg) { a.Asm.MovReg64(dst, src) }
func (a *machine) MovReg32(dst, src Reg) { a.Zext32(dst, src) }

func (a *machine) Add64(dst, left, right Reg)  { a.Add(dst, left, right) }
func (a *machine) Add32(dst, left, right Reg)  { a.Addw(dst, left, right); a.Zext32(dst, dst) }
func (a *machine) Sub64(dst, left, right Reg)  { a.Sub(dst, left, right) }
func (a *machine) Sub32(dst, left, right Reg)  { a.Subw(dst, left, right); a.Zext32(dst, dst) }
func (a *machine) Mul64(dst, left, right Reg)  { a.Mul(dst, left, right) }
func (a *machine) Mul32(dst, left, right Reg)  { a.Mulw(dst, left, right); a.Zext32(dst, dst) }
func (a *machine) Sdiv64(dst, left, right Reg) { a.Div(dst, left, right) }
func (a *machine) Sdiv32(dst, left, right Reg) { a.Divw(dst, left, right); a.Zext32(dst, dst) }
func (a *machine) Udiv64(dst, left, right Reg) { a.Divu(dst, left, right) }
func (a *machine) Udiv32(dst, left, right Reg) { a.Divuw(dst, left, right); a.Zext32(dst, dst) }
func (a *machine) And64(dst, left, right Reg)  { a.And(dst, left, right) }
func (a *machine) And32(dst, left, right Reg) {
	a.And(dst, left, right)
	a.Zext32(dst, dst)
}
func (a *machine) Orr64(dst, left, right Reg) { a.Or(dst, left, right) }
func (a *machine) Orr32(dst, left, right Reg) {
	a.Or(dst, left, right)
	a.Zext32(dst, dst)
}
func (a *machine) Eor64(dst, left, right Reg) { a.Xor(dst, left, right) }
func (a *machine) Eor32(dst, left, right Reg) {
	a.Xor(dst, left, right)
	a.Zext32(dst, dst)
}

func (a *machine) addImm(dst, src Reg, value any, word bool) {
	imm, ok := imm32(value)
	if ok {
		if word && a.Addiw(dst, src, imm) {
			a.Zext32(dst, dst)
			return
		}
		if !word && a.Addi(dst, src, imm) {
			return
		}
	}
	a.MovImm64(addrScratch, uint64(int64(imm)))
	if word {
		a.Addw(dst, src, addrScratch)
		a.Zext32(dst, dst)
	} else {
		a.Add(dst, src, addrScratch)
	}
}
func (a *machine) AddImm64(dst, src Reg, value any) { a.addImm(dst, src, value, false) }
func (a *machine) AddImm32(dst, src Reg, value any) { a.addImm(dst, src, value, true) }
func (a *machine) SubImm64(dst, src Reg, value any) {
	imm, ok := imm32(value)
	if ok && imm != -2048 && a.Addi(dst, src, -imm) {
		return
	}
	a.MovImm64(addrScratch, uint64(int64(imm)))
	a.Sub(dst, src, addrScratch)
}
func (a *machine) SubImm32(dst, src Reg, value any) {
	imm, ok := imm32(value)
	if ok && imm != -2048 && a.Addiw(dst, src, -imm) {
		a.Zext32(dst, dst)
		return
	}
	a.MovImm64(addrScratch, uint64(int64(imm)))
	a.Subw(dst, src, addrScratch)
	a.Zext32(dst, dst)
}
func (a *machine) AndImm64(dst, src Reg, value uint64) bool {
	if int64(value) >= -2048 && int64(value) <= 2047 && a.Andi(dst, src, int32(value)) {
		return true
	}
	a.MovImm64(addrScratch, value)
	a.And(dst, src, addrScratch)
	return true
}
func (a *machine) OrrImm64(dst, src Reg, value uint64) bool {
	if int64(value) >= -2048 && int64(value) <= 2047 && a.Ori(dst, src, int32(value)) {
		return true
	}
	a.MovImm64(addrScratch, value)
	a.Or(dst, src, addrScratch)
	return true
}
func (a *machine) EorImm64(dst, src Reg, value uint64) bool {
	if int64(value) >= -2048 && int64(value) <= 2047 && a.Xori(dst, src, int32(value)) {
		return true
	}
	a.MovImm64(addrScratch, value)
	a.Xor(dst, src, addrScratch)
	return true
}
func (a *machine) AndImm32(dst, src Reg, value uint32) bool {
	a.MovImm32(addrScratch, value)
	a.And(dst, src, addrScratch)
	a.Zext32(dst, dst)
	return true
}
func (a *machine) OrrImm32(dst, src Reg, value uint32) bool {
	a.MovImm32(addrScratch, value)
	a.Or(dst, src, addrScratch)
	a.Zext32(dst, dst)
	return true
}
func (a *machine) EorImm32(dst, src Reg, value uint32) bool {
	a.MovImm32(addrScratch, value)
	a.Xor(dst, src, addrScratch)
	a.Zext32(dst, dst)
	return true
}

func (a *machine) Popcnt(dst, src Reg, word bool) {
	if word {
		a.Zext32(dst, src)
	} else {
		a.MovReg64(dst, src)
	}
	a.Srli(addrScratch, dst, 1)
	mask := uint64(0x5555555555555555)
	if word {
		mask = 0x55555555
	}
	a.MovImm64(machineBranchScratch, mask)
	a.And(addrScratch, addrScratch, machineBranchScratch)
	a.Sub(dst, dst, addrScratch)
	mask = 0x3333333333333333
	if word {
		mask = 0x33333333
	}
	a.MovImm64(machineBranchScratch, mask)
	a.And(addrScratch, dst, machineBranchScratch)
	a.Srli(dst, dst, 2)
	a.And(dst, dst, machineBranchScratch)
	a.Add(dst, dst, addrScratch)
	a.Srli(addrScratch, dst, 4)
	a.Add(dst, dst, addrScratch)
	mask = 0x0f0f0f0f0f0f0f0f
	if word {
		mask = 0x0f0f0f0f
	}
	a.MovImm64(machineBranchScratch, mask)
	a.And(dst, dst, machineBranchScratch)
	for _, shift := range []uint8{8, 16} {
		a.Srli(addrScratch, dst, shift)
		a.Add(dst, dst, addrScratch)
	}
	if !word {
		a.Srli(addrScratch, dst, 32)
		a.Add(dst, dst, addrScratch)
	}
	a.Andi(dst, dst, 0x7f)
}

func (a *machine) Clz(dst, src Reg, word bool) {
	if word {
		a.Zext32(dst, src)
	} else {
		a.MovReg64(dst, src)
	}
	for _, shift := range []uint8{1, 2, 4, 8, 16} {
		a.Srli(addrScratch, dst, shift)
		a.Or(dst, dst, addrScratch)
	}
	width := int32(32)
	if !word {
		a.Srli(addrScratch, dst, 32)
		a.Or(dst, dst, addrScratch)
		width = 64
	}
	a.Popcnt(dst, dst, word)
	a.MovSigned32(addrScratch, width)
	a.Sub(dst, addrScratch, dst)
}

func (a *machine) Ctz(dst, src Reg, word bool) {
	if word {
		a.Zext32(dst, src)
	} else {
		a.MovReg64(dst, src)
	}
	a.Sub(addrScratch, ZR, dst)
	a.And(dst, dst, addrScratch)
	a.Addi(dst, dst, -1)
	a.Popcnt(dst, dst, word)
}

func (a *machine) Neg32(dst, src Reg) { a.Subw(dst, ZR, src); a.Zext32(dst, dst) }
func (a *machine) Neg64(dst, src Reg) { a.Sub(dst, ZR, src) }
func (a *machine) Mvn32(dst, src Reg) {
	a.Xori(dst, src, -1)
	a.Zext32(dst, dst)
}
func (a *machine) Mvn64(dst, src Reg) { a.Xori(dst, src, -1) }

func (a *machine) Lslv32(dst, left, count Reg) { a.Sllw(dst, left, count); a.Zext32(dst, dst) }
func (a *machine) Lsrv32(dst, left, count Reg) { a.Srlw(dst, left, count); a.Zext32(dst, dst) }
func (a *machine) Asrv32(dst, left, count Reg) { a.Sraw(dst, left, count); a.Zext32(dst, dst) }
func (a *machine) Lslv64(dst, left, count Reg) { a.Sll(dst, left, count) }
func (a *machine) Lsrv64(dst, left, count Reg) { a.Srl(dst, left, count) }
func (a *machine) Asrv64(dst, left, count Reg) { a.Sra(dst, left, count) }
func (a *machine) LslImm(dst, src Reg, shift uint8, word bool) {
	if word {
		a.must(a.Slliw(dst, src, shift&31), "slliw")
		a.Zext32(dst, dst)
	} else {
		a.must(a.Slli(dst, src, shift&63), "slli")
	}
}
func (a *machine) LsrImm(dst, src Reg, shift uint8, word bool) {
	if word {
		a.must(a.Srliw(dst, src, shift&31), "srliw")
		a.Zext32(dst, dst)
	} else {
		a.must(a.Srli(dst, src, shift&63), "srli")
	}
}
func (a *machine) AsrImm(dst, src Reg, shift uint8, word bool) {
	if word {
		a.must(a.Sraiw(dst, src, shift&31), "sraiw")
		a.Zext32(dst, dst)
	} else {
		a.must(a.Srai(dst, src, shift&63), "srai")
	}
}
func (a *machine) Rorv32(dst, src, count Reg) {
	a.Srlw(machineBranchScratch, src, count)
	a.Subw(addrScratch, ZR, count)
	a.Sllw(addrScratch, src, addrScratch)
	a.Or(dst, machineBranchScratch, addrScratch)
	a.Zext32(dst, dst)
}
func (a *machine) Rorv64(dst, src, count Reg) {
	a.Srl(machineBranchScratch, src, count)
	a.Sub(addrScratch, ZR, count)
	a.Sll(addrScratch, src, addrScratch)
	a.Or(dst, machineBranchScratch, addrScratch)
}
func (a *machine) RorImm(dst, src Reg, shift uint8, word bool) {
	if word {
		shift &= 31
		a.Srliw(addrScratch, src, shift)
		a.Slliw(machineBranchScratch, src, (32-shift)&31)
		a.Or(dst, addrScratch, machineBranchScratch)
		a.Zext32(dst, dst)
	} else {
		shift &= 63
		a.Srli(addrScratch, src, shift)
		a.Slli(machineBranchScratch, src, (64-shift)&63)
		a.Or(dst, addrScratch, machineBranchScratch)
	}
}

func (a *machine) Smulh(dst, left, right Reg) { a.Mulh(dst, left, right) }
func (a *machine) Umulh(dst, left, right Reg) { a.Mulhu(dst, left, right) }
func (a *machine) Smull(dst, left, right Reg) {
	a.Sext32(addrScratch, left)
	a.Sext32(machineBranchScratch, right)
	a.Mul(dst, addrScratch, machineBranchScratch)
}
func (a *machine) Umull(dst, left, right Reg) {
	a.Zext32(addrScratch, left)
	a.Zext32(machineBranchScratch, right)
	a.Mul(dst, addrScratch, machineBranchScratch)
}
func (a *machine) Madd64(dst, left, right, addend Reg) {
	a.Mul(addrScratch, left, right)
	a.Add(dst, addrScratch, addend)
}
func (a *machine) Madd32(dst, left, right, addend Reg) {
	a.Mulw(addrScratch, left, right)
	a.Addw(dst, addrScratch, addend)
	a.Zext32(dst, dst)
}
func (a *machine) Msub64(dst, left, right, minuend Reg) {
	a.Mul(addrScratch, left, right)
	a.Sub(dst, minuend, addrScratch)
}
func (a *machine) Msub32(dst, left, right, minuend Reg) {
	a.Mulw(addrScratch, left, right)
	a.Subw(dst, minuend, addrScratch)
	a.Zext32(dst, dst)
}

func (a *machine) AddShifted(dst, left, right Reg, shift uint8, word bool) {
	if shift == 0 {
		if word {
			a.Addw(dst, left, right)
			a.Zext32(dst, dst)
		} else {
			a.Add(dst, left, right)
		}
		return
	}
	if word {
		a.Slliw(addrScratch, right, shift&31)
		a.Addw(dst, left, addrScratch)
		a.Zext32(dst, dst)
	} else {
		a.Slli(addrScratch, right, shift&63)
		a.Add(dst, left, addrScratch)
	}
}
func (a *machine) AddExtUXTW(dst, left, right Reg) {
	a.Zext32(addrScratch, right)
	a.Add(dst, left, addrScratch)
}
func (a *machine) Sxtw(dst, src Reg) { a.Sext32(dst, src) }
func (a *machine) Sxtb(dst, src Reg, word bool) {
	a.Sext8(dst, src)
	if word {
		a.Zext32(dst, dst)
	}
}
func (a *machine) Sxth(dst, src Reg, word bool) {
	a.Sext16(dst, src)
	if word {
		a.Zext32(dst, dst)
	}
}

func (a *machine) CmpReg64(left, right Reg) {
	a.pending = pendingFlags{kind: pendingCmp, left: left, right: right, wide: true}
}
func (a *machine) CmpReg32(left, right Reg) {
	a.pending = pendingFlags{kind: pendingCmp, left: left, right: right}
}
func (a *machine) CmpImm64(left Reg, value uint32) {
	a.pending = pendingFlags{kind: pendingCmp, left: left, imm: uint64(value), immediate: true, wide: true}
}
func (a *machine) CmpImm32(left Reg, value uint32) {
	a.pending = pendingFlags{kind: pendingCmp, left: left, imm: uint64(value), immediate: true}
}
func (a *machine) CmpSP64(right Reg) { a.CmpReg64(SP, right) }
func (a *machine) CmnImm64(left Reg, value uint32) {
	a.pending = pendingFlags{kind: pendingCmp, left: left, imm: uint64(-int64(value)), immediate: true, wide: true}
}
func (a *machine) CmnImm32(left Reg, value uint32) {
	a.pending = pendingFlags{kind: pendingCmp, left: left, imm: uint64(uint32(-int32(value))), immediate: true}
}
func (a *machine) Adds64(dst, left, right Reg) {
	preserved := left
	if preserved == dst {
		preserved = right
	}
	if preserved == dst {
		a.MovReg64(addrScratch, left)
		preserved = addrScratch
	}
	a.Add(dst, left, right)
	a.Sltu(machineBranchScratch, dst, preserved)
	a.pending = pendingFlags{kind: pendingAdd, left: left, right: right, result: dst, flag: machineBranchScratch, wide: true}
}
func (a *machine) Adds32(dst, left, right Reg) {
	preserved := left
	if preserved == dst {
		preserved = right
	}
	if preserved == dst {
		a.Zext32(addrScratch, left)
		preserved = addrScratch
	}
	a.Addw(dst, left, right)
	a.Zext32(dst, dst)
	if preserved != addrScratch {
		a.Zext32(addrScratch, preserved)
	}
	a.Zext32(machineBranchScratch, dst)
	a.Sltu(machineBranchScratch, machineBranchScratch, addrScratch)
	a.pending = pendingFlags{kind: pendingAdd, left: left, right: right, result: dst, flag: machineBranchScratch}
}
func (a *machine) Subs64(dst, left, right Reg) {
	a.Sub(dst, left, right)
	a.pending = pendingFlags{kind: pendingSub, left: left, right: right, result: dst, wide: true}
}
func (a *machine) Subs32(dst, left, right Reg) {
	a.Subw(dst, left, right)
	a.Zext32(dst, dst)
	a.pending = pendingFlags{kind: pendingSub, left: left, right: right, result: dst}
}
func (a *machine) SubsImm64(dst, left Reg, value uint32) {
	a.MovImm64(addrScratch, uint64(value))
	a.Subs64(dst, left, addrScratch)
}

func (a *machine) canonicalPair(p pendingFlags, unsigned bool) (Reg, Reg) {
	right := p.right
	if p.immediate {
		if p.wide {
			a.MovImm64(machineBranchScratch, p.imm)
		} else if unsigned {
			a.MovImm64(machineBranchScratch, uint64(uint32(p.imm)))
		} else {
			a.MovSigned32(machineBranchScratch, int32(p.imm))
		}
		right = machineBranchScratch
	}
	if p.wide {
		return p.left, right
	}
	// Canonicalize the right side first in case it currently occupies T5.
	if unsigned {
		a.Zext32(machineBranchScratch, right)
		a.Zext32(addrScratch, p.left)
	} else {
		a.Sext32(machineBranchScratch, right)
		a.Sext32(addrScratch, p.left)
	}
	return addrScratch, machineBranchScratch
}

func (a *machine) integerCond(dst Reg, c Cond) {
	p := a.pending
	if p.kind == pendingNone {
		a.fail("condition without compare")
	}
	if p.kind == pendingFloat {
		// RV64 has no flags, but floating min/max lowering intentionally consumes
		// the same comparison more than once (unordered, then relation). Keep the
		// source pair until the next explicit compare overwrites it.
		a.floatCond(dst, c, p)
		return
	}
	// Keep the relation live until the next explicit compare/flag-setting op.
	// The architecture-parallel control lowering may branch more than once from
	// one compare (for example overflow/range forks), matching a real flags ISA.

	if p.kind == pendingAdd {
		res := p.result
		switch c {
		case condE:
			a.Seqz(dst, res)
		case condNE:
			a.Snez(dst, res)
		case condS:
			a.Slt(dst, res, ZR)
		case condNS:
			a.Slt(dst, res, ZR)
			a.Xori(dst, dst, 1)
		case condB, condAE: // carry clear/set
			a.MovReg64(dst, p.flag)
			if c == condB {
				a.Xori(dst, dst, 1)
			}
		case condVS, condVC:
			// signed overflow: ((left^result) & (right^result)) < 0
			a.Xor(addrScratch, p.left, res)
			a.Xor(machineBranchScratch, p.right, res)
			a.And(dst, addrScratch, machineBranchScratch)
			a.Slt(dst, dst, ZR)
			if c == condVC {
				a.Xori(dst, dst, 1)
			}
		default:
			a.fail("unsupported add flags")
		}
		return
	}

	if p.kind == pendingSub {
		// A destructive SUB overwrites its left operand on RV64, unlike a flags
		// register which retains Z/N independently. Conditions derived only from
		// the arithmetic result must therefore use p.result directly. In
		// particular, counted loops commonly emit SUBS count,count,1 followed by
		// B.NE; reconstructing that relation from the now-overwritten left operand
		// would make zero compare unequal to one and run past the destination.
		switch c {
		case condE:
			a.Seqz(dst, p.result)
			return
		case condNE:
			a.Snez(dst, p.result)
			return
		case condS, condNS:
			res := p.result
			if !p.wide {
				a.Sext32(addrScratch, res)
				res = addrScratch
			}
			a.Slt(dst, res, ZR)
			if c == condNS {
				a.Xori(dst, dst, 1)
			}
			return
		}
	}

	unsigned := c == condB || c == condAE || c == condBE || c == condA
	left, right := a.canonicalPair(p, unsigned)
	switch c {
	case condE:
		a.Xor(dst, left, right)
		a.Seqz(dst, dst)
	case condNE:
		a.Xor(dst, left, right)
		a.Snez(dst, dst)
	case condB:
		a.Sltu(dst, left, right)
	case condAE:
		a.Sltu(dst, left, right)
		a.Xori(dst, dst, 1)
	case condBE:
		a.Sltu(dst, right, left)
		a.Xori(dst, dst, 1)
	case condA:
		a.Sltu(dst, right, left)
	case condL:
		a.Slt(dst, left, right)
	case condGE:
		a.Slt(dst, left, right)
		a.Xori(dst, dst, 1)
	case condLE:
		a.Slt(dst, right, left)
		a.Xori(dst, dst, 1)
	case condG:
		a.Slt(dst, right, left)
	case condS:
		a.Sub(addrScratch, left, right)
		a.Slt(dst, addrScratch, ZR)
	case condNS:
		a.Sub(addrScratch, left, right)
		a.Slt(dst, addrScratch, ZR)
		a.Xori(dst, dst, 1)
	case condVS, condVC:
		// subtraction overflow: ((left^right) & (left^result)) < 0
		res := p.result
		if p.kind == pendingCmp {
			a.Sub(addrScratch, left, right)
			res = addrScratch
		}
		a.Xor(addrScratch, left, right)
		a.Xor(machineBranchScratch, left, res)
		a.And(dst, addrScratch, machineBranchScratch)
		a.Slt(dst, dst, ZR)
		if c == condVC {
			a.Xori(dst, dst, 1)
		}
	default:
		a.fail("integer condition")
	}
}

func (a *machine) Cset32(dst Reg, c Cond)                   { a.integerCond(dst, c) }
func (a *machine) Cset64(dst Reg, c Cond)                   { a.integerCond(dst, c) }
func (a *machine) Csel32(dst, yes, no Reg, c Cond)          { a.csel(dst, yes, no, c, false) }
func (a *machine) Csel64(dst, yes, no Reg, c Cond)          { a.csel(dst, yes, no, c, true) }
func (a *machine) Csel(dst, yes, no Reg, c Cond, wide bool) { a.csel(dst, yes, no, c, wide) }
func (a *machine) csel(dst, yes, no Reg, c Cond, wide bool) {
	a.integerCond(addrScratch, c)
	take := a.FarBcond(addrScratch, ZR, rv.CondNE, machineBranchScratch)
	if wide {
		a.MovReg64(dst, no)
	} else {
		a.MovReg32(dst, no)
	}
	done := a.FarJump(ZR, machineBranchScratch)
	trueAt := a.Len()
	if wide {
		a.MovReg64(dst, yes)
	} else {
		a.MovReg32(dst, yes)
	}
	a.must(a.PatchFarBranch(take, trueAt), "csel true")
	a.must(a.PatchFarJump(done, a.Len()), "csel end")
}

func (a *machine) Bcond(c Cond) int {
	a.integerCond(addrScratch, c)
	return a.FarBcond(addrScratch, ZR, rv.CondNE, machineBranchScratch)
}
func (a *machine) Cbz64(value Reg) int { return a.FarBcond(value, ZR, rv.CondEQ, machineBranchScratch) }
func (a *machine) Cbnz64(value Reg) int {
	return a.FarBcond(value, ZR, rv.CondNE, machineBranchScratch)
}
func (a *machine) Branch() int    { return a.FarJump(ZR, machineBranchScratch) }
func (a *machine) Bl() int        { return a.FarCall(machineBranchScratch) }
func (a *machine) Blr(target Reg) { a.Jalr(LR, target, 0) }
func (a *machine) Br(target Reg)  { a.Jalr(ZR, target, 0) }
func (a *machine) PatchBranch19(at, target int) bool {
	return a.PatchFarBranch(at, target)
}
func (a *machine) PatchBranch26(at, target int) { a.must(a.PatchFarJump(at, target), "far jump/call") }

func (a *machine) addr(base Reg, off int32, avoid Reg) (Reg, int32, bool) {
	if off >= -2048 && off <= 2047 {
		return base, off, false
	}
	s := addrScratch
	if s == base || s == avoid {
		s = machineBranchScratch
	}
	spillRA := false
	if s == base || s == avoid {
		// Both fixed temporaries are live. Preserve the wasm return address in one
		// aligned foreign-stack slot and use RA only for this single memory op.
		a.must(a.Addi(SP, SP, -16), "address RA spill frame")
		a.must(a.Sd(LR, SP, 0), "address RA spill")
		s = LR
		spillRA = true
	}
	a.MovImm64(s, uint64(int64(off)))
	a.Add(s, base, s)
	return s, 0, spillRA
}
func (a *machine) finishAddr(spillRA bool) {
	if spillRA {
		a.must(a.Ld(LR, SP, 0), "address RA restore")
		a.must(a.Addi(SP, SP, 16), "address RA spill frame restore")
	}
}
func (a *machine) Load64(dst, base Reg, value any) {
	off, ok := imm32(value)
	if !ok {
		a.fail("load64 offset")
	}
	base, off, spill := a.addr(base, off, dst)
	a.must(a.Ld(dst, base, off), "ld")
	a.finishAddr(spill)
}
func (a *machine) Load32(dst, base Reg, value any) {
	off, ok := imm32(value)
	if !ok {
		a.fail("load32 offset")
	}
	base, off, spill := a.addr(base, off, dst)
	a.must(a.Lwu(dst, base, off), "lwu")
	a.finishAddr(spill)
}
func (a *machine) Load32S(dst, base Reg, value any) {
	off, ok := imm32(value)
	if !ok {
		a.fail("load32s offset")
	}
	base, off, spill := a.addr(base, off, dst)
	a.must(a.Lw(dst, base, off), "lw")
	a.finishAddr(spill)
}
func (a *machine) Load32U(dst, base Reg, value any) {
	off, ok := imm32(value)
	if !ok {
		a.fail("load32u offset")
	}
	base, off, spill := a.addr(base, off, dst)
	a.must(a.Lwu(dst, base, off), "lwu")
	a.finishAddr(spill)
}
func (a *machine) Load16(dst, base Reg, off int32) {
	base, off, spill := a.addr(base, off, dst)
	a.must(a.Lhu(dst, base, off), "lhu")
	a.finishAddr(spill)
}
func (a *machine) Load16S(dst, base Reg, off int32, word bool) {
	base, off, spill := a.addr(base, off, dst)
	a.must(a.Lh(dst, base, off), "lh")
	a.finishAddr(spill)
	if word {
		a.Zext32(dst, dst)
	}
}
func (a *machine) Load8(dst, base Reg, off int32) {
	base, off, spill := a.addr(base, off, dst)
	a.must(a.Lbu(dst, base, off), "lbu")
	a.finishAddr(spill)
}
func (a *machine) Load8S(dst, base Reg, off int32, word bool) {
	base, off, spill := a.addr(base, off, dst)
	a.must(a.Lb(dst, base, off), "lb")
	a.finishAddr(spill)
	if word {
		a.Zext32(dst, dst)
	}
}
func (a *machine) Store64(src, base Reg, value any) {
	off, ok := imm32(value)
	if !ok {
		a.fail("store64 offset")
	}
	base, off, spill := a.addr(base, off, src)
	a.must(a.Sd(src, base, off), "sd")
	a.finishAddr(spill)
}
func (a *machine) Store32(src, base Reg, value any) {
	off, ok := imm32(value)
	if !ok {
		a.fail("store32 offset")
	}
	base, off, spill := a.addr(base, off, src)
	a.must(a.Sw(src, base, off), "sw")
	a.finishAddr(spill)
}
func (a *machine) Store16(src, base Reg, off int32) {
	base, off, spill := a.addr(base, off, src)
	a.must(a.Sh(src, base, off), "sh")
	a.finishAddr(spill)
}
func (a *machine) Store8(src, base Reg, off int32) {
	base, off, spill := a.addr(base, off, src)
	a.must(a.Sb(src, base, off), "sb")
	a.finishAddr(spill)
}
func (a *machine) LoadIdx(dst, base, index Reg, disp int32, size int, signed, wideDest bool) {
	address := addrScratch
	if address == base || address == index {
		// The destination may double as the effective-address temporary: every
		// RISC-V load reads rs1 before writing rd. This is essential for br_table,
		// whose table base intentionally lives in T5 (addrScratch); overwriting T5
		// with base+index would lose the base needed to add the loaded table offset.
		address = dst
		if address == base || address == index {
			address = machineBranchScratch
		}
	}
	a.Add(address, base, index)
	addr, off, spill := a.addr(address, disp, dst)
	switch size {
	case 1:
		if signed {
			a.Load8S(dst, addr, off, !wideDest)
		} else {
			a.Load8(dst, addr, off)
		}
	case 2:
		if signed {
			a.Load16S(dst, addr, off, !wideDest)
		} else {
			a.Load16(dst, addr, off)
		}
	case 4:
		if signed && wideDest {
			a.Load32S(dst, addr, off)
		} else {
			a.Load32U(dst, addr, off)
		}
	case 8:
		a.Load64(dst, addr, off)
	default:
		a.fail("indexed load size")
	}
	a.finishAddr(spill)
}
func (a *machine) StoreIdx(base, index, src Reg, disp int32, size int) {
	a.Add(addrScratch, base, index)
	addr, off, spill := a.addr(addrScratch, disp, src)
	switch size {
	case 1:
		a.Store8(src, addr, off)
	case 2:
		a.Store16(src, addr, off)
	case 4:
		a.Store32(src, addr, off)
	case 8:
		a.Store64(src, addr, off)
	default:
		a.fail("indexed store size")
	}
	a.finishAddr(spill)
}
func (a *machine) StoreImmIdx(base, index Reg, disp, value int32, size int) {
	a.Add(addrScratch, base, index)
	addr, off, spill := a.addr(addrScratch, disp, machineBranchScratch)
	a.MovSigned32(machineBranchScratch, value)
	switch size {
	case 1:
		a.Store8(machineBranchScratch, addr, off)
	case 2:
		a.Store16(machineBranchScratch, addr, off)
	case 4:
		a.Store32(machineBranchScratch, addr, off)
	case 8:
		a.Store64(machineBranchScratch, addr, off)
	default:
		a.fail("indexed immediate store size")
	}
	a.finishAddr(spill)
}

func (a *machine) LeaSP(dst Reg, off int32) { a.AddImm64(dst, SP, off) }
func (a *machine) SubSPReg(r Reg)           { a.Sub(SP, SP, r) }
func (a *machine) AddSPReg(r Reg)           { a.Add(SP, SP, r) }
func (a *machine) SubSP64(value any)        { a.SubImm64(SP, SP, value) }
func (a *machine) AddSP64(value any)        { a.AddImm64(SP, SP, value) }
func (a *machine) StpPre(first, second, base Reg, off int32) {
	a.AddImm64(base, base, off)
	a.Store64(first, base, 0)
	a.Store64(second, base, 8)
}
func (a *machine) LdpPost(first, second, base Reg, off int32) {
	a.Load64(first, base, 0)
	a.Load64(second, base, 8)
	a.AddImm64(base, base, off)
}

func (a *machine) Movz64(dst Reg, _ uint16, _ uint32) { a.Lui(dst, 0) }
func (a *machine) Movk64(dst Reg, _ uint16, _ uint32) { a.Addi(dst, dst, 0) }
func (a *machine) PatchStackAdjust(at int, size int32, subtract bool) {
	var p rv.Asm
	if size != 0 {
		if subtract {
			size = -size
		}
		p.Addi(SP, SP, size)
	} else {
		p.Nop()
	}
	a.PatchU32(at, binary.LittleEndian.Uint32(p.B))
	var nop rv.Asm
	nop.Nop()
	n := binary.LittleEndian.Uint32(nop.B)
	a.PatchU32(at+4, n)
	a.PatchU32(at+8, n)
}

func (a *machine) PatchMovImm(at int, value uint32) {
	d := int64(value)
	hi := (d + 0x800) >> 12
	lo := d - hi<<12
	if hi < -(1<<19) || hi >= 1<<19 || lo < -2048 || lo > 2047 {
		a.fail("frame size placeholder")
	}
	// LUI immediate occupies bits 31:12; ADDI immediate occupies bits 31:20.
	w0 := uint32(a.B[at]) | uint32(a.B[at+1])<<8 | uint32(a.B[at+2])<<16 | uint32(a.B[at+3])<<24
	w1 := uint32(a.B[at+4]) | uint32(a.B[at+5])<<8 | uint32(a.B[at+6])<<16 | uint32(a.B[at+7])<<24
	w0 = (w0 & 0x00000fff) | (uint32(hi)&0xfffff)<<12
	w1 = (w1 & 0x000fffff) | (uint32(lo)&0xfff)<<20
	for i, w := range []uint32{w0, w1} {
		p := at + i*4
		a.B[p] = byte(w)
		a.B[p+1] = byte(w >> 8)
		a.B[p+2] = byte(w >> 16)
		a.B[p+3] = byte(w >> 24)
	}
}

func (a *machine) FandBits(dst, left, right Reg, f64 bool) {
	a.FmvToGPR(addrScratch, left, f64)
	a.FmvToGPR(machineBranchScratch, right, f64)
	a.And(addrScratch, addrScratch, machineBranchScratch)
	a.FmvFromGPR(dst, addrScratch, f64)
}
func (a *machine) ForBits(dst, left, right Reg, f64 bool) {
	a.FmvToGPR(addrScratch, left, f64)
	a.FmvToGPR(machineBranchScratch, right, f64)
	a.Or(addrScratch, addrScratch, machineBranchScratch)
	a.FmvFromGPR(dst, addrScratch, f64)
}
func (a *machine) Fcopysign(dst, magnitude, sign Reg, f64 bool) {
	a.Asm.Fsgnj(dst, magnitude, sign, f64)
}

func (a *machine) FmovFromGpr(dst, src Reg, f64 bool) { a.FmvFromGPR(dst, src, f64) }
func (a *machine) FmovToGpr(dst, src Reg, f64 bool)   { a.FmvToGPR(dst, src, f64) }
func (a *machine) FMov(dst, src Reg, f64 bool)        { a.FmovReg(dst, src, f64) }
func (a *machine) Fadd(dst, left, right Reg, f64 bool) {
	a.must(a.Asm.Fadd(dst, left, right, f64, rv.RoundNearestEven), "fadd")
}
func (a *machine) Fsub(dst, left, right Reg, f64 bool) {
	a.must(a.Asm.Fsub(dst, left, right, f64, rv.RoundNearestEven), "fsub")
}
func (a *machine) Fmul(dst, left, right Reg, f64 bool) {
	a.must(a.Asm.Fmul(dst, left, right, f64, rv.RoundNearestEven), "fmul")
}
func (a *machine) Fdiv(dst, left, right Reg, f64 bool) {
	a.must(a.Asm.Fdiv(dst, left, right, f64, rv.RoundNearestEven), "fdiv")
}
func (a *machine) Fsqrt(dst, src Reg, f64 bool) {
	a.must(a.Asm.Fsqrt(dst, src, f64, rv.RoundNearestEven), "fsqrt")
}
func (a *machine) Frint(dst, src Reg, f64 bool, mode byte) {
	rm := rv.RoundNearestEven
	switch mode {
	case roundFloor:
		rm = rv.RoundDown
	case roundCeil:
		rm = rv.RoundUp
	case roundTrunc:
		rm = rv.RoundTowardZero
	case roundNearest:
		rm = rv.RoundNearestEven
	default:
		a.fail("float rounding mode")
	}
	// Every finite non-integral f32/f64 value fits a signed i64. Values whose
	// exponent is at or above the mantissa width (including infinities and NaNs)
	// are already integral or must be preserved bit-for-bit.
	a.FmvToGPR(addrScratch, src, f64)
	threshold := uint64(150) // f32: bias 127 + 23 fraction bits
	if f64 {
		a.Srli(machineBranchScratch, addrScratch, 52)
		a.Andi(machineBranchScratch, machineBranchScratch, 0x7ff)
		threshold = 1075 // f64: bias 1023 + 52 fraction bits
	} else {
		a.Srli(machineBranchScratch, addrScratch, 23)
		a.Andi(machineBranchScratch, machineBranchScratch, 0xff)
	}
	a.MovImm64(addrScratch, threshold)
	preserve := a.FarBcond(machineBranchScratch, addrScratch, rv.CondGEU, machineBranchScratch)
	a.FcvtFloatToInt(addrScratch, src, f64, false, true, rm)
	// Integer conversion would turn a negative rounded zero into +0. Test the
	// integer result before overwriting dst: src and dst may alias, so the original
	// sign must be captured while src still contains the input lane.
	nonzero := a.FarBcond(addrScratch, ZR, rv.CondNE, machineBranchScratch)
	a.FmvToGPR(machineBranchScratch, src, f64)
	if f64 {
		a.Srli(machineBranchScratch, machineBranchScratch, 63)
		a.Slli(machineBranchScratch, machineBranchScratch, 63)
	} else {
		a.Srli(machineBranchScratch, machineBranchScratch, 31)
		a.Slli(machineBranchScratch, machineBranchScratch, 31)
	}
	a.FcvtIntToFloat(dst, addrScratch, f64, false, true, rv.RoundNearestEven)
	a.FmvToGPR(addrScratch, dst, f64)
	a.Or(addrScratch, addrScratch, machineBranchScratch)
	a.FmvFromGPR(dst, addrScratch, f64)
	zeroDone := a.FarJump(ZR, machineBranchScratch)

	nonzeroAt := a.Len()
	a.FcvtIntToFloat(dst, addrScratch, f64, false, true, rv.RoundNearestEven)
	nonzeroDone := a.FarJump(ZR, machineBranchScratch)
	a.must(a.PatchFarBranch(nonzero, nonzeroAt), "frint nonzero")

	preserveAt := a.Len()
	// FMIN x,x preserves finite values, infinities, and signed zero while
	// quieting signaling NaNs to an arithmetic NaN as WebAssembly requires.
	a.Asm.Fmin(dst, src, src, f64)
	a.must(a.PatchFarBranch(preserve, preserveAt), "frint preserve")
	end := a.Len()
	a.must(a.PatchFarJump(zeroDone, end), "frint zero end")
	a.must(a.PatchFarJump(nonzeroDone, end), "frint nonzero end")
}
func (a *machine) Fcmp(left, right Reg, f64 bool) {
	a.pending = pendingFlags{kind: pendingFloat, left: left, right: right, f64: f64}
}
func (a *machine) floatCond(dst Reg, c Cond, p pendingFlags) {
	switch c {
	case condE:
		a.Feq(dst, p.left, p.right, p.f64)
	case condNE:
		a.Feq(dst, p.left, p.right, p.f64)
		a.Xori(dst, dst, 1)
	case condL, condS:
		a.Flt(dst, p.left, p.right, p.f64)
	case condLE:
		a.Fle(dst, p.left, p.right, p.f64)
	case condG:
		a.Flt(dst, p.right, p.left, p.f64)
	case condGE, condNS:
		a.Fle(dst, p.right, p.left, p.f64)
	case condVS, condVC:
		a.Fclass(addrScratch, p.left, p.f64)
		a.Fclass(machineBranchScratch, p.right, p.f64)
		a.Or(dst, addrScratch, machineBranchScratch)
		a.Andi(dst, dst, 0x300)
		a.Snez(dst, dst)
		if c == condVC {
			a.Xori(dst, dst, 1)
		}
	default:
		a.fail("floating condition")
	}
}
func (a *machine) Fmin(dst, left, right Reg, f64 bool) { a.Asm.Fmin(dst, left, right, f64) }
func (a *machine) Fmax(dst, left, right Reg, f64 bool) { a.Asm.Fmax(dst, left, right, f64) }
func (a *machine) Fabs(dst, src Reg, f64 bool)         { a.Asm.Fabs(dst, src, f64) }
func (a *machine) Fneg(dst, src Reg, f64 bool)         { a.Asm.Fneg(dst, src, f64) }
func (a *machine) FcvtS2D(dst, src Reg)                { a.Asm.FcvtS2D(dst, src, rv.RoundNearestEven) }
func (a *machine) FcvtD2S(dst, src Reg)                { a.Asm.FcvtD2S(dst, src, rv.RoundNearestEven) }
func (a *machine) Fcvtzs(dst, src Reg, f64src, dstWide bool) {
	a.FcvtFloatToInt(dst, src, f64src, false, dstWide, rv.RoundTowardZero)
}
func (a *machine) Scvtf(dst, src Reg, f64dst, srcWide bool) {
	a.FcvtIntToFloat(dst, src, f64dst, false, srcWide, rv.RoundNearestEven)
}
func (a *machine) Ucvtf(dst, src Reg, f64dst, srcWide bool) {
	a.FcvtIntToFloat(dst, src, f64dst, true, srcWide, rv.RoundNearestEven)
}
func (a *machine) LdrF(dst, base Reg, off int32, f64 bool) {
	base, off, spill := a.addr(base, off, dst)
	a.must(a.FLoad(dst, base, off, f64), "float load")
	a.finishAddr(spill)
}
func (a *machine) StrF(base Reg, off int32, src Reg, f64 bool) {
	base, off, spill := a.addr(base, off, src)
	a.must(a.FStore(src, base, off, f64), "float store")
	a.finishAddr(spill)
}
func (a *machine) FLoadDisp(dst, base Reg, off int32, f64 bool) { a.LdrF(dst, base, off, f64) }
func (a *machine) FStoreDisp(base Reg, off int32, src Reg, f64 bool) {
	a.StrF(base, off, src, f64)
}
func (a *machine) LdrS(dst, base Reg, off int32)     { a.LdrF(dst, base, off, false) }
func (a *machine) LdrD(dst, base Reg, off int32)     { a.LdrF(dst, base, off, true) }
func (a *machine) StrS(base Reg, off int32, src Reg) { a.StrF(base, off, src, false) }
func (a *machine) StrD(base Reg, off int32, src Reg) { a.StrF(base, off, src, true) }
func (a *machine) LdrFIdx(dst, base, index Reg, disp int32, f64 bool) {
	a.Add(addrScratch, base, index)
	a.LdrF(dst, addrScratch, disp, f64)
}
func (a *machine) StrFIdx(base, index, src Reg, disp int32, f64 bool) {
	a.Add(addrScratch, base, index)
	a.StrF(addrScratch, disp, src, f64)
}

func (a *machine) Ldur64(dst, base Reg, off int32)     { a.Load64(dst, base, off) }
func (a *machine) Ldur32(dst, base Reg, off int32)     { a.Load32(dst, base, off) }
func (a *machine) Stur64(src, base Reg, off int32)     { a.Store64(src, base, off) }
func (a *machine) Stur32(src, base Reg, off int32)     { a.Store32(src, base, off) }
func (a *machine) Ldrb(dst, base Reg, off uint32) bool { a.Load8(dst, base, int32(off)); return true }
func (a *machine) Strb(src, base Reg, off uint32) bool { a.Store8(src, base, int32(off)); return true }
func (a *machine) Ldrh(dst, base Reg, off uint32) bool { a.Load16(dst, base, int32(off)); return true }
func (a *machine) Strh(src, base Reg, off uint32) bool { a.Store16(src, base, int32(off)); return true }

func (a *machine) VMovdquLoadDisp(_, _ Reg, _ int32) {
	panic("riscv64: SIMD reached scalar backend")
}
func (a *machine) VMovdquStoreDisp(_ Reg, _ int32, _ Reg) {
	panic("riscv64: SIMD reached scalar backend")
}
func (a *machine) LdrQ(_ Reg, _ Reg, _ int32) { panic("riscv64: SIMD reached scalar backend") }
func (a *machine) StrQ(_ Reg, _ int32, _ Reg) { panic("riscv64: SIMD reached scalar backend") }
func (a *machine) NeonEor16b(_, _, _ Reg)     { panic("riscv64: SIMD reached scalar backend") }
func (a *machine) NeonMov16b(_, _ Reg)        { panic("riscv64: SIMD reached scalar backend") }
func (a *machine) NeonInsD(_, _ Reg, _ byte)  { panic("riscv64: SIMD reached scalar backend") }

func (a *machine) String() string { return fmt.Sprintf("rv64 machine: %d bytes", len(a.B)) }
