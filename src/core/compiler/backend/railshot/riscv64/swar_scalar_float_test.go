//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type swarScalarFloatCase struct {
	op, input, output   byte
	sub                 byte
	unary, fp, trapping bool
}

func swarScalarFloatCases() []swarScalarFloatCase {
	var cases []swarScalarFloatCase
	for op := byte(0x5b); op <= 0x66; op++ {
		input := byte(0x7d)
		if op >= 0x61 {
			input = 0x7c
		}
		cases = append(cases, swarScalarFloatCase{op: op, input: input, output: 0x7f, fp: true})
	}
	for op := byte(0x8b); op <= 0xa6; op++ {
		typ, n := byte(0x7d), op-0x8b
		if op >= 0x99 {
			typ, n = 0x7c, op-0x99
		}
		cases = append(cases, swarScalarFloatCase{op: op, input: typ, output: typ, unary: n <= 6, fp: n != 0 && n != 1 && n != 13})
	}
	for _, base := range []byte{0xa8, 0xae} {
		for n := byte(0); n < 4; n++ {
			input, output := byte(0x7d), byte(0x7f)
			if n >= 2 {
				input = 0x7c
			}
			if base == 0xae {
				output = 0x7e
			}
			cases = append(cases, swarScalarFloatCase{op: base + n, input: input, output: output, unary: true, fp: true, trapping: true})
		}
	}
	for n, inOut := range [][2]byte{{0x7f, 0x7d}, {0x7f, 0x7d}, {0x7e, 0x7d}, {0x7e, 0x7d}, {0x7c, 0x7d}, {0x7f, 0x7c}, {0x7f, 0x7c}, {0x7e, 0x7c}, {0x7e, 0x7c}, {0x7d, 0x7c}, {0x7d, 0x7f}, {0x7c, 0x7e}, {0x7f, 0x7d}, {0x7e, 0x7c}} {
		cases = append(cases, swarScalarFloatCase{op: 0xb2 + byte(n), input: inOut[0], output: inOut[1], unary: true, fp: n < 10})
	}
	for sub := byte(0); sub < 8; sub++ {
		input, output := byte(0x7d), byte(0x7f)
		if sub&2 != 0 {
			input = 0x7c
		}
		if sub&4 != 0 {
			output = 0x7e
		}
		cases = append(cases, swarScalarFloatCase{op: 0xfc, sub: sub, input: input, output: output, unary: true, fp: true})
	}
	return cases
}

func swarScalarFloatFixture(c swarScalarFloatCase) (fixtureFunc, int) {
	f := fixtureFunc{params: []byte{0x7b, c.input}, results: []byte{0x7b, c.output, 0x7b}, body: []byte{0x20, 0, 0x20, 1}}
	if !c.unary {
		f.params = append(f.params, c.input)
		f.body = append(f.body, 0x20, 2)
	}
	opOffset := len(f.body)
	f.body = append(f.body, c.op)
	if c.op == 0xfc {
		f.body = append(f.body, c.sub)
	}
	splat := map[byte]byte{0x7f: 17, 0x7e: 18, 0x7d: 19, 0x7c: 20}[c.output]
	f.body = append(f.body, 0x22, byte(len(f.params)), 0x20, byte(len(f.params)), 0xfd, splat, 0x0b)
	f.locals = []byte{2, 1, c.output, byte(127 - len(f.params)), 0x7b}
	return f, opOffset
}

func TestSWARScalarFloatExactClassesBridgesAndStaticRounding(t *testing.T) {
	cases := swarScalarFloatCases()
	if len(cases) != 70 {
		t.Fatal("FP fixture coverage", len(cases))
	}
	for _, c := range cases {
		fixture, _ := swarScalarFloatFixture(c)
		fn, ft := swarValidatedFunction(t, fixture)
		summary, err := walkSWARPairs(fn, ft, false, nil)
		if err != nil || summary.usesFP != c.fp || summary.usesTraps != c.trapping {
			t.Fatal("scalar FP admission flags", c, summary, err)
		}
		for _, production := range []bool{false, true} {
			code, info, err := compileSWARPairs(fn, ft, emissionMode{production: production})
			if c.fp && !production {
				if code != nil || err == nil {
					t.Fatal("isolated scalar FCSR bridge admitted", c, err)
				}
				continue
			}
			if len(code) == 0 || err != nil || info.FrameBytes <= 2048 || info.Results != 5 {
				t.Fatal("mixed scalar FP lowering", c, info, err)
			}
			for offset := 0; offset < len(code); offset += 4 {
				word := binary.LittleEndian.Uint32(code[offset:])
				if word&127 != 0x53 {
					continue
				}
				if !c.fp {
					t.Fatal("raw scalar operation emitted FP", c, offset)
				}
				f7 := word >> 25
				if f7 <= 0x0d || f7 >= 0x20 && f7 <= 0x2d || f7 >= 0x60 && f7 <= 0x69 {
					if mode := word >> 12 & 7; mode > 4 {
						t.Fatal("dynamic scalar rounding mode", c, offset, mode)
					}
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
					t.Fatal("FP input carrier emitted", c, operand, wrong, err)
				}
			}
		}
		bad := *ft
		bad.Results = append([]wasm.ValType(nil), ft.Results...)
		bad.Results[1] = wasm.V128
		if code, _, err := compileSWARPairs(fn, &bad, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("FP result carrier emitted", c, err)
		}
		body := []byte{c.op}
		if c.op == 0xfc {
			body = append(body, c.sub)
		}
		body = append(body, 0x0b)
		if code, _, err := compileSWARPairs(&wasm.Func{BodyBytes: body}, ft, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("FP underflow emitted", c, err)
		}
	}
	for _, sub := range []uint32{8, 18, 255, ^uint32(0)} {
		body := uleb([]byte{0xfc}, sub)
		body = append(body, 0x0b)
		if code, _, err := compileSWARPairs(&wasm.Func{BodyBytes: body}, &wasm.CompType{Kind: wasm.CompFunc}, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("pending prefixed scalar emitted", sub, err)
		}
	}
}
