//go:build linux && !tinygo

package riscv64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func swarMemoryFixture(store bool, align byte, offset uint32) fixtureFunc {
	f := fixtureFunc{params: []byte{0x7f}, result: 0x7b, body: []byte{0x20, 0}}
	kind := byte(0)
	if store {
		f.params = append(f.params, 0x7b)
		f.body = append(f.body, 0x20, 1)
		kind = 11
	}
	f.body = append(f.body, 0xfd, kind, align)
	f.body = uleb(f.body, offset)
	if store {
		f.body = append(f.body, 0x20, 1)
	}
	f.body = append(f.body, 0x0b)
	return f
}

func swarValidatedMemoryFunction(t *testing.T, fixture fixtureFunc) (*wasm.Func, *wasm.CompType, *wasm.MemType) {
	t.Helper()
	m, err := wasm.DecodeModule(stateModule([]fixtureFunc{fixture}, false, 1, 1, true, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
		t.Fatal(err)
	}
	ft, ok := m.LocalFuncType(0)
	if !ok || len(m.Memories) != 1 {
		t.Fatal("vector memory fixture type/resource")
	}
	return &m.Code[0], ft, &m.Memories[0]
}

func TestSWARWholeFunctionMemoryAdmissionAndResources(t *testing.T) {
	mode := emissionMode{production: true}
	for _, store := range []bool{false, true} {
		for align := byte(0); align <= 4; align++ {
			fixture := swarMemoryFixture(store, align, 0xffffffff)
			fn, ft, memory := swarValidatedMemoryFunction(t, fixture)
			code, _, err := compileSWARPairsWithMemory(fn, ft, memory, mode)
			if err != nil || len(code) == 0 {
				t.Fatal("valid implicit memory0 memarg", store, align, err)
			}
			if code, _, err := compileSWARPairs(fn, ft, mode); code != nil || err == nil {
				t.Fatal("memory without resource emitted", code, err)
			}
			// Private lowering does not widen whole-module SIMD policy.
			if compiled, err := CompileProduction(stateModule([]fixtureFunc{fixture}, false, 1, 1, true, nil, nil), ProductionOptions{DeferCodeMapping: true}); compiled != nil || err == nil {
				t.Fatal("public SIMD policy widened", compiled, err)
			}
		}
	}
	fn, ft, memory := swarValidatedMemoryFunction(t, swarMemoryFixture(false, 0, 0))
	for _, bad := range []wasm.MemType{
		{Shared: true, Limits: wasm.Limits{Min: 1, Max: 1, HasMax: true}},
		{Limits: wasm.Limits{Addr64: true, Min: 1}},
		{Limits: wasm.Limits{Min: 65537}},
		{Limits: wasm.Limits{Min: 2, Max: 1, HasMax: true}},
		{Limits: wasm.Limits{Min: 1, Max: 65537, HasMax: true}},
	} {
		if code, _, err := compileSWARPairsWithMemory(fn, ft, &bad, mode); code != nil || err == nil {
			t.Fatal("unsupported resource emitted", bad, err)
		}
	}
	if code, _, err := compileSWARPairsWithMemory(fn, ft, memory, emissionMode{}); code != nil || err == nil {
		t.Fatal("memory requires production home", err)
	}
	for _, body := range [][]byte{
		{0xfd, 0, 0, 0, 0x0b},                                     // address underflow
		{0x20, 0, 0xfd, 11, 0, 0, 0x0b},                           // store value underflow
		{0x20, 0, 0x20, 0, 0xfd, 11, 0, 0, 0x0b},                  // i32 store value
		{0x20, 0, 0xfd, 0, 0, 0, 0xfd, 0, 0, 0, 0x0b},             // vector address
		{0x20, 0, 0xfd, 0, 5, 0, 0x0b},                            // invalid alignment
		{0x20, 0, 0xfd, 0, 64, 0, 0, 0x0b},                        // explicit memory index
		{0x20, 0, 0xfd, 0, 0, 0x80, 0x80, 0x80, 0x80, 0x10, 0x0b}, // memory32 offset overflow
		{0x20, 0, 0xfd, 0, 0},                                     // truncated offset
		{0x20, 0, 0xfd, 0, 0, 0x80},                               // continued offset truncated at EOF
		{0x20, 0, 0xfd, 0, 0, 0},                                  // complete memarg, missing function end
		{0x20, 0, 0xfd, 87, 0, 0, 2, 0x0b},                        // lane load missing vector/invalid lane
		{0x20, 0, 0xfd, 0, 0, 0, 0xfd, 103, 0x0b},                 // unsupported op after load
	} {
		bad := *fn
		bad.BodyBytes = body
		if code, _, err := compileSWARPairsWithMemory(&bad, ft, memory, mode); code != nil || err == nil {
			t.Fatal("invalid/unsupported memory body emitted", body, err)
		}
	}
}
