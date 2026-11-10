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

func TestTailPairInlinePermutationsAndFunctionLabels(t *testing.T) {
	if profileEnabled {
		t.Skip("tail inlining excludes profiling")
	}
	for _, wide := range []bool{false, true} {
		typ, k, add, mul, xor := wasm.I32, byte(0x41), byte(0x6a), byte(0x6c), byte(0x73)
		if wide {
			typ, k, add, mul, xor = wasm.I64, 0x42, 0x7c, 0x7e, 0x85
		}
		fold := func(n uint64) uint64 {
			if !wide {
				return uint64(uint32(n))
			}
			return n
		}
		params, results := []wasm.ValType{wasm.I32, typ, typ}, []wasm.ValType{typ}
		first := []byte{0, 0x20, 0, 0x45, 0x04, 0x40, 0x20, 1, k, 3, mul, 0x20, 2, add, 0x0c, 1, 0x0b,
			0x20, 0, 0x41, 1, 0x6b, 0x20, 2, 0x20, 1, k, 7, add, 0x12, 2, 0x0b}
		second := []byte{0, 0x20, 0, 0x45, 0x04, 0x40, 0x20, 1, k, 5, mul, 0x20, 2, add, 0x0c, 1, 0x0b,
			0x20, 0, 0x41, 1, 0x6b, 0x20, 1, 0x20, 2, add, 0x20, 1, 0x20, 2, xor, 0x12, 1, 0x0b}
		for _, constant := range []bool{false, true} {
			wrapper := []byte{0, 0x20, 0, 0x20, 1}
			if constant {
				wrapper = append(wrapper, k, 7)
			} else {
				wrapper = append(wrapper, 0x20, 2)
			}
			wrapper = append(wrapper, 0x10, 1, 0x0b)
			m := modFuncs(t, funcDef{params, results, wrapper}, funcDef{params, results, first}, funcDef{params, results, second})
			for _, workers := range []int{1, 4} {
				for _, enabled := range []bool{false, true} {
					var stats ModuleStats
					c, err := CompileModuleWith(m, CompileOptions{Workers: workers, Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"tail-loop-inline": enabled}})
					if err != nil {
						t.Fatal(err)
					}
					for _, n := range []uint64{0, 1, 2, 3, 31, 100000} {
						a, b := fold(0xfffffffffffffff0), fold(0x80000000cafebabe)
						if constant {
							b = 7
						}
						aa, bb := a, b
						if !wide {
							aa |= 0xdeadbeef00000000
							bb |= 0xcafebabe00000000
						}
						for i := uint64(0); i < n; i++ {
							if i&1 == 0 {
								a, b = b, fold(a+7)
							} else {
								a, b = fold(a+b), a^b
							}
						}
						weight := uint64(3)
						if n&1 != 0 {
							weight = 5
						}
						want := fold(weight*a + b)
						if got := runCompiledAmd64u(t, c, n|0xdeadbeef00000000, aa, bb); got != want {
							t.Fatalf("wide=%t constant=%t workers=%d enabled=%t n=%d got=%x want=%x", wide, constant, workers, enabled, n, got, want)
						}
					}
					if c.CodeImage != nil {
						c.CodeImage.Close()
					}
					if diagnosticsEnabled && (stats.Funcs[0].Peephole["inline-tail-pair"] != 0) != enabled {
						t.Fatal("proved two-function cycle was not inlined")
					}
					if diagnosticsEnabled && (stats.Funcs[0].Peephole["inline-tail-argument-flags"] != 0) != enabled {
						t.Fatal("tail argument decrement still repeats its zero test")
					}

				}
			}
		}
	}
}

func TestTailPairInlineCallerResultConversion(t *testing.T) {
	if profileEnabled {
		t.Skip("tail inlining excludes profiling")
	}
	params, results := []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}
	body := func(value, target byte) []byte {
		return []byte{0, 0x20, 0, 0x45, 0x04, 0x7f, 0x41, value, 0x05, 0x20, 0, 0x41, 1, 0x6b, 0x12, target, 0x0b, 0x0b}
	}
	m := modFuncs(t, funcDef{params, []wasm.ValType{wasm.I64}, []byte{0, 0x20, 0, 0x10, 1, 0xad, 0x42, 31, 0x7c, 0x0b}}, funcDef{params, results, body(5, 2)}, funcDef{params, results, body(9, 1)})
	var stats ModuleStats
	c, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats)})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CodeImage.Close()
	for _, n := range []uint64{0, 1, 2, 31, 100000} {
		want := uint64(36)
		if n&1 != 0 {
			want = 40
		}
		if got := runCompiledAmd64u(t, c, n); got != want {
			t.Fatalf("n=%d got=%d want=%d", n, got, want)
		}
	}
	if diagnosticsEnabled && stats.Funcs[0].Peephole["inline-tail-pair"] == 0 {
		t.Fatal("caller conversion did not exercise pair inlining")
	}
}

func TestTailPairInlineKeepsOtherGraphsOnFallback(t *testing.T) {
	if profileEnabled {
		t.Skip("tail inlining excludes profiling")
	}
	params, results := []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}
	member := func(value, target byte) []byte {
		return []byte{0, 0x20, 0, 0x45, 0x04, 0x7f, 0x41, value, 0x05, 0x20, 0, 0x41, 1, 0x6b, 0x12, target, 0x0b, 0x0b}
	}
	wrapper := funcDef{params, results, []byte{0, 0x20, 0, 0x10, 1, 0x0b}}
	for _, kind := range []string{"three-member-cycle", "different-parameters", "declared-local", "ordinary-call", "explicit-loop", "large-body", "three-tail-sites"} {
		t.Run(kind, func(t *testing.T) {
			a, b := member(5, 2), member(9, 1)
			defs := []funcDef{wrapper, {params, results, a}, {params, results, b}}
			want := func(n uint64) uint64 {
				if n&1 != 0 {
					return 9
				}
				return 5
			}
			switch kind {
			case "three-member-cycle":
				defs[2].body = member(9, 3)
				defs = append(defs, funcDef{params, results, member(13, 1)})
				want = func(n uint64) uint64 { return []uint64{5, 9, 13}[n%3] }
			case "different-parameters":
				defs[1].body = []byte{0, 0x20, 0, 0x45, 0x04, 0x7f, 0x41, 5, 0x05, 0x20, 0, 0x41, 1, 0x6b, 0x41, 7, 0x12, 2, 0x0b, 0x0b}
				defs[2] = funcDef{[]wasm.ValType{wasm.I32, wasm.I32}, results, []byte{0, 0x20, 0, 0x45, 0x04, 0x7f, 0x20, 1, 0x05, 0x20, 0, 0x41, 1, 0x6b, 0x12, 1, 0x0b, 0x0b}}
				want = func(n uint64) uint64 {
					if n&1 != 0 {
						return 7
					}
					return 5
				}
			case "declared-local":
				defs[2].body = []byte{1, 1, 0x7f, 0x20, 0, 0x45, 0x04, 0x7f, 0x20, 1, 0x41, 9, 0x6a, 0x05, 0x41, 13, 0x21, 1, 0x20, 0, 0x41, 1, 0x6b, 0x12, 1, 0x0b, 0x0b}
			case "ordinary-call":
				defs[2].body = append([]byte{0, 0x20, 0, 0x10, 3, 0x1a}, b[1:]...)
				defs = append(defs, funcDef{params, results, []byte{0, 0x20, 0, 0x41, 1, 0x6a, 0x0b}})
			case "explicit-loop":
				defs[2].body = append([]byte{0, 0x02, 0x40, 0x03, 0x40, 0x0c, 1, 0x0b, 0x0b}, b[1:]...)
			case "large-body":
				defs[2].body = append([]byte{0}, make([]byte, 64)...)
				for i := 1; i < len(defs[2].body); i++ {
					defs[2].body[i] = 0x01
				}
				defs[2].body = append(defs[2].body, b[1:]...)
			case "three-tail-sites":
				defs[1].body = []byte{0, 0x20, 0, 0x45, 0x04, 0x7f, 0x41, 5, 0x05, 0x20, 0, 0x41, 1, 0x71, 0x04, 0x7f,
					0x20, 0, 0x41, 1, 0x6b, 0x12, 2, 0x05, 0x20, 0, 0x41, 2, 0x71, 0x04, 0x7f,
					0x20, 0, 0x41, 1, 0x6b, 0x12, 2, 0x05, 0x20, 0, 0x41, 1, 0x6b, 0x12, 2, 0x0b, 0x0b, 0x0b, 0x0b}
			}
			m := modFuncs(t, defs...)
			var stats ModuleStats
			c, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats)})
			if err != nil {
				t.Fatal(err)
			}
			defer c.CodeImage.Close()
			for _, n := range []uint64{0, 1, 2, 3, 31, 1000} {
				if got := runCompiledAmd64u(t, c, n); got != want(n) {
					t.Fatalf("n=%d got=%d want=%d", n, got, want(n))
				}
			}
			if diagnosticsEnabled && stats.Funcs[0].Peephole["inline-tail-pair"] != 0 {
				t.Fatal("unproved graph was pair-inlined")
			}
		})
	}
}

func TestTailPairArgumentFlagsKeepOtherOperandTests(t *testing.T) {
	if profileEnabled {
		t.Skip("tail inlining excludes profiling")
	}
	params, results := []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}
	root := []byte{0, 0x20, 0, 0x45, 0x04, 0x7f, 0x41, 5, 0x05, 0x20, 0, 0x41, 1, 0x6b, 0x20, 1, 0x12, 2, 0x0b, 0x0b}
	partner := []byte{0, 0x20, 1, 0x45, 0x04, 0x7f, 0x41, 9, 0x05, 0x20, 0, 0x45, 0x04, 0x7f, 0x41, 11, 0x05, 0x20, 0, 0x41, 1, 0x6b, 0x20, 1, 0x12, 1, 0x0b, 0x0b, 0x0b}
	m := modFuncs(t, funcDef{params, results, []byte{0, 0x20, 0, 0x20, 1, 0x10, 1, 0x0b}}, funcDef{params, results, root}, funcDef{params, results, partner})
	var stats ModuleStats
	c, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats)})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CodeImage.Close()
	for _, n := range []uint64{0, 1, 2, 3, 31, 1000} {
		for _, value := range []uint64{0, 1} {
			want := uint64(5)
			if n != 0 && value == 0 {
				want = 9
			} else if n&1 != 0 {
				want = 11
			}
			if got := runCompiledAmd64u(t, c, n, value); got != want {
				t.Fatalf("n=%d value=%d got=%d want=%d", n, value, got, want)
			}
		}
	}
	if diagnosticsEnabled && (stats.Funcs[0].Peephole["inline-tail-pair"] == 0 || stats.Funcs[0].Peephole["inline-tail-argument-flags"] != 0) {
		t.Fatal("other operand must retain its test in an inlined pair")
	}
}

func TestTailPairArgumentFlagsKeepNarrowedOperandTests(t *testing.T) {
	if profileEnabled {
		t.Skip("tail inlining excludes profiling")
	}
	params, results := []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}
	root := []byte{0, 0x20, 0, 0x50, 0x04, 0x7f, 0x41, 5, 0x05, 0x20, 0, 0x42, 1, 0x7d, 0x12, 2, 0x0b, 0x0b}
	partner := []byte{0, 0x20, 0, 0xa7, 0x45, 0x04, 0x7f, 0x41, 9, 0x05, 0x20, 0, 0x42, 1, 0x7d, 0x12, 1, 0x0b, 0x0b}
	m := modFuncs(t, funcDef{params, results, []byte{0, 0x20, 0, 0x10, 1, 0x0b}}, funcDef{params, results, root}, funcDef{params, results, partner})
	var stats ModuleStats
	c, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats)})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CodeImage.Close()
	for _, test := range []struct{ n, want uint64 }{{0, 5}, {1, 9}, {2, 5}, {3, 9}, {0x100000001, 9}} {
		if got := runCompiledAmd64u(t, c, test.n); got != test.want {
			t.Fatalf("n=%x got=%d want=%d", test.n, got, test.want)
		}
	}
	if diagnosticsEnabled && (stats.Funcs[0].Peephole["inline-tail-pair"] == 0 || stats.Funcs[0].Peephole["inline-tail-argument-flags"] != 0) {
		t.Fatal("narrowed operand must retain its test in an inlined pair")
	}
}
