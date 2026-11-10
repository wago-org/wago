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
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
	"testing"
)

func TestDirectIntegerPairResultsPreserveAssignments(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, add, xor := wasm.I32, byte(0x7f), byte(0x41), byte(0x6a), byte(0x73)
		fold := func(x uint64) uint64 { return uint64(uint32(x)) }
		if wide {
			typ, bt, k, add, xor = wasm.I64, 0x7e, 0x42, 0x7c, 0x85
			fold = func(x uint64) uint64 { return x }
		}
		for _, variant := range []string{"identity", "swap", "duplicate", "arithmetic"} {
			body := []byte{0, 0x20, 0, 0x20, 1, 0x0b}
			switch variant {
			case "swap":
				body = []byte{0, 0x20, 1, 0x20, 0, 0x0b}
			case "duplicate":
				body = []byte{0, 0x20, 0, 0x20, 0, 0x0b}
			case "arithmetic":
				body = []byte{0, 0x20, 0, 0x20, 1, xor, 0x20, 0, k, 7, add, 0x0b}
			}
			caller := []byte{1, 2, bt, 0x20, 1, 0x20, 2, 0x10, 1, 0x21, 4, 0x21, 3, 0x20, 0, 0x04, bt, 0x20, 4, 0x05, 0x20, 3, 0x0b, 0x0b}
			m := modFuncs(t, funcDef{[]wasm.ValType{wasm.I32, typ, typ}, []wasm.ValType{typ}, caller}, funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ, typ}, body})
			for _, enabled := range []bool{false, true} {
				for _, workers := range []int{1, 4} {
					for _, input := range [][2]uint64{{0, 1}, {3, 7}, {0x80000001, 0xfffffff0}, {^uint64(0), 0x8000000000000001}} {
						a, b := fold(input[0]), fold(input[1])
						want := [2]uint64{a, b}
						switch variant {
						case "swap":
							want = [2]uint64{b, a}
						case "duplicate":
							want = [2]uint64{a, a}
						case "arithmetic":
							want = [2]uint64{a ^ b, fold(a + 7)}
						}
						if !wide {
							a |= 0xdeadbeef00000000
							b |= 0xcafebabe00000000
						}
						for result := range 2 {
							var stats ModuleStats
							opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"inline": false, "reg-abi": enabled, "value-facts": false}}
							if diagnosticsEnabled {
								opts.Stats = &stats
							}
							got, err := runArm64WrapperWithOptions(t, m, opts, uint64(result), a, b)
							if !wide {
								got = fold(got)
							}
							if err != nil || got != want[result] {
								t.Fatalf("wide=%t kind=%s enabled=%t workers=%d input=%x result=%d got=%x err=%v want=%x", wide, variant, enabled, workers, input, result, got, err, want[result])
							}
							if diagnosticsEnabled && (stats.Funcs[1].Peephole["direct-integer-pair-results"] != 0) != (enabled && !profileEnabled) {
								t.Fatal("pair result path selection mismatch")
							}
						}
					}
				}
			}
		}
	}
}

func TestDirectIntegerPairKeepsControlAndCallsOnFallback(t *testing.T) {
	params, results := []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32, wasm.I32}
	caller := funcDef{[]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, []byte{1, 2, 0x7f, 0x20, 1, 0x20, 2, 0x10, 1, 0x21, 4, 0x21, 3, 0x20, 0, 0x04, 0x7f, 0x20, 4, 0x05, 0x20, 3, 0x0b, 0x0b}}
	for _, kind := range []string{"return", "function-branch", "loop", "call", "large-body", "many-locals"} {
		t.Run(kind, func(t *testing.T) {
			body := []byte{0, 0x20, 0, 0x20, 1, 0x0b}
			switch kind {
			case "return":
				body = []byte{0, 0x20, 0, 0x20, 1, 0x0f, 0x0b}
			case "function-branch":
				body = []byte{0, 0x20, 0, 0x20, 1, 0x0c, 0, 0x0b}
			case "loop":
				body = []byte{0, 0x02, 0x40, 0x03, 0x40, 0x20, 0, 0x45, 0x0d, 1, 0x20, 0, 0x41, 1, 0x6b, 0x21, 0, 0x0c, 0, 0x0b, 0x0b, 0x20, 0, 0x20, 1, 0x0b}
			case "call":
				body = []byte{0, 0x20, 0, 0x20, 1, 0x10, 2, 0x0b}
			case "large-body":
				body = []byte{0}
				for range 65 {
					body = append(body, 0x01)
				}
				body = append(body, 0x20, 0, 0x20, 1, 0x0b)
			case "many-locals":
				body = []byte{1, 7, 0x7f, 0x20, 0, 0x20, 1, 0x0b}
			}
			defs := []funcDef{caller, {params, results, body}}
			if kind == "call" {
				defs = append(defs, funcDef{params, results, []byte{0, 0x20, 0, 0x20, 1, 0x0b}})
			}
			m := modFuncs(t, defs...)
			for _, input := range [][2]uint64{{0, 1}, {3, 7}, {17, 29}} {
				for result := range 2 {
					want := input[result]
					if kind == "loop" && result == 0 {
						want = 0
					}
					var stats ModuleStats
					opts := CompileOptions{Optimizations: map[string]bool{"inline": false}}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					got, err := runArm64WrapperWithOptions(t, m, opts, uint64(result), input[0], input[1])
					if err != nil || got != want {
						t.Fatalf("input=%v result=%d got=%d err=%v want=%d", input, result, got, err, want)
					}
					if diagnosticsEnabled && stats.Funcs[1].Peephole["direct-integer-pair-results"] != 0 {
						t.Fatal("non-fallthrough or oversized function used direct pair results")
					}
				}
			}
		})
	}
}

func TestDirectIntegerPairMixedWidths(t *testing.T) {
	caller := funcDef{[]wasm.ValType{wasm.I32, wasm.I64, wasm.I32}, []wasm.ValType{wasm.I64}, []byte{2, 1, 0x7f, 1, 0x7e, 0x20, 1, 0x20, 2, 0x10, 1, 0x21, 4, 0x21, 3, 0x20, 0, 0x04, 0x7e, 0x20, 4, 0x05, 0x20, 3, 0xad, 0x0b, 0x0b}}
	helper := funcDef{[]wasm.ValType{wasm.I64, wasm.I32}, []wasm.ValType{wasm.I32, wasm.I64}, []byte{0, 0x20, 1, 0x20, 0, 0x0b}}
	m := modFuncs(t, caller, helper)
	for _, input := range [][2]uint64{{0, 1}, {0x8000000000000001, 0xdeadbeefffffffff}, {^uint64(0), 0xcafebabe80000000}} {
		for result := range 2 {
			want := uint64(uint32(input[1]))
			if result == 1 {
				want = input[0]
			}
			var stats ModuleStats
			opts := CompileOptions{Optimizations: map[string]bool{"inline": false}}
			if diagnosticsEnabled {
				opts.Stats = &stats
			}
			got, err := runArm64WrapperWithOptions(t, m, opts, uint64(result), input[0], input[1])
			if err != nil || got != want {
				t.Fatalf("input=%x result=%d got=%x err=%v want=%x", input, result, got, err, want)
			}
			if diagnosticsEnabled && (stats.Funcs[1].Peephole["direct-integer-pair-results"] != 0) != (!profileEnabled) {
				t.Fatal("mixed integer pair path was not selected")
			}
		}
	}
}

func TestDirectIntegerPairDivisionTraps(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, div, rem := wasm.I32, byte(0x6e), byte(0x70)
		if wide {
			typ, div, rem = wasm.I64, 0x80, 0x82
		}
		for _, secondOnly := range []bool{false, true} {
			body := []byte{0, 0x20, 0, 0x20, 1, div, 0x20, 0, 0x20, 1, rem, 0x0b}
			want := uint64(3)
			if secondOnly {
				body = []byte{0, 0x20, 0, 0x20, 0, 0x20, 1, div, 0x0b}
				want = 17
			}
			helper := funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ, typ}, body}
			caller := funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ}, []byte{0, 0x20, 0, 0x20, 1, 0x10, 1, 0x1a, 0x0b}}
			m := modFuncs(t, caller, helper)
			var stats ModuleStats
			opts := CompileOptions{Optimizations: map[string]bool{"inline": false}}
			if diagnosticsEnabled {
				opts.Stats = &stats
			}
			got, err := runArm64WrapperWithOptions(t, m, opts, 17, 5)
			if err != nil || got != want {
				t.Fatalf("wide=%t secondOnly=%t result=%d err=%v want=%d", wide, secondOnly, got, err, want)
			}
			wantDirect := !profileEnabled
			if diagnosticsEnabled && (stats.Funcs[1].Peephole["direct-integer-pair-results"] != 0) != wantDirect {
				t.Fatal("division pair path was not selected")
			}
			_, err = runArm64WrapperWithOptions(t, m, opts, 17, 0)
			var trap *rt.TrapError
			if !errors.As(err, &trap) || trap.Code != rt.TrapDivZero {
				t.Fatalf("wide=%t secondOnly=%t got %v want division-by-zero", wide, secondOnly, err)
			}
		}
	}
}

func TestDirectIntegerPairEightParametersAndDeferredTrees(t *testing.T) {
	params := make([]wasm.ValType, 8)
	for i := range params {
		params[i] = wasm.I64
	}
	callerParams := append([]wasm.ValType{wasm.I32}, params...)
	body := []byte{1, 2, 0x7e}
	for i := 1; i <= 8; i++ {
		body = append(body, 0x20, byte(i))
	}
	body = append(body, 0x10, 1, 0x21, 10, 0x21, 9, 0x20, 0, 0x04, 0x7e, 0x20, 10, 0x05, 0x20, 9, 0x0b, 0x0b)
	helper := []byte{0, 0x20, 0, 0x20, 1, 0x7c, 0x20, 2, 0x20, 3, 0x7c, 0x7e, 0x20, 4, 0x20, 5, 0x7c, 0x20, 6, 0x20, 7, 0x7c, 0x85, 0x0b}
	m := modFuncs(t, funcDef{callerParams, []wasm.ValType{wasm.I64}, body}, funcDef{params, []wasm.ValType{wasm.I64, wasm.I64}, helper})
	seed := uint64(0x123456789abcdef)
	for range 16 {
		input := make([]uint64, 8)
		for i := range input {
			seed ^= seed << 13
			seed ^= seed >> 7
			seed ^= seed << 17
			input[i] = seed
		}
		want := [2]uint64{(input[0] + input[1]) * (input[2] + input[3]), (input[4] + input[5]) ^ (input[6] + input[7])}
		for result := range 2 {
			var stats ModuleStats
			opts := CompileOptions{Workers: 4, Optimizations: map[string]bool{"inline": false}}
			if diagnosticsEnabled {
				opts.Stats = &stats
			}
			args := append([]uint64{uint64(result)}, input...)
			got, err := runArm64WrapperWithOptions(t, m, opts, args...)
			if err != nil || got != want[result] {
				t.Fatalf("input=%x result=%d got=%x err=%v want=%x", input, result, got, err, want[result])
			}
			wantDirect := !profileEnabled
			if diagnosticsEnabled && (stats.Funcs[1].Peephole["direct-integer-pair-results"] != 0) != wantDirect {
				t.Fatal("eight-parameter pair path was not selected")
			}
		}
	}
}
