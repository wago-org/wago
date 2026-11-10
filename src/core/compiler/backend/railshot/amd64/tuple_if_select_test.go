// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestIntegerTupleSelectionPreservesCallerAliases(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, le, mul, add := wasm.I32, byte(0x7f), byte(0x41), byte(0x4d), byte(0x6c), byte(0x6a)
		fold := func(x uint64) uint64 { return uint64(uint32(x)) }
		if wide {
			typ, bt, k, le, mul, add = wasm.I64, 0x7e, 0x42, 0x58, 0x7e, 0x7c
			fold = func(x uint64) uint64 { return x }
		}
		for _, signed := range []bool{false, true} {
			cmp := le
			if signed {
				cmp = 0x4c
				if wide {
					cmp = 0x57
				}
			}
			for _, alias := range []bool{false, true} {
				lo, hi := byte(3), byte(4)
				caller := []byte{1, 2, bt}
				if alias {
					lo, hi, caller = 0, 1, []byte{0}
				}
				// Capture a caller input before the helper, then overwrite it with
				// a result in the alias case. The old prefix must keep its get-time value.
				caller = append(caller, 0x20, 0, 0x20, 0, 0x20, 1, 0x10, 1,
					0x21, hi, 0x21, lo, 0x20, lo, k, 17, mul, 0x20, hi, add, add, 0x0b)
				helper := []byte{0, 0x20, 0, 0x20, 1, cmp, 0x04, 2,
					0x20, 0, 0x20, 1, 0x05, 0x20, 1, 0x20, 0, 0x0b, 0x0b}
				// Third function supplies the zero-parameter pair block signature.
				m := modFuncs(t,
					funcDef{[]wasm.ValType{typ, typ, typ}, []wasm.ValType{typ}, caller},
					funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ, typ}, helper},
					funcDef{nil, []wasm.ValType{typ, typ}, []byte{0, k, 0, k, 0, 0x0b}})
				for _, enabled := range []bool{false, true} {
					for _, workers := range []int{1, 4} {
						for _, pair := range [][2]uint64{{0, 0}, {3, 7}, {17, 5}, {13, 13}, {0x80000001, 7}, {^uint64(0), 0x8000000000000001}} {
							a, b := fold(pair[0]), fold(pair[1])
							less := a <= b
							if signed {
								less = int32(a) <= int32(b)
								if wide {
									less = int64(a) <= int64(b)
								}
							}
							low, high := a, b
							if !less {
								low, high = b, a
							}
							want := fold(17*low + high + a)
							var stats ModuleStats
							opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"inline": enabled, "value-facts": false}}
							if diagnosticsEnabled {
								opts.Stats = &stats
							}
							argA, argB := pair[0], pair[1]
							if !wide {
								argA |= 0xdeadbeef00000000
								argB |= 0xcafebabe00000000
							}
							got, _, err := runMemAmd64WithOptions(t, m, opts, nil, argA, argB, 11)
							if err != nil || got != want {
								t.Fatalf("wide=%t signed=%t alias=%t inline=%t workers=%d input=%x got=%x err=%v want=%x", wide, signed, alias, enabled, workers, pair, got, err, want)
							}
							if diagnosticsEnabled && (stats.Funcs[0].Peephole["tuple-if-select"] != 0) != (enabled && !profileEnabled) {
								t.Fatalf("inline=%t profile=%t stats=%v", enabled, profileEnabled, stats.Funcs[0].Peephole)
							}
						}
					}
				}
			}
		}
	}
}

func TestTupleSelectionRetainsEnclosingBlockPrefix(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, le, add := wasm.I32, byte(0x7f), byte(0x41), byte(0x4d), byte(0x6a)
		fold := func(x uint64) uint64 { return uint64(uint32(x)) }
		if wide {
			typ, bt, k, le, add = wasm.I64, 0x7e, 0x42, 0x58, 0x7c
			fold = func(x uint64) uint64 { return x }
		}
		// The prefix is outside the caller's block and aliases the low-result
		// home overwritten inside it. Every private helper path falls through.
		caller := []byte{0, 0x20, 0, 0x02, bt, 0x20, 0, 0x20, 1, 0x10, 1,
			0x21, 1, 0x21, 0, 0x20, 0, 0x20, 1, add, 0x0b, add, 0x0b}
		helper := []byte{0, 0x20, 0, 0x20, 1, le, 0x04, 2,
			0x20, 0, 0x20, 1, 0x05, 0x20, 1, 0x20, 0, 0x0b, 0x0b}
		m := modFuncs(t,
			funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ}, caller},
			funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ, typ}, helper},
			funcDef{nil, []wasm.ValType{typ, typ}, []byte{0, k, 0, k, 0, 0x0b}})
		for _, enabled := range []bool{false, true} {
			for _, workers := range []int{1, 4} {
				for _, input := range [][2]uint64{{3, 7}, {17, 5}, {0, 0}, {^uint64(0), 0x8000000000000001}} {
					var stats ModuleStats
					opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"inline": enabled, "value-facts": false}}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					got, _, err := runMemAmd64WithOptions(t, m, opts, nil, input[0], input[1])
					want := fold(2*fold(input[0]) + fold(input[1]))
					if err != nil || got != want {
						t.Fatalf("wide=%t inline=%t workers=%d input=%x got=%x err=%v want=%x", wide, enabled, workers, input, got, err, want)
					}
					if diagnosticsEnabled && (stats.Funcs[0].Peephole["tuple-if-select-local-stores"] != 0) != (enabled && !profileEnabled) {
						t.Fatalf("inline=%t profile=%t expected direct result homes: %v", enabled, profileEnabled, stats.Funcs[0].Peephole)
					}
					if diagnosticsEnabled && (stats.Funcs[0].Peephole["straight-line-block"] != 0) != (enabled && !profileEnabled) {
						t.Fatalf("inline=%t profile=%t stats=%v", enabled, profileEnabled, stats.Funcs[0].Peephole)
					}
				}
			}
		}
	}
}
