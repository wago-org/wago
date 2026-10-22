//go:build linux && !tinygo

package riscv64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

var swarWidenKinds = []uint32{124, 125, 126, 127, 135, 136, 137, 138, 167, 168, 169, 170, 199, 200, 201, 202}

func TestSWARWideningAdmissionAndClasses(t *testing.T) {
	for _, kind := range swarWidenKinds {
		fn, ft := swarValidatedFunction(t, swarUnaryFixture(kind, false))
		if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); len(code) == 0 || err != nil {
			t.Fatal("valid widen", kind, err)
		}
		badType := *ft
		badType.Results = []wasm.ValType{wasm.I64}
		if code, _, err := compileSWARPairs(fn, &badType, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("wrong widening result class", kind, err)
		}
		for _, prefix := range [][]byte{nil, {0x41, 0}, {0x42, 0}} {
			bad := *fn
			bad.BodyBytes = append(append([]byte(nil), prefix...), fn.BodyBytes[2:]...)
			if code, _, err := compileSWARPairs(&bad, ft, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("widen input class/underflow", kind, err)
			}
		}
	}
}

func TestSWARWidenOpcodeTable(t *testing.T) {
	for _, group := range []struct {
		base  uint32
		width uint8
	}{{135, 8}, {167, 16}, {199, 32}} {
		for offset := uint32(0); offset < 4; offset++ {
			width, signed, high, pairwise, ok := swarWidenOpcode(group.base + offset)
			if !ok || width != group.width || signed != (offset < 2) || high != (offset%2 == 1) || pairwise {
				t.Fatal(group, offset, width, signed, high, pairwise, ok)
			}
		}
	}
	for offset := uint32(0); offset < 4; offset++ {
		width, signed, high, pairwise, ok := swarWidenOpcode(124 + offset)
		if !ok || width != uint8(8<<(offset/2)) || signed != (offset%2 == 0) || high || !pairwise {
			t.Fatal(offset, width, signed, high, pairwise, ok)
		}
	}
	for _, kind := range []uint32{0, 123, 128, 134, 139, 166, 171, 198, 203, ^uint32(0)} {
		if _, _, _, _, ok := swarWidenOpcode(kind); ok {
			t.Fatal("widen neighbor admitted", kind)
		}
	}
}

func TestSWARWidenHelpersRejectBeforeBytes(t *testing.T) {
	for _, test := range []struct {
		width          uint8
		high, pairwise bool
	}{{0, false, false}, {7, false, false}, {24, false, false}, {64, false, false}, {255, false, false}, {32, false, true}, {8, true, true}, {16, true, true}} {
		for _, signed := range []bool{false, true} {
			f := integerEmitter{}
			f.a.Addi(enc.X6, enc.Zero, 1)
			before := len(f.a.B)
			f.swarWiden(test.width, signed, test.high, test.pairwise)
			if f.err == nil || len(f.a.B) != before {
				t.Fatal("invalid widen partially emitted", test, signed, f.err)
			}
			f.swarWiden(8, true, false, false)
			if len(f.a.B) != before {
				t.Fatal("sticky widening error appended")
			}
		}
	}
}
