// Package riscv64 contains wago's Linux/RV64 railshot backend. This file is the
// deliberately small integer/control beachhead: it proves validated wasm bodies
// can be lowered through the new encoder and executed through the no-cgo runtime
// boundary before the full valent-block backend is ported.
package riscv64

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	rv "github.com/wago-org/wago/src/core/encoder/riscv64"
)

// A0/A1 are copied into callee-saved local homes before the argument registers
// join the scratch pool. X24/X25 are reserved for mem-size/linMem, X26/X27 for
// Go CTXT/g, and X31 for fixed-size far control transfers.
var scratchRegs = []rv.Reg{rv.X10, rv.X11, rv.X12, rv.X13, rv.X14, rv.X15, rv.X16, rv.X17, rv.X28, rv.X29}
var localRegs = []rv.Reg{rv.X8, rv.X9, rv.X18, rv.X19, rv.X20, rv.X21, rv.X22, rv.X23}

const branchScratch = rv.X31

type operand struct {
	constant bool
	reg      rv.Reg
	value    int32
}

type controlFrame struct {
	function bool
	loop     bool
	ifBlock  bool
	header   int
	elseSite int
	pending  []int
}

type compiler struct {
	a       rv.Asm
	stack   []operand
	free    []rv.Reg
	locals  []rv.Reg
	control []*controlFrame
}

// CompileBeachhead lowers one wasm function body using the temporary integer
// register ABI: up to eight i32 parameters arrive in A0..A7 and one i32 result
// returns in A0. It supports locals, integer arithmetic/comparisons, and
// structured block/loop/if/br/br_if control with void block types.
func CompileBeachhead(numParams int, body []byte) ([]byte, error) {
	c := &compiler{free: append([]rv.Reg(nil), scratchRegs...)}
	r := wasm.NewReader(body)

	groups, err := r.U32()
	if err != nil {
		return nil, fmt.Errorf("local declarations: %w", err)
	}
	declared := 0
	for i := uint32(0); i < groups; i++ {
		n, err := r.U32()
		if err != nil {
			return nil, fmt.Errorf("local declaration count: %w", err)
		}
		t, err := r.Byte()
		if err != nil {
			return nil, fmt.Errorf("local declaration type: %w", err)
		}
		if t != 0x7f { // i32 value type
			return nil, fmt.Errorf("riscv64 beachhead supports only i32 locals, got %#x", t)
		}
		declared += int(n)
	}
	total := numParams + declared
	if numParams < 0 || numParams > 8 {
		return nil, fmt.Errorf("riscv64 beachhead supports 0..8 parameters, got %d", numParams)
	}
	if total > len(localRegs) {
		return nil, fmt.Errorf("riscv64 beachhead supports up to %d locals, got %d", len(localRegs), total)
	}
	c.locals = append(c.locals, localRegs[:total]...)
	for i := 0; i < numParams; i++ {
		c.a.MovReg32(c.locals[i], rv.A0+rv.Reg(i))
	}
	for i := numParams; i < total; i++ {
		c.a.MovSigned32(c.locals[i], 0)
	}
	c.control = []*controlFrame{{function: true, elseSite: -1}}

	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return nil, err
		}
		switch op {
		case 0x02: // block
			if err := readVoidBlockType(r); err != nil {
				return nil, err
			}
			c.control = append(c.control, &controlFrame{elseSite: -1})
		case 0x03: // loop
			if err := readVoidBlockType(r); err != nil {
				return nil, err
			}
			c.control = append(c.control, &controlFrame{loop: true, header: c.a.Len(), elseSite: -1})
		case 0x04: // if
			if err := readVoidBlockType(r); err != nil {
				return nil, err
			}
			cond := c.materialize(c.pop())
			site := c.a.FarBcond(cond, rv.Zero, rv.CondEQ, branchScratch)
			c.release(cond)
			c.control = append(c.control, &controlFrame{ifBlock: true, elseSite: site})
		case 0x05: // else
			fr, err := c.topControl()
			if err != nil || !fr.ifBlock || fr.elseSite < 0 {
				return nil, fmt.Errorf("unexpected else")
			}
			fr.pending = append(fr.pending, c.a.FarJump(rv.Zero, branchScratch))
			if !c.a.PatchFarBranch(fr.elseSite, c.a.Len()) {
				return nil, fmt.Errorf("if else target out of range")
			}
			fr.elseSite = -1
		case 0x0b: // end
			fr, err := c.topControl()
			if err != nil {
				return nil, err
			}
			c.control = c.control[:len(c.control)-1]
			if fr.function {
				c.emitReturn()
				return c.a.B, nil
			}
			here := c.a.Len()
			for _, site := range fr.pending {
				if !c.a.PatchFarJump(site, here) {
					return nil, fmt.Errorf("control target out of range")
				}
			}
			if fr.ifBlock && fr.elseSite >= 0 && !c.a.PatchFarBranch(fr.elseSite, here) {
				return nil, fmt.Errorf("if end target out of range")
			}
		case 0x0c: // br
			if err := c.branch(r, false); err != nil {
				return nil, err
			}
		case 0x0d: // br_if
			if err := c.branch(r, true); err != nil {
				return nil, err
			}
		case 0x0f: // return
			c.emitReturn()
		case 0x20: // local.get
			idx, err := r.U32()
			if err != nil || int(idx) >= len(c.locals) {
				return nil, fmt.Errorf("local.get index %d", idx)
			}
			dst := c.alloc()
			c.a.MovReg32(dst, c.locals[idx])
			c.push(operand{reg: dst})
		case 0x21, 0x22: // local.set / local.tee
			idx, err := r.U32()
			if err != nil || int(idx) >= len(c.locals) {
				return nil, fmt.Errorf("local index %d", idx)
			}
			value := c.materialize(c.pop())
			c.a.MovReg32(c.locals[idx], value)
			if op == 0x22 {
				c.push(operand{reg: value})
			} else {
				c.release(value)
			}
		case 0x41: // i32.const
			v, err := r.I32()
			if err != nil {
				return nil, err
			}
			c.push(operand{constant: true, value: v})
		case 0x45: // i32.eqz
			c.eqz()
		case 0x46, 0x47, 0x48, 0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f:
			c.compare(op)
		case 0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76:
			c.binary(op)
		default:
			return nil, fmt.Errorf("riscv64 beachhead unsupported opcode %#x", op)
		}
	}
	return nil, fmt.Errorf("function body did not terminate with end")
}

func readVoidBlockType(r *wasm.Reader) error {
	bt, err := r.Byte()
	if err != nil {
		return err
	}
	if bt != 0x40 {
		return fmt.Errorf("riscv64 beachhead supports only void block type, got %#x", bt)
	}
	return nil
}

func (c *compiler) topControl() (*controlFrame, error) {
	if len(c.control) == 0 {
		return nil, fmt.Errorf("control stack underflow")
	}
	return c.control[len(c.control)-1], nil
}

func (c *compiler) branch(r *wasm.Reader, conditional bool) error {
	depth, err := r.U32()
	if err != nil {
		return err
	}
	index := len(c.control) - 1 - int(depth)
	if index < 0 {
		return fmt.Errorf("branch depth %d exceeds control stack", depth)
	}
	fr := c.control[index]
	if !conditional {
		switch {
		case fr.function:
			c.emitReturn()
		case fr.loop:
			site := c.a.FarJump(rv.Zero, branchScratch)
			if !c.a.PatchFarJump(site, fr.header) {
				return fmt.Errorf("loop target out of range")
			}
		default:
			fr.pending = append(fr.pending, c.a.FarJump(rv.Zero, branchScratch))
		}
		return nil
	}

	cond := c.materialize(c.pop())
	switch {
	case fr.function:
		skip := c.a.FarBcond(cond, rv.Zero, rv.CondEQ, branchScratch)
		c.emitReturn()
		if !c.a.PatchFarBranch(skip, c.a.Len()) {
			return fmt.Errorf("conditional return target out of range")
		}
	case fr.loop:
		site := c.a.FarBcond(cond, rv.Zero, rv.CondNE, branchScratch)
		if !c.a.PatchFarBranch(site, fr.header) {
			return fmt.Errorf("loop branch target out of range")
		}
	default:
		site := c.a.FarBcond(cond, rv.Zero, rv.CondNE, branchScratch)
		fr.pending = append(fr.pending, site+4) // AUIPC+JALR starts after inverse branch.
	}
	c.release(cond)
	return nil
}

func (c *compiler) emitReturn() {
	if len(c.stack) != 0 {
		result := c.materialize(c.pop())
		if result != rv.A0 {
			c.a.MovReg32(rv.A0, result)
		}
		c.release(result)
	}
	c.a.Ret()
}

func (c *compiler) binary(op byte) {
	right := c.materialize(c.pop())
	left := c.materialize(c.pop())
	dst := c.alloc()
	switch op {
	case 0x6a:
		c.a.Addw(dst, left, right)
	case 0x6b:
		c.a.Subw(dst, left, right)
	case 0x6c:
		c.a.Mulw(dst, left, right)
	case 0x71:
		c.a.And(dst, left, right)
	case 0x72:
		c.a.Or(dst, left, right)
	case 0x73:
		c.a.Xor(dst, left, right)
	case 0x74:
		c.a.Sllw(dst, left, right)
	case 0x75:
		c.a.Sraw(dst, left, right)
	case 0x76:
		c.a.Srlw(dst, left, right)
	}
	c.release(left)
	c.release(right)
	c.push(operand{reg: dst})
}

func (c *compiler) compare(op byte) {
	right := c.materialize(c.pop())
	left := c.materialize(c.pop())
	// Canonicalize operands for the selected signedness. Wasm i32 values only
	// define their low 32 bits; RV64 comparisons consume all 64 bits.
	unsigned := op == 0x49 || op == 0x4b || op == 0x4d || op == 0x4f
	if unsigned {
		c.a.Zext32(left, left)
		c.a.Zext32(right, right)
	} else {
		c.a.Sext32(left, left)
		c.a.Sext32(right, right)
	}
	dst := c.alloc()
	switch op {
	case 0x46: // eq
		c.a.Xor(dst, left, right)
		c.a.Seqz(dst, dst)
	case 0x47: // ne
		c.a.Xor(dst, left, right)
		c.a.Snez(dst, dst)
	case 0x48: // lt_s
		c.a.Slt(dst, left, right)
	case 0x49: // lt_u
		c.a.Sltu(dst, left, right)
	case 0x4a: // gt_s
		c.a.Slt(dst, right, left)
	case 0x4b: // gt_u
		c.a.Sltu(dst, right, left)
	case 0x4c: // le_s
		c.a.Slt(dst, right, left)
		c.a.Xori(dst, dst, 1)
	case 0x4d: // le_u
		c.a.Sltu(dst, right, left)
		c.a.Xori(dst, dst, 1)
	case 0x4e: // ge_s
		c.a.Slt(dst, left, right)
		c.a.Xori(dst, dst, 1)
	case 0x4f: // ge_u
		c.a.Sltu(dst, left, right)
		c.a.Xori(dst, dst, 1)
	}
	c.release(left)
	c.release(right)
	c.push(operand{reg: dst})
}

func (c *compiler) eqz() {
	value := c.materialize(c.pop())
	c.a.Sext32(value, value)
	dst := c.alloc()
	c.a.Seqz(dst, value)
	c.release(value)
	c.push(operand{reg: dst})
}

func (c *compiler) push(v operand) { c.stack = append(c.stack, v) }
func (c *compiler) pop() operand {
	if len(c.stack) == 0 {
		panic("riscv64 beachhead: operand stack underflow")
	}
	v := c.stack[len(c.stack)-1]
	c.stack = c.stack[:len(c.stack)-1]
	return v
}

func (c *compiler) materialize(v operand) rv.Reg {
	if !v.constant {
		return v.reg
	}
	dst := c.alloc()
	c.a.MovSigned32(dst, v.value)
	return dst
}

func (c *compiler) alloc() rv.Reg {
	if len(c.free) == 0 {
		panic("riscv64 beachhead: expression exceeds scratch register capacity")
	}
	reg := c.free[0]
	c.free = c.free[1:]
	return reg
}

func (c *compiler) release(reg rv.Reg) {
	for _, scratch := range scratchRegs {
		if scratch == reg {
			c.free = append(c.free, reg)
			return
		}
	}
}
