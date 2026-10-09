//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestReferenceTailPrivateClassesAndFloors(t *testing.T) {
	unary := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.FuncRef}, Results: []wasm.ValType{wasm.FuncRef}}
	for _, tc := range []struct {
		name string
		body []byte
		ok   bool
	}{
		{"identity", []byte{0x20, 0, 0x12, 0, 0x0b}, true},
		{"discard-prefix", []byte{0x42, 7, 0x20, 0, 0x12, 0, 0x0b}, true},
		{"wrong-call", []byte{0x42, 0, 0x12, 0, 0x0b}, false},
		{"dead-missing", []byte{0, 0x12, 0, 0x0b}, true},
		{"dead-unknown", []byte{0, 0x1b, 0x12, 0, 0x0b}, true},
		{"dead-known", []byte{0, 0x42, 0, 0x12, 0, 0x0b}, false},
		{"inherited-dead", []byte{0, 2, 0x40, 0x12, 0, 0x0b, 0x0b}, false},
		{"cross-floor", []byte{0x20, 0, 2, 0x40, 0x12, 0, 0x0b, 0x0b}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := referenceFunctionModule([]*wasm.CompType{unary}, []uint32{0, 0}, [][]byte{{0x20, 0, 0x0b}, tc.body})
			err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256})
			var invalid *wasm.ValidationError
			if tc.ok && err != nil || !tc.ok && (!errors.As(err, &invalid) || invalid.Code != wasm.ErrTypeMismatch) {
				t.Fatal("frontend tail oracle", err)
			}
			for _, vector := range []bool{false, true} {
				for _, enabled := range []bool{false, true} {
					err := referenceFunctionWalkWithTails(m, 1, vector, true, enabled)
					if (err == nil) != (tc.ok && enabled) {
						t.Fatal("private reference tail class/floor/permission", vector, enabled, err)
					}
				}
			}
		})
	}
}

func TestReferenceTailPrivatePartialDeadArguments(t *testing.T) {
	ft := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32, wasm.FuncRef}}
	for _, tc := range []struct {
		name string
		body []byte
		ok   bool
	}{
		{"missing-prefix", []byte{0, 0xd0, 0x70, 0x12, 0, 0x0b}, true},
		{"unknown-prefix", []byte{0, 0x1b, 0xd0, 0x70, 0x12, 0, 0x0b}, true},
		{"bad-suffix", []byte{0, 0x1b, 0x42, 0, 0x12, 0, 0x0b}, false},
		{"bad-prefix", []byte{0, 0x42, 0, 0xd0, 0x70, 0x12, 0, 0x0b}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := referenceFunctionModule([]*wasm.CompType{ft, {Kind: wasm.CompFunc}}, []uint32{0, 1}, [][]byte{{0x0b}, tc.body})
			err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256})
			var invalid *wasm.ValidationError
			if tc.ok && err != nil || !tc.ok && (!errors.As(err, &invalid) || invalid.Code != wasm.ErrTypeMismatch) {
				t.Fatal("frontend partial tail oracle", err)
			}
			for _, vector := range []bool{false, true} {
				if err := referenceFunctionWalkWithTails(m, 1, vector, true, true); (err == nil) != tc.ok {
					t.Fatal("partial tail operands", vector, err)
				}
			}
		})
	}
}

func TestReferenceTailPrivateExactResults(t *testing.T) {
	numeric := &wasm.CompType{Kind: wasm.CompFunc}
	nn := wasm.RefVal(wasm.Ref(false, wasm.AbsHeap(wasm.HeapFunc), false))
	indexed := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{}), false))
	other := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 1}), false))
	for _, tc := range []struct {
		name               string
		input, output      wasm.ValType
		frontend, admitted bool
	}{
		{"nullable", wasm.FuncRef, wasm.FuncRef, true, true},
		{"nonnull", nn, nn, true, true},
		{"indexed", indexed, indexed, true, true},
		{"nonnull-subtype", nn, wasm.FuncRef, true, false},
		{"indexed-subtype", indexed, nn, true, false},
		{"different-index", indexed, other, true, false},
		{"wrong-class", wasm.I64, wasm.FuncRef, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			leaf := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{tc.input}, Results: []wasm.ValType{tc.input}}
			caller := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{tc.input}, Results: []wasm.ValType{tc.output}}
			m := referenceFunctionModule([]*wasm.CompType{numeric, numeric, leaf, caller}, []uint32{2, 3}, [][]byte{{0x20, 0, 0x0b}, {0x20, 0, 0x12, 0, 0x0b}})
			err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256})
			if (err == nil) != tc.frontend {
				t.Fatal("frontend exact/subtype oracle", err)
			}
			for _, vector := range []bool{false, true} {
				if err := referenceFunctionWalkWithTails(m, 1, vector, true, true); (err == nil) != tc.admitted {
					t.Fatal("private exact result profile", vector, err)
				}
			}
		})
	}
}

func TestReferenceTailPrivateBoundsAndBudget(t *testing.T) {
	for _, vector := range []bool{false, true} {
		limit := 256
		if vector {
			limit = 128
		}
		for _, n := range []int{limit, limit + 1} {
			params := make([]wasm.ValType, n)
			var get []byte
			for i := range params {
				params[i] = wasm.FuncRef
				if vector && i > 0 {
					params[i] = wasm.V128
				}
				get = append(get, 0x20)
				get = append(get, wasmtest.ULEB(uint32(i))...)
			}
			ft := &wasm.CompType{Kind: wasm.CompFunc, Params: params, Results: params}
			m := referenceFunctionModule([]*wasm.CompType{ft}, []uint32{0, 0}, [][]byte{append(append([]byte{}, get...), 0x0b), append(append([]byte{}, get...), 0x12, 0, 0x0b)})
			if err := referenceFunctionWalkWithTails(m, 1, vector, true, true); (err == nil) != (n == limit) {
				t.Fatal("reference tail arity", vector, n, err)
			}
			if n != limit {
				continue
			}
			cells := n
			if vector {
				cells = 2*n - 1
			}
			initial := n
			if vector {
				initial = 0
			}
			good := (integerBudget - initial - 2) / (2*cells + 1)
			for _, count := range []int{good, good + 1} {
				body := []byte{0}
				for range count {
					body = append(body, 0x12, 0)
				}
				body = append(body, 0x0b)
				m.Code[1].BodyBytes = body
				if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
					t.Fatal("tail budget frontend oracle", err)
				}
				if err := referenceFunctionWalkWithTails(m, 1, vector, true, true); (err == nil) != (count == good) {
					t.Fatal("tail transfer budget", vector, count, err)
				}
			}
		}
		ft := &wasm.CompType{Kind: wasm.CompFunc}
		for _, count := range []int{maxDirectCalls, maxDirectCalls + 1} {
			body := []byte{0}
			for range count {
				body = append(body, 0x12, 0)
			}
			body = append(body, 0x0b)
			m := referenceFunctionModule([]*wasm.CompType{ft}, []uint32{0}, [][]byte{body})
			if err := referenceFunctionWalkWithTails(m, 0, vector, true, true); (err == nil) != (count == maxDirectCalls) {
				t.Fatal("dead tail call count", vector, count, err)
			}
		}
	}
}

func TestReferenceTailPrivateOtherCallsAndMode(t *testing.T) {
	ft := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.FuncRef}, Results: []wasm.ValType{wasm.FuncRef}}
	for _, op := range []byte{0x11, 0x13, 0x14, 0x15} {
		for _, dead := range []bool{false, true} {
			for _, vector := range []bool{false, true} {
				t.Run(fmt.Sprintf("op%x-dead%t-vector%t", op, dead, vector), func(t *testing.T) {
					body := []byte{op, 0}
					if op == 0x11 || op == 0x13 {
						body = append(body, 0)
					}
					if dead {
						body = append([]byte{0}, body...)
					}
					body = append(body, 0x0b)
					m := referenceFunctionModule([]*wasm.CompType{ft}, []uint32{0}, [][]byte{body})
					if err := referenceFunctionWalkWithTails(m, 0, vector, true, true); !errors.Is(err, ErrUnsupported) {
						t.Fatal("nonlocal reference tail ABI admitted", err)
					}
				})
			}
		}
	}
	for _, dead := range []bool{false, true} {
		body := []byte{0x20, 0, 0x12, 0, 0x0b}
		if dead {
			body = append([]byte{0}, body...)
		}
		fn := &wasm.Func{BodyBytes: body}
		state := moduleState{production: true, importReferences: true, referenceCalls: true, referenceFunctions: true, localTailCalls: true, functions: []functionSpec{{signature: ft}}}
		summary, err := walkInteger(fn, ft, &state, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, mode := range []emissionMode{
			{production: true, interruptible: true, referenceFunctions: true},
			{production: true, referenceFunctions: true, localTailCalls: true},
			{interruptible: true, referenceFunctions: true, localTailCalls: true},
		} {
			if code, _, _, err := compileIntegerMode(fn, ft, &state, summary, mode); code != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("private scalar tail emission permission/frame", dead, err)
			}
		}
		state.localTailCalls = false
		fn.BodyBytes = []byte{0x20, 0, 0x0b}
		summary, err = walkInteger(fn, ft, &state, nil)
		if err != nil {
			t.Fatal(err)
		}
		if code, _, _, err := compileIntegerMode(fn, ft, &state, summary, emissionMode{production: true, interruptible: true, referenceFunctions: true, localTailCalls: true}); code != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("reverse tail permission mismatch", err)
		}
		for _, mode := range []emissionMode{{production: true, referenceFunctions: true, localTailCalls: true}, {interruptible: true, referenceFunctions: true, localTailCalls: true}} {
			if code, _, _, err := compileSWARPairsWithGlobals(fn, ft, nil, nil, []*wasm.CompType{ft}, nil, mode); code != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("SWAR tail frame gate", err)
			}
		}
		for _, imports := range []bool{false, true} {
			state.localTailCalls = true
			state.importTailCalls = imports
			state.dynamicTailCalls = !imports
			if _, err := walkInteger(fn, ft, &state, nil); imports && err != nil || !imports && !errors.Is(err, ErrUnsupported) {
				t.Fatal("private scalar direct/dynamic tail mode", err)
			}
			if _, err := walkSWARPairsWithReferenceFunctionMode(fn, ft, memoryProfile{}, nil, []*wasm.CompType{ft}, nil, 0, nil, nil, 0, 0, true, false, true, false, imports, !imports, true, true, nil); imports && err != nil || !imports && !errors.Is(err, ErrUnsupported) {
				t.Fatal("private SWAR direct/dynamic tail mode", err)
			}
		}
	}
}
