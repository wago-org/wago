//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

var swarFloatMathKinds = []uint32{224, 225, 227, 228, 229, 230, 231, 236, 237, 239, 240, 241, 242, 243}

func TestSWARFloatingMathAdmissionAndMapping(t *testing.T) {
	for _, kind := range swarFloatMathKinds {
		width, operation, unary, ok := swarFloatMathOpcode(kind)
		wantWidth, base := uint8(32), uint32(224)
		if kind >= 236 {
			wantWidth, base = 64, 236
		}
		wantOperation := swarFloatMath(kind - base)
		if kind-base >= 3 {
			wantOperation--
		}
		if !ok || width != wantWidth || operation != wantOperation || unary != (operation <= swarFloatSqrt) {
			t.Fatal("floating math mapping", kind, width, operation, unary, ok)
		}
		fixture := swarArithmeticFixture(kind)
		if unary {
			fixture = swarUnaryFixture(kind, false)
		}
		fn, ft := swarValidatedFunction(t, fixture)
		if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); len(code) == 0 || err != nil {
			t.Fatal("valid floating math", kind, err)
		}
		code, _, err := compileSWARPairs(fn, ft, emissionMode{})
		if operation >= swarFloatSqrt {
			if code != nil || err == nil {
				t.Fatal("FP math through isolated bridge", kind, err)
			}
		} else {
			if len(code) == 0 || err != nil {
				t.Fatal("raw sign operation rejected isolated", kind, err)
			}
			for i := 0; i < len(code); i += 4 {
				switch binary.LittleEndian.Uint32(code[i:]) & 0x7f {
				case 0x07, 0x27, 0x43, 0x47, 0x4b, 0x4f, 0x53:
					t.Fatal("raw sign operation emitted FP", kind, i)
				}
			}
		}
		badType := *ft
		badType.Results = []wasm.ValType{wasm.F64}
		if code, _, err := compileSWARPairs(fn, &badType, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("floating math result class", kind, err)
		}
		prefixes := [][]byte{nil, {0x20, 0}, {0x20, 0, 0x42, 0}, {0x41, 0, 0x20, 1}}
		argsBytes := 4
		if unary {
			prefixes = [][]byte{nil, {0x41, 0}, {0x42, 0}}
			argsBytes = 2
		}
		for _, prefix := range prefixes {
			bad := *fn
			bad.BodyBytes = append(append([]byte(nil), prefix...), fn.BodyBytes[argsBytes:]...)
			if code, _, err := compileSWARPairs(&bad, ft, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("floating math carrier/underflow", kind, err)
			}
		}
	}
	for _, kind := range []uint32{0, 223, 226, 232, 233, 234, 235, 238, 244, 245, 246, 247, ^uint32(0)} {
		if _, _, _, ok := swarFloatMathOpcode(kind); ok {
			t.Fatal("floating math neighbor mapped", kind)
		}
	}
}

func TestSWARFloatingMathStaticRNEAndStickyErrors(t *testing.T) {
	for _, width := range []uint8{32, 64} {
		for operation := swarFloatSqrt; operation <= swarFloatDiv; operation++ {
			f := integerEmitter{}
			f.swarFloatMath(width, operation)
			if f.err != nil || f.a.Err() != nil {
				t.Fatal(width, operation, f.err, f.a.Err())
			}
			seen := false
			for i := 0; i < len(f.a.B); i += 4 {
				word := binary.LittleEndian.Uint32(f.a.B[i:])
				if word&0x7f == 0x53 && (word>>25)&0x7f < 0x60 {
					seen = true
					if (word>>12)&7 != uint32(enc.RNE) {
						t.Fatal("dynamic/nonRNE arithmetic", width, operation, word)
					}
				}
			}
			if !seen {
				t.Fatal("no native FP arithmetic", width, operation)
			}
		}
	}
	for _, test := range []struct {
		width     uint8
		operation swarFloatMath
	}{{0, swarFloatAbs}, {16, swarFloatAbs}, {63, swarFloatAdd}, {65, swarFloatDiv}, {255, swarFloatNeg}, {32, 7}, {64, 255}} {
		f := integerEmitter{}
		f.a.Addi(enc.X5, enc.Zero, 0)
		before := len(f.a.B)
		f.swarFloatMath(test.width, test.operation)
		if f.err == nil || len(f.a.B) != before {
			t.Fatal("invalid float math emitted", test, f.err)
		}
		f.swarFloatMath(32, swarFloatAdd)
		if len(f.a.B) != before {
			t.Fatal("sticky float math error appended")
		}
	}
}
