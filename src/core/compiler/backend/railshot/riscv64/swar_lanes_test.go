//go:build linux && !tinygo

package riscv64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func swarLaneFixture(kind uint32, lane byte) fixtureFunc {
	f := fixtureFunc{params: []byte{0x7b}, result: 0x7f, body: []byte{0x20, 0}}
	if kind >= 15 && kind <= 17 {
		f.params[0], f.result = 0x7f, 0x7b
	} else if kind == 23 || kind == 26 || kind == 28 {
		f.params = append(f.params, 0x7f)
		f.result = 0x7b
		f.body = append(f.body, 0x20, 1)
	}
	f.body = uleb(append(f.body, 0xfd), kind)
	if kind >= 21 {
		f.body = append(f.body, lane)
	}
	f.body = append(f.body, 0x0b)
	return f
}

func TestSWARScalarLanesWholeFunctionAdmission(t *testing.T) {
	for _, family := range []struct {
		kind  uint32
		lanes byte
	}{{15, 1}, {16, 1}, {17, 1}, {21, 16}, {22, 16}, {23, 16}, {24, 8}, {25, 8}, {26, 8}, {27, 4}, {28, 4}} {
		for lane := byte(0); lane < family.lanes; lane++ {
			fn, ft := swarValidatedFunction(t, swarLaneFixture(family.kind, lane))
			if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); len(code) == 0 || err != nil {
				t.Fatal("valid scalar lane", family.kind, lane, err)
			}
			if family.kind >= 21 {
				bad := *fn
				bad.BodyBytes = append([]byte(nil), fn.BodyBytes...)
				bad.BodyBytes[len(bad.BodyBytes)-2] = family.lanes
				if code, _, err := compileSWARPairs(&bad, ft, emissionMode{production: true}); code != nil || err == nil {
					t.Fatal("out-of-range lane emitted", family.kind, err)
				}
			}
		}
	}
	ft := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.V128}, Results: []wasm.ValType{wasm.I32}}
	for _, body := range [][]byte{
		{0xfd, 15, 0x0b},                                  // splat underflow
		{0x20, 0, 0xfd, 15, 0x0b},                         // vector in splat
		{0xfd, 21, 0, 0x0b},                               // extract underflow
		{0x41, 0, 0xfd, 21, 0, 0x0b},                      // scalar in extract
		{0x20, 0, 0xfd, 21},                               // missing lane immediate
		{0x20, 0, 0xfd, 23, 0, 0x0b},                      // replace underflow
		{0x20, 0, 0x20, 0, 0xfd, 23, 0, 0x0b},             // vector replacement value
		{0x41, 0, 0x41, 0, 0xfd, 23, 0, 0x0b},             // scalar vector input
		{0x20, 0, 0xfd, 29, 0, 0x0b},                      // I64 transport still pending
		{0x20, 0, 0xfd, 21, 0, 0xfd, 15, 0xfd, 110, 0x0b}, // unsupported later operation
	} {
		if code, _, err := compileSWARPairs(&wasm.Func{BodyBytes: body}, ft, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("malformed/unsupported scalar lane emitted", body, err)
		}
	}
}

func TestSWARScalarLaneHelpersRejectWithoutEmission(t *testing.T) {
	for _, invalid := range []func(*integerEmitter){
		func(f *integerEmitter) { f.swarSplat(0) },
		func(f *integerEmitter) { f.swarSplat(64) },
		func(f *integerEmitter) { f.swarExtract(8, 16, false) },
		func(f *integerEmitter) { f.swarReplace(16, 8) },
		func(f *integerEmitter) { f.swarReplace(32, 4) },
	} {
		f := integerEmitter{}
		f.a.Addi(enc.X5, enc.Zero, 1)
		size := len(f.a.B)
		invalid(&f)
		if f.err == nil || len(f.a.B) != size {
			t.Fatal("invalid helper partially emitted", f.err, len(f.a.B), size)
		}
		f.swarSplat(8)
		f.swarExtract(8, 0, true)
		f.swarReplace(8, 0)
		if len(f.a.B) != size {
			t.Fatal("sticky lane error appended code")
		}
	}
}
