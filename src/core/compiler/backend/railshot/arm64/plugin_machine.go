//go:build arm64

package arm64

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/machinecode"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

type pluginARM64Context struct {
	f          *fn
	paramSlots []int
	paramWidth []int32
	gp, vector regMask
	output     Reg
	outputSet  bool
}

func (c *pluginARM64Context) Encoder() *a64.Asm { return c.f.a }

func (c *pluginARM64Context) InputI32(index int) (a64.Reg, error) {
	if index < 0 || index >= len(c.paramSlots) {
		return 0, fmt.Errorf("arm64 plugin input %d out of range", index)
	}
	r := c.AllocGP()
	c.f.ld64(r, SP, c.f.spillOff(c.paramSlots[index]))
	if width := c.paramWidth[index]; width < 32 {
		mask := uint32((uint64(1) << uint(width)) - 1)
		if !c.f.a.AndImm32(r, r, mask) {
			tmp := c.AllocGP(r)
			c.f.a.MovImm64(tmp, uint64(mask))
			c.f.a.And32(r, r, tmp)
			c.Release(tmp)
		}
	}
	return r, nil
}

func arm64ExclusionMask(regs []a64.Reg) regMask { return maskOf(regs...) }

func (c *pluginARM64Context) AllocGP(exclude ...a64.Reg) a64.Reg {
	r := c.f.allocReg(arm64ExclusionMask(exclude))
	c.f.pinned = c.f.pinned.add(r)
	c.gp = c.gp.add(r)
	return r
}

func (c *pluginARM64Context) AllocVector(exclude ...a64.Reg) a64.Reg {
	r := c.f.allocFReg(arm64ExclusionMask(exclude))
	c.f.fpinned = c.f.fpinned.add(r)
	c.vector = c.vector.add(r)
	return r
}

func (c *pluginARM64Context) ReserveGP(reg a64.Reg) error {
	if gpAllocPos(reg) < 0 || c.f.reserved.has(reg) || c.f.pinnedLocalMask.has(reg) {
		return fmt.Errorf("arm64 plugin cannot reserve GP register %d", reg)
	}
	if c.gp.has(reg) {
		return nil
	}
	c.f.spillIfUsed(reg)
	c.f.pinned = c.f.pinned.add(reg)
	c.gp = c.gp.add(reg)
	return nil
}

func (c *pluginARM64Context) ReserveVector(reg a64.Reg) error {
	if reg >= 32 || c.f.fpinnedLocalMask.has(reg) || c.f.fconstMask().has(reg) || c.f.v128ConstMask().has(reg) {
		return fmt.Errorf("arm64 plugin cannot reserve vector register %d", reg)
	}
	if c.vector.has(reg) {
		return nil
	}
	if user := c.f.fregUser[reg]; user != nil {
		c.f.spillF(user)
	}
	c.f.fpinned = c.f.fpinned.add(reg)
	c.vector = c.vector.add(reg)
	return nil
}

func (c *pluginARM64Context) Release(reg a64.Reg) {
	if c.outputSet && reg == c.output {
		return
	}
	if c.gp.has(reg) {
		c.gp = c.gp.remove(reg)
		c.f.pinned = c.f.pinned.remove(reg)
		c.f.release(reg)
	}
	if c.vector.has(reg) {
		c.vector = c.vector.remove(reg)
		c.f.fpinned = c.f.fpinned.remove(reg)
		c.f.releaseF(reg)
	}
}

func (*pluginARM64Context) MemoryBase() a64.Reg { return linMemReg }

func (c *pluginARM64Context) CheckedMemory(input int, offset uint32, size int) (a64.Reg, a64.Reg, int32, error) {
	if input < 0 || input >= len(c.paramSlots) {
		return 0, 0, 0, fmt.Errorf("arm64 plugin memory input %d out of range", input)
	}
	if size <= 0 {
		return 0, 0, 0, fmt.Errorf("arm64 plugin memory access has invalid size %d", size)
	}
	c.f.pushValue(storage{kind: stSlot, typ: mtI32, slot: c.paramSlots[input]})
	ea, owned, _, disp := c.f.memAddr(offset, size, true)
	if owned {
		c.f.pinned = c.f.pinned.add(ea)
		c.gp = c.gp.add(ea)
	}
	return linMemReg, ea, disp, nil
}

func (c *pluginARM64Context) OutputI32(reg a64.Reg) error {
	if !c.gp.has(reg) {
		return fmt.Errorf("arm64 plugin output register %d is not owned by the lowering", reg)
	}
	if c.outputSet && c.output != reg {
		return fmt.Errorf("arm64 plugin output already assigned")
	}
	c.output, c.outputSet = reg, true
	return nil
}

func (c *pluginARM64Context) finish(resultWidth int32) {
	for r := Reg(0); r < 32; r++ {
		if c.gp.has(r) && (!c.outputSet || r != c.output) {
			c.Release(r)
		}
		if c.vector.has(r) {
			c.Release(r)
		}
	}
	if !c.outputSet {
		return
	}
	c.gp = c.gp.remove(c.output)
	c.f.pinned = c.f.pinned.remove(c.output)
	c.f.release(c.output)
	c.f.pushReg(c.output, mtI32)
	if resultWidth < 32 {
		mask := uint32((uint64(1) << uint(resultWidth)) - 1)
		if !c.f.a.AndImm32(c.output, c.output, mask) {
			tmp := c.f.allocReg(maskOf(c.output))
			c.f.a.MovImm64(tmp, uint64(mask))
			c.f.a.And32(c.output, c.output, tmp)
			c.f.release(tmp)
		}
	}
}

func (f *fn) emitPluginARM64(lowering *machinecode.ARM64Lowering, inputWidths []int32, resultWidth int32, resultCount int) error {
	paramCount := len(inputWidths)
	types := f.currentLogicalTypes()
	if len(types) < paramCount {
		return fmt.Errorf("arm64 plugin lowering has %d stack argument(s), want %d", len(types), paramCount)
	}
	base := len(types) - paramCount
	f.flush()
	roots := f.rootsBottomToTop()
	ctx := &pluginARM64Context{f: f, paramSlots: make([]int, paramCount), paramWidth: inputWidths, output: regNone}
	for i := range ctx.paramSlots {
		e := roots[base+i]
		if e.kind != ekValue || e.st.kind != stSlot || e.st.typ != mtI32 {
			return fmt.Errorf("arm64 plugin input %d is not a canonical i32 slot", i)
		}
		ctx.paramSlots[i] = e.st.slot
	}
	switch lowering.Compatibility {
	case machinecode.ARM64CompatibilityManaged:
		if err := lowering.Managed(ctx); err != nil {
			return err
		}
	case machinecode.ARM64CompatibilityFullAccess:
		if err := lowering.Emit(ctx); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported arm64 plugin compatibility mode %d", lowering.Compatibility)
	}
	if resultCount == 1 && !ctx.outputSet {
		return fmt.Errorf("arm64 plugin lowering did not set its i32 output")
	}
	if resultCount == 0 && ctx.outputSet {
		return fmt.Errorf("void arm64 plugin lowering set an output")
	}
	f.setDepthTypes(types[:base])
	ctx.finish(resultWidth)
	f.stats.call("custom-machine-code")
	return nil
}
