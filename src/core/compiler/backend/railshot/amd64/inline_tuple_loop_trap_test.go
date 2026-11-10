// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
)

func TestInlineTupleLoopDroppedResultRetainsDivisionTrap(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, eqz, sub, add, divU, divS := wasm.I32, byte(0x7f), byte(0x41), byte(0x45), byte(0x6b), byte(0x6a), byte(0x6e), byte(0x6d)
		minimum := uint64(0x80000000)
		fold := func(v uint64) uint64 { return uint64(uint32(v)) }
		if wide {
			typ, bt, k, eqz, sub, add, divU, divS = wasm.I64, 0x7e, 0x42, 0x50, 0x7d, 0x7c, 0x80, 0x7f
			minimum = 0x8000000000000000
			fold = func(v uint64) uint64 { return v }
		}
		for _, signed := range []bool{false, true} {
			div := divU
			if signed {
				div = divS
			}
			helper := []byte{1, 1, bt, 0x02, 0x40, 0x03, 0x40, 0x20, 0, eqz, 0x0d, 1,
				0x20, 1, 0x20, 2, div, 0x21, 3, 0x20, 0, k, 1, sub, 0x21, 0,
				0x0c, 0, 0x0b, 0x0b, 0x20, 1, 0x20, 3, 0x0b}
			// Keep an older caller operand below the call, then drop its trapping
			// result. Zero iterations must skip the division, not trap eagerly.
			caller := []byte{0, 0x20, 1, 0x20, 0, 0x20, 1, 0x20, 2, 0x10, 1, 0x1a, add, 0x0b}
			m := modFuncs(t, funcDef{[]wasm.ValType{typ, typ, typ}, []wasm.ValType{typ}, caller}, funcDef{[]wasm.ValType{typ, typ, typ}, []wasm.ValType{typ, typ}, helper})
			for _, enabled := range []bool{false, true} {
				for _, workers := range []int{1, 4} {
					for _, tc := range []struct {
						n, a, b uint64
						trap    rt.TrapCode
					}{{0, minimum, 0, 0}, {0, minimum, ^uint64(0), 0}, {1, 17, 5, 0}, {3, minimum, 3, 0}, {1, 17, 0, rt.TrapDivZero}, {2, minimum, ^uint64(0), 0}} {
						wantTrap := tc.trap
						if signed && tc.n != 0 && tc.a == minimum && tc.b == ^uint64(0) {
							wantTrap = rt.TrapDivOverflow
						}
						var stats ModuleStats
						opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"inline": enabled, "value-facts": false}}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						got, _, err := runMemAmd64WithOptions(t, m, opts, nil, tc.n, tc.a, tc.b)
						if wantTrap != 0 {
							var trap *rt.TrapError
							if !errors.As(err, &trap) || trap.Code != wantTrap {
								t.Fatalf("wide=%t signed=%t inline=%t workers=%d input=%v err=%v want trap=%v", wide, signed, enabled, workers, tc, err, wantTrap)
							}
						} else if err != nil || got != fold(2*tc.a) {
							t.Fatalf("wide=%t signed=%t inline=%t workers=%d input=%v got=%x err=%v", wide, signed, enabled, workers, tc, got, err)
						}
						if diagnosticsEnabled && (stats.Funcs[0].Calls[callKindInline] != 0) != (enabled && !profileEnabled) {
							t.Fatalf("inline=%t profile=%t calls=%v", enabled, profileEnabled, stats.Funcs[0].Calls)
						}
					}
				}
			}
		}
	}
}
