//go:build linux && !tinygo

package riscv64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func swarUnaryFixture(kind uint32, scalar bool) fixtureFunc {
	result := byte(0x7b)
	if scalar {
		result = 0x7f
	}
	body := uleb([]byte{0x20, 0, 0xfd}, kind)
	return fixtureFunc{params: []byte{0x7b}, result: result, body: append(body, 0x0b)}
}

func TestSWARUnaryIntegerAdmissionAndResultClasses(t *testing.T) {
	for _, base := range []uint32{96, 128, 160, 192} {
		for _, offset := range []uint32{0, 1, 3, 4} {
			kind := base + offset
			scalar := offset >= 3
			fn, ft := swarValidatedFunction(t, swarUnaryFixture(kind, scalar))
			if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); len(code) == 0 || err != nil {
				t.Fatal("valid unary integer vector", kind, err)
			}
			badType := *ft
			badType.Results = []wasm.ValType{wasm.I32}
			if scalar {
				badType.Results[0] = wasm.V128
			}
			if code, _, err := compileSWARPairs(fn, &badType, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("wrong unary result class emitted", kind, err)
			}
			for _, prefix := range [][]byte{nil, {0x41, 0}} {
				bad := *fn
				bad.BodyBytes = append(append([]byte(nil), prefix...), fn.BodyBytes[2:]...)
				if code, _, err := compileSWARPairs(&bad, ft, emissionMode{production: true}); code != nil || err == nil {
					t.Fatal("unary input underflow/class emitted", kind, err)
				}
			}
		}
	}
	fn, ft := swarValidatedFunction(t, swarUnaryFixture(227, false))
	if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); code != nil || err == nil {
		t.Fatal("pending vector FP sqrt emitted", err)
	}
}

func TestSWARUnaryIntegerHelpersRejectBeforeBytes(t *testing.T) {
	for _, test := range []struct {
		width     uint8
		operation swarUnary
	}{{0, swarAbs}, {7, swarNeg}, {24, swarAllTrue}, {65, swarBitmask}, {255, swarAbs}, {8, 4}, {64, 255}} {
		f := integerEmitter{}
		f.a.Addi(enc.X5, enc.Zero, 1)
		before := len(f.a.B)
		f.swarUnary(test.width, test.operation)
		if f.err == nil || len(f.a.B) != before {
			t.Fatal("invalid unary helper partially emitted", test, f.err)
		}
		f.swarUnary(8, swarAbs)
		if len(f.a.B) != before {
			t.Fatal("sticky unary error appended bytes")
		}
	}
}
