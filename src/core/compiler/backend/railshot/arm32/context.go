package arm32

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	a32 "github.com/wago-org/wago/src/core/encoder/arm32"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

const armContextReg = a32.R11

func (c *compiler) load(r *wasm.Reader, op byte) error {
	if _, err := r.U32(); err != nil { // alignment is advisory.
		return err
	}
	offset, err := r.U32()
	if err != nil {
		return err
	}
	width := uint32(4)
	signed := false
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
	tmp, dst := c.alloc(), c.alloc()
	c.must(c.a.MovImm32(tmp, offset), "load offset")
	c.must(c.a.Adds(addr, addr, tmp), "load address")
	traps := []int{c.a.FarBcond(a32.CondCS)}
	c.must(c.a.Ldr(tmp, armContextReg, embedded32.ContextLinearMemoryLengthOffset), "load length")
	c.must(c.a.MovImm32(dst, width), "load width")
	c.must(c.a.Cmp(tmp, dst), "load short compare")
	traps = append(traps, c.a.FarBcond(a32.CondCC))
	c.must(c.a.Sub(tmp, tmp, dst), "load bound")
	c.must(c.a.Cmp(tmp, addr), "load bounds compare")
	traps = append(traps, c.a.FarBcond(a32.CondCC))
	c.must(c.a.Ldr(tmp, armContextReg, embedded32.ContextLinearMemoryBaseOffset), "load base")
	c.must(c.a.Add(tmp, tmp, addr), "load pointer")
	switch width {
	case 1:
		if signed {
			c.must(c.a.Ldrsb(dst, tmp, 0), "load8_s")
		} else {
			c.must(c.a.Ldrb(dst, tmp, 0), "load8_u")
		}
	case 2:
		if signed {
			c.must(c.a.Ldrsh(dst, tmp, 0), "load16_s")
		} else {
			c.must(c.a.Ldrh(dst, tmp, 0), "load16_u")
		}
	default:
		c.must(c.a.Ldr(dst, tmp, 0), "load32")
	}
	done := c.a.Branch()
	trap := c.a.Len()
	c.emitContextTrap(embedded32.TrapMemoryOutOfBounds)
	finish := c.a.Len()
	if !c.a.PatchBranch(done, finish) {
		return fmt.Errorf("arm32: load done branch out of range")
	}
	for _, at := range traps {
		if !c.a.PatchFarBranch(at, trap) {
			return fmt.Errorf("arm32: load trap branch out of range")
		}
	}
	c.release(addr)
	c.release(tmp)
	c.push(operand{reg: dst})
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
	tmp := c.alloc()
	c.must(c.a.MovImm32(tmp, offset), "store offset")
	c.must(c.a.Adds(addr, addr, tmp), "store address")
	traps := []int{c.a.FarBcond(a32.CondCS)}
	c.must(c.a.Ldr(tmp, armContextReg, embedded32.ContextLinearMemoryLengthOffset), "store length")
	c.must(c.a.MovImm32(a32.R12, width), "store width")
	c.must(c.a.Cmp(tmp, a32.R12), "store short compare")
	traps = append(traps, c.a.FarBcond(a32.CondCC))
	c.must(c.a.Sub(tmp, tmp, a32.R12), "store bound")
	c.must(c.a.Cmp(tmp, addr), "store bounds compare")
	traps = append(traps, c.a.FarBcond(a32.CondCC))
	c.must(c.a.Ldr(tmp, armContextReg, embedded32.ContextLinearMemoryBaseOffset), "store base")
	c.must(c.a.Add(tmp, tmp, addr), "store pointer")
	switch width {
	case 1:
		c.must(c.a.Strb(value, tmp, 0), "store8")
	case 2:
		c.must(c.a.Strh(value, tmp, 0), "store16")
	default:
		c.must(c.a.Str(value, tmp, 0), "store32")
	}
	c.must(c.a.MovImm32(a32.R12, 0), "restore zero")
	done := c.a.Branch()
	trap := c.a.Len()
	c.emitContextTrap(embedded32.TrapMemoryOutOfBounds)
	finish := c.a.Len()
	if !c.a.PatchBranch(done, finish) {
		return fmt.Errorf("arm32: store done branch out of range")
	}
	for _, at := range traps {
		if !c.a.PatchFarBranch(at, trap) {
			return fmt.Errorf("arm32: store trap branch out of range")
		}
	}
	c.release(addr)
	c.release(value)
	c.release(tmp)
	return nil
}

func (c *compiler) memorySize() {
	dst := c.alloc()
	c.must(c.a.Ldr(dst, armContextReg, embedded32.ContextLinearMemoryLengthOffset), "memory.size length")
	c.must(c.a.LsrImm(dst, dst, 16), "memory.size pages")
	c.push(operand{reg: dst})
}

func (c *compiler) memoryGrow() error {
	delta := c.materialize(c.pop())
	old, current, limit := c.alloc(), c.alloc(), c.alloc()
	c.must(c.a.Ldr(current, armContextReg, embedded32.ContextLinearMemoryLengthOffset), "memory.grow current")
	c.must(c.a.MovReg(old, current), "memory.grow old")
	c.must(c.a.LsrImm(old, old, 16), "memory.grow old pages")
	c.must(c.a.LsrImm(limit, delta, 16), "memory.grow delta overflow")
	c.must(c.a.Cmp(limit, a32.R12), "memory.grow delta compare")
	fails := []int{c.a.FarBcond(a32.CondNE)}
	c.must(c.a.LslImm(delta, delta, 16), "memory.grow delta bytes")
	c.must(c.a.Adds(delta, current, delta), "memory.grow new length")
	fails = append(fails, c.a.FarBcond(a32.CondCS))
	c.must(c.a.Ldr(limit, armContextReg, embedded32.ContextLinearMemoryMaximumOffset), "memory.grow maximum")
	c.must(c.a.Cmp(limit, delta), "memory.grow maximum compare")
	fails = append(fails, c.a.FarBcond(a32.CondCC))
	c.must(c.a.Str(delta, armContextReg, embedded32.ContextLinearMemoryLengthOffset), "memory.grow publish length")
	c.must(c.a.Ldr(limit, armContextReg, embedded32.ContextLinearMemoryBaseOffset), "memory.grow base")
	c.must(c.a.Add(current, limit, current), "memory.grow clear start")
	c.must(c.a.Add(limit, limit, delta), "memory.grow clear end")
	loop := c.a.Len()
	c.must(c.a.Cmp(current, limit), "memory.grow clear compare")
	cleared := c.a.FarBcond(a32.CondEQ)
	c.must(c.a.Str(a32.R12, current, 0), "memory.grow clear word")
	c.must(c.a.MovImm32(delta, 4), "memory.grow clear step")
	c.must(c.a.Add(current, current, delta), "memory.grow clear advance")
	back := c.a.Branch()
	if !c.a.PatchBranch(back, loop) || !c.a.PatchFarBranch(cleared, c.a.Len()) {
		return fmt.Errorf("arm32: memory.grow clear branch out of range")
	}
	done := c.a.Branch()
	fail := c.a.Len()
	c.must(c.a.MovImm32(old, 0xffffffff), "memory.grow failure result")
	finish := c.a.Len()
	if !c.a.PatchBranch(done, finish) {
		return fmt.Errorf("arm32: memory.grow done branch out of range")
	}
	for _, at := range fails {
		if !c.a.PatchFarBranch(at, fail) {
			return fmt.Errorf("arm32: memory.grow failure branch out of range")
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
	c.must(c.a.Cmp(right, a32.R12), "division zero compare")
	zeroBranch := c.a.FarBcond(a32.CondEQ)
	overflowBranch := -1
	if op == 0x6d {
		c.must(c.a.MovImm32(dst, 0x80000000), "division minimum")
		c.must(c.a.Cmp(left, dst), "division minimum compare")
		notMin := c.a.FarBcond(a32.CondNE)
		c.must(c.a.MovImm32(dst, 0xffffffff), "division minus one")
		c.must(c.a.Cmp(right, dst), "division overflow compare")
		overflowBranch = c.a.FarBcond(a32.CondEQ)
		if !c.a.PatchFarBranch(notMin, c.a.Len()) {
			return fmt.Errorf("arm32: division overflow skip out of range")
		}
	}
	if op == 0x6d || op == 0x6f {
		c.must(c.a.Sdiv(dst, left, right), "signed division")
	} else {
		c.must(c.a.Udiv(dst, left, right), "unsigned division")
	}
	if op == 0x6f || op == 0x70 {
		c.must(c.a.Mul(dst, dst, right), "remainder product")
		c.must(c.a.Sub(dst, left, dst), "remainder subtract")
	}
	done := c.a.Branch()
	zeroTrap := c.a.Len()
	c.emitContextTrap(embedded32.TrapIntegerDivideByZero)
	overflowTrap := c.a.Len()
	if overflowBranch >= 0 {
		c.emitContextTrap(embedded32.TrapIntegerOverflow)
	}
	finish := c.a.Len()
	if !c.a.PatchBranch(done, finish) || !c.a.PatchFarBranch(zeroBranch, zeroTrap) {
		return fmt.Errorf("arm32: division branch out of range")
	}
	if overflowBranch >= 0 && !c.a.PatchFarBranch(overflowBranch, overflowTrap) {
		return fmt.Errorf("arm32: division overflow branch out of range")
	}
	c.release(left)
	c.release(right)
	c.push(operand{reg: dst})
	return nil
}

func (c *compiler) emitContextTrap(trap embedded32.Trap) {
	c.must(c.a.Ldr(a32.R12, armContextReg, embedded32.ContextTrapCellOffset), "trap cell")
	c.must(c.a.MovImm32(a32.R0, uint32(trap)), "trap code")
	c.must(c.a.Str(a32.R0, a32.R12, 0), "trap write")
	c.must(c.a.MovImm32(a32.R0, 0), "trap result")
	c.a.Ret()
	c.a.Align4()
}
