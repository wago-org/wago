//go:build amd64

package amd64

import (
	"fmt"
	"math"

	compilerir "github.com/wago-org/wago/src/core/compiler/backend/railshot/straightline"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func (f *fn) prepareStraightLineSSA(funcIdx int, code *wasm.Func, hints funcHints) {
	if !straightLineSSAEnabled || !f.guardMode || len(code.BodyBytes) < 1024 || len(code.BodyBytes) > maxIntervalRegionBody ||
		hints.hasCall || hints.hasControlFlow || hints.usesBulkMem || f.moduleEH || len(f.ft.Results) != 0 {
		return
	}
	irf, err := compilerir.BuildFunc(f.m, funcIdx)
	if err != nil || !straightLineSSASupported(irf) {
		return
	}
	plan := compilerir.BuildStraightLinePlan(irf)
	if plan == nil {
		return
	}
	f.ssaFunc, f.ssaPlan = irf, plan
	f.stats.peep("straightline-ssa")
}

func straightLineSSASupported(irf *compilerir.Func) bool {
	if irf == nil {
		return false
	}
	seenStore := false
	for i := range irf.Insts {
		in := &irf.Insts[i]
		switch in.Op {
		case compilerir.OpConst, compilerir.OpLocalGet, compilerir.OpLocalSet, compilerir.OpLocalTee:
		case compilerir.OpIBinary:
			kind := compilerir.IBinaryOp(uint8(in.Aux))
			if kind != compilerir.IBinAdd && kind != compilerir.IBinSub && kind != compilerir.IBinMul &&
				kind != compilerir.IBinAnd && kind != compilerir.IBinOr && kind != compilerir.IBinXor &&
				kind != compilerir.IBinShl && kind != compilerir.IBinShrS && kind != compilerir.IBinShrU &&
				kind != compilerir.IBinRotl && kind != compilerir.IBinRotr {
				return false
			}
			if kind >= compilerir.IBinShl {
				right := irf.ValueIDs[in.Args.Start+1]
				d := irf.Values[right]
				if d.DefKind != compilerir.ValueDefInst || irf.Insts[d.Def].Op != compilerir.OpConst {
					return false
				}
			}
		case compilerir.OpConvert:
			if compilerir.ConvertOp(uint8(in.Aux)) != compilerir.ConvWrapI64ToI32 {
				return false
			}
		case compilerir.OpLoad:
			if !straightLineIntegerLoad(compilerir.MemOp(uint8(in.Aux))) || uint32(in.Aux>>32) > math.MaxInt32 {
				return false
			}
		case compilerir.OpStore:
			seenStore = true
			if !straightLineIntegerStore(compilerir.MemOp(uint8(in.Aux))) || uint32(in.Aux>>32) > math.MaxInt32 {
				return false
			}
		default:
			return false
		}
	}
	return seenStore
}

func straightLineIntegerLoad(k compilerir.MemOp) bool {
	return k == compilerir.MemI32 || k == compilerir.MemI64 ||
		(k >= compilerir.MemI32Load8S && k <= compilerir.MemI64Load32U)
}

func straightLineIntegerStore(k compilerir.MemOp) bool {
	return k == compilerir.MemI32 || k == compilerir.MemI64 ||
		(k >= compilerir.MemI32Store8 && k <= compilerir.MemI64Store32)
}

type ssaValueLoc struct {
	kind uint8 // 0 absent, 1 GPR, 2 spill slot
	reg  Reg
	slot int
}

type straightLineSSACompiler struct {
	f    *fn
	irf  *compilerir.Func
	plan *compilerir.StraightLinePlan

	values   []ssaValueLoc
	gpOwner  [16]compilerir.ValueID
	gpPinned regMask

	scalarUses    []int
	useDefs       []int
	useStart      []int
	useCursor     []int
	markedScalar  []bool
	initialLocal  map[compilerir.ValueID]int
	nextSlot      int
	savedReserved []ssaSavedReg
}

type ssaSavedReg struct {
	reg  Reg
	slot int
}

func (f *fn) emitStraightLineSSA() error {
	c := &straightLineSSACompiler{f: f, irf: f.ssaFunc, plan: f.ssaPlan}
	c.values = make([]ssaValueLoc, len(c.irf.Values))
	c.scalarUses = make([]int, len(c.irf.Values))
	c.useStart = make([]int, len(c.irf.Values)+1)
	c.useCursor = make([]int, len(c.irf.Values))
	c.markedScalar = make([]bool, len(c.irf.Values))
	c.initialLocal = make(map[compilerir.ValueID]int, len(c.plan.InitialLocals))
	for r := range c.gpOwner {
		c.gpOwner[r] = compilerir.InvalidValue
	}
	for _, r := range gpAlloc {
		if f.reserved.has(r) {
			slot := c.allocSlots(1)
			f.a.Store64(RSP, f.spillOff(slot), r)
			c.savedReserved = append(c.savedReserved, ssaSavedReg{reg: r, slot: slot})
		}
	}
	for local, value := range c.plan.InitialLocals {
		if value != compilerir.InvalidValue {
			c.initialLocal[c.plan.Resolve(value)] = local
		}
	}
	for i := range c.irf.Insts {
		in := &c.irf.Insts[i]
		switch in.Op {
		case compilerir.OpIBinary, compilerir.OpConvert, compilerir.OpLoad, compilerir.OpStore:
			for arg := uint32(0); arg < in.Args.Len; arg++ {
				v := c.arg(in, arg)
				c.useStart[int(v)+1]++
			}
		}
	}
	for i := 1; i < len(c.useStart); i++ {
		c.useStart[i] += c.useStart[i-1]
	}
	c.useDefs = make([]int, c.useStart[len(c.useStart)-1])
	fill := append([]int(nil), c.useStart[:len(c.irf.Values)]...)
	copy(c.useCursor, fill)
	for i := range c.irf.Insts {
		in := &c.irf.Insts[i]
		switch in.Op {
		case compilerir.OpIBinary, compilerir.OpConvert, compilerir.OpLoad, compilerir.OpStore:
			for arg := uint32(0); arg < in.Args.Len; arg++ {
				v := c.arg(in, arg)
				c.useDefs[fill[v]] = i
				fill[v]++
			}
		}
	}
	for i := range c.irf.Insts {
		in := &c.irf.Insts[i]
		switch in.Op {
		case compilerir.OpLoad:
			c.markScalarRef(c.arg(in, 0))
		case compilerir.OpStore:
			c.markScalarRef(c.arg(in, 0))
			c.markScalarRef(c.arg(in, 1))
		}
	}
	for i := range c.irf.Insts {
		switch c.irf.Insts[i].Op {
		case compilerir.OpLoad:
			if err := c.emitLoad(&c.irf.Insts[i]); err != nil {
				return err
			}
		case compilerir.OpStore:
			if err := c.emitStore(&c.irf.Insts[i]); err != nil {
				return err
			}
		}
	}
	for _, saved := range c.savedReserved {
		f.a.Load64(saved.reg, RSP, f.spillOff(saved.slot))
	}
	f.stats.peep("straightline-ssa-emitted")
	return nil
}

func (c *straightLineSSACompiler) arg(in *compilerir.Inst, i uint32) compilerir.ValueID {
	return c.plan.Resolve(c.irf.ValueIDs[in.Args.Start+i])
}

func (c *straightLineSSACompiler) result(in *compilerir.Inst) compilerir.ValueID {
	return c.plan.Resolve(c.irf.ValueIDs[in.Results.Start])
}

func (c *straightLineSSACompiler) inst(v compilerir.ValueID) (*compilerir.Inst, bool) {
	v = c.plan.Resolve(v)
	if v == compilerir.InvalidValue || int(v) >= len(c.irf.Values) {
		return nil, false
	}
	d := c.irf.Values[v]
	if d.DefKind != compilerir.ValueDefInst || int(d.Def) >= len(c.irf.Insts) {
		return nil, false
	}
	return &c.irf.Insts[d.Def], true
}

func (c *straightLineSSACompiler) markScalarRef(v compilerir.ValueID) {
	v = c.plan.Resolve(v)
	if v == compilerir.InvalidValue {
		return
	}
	c.scalarUses[v]++
	if c.markedScalar[v] {
		return
	}
	c.markedScalar[v] = true
	in, ok := c.inst(v)
	if !ok {
		return
	}
	switch in.Op {
	case compilerir.OpIBinary, compilerir.OpConvert:
		for i := uint32(0); i < in.Args.Len; i++ {
			c.markScalarRef(c.arg(in, i))
		}
	case compilerir.OpLoad:
		c.markScalarRef(c.arg(in, 0))
	}
}

func (c *straightLineSSACompiler) emitLoad(in *compilerir.Inst) error {
	addrID := c.arg(in, 0)
	addr, err := c.scalar(addrID)
	if err != nil {
		return err
	}
	c.f.a.MovRegReg32(addr, addr)
	dst := addr
	if !c.canReuseScalar(addrID, addr) {
		dst = c.allocGP(maskOf(addr))
	}
	size, signed, wide := straightLineLoadShape(compilerir.MemOp(uint8(in.Aux)))
	c.f.a.LoadIdx(dst, RBX, addr, int32(uint32(in.Aux>>32)), size, signed, wide)
	v := c.result(in)
	c.consumeScalar(addrID)
	c.values[v] = ssaValueLoc{kind: 1, reg: dst}
	c.gpOwner[dst] = v
	return nil
}

func (c *straightLineSSACompiler) emitStore(in *compilerir.Inst) error {
	addr, err := c.scalar(c.arg(in, 0))
	if err != nil {
		return err
	}
	savedPinned := c.gpPinned
	c.gpPinned = c.gpPinned.add(addr)
	value, err := c.scalar(c.arg(in, 1))
	c.gpPinned = savedPinned
	if err != nil {
		return err
	}
	c.f.a.MovRegReg32(addr, addr)
	c.f.a.StoreIdx(RBX, addr, value, int32(uint32(in.Aux>>32)), straightLineStoreSize(compilerir.MemOp(uint8(in.Aux))))
	c.consumeScalar(c.arg(in, 0))
	c.consumeScalar(c.arg(in, 1))
	return nil
}

func (c *straightLineSSACompiler) scalar(v compilerir.ValueID) (Reg, error) {
	v = c.plan.Resolve(v)
	if loc := c.values[v]; loc.kind == 1 {
		if c.gpOwner[loc.reg] == v {
			return loc.reg, nil
		}
		c.values[v].kind = 0
	} else if loc.kind == 2 {
		r := c.allocGP(0)
		c.f.a.Load64(r, RSP, c.f.spillOff(loc.slot))
		c.values[v] = ssaValueLoc{kind: 1, reg: r, slot: loc.slot}
		c.gpOwner[r] = v
		return r, nil
	}
	if local, ok := c.initialLocal[v]; ok {
		r := c.allocGP(0)
		c.f.a.Load64(r, RSP, c.f.localOff(local))
		c.values[v] = ssaValueLoc{kind: 1, reg: r}
		c.gpOwner[r] = v
		return r, nil
	}
	in, ok := c.inst(v)
	if !ok {
		return regNone, fmt.Errorf("amd64: straight-line SSA value %d has no definition", v)
	}
	switch in.Op {
	case compilerir.OpConst:
		r := c.allocGP(0)
		if c.irf.Values[v].Type == wasm.I64 {
			c.f.a.MovImm64(r, in.Aux)
		} else {
			c.f.a.MovImm32(r, int32(in.Aux))
		}
		c.values[v] = ssaValueLoc{kind: 1, reg: r}
		c.gpOwner[r] = v
		return r, nil
	case compilerir.OpIBinary:
		return c.scalarBinary(v, in)
	case compilerir.OpConvert:
		srcID := c.arg(in, 0)
		src, err := c.scalar(srcID)
		if err != nil {
			return regNone, err
		}
		dst := src
		if !c.canReuseScalar(srcID, src) {
			dst = c.allocGP(maskOf(src))
		}
		c.f.a.MovRegReg32(dst, src)
		c.consumeScalar(srcID)
		c.values[v] = ssaValueLoc{kind: 1, reg: dst}
		c.gpOwner[dst] = v
		return dst, nil
	}
	return regNone, fmt.Errorf("amd64: unsupported straight-line SSA value %d op %d", v, in.Op)
}

func (c *straightLineSSACompiler) scalarBinary(v compilerir.ValueID, in *compilerir.Inst) (Reg, error) {
	leftID, rightID := c.arg(in, 0), c.arg(in, 1)
	left, err := c.scalar(leftID)
	if err != nil {
		return regNone, err
	}
	kind := compilerir.IBinaryOp(uint8(in.Aux))
	wide := c.irf.Values[v].Type == wasm.I64
	if kind == compilerir.IBinShl || kind == compilerir.IBinShrS || kind == compilerir.IBinShrU || kind == compilerir.IBinRotl || kind == compilerir.IBinRotr {
		if ri, ok := c.inst(rightID); ok && ri.Op == compilerir.OpConst {
			dst := left
			reuse := c.canReuseScalar(leftID, left)
			if !reuse {
				dst = c.allocGP(maskOf(left))
			}
			count := byte(ri.Aux)
			if wide {
				count &= 63
			} else {
				count &= 31
			}
			if kind == compilerir.IBinRotr && bmi2RorxEnabled {
				c.f.a.Rorx(dst, left, count, wide)
			} else {
				if !reuse {
					if wide {
						c.f.a.MovReg64(dst, left)
					} else {
						c.f.a.MovRegReg32(dst, left)
					}
				}
				digit := byte(1)
				switch kind {
				case compilerir.IBinShl:
					digit = 4
				case compilerir.IBinShrS:
					digit = 7
				case compilerir.IBinShrU:
					digit = 5
				case compilerir.IBinRotl:
					digit = 0
				}
				c.f.a.ShiftImm(digit, dst, count, wide)
			}
			c.consumeScalar(leftID)
			c.consumeScalar(rightID)
			c.values[v] = ssaValueLoc{kind: 1, reg: dst}
			c.gpOwner[dst] = v
			return dst, nil
		}
	}
	savedPinned := c.gpPinned
	var right Reg
	// Reserving more module-global registers leaves fewer long-lived values in
	// the ordinary allocator before this tier saves them. Scale the recursion
	// spine accordingly: one saved register benefits from switching at depth 6,
	// two at depth 7, while still leaving a broad scratch floor.
	if c.gpPinned.count() >= 5+len(c.savedReserved) {
		// Do not extend an already-deep left spine until every allocatable GPR is
		// pinned. The value is recoverable from its SSA definition or spill; form
		// the right side first, then recover the left while protecting the right.
		right, err = c.scalar(rightID)
		if err == nil {
			c.gpPinned = c.gpPinned.add(right)
			left, err = c.scalar(leftID)
		}
		c.gpPinned = savedPinned
	} else {
		c.gpPinned = c.gpPinned.add(left)
		right, err = c.scalar(rightID)
		c.gpPinned = savedPinned
	}
	if err != nil {
		return regNone, err
	}
	dst, lhs, rhs := left, left, right
	if !c.canReuseScalar(leftID, left) {
		if kind != compilerir.IBinSub && c.canReuseScalar(rightID, right) {
			dst, lhs, rhs = right, right, left
		} else {
			dst = c.allocGP(maskOf(left, right))
			if wide {
				c.f.a.MovReg64(dst, left)
			} else {
				c.f.a.MovRegReg32(dst, left)
			}
			lhs = dst
		}
	}
	opcode := byte(0)
	switch kind {
	case compilerir.IBinAdd:
		opcode = 0x01
	case compilerir.IBinSub:
		opcode = 0x29
	case compilerir.IBinAnd:
		opcode = 0x21
	case compilerir.IBinOr:
		opcode = 0x09
	case compilerir.IBinXor:
		opcode = 0x31
	case compilerir.IBinMul:
		c.f.a.IMul(lhs, rhs, wide)
	default:
		return regNone, fmt.Errorf("amd64: unsupported straight-line SSA binary %d", kind)
	}
	if kind != compilerir.IBinMul {
		c.f.a.AluRR(opcode, lhs, rhs, wide)
	}
	c.consumeScalar(leftID)
	c.consumeScalar(rightID)
	c.values[v] = ssaValueLoc{kind: 1, reg: dst}
	c.gpOwner[dst] = v
	return dst, nil
}

func straightLineLoadShape(k compilerir.MemOp) (size int, signed, wide bool) {
	switch k {
	case compilerir.MemI32:
		return 4, false, false
	case compilerir.MemI64:
		return 8, false, true
	case compilerir.MemI32Load8S:
		return 1, true, false
	case compilerir.MemI32Load8U:
		return 1, false, false
	case compilerir.MemI32Load16S:
		return 2, true, false
	case compilerir.MemI32Load16U:
		return 2, false, false
	case compilerir.MemI64Load8S:
		return 1, true, true
	case compilerir.MemI64Load8U:
		return 1, false, true
	case compilerir.MemI64Load16S:
		return 2, true, true
	case compilerir.MemI64Load16U:
		return 2, false, true
	case compilerir.MemI64Load32S:
		return 4, true, true
	case compilerir.MemI64Load32U:
		return 4, false, true
	default:
		panic(fmt.Sprintf("unsupported straight-line load %d", k))
	}
}

func straightLineStoreSize(k compilerir.MemOp) int {
	switch k {
	case compilerir.MemI32, compilerir.MemI64Store32:
		return 4
	case compilerir.MemI64:
		return 8
	case compilerir.MemI32Store8, compilerir.MemI64Store8:
		return 1
	case compilerir.MemI32Store16, compilerir.MemI64Store16:
		return 2
	default:
		panic(fmt.Sprintf("unsupported straight-line store %d", k))
	}
}

func (c *straightLineSSACompiler) canReuseScalar(v compilerir.ValueID, r Reg) bool {
	v = c.plan.Resolve(v)
	return c.scalarUses[v] == 1 && c.values[v].kind == 1 && c.values[v].reg == r && c.gpOwner[r] == v && !c.gpPinned.has(r)
}

func (c *straightLineSSACompiler) consumeScalar(v compilerir.ValueID) {
	v = c.plan.Resolve(v)
	if c.scalarUses[v] <= 0 {
		return
	}
	c.scalarUses[v]--
	if c.useCursor[v] < c.useStart[int(v)+1] {
		c.useCursor[v]++
	}
	if c.scalarUses[v] == 0 && c.values[v].kind == 1 {
		c.gpOwner[c.values[v].reg] = compilerir.InvalidValue
		c.values[v].kind = 0
	}
}

func (c *straightLineSSACompiler) allocGP(avoid regMask) Reg {
	// Module-global pins are saved once around the region, so their registers are
	// available to the scheduler while the wasm-to-wasm ABI remains intact.
	avoid = avoid.union(c.gpPinned)
	for _, r := range gpAlloc {
		if !avoid.has(r) && c.gpOwner[r] == compilerir.InvalidValue {
			return r
		}
	}
	victim := compilerir.InvalidValue
	best := -1
	for _, r := range gpAlloc {
		v := c.gpOwner[r]
		if avoid.has(r) || v == compilerir.InvalidValue {
			continue
		}
		next := int(^uint(0) >> 1)
		if at := c.useCursor[v]; at < c.useStart[int(v)+1] {
			next = c.useDefs[at]
		}
		if next > best {
			best, victim = next, v
		}
	}
	if victim == compilerir.InvalidValue {
		panic(fmt.Sprintf("straight-line SSA GPR exhausted: avoid=%#x pinned=%#x", avoid, c.gpPinned))
	}
	r := c.values[victim].reg
	slot := c.allocSlots(1)
	c.f.a.Store64(RSP, c.f.spillOff(slot), r)
	c.values[victim] = ssaValueLoc{kind: 2, slot: slot}
	c.gpOwner[r] = compilerir.InvalidValue
	return r
}

func (c *straightLineSSACompiler) allocSlots(n int) int {
	slot := c.nextSlot
	c.nextSlot += n
	if c.nextSlot > c.f.maxSpill {
		c.f.maxSpill = c.nextSlot
	}
	return slot
}
