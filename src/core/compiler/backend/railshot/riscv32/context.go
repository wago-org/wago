package riscv32

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rv "github.com/wago-org/wago/src/core/encoder/riscv32"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

const (
	rvContextReg        = rv.X23
	rvModuleFrame       = 128
	rvLiveBase          = 40
	rvArgumentBase      = 80
	rvCallResultSlot    = 112
	rvImportContextSlot = 116
)

var rvModuleSavedRegs = []rv.Reg{rv.X8, rv.X9, rv.X18, rv.X19, rv.X20, rv.X21, rv.X22, rv.X24, rv.X25}

func (c *compiler) emitModulePrologue() {
	c.frameSize = rvModuleFrame
	c.a.Addi(rv.SP, rv.SP, -c.frameSize)
	c.a.Lw(rv.T6, rvContextReg, embedded32.ContextStackLimitOffset)
	stackOK := c.a.FarBcond(rv.SP, rv.T6, rv.CondGEU, branchScratch)
	c.a.Addi(rv.SP, rv.SP, c.frameSize)
	c.a.Lw(rv.T6, rvContextReg, embedded32.ContextTrapCellOffset)
	c.a.MovImm32(rv.T5, uint32(embedded32.TrapStackOverflow))
	c.a.Sw(rv.T5, rv.T6, 0)
	c.a.MovImm32(rv.A0, 0)
	c.a.Ret()
	if !c.a.PatchFarBranch(stackOK, c.a.Len()) {
		panic("riscv32: module stack branch out of range")
	}
	for i, reg := range rvModuleSavedRegs {
		c.a.Sw(reg, rv.SP, int32(i*4))
	}
	c.a.Sw(rv.RA, rv.SP, 36)
}

func (c *compiler) emitModuleReturn() {
	if !c.context {
		c.a.Ret()
		return
	}
	for i, reg := range rvModuleSavedRegs {
		c.a.Lw(reg, rv.SP, int32(i*4))
	}
	c.a.Lw(rv.RA, rv.SP, 36)
	c.a.Addi(rv.SP, rv.SP, c.frameSize)
	c.a.Ret()
}

func rvScratchSlot(reg rv.Reg) (int32, bool) {
	for i, candidate := range scratchRegs {
		if candidate == reg {
			return rvLiveBase + int32(i*4), true
		}
	}
	return 0, false
}

func (c *compiler) pollCancellation() error {
	poll := c.alloc()
	c.a.Lw(poll, rvContextReg, embedded32.ContextCancelCellOffset)
	c.a.Lw(poll, poll, 0)
	clear := c.a.FarBcond(poll, rv.Zero, rv.CondEQ, branchScratch)
	c.emitContextTrap(embedded32.TrapCanceled)
	if !c.a.PatchFarBranch(clear, c.a.Len()) {
		return fmt.Errorf("riscv32: cancellation branch out of range")
	}
	c.release(poll)
	return nil
}

func (c *compiler) call(target int) error {
	if c.module == nil || c.relocSink == nil {
		return fmt.Errorf("riscv32: call requires module compilation")
	}
	ft, ok := c.module.FuncSignature(uint32(target))
	if !ok || ft.Kind != wasm.CompFunc || len(ft.Params) > 8 || len(ft.Results) > 1 {
		return fmt.Errorf("riscv32: unsupported call target %d", target)
	}
	for _, typ := range ft.Params {
		if typ != wasm.I32 {
			return fmt.Errorf("riscv32: call target %d has non-i32 parameter", target)
		}
	}
	if len(ft.Results) == 1 && ft.Results[0] != wasm.I32 {
		return fmt.Errorf("riscv32: call target %d has non-i32 result", target)
	}
	for _, v := range c.stack {
		if v.constant {
			continue
		}
		off, ok := rvScratchSlot(v.reg)
		if !ok || !c.a.Sw(v.reg, rv.SP, off) {
			panic("riscv32: call live spill")
		}
	}
	args := make([]rv.Reg, len(ft.Params))
	for i := len(args) - 1; i >= 0; i-- {
		args[i] = c.materialize(c.pop())
	}
	for i, reg := range args {
		if !c.a.Sw(reg, rv.SP, rvArgumentBase+int32(i*4)) {
			panic("riscv32: call argument spill")
		}
		c.release(reg)
	}
	for i := range args {
		if !c.a.Lw(rv.A0+rv.Reg(i), rv.SP, rvArgumentBase+int32(i*4)) {
			panic("riscv32: call argument load")
		}
	}
	imported := c.module.ImportedFuncCount()
	if target < imported {
		if uint64(target) > uint64(^uint32(0)/embedded32.ImportFunctionABIBytes) {
			return fmt.Errorf("riscv32: import index %d exceeds addressable descriptors", target)
		}
		c.a.Lw(rv.T6, rvContextReg, embedded32.ContextImportsBaseOffset)
		c.a.MovImm32(rv.T5, uint32(target)*embedded32.ImportFunctionABIBytes)
		c.a.Add(rv.T6, rv.T6, rv.T5)
		c.a.Lw(rv.T5, rv.T6, embedded32.ImportFunctionEntryOffset)
		c.a.Lw(rv.T6, rv.T6, embedded32.ImportFunctionContextOffset)
		c.a.Sw(rvContextReg, rv.SP, rvImportContextSlot)
		c.a.MovReg(rvContextReg, rv.T6)
		c.a.Blr(rv.T5)
	} else {
		at := c.a.FarCall(rv.T6)
		*c.relocSink = append(*c.relocSink, callReloc{at: at, target: target - imported})
	}
	if len(ft.Results) == 1 && !c.a.Sw(rv.A0, rv.SP, rvCallResultSlot) {
		panic("riscv32: call result spill")
	}
	if target < imported {
		c.a.Lw(rv.T6, rvContextReg, embedded32.ContextTrapCellOffset)
		c.a.Lw(rv.T6, rv.T6, 0)
		c.a.Lw(rvContextReg, rv.SP, rvImportContextSlot)
		c.a.Lw(rv.T5, rvContextReg, embedded32.ContextTrapCellOffset)
		c.a.Sw(rv.T6, rv.T5, 0)
	}
	c.a.Lw(rv.T6, rvContextReg, embedded32.ContextTrapCellOffset)
	c.a.Lw(rv.T6, rv.T6, 0)
	clear := c.a.FarBcond(rv.T6, rv.Zero, rv.CondEQ, branchScratch)
	c.a.MovImm32(rv.A0, 0)
	c.emitModuleReturn()
	if !c.a.PatchFarBranch(clear, c.a.Len()) {
		return fmt.Errorf("riscv32: call trap branch out of range")
	}
	for _, v := range c.stack {
		if v.constant {
			continue
		}
		off, _ := rvScratchSlot(v.reg)
		if !c.a.Lw(v.reg, rv.SP, off) {
			panic("riscv32: call live reload")
		}
	}
	if len(ft.Results) == 1 {
		dst := c.alloc()
		if !c.a.Lw(dst, rv.SP, rvCallResultSlot) {
			panic("riscv32: call result reload")
		}
		c.push(operand{reg: dst})
	}
	return nil
}

func (c *compiler) globalGet(index uint32) error {
	typ, _, target, imported, ok := shared.EmbeddedGlobalLocation(c.module, index)
	if !ok || typ != wasm.I32 {
		return fmt.Errorf("riscv32: unsupported global.get %d", index)
	}
	base, dst := c.alloc(), c.alloc()
	contextOffset := int32(embedded32.ContextGlobalsBaseOffset)
	if imported {
		contextOffset = embedded32.ContextImportedGlobalsBaseOffset
	}
	c.a.Lw(base, rvContextReg, contextOffset)
	offset := uint64(target) * 4
	if imported {
		if offset <= 2047 {
			c.a.Lw(base, base, int32(offset))
		} else {
			c.a.MovImm32(rv.T6, uint32(offset))
			c.a.Add(base, base, rv.T6)
			c.a.Lw(base, base, 0)
		}
		offset = 0
	}
	if offset <= 2047 {
		c.a.Lw(dst, base, int32(offset))
	} else {
		c.a.MovImm32(rv.T6, uint32(offset))
		c.a.Add(base, base, rv.T6)
		c.a.Lw(dst, base, 0)
	}
	c.release(base)
	c.push(operand{reg: dst})
	return nil
}

func (c *compiler) globalSet(index uint32) error {
	typ, mutable, target, imported, ok := shared.EmbeddedGlobalLocation(c.module, index)
	if !ok || typ != wasm.I32 || !mutable {
		return fmt.Errorf("riscv32: unsupported global.set %d", index)
	}
	value := c.materialize(c.pop())
	base := c.alloc()
	contextOffset := int32(embedded32.ContextGlobalsBaseOffset)
	if imported {
		contextOffset = embedded32.ContextImportedGlobalsBaseOffset
	}
	c.a.Lw(base, rvContextReg, contextOffset)
	offset := uint64(target) * 4
	if imported {
		if offset <= 2047 {
			c.a.Lw(base, base, int32(offset))
		} else {
			c.a.MovImm32(rv.T6, uint32(offset))
			c.a.Add(base, base, rv.T6)
			c.a.Lw(base, base, 0)
		}
		offset = 0
	}
	if offset <= 2047 {
		c.a.Sw(value, base, int32(offset))
	} else {
		c.a.MovImm32(rv.T6, uint32(offset))
		c.a.Add(base, base, rv.T6)
		c.a.Sw(value, base, 0)
	}
	c.release(value)
	c.release(base)
	return nil
}

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

func (c *compiler) memoryInit(index uint32) error {
	if c.module == nil || uint64(index) >= uint64(len(c.module.Data)) {
		return fmt.Errorf("riscv32: invalid data segment %d", index)
	}
	n := c.materialize(c.pop())
	src := c.materialize(c.pop())
	dst := c.materialize(c.pop())
	descriptor := c.alloc()
	c.a.Lw(descriptor, rvContextReg, embedded32.ContextDataSegmentsBaseOffset)
	c.a.MovImm32(rv.T6, index*embedded32.DataSegmentABIBytes)
	c.a.Add(descriptor, descriptor, rv.T6)
	c.a.Lw(rv.T5, descriptor, embedded32.DataSegmentDroppedOffset)
	available := c.a.Bcond(rv.T5, rv.Zero, rv.CondEQ)
	c.a.MovImm32(rv.T5, 0)
	lengthReady := c.a.Jal(rv.Zero)
	availableTarget := c.a.Len()
	c.a.Lw(rv.T5, descriptor, embedded32.DataSegmentLengthOffset)
	lengthTarget := c.a.Len()
	if !c.a.PatchBranch13(available, availableTarget) || !c.a.PatchJAL21(lengthReady, lengthTarget) {
		return fmt.Errorf("riscv32: data length branch out of range")
	}
	traps := []int{c.a.FarBcond(rv.T5, n, rv.CondLTU, branchScratch)}
	c.a.Sub(rv.T5, rv.T5, n)
	traps = append(traps, c.a.FarBcond(rv.T5, src, rv.CondLTU, branchScratch))
	c.a.Lw(rv.T5, rvContextReg, embedded32.ContextLinearMemoryLengthOffset)
	traps = append(traps, c.a.FarBcond(rv.T5, n, rv.CondLTU, branchScratch))
	c.a.Sub(rv.T5, rv.T5, n)
	traps = append(traps, c.a.FarBcond(rv.T5, dst, rv.CondLTU, branchScratch))
	c.a.Lw(descriptor, descriptor, embedded32.DataSegmentBaseOffset)
	c.a.Add(descriptor, descriptor, src)
	c.a.Lw(rv.T5, rvContextReg, embedded32.ContextLinearMemoryBaseOffset)
	c.a.Add(dst, dst, rv.T5)
	loop := c.a.Len()
	copied := c.a.Bcond(n, rv.Zero, rv.CondEQ)
	c.a.Lbu(rv.T5, descriptor, 0)
	c.a.Sb(rv.T5, dst, 0)
	c.a.Addi(descriptor, descriptor, 1)
	c.a.Addi(dst, dst, 1)
	c.a.Addi(n, n, -1)
	back := c.a.Jal(rv.Zero)
	if !c.a.PatchJAL21(back, loop) || !c.a.PatchBranch13(copied, c.a.Len()) {
		return fmt.Errorf("riscv32: memory.init loop out of range")
	}
	done := c.a.FarJump(rv.Zero, branchScratch)
	trap := c.a.Len()
	c.emitContextTrap(embedded32.TrapMemoryOutOfBounds)
	finish := c.a.Len()
	if !c.a.PatchFarJump(done, finish) {
		return fmt.Errorf("riscv32: memory.init success branch out of range")
	}
	for _, at := range traps {
		if !c.a.PatchFarBranch(at, trap) {
			return fmt.Errorf("riscv32: memory.init trap branch out of range")
		}
	}
	c.release(n)
	c.release(src)
	c.release(dst)
	c.release(descriptor)
	return nil
}

func (c *compiler) dataDrop(index uint32) error {
	if c.module == nil || uint64(index) >= uint64(len(c.module.Data)) {
		return fmt.Errorf("riscv32: invalid data segment %d", index)
	}
	descriptor := c.alloc()
	c.a.Lw(descriptor, rvContextReg, embedded32.ContextDataSegmentsBaseOffset)
	c.a.MovImm32(rv.T6, index*embedded32.DataSegmentABIBytes)
	c.a.Add(descriptor, descriptor, rv.T6)
	c.a.MovImm32(rv.T5, 1)
	c.a.Sw(rv.T5, descriptor, embedded32.DataSegmentDroppedOffset)
	c.release(descriptor)
	return nil
}

func (c *compiler) memoryCopy() error {
	n := c.materialize(c.pop())
	src := c.materialize(c.pop())
	dst := c.materialize(c.pop())
	tmp := c.alloc()
	c.a.Lw(tmp, rvContextReg, embedded32.ContextLinearMemoryLengthOffset)
	traps := []int{c.a.FarBcond(tmp, n, rv.CondLTU, branchScratch)}
	c.a.Sub(tmp, tmp, n)
	traps = append(traps, c.a.FarBcond(tmp, dst, rv.CondLTU, branchScratch))
	traps = append(traps, c.a.FarBcond(tmp, src, rv.CondLTU, branchScratch))
	c.a.Lw(tmp, rvContextReg, embedded32.ContextLinearMemoryBaseOffset)
	c.a.Add(dst, dst, tmp)
	c.a.Add(src, src, tmp)
	forward := c.a.FarBcond(src, dst, rv.CondGEU, branchScratch)
	c.a.Add(dst, dst, n)
	c.a.Add(src, src, n)
	backLoop := c.a.Len()
	backDone := c.a.Bcond(n, rv.Zero, rv.CondEQ)
	c.a.Addi(dst, dst, -1)
	c.a.Addi(src, src, -1)
	c.a.Lbu(rv.T5, src, 0)
	c.a.Sb(rv.T5, dst, 0)
	c.a.Addi(n, n, -1)
	back := c.a.Jal(rv.Zero)
	if !c.a.PatchJAL21(back, backLoop) {
		return fmt.Errorf("riscv32: memory.copy backward loop out of range")
	}
	forwardTarget := c.a.Len()
	if !c.a.PatchFarBranch(forward, forwardTarget) {
		return fmt.Errorf("riscv32: memory.copy direction branch out of range")
	}
	forwardLoop := c.a.Len()
	forwardDone := c.a.Bcond(n, rv.Zero, rv.CondEQ)
	c.a.Lbu(rv.T5, src, 0)
	c.a.Sb(rv.T5, dst, 0)
	c.a.Addi(dst, dst, 1)
	c.a.Addi(src, src, 1)
	c.a.Addi(n, n, -1)
	forwardBack := c.a.Jal(rv.Zero)
	if !c.a.PatchJAL21(forwardBack, forwardLoop) {
		return fmt.Errorf("riscv32: memory.copy forward loop out of range")
	}
	finishCopy := c.a.Len()
	if !c.a.PatchBranch13(backDone, finishCopy) || !c.a.PatchBranch13(forwardDone, finishCopy) {
		return fmt.Errorf("riscv32: memory.copy done branch out of range")
	}
	done := c.a.FarJump(rv.Zero, branchScratch)
	trap := c.a.Len()
	c.emitContextTrap(embedded32.TrapMemoryOutOfBounds)
	finish := c.a.Len()
	if !c.a.PatchFarJump(done, finish) {
		return fmt.Errorf("riscv32: memory.copy success branch out of range")
	}
	for _, at := range traps {
		if !c.a.PatchFarBranch(at, trap) {
			return fmt.Errorf("riscv32: memory.copy trap branch out of range")
		}
	}
	c.release(n)
	c.release(src)
	c.release(dst)
	c.release(tmp)
	return nil
}

func (c *compiler) memoryFill() error {
	n := c.materialize(c.pop())
	value := c.materialize(c.pop())
	dst := c.materialize(c.pop())
	tmp := c.alloc()
	c.a.Lw(tmp, rvContextReg, embedded32.ContextLinearMemoryLengthOffset)
	traps := []int{c.a.FarBcond(tmp, n, rv.CondLTU, branchScratch)}
	c.a.Sub(tmp, tmp, n)
	traps = append(traps, c.a.FarBcond(tmp, dst, rv.CondLTU, branchScratch))
	c.a.Lw(tmp, rvContextReg, embedded32.ContextLinearMemoryBaseOffset)
	c.a.Add(dst, dst, tmp)
	loop := c.a.Len()
	filled := c.a.Bcond(n, rv.Zero, rv.CondEQ)
	c.a.Sb(value, dst, 0)
	c.a.Addi(dst, dst, 1)
	c.a.Addi(n, n, -1)
	back := c.a.Jal(rv.Zero)
	if !c.a.PatchJAL21(back, loop) || !c.a.PatchBranch13(filled, c.a.Len()) {
		return fmt.Errorf("riscv32: memory.fill loop out of range")
	}
	done := c.a.FarJump(rv.Zero, branchScratch)
	trap := c.a.Len()
	c.emitContextTrap(embedded32.TrapMemoryOutOfBounds)
	finish := c.a.Len()
	if !c.a.PatchFarJump(done, finish) {
		return fmt.Errorf("riscv32: memory.fill success branch out of range")
	}
	for _, at := range traps {
		if !c.a.PatchFarBranch(at, trap) {
			return fmt.Errorf("riscv32: memory.fill trap branch out of range")
		}
	}
	c.release(n)
	c.release(value)
	c.release(dst)
	c.release(tmp)
	return nil
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
	c.emitModuleReturn()
}
