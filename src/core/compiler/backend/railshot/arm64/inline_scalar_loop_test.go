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

func TestInlineReadonlyIntegerScalarInLoop(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, add, mul, lt := wasm.I32, byte(0x7f), byte(0x41), byte(0x6a), byte(0x6c), byte(0x49)
		fold := func(x uint64) uint64 { return uint64(uint32(x)) }
		if wide {
			typ, bt, k, add, mul, lt = wasm.I64, 0x7e, 0x42, 0x7c, 0x7e, 0x54
			fold = func(x uint64) uint64 { return x }
		}
		body := []byte{1, 2, bt, 0x03, 0x40,
			0x20, 3, 0x20, 0, 0x20, 1, 0x10, 1, add, 0x21, 3,
			0x20, 0, k, 1, add, 0x21, 0, 0x20, 1, k, 3, add, 0x21, 1,
			0x20, 2, k, 1, add, 0x22, 2, k, 7, lt, 0x0d, 0,
			0x0b, 0x20, 3, 0x0b}
		helper := []byte{0, 0x20, 0, 0x20, 1, k, 3, mul, add, k, 1, add, 0x0b}
		m := modFuncs(t, funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ}, body}, funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ}, helper})
		for _, enabled := range []bool{false, true} {
			for _, workers := range []int{1, 4} {
				for _, input := range [][2]uint64{{3, 7}, {0xfffffff0, 0x80000001}, {^uint64(0), 0x8000000000000001}} {
					x, y, want := fold(input[0]), fold(input[1]), uint64(0)
					for range 7 {
						want = fold(want + x + 3*y + 1)
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
					if diagnosticsEnabled && (stats.Funcs[0].Peephole["inline-integer-scalar-param-forward"] != 0) != enabled {
						t.Fatalf("inline=%t forwarded=%v", enabled, stats.Funcs[0].Peephole)
					}
				}
			}
		}
	}
}
