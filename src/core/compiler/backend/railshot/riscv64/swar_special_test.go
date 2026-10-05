//go:build linux && !tinygo

package riscv64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func TestSWARSpecialArithmeticAdmission(t *testing.T) {
	for _, kind := range []uint32{98, 130, 186} {
		fixture := swarArithmeticFixture(kind)
		prefixes := [][]byte{nil, {0x20, 0}, {0x20, 0, 0x42, 0}, {0x41, 0, 0x20, 1}}
		argsBytes := 4
		if kind == 98 {
			fixture = swarUnaryFixture(kind, false)
			prefixes = [][]byte{nil, {0x41, 0}, {0x42, 0}}
			argsBytes = 2
		}
		fn, ft := swarValidatedFunction(t, fixture)
		if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); len(code) == 0 || err != nil {
			t.Fatal("special arithmetic", kind, err)
		}
		badType := *ft
		badType.Results = []wasm.ValType{wasm.F64}
		if code, _, err := compileSWARPairs(fn, &badType, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("wrong special result class", kind, err)
		}
		for _, prefix := range prefixes {
			bad := *fn
			bad.BodyBytes = append(append([]byte(nil), prefix...), fn.BodyBytes[argsBytes:]...)
			if code, _, err := compileSWARPairs(&bad, ft, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("special input class/underflow", kind, err)
			}
		}
	}
	for _, kind := range []uint32{256} {
		fn, ft := swarValidatedFunction(t, swarArithmeticFixture(kind))
		if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("pending vector FP admitted", kind, err)
		}
	}
}

func TestSWARSpecialArithmeticHelperErrorsStaySticky(t *testing.T) {
	for _, kind := range []uint32{0, 98, 129, 131, 185, 187, ^uint32(0)} {
		f := integerEmitter{}
		f.a.Addi(enc.X5, enc.Zero, 1)
		before := len(f.a.B)
		f.swarSpecialMultiply(kind)
		if f.err == nil || len(f.a.B) != before {
			t.Fatal("invalid special multiply partially emitted", kind, f.err)
		}
		f.swarSpecialMultiply(130)
		f.swarPopcnt()
		if len(f.a.B) != before {
			t.Fatal("sticky special arithmetic error appended")
		}
	}
}
