// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"testing"

	x64 "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestTupleCountedLoopKeepsCompactAlignment(t *testing.T) {
	var condition, tail x64.Asm
	condition.TestSelf(R12, false)
	tail.MovRegReg32(R10, R9)
	tail.AluRI(5, R12, 1, false)
	for phase := 0; phase < 32; phase++ {
		a := x64.Asm{B: bytes.Repeat([]byte{0x90}, phase)}
		f := fn{a: &a, pinnedLocalMask: maskOf(R9, R10, R12), compactLoopAlign32: true}
		if !f.tryScalarTupleCountedLoop(condition.B, tail.B, condNE, 0) {
			t.Fatal("fixture failed counted proof")
		}
		// The final JAE group edge precedes the five-byte scalar fallback JMP.
		end := a.Len() - 5
		if end < 6 || a.B[end-6] != 0x0f || a.B[end-5] != byte(0x80|condAE) {
			t.Fatal("missing counted group edge")
		}
		target := end + int(int32(binary.LittleEndian.Uint32(a.B[end-4:end])))
		if target%32 != 0 {
			t.Fatalf("phase=%d counted group header=%d lost compact-loop-align32", phase, target)
		}
	}
}
