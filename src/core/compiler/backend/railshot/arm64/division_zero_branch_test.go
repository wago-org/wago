// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin) && arm64

package arm64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
)

func TestDivisionZeroBranchPreservesAllIntegerTrapCases(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, min, max := wasm.I32, uint64(1<<31), uint64(0xffffffff)
		fold := func(n uint64) uint64 { return uint64(uint32(n)) }
		if wide {
			typ, min, max = wasm.I64, 1<<63, ^uint64(0)
			fold = func(n uint64) uint64 { return n }
		}
		for _, op := range divOps(wide) {
			params, results := []wasm.ValType{typ, typ}, []wasm.ValType{typ}
			m := modFuncs(t, funcDef{params, results, []byte{0, 0x20, 0, 0x20, 1, op.opcode, 0x0b}}, funcDef{params, results, []byte{0, 0x20, 0, 0x0b}})
			for _, borrowed := range []bool{false, true} {
				for _, workers := range []int{1, 4} {
					for _, n := range []uint64{0, 1, min - 1, min, max} {
						for _, d := range []uint64{0, 1, 2, min, max} {
							var stats ModuleStats
							opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"borrowed-div-rem": borrowed, "value-facts": false}}
							if diagnosticsEnabled {
								opts.Stats = &stats
							}
							a, b := n, d
							if !wide {
								a |= 0xdeadbeef00000000
								b |= 0xcafebabe00000000
							}
							got, err := runArm64WrapperWithOptions(t, m, opts, a, b)
							wantTrap := rt.TrapCode(0)
							if fold(d) == 0 {
								wantTrap = rt.TrapDivZero
							} else if op.signed && !op.rem && fold(n) == min && fold(d) == max {
								wantTrap = rt.TrapDivOverflow
							}
							if wantTrap != 0 {
								var trap *rt.TrapError
								if !errors.As(err, &trap) || trap.Code != wantTrap {
									t.Fatalf("op=%s borrowed=%t n=%x d=%x err=%v want trap %d", op.name, borrowed, n, d, err, wantTrap)
								}
							} else if want := refDivRem(op, wide, n, d); err != nil || got != want {
								t.Fatalf("op=%s borrowed=%t n=%x d=%x got=%x err=%v want=%x", op.name, borrowed, n, d, got, err, want)
							}
							if diagnosticsEnabled && stats.Funcs[0].Peephole["direct-zero-branch"] != 1 {
								t.Fatalf("division zero branch admission: %v", stats.Funcs[0].Peephole)
							}
						}
					}
				}
			}
		}
	}
}
