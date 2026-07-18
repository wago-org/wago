package arm32

import (
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	a32 "github.com/wago-org/wago/src/core/encoder/arm32"
)

var wideRegPool = []a32.Reg{a32.R0, a32.R1, a32.R2, a32.R3, a32.R4, a32.R5, a32.R6, a32.R7, a32.R8, a32.R9, a32.R10, a32.R11}

type wideValue struct {
	n        int
	regs     [4]a32.Reg
	groups   [4]shared.RegGroup
	groupN   int
	spilled  bool
	spillOff uint16
}
type wideCompiler struct {
	a          a32.Asm
	stack      []wideValue
	groups     shared.GroupAllocator
	spillNext  uint16
	spillLimit uint16
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
			return wideValue{}, fmt.Errorf("arm32: wide expression exceeds register capacity")
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
				return wideValue{}, fmt.Errorf("arm32: wide expression exceeds register capacity")
			}
			v.groups[i] = g
			v.regs[i] = a32.Reg(g.Regs[0])
			v.groupN++
		}
		return v, nil
	}
	g, ok := c.groups.Alloc(uint8(n))
	if !ok {
		return wideValue{}, fmt.Errorf("arm32: wide expression exceeds register capacity")
	}
	v.groups[0] = g
	v.groupN = 1
	for i := 0; i < n; i++ {
		v.regs[i] = a32.Reg(g.Regs[i])
	}
	return v, nil
}
func (c *wideCompiler) release(v wideValue) {
	for i := 0; i < v.groupN; i++ {
		if !c.groups.Release(v.groups[i]) {
			panic("arm32: partial or stale wide-value release")
		}
	}
}
func (c *wideCompiler) push(v wideValue) { c.stack = append(c.stack, v) }
func (c *wideCompiler) pop(n int) (wideValue, error) {
	if len(c.stack) == 0 {
		return wideValue{}, fmt.Errorf("arm32: wide operand stack underflow")
	}
	v := c.stack[len(c.stack)-1]
	c.stack = c.stack[:len(c.stack)-1]
	if v.n != n {
		return wideValue{}, fmt.Errorf("arm32: wide value has %d words, want %d", v.n, n)
	}
	if v.spilled {
		fresh, err := c.alloc(n)
		if err != nil {
			return wideValue{}, err
		}
		for i := 0; i < n; i++ {
			if !c.a.Ldr(fresh.regs[i], a32.SP, v.spillOff+uint16(i*4)) {
				panic("arm32: spill reload")
			}
		}
		v = fresh
	}
	return v, nil
}
func (c *wideCompiler) enableSpills(base, size uint16) {
	c.spillNext = base
	c.spillLimit = base + size
}
func (c *wideCompiler) spillOne() bool {
	for i := 0; i < len(c.stack); i++ {
		v := &c.stack[i]
		if v.spilled || v.groupN != 1 || !c.groups.Owns(v.groups[0]) {
			continue
		}
		size := uint16(v.n * 4)
		if c.spillNext+size > c.spillLimit {
			return false
		}
		off := c.spillNext
		c.spillNext += size
		for j := 0; j < v.n; j++ {
			if !c.a.Str(v.regs[j], a32.SP, off+uint16(j*4)) {
				panic("arm32: spill store")
			}
		}
		c.release(*v)
		v.spilled = true
		v.spillOff = off
		v.groupN = 0
		v.regs = [4]a32.Reg{}
		return true
	}
	return false
}
func quad(v wideValue) a32.Quad { return a32.Quad{v.regs[0], v.regs[1], v.regs[2], v.regs[3]} }

var v128LocalHomes = [][4]a32.Reg{{a32.R4, a32.R5, a32.R6, a32.R7}, {a32.R8, a32.R9, a32.R10, a32.R11}}

// CompileV128Beachhead lowers a no-parameter direct v128 function.
func CompileV128Beachhead(body []byte) ([]byte, error) { return CompileV128Function(0, body) }

// CompileV128Function adds atomic quad parameters, locals, and bounded spills
// to the direct Thumb-2 SWAR subset. The result returns in R0..R3.
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
			return nil, fmt.Errorf("arm32: v128 function local type %#x", typ)
		}
		declared += int(n)
	}
	if numParams < 0 || numParams > 1 {
		return nil, fmt.Errorf("arm32: v128 function supports 0..1 parameters")
	}
	total := numParams + declared
	if total > len(v128LocalHomes) {
		return nil, fmt.Errorf("arm32: v128 function supports %d quad locals", len(v128LocalHomes))
	}
	locals := make([]wideValue, total)
	saveBytes := uint16(total * 16)
	frame := uint32((uint32(saveBytes) + 128 + 15) &^ 15)
	c.a.MovImm32(a32.R12, frame)
	c.a.Sub(a32.SP, a32.SP, a32.R12)
	c.enableSpills(saveBytes, 128)
	for i := 0; i < total; i++ {
		home := v128LocalHomes[i]
		for j := 0; j < 4; j++ {
			c.a.Str(home[j], a32.SP, uint16(i*16+j*4))
		}
		g, ok := c.groups.Acquire([4]uint8{uint8(home[0]), uint8(home[1]), uint8(home[2]), uint8(home[3])}, 4)
		if !ok {
			panic("arm32: v128 local acquire")
		}
		locals[i] = wideValue{n: 4, regs: home, groups: [4]shared.RegGroup{g}, groupN: 1}
		for j := 0; j < 4; j++ {
			if i < numParams {
				c.a.MovReg(home[j], a32.R0+a32.Reg(j))
			} else {
				c.a.MovImm32(home[j], 0)
			}
		}
	}
	epilogue := func() {
		for i := 0; i < total; i++ {
			home := v128LocalHomes[i]
			for j := 0; j < 4; j++ {
				c.a.Ldr(home[j], a32.SP, uint16(i*16+j*4))
			}
		}
		c.a.MovImm32(a32.R12, frame)
		c.a.Add(a32.SP, a32.SP, a32.R12)
		c.a.Ret()
		c.a.Align4()
	}
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return nil, err
		}
		if op == 0x0b {
			if len(c.stack) != 1 {
				return nil, fmt.Errorf("arm32: v128 result stack has %d values", len(c.stack))
			}
			v, e := c.pop(4)
			if e != nil {
				return nil, e
			}
			want := [4]a32.Reg{a32.R0, a32.R1, a32.R2, a32.R3}
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
				return nil, fmt.Errorf("arm32: v128 local index %d", idx)
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
			return nil, fmt.Errorf("arm32: v128 function unsupported opcode %#x", op)
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
				if !c.a.MovImm32(v.regs[i], binary.LittleEndian.Uint32(b[i*4:])) {
					panic("arm32: v128 constant")
				}
			}
			c.push(v)
		case 77:
			a, err := c.pop(4)
			if err != nil {
				return nil, err
			}
			for i := 0; i < 4; i++ {
				if !c.a.Mvn(a.regs[i], a.regs[i]) {
					panic("arm32: v128.not")
				}
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
					c.a.Bic(a.regs[i], a.regs[i], b.regs[i])
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
					panic("arm32: packed SWAR")
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
				c.a.Eor(a.regs[i], a.regs[i], b.regs[i])
				c.a.And(a.regs[i], a.regs[i], mask.regs[i])
				c.a.Eor(a.regs[i], a.regs[i], b.regs[i])
			}
			c.release(mask)
			c.release(b)
			c.push(a)
		default:
			return nil, fmt.Errorf("arm32: v128 direct beachhead unsupported subopcode %d", sub)
		}
	}
	return nil, fmt.Errorf("arm32: v128 body missing end")
}

// CompileF64BitBeachhead lowers f64.const/abs/neg/copysign through integer
// register pairs. The result returns in R0/R1.
func CompileF64BitBeachhead(body []byte) ([]byte, error) {
	c := newWideCompiler()
	r := wasm.NewReader(body)
	groups, err := r.U32()
	if err != nil {
		return nil, err
	}
	if groups != 0 {
		return nil, fmt.Errorf("arm32: f64 beachhead does not support locals")
	}
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return nil, err
		}
		switch op {
		case 0x0b:
			if len(c.stack) != 1 {
				return nil, fmt.Errorf("arm32: f64 result stack has %d values", len(c.stack))
			}
			v, _ := c.pop(2)
			if v.regs[0] != a32.R0 || v.regs[1] != a32.R1 {
				return nil, fmt.Errorf("arm32: non-canonical f64 result registers")
			}
			c.a.Ret()
			c.a.Align4()
			return c.a.B, nil
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
			var ok bool
			if op == 0x99 {
				ok = c.a.F64Abs(v.regs[0], v.regs[1], s.regs[0])
			} else {
				ok = c.a.F64Neg(v.regs[0], v.regs[1], s.regs[0])
			}
			if !ok {
				panic("arm32: f64 bit op")
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
			if !c.a.F64Copysign(mag.regs[0], mag.regs[1], mag.regs[0], mag.regs[1], sign.regs[1], s.regs[0], s.regs[1]) {
				panic("arm32: f64.copysign")
			}
			c.release(s)
			c.release(sign)
			c.push(mag)
		default:
			return nil, fmt.Errorf("arm32: f64 bit beachhead unsupported opcode %#x", op)
		}
	}
	return nil, fmt.Errorf("arm32: f64 body missing end")
}

var i64LocalHomes = [][2]a32.Reg{{a32.R4, a32.R5}, {a32.R6, a32.R7}, {a32.R8, a32.R9}, {a32.R10, a32.R11}}

// CompileI64Beachhead lowers a no-parameter i64 function.
func CompileI64Beachhead(body []byte) ([]byte, error) { return CompileI64Function(0, body) }

// CompileI64Function adds atomic pair parameters and register-backed locals to
// the direct Thumb-2 i64 beachhead while preserving every callee-saved home.
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
			return nil, fmt.Errorf("arm32: i64 function local type %#x", typ)
		}
		declared += int(n)
	}
	if numParams < 0 || numParams > 2 {
		return nil, fmt.Errorf("arm32: i64 function supports 0..2 parameters")
	}
	total := numParams + declared
	if total > len(i64LocalHomes) {
		return nil, fmt.Errorf("arm32: i64 function supports %d pair locals", len(i64LocalHomes))
	}
	locals := make([]wideValue, total)
	saveBytes := uint16(total * 8)
	frame := uint32((uint32(saveBytes) + 64 + 15) &^ 15)
	c.a.MovImm32(a32.R12, frame)
	c.a.Sub(a32.SP, a32.SP, a32.R12)
	c.enableSpills(saveBytes, 64)
	for i := 0; i < total; i++ {
		home := i64LocalHomes[i]
		if !c.a.Str(home[0], a32.SP, uint16(i*8)) || !c.a.Str(home[1], a32.SP, uint16(i*8+4)) {
			panic("arm32: local save")
		}
		g, ok := c.groups.Acquire([4]uint8{uint8(home[0]), uint8(home[1])}, 2)
		if !ok {
			panic("arm32: local home acquire")
		}
		locals[i] = wideValue{n: 2, regs: [4]a32.Reg{home[0], home[1]}, groups: [4]shared.RegGroup{g}, groupN: 1}
		if i < numParams {
			c.a.MovReg(home[0], a32.R0+a32.Reg(i*2))
			c.a.MovReg(home[1], a32.R1+a32.Reg(i*2))
		} else {
			c.a.MovImm32(home[0], 0)
			c.a.MovImm32(home[1], 0)
		}
	}
	epilogue := func() {
		for i := 0; i < total; i++ {
			home := i64LocalHomes[i]
			c.a.Ldr(home[0], a32.SP, uint16(i*8))
			c.a.Ldr(home[1], a32.SP, uint16(i*8+4))
		}
		c.a.MovImm32(a32.R12, frame)
		c.a.Add(a32.SP, a32.SP, a32.R12)
		c.a.Ret()
		c.a.Align4()
	}
	for r.HasNext() {
		op, e := r.Byte()
		if e != nil {
			return nil, e
		}
		switch op {
		case 0x0b:
			if len(c.stack) != 1 {
				return nil, fmt.Errorf("arm32: i64 result stack has %d values", len(c.stack))
			}
			v, _ := c.pop(2)
			if v.regs[0] != a32.R0 || v.regs[1] != a32.R1 {
				c.a.MovReg(a32.R0, v.regs[0])
				c.a.MovReg(a32.R1, v.regs[1])
			}
			epilogue()
			return c.a.B, nil
		case 0x20, 0x21, 0x22:
			idx, e := r.U32()
			if e != nil {
				return nil, e
			}
			if int(idx) >= len(locals) {
				return nil, fmt.Errorf("arm32: i64 local index %d", idx)
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
					var ok bool
					if op == 0x83 {
						ok = c.a.And(a.regs[i], a.regs[i], b.regs[i])
					} else if op == 0x84 {
						ok = c.a.Orr(a.regs[i], a.regs[i], b.regs[i])
					} else {
						ok = c.a.Eor(a.regs[i], a.regs[i], b.regs[i])
					}
					if !ok {
						panic("arm32: i64 bitwise")
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
			sn := 0
			if op == 0x7e {
				sn = 1
			}
			var s wideValue
			if sn != 0 {
				s, e = c.alloc(sn)
				if e != nil {
					return nil, e
				}
			}
			var ok bool
			if op == 0x7c {
				ok = c.a.Add64(out.regs[0], out.regs[1], a.regs[0], a.regs[1], b.regs[0], b.regs[1])
			} else if op == 0x7d {
				ok = c.a.Sub64(out.regs[0], out.regs[1], a.regs[0], a.regs[1], b.regs[0], b.regs[1])
			} else {
				ok = c.a.Mul64(out.regs[0], out.regs[1], a.regs[0], a.regs[1], b.regs[0], b.regs[1], s.regs[0])
			}
			if !ok {
				panic("arm32: i64 arithmetic")
			}
			c.a.MovReg(a.regs[0], out.regs[0])
			c.a.MovReg(a.regs[1], out.regs[1])
			if sn != 0 {
				c.release(s)
			}
			c.release(out)
			c.release(b)
			c.push(a)
		default:
			return nil, fmt.Errorf("arm32: i64 function unsupported opcode %#x", op)
		}
	}
	return nil, fmt.Errorf("arm32: i64 body missing end")
}
