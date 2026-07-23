//go:build amd64

package amd64

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/machinecode"
	x86 "github.com/wago-org/wago/src/core/encoder/amd64"
	coreplugins "github.com/wago-org/wago/src/core/plugins"
)

type pluginAMD64Context struct {
	f            *fn
	paramSlots   []int
	paramWidth   []int32
	paramElems   []*elem
	paramCustom  []coreplugins.CustomType
	customRead   []bool
	gp, ymm      regMask
	output       Reg
	outputSet    bool
	customOutput *coreplugins.CustomType
	customRegs   []Reg
}

func (c *pluginAMD64Context) Encoder() *x86.Asm { return c.f.a }

func (c *pluginAMD64Context) InputI32(index int) (x86.Reg, error) {
	if index < 0 || index >= len(c.paramSlots) {
		return 0, fmt.Errorf("amd64 plugin input %d out of range", index)
	}
	r := c.AllocGP()
	c.f.a.Load64(r, RSP, c.f.spillOff(c.paramSlots[index]))
	if width := c.paramWidth[index]; width < 32 {
		c.f.a.AluRI(4, r, int32((uint64(1)<<uint(width))-1), false)
	}
	return r, nil
}

func (c *pluginAMD64Context) InputCustom(index int) ([]x86.Reg, error) {
	if index < 0 || index >= len(c.paramElems) || index >= len(c.paramCustom) || c.paramCustom[index].IsZero() {
		return nil, fmt.Errorf("amd64 plugin custom input %d out of range", index)
	}
	e := c.paramElems[index]
	want := c.paramCustom[index]
	if e.kind != ekValue || e.st.typ != mtCustom || e.st.custom == nil || !e.st.custom.Equal(want) {
		return nil, fmt.Errorf("amd64 plugin custom input %d has incompatible custom type", index)
	}
	regs := c.f.materializePluginCustom(e)
	out := append([]Reg(nil), regs...)
	for _, reg := range out {
		c.f.fregUser[reg] = nil
		c.f.fpinned = c.f.fpinned.add(reg)
		c.ymm = c.ymm.add(reg)
	}
	e.st.vregs = nil
	c.customRead[index] = true
	return out, nil
}

func exclusionMask(regs []x86.Reg) regMask {
	var m regMask
	for _, r := range regs {
		if r < 16 {
			m = m.add(r)
		}
	}
	return m
}

func (c *pluginAMD64Context) AllocGP(exclude ...x86.Reg) x86.Reg {
	r := c.f.allocReg(exclusionMask(exclude))
	c.f.pinned = c.f.pinned.add(r)
	c.gp = c.gp.add(r)
	return r
}

func (c *pluginAMD64Context) AllocYMM(exclude ...x86.Reg) x86.Reg {
	r := c.f.allocFReg(exclusionMask(exclude))
	c.f.fpinned = c.f.fpinned.add(r)
	c.ymm = c.ymm.add(r)
	return r
}

func (c *pluginAMD64Context) ConstYMMRepeated128(lo, hi uint64) x86.Reg {
	r := c.f.v128ConstReg(lo, hi)
	c.f.fpinned = c.f.fpinned.add(r)
	upper := c.f.v128ConstReg(lo, hi)
	c.f.a.YInsertI128(r, r, upper, 1)
	c.f.releaseF(upper)
	c.ymm = c.ymm.add(r)
	return r
}

func (c *pluginAMD64Context) LoadYMM(input int, offset uint32) (x86.Reg, error) {
	base, index, disp, err := c.CheckedMemory(input, offset, 32)
	if err != nil {
		return 0, err
	}
	x := c.AllocYMM()
	c.f.a.YMovdquLoadIdx(x, base, index, disp)
	c.ReleaseGP(index)
	return x, nil
}

func (c *pluginAMD64Context) StoreYMM(input int, offset uint32, value x86.Reg) error {
	if !c.ymm.has(value) {
		return fmt.Errorf("amd64 plugin YMM register %d is not owned by the lowering", value)
	}
	base, index, disp, err := c.CheckedMemory(input, offset, 32)
	if err != nil {
		return err
	}
	c.f.a.YMovdquStoreIdx(base, index, value, disp)
	c.ReleaseGP(index)
	return nil
}

func (c *pluginAMD64Context) LoadZMM(input int, offset uint32) (x86.Reg, error) {
	base, index, disp, err := c.CheckedMemory(input, offset, 64)
	if err != nil {
		return 0, err
	}
	x := c.AllocYMM()
	c.f.a.ZMovdqu64LoadIdx(x, base, index, disp)
	c.ReleaseGP(index)
	return x, nil
}

func (c *pluginAMD64Context) StoreZMM(input int, offset uint32, value x86.Reg) error {
	if !c.ymm.has(value) {
		return fmt.Errorf("amd64 plugin ZMM register %d is not owned by the lowering", value)
	}
	base, index, disp, err := c.CheckedMemory(input, offset, 64)
	if err != nil {
		return err
	}
	c.f.a.ZMovdqu64StoreIdx(base, index, value, disp)
	c.ReleaseGP(index)
	return nil
}

func (c *pluginAMD64Context) ReserveGP(reg x86.Reg) error {
	if reg >= 16 || reg == RBX || reg == RSP || reg == RBP || c.f.reserved.has(reg) || c.f.pinnedLocalMask.has(reg) {
		return fmt.Errorf("amd64 plugin cannot reserve GP register %d", reg)
	}
	if c.gp.has(reg) {
		return nil
	}
	c.f.spillIfUsed(reg)
	c.f.pinned = c.f.pinned.add(reg)
	c.gp = c.gp.add(reg)
	return nil
}

func (c *pluginAMD64Context) ReserveYMM(reg x86.Reg) error {
	if reg >= 16 || c.f.fpinnedLocalMask.has(reg) || c.f.fconstMask().has(reg) {
		return fmt.Errorf("amd64 plugin cannot reserve YMM register %d", reg)
	}
	if c.ymm.has(reg) {
		return nil
	}
	if user := c.f.fregUser[reg]; user != nil {
		c.f.spillF(user)
	}
	c.f.fpinned = c.f.fpinned.add(reg)
	c.ymm = c.ymm.add(reg)
	return nil
}

func (c *pluginAMD64Context) Release(reg x86.Reg) {
	c.ReleaseGP(reg)
	c.ReleaseVector(reg)
}

func (c *pluginAMD64Context) ReleaseGP(reg x86.Reg) {
	if c.outputSet && reg == c.output {
		return
	}
	if c.gp.has(reg) {
		c.gp = c.gp.remove(reg)
		c.f.pinned = c.f.pinned.remove(reg)
		c.f.release(reg)
	}
}

func (c *pluginAMD64Context) ReleaseVector(reg x86.Reg) {
	for _, output := range c.customRegs {
		if reg == output {
			return
		}
	}
	if c.ymm.has(reg) {
		c.ymm = c.ymm.remove(reg)
		c.f.fpinned = c.f.fpinned.remove(reg)
		c.f.releaseF(reg)
	}
}

func (*pluginAMD64Context) MemoryBase() x86.Reg { return RBX }

func (c *pluginAMD64Context) CheckedMemory(input int, offset uint32, size int) (x86.Reg, x86.Reg, int32, error) {
	if input < 0 || input >= len(c.paramSlots) {
		return 0, 0, 0, fmt.Errorf("amd64 plugin memory input %d out of range", input)
	}
	if size <= 0 {
		return 0, 0, 0, fmt.Errorf("amd64 plugin memory access has invalid size %d", size)
	}
	c.f.pushValue(storage{kind: stSlot, typ: mtI32, slot: c.paramSlots[input]})
	ea, owned, _, disp := c.f.memAddr(offset, size, true)
	if owned {
		c.f.pinned = c.f.pinned.add(ea)
		c.gp = c.gp.add(ea)
	}
	return RBX, ea, disp, nil
}

func (c *pluginAMD64Context) OutputI32(reg x86.Reg) error {
	if c.customOutput != nil {
		return fmt.Errorf("amd64 plugin custom instruction cannot set an i32 output")
	}
	if !c.gp.has(reg) {
		return fmt.Errorf("amd64 plugin output register %d is not owned by the lowering", reg)
	}
	if c.outputSet && c.output != reg {
		return fmt.Errorf("amd64 plugin output already assigned")
	}
	c.output, c.outputSet = reg, true
	return nil
}

func (c *pluginAMD64Context) OutputCustom(regs ...x86.Reg) error {
	if c.customOutput == nil {
		return fmt.Errorf("amd64 plugin instruction has no custom output")
	}
	want := int((c.customOutput.Size() + 31) / 32)
	if len(regs) != want {
		return fmt.Errorf("amd64 plugin custom output has %d register(s), want %d", len(regs), want)
	}
	if c.outputSet || len(c.customRegs) != 0 {
		return fmt.Errorf("amd64 plugin output already assigned")
	}
	seen := regMask(0)
	for _, reg := range regs {
		if !c.ymm.has(reg) || seen.has(reg) {
			return fmt.Errorf("amd64 plugin custom output register %d is not uniquely owned by the lowering", reg)
		}
		seen = seen.add(reg)
	}
	c.customRegs = append([]Reg(nil), regs...)
	return nil
}

func (f *fn) materializePluginCustom(e *elem) []Reg {
	if e.st.kind == stReg {
		return e.st.vregs
	}
	if e.st.kind != stSlot || e.st.custom == nil {
		panic("amd64: cannot materialize custom plugin value")
	}
	count := int((e.st.custom.Size() + 31) / 32)
	regs := make([]Reg, count)
	var avoid regMask
	for i := 0; i < count; i++ {
		reg := f.allocFReg(avoid)
		avoid = avoid.add(reg)
		f.fpinned = f.fpinned.add(reg)
		regs[i] = reg
		f.a.YMovdquLoadDisp(reg, RSP, f.spillOff(e.st.slot+i*4))
	}
	for i := 0; i < count; i++ {
		f.fpinned = f.fpinned.remove(regs[i])
		f.fregUser[regs[i]] = e
	}
	e.st.kind, e.st.typ, e.st.reg = stReg, mtCustom, regs[0]
	e.st.vregs = regs
	return e.st.vregs
}

func (c *pluginAMD64Context) finish(resultWidth int32) {
	for r := Reg(0); r < 16; r++ {
		if c.gp.has(r) && (!c.outputSet || r != c.output) {
			c.ReleaseGP(r)
		}
		if c.ymm.has(r) {
			c.ReleaseVector(r)
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
		c.f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64((uint64(1) << uint(resultWidth)) - 1)})
		c.f.pushBinOp(opAnd, mtI32)
	}
}

func (f *fn) emitPluginAMD64(lowering *machinecode.AMD64Lowering, inputWidths []int32, resultWidth int32, resultCount int, customInputs []coreplugins.CustomType, customOutput *coreplugins.CustomType) error {
	if len(customInputs) != 0 || customOutput != nil {
		return f.emitPluginAMD64Custom(lowering, inputWidths, resultCount, customInputs, customOutput)
	}
	paramCount := len(inputWidths)
	types := f.currentLogicalTypes()
	if len(types) < paramCount {
		return fmt.Errorf("amd64 plugin lowering has %d stack argument(s), want %d", len(types), paramCount)
	}
	base := len(types) - paramCount
	f.flush()
	roots := f.rootsBottomToTop()
	ctx := &pluginAMD64Context{f: f, paramSlots: make([]int, paramCount), paramWidth: inputWidths, output: regNone}
	for i := range ctx.paramSlots {
		e := roots[base+i]
		if e.kind != ekValue || e.st.kind != stSlot || e.st.typ != mtI32 {
			return fmt.Errorf("amd64 plugin input %d is not a canonical i32 slot", i)
		}
		ctx.paramSlots[i] = e.st.slot
	}
	switch lowering.Compatibility {
	case machinecode.AMD64CompatibilityManaged:
		if err := lowering.Managed(ctx); err != nil {
			return err
		}
	case machinecode.AMD64CompatibilityFullAccess:
		if err := lowering.Emit(ctx); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported amd64 plugin compatibility mode %d", lowering.Compatibility)
	}
	if resultCount == 1 && !ctx.outputSet {
		return fmt.Errorf("amd64 plugin lowering did not set its i32 output")
	}
	if resultCount == 0 && ctx.outputSet {
		return fmt.Errorf("void amd64 plugin lowering set an output")
	}
	f.setDepthTypes(types[:base])
	ctx.finish(resultWidth)
	if lowering.Features&(machinecode.AMD64FeatureAVX2|machinecode.AMD64FeatureAVX512) != 0 {
		f.usesWide = true
	}
	f.stats.call("custom-machine-code")
	return nil
}

func (f *fn) emitPluginAMD64Custom(lowering *machinecode.AMD64Lowering, inputWidths []int32, resultCount int, customInputs []coreplugins.CustomType, customOutput *coreplugins.CustomType) error {
	paramCount := len(inputWidths)
	if len(customInputs) != paramCount {
		return fmt.Errorf("amd64 plugin custom signature has %d inputs, want %d", len(customInputs), paramCount)
	}
	roots := append([]*elem(nil), f.rootsBottomToTop()...)
	if len(roots) < paramCount {
		return fmt.Errorf("amd64 plugin lowering has %d stack argument(s), want %d", len(roots), paramCount)
	}
	base := len(roots) - paramCount
	ctx := &pluginAMD64Context{
		f: f, paramSlots: make([]int, paramCount), paramWidth: inputWidths,
		paramElems: roots[base:], paramCustom: customInputs, customRead: make([]bool, paramCount),
		output: regNone, customOutput: customOutput,
	}
	for i, typ := range customInputs {
		e := ctx.paramElems[i]
		if !typ.IsZero() {
			if e.kind != ekValue || e.st.typ != mtCustom || e.st.custom == nil || !e.st.custom.Equal(typ) {
				return fmt.Errorf("amd64 plugin custom input %d has incompatible custom type", i)
			}
			continue
		}
		if e.kind != ekValue || e.st.typ != mtI32 {
			return fmt.Errorf("amd64 plugin input %d is not i32", i)
		}
		r := f.materialize(e)
		f.spill(e)
		f.release(r)
		ctx.paramSlots[i] = e.st.slot
	}
	switch lowering.Compatibility {
	case machinecode.AMD64CompatibilityManaged:
		if err := lowering.Managed(ctx); err != nil {
			return err
		}
	case machinecode.AMD64CompatibilityFullAccess:
		if err := lowering.Emit(ctx); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported amd64 plugin compatibility mode %d", lowering.Compatibility)
	}
	for i, typ := range customInputs {
		if !typ.IsZero() && !ctx.customRead[i] {
			return fmt.Errorf("amd64 plugin lowering did not consume custom input %d", i)
		}
	}
	if customOutput != nil {
		if resultCount != 1 || len(ctx.customRegs) == 0 {
			return fmt.Errorf("amd64 plugin lowering did not set its custom output")
		}
	} else if resultCount != 0 || len(ctx.customRegs) != 0 || ctx.outputSet {
		return fmt.Errorf("amd64 custom plugin lowering has an invalid physical output")
	}
	outputs := make(map[Reg]bool, len(ctx.customRegs))
	for _, reg := range ctx.customRegs {
		outputs[reg] = true
	}
	for reg := Reg(0); reg < 16; reg++ {
		if ctx.gp.has(reg) {
			ctx.ReleaseGP(reg)
		}
		if ctx.ymm.has(reg) && !outputs[reg] {
			ctx.ReleaseVector(reg)
		}
	}
	for _, root := range ctx.paramElems {
		if root.st.typ == mtCustom {
			for _, reg := range root.st.vregs {
				f.releaseF(reg)
			}
		}
		f.erase(root)
	}
	if customOutput != nil {
		st := storage{kind: stReg, typ: mtCustom, reg: ctx.customRegs[0], custom: customOutput, vregs: append([]Reg(nil), ctx.customRegs...)}
		e := f.pushValue(st)
		for _, reg := range ctx.customRegs {
			ctx.ymm = ctx.ymm.remove(reg)
			f.fpinned = f.fpinned.remove(reg)
			f.fregUser[reg] = e
		}
	}
	if lowering.Features&(machinecode.AMD64FeatureAVX2|machinecode.AMD64FeatureAVX512) != 0 {
		f.usesWide = true
	}
	f.stats.call("custom-machine-code-custom")
	return nil
}
