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
)

func TestSelectBorrowedConditionUsedByDeferredArm(t *testing.T) {
	// The condition is also a shift count and the false alternative. It must
	// survive shift lowering and remain available afterward.
	body := []byte{0}
	for range 5 {
		body = append(body, 0x20, 0, 0x20, 1, 0x74, 0x20, 1, 0x20, 1, 0x1b, 0x1a)
	}
	body = append(body, 0x20, 0, 0x20, 1, 0x74, 0x20, 1, 0x20, 1, 0x1b, 0x20, 1, 0x6a, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, condition := range []uint32{0, 1, 2, 31, 32, ^uint32(0)} {
		want := condition
		if condition != 0 {
			want += uint32(12) << (condition & 31)
		}
		var stats ModuleStats
		opts := CompileOptions{}
		if diagnosticsEnabled {
			opts.Stats = &stats
		}
		got, err := runArm64WrapperWithOptions(t, m, opts, 12, uint64(condition))
		if err != nil || got != uint64(want) {
			t.Fatalf("condition=%x: got %x want %x: %v", condition, got, want, err)
		}
		if diagnosticsEnabled && stats.Funcs[0].Peephole["select-condition-read-borrow"] == 0 {
			t.Fatal("condition was not borrowed")
		}
	}
}

func TestSelectFlagsBorrowedFalseArm(t *testing.T) {
	body := []byte{0}
	for i := 0; i < 5; i++ {
		// select(x << y, y, x < y)
		body = append(body, 0x20, 0, 0x20, 1, 0x74, 0x20, 1, 0x20, 0, 0x20, 1, 0x48, 0x1b)
		if i < 4 {
			body = append(body, 0x1a)
		}
	}
	body = append(body, 0x20, 1, 0x6a, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, enabled := range []bool{false, true} {
		for _, y := range []uint32{0, 1, 12, 31, 32, ^uint32(0)} {
			want := y + y
			if int32(12) < int32(y) {
				want = uint32(12)<<(y&31) + y
			}
			var stats ModuleStats
			opts := CompileOptions{Optimizations: map[string]bool{"select-source-read": enabled}}
			if diagnosticsEnabled {
				opts.Stats = &stats
			}
			got, err := runArm64WrapperWithOptions(t, m, opts, 12, uint64(y))
			if err != nil || got != uint64(want) {
				t.Fatalf("enabled=%v y=%x: got %x want %x: %v", enabled, y, got, want, err)
			}
			if diagnosticsEnabled {
				peeps := stats.Funcs[0].Peephole
				if peeps["select-flags"] == 0 || (peeps["select-source-read"] > 0) != enabled {
					t.Fatalf("enabled=%v: unexpected select lowering %v", enabled, peeps)
				}
			}
		}
	}
}
