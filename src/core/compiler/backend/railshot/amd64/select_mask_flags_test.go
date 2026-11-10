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
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestSelectMaskFlagsPreserveWidthsAndTruth(t *testing.T) {
	saved := swarMaskTestEnabled
	defer func() { swarMaskTestEnabled = saved }()
	for _, wide := range []bool{false, true} {
		typ, bt, k, add := wasm.I32, byte(0x7f), byte(0x41), byte(0x6a)
		if wide {
			typ, bt, k, add = wasm.I64, 0x7e, 0x42, 0x7c
		}
		for _, ifForm := range []bool{false, true} {
			for _, mask := range []uint32{1, 0x80, 0x80000000, 0x80008001} {
				body := []byte{0, 0x20, 0} // Caller prefix survives the selection.
				if !ifForm {
					body = append(body, 0x20, 0, 0x20, 1)
				}
				body = append(body, 0x20, 2, 0x41)
				body = append(body, wasmtest.SLEB32(int32(mask))...)
				body = append(body, 0x71)
				if ifForm {
					body = append(body, 0x04, bt, 0x20, 0, k, 3, add, 0x05, 0x20, 1, k, 7, add, 0x0b)
				} else {
					body = append(body, 0x1b)
				}
				body = append(body, add, 0x0b)
				m := modFuncs(t, funcDef{[]wasm.ValType{typ, typ, wasm.I32}, []wasm.ValType{typ}, body})
				for _, enabled := range []bool{false, true} {
					swarMaskTestEnabled = enabled
					for _, workers := range []int{1, 4} {
						var stats ModuleStats
						opts := CompileOptions{Workers: workers, Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"canonical-i32": false, "value-facts": false}}
						cm, err := CompileModuleWith(m, opts)
						if err != nil {
							t.Fatal(err)
						}
						for _, condition := range []uint64{0, 1, 0x80, 0x80000000, 0xdeadbeef00000000, ^uint64(0)} {
							a, b := uint64(0xdeadbeef80000001), uint64(0x8000000000000007)
							value := b
							if uint32(condition)&mask != 0 {
								value = a
							}
							if ifForm {
								value += 7
								if uint32(condition)&mask != 0 {
									value -= 4
								}
							}
							want := a + value
							if !wide {
								want = uint64(uint32(want))
							}
							if got := runCompiledAmd64u(t, cm, a, b, condition); got != want {
								t.Fatalf("wide=%t if=%t mask=%x enabled=%t workers=%d cond=%x got=%x want=%x", wide, ifForm, mask, enabled, workers, condition, got, want)
							}
						}
						if cm.CodeImage != nil {
							cm.CodeImage.Close()
						}
						if diagnosticsEnabled && (stats.Funcs[0].Peephole["select-mask-flags"] != 0) != (enabled && mask <= 0xff && (!ifForm || !profileEnabled)) {
							t.Fatalf("incorrect masked selection admission: %v", stats.Funcs[0].Peephole)
						}
					}
				}
			}
		}
	}
}

func TestSelectMaskFlagsPreserveFixedRegistersAndTrapOrder(t *testing.T) {
	saved := swarMaskTestEnabled
	defer func() { swarMaskTestEnabled = saved }()
	for _, pressure := range []int{0, 6} {
		body := []byte{0}
		for range pressure {
			body = append(body, 0x20, 2, 0x41, 13, 0x6a, 0x20, 4, 0x41, 7, 0x6b, 0x20, 3, 0x1b)
		}
		body = append(body, 0x20, 0, 0x20, 1, 0x6d, 0x20, 2, 0x20, 3, 0x74,
			0x20, 4, 0x20, 5, 0x6e, 0x41, 1, 0x71, 0x1b)
		for range pressure {
			body = append(body, 0x6a)
		}
		body = append(body, 0x0b)
		m := modFuncs(t, funcDef{[]wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body})
		for _, enabled := range []bool{false, true} {
			swarMaskTestEnabled = enabled
			for _, workers := range []int{1, 4} {
				opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"canonical-i32": false, "value-facts": false}}
				for _, dividend := range []uint64{70, 77} {
					args := []uint64{100, 3, 7, 5, dividend, 7}
					for i := range args {
						args[i] |= 0xdeadbeef00000000
					}
					want := uint32(224)
					if (dividend/7)&1 != 0 {
						want = 33
					}
					want += uint32(pressure) * 20
					got, _, err := runMemAmd64WithOptions(t, m, opts, nil, args...)
					if err != nil || got != uint64(want) {
						t.Fatalf("pressure=%d enabled=%t workers=%d dividend=%d got=%x want=%x err=%v", pressure, enabled, workers, dividend, got, want, err)
					}
				}
				for _, numerator := range []uint64{0x80000000, 7} {
					_, _, err := runMemAmd64WithOptions(t, m, opts, nil, numerator, 0xffffffff, 7, 5, 13, 0)
					want := rt.TrapDivZero
					if numerator == 0x80000000 {
						want = rt.TrapDivOverflow
					}
					var trap *rt.TrapError
					if !errors.As(err, &trap) || trap.Code != want {
						t.Fatalf("pressure=%d enabled=%t workers=%d numerator=%x got=%v want=%v", pressure, enabled, workers, numerator, err, want)
					}
				}
			}
		}
	}
}

func TestSelectMaskFlagsCompactCarryCondition(t *testing.T) {
	savedMask, savedBit := swarMaskTestEnabled, singleBitMaskTestEnabled
	swarMaskTestEnabled = true
	defer func() { swarMaskTestEnabled, singleBitMaskTestEnabled = savedMask, savedBit }()
	for _, bitTest := range []bool{false, true} {
		singleBitMaskTestEnabled = bitTest
		for _, mask := range []uint32{1, 128, 0x80000000} {
			body := []byte{0, 0x20, 0, 0x20, 1, 0x20, 2, 0x41}
			body = append(body, wasmtest.SLEB32(int32(mask))...)
			body = append(body, 0x71, 0x1b, 0x0b)
			m := modFuncs(t, funcDef{[]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body})
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{CompactNative: true, Stats: optionalTestStats(&stats)})
			if err != nil {
				t.Fatal(err)
			}
			for _, condition := range []uint32{0, mask, ^mask, 0xffffffff} {
				want := uint64(29)
				if condition&mask != 0 {
					want = 17
				}
				if got := runCompiledAmd64u(t, cm, 17, 29, uint64(condition)|0xdeadbeef00000000); got != want {
					t.Fatalf("bitTest=%t mask=%x condition=%x got=%d want=%d", bitTest, mask, condition, got, want)
				}
			}
			if cm.CodeImage != nil {
				cm.CodeImage.Close()
			}
			if diagnosticsEnabled && (stats.Funcs[0].Peephole["single-bit-mask-test"] != 0) != (bitTest && mask <= 0xff) {
				t.Fatal("compact carry-condition fixture did not exercise BT/TEST selection")
			}
		}
	}
}
