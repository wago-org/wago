package amd64

import (
	"fmt"

	railcore "github.com/wago-org/wago/src/core/compiler/backend/railshot"
	"github.com/wago-org/wago/src/core/compiler/machinecode"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	x86 "github.com/wago-org/wago/src/core/encoder/amd64"
)

type pluginAMD64Context struct {
	f          *fn
	paramSlots []int
	paramWidth []int32
	gp, ymm    regMask
	output     Reg
	outputSet  bool
	wideIndex  [4]Reg
	wideDisp   [4]int32
	wideSize   uint32
	wideReady  bool
	paramConst [4]uint32
	constParam [4]bool
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
	r := c.f.v256Repeated128Const(lo, hi)
	c.f.fpinned = c.f.fpinned.add(r)
	c.ymm = c.ymm.add(r)
	return r
}

func (c *pluginAMD64Context) LoadYMM(input int, offset uint32) (x86.Reg, error) {
	if c.wideReady {
		if input < 0 || input >= len(c.paramSlots) || offset > c.wideSize || c.wideSize-offset < 32 {
			return 0, fmt.Errorf("amd64 plugin wide load input=%d offset=%d exceeds %d bytes", input, offset, c.wideSize)
		}
		x := c.AllocYMM()
		c.f.a.YMovdquLoadIdx(x, RBX, c.wideIndex[input], c.wideDisp[input]+int32(offset))
		return x, nil
	}
	base, index, disp, err := c.CheckedMemory(input, offset, 32)
	if err != nil {
		return 0, err
	}
	x := c.AllocYMM()
	c.f.a.YMovdquLoadIdx(x, base, index, disp)
	c.Release(index)
	return x, nil
}

func (c *pluginAMD64Context) StoreYMM(input int, offset uint32, value x86.Reg) error {
	if !c.ymm.has(value) {
		return fmt.Errorf("amd64 plugin YMM register %d is not owned by the lowering", value)
	}
	if c.wideReady {
		if input < 0 || input >= len(c.paramSlots) || offset > c.wideSize || c.wideSize-offset < 32 {
			return fmt.Errorf("amd64 plugin wide store input=%d offset=%d exceeds %d bytes", input, offset, c.wideSize)
		}
		c.f.a.YMovdquStoreIdx(RBX, c.wideIndex[input], value, c.wideDisp[input]+int32(offset))
		return nil
	}
	base, index, disp, err := c.CheckedMemory(input, offset, 32)
	if err != nil {
		return err
	}
	c.f.a.YMovdquStoreIdx(base, index, value, disp)
	c.Release(index)
	return nil
}

func (c *pluginAMD64Context) LoadZMM(input int, offset uint32) (x86.Reg, error) {
	if !c.wideReady || input < 0 || input >= len(c.paramSlots) || offset > c.wideSize || c.wideSize-offset < 64 {
		return 0, fmt.Errorf("amd64 plugin wide ZMM load input=%d offset=%d exceeds %d bytes", input, offset, c.wideSize)
	}
	x := c.AllocYMM()
	c.f.a.ZMovdqu64LoadIdx(x, RBX, c.wideIndex[input], c.wideDisp[input]+int32(offset))
	return x, nil
}

func (c *pluginAMD64Context) StoreZMM(input int, offset uint32, value x86.Reg) error {
	if !c.ymm.has(value) {
		return fmt.Errorf("amd64 plugin ZMM register %d is not owned by the lowering", value)
	}
	if !c.wideReady || input < 0 || input >= len(c.paramSlots) || offset > c.wideSize || c.wideSize-offset < 64 {
		return fmt.Errorf("amd64 plugin wide ZMM store input=%d offset=%d exceeds %d bytes", input, offset, c.wideSize)
	}
	c.f.a.ZMovdqu64StoreIdx(RBX, c.wideIndex[input], value, c.wideDisp[input]+int32(offset))
	return nil
}

type zmmEncoding struct {
	opcodeMap, pp, op byte
	w, unary          bool
	swap              bool
}

func zmmEncodingFor(sub uint32) (zmmEncoding, bool) {
	e := zmmEncoding{opcodeMap: 1, pp: 1}
	switch sub {
	case 78:
		e.op = 0xdb // vpandd
	case 80:
		e.op = 0xeb // vpord
	case 81:
		e.op = 0xef // vpxord
	case 96:
		e.opcodeMap, e.op, e.unary = 2, 0x1c, true // vpabsb
	case 110:
		e.op = 0xfc // vpaddb
	case 111:
		e.op = 0xec // vpaddsb
	case 112:
		e.op = 0xdc // vpaddusb
	case 113:
		e.op = 0xf8 // vpsubb
	case 114:
		e.op = 0xe8 // vpsubsb
	case 115:
		e.op = 0xd8 // vpsubusb
	case 118:
		e.opcodeMap, e.op = 2, 0x38 // vpminsb
	case 119:
		e.op = 0xda // vpminub
	case 120:
		e.opcodeMap, e.op = 2, 0x3c // vpmaxsb
	case 121:
		e.op = 0xde // vpmaxub
	case 123:
		e.op = 0xe0 // vpavgb
	case 128:
		e.opcodeMap, e.op, e.unary = 2, 0x1d, true // vpabsw
	case 130, 273:
		e.opcodeMap, e.op = 2, 0x0b // vpmulhrsw
	case 142:
		e.op = 0xfd // vpaddw
	case 143:
		e.op = 0xed // vpaddsw
	case 144:
		e.op = 0xdd // vpaddusw
	case 145:
		e.op = 0xf9 // vpsubw
	case 146:
		e.op = 0xe9 // vpsubsw
	case 147:
		e.op = 0xd9 // vpsubusw
	case 149:
		e.op = 0xd5 // vpmullw
	case 150:
		e.op = 0xea // vpminsw
	case 151:
		e.opcodeMap, e.op = 2, 0x3a // vpminuw
	case 152:
		e.op = 0xee // vpmaxsw
	case 153:
		e.opcodeMap, e.op = 2, 0x3e // vpmaxuw
	case 155:
		e.op = 0xe3 // vpavgw
	case 160:
		e.opcodeMap, e.op, e.unary = 2, 0x1e, true // vpabsd
	case 174:
		e.op = 0xfe // vpaddd
	case 177:
		e.op = 0xfa // vpsubd
	case 181:
		e.opcodeMap, e.op = 2, 0x40 // vpmulld
	case 182:
		e.opcodeMap, e.op = 2, 0x39 // vpminsd
	case 183:
		e.opcodeMap, e.op = 2, 0x3b // vpminud
	case 184:
		e.opcodeMap, e.op = 2, 0x3d // vpmaxsd
	case 185:
		e.opcodeMap, e.op = 2, 0x3f // vpmaxud
	case 186:
		e.op = 0xf5 // vpmaddwd
	case 192:
		e.opcodeMap, e.op, e.w, e.unary = 2, 0x1f, true, true // vpabsq
	case 206:
		e.op, e.w = 0xd4, true // vpaddq
	case 209:
		e.op, e.w = 0xfb, true // vpsubq
	case 213:
		e.opcodeMap, e.op, e.w = 2, 0x40, true // vpmullq
	case 227:
		e.pp, e.op, e.unary = 0, 0x51, true // vsqrtps
	case 228:
		e.pp, e.op = 0, 0x58 // vaddps
	case 229:
		e.pp, e.op = 0, 0x5c // vsubps
	case 230:
		e.pp, e.op = 0, 0x59 // vmulps
	case 231:
		e.pp, e.op = 0, 0x5e // vdivps
	case 234:
		e.pp, e.op, e.swap = 0, 0x5d, true // vminps, reversed for pmin
	case 235:
		e.pp, e.op, e.swap = 0, 0x5f, true // vmaxps, reversed for pmax
	case 239:
		e.op, e.w, e.unary = 0x51, true, true // vsqrtpd
	case 240:
		e.op, e.w = 0x58, true // vaddpd
	case 241:
		e.op, e.w = 0x5c, true // vsubpd
	case 242:
		e.op, e.w = 0x59, true // vmulpd
	case 243:
		e.op, e.w = 0x5e, true // vdivpd
	case 246:
		e.op, e.w, e.swap = 0x5d, true, true // vminpd, reversed for pmin
	case 247:
		e.op, e.w, e.swap = 0x5f, true, true // vmaxpd, reversed for pmax
	case 269:
		e.pp, e.op = 0, 0x5d // relaxed vminps
	case 270:
		e.pp, e.op = 0, 0x5f // relaxed vmaxps
	case 271:
		e.op, e.w = 0x5d, true // relaxed vminpd
	case 272:
		e.op, e.w = 0x5f, true // relaxed vmaxpd
	default:
		return zmmEncoding{}, false
	}
	return e, true
}

func (c *pluginAMD64Context) SIMD512ZMM(subopcode uint32, inputs ...x86.Reg) (x86.Reg, error) {
	if subopcode == 82 || subopcode >= 265 && subopcode <= 268 {
		if len(inputs) != 3 {
			return 0, fmt.Errorf("amd64 ZMM bitselect has %d inputs, want 3", len(inputs))
		}
	} else {
		e, ok := zmmEncodingFor(subopcode)
		if !ok {
			return 0, fmt.Errorf("amd64 SIMD opcode %d has no direct ZMM lowering", subopcode)
		}
		want := 2
		if e.unary {
			want = 1
		}
		if len(inputs) != want {
			return 0, fmt.Errorf("amd64 ZMM opcode %d has %d inputs, want %d", subopcode, len(inputs), want)
		}
	}
	for _, input := range inputs {
		if !c.ymm.has(input) {
			return 0, fmt.Errorf("amd64 plugin ZMM register %d is not owned by the lowering", input)
		}
	}
	dst := inputs[0]
	if subopcode == 82 || subopcode >= 265 && subopcode <= 268 {
		// With EVEX operands (dst=a, src1=b, src2=mask), 0xe4 is
		// (a & mask) | (b & ~mask).
		c.f.a.ZPternlogd(dst, inputs[1], inputs[2], 0xe4)
	} else {
		e, _ := zmmEncodingFor(subopcode)
		if e.unary {
			c.f.a.ZSIMDRR(e.opcodeMap, e.pp, e.op, e.w, dst, inputs[0])
		} else {
			x, y := inputs[0], inputs[1]
			if e.swap {
				x, y = y, x
			}
			c.f.a.ZSIMDRRR(e.opcodeMap, e.pp, e.op, e.w, dst, x, y)
		}
	}
	for _, input := range inputs[1:] {
		c.Release(input)
	}
	return dst, nil
}

// prepareWideMemory validates every pointer for the complete vector width once
// and retains its effective address. Subsequent chunk loads/stores need neither
// another pointer reload nor another bounds check.
func (c *pluginAMD64Context) prepareWideMemory(size uint32) error {
	if len(c.paramSlots) > len(c.wideIndex) {
		return fmt.Errorf("amd64 plugin wide SIMD has %d pointers, max %d", len(c.paramSlots), len(c.wideIndex))
	}
	for input := range c.paramSlots {
		if c.constParam[input] && railcore.ConstantMemoryRangeInMinimum(c.f.m, c.paramConst[input], size) {
			index := c.AllocGP()
			c.f.a.MovImm32(index, int32(c.paramConst[input]))
			c.wideIndex[input] = index
			continue
		}
		_, index, disp, err := c.CheckedMemory(input, 0, int(size))
		if err != nil {
			return err
		}
		c.wideIndex[input], c.wideDisp[input] = index, disp
	}
	c.wideSize, c.wideReady = size, true
	return nil
}

func (c *pluginAMD64Context) SIMD256YMM(subopcode uint32, immediate []byte, inputs ...x86.Reg) (x86.Reg, error) {
	var consumed regMask
	for _, input := range inputs {
		if !c.ymm.has(input) {
			return 0, fmt.Errorf("amd64 plugin YMM register %d is not owned by the lowering", input)
		}
		if consumed.has(input) {
			return 0, fmt.Errorf("amd64 plugin SIMD operation reuses consumed YMM register %d", input)
		}
		consumed = consumed.add(input)
		c.ymm = c.ymm.remove(input)
		c.f.fpinned = c.f.fpinned.remove(input)
		c.f.pushYReg(input)
	}
	r := wasm.NewReader(immediate)
	if err := c.f.emitV256Mirror(subopcode, r); err != nil {
		return 0, err
	}
	if r.HasNext() {
		return 0, fmt.Errorf("amd64 plugin SIMD operation left %d immediate byte(s)", r.BytesLeft())
	}
	result := c.f.popValue()
	if result.st.typ != mtV256 {
		return 0, fmt.Errorf("amd64 plugin SIMD operation %d did not produce a YMM value", subopcode)
	}
	x := c.f.loadV256(result)
	c.f.fregUser[x] = nil
	c.f.fpinned = c.f.fpinned.add(x)
	c.ymm = c.ymm.add(x)
	return x, nil
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
	if c.outputSet && reg == c.output {
		return
	}
	if c.gp.has(reg) {
		c.gp = c.gp.remove(reg)
		c.f.pinned = c.f.pinned.remove(reg)
		c.f.release(reg)
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
	if !c.gp.has(reg) {
		return fmt.Errorf("amd64 plugin output register %d is not owned by the lowering", reg)
	}
	if c.outputSet && c.output != reg {
		return fmt.Errorf("amd64 plugin output already assigned")
	}
	c.output, c.outputSet = reg, true
	return nil
}

func (c *pluginAMD64Context) finish(resultWidth int32) {
	for r := Reg(0); r < 16; r++ {
		if c.gp.has(r) && (!c.outputSet || r != c.output) {
			c.Release(r)
		}
		if c.ymm.has(r) {
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
		c.f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64((uint64(1) << uint(resultWidth)) - 1)})
		c.f.pushBinOp(opAnd, mtI32)
	}
}

func (f *fn) emitPluginAMD64(lowering *machinecode.AMD64Lowering, inputWidths []int32, resultWidth int32, resultCount int) error {
	paramCount := len(inputWidths)
	types := f.currentLogicalTypes()
	if len(types) < paramCount {
		return fmt.Errorf("amd64 plugin lowering has %d stack argument(s), want %d", len(types), paramCount)
	}
	base := len(types) - paramCount
	roots := f.rootsBottomToTop()
	var paramConst [4]uint32
	var constParam [4]bool
	for i := 0; i < paramCount && i < len(paramConst); i++ {
		e := roots[base+i]
		if e.kind == ekValue && e.st.kind == stConst && e.st.typ == mtI32 {
			paramConst[i], constParam[i] = uint32(e.st.cval), true
		}
	}
	f.flush()
	roots = f.rootsBottomToTop()
	ctx := &pluginAMD64Context{f: f, paramSlots: make([]int, paramCount), paramWidth: inputWidths, output: regNone}
	ctx.paramConst, ctx.constParam = paramConst, constParam
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

func (f *fn) emitCustomSIMD(simd *CustomSIMDInstruction, inputWidths []int32, ft *wasm.CompType) error {
	_, directZMM := zmmEncodingFor(simd.Subopcode)
	directZMM = directZMM || simd.Subopcode == 82 || simd.Subopcode >= 265 && simd.Subopcode <= 268
	useZMM := simd.Width == 512 && directZMM && canUseZMM(simd.Subopcode)
	features := machinecode.AMD64FeatureAVX2
	if useZMM {
		features = machinecode.AMD64FeatureAVX512
	}
	lowering := &machinecode.AMD64Lowering{
		Compatibility: machinecode.AMD64CompatibilityManaged,
		Features:      features,
		Managed: func(ctx machinecode.AMD64ManagedContext) error {
			native := ctx.(*pluginAMD64Context)
			if err := native.prepareWideMemory(uint32(simd.Width / 8)); err != nil {
				return err
			}
			if useZMM {
				var inputs [3]Reg
				for i := 0; i < int(simd.Arity); i++ {
					x, err := ctx.LoadZMM(i+1, 0)
					if err != nil {
						return err
					}
					inputs[i] = x
				}
				result, err := ctx.SIMD512ZMM(simd.Subopcode, inputs[:simd.Arity]...)
				if err != nil {
					return err
				}
				if err := ctx.StoreZMM(0, 0, result); err != nil {
					return err
				}
				ctx.Release(result)
				return nil
			}
			for offset := uint32(0); offset < uint32(simd.Width/8); offset += 32 {
				var inputs [3]Reg
				for i := 0; i < int(simd.Arity); i++ {
					x, err := ctx.LoadYMM(i+1, offset)
					if err != nil {
						return err
					}
					inputs[i] = x
				}
				result, err := ctx.SIMD256YMM(simd.Subopcode, nil, inputs[:simd.Arity]...)
				if err != nil {
					return err
				}
				if err := ctx.StoreYMM(0, offset, result); err != nil {
					return err
				}
				ctx.Release(result)
			}
			return nil
		},
	}
	return f.emitPluginAMD64(lowering, inputWidths, 0, len(ft.Results))
}
