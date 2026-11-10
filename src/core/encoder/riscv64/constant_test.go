package riscv64

import (
	"encoding/binary"
	"math"
	"math/rand"
	"testing"
)

// This small decoder models emitted instructions from ISA fields, independently
// of the encoding helpers. It is a host-independent oracle, not native execution.
func model(t *testing.T, code []byte, regs [32]uint64) [32]uint64 {
	t.Helper()
	regs[0] = 0
	for pc := 0; pc < len(code); pc += 4 {
		w := binary.LittleEndian.Uint32(code[pc:])
		op, dst, left, right := w&127, (w>>7)&31, (w>>15)&31, (w>>20)&31
		f3, f7 := (w>>12)&7, w>>25
		x, y, imm := regs[left], regs[right], int64(int32(w)>>20)
		var result uint64
		switch op {
		case 0x37:
			result = uint64(int64(int32(w & 0xfffff000)))
		case 0x13, 0x1b:
			count := (w >> 20) & 63
			if op == 0x1b {
				count &= 31
			}
			switch f3 {
			case 0:
				result = x + uint64(imm)
			case 1:
				result = x << count
			case 5:
				if op == 0x1b {
					x = uint64(uint32(x))
				}
				if w&(1<<30) != 0 {
					if op == 0x1b {
						x = uint64(int64(int32(x)))
					}
					result = uint64(int64(x) >> count)
				} else {
					result = x >> count
				}
			default:
				t.Fatalf("model: unknown immediate word %#08x", w)
			}
		case 0x33, 0x3b:
			count := y & 63
			if op == 0x3b {
				count, x = y&31, uint64(uint32(x))
			}
			switch f3 {
			case 0:
				switch f7 {
				case 0:
					result = x + y
				case 0x20:
					result = x - y
				case 1:
					result = x * y
				default:
					t.Fatalf("model: unknown arithmetic word %#08x", w)
				}
			case 1:
				result = x << count
			case 5:
				if f7 == 0x20 {
					if op == 0x3b {
						x = uint64(int64(int32(x)))
					}
					result = uint64(int64(x) >> count)
				} else {
					result = x >> count
				}
			default:
				t.Fatalf("model: unknown register word %#08x", w)
			}
		default:
			t.Fatalf("model: unknown opcode %#08x", w)
		}
		if op == 0x1b || op == 0x3b {
			result = uint64(int64(int32(result)))
		}
		if dst != 0 {
			regs[dst] = result
		}
	}
	return regs
}

func TestConstantProperties(t *testing.T) {
	values := []int64{0, 1, -1, 2047, 2048, -2048, -2049, 4095, 4096, -4096,
		0x7ffff7ff, 0x7ffff800, 0x7fffffff, 0x80000000, 0xffffffff,
		math.MinInt32, math.MaxInt32, math.MinInt64, math.MinInt64 + 2048,
		math.MaxInt64, math.MaxInt64 - 2048, 0x123456789abcdef0, -0x123456789abcdef}
	for bit := 0; bit < 64; bit++ {
		v := int64(uint64(1) << bit)
		values = append(values, v, v-1, v+1, ^v)
	}
	rng := rand.New(rand.NewSource(802))
	for i := 0; i < 8192; i++ {
		values = append(values, int64(rng.Uint64()))
	}
	for _, dst := range []Reg{X5, X10, X29, X31} {
		for _, value := range values {
			var a Asm
			a.Constant64(dst, value)
			if a.Err() != nil || a.Len() == 0 || a.Len() > 32 {
				t.Fatalf("constant %#x: bytes=%d err=%v", value, a.Len(), a.Err())
			}
			var regs [32]uint64
			for i := 1; i < len(regs); i++ {
				regs[i] = rng.Uint64()
			}
			want := regs
			want[dst] = uint64(value)
			if got := model(t, a.B, regs); got != want {
				t.Fatalf("constant %#x in x%d changed unexpected bits/registers; code %x", value, dst, a.B)
			}
		}
	}
}

func TestConstant32AndNormalization(t *testing.T) {
	rng := rand.New(rand.NewSource(32))
	values := []uint32{0, 1, 0x7ffff7ff, 0x7ffff800, 0x7fffffff, 0x80000000, 0xffffffff}
	for i := 0; i < 4096; i++ {
		values = append(values, rng.Uint32())
	}
	for _, value := range values {
		var a Asm
		a.Constant32(X5, value)
		got := model(t, a.B, [32]uint64{})
		if a.Err() != nil || a.Len() > 16 || got[X5] != uint64(value) {
			t.Fatalf("i32 constant %#x: got %#x err=%v", value, got[X5], a.Err())
		}
		for _, dst := range []Reg{X5, X6} {
			var regs [32]uint64
			regs[X5] = uint64(rng.Uint32())<<32 | uint64(value)
			before := regs[X5]
			a = Asm{}
			a.ZeroExtend32(dst, X5)
			got := model(t, a.B, regs)
			if a.Err() != nil || got[dst] != uint64(value) || dst != X5 && got[X5] != before {
				t.Fatalf("normalization alias=%t raw=%#x got=%#x", dst == X5, before, got[dst])
			}
		}
	}
}

func TestWordResultsAndAliasing(t *testing.T) {
	ops := []struct {
		name string
		emit func(*Asm, Reg, Reg, Reg)
		want func(uint32, uint32) uint32
	}{
		{"addw", (*Asm).Addw, func(x, y uint32) uint32 { return x + y }},
		{"subw", (*Asm).Subw, func(x, y uint32) uint32 { return x - y }},
		{"mulw", (*Asm).Mulw, func(x, y uint32) uint32 { return x * y }},
		{"sllw", (*Asm).Sllw, func(x, y uint32) uint32 { return x << (y & 31) }},
		{"srlw", (*Asm).Srlw, func(x, y uint32) uint32 { return x >> (y & 31) }},
		{"sraw", (*Asm).Sraw, func(x, y uint32) uint32 { return uint32(int32(x) >> (y & 31)) }},
	}
	for _, op := range ops {
		for _, dst := range []Reg{X5, X6, X7} {
			for _, x := range []uint64{0, 1, 0x80000000, 0xffffffff, 0xdeadbeef00000000, 0x1234567880000000} {
				for _, y := range []uint64{0, 1, 31, 32, 63, 64, 0xabcdef0100000001, 0xffffffff} {
					var regs [32]uint64
					regs[X5], regs[X6] = x, y
					var a Asm
					op.emit(&a, dst, X5, X6)
					word := op.want(uint32(x), uint32(y))
					got := model(t, a.B, regs)
					if got[dst] != uint64(int64(int32(word))) {
						t.Fatalf("%s raw result %x/%x = %x", op.name, x, y, got[dst])
					}
					a.ZeroExtend32(dst, dst)
					want := regs
					want[dst] = uint64(word)
					if got := model(t, a.B, regs); got != want {
						t.Fatalf("%s normalized/alias result x%d, x=%x y=%x", op.name, dst, x, y)
					}
				}
			}
		}
	}
}

func TestConstantAndFixupNoAllocations(t *testing.T) {
	buf := make([]byte, 0, 64)
	a := Asm{B: buf}
	allocs := testing.AllocsPerRun(100, func() {
		a.B = a.B[:0]
		a.Constant64(X10, 0x123456789abcdef0)
		site := a.JumpLong(false)
		if err := a.PatchLong(site, 0); err != nil {
			panic(err)
		}
	})
	if a.Err() != nil || allocs != 0 {
		t.Fatalf("err=%v allocations=%g", a.Err(), allocs)
	}
}

func FuzzConstant64(f *testing.F) {
	for _, value := range []int64{0, -1, math.MinInt64, math.MaxInt64, 0x7fffffff, 0x80000000} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value int64) {
		var a Asm
		a.Constant64(X10, value)
		got := model(t, a.B, [32]uint64{})
		if a.Err() != nil || a.Len() > 32 || got[X10] != uint64(value) {
			t.Fatalf("constant=%#x got=%#x len=%d err=%v", value, got[X10], a.Len(), a.Err())
		}
	})
}
