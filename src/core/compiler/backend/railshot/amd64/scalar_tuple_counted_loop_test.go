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

	x64 "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestTupleUnitCounterRejectsIntermediateWrites(t *testing.T) {
	for _, wide := range []bool{false, true} {
		var condition, decrement x64.Asm
		condition.TestSelf(R12, wide)
		decrement.AluRI(5, R12, 1, wide)
		pins := maskOf(R9, R10, R12)
		var body x64.Asm
		if wide {
			body.MovReg64(R10, R12)
		} else {
			body.MovRegReg32(R10, R12)
		}
		body.AluRR(0x01, R10, R9, wide)
		tail := append(append([]byte{}, body.B...), decrement.B...)
		for _, valid := range [][]byte{decrement.B, tail} {
			counter, gotWide, ok := tupleLoopUnitCounter(condition.B, valid, pins)
			if !ok || counter != R12 || gotWide != wide {
				t.Fatalf("wide=%t failed valid per-iteration counter read: %x", wide, valid)
			}
		}
		var overwrite, wrongStep, wrongTest x64.Asm
		overwrite.MovRegReg32(R12, R9)
		overwrite.B = append(overwrite.B, decrement.B...)
		wrongStep.AluRI(5, R12, 2, wide)
		wrongTest.TestSelf(R12, !wide)
		for _, tc := range []struct{ prefix, tail []byte }{
			{condition.B, overwrite.B},
			{condition.B, wrongStep.B},
			{wrongTest.B, tail},
			{append(append([]byte{}, condition.B...), 0x90), tail},
			{condition.B, append(append([]byte{}, tail...), 0x90)},
		} {
			if _, _, ok := tupleLoopUnitCounter(tc.prefix, tc.tail, pins); ok {
				t.Fatalf("wide=%t accepted unproved counter prefix=%x tail=%x", wide, tc.prefix, tc.tail)
			}
		}
		if _, _, ok := tupleLoopUnitCounter(condition.B, tail, maskOf(R9, R10)); ok {
			t.Fatal("accepted unpinned counter")
		}
	}
}
