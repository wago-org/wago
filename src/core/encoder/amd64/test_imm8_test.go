// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package amd64

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestLowByteImmediateTestEncoding(t *testing.T) {
	for _, tc := range []struct {
		reg  Reg
		code string
	}{
		{RAX, "f6c0a5"}, {RCX, "f6c1a5"}, {RDX, "f6c2a5"}, {RBX, "f6c3a5"},
		{RSP, "40f6c4a5"}, {RBP, "40f6c5a5"}, {RSI, "40f6c6a5"}, {RDI, "40f6c7a5"},
		{R8, "41f6c0a5"}, {R9, "41f6c1a5"}, {R10, "41f6c2a5"}, {R11, "41f6c3a5"},
		{R12, "41f6c4a5"}, {R13, "41f6c5a5"}, {R14, "41f6c6a5"}, {R15, "41f6c7a5"},
	} {
		want, err := hex.DecodeString(tc.code)
		if err != nil {
			t.Fatal(err)
		}
		a := new(Asm)
		a.TestImm8(tc.reg, 0xa5)
		if !bytes.Equal(a.B, want) {
			t.Fatalf("register=%d got=%x want=%x", tc.reg, a.B, want)
		}
	}
}
