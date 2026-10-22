//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func swarRelaxedDotOracle(input []byte, add bool) [16]byte {
	var result [16]byte
	var pairs [8]int32
	for lane := range pairs {
		offset := 2 * lane
		value := int32(int8(input[offset]))*int32(int8(input[16+offset])) + int32(int8(input[offset+1]))*int32(int8(input[17+offset]))
		if value > 32767 {
			value = 32767
		} else if value < -32768 {
			value = -32768
		}
		pairs[lane] = value
		if !add {
			binary.LittleEndian.PutUint16(result[offset:], uint16(value))
		}
	}
	if add {
		for lane := 0; lane < 4; lane++ {
			value := int64(pairs[2*lane]) + int64(pairs[2*lane+1]) + int64(int32(binary.LittleEndian.Uint32(input[32+4*lane:])))
			binary.LittleEndian.PutUint32(result[4*lane:], uint32(value))
		}
	}
	return result
}

func TestSWARRelaxedDotAdmissionAndSignedSaturation(t *testing.T) {
	for _, kind := range []uint32{274, 275} {
		fn, ft := swarValidatedFunction(t, swarRelaxedFixture(kind))
		for _, mode := range []emissionMode{{}, {production: true}} {
			code, _, err := compileSWARPairs(fn, ft, mode)
			if len(code) == 0 || err != nil {
				t.Fatal("relaxed dot", kind, mode, err)
			}
			for offset := 0; offset < len(code); offset += 4 {
				switch binary.LittleEndian.Uint32(code[offset:]) & 0x7f {
				case 0x07, 0x27, 0x43, 0x47, 0x4b, 0x4f, 0x53:
					t.Fatal("GP relaxed dot emitted FP", kind, offset)
				}
			}
		}
		badType := *ft
		badType.Results = []wasm.ValType{wasm.I32}
		if code, _, err := compileSWARPairs(fn, &badType, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("dot result carrier", kind, err)
		}
		arity := len(ft.Params)
		for operand := 0; operand < arity; operand++ {
			bad := *fn
			bad.BodyBytes = append([]byte(nil), fn.BodyBytes...)
			copy(bad.BodyBytes[operand*2:], []byte{0x41, 0})
			if code, _, err := compileSWARPairs(&bad, ft, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("dot input carrier", kind, operand, err)
			}
		}
		for count := 0; count < arity; count++ {
			bad := *fn
			bad.BodyBytes = append(append([]byte(nil), fn.BodyBytes[:2*count]...), fn.BodyBytes[2*arity:]...)
			if code, _, err := compileSWARPairs(&bad, ft, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("dot input underflow", kind, count, err)
			}
		}
	}
	for _, kind := range []uint32{0, 273, 276, ^uint32(0)} {
		f := integerEmitter{}
		f.a.Addi(enc.X5, enc.Zero, 0)
		before := len(f.a.B)
		f.swarRelaxedDot(kind)
		if f.err == nil || len(f.a.B) != before {
			t.Fatal("invalid dot emitted", kind)
		}
		f.swarRelaxedDot(274)
		if len(f.a.B) != before {
			t.Fatal("sticky dot appended")
		}
	}
	var input [48]byte
	for i := 0; i < 32; i++ {
		input[i] = 0x80
	}
	dot, add := swarRelaxedDotOracle(input[:], false), swarRelaxedDotOracle(input[:], true)
	for lane := 0; lane < 8; lane++ {
		if binary.LittleEndian.Uint16(dot[2*lane:]) != 32767 {
			t.Fatal("dot positive saturation edge", dot)
		}
	}
	for lane := 0; lane < 4; lane++ {
		if binary.LittleEndian.Uint32(add[4*lane:]) != 65534 {
			t.Fatal("dot-add must saturate intermediate pairs", add)
		}
	}
}
