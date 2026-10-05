//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func swarValidatedFunction(t *testing.T, fixture fixtureFunc) (*wasm.Func, *wasm.CompType) {
	t.Helper()
	m, err := wasm.DecodeModule(binaryModule(fixture))
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
		t.Fatal(err)
	}
	ft, ok := m.LocalFuncType(0)
	if !ok {
		t.Fatal("vector fixture type")
	}
	return &m.Code[0], ft
}

func swarLiteral(low, high uint64) []byte {
	body := make([]byte, 18)
	body[0], body[1] = 0xfd, 12
	binary.LittleEndian.PutUint64(body[2:], low)
	binary.LittleEndian.PutUint64(body[10:], high)
	return body
}

func TestSWARWholeFunctionAdmissionAndBoundedSlots(t *testing.T) {
	for _, fixture := range []fixtureFunc{
		{params: []byte{0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x0b}},
		{result: 0x7b, locals: []byte{1, 1, 0x7b}, body: []byte{0x20, 0, 0x0b}},
		{result: 0x7b, body: append(swarLiteral(0x8000000000000000, 0x7ff0000000000001), 0x0b)},
	} {
		fn, ft := swarValidatedFunction(t, fixture)
		code, info, err := compileSWARPairs(fn, ft, emissionMode{production: true})
		if err != nil || len(code) == 0 || info.FrameBytes&15 != 0 || info.Params != len(ft.Params)*2 || info.Results != len(ft.Results)*2 || info.FrameBytes < 16 || !info.Fallback {
			t.Fatal("pair function", info, err)
		}
	}
	// Every unsupported operation is rejected on the complete source before a
	// body image is returned, including dead code after an unsupported return.
	for _, fixture := range []fixtureFunc{
		{params: []byte{0x70}, result: 0x70, body: []byte{0x20, 0, 0x0b}},
		{params: []byte{0x7b}, result: 0x7b, body: []byte{2, 0x7b, 0x20, 0, 0x0b, 0x0b}},
		{params: []byte{0x7b, 0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x20, 1, 0xfd, 101, 0x0b}},
		{params: []byte{0x7b}, result: 0x7b, locals: []byte{1, 1, 0x70}, body: []byte{0x20, 0, 0x0b}},
		{params: []byte{0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x0f, 0x20, 0, 0x20, 0, 0xfd, 101, 0x1a, 0x0b}},
		{params: []byte{0x7b}, result: 0x7b, locals: []byte{1, 128, 1, 0x7b}, body: []byte{0x20, 0, 0x0b}},
	} {
		fn, ft := swarValidatedFunction(t, fixture)
		if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("unsupported vector body emitted", code, err)
		}
	}
	for _, body := range [][]byte{{0x0b}, {0x20, 1, 0x0b}, {0x20, 0, 0x0b, 0x01}, {0xfd, 12, 0}, {0x20, 0}, {0xfd, 77, 0x0b}} {
		fn := &wasm.Func{BodyBytes: body}
		ft := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.V128}, Results: []wasm.ValType{wasm.V128}}
		if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("malformed pilot body emitted", body, err)
		}
	}
	body := bytes.Repeat([]byte{0x20, 0}, 257)
	body = append(body, bytes.Repeat([]byte{0x1a}, 256)...)
	body = append(body, 0x0b)
	fn, ft := swarValidatedFunction(t, fixtureFunc{params: []byte{0x7b}, result: 0x7b, body: body})
	if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); code != nil || err == nil {
		t.Fatal("vector operand bound", err)
	}
}

func TestSWARWholeFunctionArityAndInstructionBudget(t *testing.T) {
	for _, count := range []int{128, 129} {
		for _, params := range []bool{false, true} {
			fixture := fixtureFunc{params: []byte{0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x0b}}
			if params {
				fixture.params = bytes.Repeat([]byte{0x7b}, count)
			} else {
				fixture.result = 0
				fixture.results = bytes.Repeat([]byte{0x7b}, count)
				fixture.body = append(bytes.Repeat([]byte{0x20, 0}, count), 0x0b)
			}
			fn, ft := swarValidatedFunction(t, fixture)
			code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true})
			if count == 128 {
				if err != nil || len(code) == 0 {
					t.Fatal("maximum pair arity", params, err)
				}
			} else if err == nil || code != nil {
				t.Fatal("excess pair arity emitted", params, err)
			}
		}
	}
	for _, ops := range []int{integerBudget, integerBudget + 1} {
		body := append(bytes.Repeat([]byte{0x01}, ops-2), 0x20, 0, 0x0b)
		fn, ft := swarValidatedFunction(t, fixtureFunc{params: []byte{0x7b}, result: 0x7b, body: body})
		code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true})
		if ops == integerBudget {
			if err != nil || len(code) == 0 {
				t.Fatal("pair instruction budget edge", err)
			}
		} else if err == nil || code != nil {
			t.Fatal("pair instruction overflow emitted", err)
		}
	}
}
