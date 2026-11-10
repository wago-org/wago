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
	x64 "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestScalarPretestCounterProofRejectsObservations(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, kind := range []string{"private", "body-read", "body-write", "guard-read", "incoming-flags", "not-pinned"} {
			var guard, tail x64.Asm
			if kind == "incoming-flags" {
				guard.MovReg64(R10, R12)
			} else if kind == "guard-read" {
				guard.TestSelf(R11, wide)
			} else {
				guard.TestSelf(R12, wide)
			}
			tail.AluRI(5, R12, 1, wide)
			if kind == "body-read" {
				tail.MovReg64(R10, R11)
			}
			if kind == "body-write" {
				tail.MovReg64(R11, R10)
			}
			tail.AluRI(0, R11, 1, wide)
			pins := maskOf(R10, R11, R12)
			if kind == "not-pinned" {
				pins = pins.remove(R11)
			}
			_, _, _, got := scalarPretestPrivateIncrement(guard.B, tail.B, pins)
			if got != (kind == "private") {
				t.Fatalf("wide=%t kind=%s admitted=%t", wide, kind, got)
			}
			for cut := 0; cut < len(tail.B); cut++ {
				if _, _, _, ok := scalarPretestPrivateIncrement(guard.B, tail.B[:cut], pins); ok {
					t.Fatalf("truncated body admitted at %d", cut)
				}
			}
		}
	}
}

func TestScalarPretestPrivateCounterEveryPartialExit(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, eqz, add, sub, xor := wasm.I32, byte(0x7f), byte(0x41), byte(0x45), byte(0x6a), byte(0x6b), byte(0x73)
		fold := func(x uint64) uint64 { return uint64(uint32(x)) }
		if wide {
			typ, bt, k, eqz, add, sub, xor = wasm.I64, 0x7e, 0x42, 0x50, 0x7c, 0x7d, 0x85
			fold = func(x uint64) uint64 { return x }
		}
		for _, observed := range []bool{false, true} {
			body := []byte{1, 2, bt, 0x20, 1, 0x21, 2, 2, 0x40, 3, 0x40, 0x20, 0, eqz, 0x0d, 1,
				0x20, 0, k, 1, sub, 0x21, 0}
			if observed {
				body = append(body, 0x20, 3, 0x20, 2, add, 0x21, 3)
			}
			body = append(body, 0x20, 2, k, 1, add, 0x21, 2, 0x0c, 0, 0x0b, 0x0b, 0x20, 2, 0x20, 3, xor, 0x0b)
			params := []wasm.ValType{typ, typ}
			m := modFuncs(t, funcDef{params, []wasm.ValType{typ}, body}, funcDef{params, []wasm.ValType{typ}, []byte{0, 0x20, 0, 0x0b}})
			for _, enabled := range []bool{false, true} {
				for _, workers := range []int{1, 4} {
					for n := uint64(0); n <= 17; n++ {
						for _, seed := range []uint64{0, 19, 0xfffffff8, ^uint64(0) - 5, 0x1234567880000001} {
							counter, sum := fold(seed), uint64(0)
							for range n {
								if observed {
									sum = fold(sum + counter)
								}
								counter = fold(counter + 1)
							}
							var stats ModuleStats
							opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"scalar-loop-unroll": enabled, "value-facts": false}}
							if diagnosticsEnabled {
								opts.Stats = &stats
							}
							got, _, err := runMemAmd64WithOptions(t, m, opts, nil, n, seed)
							if err != nil || got != counter^sum {
								t.Fatalf("wide=%t observed=%t unroll=%t workers=%d n=%d seed=%x got=%x err=%v want=%x", wide, observed, enabled, workers, n, seed, got, err, counter^sum)
							}
							if diagnosticsEnabled {
								want := enabled && !observed && !profileEnabled
								if got := stats.Funcs[0].Peephole["scalar-pretest-private-counter-group"] != 0; got != want {
									t.Fatalf("wide=%t observed=%t unroll=%t profile=%t missing counter proof: %v", wide, observed, enabled, profileEnabled, stats.Funcs[0].Peephole)
								}
							}
						}
					}
				}
			}
		}
	}
}
