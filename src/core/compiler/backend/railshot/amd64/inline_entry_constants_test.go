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

func TestInlineEntryConstantsKeepOtherLocalsFresh(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, add, eqz := wasm.I32, byte(0x7f), byte(0x41), byte(0x6a), byte(0x45)
		fold := func(x uint64) uint64 { return uint64(uint32(x)) }
		if wide {
			typ, bt, k, add, eqz = wasm.I64, 0x7e, 0x42, 0x7c, 0x50
			fold = func(x uint64) uint64 { return x }
		}
		for _, literal := range []bool{false, true} {
			// Local1 has an unconditional literal initializer. Local2 must still
			// start at zero on each splice, despite the prior call writing nine.
			helper := []byte{1, 2, bt, k, 17, 0x21, 1, 0x20, 2, 0x20, 1, add, 0x20, 0, add, k, 9, 0x21, 2, 0x0b}
			if !literal {
				// A conditional assignment cannot replace zero initialization.
				helper = []byte{1, 2, bt, 0x20, 0, eqz, 0x04, 0x40, k, 17, 0x21, 1, 0x0b, 0x20, 1, 0x0b}
			}
			caller := []byte{0, 0x20, 0, 0x10, 1, 0x20, 1, 0x10, 1, add, 0x0b}
			m := modFuncs(t,
				funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ}, caller},
				funcDef{[]wasm.ValType{typ}, []wasm.ValType{typ}, helper})
			for _, enabled := range []bool{false, true} {
				for _, workers := range []int{1, 4} {
					for _, input := range [][2]uint64{{0, 1}, {1, 0}, {0, 0}, {17, 5}, {0xdeadbeefffffffff, 0xcafebabe00000001}} {
						a, b := fold(input[0]), fold(input[1])
						want := fold(a + b + 34)
						if !literal {
							want = 0
							if a == 0 {
								want += 17
							}
							if b == 0 {
								want += 17
							}
						}
						var stats ModuleStats
						opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"inline": enabled, "value-facts": false}}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						got, _, err := runMemAmd64WithOptions(t, m, opts, nil, input[0], input[1])
						if err != nil || got != want {
							t.Fatalf("wide=%t literal=%t inline=%t workers=%d input=%x got=%x err=%v want=%x", wide, literal, enabled, workers, input, got, err, want)
						}
						if diagnosticsEnabled && (stats.Funcs[0].Peephole["inline-entry-constant-init"] != 0) != (literal && enabled && !profileEnabled) {
							t.Fatalf("wide=%t literal=%t inline=%t profile=%t entry init selection mismatch: %v", wide, literal, enabled, profileEnabled, stats.Funcs[0].Peephole)
						}
					}
				}
			}
		}
	}
}

func TestInlineEntryConstantsKeepPinnedLoopLocalsFresh(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, add, sub, eqz := wasm.I32, byte(0x7f), byte(0x41), byte(0x6a), byte(0x6b), byte(0x45)
		if wide {
			typ, bt, k, add, sub, eqz = wasm.I64, 0x7e, 0x42, 0x7c, 0x7d, 0x50
		}
		// One private GP starts with a literal. The other must still be reset
		// before each call, even though the prior call wrote nine after its get.
		helper := []byte{1, 2, bt, k, 17, 0x21, 1, 0x02, 0x40, 0x03, 0x40,
			0x20, 0, eqz, 0x0d, 1, 0x20, 1, k, 1, add, 0x21, 1,
			0x20, 0, k, 1, sub, 0x21, 0, 0x0c, 0, 0x0b, 0x0b,
			0x20, 1, 0x20, 2, k, 9, 0x21, 2, 0x0b}
		caller := []byte{0, 0x20, 0, 0x10, 1, 0x20, 1, 0x10, 1, add, add, add, 0x0b}
		m := modFuncs(t,
			funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ}, caller},
			funcDef{[]wasm.ValType{typ}, []wasm.ValType{typ, typ}, helper})
		for _, enabled := range []bool{false, true} {
			for _, workers := range []int{1, 4} {
				for _, count := range []uint64{0, 1, 2, 7, 21} {
					var stats ModuleStats
					opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"inline": enabled, "value-facts": false}}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					arg := count
					if !wide {
						arg |= 0xdeadbeef00000000
					}
					got, _, err := runMemAmd64WithOptions(t, m, opts, nil, arg, 3)
					want := 37 + count
					if err != nil || got != want {
						t.Fatalf("wide=%t inline=%t workers=%d count=%d got=%d err=%v want=%d", wide, enabled, workers, count, got, err, want)
					}
					active := enabled && !profileEnabled
					if diagnosticsEnabled && (stats.Funcs[0].Peephole["inline-entry-constant-init"] != 0) != active {
						t.Fatalf("missing entry constant path: %v", stats.Funcs[0].Peephole)
					}
					if diagnosticsEnabled && active && stats.Funcs[0].Peephole["inline-tuple-loop-local-pin"] < 3 {
						t.Fatalf("fixture did not exercise private GP homes: %v", stats.Funcs[0].Peephole)
					}
				}
			}
		}
	}
}
