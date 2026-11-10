// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
)

func TestInlineTupleFallthroughKeepsResultsAndRejectsBoundaryEdges(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, add, mul, div := wasm.I32, byte(0x7f), byte(0x41), byte(0x6a), byte(0x6c), byte(0x6e)
		fold := func(x uint64) uint64 { return uint64(uint32(x)) }
		if wide {
			typ, bt, k, add, mul, div = wasm.I64, 0x7e, 0x42, 0x7c, 0x7e, 0x80
			fold = func(x uint64) uint64 { return x }
		}
		for _, variant := range []string{"fallthrough", "inner-branch", "boundary-br", "nested-boundary-br", "root-br", "return", "mutate", "division", "drop-result", "caller-alias", "same-destination", "one-store", "unreachable", "compact"} {
			// A real loop keeps this in the bounded control-tuple class. Caller
			// local0 remains live below the call, independently of private homes.
			helper := []byte{0, 0x03, 0x40, 0x0b, 0x20, 0, 0x20, 1, 0x0b}
			switch variant {
			case "inner-branch":
				helper = []byte{0, 0x03, 0x40, 0x02, 0x40, 0x20, 2, 0x0d, 0, 0x01, 0x0b, 0x0b, 0x20, 0, 0x20, 1, 0x0b}
			case "boundary-br", "nested-boundary-br":
				helper = []byte{0, 0x03, 0x40}
				label := byte(1)
				if variant == "nested-boundary-br" {
					helper = append(helper, 0x02, 0x40)
					label = 2
				}
				helper = append(helper, 0x20, 0, 0x20, 1, 0x20, 2, 0x0d, label, 0x1a, 0x1a, 0x0b)
				if variant == "nested-boundary-br" {
					helper = append(helper, 0x0b)
				}
				helper = append(helper, 0x20, 1, 0x20, 0, 0x0b)
			case "root-br", "return":
				helper = helper[:len(helper)-1]
				if variant == "root-br" {
					helper = append(helper, 0x0c, 0)
				} else {
					helper = append(helper, 0x0f)
				}
				helper = append(helper, 0x0b)
			case "mutate":
				helper = append([]byte{0, 0x20, 0, k, 1, add, 0x21, 0}, helper[1:]...)
			case "division", "drop-result":
				helper = []byte{0, 0x03, 0x40, 0x0b, 0x20, 0, 0x20, 1, div, 0x20, 1, 0x0b}
			case "unreachable":
				helper = []byte{0, 0x03, 0x40, 0x0b, 0x00, 0x0b}
			}
			low, high := byte(3), byte(4)
			if variant == "caller-alias" {
				low, high = 0, 1
			} else if variant == "same-destination" {
				high = low
			}
			caller := []byte{1, 2, bt, 0x20, 0, 0x20, 0, 0x20, 1, 0x20, 2, 0x10, 1, 0x21, high, 0x21, low, 0x20, low, k, 3, mul, 0x20, high, k, 5, mul, add, add, 0x0b}
			if variant == "one-store" {
				caller = []byte{1, 2, bt, 0x20, 0, 0x20, 0, 0x20, 1, 0x20, 2, 0x10, 1, 0x21, 4, k, 3, mul, 0x20, 4, k, 5, mul, add, add, 0x0b}
			} else if variant == "drop-result" {
				caller = []byte{0, 0x20, 0, 0x20, 0, 0x20, 1, 0x20, 2, 0x10, 1, 0x1a, 0x1a, k, 7, add, 0x0b}
			}
			params := []wasm.ValType{typ, typ, wasm.I32}
			m := modFuncs(t, funcDef{params, []wasm.ValType{typ}, caller}, funcDef{params, []wasm.ValType{typ, typ}, helper})
			for _, enabled := range []bool{false, true} {
				for _, workers := range []int{1, 4} {
					for _, flag := range []uint64{0, 1, 0xdeadbeef00000000, ^uint64(0)} {
						for _, input := range [][2]uint64{{7, 3}, {^uint64(0), 0x8000000080000001}, {17, 0}} {
							a, b := fold(input[0]), fold(input[1])
							first, second := a, b
							if (variant == "boundary-br" || variant == "nested-boundary-br") && uint32(flag) == 0 {
								first, second = b, a
							}
							if variant == "mutate" {
								first = fold(a + 1)
							}
							if (variant == "division" || variant == "drop-result") && b != 0 {
								first = a / b
							}
							if variant == "same-destination" {
								second = first
							}
							var stats ModuleStats
							opts := CompileOptions{Workers: workers, CompactNative: variant == "compact", Optimizations: map[string]bool{"inline-integer-tuples": enabled}}
							if diagnosticsEnabled {
								opts.Stats = &stats
							}
							got, err := runArm64WrapperWithOptions(t, m, opts, input[0], input[1], flag)
							want := fold(a + 3*first + 5*second)
							if variant == "drop-result" {
								want = fold(a + 7)
							}
							if variant == "unreachable" || (variant == "division" || variant == "drop-result") && b == 0 {
								code := rt.TrapUnreachable
								if variant != "unreachable" {
									code = rt.TrapDivZero
								}
								var trap *rt.TrapError
								if !errors.As(err, &trap) || trap.Code != code {
									t.Fatalf("variant=%s got trap %v, want %v", variant, err, code)
								}
							} else if err != nil || fold(got) != want {
								t.Fatalf("wide=%t variant=%s enabled=%t workers=%d input=%x flag=%x got=%x err=%v want=%x", wide, variant, enabled, workers, input, flag, got, err, want)
							}
							positive := variant == "fallthrough" || variant == "inner-branch" || variant == "mutate" || variant == "division" || variant == "drop-result" || variant == "caller-alias" || variant == "same-destination" || variant == "one-store" || variant == "unreachable"
							if diagnosticsEnabled && (stats.Funcs[0].Peephole["inline-tuple-fallthrough"] != 0) != (enabled && positive && !profileEnabled) {
								t.Fatalf("fallthrough proof variant=%s enabled=%t stats=%v", variant, enabled, stats.Funcs[0].Peephole)
							}
							if diagnosticsEnabled && variant == "fallthrough" && (stats.Funcs[0].Peephole["inline-tuple-readonly-prefix"] != 0) != (enabled && !profileEnabled) {
								t.Fatalf("readonly prefix enabled=%v stats=%v", enabled, stats.Funcs[0].Peephole)
							}
							stores := positive && variant != "drop-result" && variant != "one-store" && variant != "unreachable"
							if diagnosticsEnabled && (stats.Funcs[0].Peephole["inline-tuple-result-stores"] != 0) != (enabled && stores && !profileEnabled) {
								t.Fatalf("tuple store path variant=%s enabled=%t stats=%v", variant, enabled, stats.Funcs[0].Peephole)
							}
						}
					}
				}
			}
		}
	}
}

func TestInlineIntegerTupleArgumentsPreserveTrapOrder(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, divS, divU, eqz, sub, add := wasm.I32, byte(0x7f), byte(0x41), byte(0x6d), byte(0x6e), byte(0x45), byte(0x6b), byte(0x6a)
		minimum := uint64(0x80000000)
		if wide {
			typ, bt, k, divS, divU, eqz, sub, add = wasm.I64, 0x7e, 0x42, 0x7f, 0x80, 0x50, 0x7d, 0x7c
			minimum = 0x8000000000000000
		}
		caller := []byte{1, 1, bt, 0x20, 0, 0x20, 1, divS, 0x20, 0, 0x20, 2, divU,
			0x10, 1, add, 0x21, 3, 0x20, 3, 0x0b}
		helper := []byte{0, 0x02, 0x40, 0x03, 0x40, 0x20, 0, eqz, 0x0d, 1,
			0x20, 0, k, 1, sub, 0x21, 0, 0x20, 1, k, 1, add, 0x21, 1,
			0x0c, 0, 0x0b, 0x0b, 0x20, 0, 0x20, 1, 0x0b}
		m := modFuncs(t,
			funcDef{[]wasm.ValType{typ, typ, typ}, []wasm.ValType{typ}, caller},
			funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ, typ}, helper})
		for _, enabled := range []bool{false, true} {
			for _, workers := range []int{1, 4} {
				var stats ModuleStats
				opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"inline-integer-tuples": enabled}}
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				got, err := runArm64WrapperWithOptions(t, m, opts, 17, 5, 3)
				if err != nil || got != 8 {
					t.Fatalf("wide=%t enabled=%t workers=%d got=%d err=%v", wide, enabled, workers, got, err)
				}
				if diagnosticsEnabled && (stats.Funcs[0].Calls[callKindInline] == 1) != (enabled && !profileEnabled) {
					t.Fatal("did not exercise expanded tuple inlining")
				}
				if diagnosticsEnabled && (stats.Funcs[0].Peephole["inline-tuple-fallthrough"] == 1) != (enabled && !profileEnabled) {
					t.Fatalf("fallthrough tuple path mismatch: enabled=%t stats=%v", enabled, stats.Funcs[0].Peephole)
				}
				for _, tc := range []struct {
					args [3]uint64
					code rt.TrapCode
				}{{[3]uint64{minimum, ^uint64(0), 0}, rt.TrapDivOverflow}, {[3]uint64{17, 5, 0}, rt.TrapDivZero}} {
					_, err := runArm64WrapperWithOptions(t, m, opts, tc.args[:]...)
					var trap *rt.TrapError
					if !errors.As(err, &trap) || trap.Code != tc.code {
						t.Fatalf("wide=%t enabled=%t workers=%d args=%x err=%v want trap=%v", wide, enabled, workers, tc.args, err, tc.code)
					}
				}
			}
		}
	}
}

func TestInlineIntegerTupleLoopRebindsAndZerosPrivateLocals(t *testing.T) {
	savedLimit := inlineTupleMaxBytes
	t.Cleanup(func() { inlineTupleMaxBytes = savedLimit })
	for _, wide := range []bool{false, true} {
		typ, bt, k, eqz, sub, add := wasm.I32, byte(0x7f), byte(0x41), byte(0x45), byte(0x6b), byte(0x6a)
		fold := func(v uint64) uint64 { return uint64(uint32(v)) }
		if wide {
			typ, bt, k, eqz, sub, add = wasm.I64, 0x7e, 0x42, 0x50, 0x7d, 0x7c
			fold = func(v uint64) uint64 { return v }
		}
		for _, variant := range []string{"call-free", "call-free-disabled", "remaining-call", "option-disabled", "body-limit", "float-local", "many-locals", "large-body"} {
			inlineTupleMaxBytes = savedLimit
			if variant == "body-limit" {
				inlineTupleMaxBytes = min(16, savedLimit)
			}
			caller := []byte{1, 1, bt, 0x20, 0, 0x20, 1, 0x20, 2, 0x10, 1, add, 0x21, 3,
				0x20, 0, 0x20, 1, 0x20, 2, 0x10, 1, add, 0x20, 3, add, 0x0b}
			// Private local5 counts iterations from zero on EACH splice. The loop
			// rotates two other private homes; both original caller arguments survive.
			helper := []byte{1, 3, bt, 0x20, 0, 0x20, 1, 0x20, 2, 0x03, 1,
				0x21, 4, 0x21, 3, 0x21, 0, 0x20, 0, eqz, 0x04, 2,
				0x20, 3, 0x20, 4, 0x20, 5, add, 0x05,
				0x20, 5, k, 1, add, 0x21, 5,
				0x20, 0, k, 1, sub, 0x20, 4, 0x20, 3, 0x20, 4, add, 0x0c, 1,
				0x0b, 0x0b, 0x0b}
			switch variant {
			case "float-local":
				helper = append([]byte{2, 3, bt, 1, 0x7c}, helper[3:]...)
			case "many-locals":
				helper[1] = 5
			case "large-body":
				extra := make([]byte, 30)
				for i := range extra {
					extra[i] = 0x01
				}
				helper = append(append(append([]byte{}, helper[:3]...), extra...), helper[3:]...)
			}
			if variant == "remaining-call" {
				caller = append(append(append([]byte{}, caller[:14]...), 0x10, 3), caller[14:]...)
			}
			opaque := []byte{0}
			for range 20 {
				opaque = append(opaque, 0x01)
			}
			opaque = append(opaque, 0x0b)
			m := modFuncs(t,
				funcDef{[]wasm.ValType{typ, typ, typ}, []wasm.ValType{typ}, caller},
				funcDef{[]wasm.ValType{typ, typ, typ}, []wasm.ValType{typ, typ}, helper},
				funcDef{nil, []wasm.ValType{typ, typ}, []byte{0, k, 0, k, 0, 0x0b}}, funcDef{nil, nil, opaque})
			for _, enabled := range []bool{false, true} {
				for _, workers := range []int{1, 4} {
					for _, count := range []uint64{0, 1, 2, 7, 12} {
						for _, input := range [][2]uint64{{3, 7}, {0xfffffff0, 0x80000001}, {^uint64(0), 0x8000000000000001}} {
							a, b := fold(input[0]), fold(input[1])
							for range count {
								a, b = b, fold(a+b)
							}
							want := fold(2 * fold(a+b+count))
							var stats ModuleStats
							opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"inline": enabled, "inline-callfree": variant != "call-free-disabled", "inline-integer-tuples": variant != "option-disabled"}}
							if diagnosticsEnabled {
								opts.Stats = &stats
							}
							got, err := runArm64WrapperWithOptions(t, m, opts, count, input[0], input[1])
							got = fold(got)
							if err != nil || got != want {
								t.Fatalf("wide=%t inline=%t workers=%d count=%d input=%x got=%x err=%v want=%x", wide, enabled, workers, count, input, got, err, want)
							}
							if diagnosticsEnabled && (stats.Funcs[0].Calls[callKindInline] == 2) != (enabled && !profileEnabled && variant == "call-free") {
								t.Fatalf("inline=%t calls=%v", enabled, stats.Funcs[0].Calls)
							}
							if diagnosticsEnabled && (stats.Funcs[0].Peephole["loop-local-params"] == 2) != (enabled && !profileEnabled && variant == "call-free") {
								t.Fatalf("inline=%t retained parameter loops=%d", enabled, stats.Funcs[0].Peephole["loop-local-params"])
							}
						}
					}
				}
			}
		}
	}
}
