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

func TestTupleCountedLoopPreservesCounterOperands(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, k, eqz, sub, add, xor := wasm.I32, byte(0x41), byte(0x45), byte(0x6b), byte(0x6a), byte(0x73)
		fold := func(x uint64) uint64 { return uint64(uint32(x)) }
		if wide {
			typ, k, eqz, sub, add, xor = wasm.I64, 0x42, 0x50, 0x7d, 0x7c, 0x85
			fold = func(x uint64) uint64 { return x }
		}
		body := []byte{0, 0x20, 0, 0x20, 1, 0x20, 2, 0x03, 1,
			0x21, 2, 0x21, 1, 0x21, 0, 0x20, 0, eqz, 0x04, 2,
			0x20, 1, 0x20, 2, 0x05,
			0x20, 0, k, 1, sub, 0x20, 1, 0x20, 0, add, 0x20, 2, 0x20, 0, xor, 0x0c, 1,
			0x0b, 0x0b, xor, 0x0b}
		m := modFuncs(t,
			funcDef{[]wasm.ValType{typ, typ, typ}, []wasm.ValType{typ}, body},
			funcDef{[]wasm.ValType{typ, typ, typ}, []wasm.ValType{typ, typ}, []byte{0, 0x20, 1, 0x20, 2, 0x0b}},
			funcDef{nil, []wasm.ValType{typ, typ}, []byte{0, k, 0, k, 0, 0x0b}})
		for _, enabled := range []bool{false, true} {
			for _, workers := range []int{1, 4} {
				var stats ModuleStats
				opts := CompileOptions{Workers: workers, Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"inline": false, "scalar-loop-unroll": enabled, "prepared-direct-entry": false, "value-facts": false}}
				cm, err := CompileModuleWith(m, opts)
				if err != nil {
					t.Fatal(err)
				}
				for _, count := range []uint64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 17, 31, 64, 79, 97, 257} {
					for _, seed := range [][2]uint64{{0, 1}, {0xfffffff0, 0x80000001}, {0x8000000000000001, ^uint64(0)}} {
						a, b := fold(seed[0]), fold(seed[1])
						for n := count; n != 0; n-- {
							a, b = fold(a+n), b^n
						}
						argCount := count
						if !wide {
							argCount |= 0xdeadbeef00000000
						}
						if got := runCompiledAmd64u(t, cm, argCount, seed[0], seed[1]); got != a^b {
							t.Fatalf("wide=%t enabled=%t workers=%d count=%d seed=%x got=%x want=%x", wide, enabled, workers, count, seed, got, a^b)
						}
					}
				}
				if diagnosticsEnabled && stats.Funcs[0].Peephole["scalar-tuple-counted-counter-group"] != 0 {
					t.Fatalf("coalesced observed counter: %v", stats.Funcs[0].Peephole)
				}
				if cm.CodeImage != nil {
					cm.CodeImage.Close()
				}
			}
		}
	}
}
