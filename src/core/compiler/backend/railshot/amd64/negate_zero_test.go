// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestNegateZeroSub(t *testing.T) {
	for _, tc := range []struct {
		typ                wasm.ValType
		constant, sub, add byte
		mask               uint64
	}{
		{wasm.I32, 0x41, 0x6b, 0x6a, 0xffffffff},
		{wasm.I64, 0x42, 0x7d, 0x7c, ^uint64(0)},
	} {
		// Leave a borrowed local live across the negation as well as consuming it
		// inside a deferred expression. The arithmetic must wrap at its width.
		body := []byte{0, tc.constant, 0, 0x20, 0, tc.constant, 1, tc.add, tc.sub,
			0x20, 0, 0x20, 1, 0x1b, 0x20, 0, tc.add, 0x0b}
		m := mod1(t, []wasm.ValType{tc.typ, wasm.I32}, []wasm.ValType{tc.typ}, body)
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"interval-region-pins": false}})
		if err != nil {
			t.Fatal(err)
		}
		if cm.CodeImage != nil {
			defer cm.CodeImage.Close()
		}
		if diagnosticsEnabled && stats.Funcs[0].Peephole["negate-zero-sub"] == 0 {
			t.Fatalf("type=%v: negate lowering not selected", tc.typ)
		}
		for _, x := range []uint64{0, 1, 0x7fffffff, 0x80000000, 0xffffffff, 0x7fffffffffffffff, 0x8000000000000000, ^uint64(0)} {
			if got, want := runCompiledAmd64u(t, cm, x, 1)&tc.mask, tc.mask; got != want {
				t.Fatalf("type=%v input=%x got=%x want=%x", tc.typ, x, got, want)
			}
			if got, want := runCompiledAmd64u(t, cm, x, 0)&tc.mask, (x+x)&tc.mask; got != want {
				t.Fatalf("type=%v input=%x false arm got=%x want=%x", tc.typ, x, got, want)
			}
		}
	}
}
