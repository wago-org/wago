//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var swarRelaxedCoreCases = []struct{ relaxed, core uint32 }{
	{256, 14}, {257, 248}, {258, 249}, {259, 252}, {260, 253},
	{265, 82}, {266, 82}, {267, 82}, {268, 82},
	{269, 232}, {270, 233}, {271, 244}, {272, 245}, {273, 130},
}

func swarRelaxedFixture(kind uint32) fixtureFunc {
	arity := 2
	if kind >= 257 && kind <= 260 {
		arity = 1
	} else if kind >= 261 && kind <= 268 || kind == 275 {
		arity = 3
	}
	fixture := fixtureFunc{params: bytes.Repeat([]byte{0x7b}, arity), result: 0x7b}
	for i := 0; i < arity; i++ {
		fixture.body = append(fixture.body, 0x20, byte(i))
	}
	fixture.body = append(uleb(append(fixture.body, 0xfd), kind), 0x0b)
	return fixture
}

func TestSWARRelaxedDeterministicCoreAdmission(t *testing.T) {
	for _, test := range swarRelaxedCoreCases {
		if core, ok := swarRelaxedCoreOpcode(test.relaxed); !ok || core != test.core {
			t.Fatal("relaxed deterministic mapping", test, core, ok)
		}
		fixture := swarRelaxedFixture(test.relaxed)
		fn, ft := swarValidatedFunction(t, fixture)
		code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true})
		if len(code) == 0 || err != nil {
			t.Fatal("relaxed lowering", test, err)
		}
		coreFixture := fixture
		coreFixture.body = append(uleb(append([]byte(nil), fixture.body[:2*len(fixture.params)+1]...), test.core), 0x0b)
		coreFn, coreFT := swarValidatedFunction(t, coreFixture)
		coreCode, _, err := compileSWARPairs(coreFn, coreFT, emissionMode{production: true})
		if err != nil || !bytes.Equal(code, coreCode) {
			t.Fatal("relaxed changed qualified core code", test, err)
		}
		usesFP := test.core >= 232
		isolated, _, err := compileSWARPairs(fn, ft, emissionMode{})
		if usesFP && (isolated != nil || err == nil) || !usesFP && (len(isolated) == 0 || err != nil) {
			t.Fatal("relaxed production FP boundary", test, err)
		}
		badType := *ft
		badType.Results = []wasm.ValType{wasm.I64}
		if code, _, err := compileSWARPairs(fn, &badType, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("relaxed result carrier", test, err)
		}
		for _, body := range [][]byte{fn.BodyBytes[2:], append([]byte{0x41, 0}, fn.BodyBytes[2:]...)} {
			bad := *fn
			bad.BodyBytes = body
			if code, _, err := compileSWARPairs(&bad, ft, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("relaxed underflow/class", test, err)
			}
		}
		if compiled, err := CompileProduction(binaryModule(fixture), ProductionOptions{DeferCodeMapping: true}); compiled != nil || err == nil {
			t.Fatal("public relaxed SIMD admission widened", test, err)
		}
	}
	for _, kind := range []uint32{0, 255, 261, 262, 263, 264, 274, 275, 276, ^uint32(0)} {
		if _, ok := swarRelaxedCoreOpcode(kind); ok {
			t.Fatal("pending relaxed mapping", kind)
		}
	}
}
