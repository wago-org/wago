// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"encoding/binary"
	"testing"
)

func TestIndirectTableOffsetEncodingARM64(t *testing.T) {
	for _, compact := range []bool{false, true} {
		for _, tc := range []struct {
			name     string
			tail     bool
			bindings []ImportBinding
		}{
			{"call", false, nil},
			{"tail", true, nil},
			{"tail-descriptor", true, []ImportBinding{}},
		} {
			m := tableOffsetModuleARM64(t, tc.tail)
			cm, err := CompileModuleWith(m, CompileOptions{ImportBindings: tc.bindings, CompactNative: compact})
			if err != nil {
				t.Fatal(err)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			wide := 0
			for pc := cm.Entry[1]; pc+4 <= len(cm.Code); pc += 4 {
				word := binary.LittleEndian.Uint32(cm.Code[pc:]) & 0xfffffc00
				switch word {
				case 0x531b6800: // LSL Wd, Wn, #5 loses high address bits.
					t.Errorf("%s compact=%v: table entry shift is 32-bit", tc.name, compact)
				case 0xd37be800: // LSL Xd, Xn, #5 preserves the byte offset.
					wide++
				}
			}
			if wide == 0 {
				t.Errorf("%s compact=%v: no 64-bit table entry shift", tc.name, compact)
			}
		}
	}
}
