package riscv32

import (
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rv "github.com/wago-org/wago/src/core/encoder/riscv32"
)

var wideRegPool = []rv.Reg{
	rv.A0, rv.A1, rv.A2, rv.A3, rv.A4, rv.A5, rv.A6, rv.A7,
	rv.T0, rv.T1, rv.T2, rv.S0, rv.S1, rv.S2, rv.S3, rv.S4,
	rv.S5, rv.S6, rv.S7, rv.S8, rv.S9, rv.T3, rv.T4, rv.T5,
}

type wideValue struct {
	n        int
	regs     [4]rv.Reg
	groups   [4]shared.RegGroup
	groupN   int
	spilled  bool
	spillOff int32
}

type wideCompiler struct {
	a          rv.Asm
	stack      []wideValue
	groups     shared.GroupAllocator
	spillBase  int32
	spillLimit int32
	spillUsed  uint32
}

func newWideCompiler() *wideCompiler {
	ids := make([]uint8, len(wideRegPool))
	for i, r := range wideRegPool {
		ids[i] = uint8(r)
	}
	return &wideCompiler{groups: shared.NewGroupAllocator(ids)}
}
func (c *wideCompiler) alloc(n int) (wideValue, error) {
	for c.groups.FreeRegisters() < n {
		if !c.spillOne() {
			return wideValue{}, fmt.Errorf("riscv32: wide expression exceeds register capacity")
		}
	}
	v := wideValue{n: n}
	if n == 3 {
		for i := 0; i < 3; i++ {
			g, ok := c.groups.Alloc(1)
			if !ok {
				for j := 0; j < i; j++ {
					c.groups.Release(v.groups[j])
				}
				return wideValue{}, fmt.Errorf("riscv32: wide expression exceeds register capacity")
			}
			v.groups[i] = g
			v.regs[i] = rv.Reg(g.Regs[0])
			v.groupN++
		}
		return v, nil
	}
	g, ok := c.groups.Alloc(uint8(n))
	if !ok {
		return wideValue{}, fmt.Errorf("riscv32: wide expression exceeds register capacity")
	}
	v.groups[0] = g
	v.groupN = 1
	for i := 0; i < n; i++ {
		v.regs[i] = rv.Reg(g.Regs[i])
	}
	return v, nil
}
func (c *wideCompiler) release(v wideValue) {
	for i := 0; i < v.groupN; i++ {
		if !c.groups.Release(v.groups[i]) {
			panic("riscv32: partial or stale wide-value release")
		}
	}
}
func (c *wideCompiler) push(v wideValue) { c.stack = append(c.stack, v) }
func (c *wideCompiler) pop(n int) (wideValue, error) {
	if len(c.stack) == 0 {
		return wideValue{}, fmt.Errorf("riscv32: wide operand stack underflow")
	}
	v := c.stack[len(c.stack)-1]
	c.stack = c.stack[:len(c.stack)-1]
	if v.n != n {
		return wideValue{}, fmt.Errorf("riscv32: wide value has %d words, want %d", v.n, n)
	}
	if v.spilled {
		fresh, err := c.alloc(n)
		if err != nil {
			return wideValue{}, err
		}
		for i := 0; i < n; i++ {
			if !c.a.Lw(fresh.regs[i], rv.SP, v.spillOff+int32(i*4)) {
				panic("riscv32: spill reload")
			}
		}
		c.freeSpill(v.spillOff, n)
		v = fresh
	}
	return v, nil
}
func (c *wideCompiler) enableSpills(base, size int32) {
	c.spillBase = base
	c.spillLimit = base + size
	c.spillUsed = 0
}
func (c *wideCompiler) allocSpill(words int) (int32, bool) {
	slots := int((c.spillLimit - c.spillBase) / 4)
	mask := uint32((uint64(1) << words) - 1)
	for i := 0; i+words <= slots; i++ {
		m := mask << i
		if c.spillUsed&m == 0 {
			c.spillUsed |= m
			return c.spillBase + int32(i*4), true
		}
	}
	return 0, false
}
func (c *wideCompiler) freeSpill(off int32, words int) {
	i := uint((off - c.spillBase) / 4)
	mask := uint32((uint64(1)<<words)-1) << i
	if c.spillUsed&mask != mask {
		panic("riscv32: invalid spill release")
	}
	c.spillUsed &^= mask
}
func (c *wideCompiler) spillOne() bool {
	for i := 0; i < len(c.stack); i++ {
		v := &c.stack[i]
		if v.spilled || v.groupN != 1 || !c.groups.Owns(v.groups[0]) {
			continue
		}
		off, ok := c.allocSpill(v.n)
		if !ok {
			return false
		}
		for j := 0; j < v.n; j++ {
			if !c.a.Sw(v.regs[j], rv.SP, off+int32(j*4)) {
				panic("riscv32: spill store")
			}
		}
		c.release(*v)
		v.spilled = true
		v.spillOff = off
		v.groupN = 0
		v.regs = [4]rv.Reg{}
		return true
	}
	return false
}
func quad(v wideValue) rv.Quad { return rv.Quad{v.regs[0], v.regs[1], v.regs[2], v.regs[3]} }

var v128LocalHomes = [][4]rv.Reg{{rv.S0, rv.S1, rv.S2, rv.S3}, {rv.S4, rv.S5, rv.S6, rv.S7}}

// CompileV128Beachhead lowers a no-parameter direct v128 function.
func CompileV128Beachhead(body []byte) ([]byte, error) { return CompileV128Function(0, body) }

// CompileV128Function adds atomic quad parameters, locals, and bounded spills
// to the direct SWAR subset. The result returns in A0..A3.
func CompileV128Function(numParams int, body []byte) ([]byte, error) {
	c := newWideCompiler()
	r := wasm.NewReader(body)
	groups, err := r.U32()
	if err != nil {
		return nil, err
	}
	declared := 0
	for i := uint32(0); i < groups; i++ {
		n, e := r.U32()
		if e != nil {
			return nil, e
		}
		typ, e := r.Byte()
		if e != nil {
			return nil, e
		}
		if typ != 0x7b {
			return nil, fmt.Errorf("riscv32: v128 function local type %#x", typ)
		}
		declared += int(n)
	}
	if numParams < 0 || numParams > 2 {
		return nil, fmt.Errorf("riscv32: v128 function supports 0..2 parameters")
	}
	total := numParams + declared
	if total > len(v128LocalHomes) {
		return nil, fmt.Errorf("riscv32: v128 function supports %d quad locals", len(v128LocalHomes))
	}
	locals := make([]wideValue, total)
	saveBytes := int32(total * 16)
	frame := (saveBytes + 128 + 15) &^ 15
	c.a.Addi(rv.SP, rv.SP, -frame)
	c.enableSpills(saveBytes, 128)
	for i := 0; i < total; i++ {
		home := v128LocalHomes[i]
		for j := 0; j < 4; j++ {
			c.a.Sw(home[j], rv.SP, int32(i*16+j*4))
		}
		g, ok := c.groups.Acquire([4]uint8{uint8(home[0]), uint8(home[1]), uint8(home[2]), uint8(home[3])}, 4)
		if !ok {
			panic("riscv32: v128 local acquire")
		}
		locals[i] = wideValue{n: 4, regs: home, groups: [4]shared.RegGroup{g}, groupN: 1}
		for j := 0; j < 4; j++ {
			if i < numParams {
				c.a.MovReg(home[j], rv.A0+rv.Reg(i*4+j))
			} else {
				c.a.MovImm32(home[j], 0)
			}
		}
	}
	epilogue := func() {
		for i := 0; i < total; i++ {
			home := v128LocalHomes[i]
			for j := 0; j < 4; j++ {
				c.a.Lw(home[j], rv.SP, int32(i*16+j*4))
			}
		}
		c.a.Addi(rv.SP, rv.SP, frame)
		c.a.Ret()
	}
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return nil, err
		}
		if op == 0x01 {
			continue
		}
		if op == 0x1a {
			v, e := c.pop(4)
			if e != nil {
				return nil, e
			}
			c.release(v)
			continue
		}
		if op == 0x0b {
			if len(c.stack) != 1 {
				return nil, fmt.Errorf("riscv32: v128 result stack has %d values", len(c.stack))
			}
			v, e := c.pop(4)
			if e != nil {
				return nil, e
			}
			want := [4]rv.Reg{rv.A0, rv.A1, rv.A2, rv.A3}
			if v.regs != want {
				for i := 0; i < 4; i++ {
					c.a.MovReg(want[i], v.regs[i])
				}
			}
			epilogue()
			return c.a.B, nil
		}
		if op == 0x20 || op == 0x21 || op == 0x22 {
			idx, e := r.U32()
			if e != nil {
				return nil, e
			}
			if int(idx) >= len(locals) {
				return nil, fmt.Errorf("riscv32: v128 local index %d", idx)
			}
			home := locals[idx]
			if op == 0x20 {
				v, e := c.alloc(4)
				if e != nil {
					return nil, e
				}
				for i := 0; i < 4; i++ {
					c.a.MovReg(v.regs[i], home.regs[i])
				}
				c.push(v)
			} else {
				v, e := c.pop(4)
				if e != nil {
					return nil, e
				}
				for i := 0; i < 4; i++ {
					c.a.MovReg(home.regs[i], v.regs[i])
				}
				if op == 0x22 {
					c.push(v)
				} else {
					c.release(v)
				}
			}
			continue
		}
		if op != 0xfd {
			return nil, fmt.Errorf("riscv32: v128 function unsupported opcode %#x", op)
		}
		sub, err := r.U32()
		if err != nil {
			return nil, err
		}
		switch sub {
		case 12:
			b, err := r.Bytes(16)
			if err != nil {
				return nil, err
			}
			v, err := c.alloc(4)
			if err != nil {
				return nil, err
			}
			for i := 0; i < 4; i++ {
				c.a.MovImm32(v.regs[i], binary.LittleEndian.Uint32(b[i*4:]))
			}
			c.push(v)
		case 77:
			a, err := c.pop(4)
			if err != nil {
				return nil, err
			}
			for i := 0; i < 4; i++ {
				c.a.Not(a.regs[i], a.regs[i])
			}
			c.push(a)
		case 78, 79, 80, 81, 110, 113, 142, 145, 174, 177:
			b, err := c.pop(4)
			if err != nil {
				return nil, err
			}
			a, err := c.pop(4)
			if err != nil {
				return nil, err
			}
			switch sub {
			case 78:
				c.a.And128(quad(a), quad(a), quad(b))
			case 79:
				for i := 0; i < 4; i++ {
					c.a.Not(b.regs[i], b.regs[i])
					c.a.And(a.regs[i], a.regs[i], b.regs[i])
				}
			case 80:
				c.a.Orr128(quad(a), quad(a), quad(b))
			case 81:
				c.a.Eor128(quad(a), quad(a), quad(b))
			case 174:
				c.a.AddI32x4(quad(a), quad(a), quad(b))
			case 177:
				c.a.SubI32x4(quad(a), quad(a), quad(b))
			default:
				s, err := c.alloc(3)
				if err != nil {
					return nil, err
				}
				width := uint8(8)
				if sub == 142 || sub == 145 {
					width = 16
				}
				if !c.a.PackedAddSub(quad(a), quad(b), width, sub == 113 || sub == 145, s.regs[0], s.regs[1], s.regs[2]) {
					panic("riscv32: packed SWAR encoding failed")
				}
				c.release(s)
			}
			c.release(b)
			c.push(a)
		case 82:
			mask, err := c.pop(4)
			if err != nil {
				return nil, err
			}
			b, err := c.pop(4)
			if err != nil {
				return nil, err
			}
			a, err := c.pop(4)
			if err != nil {
				return nil, err
			}
			for i := 0; i < 4; i++ {
				c.a.Xor(a.regs[i], a.regs[i], b.regs[i])
				c.a.And(a.regs[i], a.regs[i], mask.regs[i])
				c.a.Xor(a.regs[i], a.regs[i], b.regs[i])
			}
			c.release(mask)
			c.release(b)
			c.push(a)
		default:
			return nil, fmt.Errorf("riscv32: v128 direct beachhead unsupported subopcode %d", sub)
		}
	}
	return nil, fmt.Errorf("riscv32: v128 body missing end")
}

// CompileF64BitBeachhead lowers a no-parameter integer-only f64 function.
func CompileF64BitBeachhead(body []byte) ([]byte, error) { return CompileF64BitFunction(0, body) }

// CompileF64BitFunction adds pair parameters, locals, and bounded spills to
// f64.const/abs/neg/copysign. Arithmetic and conversions use helper thunks.
func CompileF64BitFunction(numParams int, body []byte) ([]byte, error) {
	c := newWideCompiler()
	r := wasm.NewReader(body)
	groups, err := r.U32()
	if err != nil {
		return nil, err
	}
	declared := 0
	for i := uint32(0); i < groups; i++ {
		n, e := r.U32()
		if e != nil {
			return nil, e
		}
		typ, e := r.Byte()
		if e != nil {
			return nil, e
		}
		if typ != 0x7c {
			return nil, fmt.Errorf("riscv32: f64 function local type %#x", typ)
		}
		declared += int(n)
	}
	if numParams < 0 || numParams > 4 {
		return nil, fmt.Errorf("riscv32: f64 function supports 0..4 parameters")
	}
	total := numParams + declared
	if total > len(i64LocalHomes) {
		return nil, fmt.Errorf("riscv32: f64 function supports %d pair locals", len(i64LocalHomes))
	}
	locals := make([]wideValue, total)
	saveBytes := int32(total * 8)
	frame := (saveBytes + 64 + 15) &^ 15
	c.a.Addi(rv.SP, rv.SP, -frame)
	c.enableSpills(saveBytes, 64)
	for i := 0; i < total; i++ {
		home := i64LocalHomes[i]
		c.a.Sw(home[0], rv.SP, int32(i*8))
		c.a.Sw(home[1], rv.SP, int32(i*8+4))
		g, ok := c.groups.Acquire([4]uint8{uint8(home[0]), uint8(home[1])}, 2)
		if !ok {
			panic("riscv32: f64 local acquire")
		}
		locals[i] = wideValue{n: 2, regs: [4]rv.Reg{home[0], home[1]}, groups: [4]shared.RegGroup{g}, groupN: 1}
		if i < numParams {
			c.a.MovReg(home[0], rv.A0+rv.Reg(i*2))
			c.a.MovReg(home[1], rv.A1+rv.Reg(i*2))
		} else {
			c.a.MovImm32(home[0], 0)
			c.a.MovImm32(home[1], 0)
		}
	}
	epilogue := func() {
		for i := 0; i < total; i++ {
			home := i64LocalHomes[i]
			c.a.Lw(home[0], rv.SP, int32(i*8))
			c.a.Lw(home[1], rv.SP, int32(i*8+4))
		}
		c.a.Addi(rv.SP, rv.SP, frame)
		c.a.Ret()
	}
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return nil, err
		}
		switch op {
		case 0x01:
			// nop
		case 0x1a:
			v, e := c.pop(2)
			if e != nil {
				return nil, e
			}
			c.release(v)
		case 0x0b:
			if len(c.stack) != 1 {
				return nil, fmt.Errorf("riscv32: f64 result stack has %d values", len(c.stack))
			}
			v, e := c.pop(2)
			if e != nil {
				return nil, e
			}
			if v.regs[0] != rv.A0 || v.regs[1] != rv.A1 {
				c.a.MovReg(rv.A0, v.regs[0])
				c.a.MovReg(rv.A1, v.regs[1])
			}
			epilogue()
			return c.a.B, nil
		case 0x20, 0x21, 0x22:
			idx, e := r.U32()
			if e != nil {
				return nil, e
			}
			if int(idx) >= len(locals) {
				return nil, fmt.Errorf("riscv32: f64 local index %d", idx)
			}
			home := locals[idx]
			if op == 0x20 {
				v, e := c.alloc(2)
				if e != nil {
					return nil, e
				}
				c.a.MovReg(v.regs[0], home.regs[0])
				c.a.MovReg(v.regs[1], home.regs[1])
				c.push(v)
			} else {
				v, e := c.pop(2)
				if e != nil {
					return nil, e
				}
				c.a.MovReg(home.regs[0], v.regs[0])
				c.a.MovReg(home.regs[1], v.regs[1])
				if op == 0x22 {
					c.push(v)
				} else {
					c.release(v)
				}
			}
		case 0x44:
			bits, err := r.LEU64()
			if err != nil {
				return nil, err
			}
			v, err := c.alloc(2)
			if err != nil {
				return nil, err
			}
			c.a.MovImm32(v.regs[0], uint32(bits))
			c.a.MovImm32(v.regs[1], uint32(bits>>32))
			c.push(v)
		case 0x99, 0x9a:
			v, err := c.pop(2)
			if err != nil {
				return nil, err
			}
			s, err := c.alloc(1)
			if err != nil {
				return nil, err
			}
			if op == 0x99 {
				c.a.F64Abs(v.regs[0], v.regs[1], s.regs[0])
			} else {
				c.a.F64Neg(v.regs[0], v.regs[1], s.regs[0])
			}
			c.release(s)
			c.push(v)
		case 0xa6:
			sign, err := c.pop(2)
			if err != nil {
				return nil, err
			}
			mag, err := c.pop(2)
			if err != nil {
				return nil, err
			}
			s, err := c.alloc(2)
			if err != nil {
				return nil, err
			}
			c.a.F64Copysign(mag.regs[0], mag.regs[1], mag.regs[0], mag.regs[1], sign.regs[1], s.regs[0], s.regs[1])
			c.release(s)
			c.release(sign)
			c.push(mag)
		default:
			return nil, fmt.Errorf("riscv32: f64 bit function unsupported opcode %#x", op)
		}
	}
	return nil, fmt.Errorf("riscv32: f64 body missing end")
}

var i64LocalHomes = [][2]rv.Reg{{rv.S0, rv.S1}, {rv.S2, rv.S3}, {rv.S4, rv.S5}, {rv.S6, rv.S7}, {rv.S8, rv.S9}}

// CompileI64Beachhead lowers a no-parameter i64 function.
func CompileI64Beachhead(body []byte) ([]byte, error) { return CompileI64Function(0, body) }

// CompileI64Function adds atomic pair parameters and register-backed locals to
// the direct i64 beachhead. Callee-saved local homes are preserved in an aligned
// frame; local.get/set/tee always move both words as one logical value.
func CompileI64Function(numParams int, body []byte) ([]byte, error) {
	c := newWideCompiler()
	r := wasm.NewReader(body)
	groups, err := r.U32()
	if err != nil {
		return nil, err
	}
	declared := 0
	for i := uint32(0); i < groups; i++ {
		n, e := r.U32()
		if e != nil {
			return nil, e
		}
		typ, e := r.Byte()
		if e != nil {
			return nil, e
		}
		if typ != 0x7e {
			return nil, fmt.Errorf("riscv32: i64 function local type %#x", typ)
		}
		declared += int(n)
	}
	if numParams < 0 || numParams > 4 {
		return nil, fmt.Errorf("riscv32: i64 function supports 0..4 parameters")
	}
	total := numParams + declared
	if total > len(i64LocalHomes) {
		return nil, fmt.Errorf("riscv32: i64 function supports %d pair locals", len(i64LocalHomes))
	}
	locals := make([]wideValue, total)
	saveBytes := int32(total * 8)
	frame := (saveBytes + 64 + 15) &^ 15
	c.a.Addi(rv.SP, rv.SP, -frame)
	c.enableSpills(saveBytes, 64)
	for i := 0; i < total; i++ {
		home := i64LocalHomes[i]
		if !c.a.Sw(home[0], rv.SP, int32(i*8)) || !c.a.Sw(home[1], rv.SP, int32(i*8+4)) {
			panic("riscv32: local save")
		}
		g, ok := c.groups.Acquire([4]uint8{uint8(home[0]), uint8(home[1])}, 2)
		if !ok {
			panic("riscv32: local home acquire")
		}
		locals[i] = wideValue{n: 2, regs: [4]rv.Reg{home[0], home[1]}, groups: [4]shared.RegGroup{g}, groupN: 1}
		if i < numParams {
			c.a.MovReg(home[0], rv.A0+rv.Reg(i*2))
			c.a.MovReg(home[1], rv.A1+rv.Reg(i*2))
		} else {
			c.a.MovImm32(home[0], 0)
			c.a.MovImm32(home[1], 0)
		}
	}
	epilogue := func() {
		for i := 0; i < total; i++ {
			home := i64LocalHomes[i]
			c.a.Lw(home[0], rv.SP, int32(i*8))
			c.a.Lw(home[1], rv.SP, int32(i*8+4))
		}
		c.a.Addi(rv.SP, rv.SP, frame)
		c.a.Ret()
	}
	for r.HasNext() {
		op, e := r.Byte()
		if e != nil {
			return nil, e
		}
		switch op {
		case 0x01:
			// nop
		case 0x1a:
			v, e := c.pop(2)
			if e != nil {
				return nil, e
			}
			c.release(v)
		case 0x0b:
			if len(c.stack) != 1 {
				return nil, fmt.Errorf("riscv32: i64 result stack has %d values", len(c.stack))
			}
			v, _ := c.pop(2)
			if v.regs[0] != rv.A0 || v.regs[1] != rv.A1 {
				c.a.MovReg(rv.A0, v.regs[0])
				c.a.MovReg(rv.A1, v.regs[1])
			}
			epilogue()
			return c.a.B, nil
		case 0x20, 0x21, 0x22:
			idx, e := r.U32()
			if e != nil {
				return nil, e
			}
			if int(idx) >= len(locals) {
				return nil, fmt.Errorf("riscv32: i64 local index %d", idx)
			}
			home := locals[idx]
			if op == 0x20 {
				v, e := c.alloc(2)
				if e != nil {
					return nil, e
				}
				c.a.MovReg(v.regs[0], home.regs[0])
				c.a.MovReg(v.regs[1], home.regs[1])
				c.push(v)
			} else {
				v, e := c.pop(2)
				if e != nil {
					return nil, e
				}
				c.a.MovReg(home.regs[0], v.regs[0])
				c.a.MovReg(home.regs[1], v.regs[1])
				if op == 0x22 {
					c.push(v)
				} else {
					c.release(v)
				}
			}
		case 0x42:
			x, e := r.I64()
			if e != nil {
				return nil, e
			}
			v, e := c.alloc(2)
			if e != nil {
				return nil, e
			}
			c.a.MovImm32(v.regs[0], uint32(x))
			c.a.MovImm32(v.regs[1], uint32(uint64(x)>>32))
			c.push(v)
		case 0x79, 0x7a, 0x7b:
			v, e := c.pop(2)
			if e != nil {
				return nil, e
			}
			out, e := c.alloc(2)
			if e != nil {
				return nil, e
			}
			c.a.MovImm32(out.regs[0], 0)
			if op != 0x7b {
				c.a.Or(out.regs[1], v.regs[0], v.regs[1])
				nonzero := c.a.Bcond(out.regs[1], rv.Zero, rv.CondNE)
				c.a.MovImm32(out.regs[0], 64)
				zeroDone := c.a.Jal(rv.Zero)
				loop := c.a.Len()
				if !c.a.PatchBranch13(nonzero, loop) {
					return nil, fmt.Errorf("riscv32: i64 count entry out of range")
				}
				var done int
				if op == 0x79 {
					done = c.a.Bcond(v.regs[1], rv.Zero, rv.CondLT)
					c.a.Srli(out.regs[1], v.regs[0], 31)
					c.a.Slli(v.regs[0], v.regs[0], 1)
					c.a.Slli(v.regs[1], v.regs[1], 1)
					c.a.Or(v.regs[1], v.regs[1], out.regs[1])
				} else {
					c.a.Andi(out.regs[1], v.regs[0], 1)
					done = c.a.Bcond(out.regs[1], rv.Zero, rv.CondNE)
					c.a.Slli(out.regs[1], v.regs[1], 31)
					c.a.Srli(v.regs[0], v.regs[0], 1)
					c.a.Srli(v.regs[1], v.regs[1], 1)
					c.a.Or(v.regs[0], v.regs[0], out.regs[1])
				}
				c.a.Addi(out.regs[0], out.regs[0], 1)
				back := c.a.Jal(rv.Zero)
				finish := c.a.Len()
				if !c.a.PatchJAL21(back, loop) || !c.a.PatchBranch13(done, finish) || !c.a.PatchJAL21(zeroDone, finish) {
					return nil, fmt.Errorf("riscv32: i64 count branch out of range")
				}
			} else {
				loop := c.a.Len()
				c.a.Or(out.regs[1], v.regs[0], v.regs[1])
				done := c.a.Bcond(out.regs[1], rv.Zero, rv.CondEQ)
				c.a.Andi(out.regs[1], v.regs[0], 1)
				c.a.Add(out.regs[0], out.regs[0], out.regs[1])
				c.a.Slli(out.regs[1], v.regs[1], 31)
				c.a.Srli(v.regs[0], v.regs[0], 1)
				c.a.Srli(v.regs[1], v.regs[1], 1)
				c.a.Or(v.regs[0], v.regs[0], out.regs[1])
				back := c.a.Jal(rv.Zero)
				if !c.a.PatchJAL21(back, loop) || !c.a.PatchBranch13(done, c.a.Len()) {
					return nil, fmt.Errorf("riscv32: i64 popcnt branch out of range")
				}
			}
			c.a.MovImm32(out.regs[1], 0)
			c.release(v)
			c.push(out)
		case 0x86, 0x87, 0x88, 0x89, 0x8a:
			count, e := c.pop(2)
			if e != nil {
				return nil, e
			}
			v, e := c.pop(2)
			if e != nil {
				return nil, e
			}
			c.a.Andi(count.regs[0], count.regs[0], 63)
			loop := c.a.Len()
			done := c.a.Bcond(count.regs[0], rv.Zero, rv.CondEQ)
			switch op {
			case 0x86:
				c.a.Srli(count.regs[1], v.regs[0], 31)
				c.a.Slli(v.regs[0], v.regs[0], 1)
				c.a.Slli(v.regs[1], v.regs[1], 1)
				c.a.Or(v.regs[1], v.regs[1], count.regs[1])
			case 0x87, 0x88:
				c.a.Slli(count.regs[1], v.regs[1], 31)
				c.a.Srli(v.regs[0], v.regs[0], 1)
				if op == 0x87 {
					c.a.Srai(v.regs[1], v.regs[1], 1)
				} else {
					c.a.Srli(v.regs[1], v.regs[1], 1)
				}
				c.a.Or(v.regs[0], v.regs[0], count.regs[1])
			case 0x89:
				c.a.Srli(count.regs[1], v.regs[1], 31)
				c.a.Srli(rv.T6, v.regs[0], 31)
				c.a.Slli(v.regs[0], v.regs[0], 1)
				c.a.Slli(v.regs[1], v.regs[1], 1)
				c.a.Or(v.regs[1], v.regs[1], rv.T6)
				c.a.Or(v.regs[0], v.regs[0], count.regs[1])
			case 0x8a:
				c.a.Slli(count.regs[1], v.regs[0], 31)
				c.a.Slli(rv.T6, v.regs[1], 31)
				c.a.Srli(v.regs[0], v.regs[0], 1)
				c.a.Srli(v.regs[1], v.regs[1], 1)
				c.a.Or(v.regs[0], v.regs[0], rv.T6)
				c.a.Or(v.regs[1], v.regs[1], count.regs[1])
			}
			c.a.Addi(count.regs[0], count.regs[0], -1)
			back := c.a.Jal(rv.Zero)
			if !c.a.PatchJAL21(back, loop) || !c.a.PatchBranch13(done, c.a.Len()) {
				return nil, fmt.Errorf("riscv32: i64 shift loop out of range")
			}
			c.release(count)
			c.push(v)
		case 0xc2, 0xc3, 0xc4:
			v, e := c.pop(2)
			if e != nil {
				return nil, e
			}
			if op == 0xc2 {
				c.a.Sext8(v.regs[0], v.regs[0])
			} else if op == 0xc3 {
				c.a.Sext16(v.regs[0], v.regs[0])
			}
			c.a.Srai(v.regs[1], v.regs[0], 31)
			c.push(v)
		case 0x7c, 0x7d, 0x7e, 0x83, 0x84, 0x85:
			b, e := c.pop(2)
			if e != nil {
				return nil, e
			}
			a, e := c.pop(2)
			if e != nil {
				return nil, e
			}
			if op >= 0x83 {
				for i := 0; i < 2; i++ {
					if op == 0x83 {
						c.a.And(a.regs[i], a.regs[i], b.regs[i])
					} else if op == 0x84 {
						c.a.Or(a.regs[i], a.regs[i], b.regs[i])
					} else {
						c.a.Xor(a.regs[i], a.regs[i], b.regs[i])
					}
				}
				c.release(b)
				c.push(a)
				continue
			}
			out, e := c.alloc(2)
			if e != nil {
				return nil, e
			}
			sn := 1
			if op == 0x7e {
				sn = 2
			}
			s, e := c.alloc(sn)
			if e != nil {
				return nil, e
			}
			if op == 0x7c {
				c.a.Add64(out.regs[0], out.regs[1], a.regs[0], a.regs[1], b.regs[0], b.regs[1], s.regs[0])
			} else if op == 0x7d {
				c.a.Sub64(out.regs[0], out.regs[1], a.regs[0], a.regs[1], b.regs[0], b.regs[1], s.regs[0])
			} else {
				c.a.Mul64(out.regs[0], out.regs[1], a.regs[0], a.regs[1], b.regs[0], b.regs[1], s.regs[0], s.regs[1])
			}
			c.a.MovReg(a.regs[0], out.regs[0])
			c.a.MovReg(a.regs[1], out.regs[1])
			c.release(s)
			c.release(out)
			c.release(b)
			c.push(a)
		default:
			return nil, fmt.Errorf("riscv32: i64 function unsupported opcode %#x", op)
		}
	}
	return nil, fmt.Errorf("riscv32: i64 body missing end")
}
