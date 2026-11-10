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

func TestFcvtzuMatchesAssemblerEncoding(t *testing.T) {
	for _, test := range []struct {
		sourceWide, resultWide bool
		word                   uint32
	}{{false, false, 0x1e3900a3}, {true, false, 0x1e7900a3}, {false, true, 0x9e3900a3}, {true, true, 0x9e7900a3}} {
		a := &Asm{}
		a.Fcvtzu(Reg(3), Reg(5), test.sourceWide, test.resultWide)
		if len(a.B) != 4 || binary.LittleEndian.Uint32(a.B) != test.word {
			t.Fatalf("source64=%t/result64=%t bytes=%x want=%x", test.sourceWide, test.resultWide, a.B, test.word)
		}
	}
}
