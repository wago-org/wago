// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"bytes"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
)

func TestPureIntegerIfSelectPreservesConditionsAndCallerTrees(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, add, mul := wasm.I32, byte(0x7f), byte(0x41), byte(0x6a), byte(0x6c)
		fold := func(x uint64) uint64 { return uint64(uint32(x)) }
		if wide {
			typ, bt, k, add, mul = wasm.I64, 0x7e, 0x42, 0x7c, 0x7e
			fold = func(x uint64) uint64 { return x }
		}
		for _, deferredPrefix := range []bool{false, true} {
			for _, fused := range []bool{false, true} {
				body := []byte{0, 0x20, 0}
				if deferredPrefix {
					body = append(body, k, 5, mul)
				}
				body = append(body, 0x20, 2)
				if fused {
					body = append(body, 0x41, 0, 0x47)
				}
				body = append(body, 0x04, bt, 0x20, 0, k, 3, mul, 0x05, 0x20, 1, k, 7, add, 0x0b, add, 0x0b)
				m := modFuncs(t, funcDef{[]wasm.ValType{typ, typ, wasm.I32}, []wasm.ValType{typ}, body})
				for _, enabled := range []bool{false, true} {
					for _, workers := range []int{1, 4} {
						for _, input := range [][2]uint64{{0, 0}, {7, 13}, {0xdeadbeef80000000, 0xcafebabeffffffff}, {^uint64(0), 0x8000000000000001}} {
							for _, condition := range []uint64{0, 1, 0xdeadbeef00000000, ^uint64(0)} {
								want := fold(input[1] + 7)
								if uint32(condition) != 0 {
									want = fold(input[0] * 3)
								}
								prefix := fold(input[0])
								if deferredPrefix {
									prefix = fold(prefix * 5)
								}
								want = fold(prefix + want)
								var stats ModuleStats
								opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"pure-integer-if-select": enabled}}
								if diagnosticsEnabled {
									opts.Stats = &stats
								}
								got, err := runPureIfWithOptions(t, m, opts, input[0], input[1], condition)
								if err != nil || fold(got) != want {
									t.Fatalf("wide=%v prefix=%v fused=%v enabled=%v workers=%d input=%x condition=%x got=%x want=%x err=%v", wide, deferredPrefix, fused, enabled, workers, input, condition, got, want, err)
								}
								if diagnosticsEnabled && (stats.Funcs[0].Peephole["pure-integer-if-select"] != 0) != (enabled && !profileEnabled) {
									t.Fatalf("enabled=%v stats=%v", enabled, stats.Funcs[0].Peephole)
								}
							}
						}
					}
				}
			}
		}
	}
}

func TestPureIntegerIfSelectKeepsUnselectedTrapsCold(t *testing.T) {
	body := []byte{0, 0x20, 0, 0x20, 2, 0x04, 0x7f, 0x20, 0, 0x41, 0, 0x6e, 0x05, 0x20, 1, 0x0b, 0x6a, 0x0b}
	m := modFuncs(t, funcDef{[]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body})
	for _, enabled := range []bool{false, true} {
		opts := CompileOptions{Optimizations: map[string]bool{"pure-integer-if-select": enabled}}
		got, err := runPureIfWithOptions(t, m, opts, 7, 13, 0)
		if err != nil || uint32(got) != 20 {
			t.Fatalf("enabled=%v cold trap got=%d err=%v", enabled, got, err)
		}
		_, err = runPureIfWithOptions(t, m, opts, 7, 13, 1)
		var trap *rt.TrapError
		if !errors.As(err, &trap) || trap.Code != rt.TrapDivZero {
			t.Fatalf("enabled=%v selected trap: %v", enabled, err)
		}
	}
}

func TestPureIntegerIfSelectProofIsBoundedAndPreservesReader(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   []byte
		accept bool
	}{
		{"arithmetic", []byte{0x20, 0, 0x41, 3, 0x6c, 0x05, 0x20, 0, 0x41, 7, 0x6a, 0x0b}, true},
		{"div", []byte{0x20, 0, 0x41, 0, 0x6e, 0x05, 0x20, 0, 0x0b}, false},
		{"write", []byte{0x20, 0, 0x22, 0, 0x05, 0x20, 0, 0x0b}, false},
		{"call", []byte{0x20, 0, 0x10, 0, 0x05, 0x20, 0, 0x0b}, false},
		{"outer-read", []byte{0x41, 1, 0x6a, 0x05, 0x20, 0, 0x0b}, false},
		{"wrong-width", []byte{0x42, 1, 0x05, 0x20, 0, 0x0b}, false},
		{"missing-else", []byte{0x20, 0, 0x0b}, false},
		{"missing-end", []byte{0x20, 0, 0x05, 0x20, 0}, false},
		{"unknown-local", []byte{0x20, 1, 0x05, 0x20, 0, 0x0b}, false},
		{"long", append(append(bytes.Repeat([]byte{0x01}, 70), 0x20, 0, 0x05), 0x20, 0, 0x0b), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPureSelectFn()
			f.localType = []machineType{mtI32}
			r := wasm.ReaderFrom(tc.body)
			_, ok := f.scanPureIntegerIf(r, mtI32)
			if ok != tc.accept || r.Offset() != 0 {
				t.Fatalf("accepted=%v want=%v reader=%d", ok, tc.accept, r.Offset())
			}
		})
	}
}

func TestPureIntegerIfSelectPreservesPendingTrapOrder(t *testing.T) {
	body := []byte{0, 0x20, 0, 0x41, 0x7f, 0x6d, 0x20, 1, 0x41, 0, 0x6e, 0x04, 0x7f, 0x41, 1, 0x05, 0x41, 2, 0x0b, 0x6a, 0x0b}
	m := modFuncs(t, funcDef{[]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body})
	for _, enabled := range []bool{false, true} {
		opts := CompileOptions{Optimizations: map[string]bool{"pure-integer-if-select": enabled}}
		for _, input := range []uint64{0x80000000, 7} {
			_, err := runPureIfWithOptions(t, m, opts, input, 13)
			want := rt.TrapDivZero
			if input == 0x80000000 {
				want = rt.TrapDivOverflow
			}
			var trap *rt.TrapError
			if !errors.As(err, &trap) || trap.Code != want {
				t.Fatalf("enabled=%v input=%x expected=%v got=%v", enabled, input, want, err)
			}
		}
	}
}

func TestPureIntegerIfSelectPreservesEarlierTrapAndConditionWrite(t *testing.T) {
	trapping := modFuncs(t, funcDef{[]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32},
		[]byte{0, 0x20, 0, 0x41, 0x7f, 0x6d, 0x20, 1, 0x04, 0x7f, 0x41, 3, 0x05, 0x41, 7, 0x0b, 0x6a, 0x0b}})
	writing := modFuncs(t, funcDef{[]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32},
		[]byte{0, 0x20, 0, 0x20, 1, 0x22, 0, 0x04, 0x7f, 0x20, 0, 0x41, 3, 0x6c, 0x05, 0x20, 0, 0x41, 7, 0x6a, 0x0b, 0x6a, 0x0b}})
	for _, enabled := range []bool{false, true} {
		for _, workers := range []int{1, 4} {
			for _, condition := range []uint64{0, 1, 0xdeadbeef00000000, ^uint64(0)} {
				var stats ModuleStats
				opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"pure-integer-if-select": enabled}}
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				_, err := runPureIfWithOptions(t, trapping, opts, 0x80000000, condition)
				var trap *rt.TrapError
				if !errors.As(err, &trap) || trap.Code != rt.TrapDivOverflow {
					t.Fatalf("enabled=%t workers=%d condition=%x trap=%v", enabled, workers, condition, err)
				}
				if diagnosticsEnabled && (stats.Funcs[0].Peephole["pure-integer-if-select"] != 0) != (enabled && !profileEnabled) {
					t.Fatal("earlier-trap fixture did not exercise selection")
				}
				x := uint32(condition)
				want := uint32(17) + x + 7
				if x != 0 {
					want = 17 + 3*x
				}
				got, err := runPureIfWithOptions(t, writing, opts, 17, condition)
				if err != nil || got != uint64(want) {
					t.Fatalf("enabled=%t workers=%d condition=%x got=%x want=%x err=%v", enabled, workers, condition, got, want, err)
				}
			}
		}
	}
}

func runPureIfWithOptions(t *testing.T, m *wasm.Module, opts CompileOptions, args ...uint64) (uint64, error) {
	value, _, err := runMemAmd64WithOptions(t, m, opts, nil, args...)
	return value, err
}

func TestPureIntegerIfConditionMovePreservesTreesAndBarrierCursor(t *testing.T) {
	f := newPureSelectFn()
	prefix := f.pushValue(storage{kind: stConst, typ: mtI64, cval: 13})
	f.pushValue(storage{kind: stLocalReg, typ: mtI32, reg: R9, idx: 0})
	f.pushValue(storage{kind: stLocalReg, typ: mtI32, reg: R10, idx: 1})
	f.pushBinOp(opEq, mtI32)
	condition := f.s.back()
	left, right, depth := condition.arg0, condition.arg1, f.s.logicalDepth
	f.s.pendingPrefix, f.s.pendingPrefixLoads = condition, true
	for i := range 4096 {
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(i)})
	}
	f.s.moveValentBlockToTop(condition)
	roots := f.rootsBottomToTop()
	if len(roots) != 4098 || roots[0] != prefix || roots[len(roots)-1] != condition || condition.arg0 != left || condition.arg1 != right || f.s.logicalDepth != depth+4096 || f.s.pendingPrefix != nil || f.s.pendingPrefixLoads {
		t.Fatal("moving condition lost its tree, prefix, depth, or barrier cursor")
	}
	for i := range 4096 {
		if roots[i+1].st.cval != int64(i) {
			t.Fatalf("arm %d was reordered", i)
		}
	}
	f.s.moveValentBlockToTop(condition)
	if f.s.back() != condition {
		t.Fatal("moving top condition changed it")
	}
}
