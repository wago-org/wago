//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
	"math/bits"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type swarScalarIntegerCase struct {
	op                      byte
	wide, unary, resultWide bool
	trapping                bool
}

// Describe the Wasm opcode families independently of integerOpcode, so a
// changed decoder mapping cannot silently change the fixtures and oracle.
func swarScalarIntegerCases() []swarScalarIntegerCase {
	var cases []swarScalarIntegerCase
	for op := byte(0x45); op <= 0x5a; op++ {
		cases = append(cases, swarScalarIntegerCase{op: op, wide: op >= 0x50, unary: op == 0x45 || op == 0x50})
	}
	for op := byte(0x67); op <= 0x8a; op++ {
		wide := op >= 0x79
		cases = append(cases, swarScalarIntegerCase{op: op, wide: wide, unary: op <= 0x69 || op >= 0x79 && op <= 0x7b,
			resultWide: wide, trapping: op >= 0x6d && op <= 0x70 || op >= 0x7f && op <= 0x82})
	}
	cases = append(cases, swarScalarIntegerCase{op: 0xa7, wide: true, unary: true},
		swarScalarIntegerCase{op: 0xac, unary: true, resultWide: true}, swarScalarIntegerCase{op: 0xad, unary: true, resultWide: true})
	for op := byte(0xc0); op <= 0xc4; op++ {
		cases = append(cases, swarScalarIntegerCase{op: op, wide: op >= 0xc2, unary: true, resultWide: op >= 0xc2})
	}
	return cases
}

// Keep a vector live across the integer operation, transport the scalar
// through a local, then use that result as a vector splat. The padded frame
// also exercises escaped homes and compact mixed argument/result cells.
func swarScalarIntegerFixture(c swarScalarIntegerCase) (fixtureFunc, int) {
	typ, result, splat := byte(0x7f), byte(0x7f), byte(17)
	if c.wide {
		typ = 0x7e
	}
	if c.resultWide {
		result, splat = 0x7e, 18
	}
	f := fixtureFunc{params: []byte{0x7b, typ}, results: []byte{0x7b, result, 0x7b}, body: []byte{0x20, 0, 0x20, 1}}
	if !c.unary {
		f.params = append(f.params, typ)
		f.body = append(f.body, 0x20, 2)
	}
	opOffset := len(f.body)
	f.body = append(f.body, c.op, 0x22, byte(len(f.params)), 0x20, byte(len(f.params)), 0xfd, splat, 0x0b)
	f.locals = []byte{2, 1, result, byte(127 - len(f.params)), 0x7b}
	return f, opOffset
}

func TestSWARScalarIntegerAdmissionClassesAndTrapBridge(t *testing.T) {
	cases := swarScalarIntegerCases()
	if len(cases) != 66 {
		t.Fatal("integer fixture coverage", len(cases))
	}
	for _, c := range cases {
		fixture, _ := swarScalarIntegerFixture(c)
		fn, ft := swarValidatedFunction(t, fixture)
		summary, err := walkSWARPairs(fn, ft, false, nil)
		if err != nil || summary.usesFP || summary.usesTraps != c.trapping {
			t.Fatal("integer admission flags", c, summary, err)
		}
		for _, production := range []bool{false, true} {
			code, info, err := compileSWARPairs(fn, ft, emissionMode{production: production})
			if c.trapping && !production {
				if code != nil || err == nil {
					t.Fatal("isolated integer trap bridge admitted", c, err)
				}
				continue
			}
			if len(code) == 0 || err != nil || info.FrameBytes <= 2048 || info.Results != 5 {
				t.Fatal("mixed integer lowering", c, info, err)
			}
			for offset := 0; offset < len(code); offset += 4 {
				switch binary.LittleEndian.Uint32(code[offset:]) & 0x7f {
				case 0x07, 0x27, 0x43, 0x47, 0x4b, 0x4f, 0x53:
					t.Fatal("GP integer emitted FP", c, offset)
				}
			}
		}
		for operand := 1; operand < len(ft.Params); operand++ {
			for _, wrong := range []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64, wasm.V128} {
				if wasm.EqualValType(wrong, ft.Params[operand]) {
					continue
				}
				bad := *ft
				bad.Params = append([]wasm.ValType(nil), ft.Params...)
				bad.Params[operand] = wrong
				if code, _, err := compileSWARPairs(fn, &bad, emissionMode{production: true}); code != nil || err == nil {
					t.Fatal("integer input carrier emitted", c, operand, wrong, err)
				}
			}
		}
		bad := *ft
		bad.Results = append([]wasm.ValType(nil), ft.Results...)
		bad.Results[1] = wasm.F32
		if c.resultWide {
			bad.Results[1] = wasm.F64
		}
		if code, _, err := compileSWARPairs(fn, &bad, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("integer result carrier emitted", c, err)
		}
		arity := 2
		if c.unary {
			arity = 1
		}
		for count := 0; count < arity; count++ {
			body := []byte{}
			for range count {
				if c.wide {
					body = append(body, 0x42, 1)
				} else {
					body = append(body, 0x41, 1)
				}
			}
			body = append(body, c.op, 0x0b)
			if code, _, err := compileSWARPairs(&wasm.Func{BodyBytes: body}, ft, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("integer underflow emitted", c, count, err)
			}
		}
	}
}

// Host arithmetic and bit operations provide expected values without running
// another architecture's machine code or using the compiler's opcode mapping.
func swarScalarIntegerExpected(c swarScalarIntegerCase, a, b uint64) (uint64, int) {
	if !c.wide {
		a, b = uint64(uint32(a)), uint64(uint32(b))
	}
	sa, sb, minimum := int64(a), int64(b), int64(-1<<63)
	if !c.wide {
		sa, sb, minimum = int64(int32(a)), int64(int32(b)), -1<<31
	}
	var result uint64
	if c.op >= 0x46 && c.op <= 0x4f || c.op >= 0x51 && c.op <= 0x5a {
		base := byte(0x46)
		if c.wide {
			base = 0x51
		}
		comparisons := [...]bool{a == b, a != b, sa < sb, a < b, sa > sb, a > b, sa <= sb, a <= b, sa >= sb, a >= b}
		if comparisons[c.op-base] {
			result = 1
		}
	} else if !c.unary {
		base := byte(0x6a)
		if c.wide {
			base = 0x7c
		}
		shift := b & 63
		if !c.wide {
			shift = b & 31
		}
		switch c.op - base {
		case 0:
			result = a + b
		case 1:
			result = a - b
		case 2:
			result = a * b
		case 3, 4, 5, 6:
			if b == 0 {
				return 0, TrapDivideByZero
			}
			if c.op-base == 3 && sa == minimum && sb == -1 {
				return 0, TrapIntegerOverflow
			}
			switch c.op - base {
			case 3:
				result = uint64(sa / sb)
			case 4:
				result = a / b
			case 5:
				result = uint64(sa % sb)
			case 6:
				result = a % b
			}
		case 7:
			result = a & b
		case 8:
			result = a | b
		case 9:
			result = a ^ b
		case 10:
			result = a << shift
		case 11:
			result = uint64(sa >> shift)
		case 12:
			result = a >> shift
		case 13, 14:
			n := int(shift)
			if c.op-base == 14 {
				n = -n
			}
			if c.wide {
				result = bits.RotateLeft64(a, n)
			} else {
				result = uint64(bits.RotateLeft32(uint32(a), n))
			}
		}
	} else {
		switch c.op {
		case 0x45, 0x50:
			if a == 0 {
				result = 1
			}
		case 0x67:
			result = uint64(bits.LeadingZeros32(uint32(a)))
		case 0x68:
			result = uint64(bits.TrailingZeros32(uint32(a)))
		case 0x69:
			result = uint64(bits.OnesCount32(uint32(a)))
		case 0x79:
			result = uint64(bits.LeadingZeros64(a))
		case 0x7a:
			result = uint64(bits.TrailingZeros64(a))
		case 0x7b:
			result = uint64(bits.OnesCount64(a))
		case 0xa7, 0xad:
			result = uint64(uint32(a))
		case 0xac, 0xc4:
			result = uint64(int64(int32(a)))
		case 0xc0, 0xc2:
			result = uint64(int64(int8(a)))
		case 0xc1, 0xc3:
			result = uint64(int64(int16(a)))
		}
	}
	if !c.resultWide {
		result = uint64(uint32(result))
	}
	return result, 0
}

func TestSWARScalarIntegerOracleEdges(t *testing.T) {
	for _, test := range []struct {
		op         byte
		a, b, want uint64
		trap       int
	}{
		{0x6a, 0xdeadbeefffffffff, 1, 0, 0},
		{0x48, 0xdeadbeef80000000, 0, 1, 0},
		{0x49, 0xdeadbeef80000000, 0, 0, 0},
		{0x74, 1, 32, 1, 0},
		{0x75, 0xdeadbeef80000000, 31, 0xffffffff, 0},
		{0x6d, 0x80000000, 0xffffffff, 0, TrapIntegerOverflow},
		{0x6f, 0x80000000, 0xffffffff, 0, 0},
		{0x80, 7, 0, 0, TrapDivideByZero},
		{0xac, 0xdeadbeef80000000, 0, 0xffffffff80000000, 0},
		{0xc0, 0x80, 0, 0xffffff80, 0},
	} {
		found := false
		for _, c := range swarScalarIntegerCases() {
			if c.op != test.op {
				continue
			}
			found = true
			got, trap := swarScalarIntegerExpected(c, test.a, test.b)
			if got != test.want || trap != test.trap {
				t.Fatal("integer oracle edge", test, got, trap)
			}
		}
		if !found {
			t.Fatal("missing integer oracle opcode", test.op)
		}
	}
}
