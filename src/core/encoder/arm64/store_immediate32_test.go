// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package arm64

import "testing"

func TestNarrowStoreImmediateMove32(t *testing.T) {
	for _, size := range []int{1, 2, 4, 8} {
		for _, disp := range []int32{0, 8, 0x12345} {
			off := Asm{AllowSingleNegativeMove32: false, DisableCompactMoveImmediate32: true, DisableLogicalMoveImmediate: true, DenseIdxDisp: true}
			off.StoreImmIdx(X0, X1, disp, -17, size)
			on := Asm{AllowSingleNegativeMove32: true, DisableCompactMoveImmediate32: true, DisableLogicalMoveImmediate: true, DenseIdxDisp: true}
			on.StoreImmIdx(X0, X1, disp, -17, size)
			want := len(off.B)
			if size <= 4 {
				want -= 4 * on.SingleNegativeMoves32
			}
			if len(on.B) != want {
				t.Fatalf("size=%d disp=%x bytes=%d want=%d", size, disp, len(on.B), want)
			}
			if size <= 4 && on.SingleNegativeMoves32 < 1 {
				t.Fatal("missing narrow constant selection")
			}
		}
	}
}

func TestImmediateStoreMaterializesValueOnce(t *testing.T) {
	for _, size := range []int{1, 2, 4} {
		for _, disp := range []int32{0, -8, 5, int32(4095 * size), int32(4096 * size), 0x12345} {
			a := Asm{DenseIdxDisp: true, AllowSingleNegativeMove32: true, DisableCompactMoveImmediate32: true, DisableLogicalMoveImmediate: true}
			a.StoreImmIdx(X0, X1, disp, -17, size)
			if a.SingleNegativeMoves32 != 1 {
				t.Fatalf("size=%d disp=%d value materialized %d times", size, disp, a.SingleNegativeMoves32)
			}
		}
	}
}
