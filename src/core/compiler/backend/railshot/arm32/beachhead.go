// Package arm32 contains the cross-host Thumb-2 railshot backend beachhead for
// 32-bit Armv8-M Mainline cores. It is intentionally strict and admits only the
// i32/control subset listed by CompileBeachhead; unsupported value types and
// opcodes are rejected before this package is used as a public backend.
package arm32

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	a32 "github.com/wago-org/wago/src/core/encoder/arm32"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

var scratchRegs = []a32.Reg{a32.R0, a32.R1, a32.R2, a32.R3}
var localRegs = []a32.Reg{a32.R4, a32.R5, a32.R6, a32.R7, a32.R8, a32.R9, a32.R10, a32.R11}

const zeroReg = a32.R12

type operand struct {
	constant bool
	reg      a32.Reg
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
	a       a32.Asm
	stack   []operand
	free    []a32.Reg
	locals  []a32.Reg
	control []*controlFrame
	context bool
}

// CompileBeachhead lowers one validated-style Wasm function body through a
// compact temporary ABI: up to four i32 parameters arrive in R0..R3 and one i32
// result returns in R0. Locals are held in R4..R11; R12 is a permanent zero
// register used to synthesize comparisons and branches without relying on
// architecture-specific condition values in higher layers.
func CompileBeachhead(numParams int, body []byte) ([]byte, error) {
	return compileBeachhead(numParams, body, false)
}

func compileModuleBeachhead(numParams int, body []byte) ([]byte, error) {
	return compileBeachhead(numParams, body, true)
}

func compileBeachhead(numParams int, body []byte, context bool) ([]byte, error) {
	c := &compiler{free: append([]a32.Reg(nil), scratchRegs...), context: context}
	if !c.a.MovImm32(zeroReg, 0) {
		panic("arm32: cannot establish zero register")
	}
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
		if t != 0x7f {
			return nil, fmt.Errorf("arm32 beachhead supports only i32 locals, got %#x", t)
		}
		declared += int(n)
	}
	total := numParams + declared
	if numParams < 0 || numParams > 4 {
		return nil, fmt.Errorf("arm32 beachhead supports 0..4 parameters, got %d", numParams)
	}
	availableLocals := localRegs
	if context {
		availableLocals = localRegs[:len(localRegs)-1] // R11 carries *ContextABI.
	}
	if total > len(availableLocals) {
		return nil, fmt.Errorf("arm32 beachhead supports up to %d locals, got %d", len(availableLocals), total)
	}
	c.locals = append(c.locals, availableLocals[:total]...)
	for i := 0; i < numParams; i++ {
		c.must(c.a.MovReg(c.locals[i], a32.R0+a32.Reg(i)), "parameter move")
	}
	for i := numParams; i < total; i++ {
		c.must(c.a.MovImm32(c.locals[i], 0), "local zero")
	}
	if context {
		if err := c.pollCancellation(); err != nil {
			return nil, err
		}
	}
	c.control = []*controlFrame{{function: true, elseSite: -1}}

	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return nil, err
		}
		switch op {
		case 0x00:
			if !c.context {
				return nil, fmt.Errorf("arm32 beachhead unreachable requires module context")
			}
			c.emitContextTrap(embedded32.TrapUnreachable)
		case 0x02:
			if err := readVoidBlockType(r); err != nil {
				return nil, err
			}
			c.control = append(c.control, &controlFrame{elseSite: -1})
		case 0x03:
			if err := readVoidBlockType(r); err != nil {
				return nil, err
			}
			header := c.a.Len()
			if c.context {
				if err := c.pollCancellation(); err != nil {
					return nil, err
				}
			}
			c.control = append(c.control, &controlFrame{loop: true, header: header, elseSite: -1})
		case 0x04:
			if err := readVoidBlockType(r); err != nil {
				return nil, err
			}
			cond := c.materialize(c.pop())
			c.must(c.a.Cmp(cond, zeroReg), "if compare")
			site := c.a.FarBcond(a32.CondEQ)
			c.release(cond)
			c.control = append(c.control, &controlFrame{ifBlock: true, elseSite: site})
		case 0x05:
			fr, err := c.topControl()
			if err != nil || !fr.ifBlock || fr.elseSite < 0 {
				return nil, fmt.Errorf("unexpected else")
			}
			fr.pending = append(fr.pending, c.a.Branch())
			if !c.a.PatchFarBranch(fr.elseSite, c.a.Len()) {
				return nil, fmt.Errorf("if else target out of range")
			}
			fr.elseSite = -1
		case 0x0b:
			fr, err := c.topControl()
			if err != nil {
				return nil, err
			}
			c.control = c.control[:len(c.control)-1]
			if fr.function {
				c.emitReturn()
				c.a.Align4()
				return c.a.B, nil
			}
			here := c.a.Len()
			for _, site := range fr.pending {
				if !c.a.PatchBranch(site, here) {
					return nil, fmt.Errorf("control target out of range")
				}
			}
			if fr.ifBlock && fr.elseSite >= 0 && !c.a.PatchFarBranch(fr.elseSite, here) {
				return nil, fmt.Errorf("if end target out of range")
			}
		case 0x0c:
			if err := c.branch(r, false); err != nil {
				return nil, err
			}
		case 0x0d:
			if err := c.branch(r, true); err != nil {
				return nil, err
			}
		case 0x0f:
			c.emitReturn()
		case 0x20:
			idx, err := r.U32()
			if err != nil || int(idx) >= len(c.locals) {
				return nil, fmt.Errorf("local.get index %d", idx)
			}
			dst := c.alloc()
			c.must(c.a.MovReg(dst, c.locals[idx]), "local.get")
			c.push(operand{reg: dst})
		case 0x21, 0x22:
			idx, err := r.U32()
			if err != nil || int(idx) >= len(c.locals) {
				return nil, fmt.Errorf("local index %d", idx)
			}
			value := c.materialize(c.pop())
			c.must(c.a.MovReg(c.locals[idx], value), "local.set")
			if op == 0x22 {
				c.push(operand{reg: value})
			} else {
				c.release(value)
			}
		case 0x41:
			v, err := r.I32()
			if err != nil {
				return nil, err
			}
			c.push(operand{constant: true, value: v})
		case 0x45:
			c.eqz()
		case 0x28, 0x2c, 0x2d, 0x2e, 0x2f:
			if !c.context {
				return nil, fmt.Errorf("arm32 beachhead memory operation requires module context")
			}
			if err := c.load(r, op); err != nil {
				return nil, err
			}
		case 0x36, 0x3a, 0x3b:
			if !c.context {
				return nil, fmt.Errorf("arm32 beachhead memory operation requires module context")
			}
			if err := c.store(r, op); err != nil {
				return nil, err
			}
		case 0x3f, 0x40:
			if !c.context {
				return nil, fmt.Errorf("arm32 beachhead memory operation requires module context")
			}
			reserved, err := r.Byte()
			if err != nil || reserved != 0 {
				return nil, fmt.Errorf("arm32: invalid memory size/grow immediate")
			}
			if op == 0x3f {
				c.memorySize()
			} else if err := c.memoryGrow(); err != nil {
				return nil, err
			}
		case 0x67, 0x68, 0x69:
			if err := c.countBits(op); err != nil {
				return nil, err
			}
		case 0x46, 0x47, 0x48, 0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f:
			c.compare(op)
		case 0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78:
			c.binary(op)
		case 0x6d, 0x6e, 0x6f, 0x70:
			if !c.context {
				return nil, fmt.Errorf("arm32 beachhead division requires module context")
			}
			if err := c.divRem(op); err != nil {
				return nil, err
			}
		case 0xc0, 0xc1:
			c.signExtend(op)
		default:
			return nil, fmt.Errorf("arm32 beachhead unsupported opcode %#x", op)
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
		return fmt.Errorf("arm32 beachhead supports only void block type, got %#x", bt)
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
			site := c.a.Branch()
			if !c.a.PatchBranch(site, fr.header) {
				return fmt.Errorf("loop target out of range")
			}
		default:
			fr.pending = append(fr.pending, c.a.Branch())
		}
		return nil
	}
	cond := c.materialize(c.pop())
	c.must(c.a.Cmp(cond, zeroReg), "br_if compare")
	switch {
	case fr.function:
		skip := c.a.FarBcond(a32.CondEQ)
		c.emitReturn()
		if !c.a.PatchFarBranch(skip, c.a.Len()) {
			return fmt.Errorf("conditional return target out of range")
		}
	case fr.loop:
		site := c.a.FarBcond(a32.CondNE)
		if !c.a.PatchFarBranch(site, fr.header) {
			return fmt.Errorf("loop target out of range")
		}
	default:
		fr.pending = append(fr.pending, c.a.FarBcond(a32.CondNE))
	}
	c.release(cond)
	return nil
}
func (c *compiler) emitReturn() {
	if len(c.stack) != 0 {
		result := c.materialize(c.pop())
		if result != a32.R0 {
			c.must(c.a.MovReg(a32.R0, result), "return move")
		}
		c.release(result)
	}
	c.a.Ret()
}
func (c *compiler) binary(op byte) {
	right, left := c.materialize(c.pop()), c.materialize(c.pop())
	dst := c.alloc()
	var ok bool
	switch op {
	case 0x6a:
		ok = c.a.Add(dst, left, right)
	case 0x6b:
		ok = c.a.Sub(dst, left, right)
	case 0x6c:
		ok = c.a.Mul(dst, left, right)
	case 0x71:
		ok = c.a.And(dst, left, right)
	case 0x72:
		ok = c.a.Orr(dst, left, right)
	case 0x73:
		ok = c.a.Eor(dst, left, right)
	case 0x74:
		ok = c.a.Lsl(dst, left, right)
	case 0x75:
		ok = c.a.Asr(dst, left, right)
	case 0x76:
		ok = c.a.Lsr(dst, left, right)
	case 0x77:
		tmp := c.alloc()
		c.must(c.a.Sub(tmp, zeroReg, right), "rotl count")
		ok = c.a.Ror(dst, left, tmp)
		c.release(tmp)
	case 0x78:
		ok = c.a.Ror(dst, left, right)
	}
	c.must(ok, "binary")
	c.release(left)
	c.release(right)
	c.push(operand{reg: dst})
}
func (c *compiler) countBits(op byte) error {
	value := c.materialize(c.pop())
	dst := c.alloc()
	tmp := c.alloc()
	c.must(c.a.MovImm32(dst, 0), "count init")
	if op == 0x69 { // popcnt
		loop := c.a.Len()
		c.must(c.a.Cmp(value, zeroReg), "popcnt zero")
		done := c.a.FarBcond(a32.CondEQ)
		c.must(c.a.MovImm32(tmp, 1), "popcnt mask")
		c.must(c.a.And(tmp, value, tmp), "popcnt bit")
		c.must(c.a.Add(dst, dst, tmp), "popcnt add")
		c.must(c.a.LsrImm(value, value, 1), "popcnt shift")
		back := c.a.Branch()
		if !c.a.PatchBranch(back, loop) || !c.a.PatchFarBranch(done, c.a.Len()) {
			return fmt.Errorf("arm32: popcnt branch out of range")
		}
	} else {
		c.must(c.a.Cmp(value, zeroReg), "count zero")
		nonzero := c.a.FarBcond(a32.CondNE)
		c.must(c.a.MovImm32(dst, 32), "zero count")
		finishZero := c.a.Branch()
		loop := c.a.Len()
		if !c.a.PatchFarBranch(nonzero, loop) {
			return fmt.Errorf("arm32: count entry branch out of range")
		}
		var done int
		if op == 0x67 { // clz: a negative value has its top bit set.
			c.must(c.a.Cmp(value, zeroReg), "clz top bit")
			done = c.a.FarBcond(a32.CondMI)
			c.must(c.a.LslImm(value, value, 1), "clz shift")
		} else { // ctz
			c.must(c.a.MovImm32(tmp, 1), "ctz mask")
			c.must(c.a.And(tmp, value, tmp), "ctz bit")
			c.must(c.a.Cmp(tmp, zeroReg), "ctz bit compare")
			done = c.a.FarBcond(a32.CondNE)
			c.must(c.a.LsrImm(value, value, 1), "ctz shift")
		}
		c.must(c.a.MovImm32(tmp, 1), "count increment")
		c.must(c.a.Add(dst, dst, tmp), "count add")
		back := c.a.Branch()
		if !c.a.PatchBranch(back, loop) {
			return fmt.Errorf("arm32: count loop branch out of range")
		}
		finish := c.a.Len()
		if !c.a.PatchFarBranch(done, finish) || !c.a.PatchBranch(finishZero, finish) {
			return fmt.Errorf("arm32: count finish branch out of range")
		}
	}
	c.release(value)
	c.release(tmp)
	c.push(operand{reg: dst})
	return nil
}

func (c *compiler) signExtend(op byte) {
	value := c.materialize(c.pop())
	shift := uint8(24)
	if op == 0xc1 {
		shift = 16
	}
	c.must(c.a.LslImm(value, value, shift), "sign extend left")
	c.must(c.a.AsrImm(value, value, shift), "sign extend right")
	c.push(operand{reg: value})
}

func (c *compiler) compare(op byte) {
	right, left := c.materialize(c.pop()), c.materialize(c.pop())
	cond := [...]a32.Cond{a32.CondEQ, a32.CondNE, a32.CondLT, a32.CondCC, a32.CondGT, a32.CondHI, a32.CondLE, a32.CondLS, a32.CondGE, a32.CondCS}[op-0x46]
	c.must(c.a.Cmp(left, right), "compare")
	c.release(left)
	c.release(right)
	dst := c.alloc()
	c.must(c.a.MovImm32(dst, 0), "compare false")
	skip := c.a.FarBcond(cond.Invert())
	c.must(c.a.MovImm32(dst, 1), "compare true")
	if !c.a.PatchFarBranch(skip, c.a.Len()) {
		panic("arm32: comparison skip out of range")
	}
	c.push(operand{reg: dst})
}
func (c *compiler) eqz() {
	value := c.materialize(c.pop())
	c.must(c.a.Cmp(value, zeroReg), "eqz")
	c.release(value)
	dst := c.alloc()
	c.must(c.a.MovImm32(dst, 0), "eqz false")
	skip := c.a.FarBcond(a32.CondNE)
	c.must(c.a.MovImm32(dst, 1), "eqz true")
	if !c.a.PatchFarBranch(skip, c.a.Len()) {
		panic("arm32: eqz skip out of range")
	}
	c.push(operand{reg: dst})
}
func (c *compiler) push(v operand) { c.stack = append(c.stack, v) }
func (c *compiler) pop() operand {
	if len(c.stack) == 0 {
		panic("arm32 beachhead: operand stack underflow")
	}
	v := c.stack[len(c.stack)-1]
	c.stack = c.stack[:len(c.stack)-1]
	return v
}
func (c *compiler) materialize(v operand) a32.Reg {
	if !v.constant {
		return v.reg
	}
	dst := c.alloc()
	c.must(c.a.MovImm32(dst, uint32(v.value)), "constant")
	return dst
}
func (c *compiler) alloc() a32.Reg {
	if len(c.free) == 0 {
		panic("arm32 beachhead: expression exceeds scratch register capacity")
	}
	r := c.free[0]
	c.free = c.free[1:]
	return r
}
func (c *compiler) release(r a32.Reg) {
	for _, scratch := range scratchRegs {
		if scratch == r {
			c.free = append([]a32.Reg{r}, c.free...)
			return
		}
	}
}
func (c *compiler) must(ok bool, op string) {
	if !ok {
		panic("arm32 beachhead: cannot encode " + op)
	}
}
