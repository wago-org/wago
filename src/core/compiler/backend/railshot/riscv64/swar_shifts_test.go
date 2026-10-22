//go:build linux && !tinygo

package riscv64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func swarShiftFixture(kind uint32) fixtureFunc {
	body := uleb([]byte{0x20, 0, 0x20, 1, 0xfd}, kind)
	return fixtureFunc{params: []byte{0x7b, 0x7f}, result: 0x7b, body: append(body, 0x0b)}
}

func TestSWARVectorShiftAdmission(t *testing.T) {
	for _, kind := range []uint32{107, 108, 109, 139, 140, 141, 171, 172, 173, 203, 204, 205} {
		fn, ft := swarValidatedFunction(t, swarShiftFixture(kind))
		if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); len(code) == 0 || err != nil {
			t.Fatal("valid vector shift", kind, err)
		}
		for _, body := range [][]byte{
			uleb([]byte{0xfd}, kind),
			append(uleb([]byte{0x20, 0, 0xfd}, kind), 0x0b),
			append(uleb([]byte{0x20, 0, 0x20, 0, 0xfd}, kind), 0x0b), // vector count
			append(uleb([]byte{0x20, 1, 0x20, 1, 0xfd}, kind), 0x0b), // scalar value
		} {
			bad := *fn
			bad.BodyBytes = body
			if code, _, err := compileSWARPairs(&bad, ft, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("invalid shift carriers emitted", kind, body, err)
			}
		}
	}
	fixture := swarShiftFixture(107)
	ft := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.V128, wasm.I32}, Results: []wasm.ValType{wasm.I32}}
	if code, _, err := compileSWARPairs(&wasm.Func{BodyBytes: fixture.body}, ft, emissionMode{production: true}); code != nil || err == nil {
		t.Fatal("shift serialized as scalar", err)
	}
}

func TestSWARVectorShiftHelpersRejectBeforeBytes(t *testing.T) {
	for _, test := range [][2]uint8{{0, 0}, {24, 0}, {65, 0}, {255, 0}, {8, 3}, {64, 255}} {
		f := integerEmitter{}
		f.a.Addi(enc.X5, enc.Zero, 1)
		before := len(f.a.B)
		f.swarShift(test[0], test[1])
		if f.err == nil || len(f.a.B) != before {
			t.Fatal("invalid shift partially emitted", test, f.err)
		}
		f.swarShift(8, 0)
		if len(f.a.B) != before {
			t.Fatal("sticky shift error appended bytes")
		}
	}
}
