// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestScalarPretestUnrollCombinesIndependentCounter(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, add, ge := wasm.I32, byte(0x7f), byte(0x41), byte(0x6a), byte(0x4f)
		fold := func(v uint64) uint64 { return uint64(uint32(v)) }
		if wide {
			typ, bt, k, add, ge = wasm.I64, 0x7e, 0x42, 0x7c, 0x5a
			fold = func(v uint64) uint64 { return v }
		}
		for _, variant := range []string{"independent", "observed", "reset", "before-test"} {
			body := []byte{1, 2, bt, 0x20, 3, 0x21, 5, 0x02, 0x40, 0x03, 0x40, 0x20, 1, 0x20, 0, ge, 0x0d, 1, 0x20, 4, 0x20, 2, 0x20, 1, add}
			if variant == "before-test" {
				body = append(append(append([]byte{}, body[:11]...), 0x20, 5, k, 1, add, 0x21, 5), body[11:]...)
			}
			if variant == "observed" {
				body = append(body, 0x20, 5, add)
			}
			body = append(body, add, 0x21, 4, 0x20, 1, k, 1, add, 0x21, 1)
			if variant == "reset" {
				body = append(body, 0x20, 3, 0x21, 5)
			}
			if variant != "before-test" {
				body = append(body, 0x20, 5, k, 1, add, 0x21, 5)
			}
			body = append(body, 0x0c, 0, 0x0b, 0x0b, 0x20, 4, 0x20, 1, add, 0x20, 5, add, 0x0b)
			params, result := []wasm.ValType{typ, typ, typ, typ}, []wasm.ValType{typ}
			m := modFuncs(t, funcDef{params, result, body}, funcDef{params, result, []byte{0, 0x20, 0, 0x0b}})
			for _, enabled := range []bool{false, true} {
				for _, workers := range []int{1, 4} {
					for bound := uint64(0); bound < 14; bound++ {
						for _, initial := range []uint64{0, 2, ^uint64(0)} {
							for _, seed := range []uint64{0, ^uint64(0) - 1, ^uint64(0)} {
								value := uint64(0x8000000000000003)
								index, sum, count := fold(initial), uint64(0), fold(seed)
								for {
									if variant == "before-test" {
										count = fold(count + 1)
									}
									if index >= bound {
										break
									}
									contribution := fold(value + index)
									if variant == "observed" {
										contribution = fold(contribution + count)
									}
									sum = fold(sum + contribution)
									index = fold(index + 1)
									if variant == "reset" {
										count = fold(seed)
									}
									if variant != "before-test" {
										count = fold(count + 1)
									}
								}
								want := fold(sum + index + count)
								var stats ModuleStats
								opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"scalar-loop-unroll": enabled}}
								if diagnosticsEnabled {
									opts.Stats = &stats
								}
								nArg, iArg := bound, initial
								if !wide {
									nArg |= 0xdeadbeef00000000
									iArg |= 0xcafebabe00000000
								}
								got, err := runArm64WrapperWithOptions(t, m, opts, nArg, iArg, value, seed)
								if err != nil || got != want {
									t.Fatalf("wide=%t variant=%s enabled=%t workers=%d bound=%d initial=%x seed=%x got=%x err=%v want=%x", wide, variant, enabled, workers, bound, initial, seed, got, err, want)
								}
								if diagnosticsEnabled && (stats.Funcs[0].Peephole["scalar-loop-unroll-counter"] != 0) != (enabled && variant == "independent" && !profileEnabled) {
									t.Fatalf("counter path mismatch wide=%t variant=%s enabled=%t stats=%v", wide, variant, enabled, stats.Funcs[0].Peephole)
								}
							}
						}
					}
				}
			}
		}
	}
}

func TestScalarCounterProofRejectsReadsAndAmbiguity(t *testing.T) {
	add5 := uint32(0x110004a5) // ADD W5,W5,#1
	add6 := uint32(0x110004c6)
	for _, words := range [][]uint32{
		{0x34000005, add5},             // the exit reads W5
		{0x54000000, add5, add6},       // two independent counters
		{0x6b0100bf, 0x54000000, add5}, // CMP W5,W1 feeds the exit
		{0x54000000, 0x2a0103e5, add5}, // MOV W5,W1 resets the counter
		{0x54000000, 0x1b031441, add5}, // MADD reads W5 in Ra
	} {
		f := gpClassEmitter()
		f.pinnedLocalMask = maskOf(X5, X6)
		exit := 0
		if words[0] == 0x6b0100bf {
			exit = 1
		}
		if f.unrollScalarCounter(0, words, exit, nil) {
			t.Fatalf("unsafe counter admitted: %x", words)
		}
		if len(f.a.B) != 0 {
			t.Fatal("rejected counter emitted code")
		}
	}
}
