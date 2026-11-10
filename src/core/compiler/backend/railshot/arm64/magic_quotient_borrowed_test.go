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

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestBorrowedUnsignedQuotientPreservesCallerReadsAndAssignments(t *testing.T) {
	for _, divisor := range []uint32{3, 5, 7, 17, 19, 31, 97, 127, 1000, 0x7fffffff, 0x80000001, 0xffffffff} {
		for _, assign := range []bool{false, true} {
			// A loop selects ordinary local homes; the false backedge keeps this
			// oracle to one calculation while exercising a pinned caller read.
			body := []byte{0, 0x03, 0x7f, 0x20, 0}
			if !assign {
				body = append(body, 0x20, 0)
			}
			body = append(body, 0x41)
			body = append(body, wasmtest.SLEB32(int32(divisor))...)
			body = append(body, 0x6e)
			if assign {
				body = append(body, 0x21, 0, 0x20, 0)
			} else {
				body = append(body, 0x6a)
			}
			body = append(body, 0x41, 0, 0x0d, 0, 0x0b, 0x0b)
			m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
			values := []uint32{0, 1, divisor - 1, divisor, divisor + 1, 0x7fffffff, 0x80000000, 0xffffffff}
			state := uint32(0x91e10da5)
			for range 24 {
				state ^= state << 13
				state ^= state >> 17
				state ^= state << 5
				values = append(values, state)
			}
			for _, enabled := range []bool{false, true} {
				for _, workers := range []int{1, 4} {
					for _, n := range values {
						var stats ModuleStats
						opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"magic-div": enabled, "value-facts": false, "interval-region-pins": false}}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						got, err := runArm64WrapperWithOptions(t, m, opts, uint64(n)|0xdeadbeef00000000)
						want := n / divisor
						if !assign {
							want += n
						}
						if err != nil || got != uint64(want) {
							t.Fatalf("assign=%t n=%x d=%x enabled=%t workers=%d got=%x err=%v want=%x", assign, n, divisor, enabled, workers, got, err, want)
						}
						// A self-assignment first snapshots its old local value. That owned
						// operand keeps the established path rather than borrowing its home.
						if diagnosticsEnabled && (stats.Funcs[0].Peephole["magic-quotient-borrowed"] != 0) != (enabled && !assign) {
							t.Fatalf("borrowed quotient admission assign=%t d=%x enabled=%t stats=%v", assign, divisor, enabled, stats.Funcs[0].Peephole)
						}
					}
				}
			}
		}
	}
}
