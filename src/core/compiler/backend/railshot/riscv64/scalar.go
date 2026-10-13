//go:build linux && !tinygo

// Package riscv64 is an experimental integer/control compiler. It uses shared
// scalar compilation and genuine RV64 whole-function lowering, without a public
// Wago target or non-RV machine-code fallback.
package riscv64

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

var scalarRegisters = [...]uint8{5, 6, 7, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 28, 29}

type target struct {
	regallocFnState
	a        enc.Asm
	regs     []uint8
	nlocals  int
	header   int
	err      error
	mode     emissionMode
	sourcePC uint32
}

type emissionMode struct {
	profileMode
	production        bool
	interruptible     bool
	invocationContext bool
	// The runtime authenticates and retains canonical foreign descriptors at
	// entry/resume and provides a synchronous root control frame.
	foreignTableCalls bool
	// Imported descriptors have independently checked bounded runtime capacity.
	borrowedTable                  bool
	localTailCalls                 bool
	importTailCalls                bool
	dynamicTailCalls               bool
	referenceCalls                 bool
	gcTypeSubtypingRuntimeCallCast bool
	// Private same-image direct calls only; never source/artifact admission.
	referenceFunctions     bool
	tailPlans              *[]tailImportPlan
	tailSiteBase           uint32
	localMutableReferences bool
	// Only an exact public profile may transfer a local descriptor through the
	// parked host bridge. Other confined mutable reference cells remain direct
	// instance-local storage even though they use the same carrier class.
	parkedMutableReferenceGlobals bool
	functionIndex                 uint32
	entryPC                       uint32
	gcFrameRoots                  *shared.GCFrameRootPlan
}

var _ shared.ScalarTarget = (*target)(nil)

func (f *target) Registers() ([]uint8, uint64)      { return f.regs, enc.ReservedMask }
func (*target) ParameterRegister(int) (uint8, bool) { return 0, false }
func (f *target) frameHeader() int {
	if f.header == 0 {
		return 16
	}
	return f.header
}
func (f *target) LocalOffset(i int) int32 { return int32(f.frameHeader() + 8*i) }
func (f *target) SpillOffset(i int) int32 { return int32(f.frameHeader() + 8*f.nlocals + 8*i) }
func (f *target) Constant(r uint8, v int64, w bool) {
	if w {
		f.a.Constant64(enc.Reg(r), v)
	} else {
		f.a.Constant32(enc.Reg(r), uint32(v))
	}
}
func (f *target) Move(d, s uint8, w bool) {
	if !w {
		f.a.ZeroExtend32(enc.Reg(d), enc.Reg(s))
	} else if d != s {
		f.a.Addi(enc.Reg(d), enc.Reg(s), 0)
	}
}
func (f *target) NormalizeI32(r uint8) { f.a.ZeroExtend32(enc.Reg(r), enc.Reg(r)) }
func (f *target) address(off int32) (enc.Reg, int32) {
	if off >= -2048 && off <= 2047 {
		return enc.SP, off
	}
	f.a.Constant64(enc.Scratch1, int64(off))
	f.a.Add(enc.Scratch1, enc.SP, enc.Scratch1)
	return enc.Scratch1, 0
}
func (f *target) Load(r uint8, off int32, w bool) {
	base, off := f.address(off)
	if w {
		f.a.Ld(enc.Reg(r), base, off)
	} else {
		f.a.Lwu(enc.Reg(r), base, off)
	}
}
func (f *target) Store(off int32, r uint8, w bool) {
	base, off := f.address(off)
	if w {
		f.a.Sd(enc.Reg(r), base, off)
	} else {
		f.a.Sw(enc.Reg(r), base, off)
	}
}
func (*target) Clobbers(shared.IntOp) uint64 { return 1<<enc.Scratch0 | 1<<enc.Scratch1 }
func (*target) Operand(_ shared.IntOp, _ bool, o shared.ScalarOperand) bool {
	return o.Kind == shared.ScalarRegister
}
func (*target) ScaledAdd(bool, uint8, uint8, uint8, uint8) bool { return false }

// Prepare both operands before writing the destination, preserving all aliases
// and raw input upper bits. Signed low-word comparisons use ADDIW sign extension.
func (f *target) compareOperands(op shared.IntOp, w bool, l, r enc.Reg) (enc.Reg, enc.Reg) {
	if !w {
		if op == shared.IntLtS || op == shared.IntGtS || op == shared.IntLeS || op == shared.IntGeS {
			f.a.Addiw(enc.Scratch0, l, 0)
			f.a.Addiw(enc.Scratch1, r, 0)
		} else {
			f.a.ZeroExtend32(enc.Scratch0, l)
			f.a.ZeroExtend32(enc.Scratch1, r)
		}
		return enc.Scratch0, enc.Scratch1
	}
	return l, r
}
func (f *target) Binary(op shared.IntOp, w bool, d, l uint8, o shared.ScalarOperand) {
	if o.Kind != shared.ScalarRegister {
		f.err = fmt.Errorf("rv64: nonregister scalar operand")
		return
	}
	dst, left, right := enc.Reg(d), enc.Reg(l), enc.Reg(o.Reg)
	if op >= shared.IntEq && op <= shared.IntGeU {
		left, right = f.compareOperands(op, w, left, right)
		switch op {
		case shared.IntEq:
			f.a.Xor(dst, left, right)
			f.a.Sltiu(dst, dst, 1)
		case shared.IntNe:
			f.a.Xor(dst, left, right)
			f.a.Sltu(dst, enc.Zero, dst)
		case shared.IntLtS:
			f.a.Slt(dst, left, right)
		case shared.IntLtU:
			f.a.Sltu(dst, left, right)
		case shared.IntGtS:
			f.a.Slt(dst, right, left)
		case shared.IntGtU:
			f.a.Sltu(dst, right, left)
		case shared.IntLeS:
			f.a.Slt(dst, right, left)
			f.a.Xori(dst, dst, 1)
		case shared.IntLeU:
			f.a.Sltu(dst, right, left)
			f.a.Xori(dst, dst, 1)
		case shared.IntGeS:
			f.a.Slt(dst, left, right)
			f.a.Xori(dst, dst, 1)
		case shared.IntGeU:
			f.a.Sltu(dst, left, right)
			f.a.Xori(dst, dst, 1)
		}
		return
	}
	if w {
		switch op {
		case shared.IntAdd:
			f.a.Add(dst, left, right)
		case shared.IntSub:
			f.a.Sub(dst, left, right)
		case shared.IntMul:
			f.a.Mul(dst, left, right)
		case shared.IntAnd:
			f.a.And(dst, left, right)
		case shared.IntOr:
			f.a.Or(dst, left, right)
		case shared.IntXor:
			f.a.Xor(dst, left, right)
		case shared.IntShl:
			f.a.Sll(dst, left, right)
		case shared.IntShrS:
			f.a.Sra(dst, left, right)
		case shared.IntShrU:
			f.a.Srl(dst, left, right)
		default:
			f.err = fmt.Errorf("rv64: unsupported shared scalar operation %d", op)
		}
	} else {
		switch op {
		case shared.IntAdd:
			f.a.Addw(dst, left, right)
		case shared.IntSub:
			f.a.Subw(dst, left, right)
		case shared.IntMul:
			f.a.Mulw(dst, left, right)
		case shared.IntAnd:
			f.a.And(dst, left, right)
		case shared.IntOr:
			f.a.Or(dst, left, right)
		case shared.IntXor:
			f.a.Xor(dst, left, right)
		case shared.IntShl:
			f.a.Sllw(dst, left, right)
		case shared.IntShrS:
			f.a.Sraw(dst, left, right)
		case shared.IntShrU:
			f.a.Srlw(dst, left, right)
		default:
			f.err = fmt.Errorf("rv64: unsupported shared scalar operation %d", op)
		}
		f.NormalizeI32(d)
	}
}
func (f *target) BranchZero(r uint8) int {
	f.a.ZeroExtend32(enc.Scratch0, enc.Reg(r))
	return f.a.BranchLong(enc.EQ, enc.Scratch0, enc.Zero)
}
func (f *target) BranchCompare(op shared.IntOp, w bool, l uint8, o shared.ScalarOperand) int {
	if o.Kind != shared.ScalarRegister {
		f.err = fmt.Errorf("rv64: nonregister branch operand")
		return -1
	}
	left, right := f.compareOperands(op, w, enc.Reg(l), enc.Reg(o.Reg))
	// Shared requests a false-edge branch, not a branch on the predicate.
	cond := enc.EQ
	switch op {
	case shared.IntEq:
		cond = enc.NE
	case shared.IntNe:
		cond = enc.EQ
	case shared.IntLtS:
		cond = enc.GE
	case shared.IntLtU:
		cond = enc.GEU
	case shared.IntGtS:
		cond = enc.GE
		left, right = right, left
	case shared.IntGtU:
		cond = enc.GEU
		left, right = right, left
	case shared.IntLeS:
		cond = enc.LT
		left, right = right, left
	case shared.IntLeU:
		cond = enc.LTU
		left, right = right, left
	case shared.IntGeS:
		cond = enc.LT
	case shared.IntGeU:
		cond = enc.LTU
	default:
		f.err = fmt.Errorf("rv64: unsupported comparison %d", op)
		return -1
	}
	return f.a.BranchLong(cond, left, right)
}
func (f *target) Jump() int                 { return f.a.JumpLong(false) }
func (f *target) Patch(site, pos int) error { return f.a.PatchLong(site, int64(pos)) }
func (f *target) Position() int             { return f.a.Len() }
func (f *target) Return(r uint8, w bool, _ int) {
	f.Move(uint8(enc.Scratch0), r, w)
	f.a.Ld(enc.Scratch1, enc.SP, 8)
	f.a.Sd(enc.Scratch0, enc.Scratch1, 0) // whole normalized serialized cell
	f.a.Addi(enc.X10, enc.Scratch0, 0)
}
