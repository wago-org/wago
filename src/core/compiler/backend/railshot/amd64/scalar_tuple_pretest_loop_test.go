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

func TestScalarTuplePretestPreservesEveryExit(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, k, eqz, sub, add, xor := wasm.I32, byte(0x41), byte(0x45), byte(0x6b), byte(0x6a), byte(0x73)
		fold := func(x uint64) uint64 { return uint64(uint32(x)) }
		if wide {
			typ, k, eqz, sub, add, xor = wasm.I64, 0x42, 0x50, 0x7d, 0x7c, 0x85
			fold = func(x uint64) uint64 { return x }
		}
		for _, prefix := range []bool{false, true} {
			body := []byte{0}
			if prefix {
				body = append(body, 0x20, 1)
			}
			body = append(body, 0x20, 0, 0x20, 1, 0x20, 2, 0x03, 1,
				0x21, 2, 0x21, 1, 0x21, 0, 0x20, 0, eqz, 0x04, 2,
				0x20, 1, 0x20, 2, 0x05,
				0x20, 0, k, 1, sub, 0x20, 2, 0x20, 1, 0x20, 2, add, 0x0c, 1,
				0x0b, 0x0b, xor)
			if prefix {
				body = append(body, add)
			}
			body = append(body, 0x0b)
			m := modFuncs(t,
				funcDef{[]wasm.ValType{typ, typ, typ}, []wasm.ValType{typ}, body},
				funcDef{[]wasm.ValType{typ, typ, typ}, []wasm.ValType{typ, typ}, []byte{0, 0x20, 1, 0x20, 2, 0x0b}},
				funcDef{nil, []wasm.ValType{typ, typ}, []byte{0, k, 0, k, 0, 0x0b}})
			for _, enabled := range []bool{false, true} {
				for _, workers := range []int{1, 4} {
					for _, count := range []uint64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 17, 31, 64, 79} {
						for _, seed := range [][2]uint64{{0, 1}, {0xfffffff0, 0x80000001}, {0x8000000000000001, ^uint64(0)}} {
							a, b := fold(seed[0]), fold(seed[1])
							for n := uint64(0); n < count; n++ {
								a, b = b, fold(a+b)
							}
							want := a ^ b
							if prefix {
								want = fold(want + fold(seed[0]))
							}
							var stats ModuleStats
							opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"inline": false, "scalar-loop-unroll": enabled, "prepared-direct-entry": false, "value-facts": false}}
							if diagnosticsEnabled {
								opts.Stats = &stats
							}
							argCount := count
							if !wide {
								argCount |= 0xdeadbeef00000000
							}
							got, _, err := runMemAmd64WithOptions(t, m, opts, nil, argCount, seed[0], seed[1])
							if err != nil || got != want {
								t.Fatalf("wide=%t prefix=%t enabled=%t workers=%d count=%d seed=%x got=%x err=%v want=%x", wide, prefix, enabled, workers, count, seed, got, err, want)
							}
							if diagnosticsEnabled && (stats.Funcs[0].Peephole["scalar-tuple-counted-unroll4"] != 0) != (enabled && !profileEnabled) {
								t.Fatalf("wide=%t prefix=%t enabled=%t profile=%t missing counted guard: %v", wide, prefix, enabled, profileEnabled, stats.Funcs[0].Peephole)
							}
							if diagnosticsEnabled && (stats.Funcs[0].Peephole["scalar-tuple-pretest-unroll4"] != 0) != (enabled && !profileEnabled) {
								t.Fatalf("wide=%t prefix=%t enabled=%t profile=%t missing unroll path: %v", wide, prefix, enabled, profileEnabled, stats.Funcs[0].Peephole)
							}
						}
					}
				}
			}
		}
	}
}
