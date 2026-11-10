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
)

func TestLinearIntegerPairWrapAndAliases(t *testing.T) {
	for _, multiplier := range []byte{3, 5, 9} {
		for _, constant := range []int64{0, 17, -1} {
			for _, alias := range []bool{false, true} {
				// k*x + (y-x) + c = (k-1)*x + y + c modulo2^32.
				body := []byte{0, 0x03, 0x7f, 0x20, 0, 0x41, multiplier, 0x6c,
					0x20, 1, 0x20, 0, 0x6b, 0x6a, 0x41}
				body = append(body, byte(constant)&0x7f) // These fixtures fit one signed LEB byte.
				body = append(body, 0x6a)
				if alias {
					body = append(body, 0x21, 0, 0x20, 0)
				}
				body = append(body, 0x0b, 0x0b)
				m := modFuncs(t, funcDef{[]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body})
				for _, workers := range []int{1, 4} {
					for _, input := range [][2]uint64{{0, 0}, {3, 7}, {0x7fffffff, 17}, {0x80000000, 0xffffffff}, {^uint64(0), ^uint64(0)}} {
						var stats ModuleStats
						opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"canonical-i32": false, "value-facts": false}}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						got, _, err := runMemAmd64WithOptions(t, m, opts, nil, input[0]|0xdeadbeef00000000, input[1]|0xcafebabe00000000)
						want := uint64((uint32(multiplier)-1)*uint32(input[0]) + uint32(input[1]) + uint32(constant))
						if err != nil || got != want {
							t.Fatalf("k=%d c=%d alias=%t workers=%d input=%x got=%x err=%v want=%x", multiplier, constant, alias, workers, input, got, err, want)
						}
						if diagnosticsEnabled && (stats.Funcs[0].Peephole["linear-integer-pair"] != 0) != !profileEnabled {
							t.Fatalf("linear pair not selected: %v", stats.Funcs[0].Peephole)
						}
					}
				}
			}
		}
	}
}

func TestLinearIntegerPairPreservesCapturedOldLocal(t *testing.T) {
	body := []byte{0, 0x03, 0x7f, 0x20, 0, 0x41, 3, 0x6c,
		0x20, 0, 0x41, 1, 0x6a, 0x21, 0,
		0x20, 1, 0x20, 0, 0x6b, 0x6a, 0x0b, 0x0b}
	m := modFuncs(t, funcDef{[]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body})
	for _, input := range [][2]uint64{{3, 7}, {0xffffffff, 0x80000000}, {0, 0}} {
		var stats ModuleStats
		opts := CompileOptions{Optimizations: map[string]bool{"value-facts": false}}
		if diagnosticsEnabled {
			opts.Stats = &stats
		}
		got, _, err := runMemAmd64WithOptions(t, m, opts, nil, input[0], input[1])
		want := uint64(2*uint32(input[0]) + uint32(input[1]) - 1)
		if err != nil || got != want {
			t.Fatalf("input=%x got=%x err=%v want=%x", input, got, err, want)
		}
		if diagnosticsEnabled && stats.Funcs[0].Peephole["linear-integer-pair"] != 0 {
			t.Fatal("captured older local read was collected as a current local")
		}
	}
}
