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

func TestInlineIntegerScalarParamsPreserveGetTimeValues(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, k, add, mul := wasm.I32, byte(0x41), byte(0x6a), byte(0x6c)
		fold := func(v uint64) uint64 { return uint64(uint32(v)) }
		if wide {
			typ, k, add, mul = wasm.I64, 0x42, 0x7c, 0x7e
			fold = func(v uint64) uint64 { return v }
		}
		for _, mutateBefore := range []bool{false, true} {
			for _, mutateHelper := range []bool{false, true} {
				// Keep a caller prefix and the first result live across a caller
				// write and a second splice using the same private parameter slot.
				body := []byte{0, 0x20, 0, 0x20, 0}
				if mutateBefore {
					body = append(body, k, 9, 0x21, 0)
				}
				body = append(body, 0x10, 1, k, 13, 0x21, 0, 0x20, 0, 0x10, 1, add, add, 0x0b)
				helper := []byte{0, 0x20, 0, k, 3, mul, k, 1, add}
				if mutateHelper {
					helper = append(helper, 0x22, 0)
				}
				helper = append(helper, 0x0b)
				m := modFuncs(t, funcDef{[]wasm.ValType{typ}, []wasm.ValType{typ}, body}, funcDef{[]wasm.ValType{typ}, []wasm.ValType{typ}, helper})
				for _, enabled := range []bool{false, true} {
					for _, workers := range []int{1, 4} {
						for _, input := range []uint64{0, 7, 0xdeadbeef80000001, ^uint64(0), 0x8000000000000001} {
							var stats ModuleStats
							opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"inline": enabled, "value-facts": false}}
							if diagnosticsEnabled {
								opts.Stats = &stats
							}
							want := fold(fold(input) + fold(3*fold(input)+1) + 40)
							got, _, err := runMemAmd64WithOptions(t, m, opts, nil, input)
							if err != nil || got != want {
								t.Fatalf("wide=%t mutateBefore=%t mutateHelper=%t inline=%t workers=%d input=%x got=%x err=%v want=%x", wide, mutateBefore, mutateHelper, enabled, workers, input, got, err, want)
							}
							if diagnosticsEnabled && (stats.Funcs[0].Peephole["inline-integer-scalar-param-forward"] != 0) != (enabled && !mutateHelper && !profileEnabled) {
								t.Fatalf("incorrect scalar forwarding selection: %v", stats.Funcs[0].Peephole)
							}
						}
					}
				}
			}
		}
	}
}

func TestInlineIntegerScalarParamsKeepDroppedTraps(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, k, div := wasm.I32, byte(0x41), byte(0x6e)
		if wide {
			typ, k, div = wasm.I64, 0x42, 0x80
		}
		m := modFuncs(t,
			funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ}, []byte{0, 0x20, 0, 0x20, 1, 0x10, 1, 0x1a, k, 7, 0x0b}},
			funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ}, []byte{0, 0x20, 0, 0x20, 1, div, 0x0b}})
		for _, enabled := range []bool{false, true} {
			for _, workers := range []int{1, 4} {
				var stats ModuleStats
				opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"inline": enabled, "value-facts": false}}
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				got, _, err := runMemAmd64WithOptions(t, m, opts, nil, 17, 5)
				if err != nil || got != 7 {
					t.Fatalf("wide=%t inline=%t workers=%d normal got=%d err=%v", wide, enabled, workers, got, err)
				}
				if diagnosticsEnabled && stats.Funcs[0].Peephole["inline-integer-scalar-param-forward"] != 0 {
					t.Fatal("forwarded a trapping scalar helper")
				}
				_, _, err = runMemAmd64WithOptions(t, m, opts, nil, 17, 0)
				var trap *rt.TrapError
				if !errors.As(err, &trap) || trap.Code != rt.TrapDivZero {
					t.Fatalf("wide=%t inline=%t workers=%d err=%v want division by zero", wide, enabled, workers, err)
				}
			}
		}
	}
}
