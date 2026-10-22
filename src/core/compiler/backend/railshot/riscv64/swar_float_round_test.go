//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

var swarFloatRoundKinds = []uint32{103, 104, 105, 106, 116, 117, 122, 148}

func TestSWARFloatingRoundAdmissionAndStaticModes(t *testing.T) {
	for i, kind := range swarFloatRoundKinds {
		width, mode, ok := swarFloatRoundOpcode(kind)
		wantWidth := uint8(32)
		if i >= 4 {
			wantWidth = 64
		}
		wantMode := []enc.RoundingMode{enc.RUP, enc.RDN, enc.RTZ, enc.RNE}[i%4]
		if !ok || width != wantWidth || mode != wantMode {
			t.Fatal("FP round mapping", kind, width, mode, ok)
		}
		fn, ft := swarValidatedFunction(t, swarUnaryFixture(kind, false))
		if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); len(code) == 0 || err != nil {
			t.Fatal("valid FP round", kind, err)
		}
		if code, _, err := compileSWARPairs(fn, ft, emissionMode{}); code != nil || err == nil {
			t.Fatal("FP round isolated FCSR boundary", kind, err)
		}
		badType := *ft
		badType.Results = []wasm.ValType{wasm.F64}
		if code, _, err := compileSWARPairs(fn, &badType, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("FP round result class", kind, err)
		}
		for _, prefix := range [][]byte{nil, {0x41, 0}, {0x42, 0}, {0x43, 0, 0, 0, 0}} {
			bad := *fn
			bad.BodyBytes = append(append([]byte(nil), prefix...), fn.BodyBytes[2:]...)
			if code, _, err := compileSWARPairs(&bad, ft, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("FP round underflow/class", kind, err)
			}
		}
		f := integerEmitter{}
		f.swarFloatRound(width, mode)
		seenInt, seenFP := 0, 0
		for offset := 0; offset < len(f.a.B); offset += 4 {
			word := binary.LittleEndian.Uint32(f.a.B[offset:])
			if word&0x7f != 0x53 {
				continue
			}
			funct7 := (word >> 25) & 0x7f
			switch funct7 {
			case 0x60, 0x61:
				seenInt++
				if (word>>12)&7 != uint32(wantMode) {
					t.Fatal("integral conversion rounding", kind, word)
				}
			case 0x68, 0x69:
				seenFP++
				if (word>>12)&7 != uint32(enc.RNE) {
					t.Fatal("back conversion rounding", kind, word)
				}
			}
		}
		if f.err != nil || f.a.Err() != nil || seenInt != 128/int(width) || seenFP != seenInt {
			t.Fatal("rounding conversion lanes", kind, seenInt, seenFP, f.err, f.a.Err())
		}
	}
	for _, kind := range []uint32{0, 102, 107, 115, 118, 121, 123, 147, 149, ^uint32(0)} {
		if _, _, ok := swarFloatRoundOpcode(kind); ok {
			t.Fatal("FP round neighbor", kind)
		}
	}
}

func TestSWARFloatingRoundRejectsInvalidBeforeEmission(t *testing.T) {
	for _, test := range []struct {
		width uint8
		mode  enc.RoundingMode
	}{{0, enc.RNE}, {16, enc.RUP}, {63, enc.RTZ}, {65, enc.RDN}, {255, enc.RNE}, {32, enc.RMM}, {64, enc.Dynamic}, {32, 255}} {
		f := integerEmitter{}
		f.a.Addi(enc.X5, enc.Zero, 0)
		before := len(f.a.B)
		f.swarFloatRound(test.width, test.mode)
		if f.err == nil || len(f.a.B) != before {
			t.Fatal("invalid round emitted", test, f.err)
		}
		f.swarFloatRound(32, enc.RNE)
		if len(f.a.B) != before {
			t.Fatal("sticky round error appended")
		}
	}
}
