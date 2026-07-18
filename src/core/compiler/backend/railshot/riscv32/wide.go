package riscv32

import (
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	rv "github.com/wago-org/wago/src/core/encoder/riscv32"
)

var wideRegPool = []rv.Reg{
	rv.A0, rv.A1, rv.A2, rv.A3, rv.A4, rv.A5, rv.A6, rv.A7,
	rv.T0, rv.T1, rv.T2, rv.S0, rv.S1, rv.S2, rv.S3, rv.S4,
	rv.S5, rv.S6, rv.S7, rv.S8, rv.S9, rv.T3, rv.T4, rv.T5,
}

type wideValue struct {
	n    int
	regs [4]rv.Reg
}

type wideCompiler struct {
	a     rv.Asm
	stack []wideValue
	used  [32]bool
}

func (c *wideCompiler) alloc(n int) (wideValue, error) {
	v := wideValue{n: n}
	for _, r := range wideRegPool {
		if c.used[r] {
			continue
		}
		v.regs[v.n-n] = r
		c.used[r] = true
		n--
		if n == 0 {
			return v, nil
		}
	}
	for i := 0; i < v.n-n; i++ {
		c.used[v.regs[i]] = false
	}
	return wideValue{}, fmt.Errorf("riscv32: wide expression exceeds register capacity")
}
func (c *wideCompiler) release(v wideValue) {
	for i := 0; i < v.n; i++ {
		c.used[v.regs[i]] = false
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
	return v, nil
}
func quad(v wideValue) rv.Quad { return rv.Quad{v.regs[0], v.regs[1], v.regs[2], v.regs[3]} }

// CompileV128Beachhead lowers a strict straight-line v128 expression. It is the
// direct four-GPR SWAR beachhead used to measure which operations should stay
// inline instead of using the complete embedded32 helper fallback. The result
// returns in A0..A3 as little-endian words.
func CompileV128Beachhead(body []byte) ([]byte, error) {
	c := new(wideCompiler)
	r := wasm.NewReader(body)
	groups, err := r.U32()
	if err != nil {
		return nil, err
	}
	if groups != 0 {
		return nil, fmt.Errorf("riscv32: v128 beachhead does not support locals")
	}
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return nil, err
		}
		if op == 0x0b {
			if len(c.stack) != 1 {
				return nil, fmt.Errorf("riscv32: v128 result stack has %d values", len(c.stack))
			}
			v, _ := c.pop(4)
			want := [4]rv.Reg{rv.A0, rv.A1, rv.A2, rv.A3}
			if v.regs != want {
				return nil, fmt.Errorf("riscv32: non-canonical v128 result registers")
			}
			c.a.Ret()
			return c.a.B, nil
		}
		if op != 0xfd {
			return nil, fmt.Errorf("riscv32: v128 beachhead unsupported opcode %#x", op)
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

// CompileF64BitBeachhead lowers f64.const/abs/neg/copysign entirely through
// integer register pairs. Arithmetic and conversion opcodes use helper thunks.
func CompileF64BitBeachhead(body []byte) ([]byte, error) {
	c := new(wideCompiler)
	r := wasm.NewReader(body)
	groups, err := r.U32()
	if err != nil {
		return nil, err
	}
	if groups != 0 {
		return nil, fmt.Errorf("riscv32: f64 beachhead does not support locals")
	}
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return nil, err
		}
		switch op {
		case 0x0b:
			if len(c.stack) != 1 {
				return nil, fmt.Errorf("riscv32: f64 result stack has %d values", len(c.stack))
			}
			v, _ := c.pop(2)
			if v.regs[0] != rv.A0 || v.regs[1] != rv.A1 {
				return nil, fmt.Errorf("riscv32: non-canonical f64 result registers")
			}
			c.a.Ret()
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
			return nil, fmt.Errorf("riscv32: f64 bit beachhead unsupported opcode %#x", op)
		}
	}
	return nil, fmt.Errorf("riscv32: f64 body missing end")
}

// CompileI64Beachhead lowers straight-line two-GPR i64 constants and modular
// arithmetic. The little-endian result returns in A0/A1.
func CompileI64Beachhead(body []byte) ([]byte, error) {
	c := new(wideCompiler)
	r := wasm.NewReader(body)
	groups, err := r.U32()
	if err != nil {
		return nil, err
	}
	if groups != 0 {
		return nil, fmt.Errorf("riscv32: i64 beachhead does not support locals")
	}
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return nil, err
		}
		switch op {
		case 0x0b:
			if len(c.stack) != 1 {
				return nil, fmt.Errorf("riscv32: i64 result stack has %d values", len(c.stack))
			}
			v, _ := c.pop(2)
			if v.regs[0] != rv.A0 || v.regs[1] != rv.A1 {
				return nil, fmt.Errorf("riscv32: non-canonical i64 result registers")
			}
			c.a.Ret()
			return c.a.B, nil
		case 0x42:
			x, err := r.I64()
			if err != nil {
				return nil, err
			}
			v, err := c.alloc(2)
			if err != nil {
				return nil, err
			}
			c.a.MovImm32(v.regs[0], uint32(x))
			c.a.MovImm32(v.regs[1], uint32(uint64(x)>>32))
			c.push(v)
		case 0x7c, 0x7d, 0x7e, 0x83, 0x84, 0x85:
			b, err := c.pop(2)
			if err != nil {
				return nil, err
			}
			a, err := c.pop(2)
			if err != nil {
				return nil, err
			}
			switch op {
			case 0x83, 0x84, 0x85:
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
			default:
				out, err := c.alloc(2)
				if err != nil {
					return nil, err
				}
				scratchN := 1
				if op == 0x7e {
					scratchN = 2
				}
				s, err := c.alloc(scratchN)
				if err != nil {
					return nil, err
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
			}
		default:
			return nil, fmt.Errorf("riscv32: i64 beachhead unsupported opcode %#x", op)
		}
	}
	return nil, fmt.Errorf("riscv32: i64 body missing end")
}
