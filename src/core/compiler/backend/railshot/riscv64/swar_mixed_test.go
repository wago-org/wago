//go:build linux && !tinygo

package riscv64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestSWARMixedCarrierAdmissionAndClasses(t *testing.T) {
	for _, fixture := range []fixtureFunc{
		{params: []byte{0x7f, 0x7b, 0x7f}, results: []byte{0x7f, 0x7b, 0x7f}, body: []byte{0x20, 0, 0x20, 1, 0x20, 2, 0x0b}},
		{params: []byte{0x7b}, result: 0x7f, body: []byte{0x20, 0, 0xfd, 83, 0x0b}},
		{params: []byte{0x7b, 0x7b, 0x7f}, result: 0x7b, body: []byte{0x20, 0, 0x20, 1, 0x20, 2, 0x1c, 1, 0x7b, 0x0b}},
		{result: 0x7f, body: []byte{0x41, 0x7f, 0x0b}},
	} {
		fn, ft := swarValidatedFunction(t, fixture)
		code, info, err := compileSWARPairs(fn, ft, emissionMode{production: true})
		if err != nil || len(code) == 0 || info.Params != swarSignatureSlots(ft.Params) || info.Results != swarSignatureSlots(ft.Results) {
			t.Fatal("mixed physical slots", info, err)
		}
	}
	for _, body := range [][]byte{
		{0x20, 0, 0x0b},                                                 // vector where scalar result is required
		{0x41, 0, 0x21, 0, 0x41, 0, 0x0b},                               // scalar written to vector local
		{0x41, 0, 0xfd, 83, 0x0b},                                       // scalar in any_true
		{0x41, 0, 0x41, 1, 0xfd, 78, 0x0b},                              // scalars in pair arithmetic
		{0x20, 0, 0x20, 0, 0x20, 0, 0x1c, 1, 0x7b, 0x1a, 0x41, 0, 0x0b}, // vector condition
		{0x20, 0, 0x20, 0, 0x41, 1, 0x1b, 0x1a, 0x41, 0, 0x0b},          // untyped vector select
		{0x41, 0, 0x41, 1, 0x41, 1, 0x1c, 2, 0x7f, 0x7f, 0x0b},
	} {
		ft := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.V128}, Results: []wasm.ValType{wasm.I32}}
		if code, _, err := compileSWARPairs(&wasm.Func{BodyBytes: body}, ft, emissionMode{production: true}); err == nil || code != nil {
			t.Fatal("wrong carrier class emitted", body, err)
		}
	}
}
