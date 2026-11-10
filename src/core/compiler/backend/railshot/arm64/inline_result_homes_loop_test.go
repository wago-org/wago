// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestInlineTupleResultHomesKeepZeroIterationCallerValues(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, add, mul, eqz, rem := wasm.I32, byte(0x7f), byte(0x41), byte(0x6a), byte(0x6c), byte(0x45), byte(0x70)
		fold := func(v uint64) uint64 { return uint64(uint32(v)) }
		if wide {
			typ, bt, k, add, mul, eqz, rem = wasm.I64, 0x7e, 0x42, 0x7c, 0x7e, 0x50, 0x82
			fold = func(v uint64) uint64 { return v }
		}
		callee := []byte{1, 2, bt, 0x02, 0x40, 0x03, 0x40, 0x20, 1, eqz, 0x0d, 1,
			0x20, 0, 0x20, 1, rem, 0x21, 3, 0x20, 1, 0x21, 0, 0x20, 3, 0x21, 1,
			0x20, 2, k, 1, add, 0x21, 2, 0x0c, 0, 0x0b, 0x0b, 0x20, 0, 0x20, 2, 0x0b}
		caller := []byte{1, 1, 0x7f, 0x02, 0x40, 0x03, 0x40, 0x20, 5, 0x20, 0, 0x4f, 0x0d, 1,
			0x20, 1, 0x20, 2, 0x10, 1, 0x21, 4, 0x21, 3, 0x20, 5, 0x41, 1, 0x6a, 0x21, 5, 0x0c, 0, 0x0b, 0x0b,
			0x20, 3, k, 17, mul, 0x20, 4, add, 0x0b}
		m := modFuncs(t, funcDef{[]wasm.ValType{wasm.I32, typ, typ, typ, typ}, []wasm.ValType{typ}, caller}, funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ, typ}, callee})
		for _, enabled := range []bool{false, true} {
			for _, workers := range []int{1, 4} {
				for _, n := range []uint64{0, 1, 2, 4} {
					for _, input := range [][4]uint64{{0, 0, 5, 7}, {13, 8, 21, 13}, {^uint64(0), 17, ^uint64(0), 0x8000000000000001}} {
						low, high := fold(input[2]), fold(input[3])
						if n != 0 {
							a, b, steps := fold(input[0]), fold(input[1]), uint64(0)
							for b != 0 {
								a, b = b, a%b
								steps++
							}
							low, high = a, steps
						}
						want := fold(low*17 + high)
						var stats ModuleStats
						opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"inline-integer-tuples": enabled}}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						got, err := runArm64WrapperWithOptions(t, m, opts, n|0xdeadbeef00000000, input[0], input[1], input[2], input[3])
						if err != nil || fold(got) != want {
							t.Fatalf("wide=%t enabled=%t workers=%d n=%d input=%x got=%x err=%v want=%x", wide, enabled, workers, n, input, got, err, want)
						}
						if diagnosticsEnabled && (stats.Funcs[0].Peephole["inline-tuple-result-homes"] != 0) != (enabled && !profileEnabled) {
							t.Fatalf("coalescing path mismatch: %v", stats.Funcs[0].Peephole)
						}
					}
				}
			}
		}
	}
}
