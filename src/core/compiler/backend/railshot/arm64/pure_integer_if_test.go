// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"bytes"
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
	"testing"
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
								got, err := runArm64WrapperWithOptions(t, m, opts, input[0], input[1], condition)
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
		got, err := runArm64WrapperWithOptions(t, m, opts, 7, 13, 0)
		if err != nil || uint32(got) != 20 {
			t.Fatalf("enabled=%v cold trap got=%d err=%v", enabled, got, err)
		}
		_, err = runArm64WrapperWithOptions(t, m, opts, 7, 13, 1)
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
			f := gpClassEmitter()
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
			_, err := runArm64WrapperWithOptions(t, m, opts, input, 13)
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

func TestPureIntegerIfConditionMovePreservesArenaTrees(t *testing.T) {
	f := gpClassEmitter()
	prefix := f.pushValue(storage{kind: stConst, typ: mtI64, cval: 13})
	f.pushValue(storage{kind: stLocalReg, typ: mtI32, reg: X0, idx: 0})
	f.pushValue(storage{kind: stLocalReg, typ: mtI32, reg: X1, idx: 1})
	f.pushBinOp(opEq, mtI32)
	id := f.s.head.prev
	condition := f.s.node(id)
	children := condition.st.cval
	pending := f.s.pendingDeferred
	for i := range 4096 {
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(i)})
	}
	f.s.moveValentBlockToTop(id)
	roots := f.rootsBottomToTop()
	if len(roots) != 4098 || roots[0] != prefix || roots[len(roots)-1] != condition || condition.st.cval != children || f.s.pendingDeferred != pending {
		t.Fatal("moving condition lost its tree, caller prefix or arena identity")
	}
	for i := range 4096 {
		if roots[i+1].st.cval != int64(i) {
			t.Fatalf("arm %d was reordered", i)
		}
	}
	f.s.moveValentBlockToTop(id)
	if f.s.back() != condition {
		t.Fatal("moving the already-top condition changed it")
	}
}
