//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

var swarFloatConvertKinds = []uint32{94, 95, 248, 249, 250, 251, 252, 253, 254, 255}

func TestSWARFloatingConversionAdmissionAndMapping(t *testing.T) {
	wants := []swarFloatConversion{
		{64, 32, 2, 0xb6, false, false}, {32, 64, 2, 0xbb, false, false},
		{32, 32, 4, 0, true, false}, {32, 32, 4, 0, true, true},
		{32, 32, 4, 0xb2, false, false}, {32, 32, 4, 0xb3, false, false},
		{64, 32, 2, 0, true, false}, {64, 32, 2, 0, true, true},
		{32, 64, 2, 0xb7, false, false}, {32, 64, 2, 0xb8, false, false},
	}
	for i, kind := range swarFloatConvertKinds {
		conversion, ok := swarFloatConversionOpcode(kind)
		if !ok || conversion != wants[i] {
			t.Fatal("vector conversion mapping", kind, conversion, ok)
		}
		fn, ft := swarValidatedFunction(t, swarUnaryFixture(kind, false))
		if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); len(code) == 0 || err != nil {
			t.Fatal("valid vector conversion", kind, err)
		}
		if code, _, err := compileSWARPairs(fn, ft, emissionMode{}); code != nil || err == nil {
			t.Fatal("conversion through isolated bridge", kind, err)
		}
		badType := *ft
		badType.Results = []wasm.ValType{wasm.I32}
		if code, _, err := compileSWARPairs(fn, &badType, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("conversion result class", kind, err)
		}
		for _, prefix := range [][]byte{nil, {0x41, 0}, {0x42, 0}, {0x44, 0, 0, 0, 0, 0, 0, 0, 0}} {
			bad := *fn
			bad.BodyBytes = append(append([]byte(nil), prefix...), fn.BodyBytes[2:]...)
			if code, _, err := compileSWARPairs(&bad, ft, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("conversion underflow/carrier", kind, err)
			}
		}
		f := integerEmitter{}
		f.swarFloatConvert(kind)
		seen := 0
		for offset := 0; offset < len(f.a.B); offset += 4 {
			word := binary.LittleEndian.Uint32(f.a.B[offset:])
			funct7 := (word >> 25) & 0x7f
			if word&0x7f == 0x53 && (funct7 == 0x20 || funct7 == 0x21 || funct7 == 0x60 || funct7 == 0x61 || funct7 == 0x68 || funct7 == 0x69) {
				seen++
				mode := enc.RNE
				if kind == 248 || kind == 249 || kind == 252 || kind == 253 {
					mode = enc.RTZ
				}
				if (word>>12)&7 != uint32(mode) {
					t.Fatal("vector conversion static rounding", kind, word)
				}
			}
		}
		if f.err != nil || f.a.Err() != nil || seen != int(conversion.lanes) {
			t.Fatal("native vector conversion lanes", kind, seen, f.err, f.a.Err())
		}
	}
	for _, kind := range []uint32{0, 93, 96, 247, 256, ^uint32(0)} {
		if _, ok := swarFloatConversionOpcode(kind); ok {
			t.Fatal("conversion neighbor mapped", kind)
		}
		f := integerEmitter{}
		f.a.Addi(enc.X5, enc.Zero, 0)
		before := len(f.a.B)
		f.swarFloatConvert(kind)
		if f.err == nil || len(f.a.B) != before {
			t.Fatal("invalid conversion emitted", kind)
		}
		f.swarFloatConvert(94)
		if len(f.a.B) != before {
			t.Fatal("sticky conversion appended")
		}
	}
}
