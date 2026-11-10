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

func TestRemainderPairPreservesAllLocals(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, eqz, rem, constant, add := wasm.I32, byte(0x7f), byte(0x45), byte(0x70), byte(0x41), byte(0x6a)
		fold := func(v uint64) uint64 { return uint64(uint32(v)) }
		if wide {
			typ, bt, eqz, rem, constant, add = wasm.I64, 0x7e, 0x50, 0x82, 0x42, 0x7c
			fold = func(v uint64) uint64 { return v }
		}
		body := []byte{0, 0x02, 0x40, 0x03, 0x40, 0x20, 1, eqz, 0x0d, 1,
			0x20, 0, 0x20, 1, rem, 0x21, 2, 0x20, 1, 0x21, 0, 0x20, 2, 0x21, 1,
			0x20, 3, constant, 1, add, 0x21, 3, 0x0c, 0, 0x0b, 0x0b}
		for result := 0; result < 3; result++ {
			body = append(body, 0x20, 4, 0x41, byte(result), 0x46, 0x04, bt, 0x20, byte(result), 0x05)
		}
		body = append(body, 0x20, 3, 0x0b, 0x0b, 0x0b, 0x0b)
		params := []wasm.ValType{typ, typ, typ, typ, wasm.I32}
		m := modFuncs(t, funcDef{params, []wasm.ValType{typ}, body}, funcDef{params, []wasm.ValType{typ}, []byte{0, 0x20, 0, 0x0b}})
		for _, enabled := range []bool{false, true} {
			for _, workers := range []int{1, 4} {
				for _, pair := range [][2]uint64{{0, 0}, {1, 0}, {0, 1}, {1, 1}, {7, 2}, {13, 8}, {21, 13}, {34, 21}, {55, 34}, {89, 55}, {^uint64(0), 17}, {0x8000000000000001, 29}} {
					for _, seed := range []uint64{0, ^uint64(0)} {
						a, b, r, steps := fold(pair[0]), fold(pair[1]), fold(seed), fold(seed)
						for b != 0 {
							r = a % b
							a, b = b, r
							steps = fold(steps + 1)
						}
						wants := [4]uint64{a, b, r, steps}
						for result, want := range wants {
							var stats ModuleStats
							opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"scalar-loop-unroll": enabled}}
							if diagnosticsEnabled {
								opts.Stats = &stats
							}
							aArg, bArg := pair[0], pair[1]
							if !wide {
								aArg |= 0xdeadbeef00000000
								bArg |= 0xcafebabe00000000
							}
							got, err := runArm64WrapperWithOptions(t, m, opts, aArg, bArg, seed, seed, uint64(result))
							if err != nil || fold(got) != want {
								t.Fatalf("wide=%t enabled=%t workers=%d pair=%x seed=%x result=%d got=%x err=%v want=%x", wide, enabled, workers, pair, seed, result, got, err, want)
							}
							if diagnosticsEnabled && (stats.Funcs[0].Peephole["scalar-loop-unroll-remainder-pair"] != 0) != (enabled && !profileEnabled) {
								t.Fatalf("path not selected: %v", stats.Funcs[0].Peephole)
							}
						}
					}
				}
			}
		}
	}
}
