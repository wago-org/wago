// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot"
)

func TestMagicUnsignedAddCorrectionPreservesQuotientAndRemainder(t *testing.T) {
	for _, wide := range []bool{false, true} {
		width := uint(32)
		if wide {
			width = 64
		}
		for _, divisor := range []uint64{5, 7, 19, 31, 63, 127, 0x7fffffff} {
			_, _, add := railshot.MagicU(divisor, width)
			for _, remainder := range []bool{false, true} {
				op := divOp{name: "div_u", opcode: 0x6e}
				if remainder {
					op.opcode, op.rem = 0x70, true
				}
				if wide {
					op.opcode += 0x12
				}
				m := divConstMod(t, wide, op, divisor)
				values := []uint64{0, 1, divisor - 1, divisor, divisor + 1, 3*divisor - 1, 3 * divisor, 0x7fffffff, 0x80000000, 0xffffffff, 1 << 63, ^uint64(0)}
				state := uint64(0x91e10da5a5b63f27)
				for range 16 {
					state ^= state << 13
					state ^= state >> 7
					state ^= state << 17
					values = append(values, state)
				}
				for _, enabled := range []bool{false, true} {
					for _, workers := range []int{1, 4} {
						for _, n := range values {
							var stats ModuleStats
							opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"magic-div": enabled, "value-facts": false}}
							if diagnosticsEnabled {
								opts.Stats = &stats
							}
							arg := n
							if !wide {
								arg |= 0xa5a5a5a500000000
							}
							got, err := runArm64WrapperWithOptions(t, m, opts, arg)
							want := refDivRem(op, wide, n, divisor)
							if err != nil || got != want {
								t.Fatalf("wide=%t remainder=%t d=%x n=%x enabled=%t workers=%d got=%x err=%v want=%x", wide, remainder, divisor, n, enabled, workers, got, err, want)
							}
							if diagnosticsEnabled && (stats.Funcs[0].Peephole["magic-u32-wide-reciprocal"] != 0) != (enabled && add && !wide) {
								t.Fatalf("wide reciprocal admission d=%x enabled=%t stats=%v", divisor, enabled, stats.Funcs[0].Peephole)
							}
							if diagnosticsEnabled && (stats.Funcs[0].Peephole["magic-unsigned-add-shift"] != 0) != (enabled && add && wide) {
								t.Fatalf("fused correction admission wide=%t d=%x enabled=%t stats=%v", wide, divisor, enabled, stats.Funcs[0].Peephole)
							}
						}
					}
				}
			}
		}
	}
}
