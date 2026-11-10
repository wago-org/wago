// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestShiftedMulAddPreservesAliasesAndWidths(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, constant, add, sub, mul := wasm.I32, byte(0x41), byte(0x6a), byte(0x6b), byte(0x6c)
		fold := func(x uint64) uint64 { return uint64(uint32(x)) }
		if wide {
			typ, constant, add, sub, mul = wasm.I64, 0x42, 0x7c, 0x7d, 0x7e
			fold = func(x uint64) uint64 { return x }
		}
		for _, coefficient := range []uint64{3, 5, 9, 17, 33, 65, 0x80000001, 0x8000000000000001} {
			encoded := wasmtest.SLEB32(int32(uint32(coefficient)))
			if wide {
				encoded = wasmtest.SLEB64(int64(coefficient))
			}
			for _, subtract := range []bool{false, true} {
				for _, reverse := range []bool{false, true} {
					for _, variant := range []string{"return", "source", "accumulator", "same-input", "prefix"} {
						accumulator := byte(1)
						if variant == "same-input" {
							accumulator = 0
						}
						body := []byte{0}
						if variant == "prefix" {
							body = append(body, 0x20, 0)
						}
						body = append(body, 0x20, accumulator)
						if reverse {
							body = append(append(body, constant), encoded...)
							body = append(body, 0x20, 0)
						} else {
							body = append(append(body, 0x20, 0, constant), encoded...)
						}
						body = append(body, mul)
						if subtract {
							body = append(body, sub)
						} else {
							body = append(body, add)
						}
						if variant != "return" {
							destination := byte(0)
							if variant == "accumulator" {
								destination = 1
							}
							body = append(body, 0x21, destination, 0x20, destination)
						}
						if variant == "prefix" {
							body = append(body, add)
						}
						body = append(body, 0x0b)
						m := mod1(t, []wasm.ValType{typ, typ}, []wasm.ValType{typ}, body)
						for _, enabled := range []bool{false, true} {
							for _, input := range [][2]uint64{{0, 0}, {7, 13}, {0xdeadbeef80000000, 0xcafebabeffffffff}, {^uint64(0), 0x8000000000000001}} {
								x, y := fold(input[0]), fold(input[1])
								if variant == "same-input" {
									y = x
								}
								want := y + x*coefficient
								if subtract {
									want = y - x*coefficient
								}
								if variant == "prefix" {
									want += x
								}
								var stats ModuleStats
								opts := CompileOptions{Optimizations: map[string]bool{"mul-add-fuse": enabled}}
								if diagnosticsEnabled {
									opts.Stats = &stats
								}
								got, err := runArm64WrapperWithOptions(t, m, opts, input[:]...)
								if err != nil || fold(got) != fold(want) {
									t.Fatalf("wide=%t c=%x subtract=%t reverse=%t variant=%s enabled=%t input=%x got=%x err=%v want=%x", wide, coefficient, subtract, reverse, variant, enabled, input, got, err, fold(want))
								}
								c := fold(coefficient)
								if diagnosticsEnabled && (stats.Funcs[0].Peephole["mul-add-shifted-product"] != 0) != (enabled && c > 2) {
									t.Fatalf("wide=%t c=%x variant=%s enabled=%t stats=%v", wide, coefficient, variant, enabled, stats.Funcs[0].Peephole)
								}
							}
						}
					}
				}
			}
		}
	}
}
