package riscv32

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	rv "github.com/wago-org/wago/src/core/encoder/riscv32"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

const rvContextReg = rv.X23

func (c *compiler) load(r *wasm.Reader, op byte) error {
	if _, err := r.U32(); err != nil {
		return err
	}
	offset, err := r.U32()
	if err != nil {
		return err
	}
	width, signed := uint32(4), false
	switch op {
	case 0x2c:
		width, signed = 1, true
	case 0x2d:
		width = 1
	case 0x2e:
		width, signed = 2, true
	case 0x2f:
		width = 2
	}
	addr := c.materialize(c.pop())
	effective, length, widthReg := c.alloc(), c.alloc(), c.alloc()
	c.a.MovImm32(effective, offset)
	c.a.Add(effective, addr, effective)
	traps := []int{c.a.FarBcond(effective, addr, rv.CondLTU, branchScratch)}
	c.a.Lw(length, rvContextReg, embedded32.ContextLinearMemoryLengthOffset)
	c.a.MovImm32(widthReg, width)
	traps = append(traps, c.a.FarBcond(length, widthReg, rv.CondLTU, branchScratch))
	c.a.Sub(length, length, widthReg)
	traps = append(traps, c.a.FarBcond(length, effective, rv.CondLTU, branchScratch))
	c.a.Lw(length, rvContextReg, embedded32.ContextLinearMemoryBaseOffset)
	c.a.Add(length, length, effective)
	switch width {
	case 1:
		if signed {
			c.a.Lb(widthReg, length, 0)
		} else {
			c.a.Lbu(widthReg, length, 0)
		}
	case 2:
		if signed {
			c.a.Lh(widthReg, length, 0)
		} else {
			c.a.Lhu(widthReg, length, 0)
		}
	default:
		c.a.Lw(widthReg, length, 0)
	}
	done := c.a.FarJump(rv.Zero, branchScratch)
	trap := c.a.Len()
	c.emitContextTrap(embedded32.TrapMemoryOutOfBounds)
	finish := c.a.Len()
	if !c.a.PatchFarJump(done, finish) {
		return fmt.Errorf("riscv32: load done branch out of range")
	}
	for _, at := range traps {
		if !c.a.PatchFarBranch(at, trap) {
			return fmt.Errorf("riscv32: load trap branch out of range")
		}
	}
	c.release(addr)
	c.release(effective)
	c.release(length)
	c.push(operand{reg: widthReg})
	return nil
}

func (c *compiler) store(r *wasm.Reader, op byte) error {
	if _, err := r.U32(); err != nil {
		return err
	}
	offset, err := r.U32()
	if err != nil {
		return err
	}
	width := uint32(4)
	if op == 0x3a {
		width = 1
	} else if op == 0x3b {
		width = 2
	}
	value := c.materialize(c.pop())
	addr := c.materialize(c.pop())
	effective, length, widthReg := c.alloc(), c.alloc(), c.alloc()
	c.a.MovImm32(effective, offset)
	c.a.Add(effective, addr, effective)
	traps := []int{c.a.FarBcond(effective, addr, rv.CondLTU, branchScratch)}
	c.a.Lw(length, rvContextReg, embedded32.ContextLinearMemoryLengthOffset)
	c.a.MovImm32(widthReg, width)
	traps = append(traps, c.a.FarBcond(length, widthReg, rv.CondLTU, branchScratch))
	c.a.Sub(length, length, widthReg)
	traps = append(traps, c.a.FarBcond(length, effective, rv.CondLTU, branchScratch))
	c.a.Lw(length, rvContextReg, embedded32.ContextLinearMemoryBaseOffset)
	c.a.Add(length, length, effective)
	switch width {
	case 1:
		c.a.Sb(value, length, 0)
	case 2:
		c.a.Sh(value, length, 0)
	default:
		c.a.Sw(value, length, 0)
	}
	done := c.a.FarJump(rv.Zero, branchScratch)
	trap := c.a.Len()
	c.emitContextTrap(embedded32.TrapMemoryOutOfBounds)
	finish := c.a.Len()
	if !c.a.PatchFarJump(done, finish) {
		return fmt.Errorf("riscv32: store done branch out of range")
	}
	for _, at := range traps {
		if !c.a.PatchFarBranch(at, trap) {
			return fmt.Errorf("riscv32: store trap branch out of range")
		}
	}
	c.release(addr)
	c.release(value)
	c.release(effective)
	c.release(length)
	c.release(widthReg)
	return nil
}

func (c *compiler) memorySize() {
	dst := c.alloc()
	c.a.Lw(dst, rvContextReg, embedded32.ContextLinearMemoryLengthOffset)
	c.a.Srli(dst, dst, 16)
	c.push(operand{reg: dst})
}

func (c *compiler) memoryGrow() error {
	delta := c.materialize(c.pop())
	old, current, limit := c.alloc(), c.alloc(), c.alloc()
	c.a.Lw(current, rvContextReg, embedded32.ContextLinearMemoryLengthOffset)
	c.a.MovReg(old, current)
	c.a.Srli(old, old, 16)
	c.a.Srli(limit, delta, 16)
	fails := []int{c.a.FarBcond(limit, rv.Zero, rv.CondNE, branchScratch)}
	c.a.Slli(delta, delta, 16)
	c.a.Add(delta, current, delta)
	fails = append(fails, c.a.FarBcond(delta, current, rv.CondLTU, branchScratch))
	c.a.Lw(limit, rvContextReg, embedded32.ContextLinearMemoryMaximumOffset)
	fails = append(fails, c.a.FarBcond(limit, delta, rv.CondLTU, branchScratch))
	c.a.Sw(delta, rvContextReg, embedded32.ContextLinearMemoryLengthOffset)
	c.a.Lw(limit, rvContextReg, embedded32.ContextLinearMemoryBaseOffset)
	c.a.Add(current, limit, current)
	c.a.Add(limit, limit, delta)
	loop := c.a.Len()
	cleared := c.a.Bcond(current, limit, rv.CondEQ)
	c.a.Sw(rv.Zero, current, 0)
	c.a.Addi(current, current, 4)
	back := c.a.Jal(rv.Zero)
	if !c.a.PatchJAL21(back, loop) || !c.a.PatchBranch13(cleared, c.a.Len()) {
		return fmt.Errorf("riscv32: memory.grow clear branch out of range")
	}
	done := c.a.FarJump(rv.Zero, branchScratch)
	fail := c.a.Len()
	c.a.MovImm32(old, 0xffffffff)
	finish := c.a.Len()
	if !c.a.PatchFarJump(done, finish) {
		return fmt.Errorf("riscv32: memory.grow done branch out of range")
	}
	for _, at := range fails {
		if !c.a.PatchFarBranch(at, fail) {
			return fmt.Errorf("riscv32: memory.grow failure branch out of range")
		}
	}
	c.release(delta)
	c.release(current)
	c.release(limit)
	c.push(operand{reg: old})
	return nil
}

func (c *compiler) divRem(op byte) error {
	right, left := c.materialize(c.pop()), c.materialize(c.pop())
	dst := c.alloc()
	zeroBranch := c.a.FarBcond(right, rv.Zero, rv.CondEQ, branchScratch)
	overflowBranch := -1
	if op == 0x6d {
		c.a.MovImm32(dst, 0x80000000)
		notMin := c.a.FarBcond(left, dst, rv.CondNE, branchScratch)
		c.a.MovImm32(dst, 0xffffffff)
		overflowBranch = c.a.FarBcond(right, dst, rv.CondEQ, branchScratch)
		if !c.a.PatchFarBranch(notMin, c.a.Len()) {
			return fmt.Errorf("riscv32: division overflow skip out of range")
		}
	}
	switch op {
	case 0x6d:
		c.a.Div(dst, left, right)
	case 0x6e:
		c.a.Divu(dst, left, right)
	case 0x6f:
		c.a.Rem(dst, left, right)
	case 0x70:
		c.a.Remu(dst, left, right)
	}
	done := c.a.FarJump(rv.Zero, branchScratch)
	zeroTrap := c.a.Len()
	c.emitContextTrap(embedded32.TrapIntegerDivideByZero)
	overflowTrap := c.a.Len()
	if overflowBranch >= 0 {
		c.emitContextTrap(embedded32.TrapIntegerOverflow)
	}
	finish := c.a.Len()
	if !c.a.PatchFarJump(done, finish) || !c.a.PatchFarBranch(zeroBranch, zeroTrap) {
		return fmt.Errorf("riscv32: division branch out of range")
	}
	if overflowBranch >= 0 && !c.a.PatchFarBranch(overflowBranch, overflowTrap) {
		return fmt.Errorf("riscv32: division overflow branch out of range")
	}
	c.release(left)
	c.release(right)
	c.push(operand{reg: dst})
	return nil
}

func (c *compiler) emitContextTrap(trap embedded32.Trap) {
	c.a.Lw(rv.T0, rvContextReg, embedded32.ContextTrapCellOffset)
	c.a.MovImm32(rv.T1, uint32(trap))
	c.a.Sw(rv.T1, rv.T0, 0)
	c.a.MovImm32(rv.A0, 0)
	c.a.Ret()
}
