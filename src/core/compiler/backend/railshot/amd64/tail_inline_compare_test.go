// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestInlineTailConstantLeftCompareLanesAndInversions(t *testing.T) {
	if profileEnabled {
		t.Skip("tail inlining excludes profiling")
	}
	for _, wide := range []bool{false, true} {
		typ, k, first := wasm.I32, byte(0x41), byte(0x46)
		if wide {
			typ, k, first = wasm.I64, 0x42, 0x51
		}
		for index := 0; index < 10; index++ {
			for _, invert := range []bool{false, true} {
				body := []byte{0, 0x20, 0, 0x45, 0x04, 0x40, 0x41, 5, 0x0f, 0x0b, 0x20, 1, 0x20, 2, first + byte(index)}
				if invert {
					body = append(body, 0x45)
				}
				body = append(body, 0x04, 0x40, 0x41, 11, 0x0f, 0x0b, 0x20, 0, 0x41, 1, 0x6b, 0x20, 1, 0x20, 2, 0x12, 1, 0x0b)
				results := []wasm.ValType{wasm.I32}
				m := modFuncs(t, funcDef{[]wasm.ValType{wasm.I32, typ}, results, []byte{0, 0x20, 0, k, 7, 0x20, 1, 0x10, 1, 0x0b}}, funcDef{[]wasm.ValType{wasm.I32, typ, typ}, results, body})
				var stats ModuleStats
				c, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats)})
				if err != nil {
					t.Fatal(err)
				}
				for _, x := range []uint64{0, 6, 7, 8, 0x7fffffff, 0x80000000, 0xffffffff, 0x7fffffffffffffff, 0x8000000000000000, ^uint64(0)} {
					ux, sx := x, int64(x)
					input := x
					if !wide {
						ux = uint64(uint32(x))
						sx = int64(int32(uint32(x)))
						input = ux | 0xdeadbeef00000000
					}
					predicates := [10]bool{ux == 7, ux != 7, 7 < sx, 7 < ux, 7 > sx, 7 > ux, 7 <= sx, 7 <= ux, 7 >= sx, 7 >= ux}
					predicate := predicates[index]
					if invert {
						predicate = !predicate
					}
					for _, n := range []uint64{0, 1, 31} {
						want := uint64(5)
						if n > 0 && predicate {
							want = 11
						}
						if got := runCompiledAmd64u(t, c, n, input); got != want {
							t.Fatalf("wide=%t op=%x invert=%t n=%d x=%x got=%d want=%d", wide, first+byte(index), invert, n, x, got, want)
						}
					}
				}
				if c.CodeImage != nil {
					c.CodeImage.Close()
				}
				if diagnosticsEnabled && stats.Funcs[0].Peephole["inline-tail-compare-immediate"] == 0 {
					t.Fatal("constant-left compare did not use the immediate form")
				}
			}
		}
	}
}

func TestInlineTailCompareReuseRejectsRejoiningTrueEdges(t *testing.T) {
	if profileEnabled {
		t.Skip("tail inlining excludes profiling")
	}
	for _, branch := range []bool{false, true} {
		body := []byte{0, 0x20, 0, 0x45, 0x04, 0x40, 0x41, 5, 0x0f, 0x0b,
			0x20, 1, 0x20, 2, 0x46, 0x04, 0x40, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2}
		if branch {
			body = append(body, 0x0c, 0)
		}
		body = append(body, 0x0b, 0x20, 1, 0x20, 2, 0x49, 0x04, 0x40, 0x41, 11, 0x0f, 0x0b,
			0x20, 0, 0x41, 1, 0x6b, 0x20, 1, 0x20, 2, 0x12, 1, 0x0b)
		results := []wasm.ValType{wasm.I32}
		m := modFuncs(t, funcDef{[]wasm.ValType{wasm.I32, wasm.I32}, results, []byte{0, 0x20, 0, 0x41, 7, 0x20, 1, 0x10, 1, 0x0b}}, funcDef{[]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, results, body})
		var stats ModuleStats
		c, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats)})
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range []uint64{0, 1, 2, 31} {
			for _, x := range []uint64{0, 6, 7, 8, 0xffffffff} {
				want := uint64(5)
				if n > 0 && (x == 7 || x > 7) {
					want = 11
				}
				if got := runCompiledAmd64u(t, c, n, x); got != want {
					t.Fatalf("branch=%t n=%d x=%x got=%d want=%d", branch, n, x, got, want)
				}
			}
		}
		if c.CodeImage != nil {
			c.CodeImage.Close()
		}
		if diagnosticsEnabled && stats.Funcs[0].Peephole["inline-tail-compare-reuse"] != 0 {
			t.Fatal("rejoining or mutating true edge reused stale comparison flags")
		}
	}
}
