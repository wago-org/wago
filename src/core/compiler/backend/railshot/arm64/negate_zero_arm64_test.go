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

func TestNegateZeroSub(t *testing.T) {
	for _, tc := range []struct {
		typ                wasm.ValType
		constant, sub, add byte
		mask               uint64
	}{
		{wasm.I32, 0x41, 0x6b, 0x6a, 0xffffffff},
		{wasm.I64, 0x42, 0x7d, 0x7c, ^uint64(0)},
	} {
		for _, derived := range []bool{false, true} {
			// Keep an original local read live across negation and test
			// both direct reads and a deferred arithmetic input.
			body := []byte{0, tc.constant, 0, 0x20, 0}
			if derived {
				body = append(body, tc.constant, 1, tc.add)
			}
			body = append(body, tc.sub, 0x20, 0, 0x20, 1, 0x1b, 0x20, 0, tc.add, 0x0b)
			m := modMem(t, 1, []wasm.ValType{tc.typ, wasm.I32}, []wasm.ValType{tc.typ}, body)
			if diagnosticsEnabled && compileWithStats(t, m, false).Funcs[0].Peephole["negate-zero-sub"] == 0 {
				t.Fatalf("type=%v derived=%v: negation was not selected", tc.typ, derived)
			}
			want := uint64(0)
			if derived {
				want = tc.mask
			}
			for _, x := range []uint64{0, 1, 0x80000000, 0x8000000000000000, ^uint64(0)} {
				if got := runArm64u(t, m, x, 1) & tc.mask; got != want {
					t.Fatalf("type=%v derived=%v input=%x: got %x, want %x", tc.typ, derived, x, got, want)
				}
				if got, want := runArm64u(t, m, x, 0)&tc.mask, (x+x)&tc.mask; got != want {
					t.Fatalf("type=%v derived=%v input=%x: false arm=%x, want %x", tc.typ, derived, x, got, want)
				}
			}
		}
	}
}
