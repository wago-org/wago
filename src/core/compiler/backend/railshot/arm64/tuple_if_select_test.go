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
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
	"testing"
)

func TestTupleIfSelectStoreProofRejectsOverlappingHomes(t *testing.T) {
	for _, variant := range []string{"valid", "source-overlap", "destination-overlap", "float", "unpinned", "wrong-type", "missing-local"} {
		f := gpClassEmitter()
		f.localType = []machineType{mtI32, mtI64}
		f.locals = []localDef{{reg: X0}, {reg: X1}}
		locals := [2]int{0, 1}
		plan := tupleIfSelectPlan{sources: [2][2]Reg{{X2, X3}, {X3, X2}}, types: [2]machineType{mtI32, mtI64}}
		switch variant {
		case "source-overlap":
			f.locals[0].reg = X2
		case "destination-overlap":
			f.locals[1].reg = X0
		case "float":
			f.locals[1].isFloat = true
		case "unpinned":
			f.locals[1].reg = regNone
		case "wrong-type":
			f.localType[1] = mtI32
		case "missing-local":
			locals[1] = 2
		}
		f.planTupleSelectStores(&plan, locals)
		if plan.direct != (variant == "valid") {
			t.Fatalf("variant=%s direct=%t", variant, plan.direct)
		}
	}
}

func TestTupleIfSelectPreservesPairAndPrefix(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, add, mul, le := wasm.I32, byte(0x7f), byte(0x41), byte(0x6a), byte(0x6c), byte(0x4d)
		fold := func(v uint64) uint64 { return uint64(uint32(v)) }
		if wide {
			typ, bt, k, add, mul, le = wasm.I64, 0x7e, 0x42, 0x7c, 0x7e, 0x58
			fold = func(v uint64) uint64 { return v }
		}
		caller := []byte{1, 2, bt, 0x20, 2, 0x02, bt, 0x20, 0, 0x20, 1, 0x10, 1, 0x21, 4, 0x21, 3, 0x20, 3, k, 3, mul, 0x20, 4, k, 5, mul, add, 0x0b, add, 0x0b}
		for _, withLoop := range []bool{false, true} {
			helper := []byte{0, 0x20, 0, 0x20, 1, le, 0x04, 2, 0x20, 0, 0x20, 1, 0x05, 0x20, 1, 0x20, 0, 0x0b, 0x0b}
			if withLoop {
				helper = append(append([]byte{0, 0x03, 2}, helper[1:len(helper)-1]...), 0x0b, 0x0b)
			}
			m := modFuncs(t, funcDef{[]wasm.ValType{typ, typ, typ}, []wasm.ValType{typ}, caller}, funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ, typ}, helper}, funcDef{nil, []wasm.ValType{typ, typ}, []byte{0, k, 0, k, 0, 0x0b}})
			values := []uint64{0, 1, 0x8000000000000000, 0xdeadbeef80000000, ^uint64(0)}
			for _, enabled := range []bool{false, true} {
				for _, inline := range []bool{false, true} {
					for _, workers := range []int{1, 4} {
						for _, a := range values {
							for _, b := range values {
								lo, hi := fold(a), fold(b)
								if lo > hi {
									lo, hi = hi, lo
								}
								var stats ModuleStats
								opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"tuple-if-select": enabled, "inline": inline}}
								if diagnosticsEnabled {
									opts.Stats = &stats
								}
								got, err := runArm64WrapperWithOptions(t, m, opts, a, b, 0x123456789abcdef0)
								want := fold(3*lo + 5*hi + 0x123456789abcdef0)
								if err != nil || fold(got) != want {
									t.Fatalf("wide=%t enabled=%t inline=%t workers=%d a=%x b=%x got=%x err=%v want=%x", wide, enabled, inline, workers, a, b, got, err, want)
								}
								if diagnosticsEnabled {
									hit := false
									for _, s := range stats.Funcs {
										hit = hit || s.Peephole["tuple-if-select"] != 0
									}
									if (stats.Funcs[0].Peephole["inline-tuple-if-select"] != 0) != (enabled && inline && !withLoop && !profileEnabled) {
										t.Fatalf("inline path mismatch enabled=%t inline=%t loop=%t stats=%v", enabled, inline, withLoop, stats.Funcs[0].Peephole)
									}
									if (stats.Funcs[0].Peephole["tuple-if-select-local-stores"] != 0) != (enabled && inline && !withLoop && !profileEnabled) {
										t.Fatalf("direct pair store path mismatch enabled=%t inline=%t loop=%t stats=%v", enabled, inline, withLoop, stats.Funcs[0].Peephole)
									}
									if hit != (enabled && inline && !withLoop && !profileEnabled) {
										t.Fatalf("path mismatch wide=%t enabled=%t inline=%t stats=%v", wide, enabled, inline, stats.Funcs)
									}
								}
							}
						}
					}
				}
			}
		}
	}
}

func TestTupleIfSelectBooleanAndMixedWidths(t *testing.T) {
	params := []wasm.ValType{wasm.I32, wasm.I32, wasm.I64, wasm.I32}
	pair := []wasm.ValType{wasm.I32, wasm.I64}
	caller := []byte{2, 1, 0x7f, 1, 0x7e, 0x20, 2, 0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x10, 1, 0x21, 5, 0x21, 4, 0x20, 4, 0xad, 0x42, 3, 0x7e, 0x20, 5, 0x85, 0x7c, 0x0b}
	helper := []byte{1, 1, 0x7e, 0x20, 0, 0x20, 0, 0x72, 0x04, 2, 0x20, 1, 0x20, 2, 0x05, 0x20, 3, 0x20, 4, 0x0b, 0x0b}
	m := modFuncs(t, funcDef{params, []wasm.ValType{wasm.I64}, caller}, funcDef{params, pair, helper}, funcDef{nil, pair, []byte{0, 0x41, 0, 0x42, 0, 0x0b}})
	for _, enabled := range []bool{false, true} {
		for _, workers := range []int{1, 4} {
			for _, condition := range []uint64{0, 1, 0xdeadbeef00000000, ^uint64(0)} {
				args := []uint64{condition, 0xdeadbeef80000001, 0x8000000000000001, ^uint64(0)}
				low, high := uint64(uint32(args[1])), args[2]
				if uint32(condition) == 0 {
					low, high = uint64(uint32(args[3])), 0
				}
				want := (3*low ^ high) + args[2]
				var stats ModuleStats
				opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"tuple-if-select": enabled}}
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				got, err := runArm64WrapperWithOptions(t, m, opts, args...)
				if err != nil || got != want {
					t.Fatalf("enabled=%t workers=%d condition=%x got=%x err=%v want=%x", enabled, workers, condition, got, err, want)
				}
				if diagnosticsEnabled && (stats.Funcs[0].Peephole["inline-tuple-if-select"] != 0) != (enabled && !profileEnabled) {
					t.Fatalf("path mismatch enabled=%t stats=%v", enabled, stats.Funcs[0].Peephole)
				}
				if diagnosticsEnabled && (stats.Funcs[0].Peephole["tuple-if-select-local-stores"] != 0) != (enabled && !profileEnabled) {
					t.Fatalf("mixed pair store path enabled=%t stats=%v", enabled, stats.Funcs[0].Peephole)
				}
			}
		}
	}
}

func TestTupleIfSelectLocalStoresPreserveOldCallerValues(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, add, mul, le := wasm.I32, byte(0x7f), byte(0x41), byte(0x6a), byte(0x6c), byte(0x4d)
		fold := func(x uint64) uint64 { return uint64(uint32(x)) }
		if wide {
			typ, bt, k, add, mul, le = wasm.I64, 0x7e, 0x42, 0x7c, 0x7e, 0x58
			fold = func(x uint64) uint64 { return x }
		}
		for _, variant := range []string{"declared", "parameters", "same-destination", "one-store", "compact"} {
			low, high := byte(2), byte(3)
			if variant == "parameters" {
				low, high = 0, 1
			} else if variant == "same-destination" {
				high = low
			}
			// Keep a deferred read of the old first parameter below the call.
			caller := []byte{1, 2, bt, 0x20, 0, k, 13, mul, 0x20, 0, 0x20, 1, 0x10, 1,
				0x21, high, 0x21, low, 0x20, low, k, 3, mul, 0x20, high, k, 5, mul, add, add, 0x0b}
			if variant == "one-store" {
				caller = []byte{1, 2, bt, 0x20, 0, k, 13, mul, 0x20, 0, 0x20, 1, 0x10, 1,
					0x21, high, k, 3, mul, 0x20, high, k, 5, mul, add, add, 0x0b}
			}
			helper := []byte{0, 0x20, 0, 0x20, 1, le, 0x04, 2, 0x20, 0, 0x20, 1, 0x05, 0x20, 1, 0x20, 0, 0x0b, 0x0b}
			params, pair := []wasm.ValType{typ, typ}, []wasm.ValType{typ, typ}
			m := modFuncs(t, funcDef{params, []wasm.ValType{typ}, caller}, funcDef{params, pair, helper}, funcDef{nil, pair, []byte{0, k, 0, k, 0, 0x0b}})
			for _, enabled := range []bool{false, true} {
				for _, workers := range []int{1, 4} {
					for _, a := range []uint64{0, 1, 0xdeadbeef80000000, ^uint64(0)} {
						for _, b := range []uint64{0, 7, 0x8000000000000001, ^uint64(0)} {
							lo, hi := fold(a), fold(b)
							if lo > hi {
								lo, hi = hi, lo
							}
							if variant == "same-destination" {
								hi = lo
							}
							var stats ModuleStats
							opts := CompileOptions{Workers: workers, CompactNative: variant == "compact", Optimizations: map[string]bool{"tuple-if-select": enabled}}
							if diagnosticsEnabled {
								opts.Stats = &stats
							}
							got, err := runArm64WrapperWithOptions(t, m, opts, a, b)
							if want := fold(13*fold(a) + 3*lo + 5*hi); err != nil || fold(got) != want {
								t.Fatalf("wide=%t variant=%s enabled=%t a=%x b=%x got=%x err=%v want=%x", wide, variant, enabled, a, b, got, err, want)
							}
							positive := variant == "declared" || variant == "parameters"
							if diagnosticsEnabled && (stats.Funcs[0].Peephole["tuple-if-select-local-stores"] != 0) != (enabled && positive && !profileEnabled) {
								t.Fatalf("variant=%s enabled=%t stats=%v", variant, enabled, stats.Funcs[0].Peephole)
							}
						}
					}
				}
			}
		}
	}
}

func TestTupleIfSelectKeepsTrapOrder(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, k, add, divS, divU := wasm.I32, byte(0x41), byte(0x6a), byte(0x6d), byte(0x6e)
		minimum := uint64(0x80000000)
		if wide {
			typ, k, add, divS, divU = wasm.I64, 0x42, 0x7c, 0x7f, 0x80
			minimum = 0x8000000000000000
		}
		caller := []byte{0, 0x20, 0, 0x20, 1, divS, 0x20, 0, 0x20, 2, 0x10, 1, add, add, 0x0b}
		helper := []byte{0, 0x20, 0, 0x20, 1, divU}
		if wide {
			helper = append(helper, 0xa7)
		}
		helper = append(helper, 0x04, 2, 0x20, 0, 0x20, 1, 0x05, 0x20, 1, 0x20, 0, 0x0b, 0x0b)
		pair := []wasm.ValType{typ, typ}
		m := modFuncs(t, funcDef{[]wasm.ValType{typ, typ, typ}, []wasm.ValType{typ}, caller}, funcDef{pair, pair, helper}, funcDef{nil, pair, []byte{0, k, 0, k, 0, 0x0b}})
		for _, enabled := range []bool{false, true} {
			for _, workers := range []int{1, 4} {
				opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"tuple-if-select": enabled}}
				got, err := runArm64WrapperWithOptions(t, m, opts, 17, 5, 3)
				if err != nil || got != 23 {
					t.Fatalf("wide=%t enabled=%t got=%d err=%v", wide, enabled, got, err)
				}
				for _, tc := range []struct {
					args [3]uint64
					code rt.TrapCode
				}{{[3]uint64{minimum, ^uint64(0), 0}, rt.TrapDivOverflow}, {[3]uint64{17, 5, 0}, rt.TrapDivZero}} {
					_, err := runArm64WrapperWithOptions(t, m, opts, tc.args[:]...)
					var trap *rt.TrapError
					if !errors.As(err, &trap) || trap.Code != tc.code {
						t.Fatalf("wide=%t enabled=%t err=%v want=%v", wide, enabled, err, tc.code)
					}
				}
			}
		}
	}
}

func TestTupleIfSelectDoesNotSpeculateTrappingArm(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, k, add, divU, le := wasm.I32, byte(0x41), byte(0x6a), byte(0x6e), byte(0x4d)
		if wide {
			typ, k, add, divU, le = wasm.I64, 0x42, 0x7c, 0x80, 0x58
		}
		params, pair := []wasm.ValType{typ, typ}, []wasm.ValType{typ, typ}
		caller := []byte{0, 0x20, 0, 0x20, 1, 0x10, 1, add, 0x0b}
		helper := []byte{0, 0x20, 0, 0x20, 1, le, 0x04, 2, 0x20, 0, 0x20, 1, 0x05, 0x20, 1, 0x20, 0, k, 0, divU, 0x0b, 0x0b}
		m := modFuncs(t, funcDef{params, []wasm.ValType{typ}, caller}, funcDef{params, pair, helper}, funcDef{nil, pair, []byte{0, k, 0, k, 0, 0x0b}})
		for _, enabled := range []bool{false, true} {
			for _, workers := range []int{1, 4} {
				var stats ModuleStats
				opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"tuple-if-select": enabled}}
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				got, err := runArm64WrapperWithOptions(t, m, opts, 2, 3)
				if err != nil || got != 5 {
					t.Fatalf("wide=%t enabled=%t got=%d err=%v", wide, enabled, got, err)
				}
				if diagnosticsEnabled {
					for _, s := range stats.Funcs {
						if s.Peephole["tuple-if-select"] != 0 || s.Peephole["inline-tuple-if-select"] != 0 {
							t.Fatal("trapping arm admitted")
						}
					}
				}
				_, err = runArm64WrapperWithOptions(t, m, opts, 3, 2)
				var trap *rt.TrapError
				if !errors.As(err, &trap) || trap.Code != rt.TrapDivZero {
					t.Fatalf("wide=%t enabled=%t err=%v", wide, enabled, err)
				}
			}
		}
	}
}
