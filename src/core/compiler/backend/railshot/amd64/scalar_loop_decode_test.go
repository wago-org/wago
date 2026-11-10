// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && amd64

package amd64

import "testing"

func TestScalarLoopNopDecode(t *testing.T) {
	for _, b := range [][]byte{
		{0x90}, {0x66, 0x90}, {0x0f, 0x1f, 0x00}, {0x0f, 0x1f, 0x40, 0},
		{0x0f, 0x1f, 0x44, 0, 0}, {0x66, 0x0f, 0x1f, 0x44, 0, 0},
		{0x0f, 0x1f, 0x80, 1, 2, 3, 4}, {0x0f, 0x1f, 0x84, 0, 1, 2, 3, 4},
		{0x66, 0x0f, 0x1f, 0x84, 0, 1, 2, 3, 4},
	} {
		if got := scalarLoopNopLen(b); got != len(b) {
			t.Fatalf("NOP=%x gotlen=%d want=%d", b, got, len(b))
		}
		for n := 0; n < len(b); n++ {
			if got := scalarLoopNopLen(b[:n]); got != 0 {
				t.Fatalf("truncatedNOP=%x gotlen=%d", b[:n], got)
			}
		}
	}
	for _, b := range [][]byte{{0x41, 0x90}, {0x0f, 0x1f, 0x48, 0}, {0xb8, 0x0f, 0x1f, 0, 0}, {0xf3, 0x90}} {
		if got := scalarLoopNopLen(b); got != 0 {
			t.Fatalf("nonNOP=%x gotlen=%d", b, got)
		}
	}
}

func TestScalarLoopCounterDecodeRejectsOtherEffects(t *testing.T) {
	for _, prefix := range [][]byte{nil, {0x41}, {0x85, 0xc8}, {0x85, 0xe4}, {0x45, 0x85, 0xe4, 0x90}} {
		if _, _, ok := scalarLoopZeroCounter(prefix); ok {
			t.Fatalf("accepted noncounter condition=%x", prefix)
		}
	}
	for _, tail := range [][]byte{{0x41, 0x83, 0xec, 0xff}, {0x41, 0x83, 0xc4, 1}, {0x41, 0x83, 0xfc, 1}, {0x41, 0x83, 0xec}, {0x49, 0x83, 0xec, 1}} {
		if scalarLoopUnitDecrement(tail, R12, false) {
			t.Fatalf("accepted nonunit32 decrement=%x", tail)
		}
	}
	if !scalarLoopUnitDecrement([]byte{0x41, 0x81, 0xec, 1, 0, 0, 0}, R12, false) {
		t.Fatal("rejected decoded full immediate unit decrement")
	}
}
