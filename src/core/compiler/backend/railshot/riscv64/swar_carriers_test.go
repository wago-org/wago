//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func swarWideLaneFixture(kind uint32, lane byte) fixtureFunc {
	scalar := byte(0x7e)
	switch kind {
	case 19, 31, 32:
		scalar = 0x7d
	case 20, 33, 34:
		scalar = 0x7c
	}
	f := fixtureFunc{params: []byte{0x7b}, result: scalar, body: []byte{0x20, 0}}
	if kind <= 20 {
		f.params[0], f.result = scalar, 0x7b
	} else if kind == 30 || kind == 32 || kind == 34 {
		f.params = append(f.params, scalar)
		f.result = 0x7b
		f.body = append(f.body, 0x20, 1)
	}
	f.body = uleb(append(f.body, 0xfd), kind)
	if kind >= 29 {
		f.body = append(f.body, lane)
	}
	f.body = append(f.body, 0x0b)
	return f
}

func swarScalarConstantFixture(typ byte, value uint64) fixtureFunc {
	f := fixtureFunc{result: typ}
	switch typ {
	case 0x7e:
		f.body = sleb([]byte{0x42}, int64(value))
	case 0x7d:
		f.body = make([]byte, 5)
		f.body[0] = 0x43
		binary.LittleEndian.PutUint32(f.body[1:], uint32(value))
	case 0x7c:
		f.body = make([]byte, 9)
		f.body[0] = 0x44
		binary.LittleEndian.PutUint64(f.body[1:], value)
	default:
		panic("wide scalar constant fixture type")
	}
	f.body = append(f.body, 0x0b)
	return f
}

func TestSWARExplicitScalarCarrierAdmission(t *testing.T) {
	for _, typ := range []byte{0x7e, 0x7d, 0x7c} {
		for _, fixture := range []fixtureFunc{
			{params: []byte{typ}, result: typ, body: []byte{0x20, 0, 0x0b}},
			{result: typ, locals: []byte{1, 1, typ}, body: []byte{0x20, 0, 0x0b}},
			swarScalarConstantFixture(typ, 0x7ff0000080000001),
		} {
			fn, ft := swarValidatedFunction(t, fixture)
			if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); len(code) == 0 || err != nil {
				t.Fatal("explicit scalar transport", typ, err)
			}
		}
	}
	for _, kind := range []uint32{18, 19, 20, 29, 30, 31, 32, 33, 34} {
		lanes := byte(1)
		if kind >= 29 {
			lanes = 2
			if kind == 31 || kind == 32 {
				lanes = 4
			}
		}
		for lane := byte(0); lane < lanes; lane++ {
			fn, ft := swarValidatedFunction(t, swarWideLaneFixture(kind, lane))
			if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); len(code) == 0 || err != nil {
				t.Fatal("wide/FP scalar lane transport", kind, lane, err)
			}
			if kind >= 29 {
				bad := *fn
				bad.BodyBytes = append([]byte(nil), fn.BodyBytes...)
				bad.BodyBytes[len(bad.BodyBytes)-2] = lanes
				if code, _, err := compileSWARPairs(&bad, ft, emissionMode{production: true}); code != nil || err == nil {
					t.Fatal("wide/FP lane one-past bound emitted", kind, err)
				}
			}
		}
	}
}

func TestSWARExplicitCarrierClassesRejectSameWidthMismatches(t *testing.T) {
	classes := []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64, wasm.V128}
	types := []byte{0x7f, 0x7e, 0x7d, 0x7c, 0x7b}
	for index, input := range classes {
		for _, result := range classes {
			if wasm.EqualValType(input, result) {
				continue
			}
			ft := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{input}, Results: []wasm.ValType{result}}
			if code, _, err := compileSWARPairs(&wasm.Func{BodyBytes: []byte{0x20, 0, 0x0b}}, ft, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("same-width result type mismatch emitted", input, result, err)
			}
			fn, localType := swarValidatedFunction(t, fixtureFunc{params: []byte{types[index]}, result: types[index], locals: []byte{1, 1, types[index]}, body: []byte{0x20, 0, 0x21, 1, 0x20, 0, 0x0b}})
			fn.Locals.Runs[0].Type = result
			if code, _, err := compileSWARPairs(fn, localType, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("same-width local write mismatch emitted", input, result, err)
			}
		}
	}
	for _, condition := range []wasm.ValType{wasm.I64, wasm.F32, wasm.F64, wasm.V128} {
		ft := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32, wasm.I32, condition}, Results: []wasm.ValType{wasm.I32}}
		for _, selectOp := range [][]byte{{0x1b}, {0x1c, 1, 0x7f}} {
			body := append(append([]byte{0x20, 0, 0x20, 1, 0x20, 2}, selectOp...), 0x0b)
			if code, _, err := compileSWARPairs(&wasm.Func{BodyBytes: body}, ft, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("non-I32 select condition emitted", condition, err)
			}
		}
		ft.Params = []wasm.ValType{wasm.V128, condition}
		ft.Results = []wasm.ValType{wasm.V128}
		if code, _, err := compileSWARPairs(&wasm.Func{BodyBytes: swarShiftFixture(107).body}, ft, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("non-I32 shift count emitted", condition, err)
		}
	}
	fn, ft, memory := swarValidatedMemoryFunction(t, swarMemoryFixture(false, 0, 0))
	for _, address := range []wasm.ValType{wasm.I64, wasm.F32, wasm.F64, wasm.V128} {
		bad := *ft
		bad.Params = []wasm.ValType{address}
		if code, _, err := compileSWARPairsWithMemory(fn, &bad, memory, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("non-I32 memory address emitted", address, err)
		}
	}
	for _, body := range [][]byte{{0x42, 0x80}, {0x43, 0, 0, 0}, {0x44, 0, 0, 0, 0, 0, 0, 0}} {
		fn, ft := swarValidatedFunction(t, swarScalarConstantFixture(0x7c, 0))
		fn.BodyBytes = body
		if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("truncated scalar constant emitted", body, err)
		}
	}
}

func TestSWARSameWidthTypeBoundariesReject(t *testing.T) {
	for _, pair := range [][2]byte{{0x7f, 0x7d}, {0x7d, 0x7f}, {0x7e, 0x7c}, {0x7c, 0x7e}} {
		// Build a validated equal-type select, then change only one operand's
		// same-width type. Both select forms must preserve exact class identity.
		for _, selectOp := range [][]byte{{0x1b}, {0x1c, 1, pair[0]}} {
			body := append(append([]byte{0x20, 0, 0x20, 1, 0x20, 2}, selectOp...), 0x0b)
			fn, ft := swarValidatedFunction(t, fixtureFunc{params: []byte{pair[0], pair[0], 0x7f}, result: pair[0], body: body})
			_, otherType := swarValidatedFunction(t, fixtureFunc{params: []byte{pair[1]}, result: pair[1], body: []byte{0x20, 0, 0x0b}})
			ft.Params[1] = otherType.Params[0]
			if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("same-width select operand mismatch emitted", pair, selectOp, err)
			}
		}
	}
	for _, test := range []struct {
		kind  uint32
		wrong wasm.ValType
	}{{17, wasm.F32}, {19, wasm.I32}, {18, wasm.F64}, {20, wasm.I64},
		{27, wasm.F32}, {31, wasm.I32}, {29, wasm.F64}, {33, wasm.I64},
		{28, wasm.F32}, {32, wasm.I32}, {30, wasm.F64}, {34, wasm.I64}} {
		var fixture fixtureFunc
		if test.kind == 17 || test.kind == 27 || test.kind == 28 {
			fixture = swarLaneFixture(test.kind, 0)
		} else {
			fixture = swarWideLaneFixture(test.kind, 0)
		}
		fn, ft := swarValidatedFunction(t, fixture)
		if test.kind <= 20 {
			ft.Params[0] = test.wrong
		} else if test.kind == 28 || test.kind == 30 || test.kind == 32 || test.kind == 34 {
			ft.Params[1] = test.wrong
		} else {
			ft.Results[0] = test.wrong
		}
		if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("same-width lane type mismatch emitted", test.kind, test.wrong, err)
		}
	}
}
