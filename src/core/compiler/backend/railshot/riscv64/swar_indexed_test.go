//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type swarIndexedCase struct {
	function, signature fixtureFunc
	loop                bool
}

func swarIndexedCases() []swarIndexedCase {
	cases := []swarIndexedCase{
		{function: fixtureFunc{params: []byte{0x7b, 0x7f}, results: []byte{0x7f, 0x7b}, body: []byte{0x20, 0, 0x20, 1, 2, 0, 0x21, 1, 0x21, 0, 0x20, 1, 0x20, 0, 0x0b, 0x0b}}},
		{function: fixtureFunc{params: []byte{0x7b, 0x7f, 0x7f}, results: []byte{0x7f, 0x7b}, body: []byte{0x20, 0, 0x20, 1, 0x20, 2, 4, 1, 0x21, 1, 0x21, 0, 0x20, 1, 0x20, 0, 5, 0x21, 1, 0x21, 0, 0x20, 1, 0x41, 1, 0x6a, 0x20, 0, 0xfd, 77, 0x0b, 0x0b}},
			signature: fixtureFunc{params: []byte{0x7b, 0x7f}, results: []byte{0x7f, 0x7b}}},
		{function: fixtureFunc{params: []byte{0x7b, 0x7d, 0x7c, 0x7f}, results: []byte{0x7b, 0x7d, 0x7c}, body: []byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 4, 1, 1, 0x0b, 0x0b}},
			signature: fixtureFunc{params: []byte{0x7b, 0x7d, 0x7c}, results: []byte{0x7b, 0x7d, 0x7c}}},
		{function: fixtureFunc{params: []byte{0x7b, 0x7f}, result: 0x7b, body: []byte{0x20, 0, 0x20, 1, 3, 0, 0x21, 1, 0x20, 1, 0x45, 0x0d, 1, 0x20, 1, 0x41, 1, 0x6b, 0x22, 1, 0x0c, 0, 0x0b, 0x0b}}, loop: true},
		{function: fixtureFunc{params: []byte{0x7b, 0x7d, 0x7c, 0x7f}, results: []byte{0x7b, 0x7d, 0x7c}, body: []byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 3, 1, 0x21, 3, 0x20, 3, 0x45, 0x0d, 1, 0x20, 3, 0x41, 1, 0x6b, 0x22, 3, 0x0c, 0, 0x0b, 0x0b}},
			signature: fixtureFunc{params: []byte{0x7b, 0x7d, 0x7c, 0x7f}, results: []byte{0x7b, 0x7d, 0x7c}}, loop: true},
	}
	for i := range cases {
		cases[i].function.locals = append(uleb([]byte{1}, uint32(128-len(cases[i].function.params))), 0x7b)
	}
	return cases
}

func swarValidatedIndexedFunction(t *testing.T, c swarIndexedCase) (*wasm.Func, *wasm.CompType, []*wasm.CompType) {
	t.Helper()
	functions := []fixtureFunc{c.function}
	if len(c.signature.params) != 0 || len(c.signature.results) != 0 {
		c.signature.body, c.signature.unexported = []byte{0, 0x0b}, true
		functions = append(functions, c.signature)
	}
	m, err := wasm.DecodeModule(binaryModule(functions...))
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
		t.Fatal(err)
	}
	types := make([]*wasm.CompType, len(functions))
	for i := range types {
		types[i] = new(wasm.CompType)
		if !m.ResolveTypeFunc(uint32(i), types[i]) {
			t.Fatal("indexed fixture type", i)
		}
	}
	ft, ok := m.LocalFuncType(0)
	if !ok {
		t.Fatal("indexed fixture function type")
	}
	return &m.Code[0], ft, types
}

func TestSWARIndexedClassesLoopBridgesAndDirectoryBounds(t *testing.T) {
	for index, c := range swarIndexedCases() {
		fn, ft, types := swarValidatedIndexedFunction(t, c)
		for _, mode := range []emissionMode{{}, {production: true}, {production: true, interruptible: true}} {
			code, info, err := compileSWARPairsWithTypes(fn, ft, nil, types, mode)
			if c.loop && (!mode.production || !mode.interruptible) {
				if code != nil || err == nil {
					t.Fatal("unpolled/isolated loop admitted", index, mode, err)
				}
				continue
			}
			if len(code) == 0 || err != nil || info.FrameBytes <= 2048 {
				t.Fatal("indexed control lowered", index, mode, info, err)
			}
		}
		bad := *types[0]
		bad.Params = append([]wasm.ValType(nil), bad.Params...)
		bad.Params[0] = wasm.F64
		altered := append([]*wasm.CompType(nil), types...)
		if len(types) == 1 {
			altered[0] = &bad
		} else {
			bad = *types[1]
			bad.Params = append([]wasm.ValType(nil), bad.Params...)
			bad.Params[0] = wasm.F64
			altered[1] = &bad
		}
		if code, _, err := compileSWARPairsWithTypes(fn, ft, nil, altered, emissionMode{production: true, interruptible: true}); code != nil || err == nil {
			t.Fatal("indexed operand class emitted", index, err)
		}
	}
	fn, ft := swarValidatedFunction(t, fixtureFunc{params: []byte{0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x0b}})
	for _, count := range []int{1024, 1025} {
		types := make([]*wasm.CompType, count)
		for i := range types {
			types[i] = ft
		}
		code, _, err := compileSWARPairsWithTypes(fn, ft, nil, types, emissionMode{production: true})
		if count == 1024 && (len(code) == 0 || err != nil) || count == 1025 && (code != nil || err == nil) {
			t.Fatal("type directory bound", count, err)
		}
	}
	for _, bad := range []*wasm.CompType{nil, {Kind: wasm.CompStruct}, {Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.FuncRef}}, {Kind: wasm.CompFunc, Params: make([]wasm.ValType, 129)}} {
		if code, _, err := compileSWARPairsWithTypes(fn, ft, nil, []*wasm.CompType{bad}, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("invalid control directory emitted", bad, err)
		}
	}
	// A valid large indexed signature still respects the aggregate operand bound.
	large := fixtureFunc{params: bytes.Repeat([]byte{0x7b}, 128), results: bytes.Repeat([]byte{0x7b}, 128)}
	for i := 0; i < 128; i++ {
		large.body = append(large.body, 0x20, byte(i))
	}
	large.body = append(large.body, 2, 0, 0x0b, 0x0b)
	fn, ft, types := swarValidatedIndexedFunction(t, swarIndexedCase{function: large})
	if code, info, err := compileSWARPairsWithTypes(fn, ft, nil, types, emissionMode{production: true}); len(code) == 0 || err != nil || info.Params != 256 || info.Results != 256 {
		t.Fatal("128 vector indexed parameters/results", info, err)
	}
}
