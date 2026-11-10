// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
)

func TestInlineIntegerPairInLoop(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, add, xor, lt := wasm.I32, byte(0x7f), byte(0x41), byte(0x6a), byte(0x73), byte(0x49)
		fold := func(v uint64) uint64 { return uint64(uint32(v)) }
		if wide {
			typ, bt, k, add, xor, lt = wasm.I64, 0x7e, 0x42, 0x7c, 0x85, 0x54
			fold = func(v uint64) uint64 { return v }
		}
		// Keep the accumulator below a scalar block containing a tuple call.
		// Rebind both private parameters on every loop iteration.
		body := []byte{1, 4, bt, 0x03, 0x40, 0x20, 3, 0x02, bt,
			0x20, 0, 0x20, 1, 0x10, 1, 0x21, 5, 0x21, 4,
			0x20, 4, 0x20, 5, add, 0x0b, add, 0x21, 3,
			0x20, 0, k, 1, add, 0x21, 0, 0x20, 1, k, 3, add, 0x21, 1,
			0x20, 2, k, 1, add, 0x22, 2, k, 7, lt, 0x0d, 0,
			0x0b, 0x20, 3, 0x0b}
		helper := []byte{0, 0x20, 0, 0x20, 1, add, 0x20, 0, 0x20, 1, xor, 0x0b}
		m := modFuncs(t, funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ}, body}, funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ, typ}, helper})
		for _, enabled := range []bool{false, true} {
			for _, workers := range []int{1, 4} {
				for _, input := range [][2]uint64{{3, 7}, {0xfffffff0, 0x80000001}, {^uint64(0), 0x8000000000000001}} {
					x, y, want := fold(input[0]), fold(input[1]), uint64(0)
					for range 7 {
						want = fold(want + fold(x+y) + (x ^ y))
						x, y = fold(x+1), fold(y+3)
					}
					a, b := input[0], input[1]
					if !wide {
						a |= 0xdeadbeef00000000
						b |= 0xcafebabe00000000
					}
					var stats ModuleStats
					opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"inline": enabled, "value-facts": false}}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					got, err := runArm64WrapperWithOptions(t, m, opts, a, b)
					if err != nil || got != want {
						t.Fatalf("wide=%t inline=%t workers=%d input=%x got=%x err=%v want=%x", wide, enabled, workers, input, got, err, want)
					}
					if diagnosticsEnabled && (stats.Funcs[0].Calls[callKindInline] != 0) != enabled {
						t.Fatalf("inline=%t calls=%v", enabled, stats.Funcs[0].Calls)
					}
					if diagnosticsEnabled && (stats.Funcs[0].Peephole["straight-line-block"] != 0) != enabled {
						t.Fatalf("inline=%t retained block=%d", enabled, stats.Funcs[0].Peephole["straight-line-block"])
					}
					if diagnosticsEnabled && (stats.Funcs[0].Peephole["inline-integer-pair-param-forward"] != 0) != enabled {
						t.Fatalf("inline=%t forwarded parameters=%d", enabled, stats.Funcs[0].Peephole["inline-integer-pair-param-forward"])
					}
				}
			}
		}
	}
}

func TestInlineIntegerPairDroppedMemberStillTraps(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, div := wasm.I32, byte(0x6e)
		if wide {
			typ, div = wasm.I64, 0x80
		}
		// The caller drops the only trapping result. Deferring the pure first
		// result must not discard division or move it beyond the inline boundary.
		m := modFuncs(t,
			funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ}, []byte{0, 0x20, 0, 0x20, 1, 0x10, 1, 0x1a, 0x0b}},
			funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ, typ}, []byte{0, 0x20, 0, 0x20, 0, 0x20, 1, div, 0x0b}})
		for _, enabled := range []bool{false, true} {
			for _, workers := range []int{1, 4} {
				var stats ModuleStats
				opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"inline": enabled, "value-facts": false}}
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				got, err := runArm64WrapperWithOptions(t, m, opts, 17, 5)
				if err != nil || got != 17 {
					t.Fatalf("wide=%t inline=%t workers=%d got=%d err=%v", wide, enabled, workers, got, err)
				}
				if diagnosticsEnabled && (stats.Funcs[0].Calls[callKindInline] != 0) != enabled {
					t.Fatalf("inline=%t calls=%v", enabled, stats.Funcs[0].Calls)
				}
				_, err = runArm64WrapperWithOptions(t, m, opts, 17, 0)
				var trap *rt.TrapError
				if !errors.As(err, &trap) || trap.Code != rt.TrapDivZero {
					t.Fatalf("wide=%t inline=%t workers=%d err=%v want division-by-zero", wide, enabled, workers, err)
				}
			}
		}
	}
}

func TestInlineIntegerPairResultsSurviveActualCall(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, k, add, xor := wasm.I32, byte(0x41), byte(0x6a), byte(0x73)
		fold := func(v uint64) uint64 { return uint64(uint32(v)) }
		if wide {
			typ, k, add, xor = wasm.I64, 0x42, 0x7c, 0x85
			fold = func(v uint64) uint64 { return v }
		}
		large := []byte{0}
		for range 256 {
			large = append(large, 0x01)
		}
		large = append(large, k, 7, 0x0b)
		m := modFuncs(t,
			funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ}, []byte{0, 0x20, 0, 0x20, 1, 0x10, 1, 0x10, 2, 0x1a, add, 0x0b}},
			funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ, typ}, []byte{0, 0x20, 0, 0x20, 1, add, 0x20, 0, 0x20, 1, xor, 0x0b}},
			funcDef{nil, []wasm.ValType{typ}, large})
		for _, enabled := range []bool{false, true} {
			for _, regABI := range []bool{false, true} {
				for _, workers := range []int{1, 4} {
					for _, input := range [][2]uint64{{3, 7}, {0xfffffff0, 0x80000001}, {^uint64(0), 0x8000000000000001}} {
						var stats ModuleStats
						opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"inline": enabled, "reg-abi": regABI, "value-facts": false}}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						x, y := fold(input[0]), fold(input[1])
						want := fold(fold(x+y) + (x ^ y))
						got, err := runArm64WrapperWithOptions(t, m, opts, input[0], input[1])
						if err != nil || got != want {
							t.Fatalf("wide=%t inline=%t regABI=%t workers=%d input=%x got=%x err=%v want=%x", wide, enabled, regABI, workers, input, got, err, want)
						}
						if diagnosticsEnabled && (stats.Funcs[0].Calls[callKindInline] != 0) != enabled {
							t.Fatalf("inline=%t calls=%v", enabled, stats.Funcs[0].Calls)
						}
					}
				}
			}
		}
	}
}

func TestInlineIntegerPairForwardingPreservesGetTimeValues(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, k, add := wasm.I32, byte(0x41), byte(0x6a)
		fold := func(v uint64) uint64 { return uint64(uint32(v)) }
		if wide {
			typ, k, add = wasm.I64, 0x42, 0x7c
			fold = func(v uint64) uint64 { return v }
		}
		for _, mutateBefore := range []bool{false, true} {
			body := []byte{0, 0x20, 0}
			if mutateBefore {
				body = append(body, k, 9, 0x21, 0)
			}
			body = append(body, 0x20, 1, 0x10, 1, k, 13, 0x21, 0, k, 17, 0x21, 1, add, 0x0b)
			m := modFuncs(t,
				funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ}, body},
				funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ, typ}, []byte{0, 0x20, 0, 0x20, 0, 0x20, 1, add, 0x0b}})
			for _, enabled := range []bool{false, true} {
				for _, workers := range []int{1, 4} {
					for _, input := range [][2]uint64{{3, 7}, {0xfffffff0, 0x80000001}, {^uint64(0), 0x8000000000000001}} {
						var stats ModuleStats
						opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"inline": enabled, "value-facts": false}}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						want := fold(2*fold(input[0]) + fold(input[1]))
						got, err := runArm64WrapperWithOptions(t, m, opts, input[0], input[1])
						if err != nil || got != want {
							t.Fatalf("wide=%t mutateBefore=%t inline=%t workers=%d input=%x got=%x err=%v want=%x", wide, mutateBefore, enabled, workers, input, got, err, want)
						}
						if diagnosticsEnabled && (stats.Funcs[0].Peephole["inline-integer-pair-param-forward"] != 0) != (enabled && !mutateBefore) {
							t.Fatal("incorrect forwarding selection for an argument changed before the call")
						}
					}
				}
			}
		}
	}
}

func TestInlineIntegerPairRebindingPreservesEarlierResults(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, add, mul, xor := wasm.I32, byte(0x7f), byte(0x41), byte(0x6a), byte(0x6c), byte(0x73)
		fold := func(v uint64) uint64 { return uint64(uint32(v)) }
		if wide {
			typ, bt, k, add, mul, xor = wasm.I64, 0x7e, 0x42, 0x7c, 0x7e, 0x85
			fold = func(v uint64) uint64 { return v }
		}
		for _, mutate := range []bool{false, true} {
			helper := []byte{0, 0x20, 0, 0x20, 1, k, 3, mul, add}
			if mutate {
				helper = append(helper, 0x22, 0)
			}
			helper = append(helper, 0x20, 0, 0x20, 1, xor, 0x0b)
			body := []byte{1, 4, bt, 0x20, 0, 0x20, 1, 0x10, 1, 0x10, 1,
				0x20, 1, 0x20, 0, 0x10, 1, 0x21, 5, 0x21, 4, 0x21, 3, 0x21, 2}
			for i, weight := range []byte{3, 5, 7, 11} {
				body = append(body, 0x20, byte(i+2), k, weight, mul)
				if i > 0 {
					body = append(body, add)
				}
			}
			body = append(body, 0x0b)
			m := modFuncs(t, funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ}, body}, funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ, typ}, helper})
			pair := func(x, y uint64) (uint64, uint64) {
				a := fold(x + fold(3*y))
				if mutate {
					x = a
				}
				return a, x ^ y
			}
			for _, enabled := range []bool{false, true} {
				for _, workers := range []int{1, 4} {
					for _, input := range [][2]uint64{{3, 7}, {0xfffffff0, 0x80000001}, {^uint64(0), 0x8000000000000001}} {
						x, y := fold(input[0]), fold(input[1])
						a, b := pair(x, y)
						a, b = pair(a, b)
						c, d := pair(y, x)
						want := fold(3*a + 5*b + 7*c + 11*d)
						var stats ModuleStats
						opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"inline": enabled, "value-facts": false}}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						got, err := runArm64WrapperWithOptions(t, m, opts, input[0], input[1])
						if err != nil || got != want {
							t.Fatalf("wide=%t mutate=%t inline=%t workers=%d input=%x got=%x err=%v want=%x", wide, mutate, enabled, workers, input, got, err, want)
						}
						if diagnosticsEnabled && enabled && stats.Funcs[0].Calls[callKindInline] != 3 {
							t.Fatalf("did not exercise three splices: %v", stats.Funcs[0].Calls)
						}
						if diagnosticsEnabled && mutate && stats.Funcs[0].Peephole["inline-integer-pair-param-forward"] != 0 {
							t.Fatal("forwarded a callee that mutates its parameters")
						}
					}
				}
			}
		}
	}
}
