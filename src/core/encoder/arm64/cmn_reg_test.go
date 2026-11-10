// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package arm64

import (
	"encoding/binary"
	"testing"
)

func TestCompareNegativeRegisters(t *testing.T) {
	for _, wide := range []bool{false, true} {
		a := Asm{}
		want := uint32(0x2B01001F)
		if wide {
			a.CmnReg64(X0, X1)
			want = 0xAB01001F
		} else {
			a.CmnReg32(X0, X1)
		}
		if len(a.B) != 4 || binary.LittleEndian.Uint32(a.B) != want {
			t.Fatalf("wide=%v encoding=%x want=%08x", wide, a.B, want)
		}
	}
}
