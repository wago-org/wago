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

func TestInlineTupleResultHomesMixedIntegerWidths(t *testing.T) {
	callee := []byte{0, 0x02, 0x40, 0x03, 0x40, 0x20, 0, 0x45, 0x0d, 1,
		0x20, 0, 0x41, 1, 0x6b, 0x21, 0, 0x20, 1, 0x42, 3, 0x7c, 0x21, 1, 0x0c, 0, 0x0b, 0x0b, 0x20, 0, 0x20, 1, 0x0b}
	for _, alias := range []bool{false, true} {
		low, high := byte(2), byte(3)
		if alias {
			low, high = 0, 1
		}
		caller := []byte{2, 1, 0x7f, 1, 0x7e, 0x20, 0, 0xad, 0x20, 1, 0x85,
			0x20, 0, 0x20, 1, 0x10, 1, 0x21, high, 0x21, low, 0x20, low, 0xad, 0x20, high, 0x85, 0x85, 0x0b}
		params := []wasm.ValType{wasm.I32, wasm.I64}
		m := modFuncs(t, funcDef{params, []wasm.ValType{wasm.I64}, caller}, funcDef{params, params, callee})
		for _, enabled := range []bool{false, true} {
			for _, workers := range []int{1, 4} {
				for n := uint64(0); n < 8; n++ {
					for _, value := range []uint64{0, 0x8000000000000001, ^uint64(0) - 1} {
						want := (n ^ value) ^ (value + 3*n)
						var stats ModuleStats
						opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"inline-integer-tuples": enabled}}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						got, err := runArm64WrapperWithOptions(t, m, opts, n|0xdeadbeef00000000, value)
						if err != nil || got != want {
							t.Fatalf("alias=%t enabled=%t workers=%d n=%d value=%x got=%x err=%v want=%x", alias, enabled, workers, n, value, got, err, want)
						}
						if diagnosticsEnabled && (stats.Funcs[0].Peephole["inline-tuple-result-homes"] != 0) != (enabled && !profileEnabled) {
							t.Fatalf("mixed-width path mismatch: %v", stats.Funcs[0].Peephole)
						}
					}
				}
			}
		}
	}
}
