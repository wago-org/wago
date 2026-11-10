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

func TestInlineTupleResultHomesAvoidTransfers(t *testing.T) {
	caller := []byte{1, 2, 0x7f, 0x20, 0, 0x20, 1, 0x10, 1, 0x21, 3, 0x21, 2, 0x20, 2, 0x20, 3, 0x73, 0x0b}
	callee := []byte{0, 0x02, 0x40, 0x03, 0x40, 0x20, 0, 0x45, 0x0d, 1,
		0x20, 0, 0x41, 1, 0x6b, 0x21, 0, 0x20, 1, 0x41, 3, 0x6a, 0x21, 1, 0x0c, 0, 0x0b, 0x0b, 0x20, 0, 0x20, 1, 0x0b}
	params := []wasm.ValType{wasm.I32, wasm.I32}
	m := modFuncs(t, funcDef{params, []wasm.ValType{wasm.I32}, caller}, funcDef{params, params, callee})
	for n := uint64(0); n < 9; n++ {
		var stats ModuleStats
		opts := CompileOptions{Workers: 1}
		if diagnosticsEnabled {
			opts.Stats = &stats
		}
		got, err := runArm64WrapperWithOptions(t, m, opts, n, 7)
		if err != nil || got != 7+3*n {
			t.Fatalf("n=%d got=%d err=%v", n, got, err)
		}
		if diagnosticsEnabled && !profileEnabled && stats.Funcs[0].Peephole["inline-tuple-result-homes"] == 0 {
			t.Fatal("inlined tuple outputs still require private-to-caller transfers")
		}
	}
}

func TestInlineTupleResultHomesPreserveAliasesAndRebinding(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, add, mul, eqz, rem := wasm.I32, byte(0x7f), byte(0x41), byte(0x6a), byte(0x6c), byte(0x45), byte(0x70)
		fold := func(v uint64) uint64 { return uint64(uint32(v)) }
		if wide {
			typ, bt, k, add, mul, eqz, rem = wasm.I64, 0x7e, 0x42, 0x7c, 0x7e, 0x50, 0x82
			fold = func(v uint64) uint64 { return v }
		}
		callee := []byte{1, 2, bt, 0x02, 0x40, 0x03, 0x40, 0x20, 1, eqz, 0x0d, 1,
			0x20, 0, 0x20, 1, rem, 0x21, 3, 0x20, 1, 0x21, 0, 0x20, 3, 0x21, 1,
			0x20, 2, k, 1, add, 0x21, 2, 0x0c, 0, 0x0b, 0x0b, 0x20, 0, 0x20, 2, 0x0b}
		for _, variant := range []string{"prefix", "target-arguments", "swapped-targets", "two-sites", "compact", "caller-param-only", "interruptible"} {
			args := [2]byte{0, 1}
			if variant == "target-arguments" {
				args = [2]byte{2, 3}
			}
			if variant == "swapped-targets" {
				args = [2]byte{3, 2}
			}
			caller := []byte{1, 1, bt, 0x20, 2, 0x20, 3, add, 0x20, args[0], 0x20, args[1], 0x10, 1, 0x21, 3, 0x21, 2,
				0x20, 2, k, 17, mul, 0x20, 3, add, add}
			if variant == "two-sites" {
				caller = append(caller, 0x20, 1, 0x20, 0, 0x10, 1, 0x21, 1, 0x21, 0, 0x20, 0, k, 17, mul, 0x20, 1, add, add)
			}
			if variant == "caller-param-only" {
				caller = append([]byte{0}, caller[3:]...)
			}
			caller = append(caller, 0x0b)
			m := modFuncs(t, funcDef{[]wasm.ValType{typ, typ, typ, typ}, []wasm.ValType{typ}, caller}, funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ, typ}, callee})
			gcdScore := func(a, b uint64) uint64 {
				steps := uint64(0)
				for b != 0 {
					a, b = b, a%b
					steps++
				}
				return fold(a*17 + steps)
			}
			for _, enabled := range []bool{false, true} {
				for _, workers := range []int{1, 4} {
					for _, input := range [][4]uint64{{0, 0, 5, 7}, {1, 0, 0, 0}, {0, 7, 13, 8}, {13, 8, 21, 13}, {55, 34, 89, 55}, {^uint64(0), 17, 0x8000000000000001, 29}} {
						var values [4]uint64
						for i, v := range input {
							values[i] = fold(v)
						}
						want := fold(values[2] + values[3] + gcdScore(values[args[0]], values[args[1]]))
						if variant == "two-sites" {
							want = fold(want + gcdScore(values[1], values[0]))
						}
						var stats ModuleStats
						opts := CompileOptions{Workers: workers, CompactNative: variant == "compact", Interruptible: variant == "interruptible", Optimizations: map[string]bool{"inline-integer-tuples": enabled}}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						got, err := runArm64WrapperWithOptions(t, m, opts, input[0], input[1], input[2], input[3])
						if err != nil || fold(got) != want {
							t.Fatalf("wide=%t variant=%s enabled=%t workers=%d input=%x got=%x err=%v want=%x", wide, variant, enabled, workers, input, got, err, want)
						}
						if diagnosticsEnabled {
							count := 0
							if enabled && !profileEnabled && variant != "compact" && variant != "caller-param-only" && variant != "interruptible" {
								count = 1
								if variant == "two-sites" {
									count = 2
								}
							}
							if got := stats.Funcs[0].Peephole["inline-tuple-result-homes"]; got != count {
								t.Fatalf("wide=%t variant=%s enabled=%t expected %d coalesced sites got %d: %v", wide, variant, enabled, count, got, stats.Funcs[0].Peephole)
							}
						}
					}
				}
			}
		}
	}
}
