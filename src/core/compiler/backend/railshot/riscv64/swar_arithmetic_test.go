//go:build linux && !tinygo

package riscv64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func swarArithmeticFixture(kind uint32) fixtureFunc {
	body := uleb([]byte{0x20, 0, 0x20, 1, 0xfd}, kind)
	return fixtureFunc{params: []byte{0x7b, 0x7b}, result: 0x7b, body: append(body, 0x0b)}
}

func TestSWARWrappingArithmeticAdmission(t *testing.T) {
	for _, kind := range []uint32{110, 113, 142, 145, 174, 177, 206, 209} {
		fn, ft := swarValidatedFunction(t, swarArithmeticFixture(kind))
		if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); len(code) == 0 || err != nil {
			t.Fatal("wrapping lane arithmetic", kind, err)
		}
		for _, body := range [][]byte{
			uleb([]byte{0xfd}, kind),                                 // zero inputs
			append(uleb([]byte{0x20, 0, 0xfd}, kind), 0x0b),          // one input
			append(uleb([]byte{0x20, 0, 0x41, 1, 0xfd}, kind), 0x0b), // scalar second input
			append(uleb([]byte{0x41, 1, 0x20, 0, 0xfd}, kind), 0x0b), // scalar first input
		} {
			bad := *fn
			bad.BodyBytes = body
			if code, _, err := compileSWARPairs(&bad, ft, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("arithmetic carrier mismatch/underflow emitted", kind, body, err)
			}
		}
	}
	// Valid adjacent saturation/multiply families still need actual lowering.
	for _, kind := range []uint32{111, 112, 114, 115, 143, 144, 146, 147, 149, 181, 213} {
		fn, ft := swarValidatedFunction(t, swarArithmeticFixture(kind))
		if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("pending arithmetic emitted", kind, err)
		}
	}
	ft := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.V128, wasm.V128}, Results: []wasm.ValType{wasm.I32}}
	if code, _, err := compileSWARPairs(&wasm.Func{BodyBytes: swarArithmeticFixture(110).body}, ft, emissionMode{production: true}); code != nil || err == nil {
		t.Fatal("vector arithmetic result serialized as scalar", err)
	}
}

func TestSWARWrappingArithmeticHelpersRejectBeforeBytes(t *testing.T) {
	for _, width := range []uint8{0, 1, 7, 24, 65, 255} {
		for _, subtract := range []bool{false, true} {
			f := integerEmitter{}
			f.a.Addi(enc.X5, enc.Zero, 1)
			before := len(f.a.B)
			f.swarAddSub(width, subtract)
			if f.err == nil || len(f.a.B) != before {
				t.Fatal("invalid arithmetic width partially emitted", width, f.err)
			}
			f.swarAddSub(8, false)
			if len(f.a.B) != before {
				t.Fatal("sticky arithmetic error appended bytes")
			}
		}
	}
}
